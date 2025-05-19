package bybit

import (
	"github.com/souravmenon1999/trade-engine/framed/websockets/bybitws"
)

// BybitUpdatesClient handles order status updates on Bybit.
type BybitUpdatesClient struct {
	wsClient *bybitws.BybitExchangeClient
}

// InitUpdatesClient initializes the Bybit updates client.
func InitUpdatesClient(wsURL, apiKey, apiSecret string) (*BybitUpdatesClient, error) {
	wsClient := bybitws.NewBybitExchangeClient(wsURL, apiKey, apiSecret)
	if err := wsClient.Connect(); err != nil {
		return nil, err
	}
	return &BybitUpdatesClient{
		wsClient: wsClient,
	}, nil
}