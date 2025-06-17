package bybitCache

import (
	"sync"
	"sync/atomic"

	"github.com/shopspring/decimal"
)

var (
	positions     sync.Map
	orders        sync.Map
	realizedPnL   int64
	unrealizedPnL int64
	feeRebates    int64
	totalFees     int64
	balances      sync.Map
)

type Position struct {
	Quantity          decimal.Decimal
	AverageEntryPrice decimal.Decimal
}

// Init initializes all cache variables
func Init() {
	positions = sync.Map{}
	orders = sync.Map{}
	atomic.StoreInt64(&realizedPnL, 0)
	atomic.StoreInt64(&unrealizedPnL, 0)
	atomic.StoreInt64(&feeRebates, 0)
	atomic.StoreInt64(&totalFees, 0)
	balances = sync.Map{}
}

// AddOrder adds an order to the orders sync.Map
func AddOrder(orderID string, order interface{}) {
	orders.Store(orderID, order)
}

// RemoveOrder removes an order from the orders sync.Map
func RemoveOrder(orderID string) {
	orders.Delete(orderID)
}

// GetOrder retrieves an order from the orders sync.Map
func GetOrder(orderID string) (interface{}, bool) {
	order, exists := orders.Load(orderID)
	return order, exists
}

// GetPosition retrieves a position for a given symbol
func GetPosition(symbol string) (Position, bool) {
	pos, ok := positions.Load(symbol)
	if !ok {
		return Position{}, false
	}
	return pos.(Position), true
}

// SetPosition stores a position for a given symbol
func SetPosition(symbol string, pos Position) {
	positions.Store(symbol, pos)
}

// DeletePosition removes a position for a given symbol
func DeletePosition(symbol string) {
	positions.Delete(symbol)
}

// AddRealizedPnL adds a delta to the realized PnL
func AddRealizedPnL(delta decimal.Decimal) {
	scaledDelta := delta.Mul(decimal.NewFromInt(1e6)).IntPart()
	atomic.AddInt64(&realizedPnL, scaledDelta)
}

// GetRealizedPnL retrieves the realized PnL
func GetRealizedPnL() decimal.Decimal {
	return decimal.NewFromInt(atomic.LoadInt64(&realizedPnL)).Div(decimal.NewFromInt(1e6))
}

// AddUnrealizedPnL sets the unrealized PnL
func AddUnrealizedPnL(delta decimal.Decimal) {
	scaledDelta := delta.Mul(decimal.NewFromInt(1e6)).IntPart()
	atomic.StoreInt64(&unrealizedPnL, scaledDelta)
}

// GetUnrealizedPnL retrieves the unrealized PnL
func GetUnrealizedPnL() decimal.Decimal {
	return decimal.NewFromInt(atomic.LoadInt64(&unrealizedPnL)).Div(decimal.NewFromInt(1e6))
}

// AddFeeRebate adds a fee rebate
func AddFeeRebate(delta decimal.Decimal) {
	scaledDelta := delta.Mul(decimal.NewFromInt(1e6)).IntPart()
	atomic.AddInt64(&feeRebates, scaledDelta)
}

// GetFeeRebates retrieves the fee rebates
func GetFeeRebates() decimal.Decimal {
	return decimal.NewFromInt(atomic.LoadInt64(&feeRebates)).Div(decimal.NewFromInt(1e6))
}

// AddTotalFees adds to the total fees paid
func AddTotalFees(fees decimal.Decimal) {
	scaledFees := fees.Mul(decimal.NewFromInt(1e6)).IntPart()
	atomic.AddInt64(&totalFees, scaledFees)
}

// GetTotalFees retrieves the total fees
func GetTotalFees() decimal.Decimal {
	return decimal.NewFromInt(atomic.LoadInt64(&totalFees)).Div(decimal.NewFromInt(1e6))
}

// UpdateBalance updates the balance for a given denomination
func UpdateBalance(denom string, amount string) {
	balances.Store(denom, amount)
}

// GetBalances retrieves all balances
func GetBalances() map[string]string {
	balancesMap := make(map[string]string)
	balances.Range(func(key, value interface{}) bool {
		balancesMap[key.(string)] = value.(string)
		return true
	})
	return balancesMap
}