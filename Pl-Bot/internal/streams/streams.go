package streams

import (
	"context"
	"log"

	"injective_pnl_bot/internal/pnl"

	"github.com/InjectiveLabs/sdk-go/client/common"
	exchangeclient "github.com/InjectiveLabs/sdk-go/client/exchange"
	spotExchangePB "github.com/InjectiveLabs/sdk-go/exchange/spot_exchange_rpc/pb"
)

// SubscribeToTradeStream subscribes to live trade updates for a subaccount and market
func SubscribeToTradeStream(subaccountID, marketID string) {
	// Load testnet network
	network := common.LoadNetwork("testnet", "lb")
	exchangeClient, err := exchangeclient.NewExchangeClient(network)
	if err != nil {
		log.Fatalf("Failed to create exchange client: %v", err)
	}

	// Prepare request
	ctx := context.Background()
	req := spotExchangePB.StreamTradesV2Request{
		SubaccountId: subaccountID,
		MarketId:     marketID,
	}

	// Start streaming
	stream, err := exchangeClient.StreamSpotTradesV2(ctx, &req)
	if err != nil {
		log.Fatalf("Failed to subscribe to trade stream: %v", err)
	}

	// Process stream events
	for {
		res, err := stream.Recv()
		if err != nil {
			log.Printf("Stream error: %v, reconnecting...", err)
			continue
		}
		pnl.ProcessTrade(res.Trade)
	}
}