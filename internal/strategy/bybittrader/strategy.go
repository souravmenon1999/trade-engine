// internal/strategy/bybittrader/strategy.go
package bybittrader

import (
	"context"
	"fmt" // Used for logging/error messages
	"sync"
	"sync/atomic" // Keep atomic, although mutex is used for string IDs
	"time" // Import time for sleep/timeouts
	"trading-system/internal/config"
	"trading-system/internal/exchange/bybit" // Import concrete Bybit clients
	"trading-system/internal/exchange" // Use the interface type if interacting generically (not used for trading in this specific package)
	"trading-system/internal/logging"
	"trading-system/internal/processor"
	"trading-system/internal/types"
	"log/slog"
)

// Strategy orchestrates trading on Bybit based on Bybit's own price feed.
// It maintains one buy and one sell limit order, amending them as price changes
// signaled by the price processor. It uses the concrete Bybit trading client
// for specific Bybit API calls like Amend and Cancel.
type Strategy struct {
	cfg *config.Config // Full configuration

	// Use concrete Bybit clients as this package is specific to Bybit.
	// bybitDataClient provides market data (e.g., order book).
	bybitDataClient *bybit.Client
	// bybitTradingClient provides trading functionalities (submit, amend, cancel).
	bybitTradingClient *bybit.TradingClient

	priceProcessor *processor.PriceProcessor
	logger *slog.Logger

	ctx context.Context
	cancel context.CancelFunc

	// State to track current open orders placed by THIS strategy instance.
	// Uses a mutex to protect access to the string IDs.
	orderStateMu sync.Mutex
	currentBuyOrderID string // Exchange Order ID of the current buy quote
	currentSellOrderID string // Exchange Order ID of the current sell quote

	// Optional: Track the last quoted prices if needed for specific amendment logic
	// beyond the processor signal. Not strictly used in the current logic.
	// lastQuotedBidPrice atomic.Uint64
	// lastQuotedAskPrice atomic.Uint64

    // Note: This simple strategy doesn't explicitly track in-flight operations
    // per order ID (e.g., a map[string]bool). The concurrency protection relies
    // on checking the tracked ID (*orderIDPtr) against the ID being operated on
    // within the goroutines before updating/clearing. For a strategy managing
    // many orders or complex state, tracking in-flight operations might be needed.
}

// NewStrategy creates a new Bybit trader strategy.
// Expects both the concrete Bybit data client and trading client implementations.
func NewStrategy(ctx context.Context, cfg *config.Config,
	bybitDataClient *bybit.Client, // Concrete Bybit data client
    bybitTradingClient *bybit.TradingClient, // Concrete Bybit trading client
	priceProcessor *processor.PriceProcessor,
) *Strategy {
	strategyCtx, cancel := context.WithCancel(ctx)

	s := &Strategy{
		cfg: cfg,
		bybitDataClient: bybitDataClient, // Store the data client
        bybitTradingClient: bybitTradingClient, // Store the trading client
		priceProcessor: priceProcessor,
		logger: logging.GetLogger().With("component", "bybit_trader_strategy"),
		ctx: strategyCtx,
		cancel: cancel,
	}

	s.logger.Info("Bybit Trader Strategy initialized.")

	// TODO: On startup, consider querying existing open orders belonging to this
	// strategy's tag/client ID and update currentBuyOrderID/currentSellOrderID.
	// For this simple version, we assume a clean slate on startup.

	return s
}

