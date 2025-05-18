// internal/processor/processor.go
package processor

import (
	"context"
	"fmt"
	"math"
	"sync/atomic"
	"time"
	"github.com/souravmenon1999/trade-engine/internal/config"
	"github.com/souravmenon1999/trade-engine/internal/logging"
	"github.com/souravmenon1999/trade-engine/internal/types"
	"log/slog" // Ensure slog is imported
)

// PriceProcessor determines quoting prices based on orderbook data and configuration.
type PriceProcessor struct {
	cfg      *config.ProcessorConfig // Processor-specific config (requote cooldown/threshold)
	orderCfg *config.OrderConfig     // Default order config (quantity, spread)
	logger   *slog.Logger

	lastQuoteTime atomic.Int64  // Unix Nano timestamp of the last time quotes were generated
	lastMidPrice  atomic.Uint64 // Scaled mid-price at the time of the last quote generation

	// Context for the processor's operations
	ctx    context.Context
	cancel context.CancelFunc
}

// NewPriceProcessor creates and initializes a new PriceProcessor.
func NewPriceProcessor(ctx context.Context, cfg *config.ProcessorConfig, orderCfg *config.OrderConfig) *PriceProcessor {
	// ... (NewPriceProcessor function remains the same) ...
    processorCtx, cancel := context.WithCancel(ctx)

	p := &PriceProcessor{
		cfg:    cfg,
		orderCfg: orderCfg,
		logger: logging.GetLogger().With("component", "price_processor"),
		ctx:    processorCtx,
		cancel: cancel,
	}

	p.logger.Info("Price Processor initialized", "spread_pct", orderCfg.SpreadPct, "requote_cooldown_sec", cfg.RequoteCooldown, "requote_threshold_pct", cfg.RequoteThreshold)

	return p
}

// ProcessOrderbook takes an orderbook snapshot, determines if requoting is needed,
func (p *PriceProcessor) ProcessOrderbook(ob *types.Orderbook) (*types.Order, *types.Order, bool, error) {

	


	if ob == nil || ob.Instrument == nil {
		return nil, nil, false, fmt.Errorf("invalid orderbook provided")
	}

	// Calculate mid-price
	
	midFloat, midErr := ob.MidPrice()
	if midErr != nil {
		return nil, nil, false, types.TradingError{
			Code: types.ErrInsufficientLiquidity,
			Message: "Cannot calculate mid-price due to insufficient liquidity",
			Wrapped: midErr,
		}
	}

	midScaledUint64 := uint64(midFloat * 1e6)

	 p.logger.Debug("Calculated mid-price", "symbol", ob.Instrument.Symbol, "mid_price", midFloat)

   // Check if quoting is allowed based on cooldown
	now := time.Now().UnixNano()
	lastQuote := p.lastQuoteTime.Load()
	cooldownSeconds := time.Duration(p.cfg.RequoteCooldown) * time.Second

	if lastQuote != 0 {
		elapsed := time.Duration(now - lastQuote)
		if elapsed < cooldownSeconds {
			// p.logger.Debug("Requote cooldown active", "elapsed", elapsed, "cooldown", cooldownSeconds)
			return nil, nil, false, nil // Cannot quote yet
		}
	}

	// Check if requoting is needed based on price threshold (if threshold > 0)
	requoteThresholdPct := p.cfg.RequoteThreshold
	lastMid := p.lastMidPrice.Load()

	if requoteThresholdPct > 0 && lastMid != 0 {
		priceChangePct := math.Abs(float64(midScaledUint64)-float64(lastMid)) / float64(lastMid) * 100.0

		if priceChangePct < requoteThresholdPct {
			p.logger.Debug("Price change below threshold", "change_pct", priceChangePct, "threshold_pct", requoteThresholdPct)
			return nil, nil, false, nil // Price hasn't moved enough
		}
		p.logger.Debug("Price change above threshold, requoting allowed", "change_pct", priceChangePct, "threshold_pct", requoteThresholdPct)
	} else if requoteThresholdPct == 0 && lastQuote != 0 {
        p.logger.Debug("Requote threshold is 0, requoting allowed by cooldown")
    } else {
        p.logger.Debug("First quote or requote cooldown over, quoting allowed")
    }


	// If we passed cooldown and threshold checks, calculate target prices
	spreadDecimal := p.orderCfg.SpreadPct / 100.0

	targetBidFloat := midFloat * (1 - spreadDecimal/2)
	targetAskFloat := midFloat * (1 + spreadDecimal/2)

	scaledBidPrice := uint64(targetBidFloat * 1e6)
	scaledAskPrice := uint64(targetAskFloat * 1e6)

	if scaledBidPrice == 0 || scaledAskPrice == 0 || scaledBidPrice >= scaledAskPrice {
         p.logger.Error("Calculated invalid bid/ask prices",
             "mid_float", midFloat, "spread_pct", p.orderCfg.SpreadPct,
             "scaled_bid", scaledBidPrice, "scaled_ask", scaledAskPrice)
         return nil, nil, false, fmt.Errorf("calculated invalid bid/ask prices")
    }


	// Construct the Order objects
	// Quantity needs to be scaled from float64 config to uint64
	scaledQuantity := uint64(p.orderCfg.Quantity * 1e6)
    if scaledQuantity == 0 {
        p.logger.Error("Configured order quantity scaled to zero", "quantity_float", p.orderCfg.Quantity)
        return nil, nil, false, fmt.Errorf("configured order quantity is zero after scaling")
    }


	bidOrder := &types.Order{
		Instrument: ob.Instrument,
		Side:       types.Buy,
		Quantity:   scaledQuantity,
		Price:      scaledBidPrice,
		Exchange:   types.ExchangeInjective, // Target exchange is Injective
	}
	

	askOrder := &types.Order{
		Instrument: ob.Instrument,
		Side:       types.Sell,
		Quantity:   scaledQuantity,
		Price:      scaledAskPrice,
		Exchange:   types.ExchangeInjective, // Target exchange is Injective
	}

	p.logger.Debug("Generated target quotes", "bid_order", bidOrder, "ask_order", askOrder)

	// Update last quote timestamp and mid-price *only if* we are generating quotes
	p.lastQuoteTime.Store(now)
	p.lastMidPrice.Store(midScaledUint64)

	return bidOrder, askOrder, true, nil // Signal to quote
}

// Close cleans up the PriceProcessor resources.
func (p *PriceProcessor) Close() error {
	p.cancel() // Signal context cancellation
	p.logger.Info("Price Processor shutting down.")
	return nil
}