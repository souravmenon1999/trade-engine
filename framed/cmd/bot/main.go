package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/souravmenon1999/trade-engine/framed/exchange/bybit"
	"github.com/souravmenon1999/trade-engine/framed/config"
	"github.com/souravmenon1999/trade-engine/framed/exchange/injective"
	"github.com/souravmenon1999/trade-engine/framed/processor/bybitorderbook"
	"github.com/souravmenon1999/trade-engine/framed/types"
	bybitws "github.com/souravmenon1999/trade-engine/framed/exchange/net/websockets/bybitws"
)

func main() {
	configPath := flag.String("config", "yamls/config.yaml", "Path to config file")
	flag.Parse()

	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	// Define the instrument
	instrument := &types.Instrument{
		BaseCurrency:  cfg.BybitOrderbook.BaseCurrency,
		QuoteCurrency: cfg.BybitOrderbook.QuoteCurrency,
		MinLotSize:    types.NewQuantity(100000),
		ContractType:  "Perpetual",
	}

	// Initialize Bybit VWAP processor with a callback for processed data
	vwapProcessor := bybitorderbook.NewBybitVWAPProcessor(
		func(data *types.OrderBookWithVWAP) {
			ob := data.OrderBook
			vwap := data.VWAP
			log.Info().Int64("vwap", vwap.Load()).Msg("Calculated VWAP")
			_ = ob // Placeholder; add further processing if needed
		},
		cfg.BybitOrderbook.Symbol,
		instrument,
	)

	// Initialize Bybit orderbook client with the processor's callback
	bybitOrderbookClient := bybitws.NewBybitOrderBookWSClient(cfg.BybitOrderbook.WSUrl, vwapProcessor.ProcessAndApplyMessage)
	if err := bybitOrderbookClient.Connect(); err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Bybit orderbook WebSocket")
	}
	log.Info().Msg("Bybit orderbook client connection established successfully")

	// Subscribe to orderbook
	orderBookTopic := "orderbook.50." + cfg.BybitOrderbook.Symbol
	if err := bybitOrderbookClient.Subscribe(orderBookTopic); err != nil {
		log.Fatal().Err(err).Msgf("Failed to subscribe to Bybit orderbook for %s", cfg.BybitOrderbook.Symbol)
	}

	// Initialize Bybit trading client
	bybitTradeClient, err := bybit.InitTradeClient(
		cfg.BybitExchangeClient.WSUrl,
		cfg.BybitExchangeClient.APIKey,
		cfg.BybitExchangeClient.APISecret,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize Bybit trading client")
	}
	log.Info().Msg("Bybit trading client connection established successfully")

	// Initialize Bybit updates client
	bybitUpdatesClient, err := bybit.InitUpdatesClient(
		cfg.BybitExchangeClient.WSUrl,
		cfg.BybitExchangeClient.APIKey,
		cfg.BybitExchangeClient.APISecret,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize Bybit updates client")
	}
	log.Info().Msg("Bybit updates client connection established successfully")

	// Initialize Injective trade client
	injectiveTradeClient, err := injective.InitTradeClient(
		cfg.InjectiveExchange.NetworkName,
		cfg.InjectiveExchange.Lb,
		cfg.InjectiveExchange.PrivKey,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize Injective trade client")
	}
	_ = injectiveTradeClient // Temporary usage; replace with actual logic
	log.Info().Msg("Injective trade client connection established successfully")

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	defer bybitOrderbookClient.Close()
	defer bybitTradeClient.Close()
	defer bybitUpdatesClient.Close()

	// Start reading WebSocket messages in a goroutine
	go bybitOrderbookClient.StartReading()

	<-stopCh
	log.Info().Msg("Shutting down...")
}