// Run starts the strategy's main loop. It listens for orderbook updates
// and triggers quoting/order management based on processor signals.
func (s *Strategy) Run() {
	s.logger.Info("Bybit Trader Strategy started.")
	defer s.logger.Info("Bybit Trader Strategy stopped.")

	// Get the orderbook update channel from the Bybit data client.
	obUpdateSignalChan := s.bybitDataClient.OrderbookUpdates()

	for {
		select {
		case <-s.ctx.Done():
			s.logger.Info("Strategy context cancelled, stopping Run loop.")
			return // Exit the main loop

		case _, ok := <-obUpdateSignalChan:
			if !ok {
				s.logger.Error("Bybit orderbook update channel closed. Connection lost. Strategy cannot continue without price feed.")
				// Handle this critical error - potentially rely on Bybit client's reconnect or signal strategy shutdown upstream
				return
			}

			s.logger.Debug("Received orderbook update signal from Bybit. Processing...")

			// Get the latest orderbook snapshot from the Bybit data client.
			// This snapshot is safe to access from the main goroutine.
			obSnapshot := s.bybitDataClient.GetOrderbook()
			if obSnapshot == nil || obSnapshot.Instrument == nil {
				s.logger.Warn("Received update signal but Bybit orderbook snapshot is nil or invalid. Skipping processing.")
				continue // Skip this update and wait for the next signal
			}

			// Process the orderbook to generate target quotes.
            // Note: The PriceProcessor might be generic and generate orders with a
            // generic types.Exchange field (e.g., types.ExchangeInjective).
            // The bybit.TradingClient methods used below (SubmitOrder, AmendOrder, etc.)
            // must be implemented to correctly interact with the Bybit API and should
            // typically ignore or correctly translate such generic fields if necessary,
            // relying on the instrument details passed. In this code, AmendOrder and
            // cancelSingleOrder explicitly take instrument/symbol details, which is good.
			bidOrderTarget, askOrderTarget, shouldQuote, err := s.priceProcessor.ProcessOrderbook(obSnapshot)

			if err != nil {
				s.logger.Error("Price processor failed to generate quotes", "error", err)
				continue // Continue listening for the next update
			}

			if shouldQuote {
				s.logger.Debug("Processor signaled to quote, attempting to update orders.")

				// Get current tracked order IDs safely using the mutex.
				s.orderStateMu.Lock()
				buyOrderID := s.currentBuyOrderID
				sellOrderID := s.currentSellOrderID
				s.orderStateMu.Unlock()

				// Handle Buy Order: Check if a new quote exists and if we need to Submit or Amend.
				if bidOrderTarget != nil {
					// We have a target buy quote from the processor.
					if buyOrderID == "" {
						// No existing buy order tracked, submit a new one.
						s.logger.Info("No existing buy order tracked, submitting new buy quote", "order", bidOrderTarget)
						// Use a goroutine for fire-and-forget submission to avoid blocking the main loop.
						// Pass the concrete trading client and a pointer to update the tracked ID.
						go s.submitAndTrackOrder(s.ctx, s.bybitTradingClient, bidOrderTarget, &s.currentBuyOrderID)
					} else {
						// Existing buy order tracked, attempt to amend it to the new target.
						s.logger.Info("Attempting to amend existing buy order",
                            "order_id", buyOrderID,
                            "new_price", bidOrderTarget.Price,
                            "new_quantity", bidOrderTarget.Quantity,
                         )
                        // Use a goroutine for fire-and-forget amendment.
                        // Pass the concrete trading client and details needed for amendment.
						go s.amendAndTrackOrder(
                            s.ctx,
                            s.bybitTradingClient,
                            buyOrderID,
                            obSnapshot.Instrument, // Pass instrument details for API call
                            bidOrderTarget.Price,
                            bidOrderTarget.Quantity,
                            &s.currentBuyOrderID, // Pass pointer to update ID if needed
                         )
					}
                    // Optional: Update tracked last quoted price if used elsewhere.
                    // s.lastQuotedBidPrice.Store(bidOrderTarget.Price)
				} else {
                    // Processor did not generate a buy quote (e.g., insufficient liquidity, spread too tight).
                    // If there is an existing buy order tracked, cancel it.
                     if buyOrderID != "" {
                         s.logger.Info("Processor did not generate buy quote, attempting to cancel existing buy order", "order_id", buyOrderID)
                         // Use a goroutine for fire-and-forget cancellation.
                         // Pass the concrete trading client and order details.
                         go s.cancelAndClearOrder(s.ctx, s.bybitTradingClient, buyOrderID, obSnapshot.Instrument, &s.currentBuyOrderID)
                     } else {
                          s.logger.Debug("Processor did not generate buy quote, and no existing buy order tracked. Nothing to do.")
                     }
                }

				// Handle Sell Order: Check if a new quote exists and if we need to Submit or Amend.
				if askOrderTarget != nil {
					// We have a target sell quote from the processor.
					if sellOrderID == "" {
						// No existing sell order tracked, submit a new one.
						s.logger.Info("No existing sell order tracked, submitting new sell quote", "order", askOrderTarget)
						// Use a goroutine for fire-and-forget submission.
						go s.submitAndTrackOrder(s.ctx, s.bybitTradingClient, askOrderTarget, &s.currentSellOrderID)
					} else {
						// Existing sell order tracked, attempt to amend it to the new target.
						s.logger.Info("Attempting to amend existing sell order",
                            "order_id", sellOrderID,
                            "new_price", askOrderTarget.Price,
                            "new_quantity", askOrderTarget.Quantity,
                         )
                        // Use a goroutine for fire-and-forget amendment.
						go s.amendAndTrackOrder(
                            s.ctx,
                            s.bybitTradingClient,
                            sellOrderID,
                            obSnapshot.Instrument, // Pass instrument details for API call
                            askOrderTarget.Price,
                            askOrderTarget.Quantity,
                            &s.currentSellOrderID, // Pass pointer to update ID if needed
                         )
					}
                     // Optional: Update tracked last quoted price.
                     // s.lastQuotedAskPrice.Store(askOrderTarget.Price)
				} else {
                    // Processor did not generate a sell quote.
                    // If there is an existing sell order tracked, cancel it.
                    if sellOrderID != "" {
                         s.logger.Info("Processor did not generate sell quote, attempting to cancel existing sell order", "order_id", sellOrderID)
                         // Use a goroutine for fire-and-forget cancellation.
                         go s.cancelAndClearOrder(s.ctx, s.bybitTradingClient, sellOrderID, obSnapshot.Instrument, &s.currentSellOrderID)
                     } else {
                         s.logger.Debug("Processor did not generate sell quote, and no existing sell order tracked. Nothing to do.")
                     }
                }
			} else {
                // Processor said NOT to quote (e.g., cooldown active, price threshold not met).
                // We do nothing with existing orders.
                s.logger.Debug("Processor did not signal to quote. Maintaining existing orders (if any).")
            }
		}
	}
}

