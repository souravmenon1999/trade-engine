package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/souravmenon1999/trade-engine/framed/bybit"
	"github.com/souravmenon1999/trade-engine/framed/config"
	"github.com/souravmenon1999/trade-engine/framed/injective"
	"github.com/souravmenon1999/trade-engine/framed/processor/bybitorderbook"
	"github.com/souravmenon1999/trade-engine/framed/types"
	bybitws "github.com/souravmenon1999/trade-engine/framed/websockets/bybitws"
)

func main() {
	configPath := flag.String("config", "yamls/config.yaml", "Path to config file")
	flag.Parse()

	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	bybitOrderbookClient := bybitws.NewBybitExchangeClient(cfg.BybitOrderbook.WSUrl, "", "")
	if err := bybitOrderbookClient.Connect(); err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Bybit orderbook WebSocket")
	}
	bybitOrderbookClient.StartReading()
	log.Info().Msg("Bybit orderbook client connection established successfully")

	bybitTradeClient, err := bybit.InitTradeClient(
		cfg.BybitExchangeClient.WSUrl,
		cfg.BybitExchangeClient.APIKey,
		cfg.BybitExchangeClient.APISecret,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize Bybit trading client")
	}
	log.Info().Msg("Bybit trading client connection established successfully")

	bybitUpdatesClient, err := bybit.InitUpdatesClient(
		cfg.BybitExchangeClient.WSUrl,
		cfg.BybitExchangeClient.APIKey,
		cfg.BybitExchangeClient.APISecret,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize Bybit updates client")
	}
	log.Info().Msg("Bybit updates client connection established successfully")

	injectiveTradeClient, err := injective.InitTradeClient(
		cfg.InjectiveExchange.NetworkName,
		cfg.InjectiveExchange.Lb,
		cfg.InjectiveExchange.PrivKey,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize Injective trade client")
	}
	log.Info().Msg("Injective trade client connection established successfully")

	// Commented out unused variable
	// injectiveUpdatesClient := injective.InitUpdatesClient(injectiveTradeClient.GetExchangeClient())
	// log.Info().Msg("Injective updates client initialized successfully")

	processedDataChannel := make(chan *types.OrderBookWithVWAP, 10)

	instrument := &types.Instrument{
		BaseCurrency:  cfg.BybitOrderbook.BaseCurrency,
		QuoteCurrency: cfg.BybitOrderbook.QuoteCurrency,
		MinLotSize:    types.Quantity(100000),
		ContractType:  "Perpetual",
	}

	vwapProcessor := bybitorderbook.NewBybitVWAPProcessor(bybitOrderbookClient.OrderBookCh, processedDataChannel, cfg.BybitOrderbook.Symbol, instrument)
	vwapProcessor.StartProcessing()

	orderBookTopic := "orderbook.50." + cfg.BybitOrderbook.Symbol
	if err := bybitOrderbookClient.Subscribe(orderBookTopic); err != nil {
		log.Fatal().Err(err).Msgf("Failed to subscribe to Bybit orderbook for %s", cfg.BybitOrderbook.Symbol)
	}

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for data := range processedDataChannel {
			ob := data.OrderBook
			vwap := data.VWAP
			log.Info().Int64("vwap", int64(vwap)).Msg("Calculated VWAP")
			_ = ob
		}
	}()

	defer bybitOrderbookClient.Close()
	defer bybitTradeClient.Close()
	defer bybitUpdatesClient.Close()

	<-stopCh
	log.Info().Msg("Shutting down...")
}