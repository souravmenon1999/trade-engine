// internal/exchange/bybit/client.go - Using github.com/gorilla/websocket (Corrected Parsing and Dummy Trading Methods + Fixes)
package bybit

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings" // Import the strings package
	"sync"
	"sync/atomic"
	"time"

	"github.com/souravmenon1999/trade-engine/internal/config"
	"github.com/souravmenon1999/trade-engine/internal/logging"
	"github.com/souravmenon1999/trade-engine/internal/types"
	"github.com/souravmenon1999/trade-engine/internal/exchange" // Import the exchange package for the interface check
	"log/slog"
)

// Remove the duplicate constant definitions here.
// They are defined in ws.go and used via the WSConnection methods.
// const (
// 	writeWait = 10 * time.Second
// 	pongWait = 60 * time.Second
// 	pingPeriod = (pongWait * 9) / 10
// 	maxMessageSize = 8192
// )

// Note: The WSConnection struct and its methods (Connect, Send, Messages, Close)
// are assumed to be defined and working correctly in ws.go using gorilla/websocket.
// This client.go file uses that abstraction.


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
    // This parsing is basic and might need refinement for other symbols/exchanges.
	baseCurrency := ""
	quoteCurrency := ""
	symbol := cfg.Symbol
	// Attempt to find common quote currencies at the end of the symbol
	// This is a weak heuristic; use exchange info endpoints for accuracy
	if strings.HasSuffix(symbol, "USDT") {
		baseCurrency = strings.TrimSuffix(symbol, "USDT")
		quoteCurrency = "USDT"
	} else if strings.HasSuffix(symbol, "USDC") {
		baseCurrency = strings.TrimSuffix(symbol, "USDC")
		quoteCurrency = "USDC"
	} else if strings.HasSuffix(symbol, "BTC") { // Example for BTC quoted pairs
        baseCurrency = strings.TrimSuffix(symbol, "BTC")
        quoteCurrency = "BTC"
    } else if strings.HasSuffix(symbol, "ETH") { // Example for ETH quoted pairs
        baseCurrency = strings.TrimSuffix(symbol, "ETH")
        quoteCurrency = "ETH"
    } else {
        logging.GetLogger().Warn("Could not guess base/quote currency from symbol, using full symbol as base", "symbol", symbol)
        baseCurrency = symbol // Fallback
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
		obUpdateSignalChan: make(chan struct{},10), // Buffered channel to avoid blocking sender
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
	// Check if already connected using the atomic flag
	if c.isConnected.Load() {
		c.reconnectMu.Unlock()
		c.logger.Info("Already connected, skipping subscription attempt")
		return nil // Already subscribed and connected
	}
	c.reconnectMu.Unlock()


	c.logger.Info("Attempting to subscribe to orderbook", "symbol", symbol, "url", c.cfg.WSURL)

	// Create a NEW WSConnection instance on each connect attempt
	// Pass the client's context for cancellation
	wsConn := NewWSConnection(c.ctx, c.cfg.WSURL)
	c.wsConn = wsConn // Store the connection manager

	if err := wsConn.Connect(); err != nil {
		c.logger.Error("Failed initial WebSocket connection", "error", err)
		c.isConnected.Store(false) // Mark as disconnected on failure
        // Wait a moment before triggering reconnect to avoid tight loop on instant failure
        // Use a goroutine so SubscribeOrderbook doesn't block
        go func() {
            // Use the client's context to ensure this goroutine respects shutdown signals
            select {
            case <-c.ctx.Done():
                c.logger.Debug("Initial connection failed, reconnect goroutine stopping due to context cancellation.")
                return // Client is shutting down
            case <-time.After(time.Second): // Wait 1 second
                c.logger.Debug("Initial connection failed, attempting reconnect after delay...")
                c.triggerReconnect() // Attempt reconnect on initial failure
            }
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
	// Use the connection's Send method
	if err := wsConn.Send(subMsg); err != nil {
		c.logger.Error("Failed to send subscription message", "error", err)
		// Sending subscription failed - connection might still be open but useless for this topic
		// Mark as disconnected and trigger reconnect.
		c.isConnected.Store(false) // Mark as disconnected if send fails
		c.triggerReconnect()
		// Consider closing the WS connection managed by wsConn here if the subscription is critical
		// wsConn.Close() // This will trigger the read loop to exit
		return err
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
	defer func() {
		c.logger.Info("Message processing loop stopped")
		// When read loop stops due to error or context cancellation,
		// the connection is likely broken. Signal this via channel closure.
		// Ensure the channel is only closed once if needed.
		// Context cancellation is the primary shutdown mechanism.
	}()

	// Reset snapshot state and expected sequence when starting processing
	c.isSnapshot.Store(true)
    c.expectedSeq.Store(0)

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
					// Depending on error code, might trigger reconnect or shutdown.
					// Example critical codes: 10001 (invalid topic), 10005 (not authenticated)
					if msg.Error != 0 && (msg.Error == 10001 || msg.Error == 10005) {
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
					// Decide how to handle specific errors from the exchange
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
		// Consider triggering reconnect on unmarshalling errors if they are frequent
		return
	}

	// Bybit V5 uses 'u' (Update ID) for sequence number
	receivedSeq := uint64(data.UpdateID)

	c.logger.Info("Received orderbook snapshot", "symbol", data.Symbol, "seq", receivedSeq)

	// --- ADD LOGGING FOR RAW PRICES HERE ---
	// Extracting and logging raw strings for verification during debugging
	rawBestBidPrice := "N/A"
	if len(data.Bids) > 0 && len(data.Bids[0]) > 0 {
		// Unmarshal the json.RawMessage price string into a Go string
		if err := json.Unmarshal(data.Bids[0][0], &rawBestBidPrice); err != nil {
            c.logger.Debug("Failed to unmarshal raw bid price string for logging", "error", err)
            rawBestBidPrice = "Error" // Indicate error
        }
	}

	rawBestAskPrice := "N/A"
	if len(data.Asks) > 0 && len(data.Asks[0]) > 0 {
		// Unmarshal the json.RawMessage price string into a Go string
		if err := json.Unmarshal(data.Asks[0][0], &rawBestAskPrice); err != nil {
            c.logger.Debug("Failed to unmarshal raw ask price string for logging", "error", err)
            rawBestAskPrice = "Error" // Indicate error
        }
	}

    // Use slog's structured logging for readability
	c.logger.Info("Bybit Raw Prices (Snapshot)",
		"bid_price", rawBestBidPrice,
		"ask_price", rawBestAskPrice,
		"symbol", data.Symbol, // Add symbol for context
		"seq", receivedSeq,
	)
    // --- END LOGGING ---


	// Clear the existing orderbook and rebuild from the snapshot
	c.orderbook.Bids = &sync.Map{}
	c.orderbook.Asks = &sync.Map{}

	// Populate bids and asks using the corrected parsing logic
	c.populateOrderbookLevels(c.orderbook.Bids, data.Bids, types.Buy)
	c.populateOrderbookLevels(c.orderbook.Asks, data.Asks, types.Sell)


	// Update sequence number and timestamp
	c.orderbook.UpdateSequenceNumber(receivedSeq)
	c.orderbook.UpdateTimestamp()

	// Snapshot received and processed, now ready for deltas
	c.expectedSeq.Store(receivedSeq + 1) // Expect the next update ID
	c.isSnapshot.Store(false) // No longer waiting for snapshot

	c.logger.Info("Orderbook snapshot processed successfully", "symbol", data.Symbol, "seq", receivedSeq, "bid_count", c.countSyncMap(c.orderbook.Bids), "ask_count", c.countSyncMap(c.orderbook.Asks))

	// Signal that the orderbook is updated (non-blocking send)
	select {
	case c.obUpdateSignalChan <- struct{}{}:
		c.logger.Debug("Orderbook update signal sent after snapshot")
	default:
		// This is expected if the strategy is not keeping up or the channel is small
		c.logger.Debug("Orderbook update signal channel full after snapshot, skipping send")
	}
}

// handleDelta processes an incremental orderbook update.
func (c *Client) handleDelta(msg WSMessage) {
	var data OrderbookData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.logger.Error("Failed to unmarshal delta data", "error", err)
		// Consider triggering reconnect on unmarshalling errors
		return
	}

	// Bybit V5 uses 'u' (Update ID) for sequence number
	receivedSeq := uint64(data.UpdateID)

	// If we are still waiting for a snapshot, ignore deltas unless it's the specific snapshot we need
	// The Bybit V5 docs suggest deltas might arrive before the snapshot, they should be queued.
	// However, our current simple logic just waits for the snapshot.
	// A more advanced client would queue deltas received before the expected snapshot.
	if c.isSnapshot.Load() {
		c.logger.Debug("Ignoring delta, still waiting for snapshot", "received_seq", receivedSeq)
		return
	}

	expected := c.expectedSeq.Load()            // Get the sequence number we currently expect

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

	// c.logger.Debug("Orderbook delta processed", "seq", receivedSeq)

	// Signal that the orderbook has been updated (non-blocking send)
	select {
	case c.obUpdateSignalChan <- struct{}{}:
		// c.logger.Debug("Orderbook update signal sent after delta")
	default:
		// This is expected if the strategy is not keeping up or the channel is small
		c.logger.Debug("Orderbook update signal channel full after delta, skipping send")
	}
}

// populateOrderbookLevels parses and adds/updates levels from a list of [price, quantity] string arrays.
// Used for snapshots. Handles Bybit's string prices/quantities.
func (c *Client) populateOrderbookLevels(levelsMap *sync.Map, entries [][]json.RawMessage, side types.Side) {
	for _, entry := range entries {
		if len(entry) != 2 {
			c.logger.Warn("Unexpected level entry format in snapshot", "entry", string(entry[0])) // Log raw bytes for debug
			continue
		}

		// --- Corrected Parsing Logic: Unmarshal JSON string then parse float ---
		var priceStr, quantityStr string

		// Unmarshal RawMessage into string to handle escaped quotes from Bybit
		if err := json.Unmarshal(entry[0], &priceStr); err != nil {
			c.logger.Error("Failed to unmarshal price json.RawMessage into string in snapshot", "error", err, "raw_bytes", string(entry[0]))
			continue
		}
		if err := json.Unmarshal(entry[1], &quantityStr); err != nil {
			c.logger.Error("Failed to unmarshal quantity json.RawMessage into string in snapshot", "error", err, "raw_bytes", string(entry[1]))
			continue
		}

		// c.logger.Debug("Raw Price Level Received (Snapshot)",
        //     "side", side,
        //     "price_str", priceStr,
        //     "quantity_str", quantityStr,
        // )

		// Parse the string as float64
		priceFloat, err := strconv.ParseFloat(priceStr, 64)
		if err != nil {
			c.logger.Error("Failed to parse price string as float in snapshot", "error", err, "price_str", priceStr)
			continue
		}
		quantityFloat, err := strconv.ParseFloat(quantityStr, 64)
		if err != nil {
			c.logger.Error("Failed to parse quantity string as float in snapshot", "error", err, "quantity_str", quantityStr)
			continue
		}

		// Scale the floats to uint64 (adjust scaling factor 1e6 if needed per exchange/market)
		// Use math.Round for more accurate scaling if values have fractional parts
		scaledPrice := uint64(math.Round(priceFloat * 1e6))
		scaledQuantity := uint64(math.Round(quantityFloat * 1e6))

        if scaledPrice == 0 && priceFloat > 0 {
            c.logger.Warn("Snapshot price scaled to zero, potential precision issue", "price_float", priceFloat, "scaled_price", scaledPrice)
        }
         if scaledQuantity == 0 && quantityFloat > 0 {
            c.logger.Warn("Snapshot quantity scaled to zero, potential precision issue", "quantity_float", quantityFloat, "scaled_quantity", scaledQuantity)
        }


		// Add or update the level in the map
		level, ok := levelsMap.Load(scaledPrice) // Use scaled price as the key
		if !ok {
			// New price level
			level = &types.PriceLevel{Price: scaledPrice}
			levelsMap.Store(scaledPrice, level)
			// c.logger.Debug("Added price level from snapshot", "side", side, "price", priceFloat, "quantity", quantityFloat, "scaled_price", scaledPrice, "scaled_quantity", scaledQuantity)
		}
		// Update quantity using atomic store
		level.(*types.PriceLevel).Quantity.Store(scaledQuantity)
		// Log level updates less verbosely for snapshot population if needed
	}
}


// updateOrderbookLevels applies delta updates from a list of [price, quantity] string arrays.
// Used for deltas. Handles quantity == 0 for removal. Handles Bybit's string prices/quantities.
func (c *Client) updateOrderbookLevels(levelsMap *sync.Map, updates [][]json.RawMessage, side types.Side) {
	for _, entry := range updates {
		if len(entry) != 2 {
			// c.logger.Warn("Unexpected level entry format in delta", "entry", string(entry[0])) // Log raw bytes for debug
			continue
		}

		// --- Corrected Parsing Logic: Unmarshal JSON string then parse float ---
		var priceStr, quantityStr string

		// Unmarshal RawMessage into string to handle escaped quotes from Bybit
		if err := json.Unmarshal(entry[0], &priceStr); err != nil {
			c.logger.Error("Failed to unmarshal price json.RawMessage into string in delta", "error", err, "raw_bytes", string(entry[0]))
			continue
		}
		if err := json.Unmarshal(entry[1], &quantityStr); err != nil {
			c.logger.Error("Failed to unmarshal quantity json.RawMessage into string in delta", "error", err, "raw_bytes", string(entry[1]))
			continue
		}

		// c.logger.Debug("Raw Price Level Received (Delta)",
        //     "side", side,
        //     "price_str", priceStr,
        //     "quantity_str", quantityStr,
        // )


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

		// Scale the floats to uint64 (adjust scaling factor 1e6 if needed per exchange/market)
		scaledPrice := uint64(math.Round(priceFloat * 1e6))
		scaledQuantity := uint64(math.Round(quantityFloat * 1e6))

         if scaledPrice == 0 && priceFloat > 0 {
            c.logger.Warn("Delta price scaled to zero, potential precision issue", "price_float", priceFloat, "scaled_price", scaledPrice)
        }
         if scaledQuantity == 0 && quantityFloat > 0 {
            c.logger.Warn("Delta quantity scaled to zero, potential precision issue", "quantity_float", quantityFloat, "scaled_quantity", scaledQuantity)
        }


		if scaledQuantity == 0 {
			// Quantity 0 means remove the level
			if _, loaded := levelsMap.LoadAndDelete(scaledPrice); loaded {
				// c.logger.Debug("Removed price level from delta", "side", side, "price", priceFloat, "scaled_price", scaledPrice)
			} else {
                 c.logger.Debug("Attempted to remove non-existent price level from delta", "side", side, "price", priceFloat, "scaled_price", scaledPrice)
            }

		} else {
			// Add or update the level
			level, ok := levelsMap.Load(scaledPrice) // Use scaled price as the key
			if !ok {
				// New price level
				level = &types.PriceLevel{Price: scaledPrice}
				levelsMap.Store(scaledPrice, level)
				// c.logger.Debug("Added new price level from delta", "side", side, "price", priceFloat, "quantity", quantityFloat, "scaled_price", scaledPrice, "scaled_quantity", scaledQuantity)
			}
			// Update quantity using atomic store
			level.(*types.PriceLevel).Quantity.Store(scaledQuantity)
			// c.logger.Debug("Updated price level quantity from delta", "side", side, "price", priceFloat, "quantity", quantityFloat, "scaled_price", scaledPrice, "scaled_quantity", scaledQuantity)
		}
	}
}


// --- Dummy Trading Methods to satisfy ExchangeClient interface ---

// SubmitOrder is not implemented for the Bybit data client in this strategy.
func (c *Client) SubmitOrder(ctx context.Context, order types.Order) (string, error) {
	c.logger.Warn("SubmitOrder is not implemented for the Bybit data client in this strategy.")
	return "", fmt.Errorf("SubmitOrder not supported by Bybit data client")
}

// CancelAllOrders is not implemented for the Bybit data client in this strategy.
func (c *Client) CancelAllOrders(ctx context.Context, instrument *types.Instrument) error {
	c.logger.Warn("CancelAllOrders is not implemented for the Bybit data client in this strategy.")
	return fmt.Errorf("CancelAllOrders not supported by Bybit data client")
}

// ReplaceQuotes is not implemented for the Bybit data client in this strategy.
func (c *Client) ReplaceQuotes(ctx context.Context, instrument *types.Instrument, ordersToPlace []*types.Order) ([]string, error) {
	c.logger.Warn("ReplaceQuotes is not implemented for the Bybit data client in this strategy.")
	return nil, fmt.Errorf("ReplaceQuotes not supported by Bybit data client")
}

// --- End Dummy Trading Methods ---


// Close cleans up the Bybit client resources.
func (c *Client) Close() error {
	c.cancel() // Cancel the client's context, which also cancels the WS connection context
	c.logger.Info("Bybit client shutting down")

    // Close the message channel first to signal processor loop to stop
    // Note: Closing a channel can cause panics if receivers are not ready or nil.
    // Use context cancellation as the primary signal.
    // close(c.obUpdateSignalChan) // Optional: close the signal channel


	// Close the WebSocket connection manager
	if c.wsConn != nil {
		return c.wsConn.Close() // wsConn.Close() handles cancelling its own context and closing the gorilla conn
	}
	return nil
}

// triggerReconnect attempts to reconnect after a delay.
func (c *Client) triggerReconnect() {
	c.reconnectMu.Lock()
	defer c.reconnectMu.Unlock()

	// Check if already connected or if a reconnect attempt is already scheduled/running
	// A simple check for isConnected might not be enough if triggerReconnect is called
	// multiple times concurrently. A flag for "reconnect_in_progress" could be added.
	if c.isConnected.Load() {
		return // Already reconnected
	}

	attempt := c.reconnectAttempt.Add(1)
	// Exponential backoff
	baseDelay := time.Second * time.Duration(math.Pow(2, float64(attempt-1)))

    // Add jitter: a random duration up to 1 second
    // Use rand.Float64() for a float between 0.0 and 1.0, then scale it.
    // Ensure the random number generator is seeded if necessary for non-deterministic jitter
    // rand.Seed(time.Now().UnixNano()) // Seed typically once at the start of the program
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

		// Re-check if already connected before attempting to dial, in case a previous
		// concurrent reconnect attempt succeeded while waiting for the delay.
		if c.isConnected.Load() {
			c.logger.Debug("Reconnect goroutine found connection already established.")
			return // Another goroutine reconnected
		}

		c.logger.Info("Attempting to reconnect now...")
		// Call SubscribeOrderbook again. It has logic to handle if already connected
		// or if the connection attempt fails (which will trigger triggerReconnect again).
		err := c.SubscribeOrderbook(c.ctx, c.cfg.Symbol)
		if err != nil {
			c.logger.Error("Reconnect attempt failed", "error", err)
			// SubscribeOrderbook failing will likely trigger triggerReconnect again internally.
		} else {
			c.logger.Info("Reconnect successful!")
			// Reset reconnect attempt counter happens inside SubscribeOrderbook on successful connection
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
// This line must be outside any function or method.
var _ exchange.ExchangeClient = (*Client)(nil) // Use exchange.ExchangeClient