// submitAndTrackOrder submits a new order using the trading client and updates
// the tracked order ID upon successful API response. Runs in a goroutine.
// orderIDPtr is a pointer to either s.currentBuyOrderID or s.currentSellOrderID.
func (s *Strategy) submitAndTrackOrder(ctx context.Context, tradingClient *bybit.TradingClient, order *types.Order, orderIDPtr *string) {
	if tradingClient == nil || order == nil || orderIDPtr == nil {
		s.logger.Error("submitAndTrackOrder called with nil client, order, or ID pointer")
		return
	}

	// Use a derived context with timeout for the API call.
	submitCtx, cancel := context.WithTimeout(ctx, 15*time.Second) // Example timeout
	defer cancel()

	s.logger.Info("Submitting order...", "order_side", order.Side, "order_price", order.Price, "order_qty", order.Quantity, "order_symbol", order.Instrument.Symbol)

    // Call the TradingClient's SubmitOrder method.
	exchangeOrderID, err := tradingClient.SubmitOrder(submitCtx, *order) // Pass order by value

	if err != nil {
		s.logger.Error("Order submission failed", "order_side", order.Side, "error", err)
		// If submission fails, the tracked ID should remain empty or be cleared
		// so the next processing cycle attempts to submit again.
        // The *orderIDPtr should ideally already be empty if we were attempting to submit.
        // Defensive check: ensure it's empty.
        s.orderStateMu.Lock()
        *orderIDPtr = "" // Ensure the tracked ID is cleared on submission failure
        s.orderStateMu.Unlock()
        s.logger.Debug("Cleared tracked order ID after submission failure.")

	} else {
		s.logger.Info("Order submitted successfully", "order_side", order.Side, "exchange_order_id", exchangeOrderID)
		// Update the tracked order ID safely using the mutex.
		s.orderStateMu.Lock()
		*orderIDPtr = exchangeOrderID // Update the string pointed to
		s.orderStateMu.Unlock()
		s.logger.Debug("Tracked order ID updated after submission", "side", order.Side, "new_id", exchangeOrderID)

		// TODO: Consider starting a goroutine here to monitor this order's status
		// (e.g., query by ID, listen to user data stream) for more robust state management.
		// For this simple strategy, relying on subsequent processor signals is sufficient
		// to trigger amendments or cancellations.
	}
}

