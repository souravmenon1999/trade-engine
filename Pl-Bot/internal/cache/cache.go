package cache

import (
    "sync"
    "sync/atomic"
    "github.com/shopspring/decimal"
)

var (
    positions   sync.Map
    realizedPnL int64 
    unrealizedPnL int64
)

type Position struct {
    Quantity          decimal.Decimal
    AverageEntryPrice decimal.Decimal
}

func Init() {
    realizedPnL = 0
    unrealizedPnL = 0
    positions = sync.Map{}
}

func GetPosition(marketID string) (Position, bool) {
    pos, ok := positions.Load(marketID)
    if !ok {
        return Position{}, false
    }
    return pos.(Position), true
}

func SetPosition(marketID string, pos Position) {
    positions.Store(marketID, pos)
}

func DeletePosition(marketID string) {
    positions.Delete(marketID)
}

func AddRealizedPnL(delta decimal.Decimal) {
    // Scale delta (in 1e6 units) by 1e6 to get 1e12 units
    scaledDelta := delta.Mul(decimal.NewFromInt(1e6)).IntPart()
    atomic.AddInt64(&realizedPnL, scaledDelta)
}

func GetRealizedPnL() decimal.Decimal {
    // Convert back to 1e6 units
    return decimal.NewFromInt(atomic.LoadInt64(&realizedPnL)).Div(decimal.NewFromInt(1e12))
}

func AddUnrealizedPnL(delta decimal.Decimal) {
	// Scale delta (in 1e6 units) by 1e6 to get 1e12 units
	scaledDelta := delta.Mul(decimal.NewFromInt(1e6)).IntPart()
	atomic.StoreInt64(&unrealizedPnL, scaledDelta) // Replace, not add, since recalculated per trade
}

func GetUnrealizedPnL() decimal.Decimal {
	// Convert back to 1e6 units
	return decimal.NewFromInt(atomic.LoadInt64(&unrealizedPnL)).Div(decimal.NewFromInt(1e12))
}