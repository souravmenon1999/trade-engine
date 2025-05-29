package exchange

import "github.com/souravmenon1999/trade-engine/framed/types"

type Exchange interface {
    SendOrder(order *types.Order) (string, error)
    CancelOrder(orderID string) error // Simple: just the ID or hash    // Add more methods as needed
}