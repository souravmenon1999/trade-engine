package exchange

import "github.com/souravmenon1999/trade-engine/framedCopy/types"

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