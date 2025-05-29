package exchange

import "github.com/souravmenon1999/trade-engine/framed/types"

type Exchange interface {

    PlaceOrder(order *types.Order) (string, error)
    CancelOrder(orderID string) error
    // Add more methods like SubscribeOrderBook, GetPosition, etc., as needed
}