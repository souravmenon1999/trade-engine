// pkg/pricer/pricer.go
package pricer

import (
	"fmt"
	"time"
	"yourproject/pkg/types"
)

type Pricer struct {
	exchanges map[types.ExchangeID]types.Exchange
}

func NewPricer(exchanges map[types.ExchangeID]types.Exchange) *Pricer {
	return &Pricer{exchanges: exchanges}
}

func (p *Pricer) GetAdjustedPrice(instrument string) (float64, error) {
	bybitEx, ok := p.exchanges[types.ExchangeIDBybit]
	if !ok {
		return 0, fmt.Errorf("Bybit exchange not found")
	}
	injEx, ok := p.exchanges[types.ExchangeIDInjective]
	if !ok {
		return 0, fmt.Errorf("Injective exchange not found")
	}

	pBybit, err := bybitEx.GetCurrentPrice(instrument)
	if err != nil {
		return 0, fmt.Errorf("failed to get Bybit price: %w", err)
	}

	fInj, err := injEx.GetFundingRate(instrument)
	if err != nil {
		return 0, fmt.Errorf("failed to get Injective funding rate: %w", err)
	}
	tInj, err := injEx.GetTimeElapsedSinceLastFunding(instrument)
	if err != nil {
		return 0, fmt.Errorf("failed to get Injective time elapsed: %w", err)
	}
	TInj, err := injEx.GetFundingInterval(instrument)
	if err != nil {
		return 0, fmt.Errorf("failed to get Injective funding interval: %w", err)
	}

	fBybit, err := bybitEx.GetFundingRate(instrument)
	if err != nil {
		return 0, fmt.Errorf("failed to get Bybit funding rate: %w", err)
	}
	tBybit, err := bybitEx.GetTimeElapsedSinceLastFunding(instrument)
	if err != nil {
		return 0, fmt.Errorf("failed to get Bybit time elapsed: %w", err)
	}
	TBybit, err := bybitEx.GetFundingInterval(instrument)
	if err != nil {
		return 0, fmt.Errorf("failed to get Bybit funding interval: %w", err)
	}

	injFundingComponent := fInj * (float64(tInj) / float64(TInj))
	bybitFundingComponent := fBybit * (float64(tBybit) / float64(TBybit))
	adjustment := injFundingComponent - bybitFundingComponent

	adjustedPrice := pBybit * (1 + adjustment)
	return adjustedPrice, nil
}