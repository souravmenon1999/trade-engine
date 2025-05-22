package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	bybit "github.com/souravmenon1999/trade-engine/framed/exchange/bybit"
	"github.com/souravmenon1999/trade-engine/framed/config"
	"github.com/souravmenon1999/trade-engine/framed/exchange/injective"
	"github.com/souravmenon1999/trade-engine/framed/processor/bybitorderbook"
	"github.com/souravmenon1999/trade-engine/framed/types"
)

func main() {
	configPath := flag.String("config", "yamls/config.yaml", "Path to config file")
	flag.Parse()

	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	instrument := &types.Instrument{
		BaseCurrency:  cfg.BybitOrderbook.BaseCurrency,
		QuoteCurrency: cfg.BybitOrderbook.QuoteCurrency,
		MinLotSize:    types.NewQuantity(100000),
		ContractType:  "Perpetual",
	}

	vwapProcessor := bybitorderbook.NewBybitVWAPProcessor(
		func(data *types.OrderBookWithVWAP) {
			ob := data.OrderBook
			vwap := data.VWAP
			log.Info().Int64("vwap", vwap.Load()).Msg("Calculated VWAP")
			_ = ob
		},
		cfg.BybitOrderbook.Symbol,
		instrument,
	)

	bybitClient := bybit.NewBybitClient(
		cfg.BybitOrderbook.WSUrl,
		cfg.BybitExchangeClient.APIKey,
		cfg.BybitExchangeClient.APISecret,
	)
	if err := bybitClient.Connect(); err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Bybit WebSocket")
	}

	orderBookTopic := "orderbook.50." + cfg.BybitOrderbook.Symbol
	if err := bybitClient.Subscribe(orderBookTopic, vwapProcessor.ProcessAndApplyMessage); err != nil {
		log.Fatal().Err(err).Msgf("Failed to subscribe to %s", orderBookTopic)
	}


	// Callback for order updates
	orderUpdateCallback := func(data []byte) {
		log.Info().Msgf("Received Injective order history update: %s", string(data))
	}

	// Initialize Injective client
	// Initialize client WITH callback in constructor
injectiveClient, err := injective.NewInjectiveClient(
    cfg.InjectiveExchange.NetworkName,
    cfg.InjectiveExchange.Lb,
    cfg.InjectiveExchange.PrivKey,
    cfg.InjectiveExchange.MarketId,
    cfg.InjectiveExchange.SubaccountId,
    orderUpdateCallback,
)
if err != nil {
    log.Fatal().Err(err).Msg("Failed to initialize Injective client")
}
log.Info().
    Str("market_id", cfg.InjectiveExchange.MarketId).
    Msg("Injective client operational - listening for order updates")

// Add empty struct usage (safely consumes the variable)
_ = injectiveClient.GetSenderAddress() // No-op but uses the client

	bybitClient.StartReading()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	defer bybitClient.Close()

	<-stopCh
	log.Info().Msg("Shutting down...")
}