// amendAndTrackOrder attempts to amend an existing order using the trading client.
// It updates the tracked order ID if the amendment is successful and the API
// returns a potentially new ID (though Bybit typically keeps the same ID).
// Runs in a goroutine. orderIDPtr points to the tracked order ID variable.
func (s *Strategy) amendAndTrackOrder(ctx context.Context, tradingClient *bybit.TradingClient, currentOrderID string, instrument *types.Instrument, newPrice uint64, newQuantity uint64, orderIDPtr *string) {
    if tradingClient == nil || orderIDPtr == nil {
        s.logger.Error("amendAndTrackOrder called with nil client or ID pointer")
        return
    }
     if currentOrderID == "" {
         s.logger.Error("amendAndTrackOrder called with empty currentOrderID. Cannot amend.", "symbol", instrument.Symbol)
         // If the ID is unexpectedly empty here, clear the tracked ID defensively.
          s.orderStateMu.Lock()
         *orderIDPtr = ""
          s.orderStateMu.Unlock()
         return
     }
     if instrument == nil {
         s.logger.Error("amendAndTrackOrder called with nil instrument. Cannot amend.", "order_id", currentOrderID)
         // Cannot amend without instrument/symbol info.
         // Clear the tracked ID defensively so a new order is attempted next cycle.
          s.orderStateMu.Lock()
         *orderIDPtr = ""
          s.orderStateMu.Unlock()
         return
     }

	// Use a derived context with timeout for the API call.
	amendCtx, cancel := context.WithTimeout(ctx, 15*time.Second) // Timeout for amendment
	defer cancel()

	s.logger.Info("Attempting to amend order...", "order_id", currentOrderID, "symbol", instrument.Symbol, "new_price", newPrice, "new_quantity", newQuantity)

    // Call the TradingClient's AmendOrder method.
	amendedOrderID, err := tradingClient.AmendOrder(amendCtx, currentOrderID, instrument, newPrice, newQuantity)

	if err != nil {
		s.logger.Error("Order amendment failed", "order_id", currentOrderID, "error", err)
		// Handle amendment failure: log and clear the tracked order ID.
		// Clearing the ID ensures the next processing cycle will attempt to submit a new order.
		s.orderStateMu.Lock()
		// Only clear if the currently tracked ID is still the one we *attempted* to amend.
		// This prevents clearing an ID that was already updated by another goroutine
		// (e.g., if a submit succeeded after a previous amend failed on the old ID).
		if *orderIDPtr == currentOrderID {
			*orderIDPtr = "" // Clear the tracked ID on failure
			s.orderStateMu.Unlock()
			s.logger.Warn("Cleared tracked order ID after amendment failure", "old_id", currentOrderID)
		} else {
             s.orderStateMu.Unlock() // Unlock in this branch too
             s.logger.Debug("Amendment failed, but tracked ID was already updated by another operation", "failed_id", currentOrderID, "current_tracked_id", *orderIDPtr)
        }

	} else {
		s.logger.Info("Order amended successfully", "old_id", currentOrderID, "amended_id", amendedOrderID)
		// For Bybit Amend, the Order ID typically remains the same.
		// If the API *does* return a different ID, update the tracked ID.
		if amendedOrderID != "" && amendedOrderID != currentOrderID {
             s.logger.Warn("Bybit Amend returned a different Order ID than requested", "requested_id", currentOrderID, "response_id", amendedOrderID)
             // Update tracked ID to the new one returned by the API.
             s.orderStateMu.Lock()
             // Check again in case it was updated by another goroutine concurrently.
             if *orderIDPtr == currentOrderID {
                *orderIDPtr = amendedOrderID // Update to the new ID returned by API
                s.orderStateMu.Unlock()
                s.logger.Info("Updated tracked order ID after amendment returned new ID", "old_id", currentOrderID, "new_id", amendedOrderID)
             } else {
                  s.orderStateMu.Unlock() // Unlock in this branch
                  s.logger.Debug("Amendment returned new ID, but tracked ID was already updated by another goroutine", "returned_id", amendedOrderID, "current_tracked_id", *orderIDPtr)
             }
         } else {
             // Amendment successful and ID is the same or empty (if API returns empty on success, though unlikely).
             // No need to update the tracked ID in this case as it's already correct.
             s.logger.Debug("Tracked order ID remains the same after successful amendment", "order_id", currentOrderID)
         }
	}
}

