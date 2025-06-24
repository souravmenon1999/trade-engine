package exchange

import (
    "sync"
    "github.com/souravmenon1999/trade-engine/framedCopy/types"
    "github.com/souravmenon1999/trade-engine/framedCopy/config"

)

type ConnectionState interface {
    IsConnected() bool
    RegisterConnectionCallback(callback func())
}

type Exchange interface {
    ConnectionState
    SendOrder(order *types.Order) (string, error)
    CancelOrder(orderID string) error
    SetTradingHandler(handler TradingHandler)
    SetExecutionHandler(handler ExecutionHandler)
    SetAccountHandler(handler AccountHandler)
    SetOnReadyCallback(callback func())
    GetOnReadyCallback() func()
    Connect() error
    SubscribeAll(cfg *config.Config) error
    StartReading()
    SetFundingRateHandler(handler FundingRateHandler)
    SetOrderbookHandler(handler OrderbookHandler)
    
}

type TradingHandler interface {
    OnOrderUpdate(update *types.OrderUpdate)
    OnForceCancel(order *types.Order)
    OnOrderDisconnect()
    OnOrderError(error string)
    OnOrderConnect()
}

type FundingRateHandler interface {
    OnFundingRateUpdate(exchange types.ExchangeID, fundingRate *types.FundingRate)
    OnFundingRateError(error string)
    OnFundingRateConnect()
    OnFundingRateDisconnect()
}

type ExecutionHandler interface {
    OnExecutionUpdate(executions []*types.OrderUpdate)
    OnExecutionDisconnect()
    OnExecutionError(error string)
    OnExecutionConnect()
}

type OrderbookHandler interface {
    OnOrderbookUpdate(exchangeID types.ExchangeID, update *types.OrderBookUpdate)
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
    Orders sync.Map
}

var GlobalOrderStore = &OrderStore{}

func (s *OrderStore) StoreOrder(order *types.Order) {
    s.Orders.Store(order.ClientOrderID, order)
}

func (s *OrderStore) GetOrder(clientOrderID string) (*types.Order, bool) {
    order, ok := s.Orders.Load(clientOrderID)
    if !ok {
        return nil, false
    }
    return order.(*types.Order), true
}