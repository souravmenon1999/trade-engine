package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/cometbft/cometbft/types"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/souravmenon1999/trade-engine/framed/config"
	"github.com/souravmenon1999/trade-engine/framed/injective"
	"github.com/souravmenon1999/trade-engine/framed/processor/bybitorderbook"
	bybitws "github.com/souravmenon1999/trade-engine/framed/websockets/bybit"
)

func main() {
	configPath := flag.String("config", "yamls/config.yaml", "Path to config file")
	flag.Parse()

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	bybitOrderbookWSClient := bybitws.NewBybitOrderBookWSClient(cfg.BybitOrderbook.WSUrl)
	if err := bybitOrderbookWSClient.Connect(); err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Bybit orderbook websocket")
	}
	defer bybitOrderbookWSClient.Close()

	processedDataChannel := make(chan *types.OrderBookWithVWAP, 10)
	injectiveUpdatesChannel := make(chan types.EventData, 10)

	instrument := &types.Instrument{
		BaseCurrency:  cfg.BybitOrderbook.BaseCurrency,
		QuoteCurrency: cfg.BybitOrderbook.QuoteCurrency,
		MinLotSize:    types.Quantity(100000),
		ContractType:  "Perpetual",
	}

	vwapProcessor := bybitorderbook.NewBybitVWAPProcessor(bybitOrderbookWSClient.RawMessageCh, processedDataChannel, cfg.BybitOrderbook.Symbol, instrument)
	vwapProcessor.StartProcessing()

	orderBookTopic := "orderbook.50." + cfg.BybitOrderbook.Symbol
	if err := bybitOrderbookWSClient.Subscribe(orderBookTopic); err != nil {
		log.Fatal().Err(err).Msgf("Failed to subscribe to Bybit orderbook for %s", cfg.BybitOrderbook.Symbol)
	}

	// Initialize Injective Trade Client
	tradeClient, err := injective.InitTradeClient(
		cfg.InjectiveExchange.NetworkName,
		cfg.InjectiveExchange.Lb,
		cfg.InjectiveExchange.PrivKey,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize Injective trade client")
	}

	// Initialize Injective Updates Client
	updatesClient, err := injective.InitUpdatesClient("wss://tm.injective.testnet.network/websocket")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize Injective updates client")
	}

	_ = tradeClient
	_ = updatesClient
	_ = injectiveUpdatesChannel

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for data := range processedDataChannel {
			log.Info().Int64("vwap", int64(data.VWAP)).Msg("Calculated VWAP")
		}
		log.Println("Main processing goroutine finished.")
	}()

	<-stopCh
	log.Info().Msg("Shutting down...")
}