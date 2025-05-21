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

	// Example: Subscribe to order updates (can be extended for other topics)
	// bybitClient.Subscribe("order", func(data []byte) {
	// 	log.Info().Msgf("Received order update: %s", string(data))
	// })

	injectiveTradeClient, err := injective.InitTradeClient(
		cfg.InjectiveExchange.NetworkName,
		cfg.InjectiveExchange.Lb,
		cfg.InjectiveExchange.PrivKey,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize Injective trade client")
	}
	_ = injectiveTradeClient

	bybitClient.StartReading()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	defer bybitClient.Close()

	<-stopCh
	log.Info().Msg("Shutting down...")
}