// cancelAndClearOrder attempts to cancel a specific order using the trading client
// and clears the tracked order ID upon successful API response. Runs in a goroutine.
// orderIDPtr points to the tracked order ID variable.
func (s *Strategy) cancelAndClearOrder(ctx context.Context, tradingClient *bybit.TradingClient, orderID string, instrument *types.Instrument, orderIDPtr *string) {
     if tradingClient == nil || orderIDPtr == nil {
        s.logger.Error("cancelAndClearOrder called with nil client or ID pointer")
        return
    }
    if orderID == "" {
        s.logger.Debug("cancelAndClearOrder called with empty orderID, nothing to cancel")
        // Ensure the tracked ID is also clear if it was unexpectedly empty.
         s.orderStateMu.Lock()
         *orderIDPtr = ""
         s.orderStateMu.Unlock()
         return
    }
     if instrument == nil {
         s.logger.Error("cancelAndClearOrder called with nil instrument. Cannot cancel.", "order_id", orderID)
         // Cannot cancel without instrument/symbol info.
         // Clear the tracked ID defensively.
         s.orderStateMu.Lock()
         *orderIDPtr = ""
         s.orderStateMu.Unlock()
         return
     }

	// Use a derived context with timeout for the API call.
	cancelCtx, cancel := context.WithTimeout(ctx, 10*time.Second) // Timeout for cancellation
	defer cancel()

	s.logger.Info("Attempting to cancel order...", "order_id", orderID, "symbol", instrument.Symbol)

    // Call the TradingClient's cancelSingleOrder method.
    // Note: This method name suggests it's concrete to Bybit.
	err := tradingClient.CancelSingleOrder(cancelCtx, orderID, instrument.Symbol)

	if err != nil {
		s.logger.Error("Order cancellation failed", "order_id", orderID, "error", err)
		// Handle cancellation failure: log and clear the tracked order ID.
		// Clearing the ID assumes failure means the order is no longer manageable by this ID,
		// prompting the strategy to potentially place a new order next cycle.
        s.orderStateMu.Lock()
		// Only clear if the currently tracked ID is still the one we *attempted* to cancel.
		if *orderIDPtr == orderID {
        	*orderIDPtr = "" // Clear the tracked ID on failure
        	s.orderStateMu.Unlock()
            s.logger.Warn("Cleared tracked order ID after cancellation failure", "old_id", orderID)
        } else {
             s.orderStateMu.Unlock() // Unlock in this branch
             s.logger.Debug("Cancellation failed, but tracked ID was already updated by another operation", "failed_id", orderID, "current_tracked_id", *orderIDPtr)
        }

	} else {
		s.logger.Info("Order cancelled successfully", "order_id", orderID)
		// Clear the tracked order ID upon successful cancellation.
		s.orderStateMu.Lock()
		// Only clear if the currently tracked ID is still the one we *successfully* cancelled.
		// This handles the case where a new order might have been placed (and ID updated)
		// by the main loop processing a new signal before the cancel goroutine finished.
		if *orderIDPtr == orderID {
        	*orderIDPtr = "" // Clear the tracked ID on success
        	s.orderStateMu.Unlock()
            s.logger.Debug("Cleared tracked order ID after successful cancellation", "old_id", orderID)
        } else {
             s.orderStateMu.Unlock() // Unlock in this branch
             // This can happen if a new order was placed (and ID updated) before the cancel goroutine finished.
             s.logger.Debug("Order cancelled successfully, but tracked ID was already updated by another operation", "cancelled_id", orderID)
        }
	}
}

