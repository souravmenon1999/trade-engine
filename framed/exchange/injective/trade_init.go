package injective

import (
	"github.com/souravmenon1999/trade-engine/framed/exchange/net/grpc/injective"
)

// InitTradeClient initializes the Injective trading client.
func InitTradeClient(networkName, lb, privKey string) (*injective.InjectiveTradeClient, error) {
	return injective.NewInjectiveTradeClient(networkName, lb, privKey)
}