// internal/exchange/bybit/client.go
package bybit

import (
	 
	"context"
	"encoding/json" // Keep json import as it's used for message Data
	"fmt"
	"math" // Added for triggerReconnect
	"math/rand"
	"strconv" // Keep strconv as it's used for parsing prices/quantities
	"sync"
	"sync/atomic"
	"time"
	"github.com/souravmenon1999/trade-engine/internal/config" // Corrected import path
	"github.com/souravmenon1999/trade-engine/internal/logging"
	"github.com/souravmenon1999/trade-engine/internal/types"
	// REMOVE THIS LINE: "nhooyr.io/websocket" // Ensure this is imported
	"log/slog" // Ensure slog is imported
)

// Note: The WSConnection struct and its methods (Connect, Send, Messages, Close)
// are defined in ws.go. This client.go file uses that abstraction.
// Make sure your internal/exchange/bybit/ws.go file is updated
// to use github.com/gorilla/websocket as shown in the previous step.

// Client implements the ExchangeClient interface for Bybit.
type Client struct {
	cfg          *config.BybitConfig
	instrument   *types.Instrument // The instrument we are trading/monitoring
	orderbook    *types.Orderbook  // Local orderbook state
	wsConn       *WSConnection     // WebSocket connection manager (defined in ws.go)
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
	// Assuming symbol format like "ETHUSDT" for basic currency extraction
    // In a real application, you'd likely fetch instrument details via REST API or configure them fully.
	baseCurrency := ""
	quoteCurrency := ""
	if len(cfg.Symbol) > 4 { // Simple check assuming QuoteCurrency is 3-4 chars
		quoteCurrency = cfg.Symbol[len(cfg.Symbol)-4:]
		if types.Currency(quoteCurrency) == types.CurrencyUSDT || types.Currency(quoteCurrency) == types.Currency("USDC") { // Add other common quote currencies
			baseCurrency = cfg.Symbol[:len(cfg.Symbol)-4]
		} else if len(cfg.Symbol) > 3 { // Try 3 chars
            quoteCurrency = cfg.Symbol[len(cfg.Symbol)-3:]
             if types.Currency(quoteCurrency) == types.Currency("BTC") { // Example
                baseCurrency = cfg.Symbol[:len(cfg.Symbol)-3]
            } else {
                 // Fallback or error if format is unexpected
                 logging.GetLogger().Warn("Could not guess base/quote currency from symbol, using full symbol", "symbol", cfg.Symbol)
                 baseCurrency = cfg.Symbol // Use full symbol as base, quote empty
                 quoteCurrency = ""
            }
        } else {
             logging.GetLogger().Warn("Could not guess base/quote currency from symbol, using full symbol", "symbol", cfg.Symbol)
             baseCurrency = cfg.Symbol // Use full symbol as base, quote empty
             quoteCurrency = ""
        }

	} else {
        logging.GetLogger().Warn("Symbol too short to guess base/quote currency, using full symbol", "symbol", cfg.Symbol)
        baseCurrency = cfg.Symbol // Use full symbol as base, quote empty
        quoteCurrency = ""
    }

	instrument := &types.Instrument{
		Symbol: cfg.Symbol,
		BaseCurrency: types.Currency(baseCurrency),
		QuoteCurrency: types.Currency(quoteCurrency),
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

// GetExchangeType returns the type of this exchange client.
func (c *Client) GetExchangeType() types.ExchangeType {
	return types.ExchangeBybit
}

// GetOrderbook returns the current state of the local orderbook.
// It returns a snapshot to ensure consistency when read externally.
func (c *Client) GetOrderbook() *types.Orderbook {
	// Return a snapshot to avoid concurrent modification issues
	return c.orderbook.Snapshot()
}


// SubscribeOrderbook connects to the WS, subscribes, and starts processing.
func (c *Client) SubscribeOrderbook(ctx context.Context, symbol string) error {
	// Ensure we only attempt one connection at a time
	c.reconnectMu.Lock()
	if c.isConnected.Load() {
		c.reconnectMu.Unlock()
		c.logger.Info("Already connected, skipping subscription attempt")
		return nil // Already subscribed and connected
	}
	c.reconnectMu.Unlock()


	c.logger.Info("Attempting to subscribe to orderbook", "symbol", symbol, "url", c.cfg.WSURL)

	// Use the client's internal context for WS connection
	// Create a NEW WSConnection instance on each connect attempt
	wsConn := NewWSConnection(c.ctx, c.cfg.WSURL)
	c.wsConn = wsConn // Store the connection manager

	if err := wsConn.Connect(); err != nil {
		c.logger.Error("Failed initial WebSocket connection", "error", err)
		c.isConnected.Store(false) // Mark as disconnected on failure
        // Wait a moment before triggering reconnect to avoid tight loop on instant failure
        go func() {
            time.Sleep(time.Second) // Wait 1 second
            c.triggerReconnect() // Attempt reconnect on initial failure
        }()
		return err
	}

	c.isConnected.Store(true)
	c.reconnectAttempt.Store(0) // Reset reconnect counter on successful connection

	// Send the subscription message
	subMsg := WSSubscribe{
		Op: "subscribe",
		Args: []string{TopicOrderbook + symbol},
		ReqID: fmt.Sprintf("sub-ob-%s-%d", symbol, time.Now().UnixNano()), // Unique request ID
	}
	if err := wsConn.Send(subMsg); err != nil {
		c.logger.Error("Failed to send subscription message", "error", err)
		// Sending subscription failed - connection might still be open but useless for this topic
		// Mark as disconnected and trigger reconnect.
		c.isConnected.Store(false) // Mark as disconnected if send fails
		c.triggerReconnect()
		return err // Consider if connection should be closed immediately
	}

	c.logger.Info("Subscription message sent", "symbol", symbol)

	// Start processing incoming messages
	// This goroutine will exit if the wsConn.Messages() channel is closed (e.g. on WS error/close)
	go c.processMessages(wsConn.Messages())

	// The function returns, processing continues in goroutines.
	return nil
}

// processMessages reads from the WebSocket message channel and handles messages.
func (c *Client) processMessages(msgChan <-chan WSMessage) {
	c.logger.Info("Starting message processing loop")
	defer c.logger.Info("Message processing loop stopped")

	// Signal that we are waiting for a snapshot
	c.isSnapshot.Store(true)
    c.expectedSeq.Store(0) // Reset expected sequence when waiting for snapshot

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("Client context cancelled, stopping message processing")
			return // Client is shutting down
		case msg, ok := <-msgChan:
			if !ok {
				// Channel closed, means WS connection is likely broken (read loop exited)
				c.logger.Warn("WebSocket message channel closed. Connection lost.")
				c.isConnected.Store(false)
				c.triggerReconnect() // Attempt to reconnect
				return // Exit processing loop as channel is closed
			}

			// Handle different message types
			switch msg.Type {
			case "snapshot":
				c.handleSnapshot(msg)
			case "delta":
				c.handleDelta(msg)
			// Handle other types like "subscribe", "pong" etc.
			case "subscribe":
				if msg.Success {
					c.logger.Info("Subscription confirmed", "topic", msg.Topic, "req_id", msg.RequestID)
				} else {
					c.logger.Error("Subscription failed", "topic", msg.Topic, "code", msg.Error, "msg", msg.ErrorMsg)
					// Handle failed subscription - maybe critical error or retry?
					// For now, just log. Depending on error code, might trigger reconnect.
					if msg.Error == 10001 || msg.Error == 10005 { // Example error codes for invalid topic/auth
                         c.logger.Error("Critical subscription error, triggering reconnect", "code", msg.Error)
                         c.isConnected.Store(false)
                         c.triggerReconnect()
                         return // Exit processing loop
                    }
				}
			case "pong":
				c.logger.Debug("Received pong")
				// The ws.go handles resetting read deadline on pong. No action needed here.
			case "": // Empty type might indicate a response or error
				if msg.Error != 0 {
					c.logger.Error("Received error message from WS", "code", msg.Error, "msg", msg.ErrorMsg)
					// Decide how to handle specific errors
					// Example: if it's a critical error, close and maybe don't reconnect immediately
				} else if msg.RequestID != "" {
                    c.logger.Debug("Received WS response", "success", msg.Success, "req_id", msg.RequestID)
                } else {
                    c.logger.Debug("Received unhandled empty type message", "msg", msg)
                }
			default:
				c.logger.Debug("Received unhandled message type", "type", msg.Type, "topic", msg.Topic)
			}
		}
	}
}


