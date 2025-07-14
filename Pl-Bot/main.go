package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sync"
	//"fmt"
	"time"
    //"sync/atomic"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/api/injective"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/cache/injective"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/pnl/injective"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/streams/injective"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/api/bybit"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/pnl/bybit"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/cache/bybit"
	accountPB "github.com/InjectiveLabs/sdk-go/exchange/accounts_rpc/pb"
	exchangeclient "github.com/InjectiveLabs/sdk-go/client/exchange"
	chainclient "github.com/InjectiveLabs/sdk-go/client/chain"
	"github.com/InjectiveLabs/sdk-go/client/common"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	"gopkg.in/yaml.v3"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
    //"github.com/go-telegram-bot-api/telegram-bot-api/v5"
    "github.com/souravmenon1999/trade-engine/Pl-Bot/internal/telegram"
    "github.com/robfig/cron/v3"
)

type Config struct {
	MarketID       string `yaml:"market_id"`
	SubaccountID   string `yaml:"subaccount_id"`
	AccountAddress string `yaml:"AccountAddress"`
	MongoURI       string `yaml:"mongo_uri"`
	Bybit          struct {
		APIKey    string `yaml:"api_key"`
		APISecret string `yaml:"api_secret"`
		Symbol    string `yaml:"symbol"`
	} `yaml:"bybit"`
    Telegram struct {
        BotToken  string `yaml:"bot_token"`
        ChannelID string `yaml:"channel_id"`
    } `yaml:"telegram"`
}

var (
	lastFetch        time.Time
	fetchMutex       sync.Mutex
	lastGasFetch     time.Time
	rateLimit        = 5 * time.Second
	gasFetchInterval = 5 * time.Minute
	
)

func rateLimitedFetchPortfolio(client exchangeclient.ExchangeClient, ctx context.Context, accountAddress, subaccountID string, includeSubaccounts bool) error {
	fetchMutex.Lock()
	defer fetchMutex.Unlock()
	if time.Since(lastFetch) < rateLimit {
		log.Printf("Rate limit: Skipping portfolio fetch, last fetch at %v", lastFetch)
		return nil
	}
	log.Printf("fetched")
	lastFetch = time.Now()
	return injectiveApi.FetchAccountPortfolio(client, ctx, accountAddress, subaccountID, includeSubaccounts)
}

func calculateGasFromMongo(ctx context.Context, chainClient chainclient.ChainClient, mongoClient *mongo.Client, accountAddress string, since time.Time, fetchAll bool) error {
    collection := mongoClient.Database("trading").Collection("trading")
    count, err := collection.CountDocuments(ctx, bson.M{})
    if err != nil {
        log.Printf("Failed to count documents: %v", err)
        return err
    }
    log.Printf("Found %d documents in trading.trading", count)

    filter := bson.M{"account_address": accountAddress}
    if !fetchAll {
        filter["created_at"] = bson.M{"$gt": since}
    }
    log.Printf("Querying MongoDB with filter: %+v", filter)
    cursor, err := collection.Find(ctx, filter)
    if err != nil {
        log.Printf("Failed to query MongoDB: %v", err)
        return err
    }
    defer cursor.Close(ctx)

    gasCalc := injectivePnl.NewGasCalculator()
    var txCount int
    for cursor.Next(ctx) {
        var txDoc struct {
            TxHash    string    `bson:"tx_hash"`
            CreatedAt time.Time `bson:"created_at"`
        }
        if err := cursor.Decode(&txDoc); err != nil {
            log.Printf("Failed to decode document: %v", err)
            continue
        }
        log.Printf("Fetched tx_hash: %s, created_at: %v", txDoc.TxHash, txDoc.CreatedAt)
        gasCalc.AddTxHash(txDoc.TxHash)
        txCount++
    }
    log.Printf("Fetched %d transaction hashes", txCount)

    if err := cursor.Err(); err != nil {
        log.Printf("Cursor error: %v", err)
        return err
    }

    totalGas, err := gasCalc.CalculateGasWithChainClient(ctx, chainClient)
    if err != nil {
        log.Printf("Failed to calculate gas: %v", err)
        return err
    }
    injectiveCache.ResetTotalGas()
    injectiveCache.AddTotalGas(totalGas)
    log.Printf("Updated total gas: %v", injectiveCache.GetTotalGas())
    return nil
}

