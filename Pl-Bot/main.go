package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/api"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/cache"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/pnl"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/streams"

	"gopkg.in/yaml.v3"
)

type Config struct {
	MarketID     string `yaml:"market_id"`
	SubaccountID string `yaml:"subaccount_id"`
}

func main() {
	// Load configuration
	configFile, err := os.ReadFile("config.yaml")
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}

	var config Config
	err = yaml.Unmarshal(configFile, &config)
	if err != nil {
		log.Fatalf("Failed to unmarshal config: %v", err)
	}

	// Initialize cache
	cache.Init()

	// Fetch market details
	ctx := context.Background()
	marketDetails, err := streams.FetchMarketDetails(ctx, config.MarketID)
	if err != nil {
		log.Fatalf("Failed to fetch market details: %v", err)
	}

	// Extract oracle details
	baseSymbol := marketDetails.Market.OracleBase
	quoteSymbol := marketDetails.Market.OracleQuote
	oracleType := marketDetails.Market.OracleType

	// Start price stream
	go streams.SubscribeToMarketPriceStream(baseSymbol, quoteSymbol, oracleType)

	// Wait briefly for price stream to initialize
	time.Sleep(2 * time.Second)

	// Fetch historical trades
	err = api.FetchHistoricalTrades(ctx, config.SubaccountID, config.MarketID)
	if err != nil {
		log.Fatalf("Failed to fetch historical trades: %v", err)
	}

	// Start live trade stream
	go func() {
		err := api.StreamLiveTrades(config.SubaccountID, config.MarketID)
		if err != nil {
			log.Fatalf("Failed to stream live trades: %v", err)
		}
	}()

	// Periodically print PnL
	for {
		time.Sleep(5 * time.Second)
		pnl.PrintRealizedPnL()
	}
}