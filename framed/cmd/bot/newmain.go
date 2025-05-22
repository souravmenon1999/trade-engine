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
	"github.com/souravmenon1999/trade-engine/framed/strategy"
)

func main() {
	configPath := flag.String("config", "yamls/config.yaml", "Path to config file")
	flag.Parse()

	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	// Initialize Bybit client
	bybitClient := bybit.NewBybitClient(
		cfg.BybitOrderbook.WSUrl,
		cfg.BybitExchangeClient.APIKey,
		cfg.BybitExchangeClient.APISecret,
	)
	if err := bybitClient.Connect(); err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Bybit WebSocket")
	}

	// Callback for Injective order updates
	orderUpdateCallback := func(data []byte) {
		log.Info().Msgf("Received Injective order history update: %s", string(data))
	}

	// Initialize Injective client
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

	// Initialize strategy with clients and config
	strategy.InitStrategy(bybitClient, injectiveClient, cfg)

	// Start reading Bybit WebSocket messages
	bybitClient.StartReading()

	// Handle graceful shutdown
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	defer bybitClient.Close()

	<-stopCh
	log.Info().Msg("Shutting down...")
}