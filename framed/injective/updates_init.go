package injective

import (
	"github.com/souravmenon1999/trade-engine/framed/grpc/injective"
)

// InitUpdatesClient initializes the Injective updates client.
func InitUpdatesClient(tmEndpoint string) (*injective.InjectiveUpdatesClient, error) {
	return injective.NewInjectiveUpdatesClient(tmEndpoint)
}