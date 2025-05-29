package ordermanager

import (
    "github.com/souravmenon1999/trade-engine/framed/exchange"
    "github.com/souravmenon1999/trade-engine/framed/types"
    "fmt"
)

type OrderManager struct {
    exchanges map[types.ExchangeID]exchange.Exchange
}

func NewOrderManager() *OrderManager {
    return &OrderManager{
        exchanges: make(map[types.ExchangeID]exchange.Exchange),
    }
}

func (om *OrderManager) RegisterExchange(id types.ExchangeID, ex exchange.Exchange) {
    om.exchanges[id] = ex
}

func (om *OrderManager) PlaceOrder(order *types.Order) (string, error) {
    ex := om.exchanges[order.ExchangeID]
    if ex == nil {
        return "", fmt.Errorf("no exchange found for ID: %s", order.ExchangeID)
    }
    return ex.SendOrder(order)
}

func (om *OrderManager) CancelOrder(exchangeID types.ExchangeID, orderID string) error {
    ex := om.exchanges[exchangeID]
    if ex == nil {
        return fmt.Errorf("no exchange found for ID: %s", exchangeID)
    }
    return ex.CancelOrder(orderID)
}