// pkg/quoter/quoter.go
package quoter

import (
	"yourproject/pkg/pricer"
)

type Quote struct {
	Bid float64
	Ask float64
}

type Quoter struct {
	pricer *pricer.Pricer
	spread float64
}

func NewQuoter(pricer *pricer.Pricer, spread float64) *Quoter {
	return &Quoter{pricer: pricer, spread: spread}
}

func (q *Quoter) GetQuote(instrument string) (Quote, error) {
	adjustedPrice, err := q.pricer.GetAdjustedPrice(instrument)
	if err != nil {
		return Quote{}, err
	}

	bid := adjustedPrice * (1 - q.spread)
	ask := adjustedPrice * (1 + q.spread)

	return Quote{Bid: bid, Ask: ask}, nil
}