// handleSnapshot processes a full orderbook snapshot.
func (c *Client) handleSnapshot(msg WSMessage) {
	var data OrderbookData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.logger.Error("Failed to unmarshal snapshot data", "error", err)
		return
	}

	c.logger.Info("Received orderbook snapshot", "symbol", data.Symbol, "seq", data.Seq)

	// Clear the existing orderbook and rebuild from the snapshot
	c.orderbook.Bids = &sync.Map{}
	c.orderbook.Asks = &sync.Map{}

	// Populate bids and asks using the corrected parsing logic
	c.populateOrderbookLevels(c.orderbook.Bids, data.Bids, types.Buy)
	c.populateOrderbookLevels(c.orderbook.Asks, data.Asks, types.Sell)


	// Update sequence number and timestamp
	// Bybit uses 'u' (Update ID) for sequence in V5. Use data.UpdateID.
	c.orderbook.UpdateSequenceNumber(uint64(data.UpdateID))
	c.orderbook.UpdateTimestamp()

	// Snapshot received and processed, now ready for deltas
	c.expectedSeq.Store(uint64(data.UpdateID) + 1) // Expect the next update ID
	c.isSnapshot.Store(false) // No longer waiting for snapshot

	c.logger.Info("Orderbook snapshot processed successfully", "symbol", data.Symbol, "seq", data.UpdateID, "bid_count", c.countSyncMap(c.orderbook.Bids), "ask_count", c.countSyncMap(c.orderbook.Asks))

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
	var data OrderbookData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.logger.Error("Failed to unmarshal delta data", "error", err)
		return
	}

	// If we are still waiting for a snapshot, ignore deltas
	if c.isSnapshot.Load() {
		c.logger.Debug("Ignoring delta, waiting for snapshot", "seq", data.UpdateID)
		return
	}

	expected := c.expectedSeq.Load()            // Get the sequence number we currently expect
	receivedSeq := uint64(data.UpdateID)        // Use data.UpdateID for sequence in V5

	// Sequence validation
	if receivedSeq < expected {
		// Old update, ignore
		c.logger.Debug("Received old delta update, ignoring", "received_seq", receivedSeq, "expected_seq", expected)
		return
	}

	if receivedSeq > expected {
		// Sequence gap detected!
		c.logger.Error("Sequence gap detected!", "expected_seq", expected, "received_seq", receivedSeq)
		c.isConnected.Store(false) // Mark as disconnected due to gap
		c.triggerReconnect() // Trigger reconnection to get a new snapshot
		return // Stop processing deltas until new snapshot
	}

	// Process delta updates (add, update, remove levels) using corrected parsing
	c.updateOrderbookLevels(c.orderbook.Bids, data.Bids, types.Buy)
	c.updateOrderbookLevels(c.orderbook.Asks, data.Asks, types.Sell)

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