func logPortfolio(client exchangeclient.ExchangeClient, ctx context.Context, accountAddress string) {
	res, err := client.GetPortfolio(ctx, accountAddress)
	if err != nil {
		log.Printf("Failed to fetch portfolio: %v", err)
		return
	}
	str, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal portfolio response: %v", err)
		return
	}
	log.Printf("Raw Portfolio Response:\n%s", string(str))

	portfolio := res.GetPortfolio()
	if portfolio == nil {
		log.Println("Portfolio response is empty")
	}
}

func logRewards(client exchangeclient.ExchangeClient, ctx context.Context, accountAddress string) {
	req := accountPB.RewardsRequest{
		Epoch:         -1,
		AccountAddress: accountAddress,
	}
	res, err := client.GetRewards(ctx, &req)
	if err != nil {
		log.Printf("Failed to fetch rewards: %v", err)
		return
	}
	if len(res.GetRewards()) > 0 {
		for _, reward := range res.GetRewards() {
			for _, coin := range reward.GetRewards() {
				log.Printf("Rewards - Denom: %s, Amount: %s, Distributed At: %d",
					coin.GetDenom(), coin.GetAmount(), reward.GetDistributedAt())
			}
		}
	} else {
		log.Println("No rewards found")
	}
	str, _ := json.MarshalIndent(res, "", "  ")
	log.Printf("Full Rewards Response:\n%s", string(str))
}

