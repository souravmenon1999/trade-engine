// internal/exchange/exchange.go
package exchange

import (
	"context"
	"github.com/souravmenon1999/trade-engine/internal/types"
	// "github.com/souravmenon1999/trade-engine/internal/exchange/bybit"

)

// ExchangeClient defines the common interface for interacting with exchanges.
type ExchangeClient interface {
	// SubscribeOrderbook connects to the exchange's data feed and starts
	SubscribeOrderbook(ctx context.Context, symbol string) error

	// SubmitOrder sends an order to the exchange.
	SubmitOrder(ctx context.Context, order types.Order) (string, error)

 // CancelAllOrders cancels all open orders for a given instrument on the exchange.
	
	CancelAllOrders(ctx context.Context, instrument *types.Instrument) error

	// ReplaceQuotes atomically cancels existing quotes for an instrument
	ReplaceQuotes(ctx context.Context, instrument *types.Instrument, ordersToPlace []*types.Order) ([]string, error)



	// Close cleans up any resources (connections, goroutines).
	Close() error

	// GetOrderbook returns the current state of the orderbook.
	GetOrderbook() *types.Orderbook

	// GetExchangeType returns the type of the exchange.
	GetExchangeType() types.ExchangeType

	
}

