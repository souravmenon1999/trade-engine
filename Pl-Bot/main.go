package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/api/injective"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/cache/injective"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/pnl/injective"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/streams/injective"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/api/bybit"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/pnl/bybit"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/cache/bybit"

	exchangeclient "github.com/InjectiveLabs/sdk-go/client/exchange"
	//explorerclient "github.com/InjectiveLabs/sdk-go/client/explorer"
	"github.com/InjectiveLabs/sdk-go/client/common"
	"gopkg.in/yaml.v3"
)

type Config struct {
	MarketID     string `yaml:"market_id"`
	SubaccountID string `yaml:"subaccount_id"`
	AccountAddress string `yaml:"AccountAddress"`
	Bybit          struct {
		APIKey    string `yaml:"api_key"`
		APISecret string `yaml:"api_secret"`
		Symbol    string `yaml:"symbol"`
	} `yaml:"bybit"`
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

	// Create the exchange client
	network := common.LoadNetwork("mainnet", "lb")
	client, err := exchangeclient.NewExchangeClient(network)
	if err != nil {
		log.Fatalf("Failed to create exchange client: %v", err)
	}

	// Create Bybit client
	bybitClient := bybitApi.NewClient(config.Bybit.APIKey, config.Bybit.APISecret)

		// Create the explorer client
	// explorerClient, err := explorerclient.NewExplorerClient(network)
	// if err != nil {
	// 	log.Fatalf("Failed to create explorer client: %v", err)
	// }

	// Initialize cache
	injectiveCache.Init()
	bybitCache.Init()


		// Initialize order cache for tracking order IDs
	
	// Fetch market details
	ctx := context.Background()
	marketDetails, err := injectiveStreams.FetchMarketDetails(client, ctx, config.MarketID)
	if err != nil {
		log.Fatalf("Failed to fetch market details: %v", err)
	}

	// Extract oracle details
	baseSymbol := marketDetails.Market.OracleBase
	quoteSymbol := marketDetails.Market.OracleQuote
	oracleType := marketDetails.Market.OracleType

	// Start price stream
	go injectiveStreams.SubscribeToMarketPriceStream(client, baseSymbol, quoteSymbol, oracleType)


	// Start account portfolio stream
	
	err = injectiveApi.FetchAccountPortfolio(client, ctx, config.AccountAddress, config.SubaccountID, true)
    if err != nil {
        log.Fatalf("Failed to fetch account portfolio: %v", err)
    }

    log.Println("Portfolio fetched successfully")

	
// Start account portfolio stream
	  go func() {
        err := injectiveStreams.SubscribeToAccountPortfolioStream(
            client,
            config.AccountAddress,
            config.SubaccountID,
            "total_balances",
            func(res *injectiveStreams.PortfolioResponse, err error) {
                if err != nil {
                    log.Printf("Portfolio error: %v", err)
                    return
                }
                if res.Type == "total_balances" {
                    log.Printf("Balance update - Denom: %s, Amount: %s", res.Denom, res.Amount)
                }
            },
        )
        if err != nil {
            log.Printf("Failed to start portfolio stream: %v", err)
        }
    }()

	// Fetch historical trades
	err =injectiveApi.FetchHistoricalTrades(client,ctx, config.SubaccountID, config.MarketID)
	if err != nil {
		log.Fatalf("Failed to fetch historical trades: %v", err)
	}

	err = bybitClient.FetchHistoricalTrades(ctx, "linear", config.Bybit.Symbol)
	if err != nil {
		log.Fatalf("Failed to fetch Bybit historical trades: %v", err)
	}
	 

	// Start live trade stream
	go func() {
		err := injectiveApi.StreamLiveTrades(client,config.SubaccountID, config.MarketID)
		if err != nil {
			log.Fatalf("Failed to stream live trades: %v", err)
		}
	}()

	// Periodically print PnL
	for {
		time.Sleep(5 * time.Second)
		injectivePnl.PrintRealizedPnL()
		bybitPnl.PrintRealizedPnL()
	}
}