func (c *Client) populateOrderbookLevels(levelsMap *sync.Map, entries [][]json.RawMessage, side types.Side) {
	for _, entry := range entries {
		if len(entry) != 2 {
			c.logger.Warn("Unexpected level entry format", "entry", string(entry[0])) // Log raw bytes for debug
			continue
		}

		// --- Corrected Parsing Logic ---
		var priceStr, quantityStr string

		// Unmarshal RawMessage into string to handle escaped quotes
		if err := json.Unmarshal(entry[0], &priceStr); err != nil {
			c.logger.Error("Failed to unmarshal price json.RawMessage into string", "error", err, "raw_bytes", string(entry[0]))
			continue
		}
		if err := json.Unmarshal(entry[1], &quantityStr); err != nil {
			c.logger.Error("Failed to unmarshal quantity json.RawMessage into string", "error", err, "raw_bytes", string(entry[1]))
			continue
		}

		// Parse the string as float64
		priceFloat, err := strconv.ParseFloat(priceStr, 64)
		if err != nil {
			c.logger.Error("Failed to parse price string as float", "error", err, "price_str", priceStr)
			continue
		}
		quantityFloat, err := strconv.ParseFloat(quantityStr, 64)
		if err != nil {
			c.logger.Error("Failed to parse quantity string as float", "error", err, "quantity_str", quantityStr)
			continue
		}

		// Scale the floats to uint64
		scaledPrice := uint64(priceFloat * 1e6)
		scaledQuantity := uint64(quantityFloat * 1e6)

		// Add or update the level in the map
		level, ok := levelsMap.Load(scaledPrice) // Use scaled price as the key
		if !ok {
			// New price level
			level = &types.PriceLevel{Price: scaledPrice}
			levelsMap.Store(scaledPrice, level)
			// Log only info/debug for new levels in snapshot, warning for unexpected in delta if separate func
			c.logger.Debug("Added price level", "side", side, "price", priceFloat, "quantity", quantityFloat, "scaled_price", scaledPrice, "scaled_quantity", scaledQuantity)
		}
		// Update quantity using atomic store
		level.(*types.PriceLevel).Quantity.Store(scaledQuantity)
		c.logger.Debug("Updated price level quantity", "side", side, "price", priceFloat, "quantity", quantityFloat, "scaled_price", scaledPrice, "scaled_quantity", scaledQuantity)

		// Note: This helper doesn't handle quantity == 0 for removal.
		// A dedicated update function is better for deltas.
	}
}

