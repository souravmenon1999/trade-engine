package injectiveCache

import (
	"sync"
	"sync/atomic"
	"github.com/shopspring/decimal"
	
	derivativeExchangePB "github.com/InjectiveLabs/sdk-go/exchange/derivative_exchange_rpc/pb"
)

var (
	positions     sync.Map
	orders        sync.Map // New sync.Map for orders
	realizedPnL   int64
	unrealizedPnL int64
	feeRebates    int64
	totalGas      decimal.Decimal
	totalUSD      string
)

type Position struct {
	Quantity          decimal.Decimal
	AverageEntryPrice decimal.Decimal // Stored in USDT (1e6 scaled)
}

// Init initializes all cache variables
func Init() {
	positions = sync.Map{}
	orders = sync.Map{}
	atomic.StoreInt64(&realizedPnL, 0)
	atomic.StoreInt64(&unrealizedPnL, 0)
	atomic.StoreInt64(&feeRebates, 0)
	totalGas = decimal.Zero
	totalUSD = "0"
}

// AddOrder adds an order to the orders sync.Map if it's in a non-terminal state
func AddOrder(order *derivativeExchangePB.DerivativeOrderHistory) {
	if order.State == "booked" || order.State == "partial_filled" {
		orders.Store(order.OrderHash, order)
	}
}

// RemoveOrder removes an order from the orders sync.Map
func RemoveOrder(orderHash string) {
	orders.Delete(orderHash)
}

// GetOrder retrieves an order from the orders sync.Map
func GetOrder(orderHash string) (*derivativeExchangePB.DerivativeOrderHistory, bool) {
	order, exists := orders.Load(orderHash)
	if !exists {
		return nil, false
	}
	return order.(*derivativeExchangePB.DerivativeOrderHistory), true
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
	scaledDelta := delta.Mul(decimal.NewFromInt(1e6)).IntPart()
	atomic.AddInt64(&realizedPnL, scaledDelta)
}

func GetRealizedPnL() decimal.Decimal {
	return decimal.NewFromInt(atomic.LoadInt64(&realizedPnL)).Div(decimal.NewFromInt(1e6))
}

func AddUnrealizedPnL(delta decimal.Decimal) {
	scaledDelta := delta.Mul(decimal.NewFromInt(1e6)).IntPart()
	atomic.StoreInt64(&unrealizedPnL, scaledDelta)
}

func GetUnrealizedPnL() decimal.Decimal {
	return decimal.NewFromInt(atomic.LoadInt64(&unrealizedPnL)).Div(decimal.NewFromInt(1e6))
}

func AddFeeRebate(delta decimal.Decimal) {
	scaledDelta := delta.Mul(decimal.NewFromInt(1e6)).IntPart()
	atomic.AddInt64(&feeRebates, scaledDelta)
}

func GetFeeRebates() decimal.Decimal {
	return decimal.NewFromInt(atomic.LoadInt64(&feeRebates)).Div(decimal.NewFromInt(1e6))
}

func AddTotalGas(gas decimal.Decimal) {
    totalGas = totalGas.Add(gas)
}
func GetTotalGas() decimal.Decimal {
    return totalGas
}
func ResetTotalGas() {
    totalGas = decimal.Zero
}

func UpdateTotalUSD(usd string) {
	totalUSD = usd
}


func GetTotalUSD() string {
	return totalUSD
}

