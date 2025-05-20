package injective

import (
	"github.com/InjectiveLabs/sdk-go/client/exchange"
	"github.com/souravmenon1999/trade-engine/framed/exchange/net/grpc/injective"
)

// InitUpdatesClient initializes the Injective updates client.
func InitUpdatesClient(exchangeClient exchange.ExchangeClient) *injective.InjectiveUpdatesClient {
	return injective.NewInjectiveUpdatesClient(exchangeClient)
}