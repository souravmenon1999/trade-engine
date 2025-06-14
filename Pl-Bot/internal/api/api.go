package api

import (
	"context"
	"encoding/json"
	"log"

	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/pnl"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/streams"

	"github.com/InjectiveLabs/sdk-go/client/common"
	exchangeclient "github.com/InjectiveLabs/sdk-go/client/exchange"
	derivativeExchangePB "github.com/InjectiveLabs/sdk-go/exchange/derivative_exchange_rpc/pb"
)

// FetchHistoricalTrades fetches historical trades for the given subaccount and market.
func FetchHistoricalTrades(ctx context.Context, subaccountID, marketID string) error {
	network := common.LoadNetwork("mainnet", "lb")
	exchangeClient, err := exchangeclient.NewExchangeClient(network)
	if err != nil {
		panic (err)
	}

	
	

	req := derivativeExchangePB.TradesV2Request{
		SubaccountId: subaccountID,
		MarketId:     marketID,
	}

	res, err := exchangeClient.GetDerivativeTradesV2(ctx, &req)
	if err != nil {
		return err
	}

	tradesJSON, err := json.MarshalIndent(res.Trades, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal trades to JSON: %v", err)
	} else {
		log.Printf("Fetched %d historical derivative trades:\n%s", len(res.Trades), string(tradesJSON))
	}

	for _, trade := range res.Trades {
		pnl.ProcessTrade(trade, streams.GetLatestMarketPrice())
	}

	return nil
}

// StreamLiveTrades starts streaming live trades for the given subaccount and market.
func StreamLiveTrades(subaccountID, marketID string) error {
	receiver, err := streams.SubscribeToTradeStream(subaccountID, marketID)
	if err != nil {
		return err
	}

	for {
		trade, err := receiver()
		if err != nil {
			log.Printf("Error receiving trade, attempting to continue: %v", err)
			continue
		}
		pnl.ProcessTrade(trade, streams.GetLatestMarketPrice())
	}
}