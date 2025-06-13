package main

import (
	"context"
	"log"
	"time"

	"injective_pnl_bot/internal/api"
	"injective_pnl_bot/internal/cache"
	"injective_pnl_bot/internal/pnl"
	"injective_pnl_bot/internal/streams"
)

func main() {
	// Initialize the cache
	cache.Init()

	// User-specific details (replace these with your actual values)
	subaccountID := "0xaf79152ac5df276d9a8e1e2e22822f9713474902000000000000000000000000"
	marketID := "0xa508cb32923323679f29a032c70342c147c17d0145625922b0ef22e955c844c0"

	// Fetch all historical trades first
	ctx := context.Background()
	err := api.FetchHistoricalTrades(ctx, subaccountID, marketID)
	if err != nil {
		log.Fatalf("Failed to fetch historical trades: %v", err)
	}

	// Start streaming live trades in a goroutine
	go streams.SubscribeToTradeStream(subaccountID, marketID)

	// Periodically print realized PnL (every 5 seconds)
	for {
		time.Sleep(5 * time.Second)
		pnl.PrintRealizedPnL()
	}
}