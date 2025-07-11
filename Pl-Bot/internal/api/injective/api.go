package injectiveApi

import (
	"context"
	"encoding/json"
	"log"
	"strings"
explorer "github.com/InjectiveLabs/sdk-go/client/explorer"

	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/pnl/injective"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/streams/injective"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/cache/injective"

	//"github.com/InjectiveLabs/sdk-go/client/common"
	exchangeclient "github.com/InjectiveLabs/sdk-go/client/exchange"
	derivativeExchangePB "github.com/InjectiveLabs/sdk-go/exchange/derivative_exchange_rpc/pb"
)

type PortfolioResponse struct {
    Type         string `json:"type"`
    Denom        string `json:"denom"`
    Amount       string `json:"amount"`
    USDValue     string `json:"usd_value"` // Add this if the stream provides it
    SubaccountId string `json:"subaccount_id"`
}

func FetchAccountPortfolio(client exchangeclient.ExchangeClient, ctx context.Context, accountAddress, subaccountID string, includeSubaccounts bool) error {
    res, err := client.GetAccountPortfolioBalances(ctx, accountAddress, includeSubaccounts)
    if err != nil {
        return err
    }
    log.Printf("Account portfolio response:\n%s", res.String())
    if res != nil {
        portfolio := res.GetPortfolio()
        if portfolio != nil {
            injectiveCache.UpdateTotalUSD(portfolio.GetTotalUsd())
            log.Printf("Updated total USD: %s", portfolio.GetTotalUsd())
        }
    }
    return nil
}

// FetchHistoricalTrades fetches historical trades for the given subaccount and market.
func FetchHistoricalTrades(client exchangeclient.ExchangeClient , ctx context.Context, subaccountID, marketID string) error {
	

	req := derivativeExchangePB.TradesV2Request{
		SubaccountId: subaccountID,
		MarketId:     marketID,
	}

	res, err := client.GetDerivativeTradesV2(ctx, &req)
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
		injectivePnl.ProcessTrade(trade, injectiveStreams.GetLatestMarketPrice())
	}

	return nil
}

// StreamLiveTrades starts streaming live trades for the given subaccount and market.
func StreamLiveTrades(client exchangeclient.ExchangeClient,subaccountID, marketID string) error {
	receiver, err := injectiveStreams.SubscribeToTradeStream( client  ,subaccountID, marketID)
	if err != nil {
		return err
	}

	for {
		trade, err := receiver()
		if err != nil {
			log.Printf("Error receiving trade, attempting to continue: %v", err)
			continue
		}
		injectivePnl.ProcessTrade(trade, injectiveStreams.GetLatestMarketPrice())
	}
}

// FetchHistoricalOrders fetches historical orders with pagination and calculates gas.
func FetchHistoricalOrders(client exchangeclient.ExchangeClient, explorerClient explorer.ExplorerClient, ctx context.Context, subaccountID, marketID string, skip uint64, limit int32) ([]*derivativeExchangePB.DerivativeOrderHistory, error) {
	req := derivativeExchangePB.OrdersHistoryRequest{
		SubaccountId: subaccountID,
		MarketId:     marketID,
		Skip:         skip,
		Limit:        limit,
		 State:        "filled", // Uncomment to filter by filled orders
	}
	res, err := client.GetHistoricalDerivativeOrders(ctx, &req)
	if err != nil {
		return nil, err
	}

	gasCalc := injectivePnl.NewGasCalculator(explorerClient)
	var validTxHashes, invalidTxHashes []string
	stateCounts := make(map[string]int)
	for _, order := range res.Orders {
		injectiveCache.AddOrder(order) // Add to sync.Map if non-terminal
		stateCounts[order.State]++
		if order.TxHash != "" {
			normalized := strings.TrimPrefix(order.TxHash, "0x")
			if len(normalized) == 64 && normalized != "0000000000000000000000000000000000000000000000000000000000000000" {
				validTxHashes = append(validTxHashes, order.TxHash)
			} else {
				invalidTxHashes = append(invalidTxHashes, order.TxHash)
			}
			gasCalc.AddTxHash(order.TxHash)
		}
	}

	// Log order state distribution
	log.Printf("Fetched %d historical orders with state distribution: booked=%d, partial_filled=%d, filled=%d, canceled=%d",
		len(res.Orders), stateCounts["booked"], stateCounts["partial_filled"], stateCounts["filled"], stateCounts["canceled"])

	// Log valid and invalid transaction hashes
	log.Printf("Collected %d transaction hashes from historical orders: %d valid, %d invalid", len(validTxHashes)+len(invalidTxHashes), len(validTxHashes), len(invalidTxHashes))
	if len(validTxHashes) > 0 {
		log.Printf("Valid tx_hashes: %s", strings.Join(validTxHashes, ", "))
	}
	if len(invalidTxHashes) > 0 {
		log.Printf("Invalid tx_hashes: %s", strings.Join(invalidTxHashes, ", "))
	}

	totalGas, err := gasCalc.CalculateGas(ctx)
	if err != nil {
		log.Printf("Error calculating gas: %v", err)
	} else {
		injectiveCache.AddTotalGas(totalGas)
	}

	return res.Orders, nil
}