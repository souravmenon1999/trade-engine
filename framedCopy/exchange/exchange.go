package exchange

import (
    "sync"
    "github.com/souravmenon1999/trade-engine/framedCopy/types")


type Exchange interface {
    SendOrder(order *types.Order) (string, error)
    CancelOrder(orderID string) error
    SetTradingHandler(handler TradingHandler)
    SetExecutionHandler(handler ExecutionHandler)
}

type TradingHandler interface {
    OnOrderUpdate(update *types.OrderUpdate)
    OnForceCancel(order *types.Order)
    OnOrderDisconnect()
    OnOrderError(error string)
    OnOrderConnect()
}

type ExecutionHandler interface {
    OnExecutionUpdate(executions []*types.OrderUpdate)
    OnExecutionDisconnect()
    OnExecutionError(error string)
    OnExecutionConnect()
}

type OrderbookHandler interface {
    OnOrderbook(orderbook *types.OrderBook)
    OnOrderbookDisconnect()
    OnOrderbookError(error string)
    OnOrderbookConnect()
}

type AccountHandler interface {
	OnAccountUpdate(update *types.AccountUpdate)
	OnPositionUpdate(position *types.Position)
	OnPositionDisconnect()
	OnPositionError(error string)
	OnPositionConnect()
}
type PriorityFeeHandler interface {
	OnPriorityFee(feeInMicroLamport int64)
}


type OrderStore struct {
    Orders sync.Map // Replaced map[string]*types.Order with sync.Map
}

var GlobalOrderStore = &OrderStore{}

func (s *OrderStore) StoreOrder(order *types.Order) {
    s.Orders.Store(order.ClientOrderID, order) // Using sync.Map Store
}

func (s *OrderStore) GetOrder(clientOrderID string) (*types.Order, bool) {
    order, ok := s.Orders.Load(clientOrderID) // Using sync.Map Load
    if !ok {
        return nil, false
    }
    return order.(*types.Order), true // Type assertion to *types.Order
}