// updateLevels applies delta updates to a bid or ask map.
func (c *Client) updateOrderbookLevels(levelsMap *sync.Map, updates [][]json.RawMessage, side types.Side) {
	for _, entry := range updates {
		if len(entry) != 2 {
			c.logger.Warn("Unexpected level entry format in delta", "entry", string(entry[0])) // Log raw bytes for debug
			continue
		}

		// --- Corrected Parsing Logic ---
		var priceStr, quantityStr string

		// Unmarshal RawMessage into string to handle escaped quotes
		if err := json.Unmarshal(entry[0], &priceStr); err != nil {
			c.logger.Error("Failed to unmarshal price json.RawMessage into string in delta", "error", err, "raw_bytes", string(entry[0]))
			continue
		}
		if err := json.Unmarshal(entry[1], &quantityStr); err != nil {
			c.logger.Error("Failed to unmarshal quantity json.RawMessage into string in delta", "error", err, "raw_bytes", string(entry[1]))
			continue
		}

		// Parse the string as float64
		priceFloat, err := strconv.ParseFloat(priceStr, 64)
		if err != nil {
			c.logger.Error("Failed to parse price string as float in delta", "error", err, "price_str", priceStr)
			continue
		}
		quantityFloat, err := strconv.ParseFloat(quantityStr, 64)
		if err != nil {
			c.logger.Error("Failed to parse quantity string as float in delta", "error", err, "quantity_str", quantityStr)
			continue
		}

		// Scale the floats to uint64
		scaledPrice := uint64(priceFloat * 1e6)
		scaledQuantity := uint64(quantityFloat * 1e6)


		if scaledQuantity == 0 {
			// Quantity 0 means remove the level
			if _, loaded := levelsMap.LoadAndDelete(scaledPrice); loaded {
				c.logger.Debug("Removed price level", "side", side, "price", priceFloat, "scaled_price", scaledPrice)
			} else {
                 c.logger.Debug("Attempted to remove non-existent price level", "side", side, "price", priceFloat, "scaled_price", scaledPrice)
            }

		} else {
			// Add or update the level
			level, ok := levelsMap.Load(scaledPrice) // Use scaled price as the key
			if !ok {
				// New price level
				level = &types.PriceLevel{Price: scaledPrice}
				levelsMap.Store(scaledPrice, level)
				c.logger.Debug("Added new price level from delta", "side", side, "price", priceFloat, "quantity", quantityFloat, "scaled_price", scaledPrice, "scaled_quantity", scaledQuantity)
			}
			// Update quantity using atomic store
			level.(*types.PriceLevel).Quantity.Store(scaledQuantity)
			c.logger.Debug("Updated price level quantity from delta", "side", side, "price", priceFloat, "quantity", quantityFloat, "scaled_price", scaledPrice, "scaled_quantity", scaledQuantity)
		}
	}
}

