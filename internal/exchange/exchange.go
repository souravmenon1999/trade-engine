// internal/exchange/exchange.go
package exchange

import (
	"context"
	"github.com/souravmenon1999/trade-engine/internal/types"
	"github.com/souravmenon1999/trade-engine/internal/exchange/bybit"
	"github.com/souravmenon1999/trade-engine/internal/exchange/injective"

)

// ExchangeClient defines the common interface for interacting with exchanges.
type ExchangeClient interface {
	// SubscribeOrderbook connects to the exchange's data feed and starts
	// processing orderbook updates for the given symbol.
	SubscribeOrderbook(ctx context.Context, symbol string) error

	// SubmitOrder sends an order to the exchange.
	// Returns the exchange's order ID on success.
	SubmitOrder(ctx context.Context, order types.Order) (string, error)

 // CancelAllOrders cancels all open orders for a given instrument on the exchange.
	// This is useful for clearing the books.
	CancelAllOrders(ctx context.Context, instrument *types.Instrument) error

	// ReplaceQuotes atomically cancels existing quotes for an instrument
	// (typically all for a market/subaccount) and places the provided new orders.
	// This is suitable for strategies that update their entire quote.
	// Returns IDs for the newly placed orders.
	ReplaceQuotes(ctx context.Context, instrument *types.Instrument, ordersToPlace []*types.Order) ([]string, error)



	// Close cleans up any resources (connections, goroutines).
	Close() error

	// GetOrderbook returns the current state of the orderbook.
	// It should return a thread-safe representation or snapshot.
	// Note: Not all clients (like our Injective client) will provide a live OB.
	// A client might return a nil or empty Orderbook if it's purely for trading.
	GetOrderbook() *types.Orderbook

	// GetExchangeType returns the type of the exchange.
	GetExchangeType() types.ExchangeType
}

// Ensure BybitClient implements the interface (add a dummy SubmitOrder for now)
var _ ExchangeClient = (*bybit.Client)(nil) // Add this line in internal/exchange/bybit/client.go

// Ensure InjectiveClient implements the interface
// var _ ExchangeClient = (*injective.Client)(nil) // Add this line later in internal/exchange/injective/client.go