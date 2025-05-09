// internal/strategy/bybitinjective/strategy.go
package bybitinjective

import (
	"context"
	"fmt"
	"trading-system/internal/config"
	"trading-system/internal/exchange/bybit"
	"trading-system/internal/exchange/injective"
	"trading-system/internal/logging"
	"trading-system/internal/processor"
	"trading-system/internal/types"
	"log/slog"
)

// Strategy orchestrates trading based on Bybit price and Injective execution.
type Strategy struct {
	cfg *config.Config // Full configuration for potential cross-component settings
	bybitClient *bybit.Client
	injectiveClient *injective.Client
	priceProcessor *processor.PriceProcessor
	logger *slog.Logger

	// Context for the strategy's operations
	ctx context.Context
	cancel context.CancelFunc

	// Channel to potentially handle results/errors from submitted orders (Fire-and-Forget results)
	// orderResultChan chan OrderSubmissionResult // Define this struct if needed for tracking
}

// OrderSubmissionResult might be used to track outcomes if needed
// type OrderSubmissionResult struct {
// 	ClientOrderID string // Our internal ID for the order
// 	ExchangeOrderID string // ID returned by the exchange
// 	Err error
// }

// NewStrategy creates a new Bybit-Injective strategy.
func NewStrategy(ctx context.Context, cfg *config.Config,
	bybitClient *bybit.Client,
	injectiveClient *injective.Client,
	priceProcessor *processor.PriceProcessor,
) *Strategy {
	strategyCtx, cancel := context.WithCancel(ctx)

	s := &Strategy{
		cfg: cfg,
		bybitClient: bybitClient,
		injectiveClient: injectiveClient,
		priceProcessor: priceProcessor,
		logger: logging.GetLogger().With("component", "bybit_injective_strategy"),
		ctx: strategyCtx,
		cancel: cancel,
		// orderResultChan: make(chan OrderSubmissionResult, 10), // Example channel
	}

	s.logger.Info("Bybit-Injective Strategy initialized.")

	return s
}

// Run starts the strategy's main loop.
func (s *Strategy) Run() {
	s.logger.Info("Bybit-Injective Strategy started.")
	defer s.logger.Info("Bybit-Injective Strategy stopped.")

	// Listen for orderbook update signals from the Bybit client
	obUpdateSignalChan := s.bybitClient.OrderbookUpdates()

	for {
		select {
		case <-s.ctx.Done():
			s.logger.Info("Strategy context cancelled, stopping Run loop.")
			return // Exit the main loop

		case _, ok := <-obUpdateSignalChan:
			if !ok {
				s.logger.Error("Bybit orderbook update channel closed. Strategy cannot continue without price feed.")
				// Handle this critical error - maybe attempt to reconnect Bybit or shut down?
				// For now, we'll exit the loop.
				return
			}

			s.logger.Debug("Received orderbook update signal from Bybit. Processing...")

			// Get the latest orderbook snapshot from the Bybit client
			// The Bybit client's GetOrderbook() returns a snapshot, which is safe to use here.
			obSnapshot := s.bybitClient.GetOrderbook()
			if obSnapshot == nil || obSnapshot.Instrument == nil {
				s.logger.Warn("Received update signal but Bybit orderbook snapshot is nil or invalid. Skipping processing.")
				continue // Skip this update
			}

			// Process the orderbook to generate quotes
			bidOrder, askOrder, shouldQuote, err := s.priceProcessor.ProcessOrderbook(obSnapshot)
			if err != nil {
				s.logger.Error("Price processor failed to generate quotes", "error", err)
				// Decide how to handle processor errors - log, metrics, kill switch?
				continue // Continue processing next OB update
			}

			if shouldQuote {
				s.logger.Info("Processor generated quotes, attempting to submit orders", "bid", bidOrder, "ask", askOrder)

				// --- Submit Orders (Fire-and-Forget with Goroutines) ---
				// Submit the bid order
				go s.submitSingleOrder(s.ctx, bidOrder)

				// Submit the ask order
				go s.submitSingleOrder(s.ctx, askOrder)

				// If you were tracking results:
				// go s.submitSingleOrder(s.ctx, bidOrder, s.orderResultChan)
				// go s.submitSingleOrder(s.ctx, askOrder, s.orderResultChan)
			}

		// Optional: Handle order submission results if you implemented the channel
		// case result := <-s.orderResultChan:
		// 	if result.Err != nil {
		// 		s.logger.Error("Order submission failed", "client_order_id", result.ClientOrderID, "error", result.Err)
		// 		// Implement retry logic, risk management, etc. here
		// 	} else {
		// 		s.logger.Info("Order submitted successfully", "client_order_id", result.ClientOrderID, "exchange_order_id", result.ExchangeOrderID)
		// 		// Track open orders, update positions, etc.
		// 	}
		}
	}
}

// submitSingleOrder is a helper to submit a single order in a goroutine.
// It encapsulates the call to the Injective client and error handling.
func (s *Strategy) submitSingleOrder(ctx context.Context, order *types.Order /*, resultChan chan<- OrderSubmissionResult // Add if tracking results */) {
	// Use a derived context with timeout for the submission if needed, or just pass the strategy context
	// submitCtx, cancel := context.WithTimeout(ctx, 10*time.Second) // Example timeout
	// defer cancel()

	s.logger.Info("Submitting order...", "order", order)

	exchangeOrderID, err := s.injectiveClient.SubmitOrder(ctx, *order) // Pass order by value if SubmitOrder expects value
	// Or pass pointer if SubmitOrder expects pointer: exchangeOrderID, err := s.injectiveClient.SubmitOrder(ctx, order)


	if err != nil {
		s.logger.Error("Order submission failed", "order", order, "error", err)
		// Decide on retry strategy, alerting, etc.
		// If using result channel:
		// select {
		// case resultChan <- OrderSubmissionResult{ClientOrderID: "TODO", Err: err}: // Need a ClientOrderID on types.Order
		// default: s.logger.Error("Failed to send order submission result to channel")
		// }
	} else {
		s.logger.Info("Order submitted successfully", "order", order, "exchange_order_id", exchangeOrderID)
		// Track the order lifecycle using exchangeOrderID
		// If using result channel:
		// select {
		// case resultChan <- OrderSubmissionResult{ClientOrderID: "TODO", ExchangeOrderID: exchangeOrderID}: // Need a ClientOrderID
		// default: s.logger.Error("Failed to send order submission result to channel")
		// }
	}
}


// Close cleans up the Strategy resources.
func (s *Strategy) Close() error {
	s.cancel() // Signal the Run loop to stop
	s.logger.Info("Bybit-Injective Strategy shutting down.")
	// No other resources managed directly by the strategy need closing beyond its context.
	return nil
}