func main() {
    configFile, err := os.ReadFile("config.yaml")
    if err != nil {
        log.Fatalf("Failed to read config file: %v", err)
    }
    var config Config
    err = yaml.Unmarshal(configFile, &config)
    if err != nil {
        log.Fatalf("Failed to unmarshal config: %v", err)
    }



    network := common.LoadNetwork("mainnet", "lb")
    client, err := exchangeclient.NewExchangeClient(network)
    if err != nil {
        log.Fatalf("Failed to create exchange client: %v", err)
    }

    bybitClient := bybitApi.NewClient(config.Bybit.APIKey, config.Bybit.APISecret)

    bot, err := telegram.NewBot(telegram.TelegramConfig{
    BotToken:  config.Telegram.BotToken,
    ChannelID: config.Telegram.ChannelID,
    })
    if err != nil {
        log.Fatalf("Failed to initialize Telegram bot: %v", err)
    }

    ctx := context.Background()
    mongoClient, err := mongo.Connect(ctx, options.Client().ApplyURI(config.MongoURI))
    if err != nil {
        log.Fatalf("Failed to connect to MongoDB: %v, URI: %s", err, config.MongoURI)
    }
    err = mongoClient.Ping(ctx, nil)
    if err != nil {
        log.Fatalf("Failed to ping MongoDB: %v", err)
    }
    log.Printf("Successfully connected to MongoDB")
    collection := mongoClient.Database("plbot").Collection("transactions")
    count, err := collection.CountDocuments(ctx, bson.M{})
    if err != nil {
        log.Fatalf("Failed to count documents in plbot.transactions: %v", err)
    }
    log.Printf("Found %d documents in plbot.transactions", count)
    defer mongoClient.Disconnect(ctx)

    tmRPC, err := rpchttp.New(network.TmEndpoint, "/websocket")
    if err != nil {
        log.Fatalf("Failed to create Tendermint RPC client: %v", err)
    }
    clientCtx, err := chainclient.NewClientContext(network.ChainId, config.AccountAddress, nil)
    if err != nil {
        log.Fatalf("Failed to create client context: %v", err)
    }
    clientCtx = clientCtx.WithNodeURI(network.TmEndpoint).WithClient(tmRPC)
    chainClient, err := chainclient.NewChainClient(clientCtx, network, common.OptionGasPrices("0.0005inj"))
    if err != nil {
        log.Fatalf("Failed to create chain client: %v", err)
    }

    injectiveCache.Init()
    bybitCache.Init()

    // marketDetails, err := injectiveStreams.FetchMarketDetails(client, ctx, config.MarketID)
    // if err != nil {
    //     log.Fatalf("Failed to fetch market details: %v", err)
    // }

    // baseSymbol := marketDetails.Market.OracleBase
    // quoteSymbol := marketDetails.Market.OracleQuote
    // oracleType := marketDetails.Market.OracleType

    // go injectiveStreams.SubscribeToMarketPriceStream(client, baseSymbol, quoteSymbol, oracleType)

    err = injectiveApi.FetchAccountPortfolio(client, ctx, config.AccountAddress, config.SubaccountID, true)
    if err != nil {
        log.Fatalf("Failed to fetch account portfolio: %v", err)
    }
    log.Println("Portfolio fetched successfully")

    err = calculateGasFromMongo(ctx, chainClient, mongoClient, config.AccountAddress, time.Time{}, true)
    if err != nil {
        log.Printf("Failed to calculate initial gas: %v", err)
    }

    logPortfolio(client, ctx, config.AccountAddress)
    logRewards(client, ctx, config.AccountAddress)

        c := cron.New()
    _, err = c.AddFunc("*/10 * * * *", func() {
        err := telegram.SendHourlyUpdate(ctx, bot, config.Telegram.ChannelID)
        if err != nil {
            log.Printf("Failed to send Telegram update: %v", err)
        }
    })
    if err != nil {
        log.Fatalf("Failed to schedule Telegram updates: %v", err)
    }
    c.Start()
    defer c.Stop()

    go func() {
        time.Sleep(10 * time.Second)
        err := telegram.SendHourlyUpdate(ctx, bot, config.Telegram.ChannelID)
        if err != nil {
            log.Printf("Failed to send initial Telegram update: %v", err)
        }
    }()

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
                    if err := rateLimitedFetchPortfolio(client, ctx, config.AccountAddress, config.SubaccountID, true); err != nil {
                        log.Printf("Failed to fetch portfolio on stream update: %v", err)
                    }
                }
            },
        )
        if err != nil {
            log.Printf("Failed to start portfolio stream: %v", err)
        }
    }()

    err = injectiveApi.FetchHistoricalTrades(client, ctx, config.SubaccountID, config.MarketID)
    if err != nil {
        log.Fatalf("Failed to fetch historical trades: %v", err)
    }

    err = bybitClient.FetchHistoricalTrades(ctx, "linear", config.Bybit.Symbol)
    if err != nil {


        log.Fatalf("Failed to fetch Bybit historical trades: %v", err)
    }

    go func() {
        err := injectiveApi.StreamLiveTrades(client, config.SubaccountID, config.MarketID)
        if err != nil {
            log.Fatalf("Failed to stream live trades: %v", err)
        }
    }()

    go func() {
        for {
            time.Sleep(5 * time.Second)
            if time.Since(lastGasFetch) >= gasFetchInterval {
                err := calculateGasFromMongo(ctx, chainClient, mongoClient, config.AccountAddress, lastGasFetch, false)
                if err != nil {
                    log.Printf("Failed to calculate periodic gas: %v", err)
                }
                lastGasFetch = time.Now()
            }
        }
    }()

    for {
        time.Sleep(5 * time.Second)
        injectivePnl.PrintRealizedPnL()
        bybitPnl.PrintRealizedPnL()
        //logPortfolio(client, ctx, config.AccountAddress)
        //logRewards(client, ctx, config.AccountAddress)
    }
}