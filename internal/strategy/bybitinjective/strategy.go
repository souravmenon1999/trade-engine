// internal/strategy/bybitinjective/strategy.go - Updated Strategy (Corrected Imports)
package bybitinjective

import (
	"context"
	//"fmt" // fmt is not used in this specific code block, but keep if needed elsewhere
	"github.com/souravmenon1999/trade-engine/internal/config"
	"github.com/souravmenon1999/trade-engine/internal/exchange" // Import the exchange package
	// Remove specific client imports if only using interface type
	// "github.com/souravmenon1999/trade-engine/internal/exchange/bybit"
	// "github.com/souravmenon1999/trade-engine/internal/exchange/injective"
	"github.com/souravmenon1999/trade-engine/internal/logging"
	"github.com/souravmenon1999/trade-engine/internal/processor"
	"github.com/souravmenon1999/trade-engine/internal/types"
	"log/slog"
)

// Strategy orchestrates trading based on Bybit price and Injective execution.
type Strategy struct {
	cfg *config.Config // Full configuration for potential cross-component settings
	bybitClient exchange.ExchangeClient // Use exchange.ExchangeClient interface type
	injectiveClient exchange.ExchangeClient // Use exchange.ExchangeClient interface type
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
	bybitClient exchange.ExchangeClient, // Use exchange.ExchangeClient interface type
	injectiveClient exchange.ExchangeClient, // Use exchange.ExchangeClient interface type
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
	// This requires a type assertion because OrderbookUpdates() is specific to bybit.Client,
	// not part of the generic ExchangeClient interface.
	bybitWithUpdates, ok := s.bybitClient.(interface {
		OrderbookUpdates() <-chan struct{}
		GetOrderbook() *types.Orderbook // Also assert this here if only calling GetOrderbook after signal
	})
	if !ok {
		s.logger.Error("Bybit client does not provide required methods (OrderbookUpdates, GetOrderbook). Strategy cannot run.")
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
			// Use the asserted client from bybitWithUpdates
			obSnapshot := bybitWithUpdates.GetOrderbook()
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

                // Ensure there are orders to place before calling ReplaceQuotes, unless ReplaceQuotes is designed to handle 0 orders (like a CancelAll)
                // Our current Injective ReplaceQuotes allows 0 orders, effectively just cancelling.
                // If you only want to cancel when generating *new* quotes, this check is fine.
                // If you want to allow the processor to signal "just cancel", you'd need a different return from the processor.
                // Let's assume for now if shouldQuote is true, we either place 1-2 orders or 0 orders if bid/ask processing failed internally.
                if len(ordersToPlace) == 0 && (bidOrder != nil || askOrder != nil) {
                     // This case should ideally not happen if bidOrder/askOrder were non-nil but resulted in an empty slice
                     s.logger.Error("Processor generated non-nil orders, but ordersToPlace slice is empty. Skipping submission.")
                     continue
                }


				// Call ReplaceQuotes (Fire-and-Forget)
				go func() {
					// Use a new context derived from the strategy context for the goroutine
					submitCtx, cancel := context.WithCancel(s.ctx)
					defer cancel()

					// The Injective client's ReplaceQuotes should return queued order IDs or an error
					// Call ReplaceQuotes on the generic interface
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