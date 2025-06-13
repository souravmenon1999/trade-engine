package cache

import (
	"sync"
	"sync/atomic"

	"github.com/shopspring/decimal"
)

var (
	positions   sync.Map // MarketID -> Position
	realizedPnL int64    // Atomic int64 for realized PnL (scaled by 1e18)
)

// Position represents a market position
type Position struct {
	Quantity          decimal.Decimal
	AverageEntryPrice decimal.Decimal
}

// Init initializes the cache (currently a no-op, but here for future use)
func Init() {
	// No initialization needed yet
}

// GetPosition retrieves a position from the cache
func GetPosition(marketID string) (Position, bool) {
	pos, ok := positions.Load(marketID)
	if !ok {
		return Position{}, false
	}
	return pos.(Position), true
}

// SetPosition stores a position in the cache
func SetPosition(marketID string, pos Position) {
	positions.Store(marketID, pos)
}

// DeletePosition removes a position from the cache
func DeletePosition(marketID string) {
	positions.Delete(marketID)
}

// AddRealizedPnL atomically updates the realized PnL
func AddRealizedPnL(delta decimal.Decimal) {
	atomic.AddInt64(&realizedPnL, delta.Mul(decimal.NewFromInt(1e18)).IntPart())
}

// GetRealizedPnL retrieves the current realized PnL as a decimal
func GetRealizedPnL() decimal.Decimal {
	return decimal.NewFromInt(atomic.LoadInt64(&realizedPnL)).Div(decimal.NewFromInt(1e18))
}