// Close cleans up the Strategy resources, including signalling the Run loop to stop
// and attempting to cancel any open orders tracked by the strategy.
func (s *Strategy) Close() error {
	s.logger.Info("Bybit Trader Strategy shutting down. Signalling Run loop to stop.")
	s.cancel() // Signal the Run loop context to cancel

    // Attempt to cancel any remaining open orders tracked by the strategy.
    // This is a best-effort, fire-and-forget attempt during shutdown.
    s.orderStateMu.Lock()
    buyOrderID := s.currentBuyOrderID
    sellOrderID := s.currentSellOrderID
    s.orderStateMu.Unlock() // Unlock before potentially calling API methods

    // Get instrument info from the data client's latest orderbook snapshot.
    // We need the instrument symbol for cancellation API calls.
    obSnapshot := s.bybitDataClient.GetOrderbook()
    if obSnapshot == nil || obSnapshot.Instrument == nil {
         s.logger.Warn("Instrument not available from orderbook snapshot during shutdown cleanup. Cannot cancel orders.")
         // Cannot cancel without symbol, just log and return.
         return nil
    }
    symbol := obSnapshot.Instrument.Symbol

    // Use a separate context for shutdown cancellation requests with a short timeout.
    cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // Short timeout for shutdown cancels
    defer cancel()

    var wg sync.WaitGroup // Use a WaitGroup to wait for shutdown cancels to be attempted

    if buyOrderID != "" {
        wg.Add(1)
        go func(orderID string) {
            defer wg.Done()
            s.logger.Info("Attempting to cancel remaining buy order on shutdown", "order_id", orderID)
            // Call the TradingClient's CancelSingleOrder method.
            err := s.bybitTradingClient.CancelSingleOrder(cancelCtx, orderID, symbol)
            if err != nil {
                 s.logger.Error("Failed to cancel remaining buy order on shutdown", "order_id", orderID, "error", err)
            } else {
                 s.logger.Info("Remaining buy order cancelled on shutdown", "order_id", orderID)
            }
        }(buyOrderID) // Pass orderID by value to the goroutine
    }

    if sellOrderID != "" {
        wg.Add(1)
        go func(orderID string) {
            defer wg.Done()
            s.logger.Info("Attempting to cancel remaining sell order on shutdown", "order_id", orderID)
            // Call the TradingClient's CancelSingleOrder method.
             err := s.bybitTradingClient.CancelSingleOrder(cancelCtx, orderID, symbol)
             if err != nil {
                  s.logger.Error("Failed to cancel remaining sell order on shutdown", "order_id", orderID, "error", err)
             } else {
                  s.logger.Info("Remaining sell order cancelled on shutdown", "order_id", orderID)
             }
        }(sellOrderID) // Pass orderID by value to the goroutine
    }

    // Wait briefly for shutdown cancellation goroutines to finish.
    // Use a separate goroutine for the wait to not block the main close path
    // if wg.Wait() takes longer than the Close caller expects.
    // Or, just call wg.Wait() and accept it might block briefly. Let's call it directly for simplicity here.
    wg.Wait()

	s.logger.Info("Shutdown cancellation attempts finished.")

	// TODO: Consider closing client connections here if they are owned by the strategy.
	// In this architecture, clients might be shared, so typically they are closed upstream.

	return nil
}