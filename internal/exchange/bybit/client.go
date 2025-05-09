// internal/exchange/bybit/client.go
package bybit

import (
	"context"
	"encoding/json"
	"fmt"
	"math" // Added for triggerReconnect
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"trading-system/internal/config"
	"trading-system/internal/logging"
	"trading-system/internal/types"
	"nhooyr.io/websocket" // Ensure this is imported
	"log/slog" // Ensure slog is imported
	"errors" // Ensure errors is imported
)

// Client implements the ExchangeClient interface for Bybit.
type Client struct {
	cfg          *config.BybitConfig
	instrument   *types.Instrument // The instrument we are trading/monitoring
	orderbook    *types.Orderbook  // Local orderbook state
	wsConn       *WSConnection     // WebSocket connection manager
	logger       *slog.Logger

	// State for sequence number handling and reconnection
	expectedSeq atomic.Uint64
	isSnapshot  atomic.Bool // True if we are waiting for or just received a snapshot
	isConnected atomic.Bool // Tracks connection status

	// For reconnect logic
	reconnectMu sync.Mutex
	reconnectAttempt atomic.Uint32 // Counts consecutive reconnect attempts

	// Context for the client's operations
	ctx context.Context
	cancel context.CancelFunc

	// Channel to signal orderbook updates to the strategy/processor
	obUpdateSignalChan chan struct{} // Signal when the internal OB state is updated
}

// NewClient creates a new Bybit client.
// It requires a parent context for cancellation.
func NewClient(ctx context.Context, cfg *config.BybitConfig) *Client {
	instrument := &types.Instrument{
		Symbol: cfg.Symbol,
		BaseCurrency: types.Currency(cfg.Symbol[:len(cfg.Symbol)-4]), // Basic guess
		QuoteCurrency: types.Currency(cfg.Symbol[len(cfg.Symbol)-4:]), // Basic guess
		MinLotSize: atomic.Uint64{}, // Placeholder, should be fetched from exchange info
		ContractType: types.ContractTypeUnknown, // Placeholder, should be fetched
	}

	clientCtx, cancel := context.WithCancel(ctx)

	ob := types.NewOrderbook(instrument)

	return &Client{
		cfg:        cfg,
		instrument: instrument,
		orderbook:  ob, // Initialize the orderbook
		logger:     logging.GetLogger().With("exchange", "bybit"),
		ctx:        clientCtx,
		cancel:     cancel,
		obUpdateSignalChan: make(chan struct{}, 1), // Buffered channel to avoid blocking sender
	}
}

// OrderbookUpdates returns a channel that receives a signal
// whenever the internal orderbook state has been updated.
// The receiver should then call GetOrderbook() to get the latest state.
func (c *Client) OrderbookUpdates() <-chan struct{} {
	return c.obUpdateSignalChan
}


// --- Other methods (GetExchangeType, GetOrderbook, SubscribeOrderbook, Close) remain the same ---
// --- processMessages also remains the same, calling handleSnapshot/handleDelta ---

// handleSnapshot processes a full orderbook snapshot.
func (c *Client) handleSnapshot(msg WSMessage) {
	// ... (previous snapshot processing logic) ...

	// Snapshot received and processed, now ready for deltas
	c.expectedSeq.Store(uint64(data.Seq) + 1) // Expect the next sequence number (Using data.Seq as example, verify with Bybit docs UpdateID vs Seq)
	c.isSnapshot.Store(false) // No longer waiting for snapshot

	c.logger.Info("Orderbook snapshot processed successfully", "symbol", data.Symbol, "seq", data.Seq, "bid_count", c.countSyncMap(c.orderbook.Bids), "ask_count", c.countSyncMap(c.orderbook.Asks))

	// Signal that the orderbook is updated (non-blocking send)
	select {
	case c.obUpdateSignalChan <- struct{}{}:
		c.logger.Debug("Orderbook update signal sent after snapshot")
	default:
		c.logger.Debug("Orderbook update signal channel full after snapshot, skipping send")
	}
}

// handleDelta processes an incremental orderbook update.
func (c *Client) handleDelta(msg WSMessage) {
	// ... (previous delta processing logic) ...

	// If sequence gap detected or still waiting for snapshot, return early (handled in M2)
	if c.isSnapshot.Load() || receivedSeq > expected {
		return // Error already logged and reconnect triggered
	}

	// Process delta updates (add, update, remove levels)
	c.updateLevels(c.orderbook.Bids, data.Bids, types.Buy)
	c.updateLevels(c.orderbook.Asks, data.Asks, types.Sell)

	// Update sequence number and timestamp *after* successful processing
	c.orderbook.UpdateSequenceNumber(receivedSeq)
	c.orderbook.UpdateTimestamp()
	c.expectedSeq.Store(receivedSeq + 1) // Update expected sequence for the next delta

	c.logger.Debug("Orderbook delta processed", "seq", receivedSeq)

	// Signal that the orderbook has been updated (non-blocking send)
	select {
	case c.obUpdateSignalChan <- struct{}{}:
		c.logger.Debug("Orderbook update signal sent after delta")
	default:
		c.logger.Debug("Orderbook update signal channel full after delta, skipping send")
	}
}

// ... (updateLevels, Close, triggerReconnect, countSyncMap methods remain the same) ...