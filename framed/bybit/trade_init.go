package bybit

import (
	"github.com/souravmenon1999/trade-engine/framed/websockets/bybitws"
)

// BybitTradeClient handles trading operations on Bybit.
type BybitTradeClient struct {
	wsClient *bybitws.BybitExchangeClient
}

// InitTradeClient initializes the Bybit trading client.
func InitTradeClient(wsURL, apiKey, apiSecret string) (*BybitTradeClient, error) {
	wsClient := bybitws.NewBybitExchangeClient(wsURL, apiKey, apiSecret)
	if err := wsClient.Connect(); err != nil {
		return nil, err
	}
	return &BybitTradeClient{
		wsClient: wsClient,
	}, nil
}