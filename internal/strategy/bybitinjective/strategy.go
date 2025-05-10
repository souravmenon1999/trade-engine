// internal/strategy/bybitinjective/strategy.go - Updated Strategy
package bybitinjective

import (
	"context"
	"fmt"
	"github.com/souravmenon1999/trade-engine/internal/config"
	"github.com/souravmenon1999/trade-engine/internal/exchange/bybit"
	"github.com/souravmenon1999/trade-engine/internal/exchange/injective" // Ensure injective is imported
	"github.com/souravmenon1999/trade-engine/internal/logging"
	"github.com/souravmenon1999/trade-engine/internal/processor"
	"github.com/souravmenon1999/trade-engine/internal/types"
	"log/slog"
)

// Strategy orchestrates trading based on Bybit price and Injective execution.
type Strategy struct {
	cfg *config.Config // Full configuration for potential cross-component settings
	bybitClient bybit.ExchangeClient // Use interface type
	injectiveClient types.ExchangeClient // Use interface type
	priceProcessor *processor.PriceProcessor
	logger *slog.Logger

	// Context for the strategy's operations
	ctx context.Context
	cancel context.CancelFunc

	// Channel to potentially handle results/errors from submitted orders (Fire-and-Forget results)
	// orderResultChan chan OrderSubmissionResult // Define this struct if needed for tracking
}

// NewStrategy creates a new Bybit-Injective strategy.
// Accept clients by interface type for better modularity.
func NewStrategy(ctx context.Context, cfg *config.Config,
	bybitClient bybit.ExchangeClient,
	injectiveClient types.ExchangeClient,
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

	// Ensure the bybitClient actually provides orderbook updates
	bybitWithUpdates, ok := s.bybitClient.(interface{ OrderbookUpdates() <-chan struct{} })
	if !ok {
		s.logger.Error("Bybit client does not provide OrderbookUpdates channel. Strategy cannot run.")
		// Handle this fatal configuration error
		return
	}
	obUpdateSignalChan := bybitWithUpdates.OrderbookUpdates()


	for {
		select {
		case <-s.ctx.Done():
			s.logger.Info("Strategy context cancelled, stopping Run loop.")
			return // Exit the main loop

		case _, ok := <-obUpdateSignalChan:
			if !ok {
				s.logger.Error("Bybit orderbook update channel closed. Connection lost. Strategy cannot continue without price feed.")
				// Handle this critical error - potentially rely on Bybit client's reconnect or signal shutdown
				return
			}

			s.logger.Debug("Received orderbook update signal from Bybit. Processing...")

			// Get the latest orderbook snapshot from the Bybit client
			obSnapshot := s.bybitClient.GetOrderbook()
			if obSnapshot == nil || obSnapshot.Instrument == nil {
				s.logger.Warn("Received update signal but Bybit orderbook snapshot is nil or invalid. Skipping processing.")
				continue // Skip this update
			}

			// Process the orderbook to generate quotes
			bidOrder, askOrder, shouldQuote, err := s.priceProcessor.ProcessOrderbook(obSnapshot)
			if err != nil {
				s.logger.Error("Price processor failed to generate quotes", "error", err)
				continue // Continue processing next OB update
			}

			if shouldQuote {
				s.logger.Info("Processor generated quotes, attempting to replace orders.")

				// --- Replace Orders using the Injective Client ---
				ordersToPlace := []*types.Order{}
				if bidOrder != nil {
					ordersToPlace = append(ordersToPlace, bidOrder)
				}
				if askOrder != nil {
					ordersToPlace = append(ordersToPlace, askOrder)
				}

				// Call ReplaceQuotes (Fire-and-Forget)
				// This method is expected to handle the TX building, signing, and queuing internally
				go func() {
					// Use a new context derived from the strategy context for the goroutine
					// This allows the goroutine to be cancelled if the strategy shuts down.
					submitCtx, cancel := context.WithCancel(s.ctx)
					defer cancel()

					// The Injective client's ReplaceQuotes should return queued order IDs or an error
					queuedOrderIDs, submitErr := s.injectiveClient.ReplaceQuotes(submitCtx, obSnapshot.Instrument, ordersToPlace)

					if submitErr != nil {
						s.logger.Error("Failed to queue replace quotes transaction", "error", submitErr, "instrument", obSnapshot.Instrument.Symbol)
						// Handle submission failure (e.g., log, alert, retry strategy - complex!)
					} else {
						s.logger.Info("Replace quotes transaction queued successfully",
							"instrument", obSnapshot.Instrument.Symbol,
							"queued_order_cids", queuedOrderIDs)
						// Note: queuedOrderIDs are Client IDs (Cids) returned by our client.
						// Tracking their status on chain requires more logic.
					}
				}()
			}
		}
	}
}

// Close cleans up the Strategy resources.
func (s *Strategy) Close() error {
	s.cancel() // Signal the Run loop to stop
	s.logger.Info("Bybit-Injective Strategy shutting down.")
	// No other resources managed directly by the strategy need closing beyond its context.
	return nil
}

// No longer needed as ReplaceQuotes handles batch submission
// func (s *Strategy) submitSingleOrder(...) { ... }