// SubmitOrder is not implemented for the Bybit data client.
func (c *Client) SubmitOrder(ctx context.Context, order types.Order) (string, error) {
	c.logger.Warn("SubmitOrder is not implemented for the Bybit data client.")
	return "", fmt.Errorf("SubmitOrder not supported by Bybit data client")
}


// Close cleans up the Bybit client resources.
func (c *Client) Close() error {
	c.cancel() // Cancel the client's context, which also cancels the WS connection context
	c.logger.Info("Bybit client shutting down")

    // Close the message channel first to signal processor loop to stop
    // close(c.obUpdateSignalChan) // Optional: close the signal channel

	// Close the WebSocket connection manager
	if c.wsConn != nil {
		return c.wsConn.Close()
	}
	return nil
}

// triggerReconnect attempts to reconnect after a delay.
func (c *Client) triggerReconnect() {
	c.reconnectMu.Lock()
	defer c.reconnectMu.Unlock()

	if c.isConnected.Load() {
		return // Already reconnected
	}

	attempt := c.reconnectAttempt.Add(1)
	// Exponential backoff
	baseDelay := time.Second * time.Duration(math.Pow(2, float64(attempt-1)))

    // Add jitter: a random duration up to 1 second
    // Use rand.Float64() for a float between 0.0 and 1.0, then scale it.
	jitter := time.Duration(rand.Float64() * float64(time.Second))
	delay := baseDelay + jitter

	// Cap the delay
	maxDelay := 60 * time.Second
	if delay > maxDelay {
		delay = maxDelay
	}

	c.logger.Warn("Connection lost or sequence gap, attempting reconnect", "attempt", attempt, "delay", delay)

	// Use a goroutine to attempt reconnection after the delay
	go func() {
		// Use the client's context to ensure this goroutine respects shutdown signals
		select {
		case <-c.ctx.Done():
			c.logger.Debug("Reconnect goroutine stopping due to context cancellation.")
			return // Client is shutting down
		case <-time.After(delay):
			// Wait for the calculated delay
		}


		c.logger.Info("Attempting to reconnect now...")
		// Call SubscribeOrderbook again. It has logic to handle if already connected.
		err := c.SubscribeOrderbook(c.ctx, c.cfg.Symbol)
		if err != nil {
			c.logger.Error("Reconnect attempt failed", "error", err)
			// The logic inside SubscribeOrderbook will trigger triggerReconnect again on failure
		} else {
			c.logger.Info("Reconnect successful!")
			// Reset reconnect attempt counter happens inside SubscribeOrderbook on success
		}
	}()
}

// countSyncMap is a helper for logging/debugging sync.Map size.
func (c *Client) countSyncMap(m *sync.Map) int {
	count := 0
	m.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// Add this line at the end of the file to ensure it implements the interface
var _ExchangeClient = (*Client)(nil)