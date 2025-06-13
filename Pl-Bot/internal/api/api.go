package api

import (
	"context"
	"log"

	"injective_pnl_bot/internal/cache"
	"injective_pnl_bot/internal/pnl"

	"github.com/InjectiveLabs/sdk-go/client/common"
	exchangeclient "github.com/InjectiveLabs/sdk-go/client/exchange"
	spotExchangePB "github.com/InjectiveLabs/sdk-go/exchange/spot_exchange_rpc/pb"
)

// FetchHistoricalTrades retrieves all historical trades for a subaccount and market
func FetchHistoricalTrades(ctx context.Context, subaccountID, marketID string) error {
	// Load testnet network
	network := common.LoadNetwork("testnet", "lb")
	exchangeClient, err := exchangeclient.NewExchangeClient(network)
	if err != nil {
		return err
	}

	// Prepare request
	req := spotExchangePB.TradesV2Request{
		SubaccountId: subaccountID,
		MarketId:     marketID,
	}

	// Fetch trades
	res, err := exchangeClient.GetSpotTradesV2(ctx, &req)
	if err != nil {
		return err
	}

	// Process each trade
	for _, trade := range res.Trades {
		pnl.ProcessTrade(trade)
	}

	log.Printf("Fetched and processed %d historical trades", len(res.Trades))
	return nil
}