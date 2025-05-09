// internal/exchange/exchange.go
package exchange

import (
	"context"
	"trading-system/internal/types"
)

// ExchangeClient defines the common interface for interacting with exchanges.
type ExchangeClient interface {
	// SubscribeOrderbook connects to the exchange's data feed and starts
	// processing orderbook updates for the given symbol.
	SubscribeOrderbook(ctx context.Context, symbol string) error

	// SubmitOrder sends an order to the exchange.
	// Returns the exchange's order ID on success.
	SubmitOrder(ctx context.Context, order types.Order) (string, error)

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