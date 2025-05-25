package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	injective "github.com/souravmenon1999/trade-engine/framed/exchange/injective"
	"github.com/souravmenon1999/trade-engine/framed/config"
	// "github.com/shopspring/decimal"
)

func main() {
	configPath := flag.String("config", "yamls/config.yaml", "Path to config file")
	flag.Parse()

	// Set up zerolog
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	// Load config
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}
	log.Info().Msg("Config loaded successfully")

	// Initialize Injective client
	client, err := injective.NewInjectiveClient(
		cfg.InjectiveExchange.NetworkName,
		cfg.InjectiveExchange.Lb,
		cfg.InjectiveExchange.PrivKey,
		cfg.InjectiveExchange.MarketId,
		cfg.InjectiveExchange.SubaccountId,
		nil, // No callback for now
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize Injective client")
	}

	// Test order parameters
	// side := "buy" // Can change to "sell" for testing
	// price, err := decimal.NewFromString("2000") // Example price
	// if err != nil {
	// 	log.Fatal().Err(err).Msg("Invalid price")
	// }
	// quantity, err := decimal.NewFromString("0.01") // Small quantity for safety
	// if err != nil {
	// 	log.Fatal().Err(err).Msg("Invalid quantity")
	// }
	// leverage, err := decimal.NewFromString("1") // Example leverage
	// if err != nil {
	// 	log.Fatal().Err(err).Msg("Invalid leverage")
	// }

	// Send the order
	// log.Info().Msg("Sending limit order")
	// err = client.SendOrder(
	// 	cfg.InjectiveExchange.MarketId,
	// 	cfg.InjectiveExchange.SubaccountId,
	// 	side,
	// 	price,
	// 	quantity,
	// 	leverage,
	// )
	// if err != nil {
	// 	log.Fatal().Err(err).Msg("Failed to send order")
	// }
	// log.Info().Msg("Order sent successfully")

	orderHash := "0x0288ba975e6ae08a67641f7268826a22f12a7b81bb480ef4c4cfaacfd50a6bd0"

	err = client.CancelOrder(
    cfg.InjectiveExchange.MarketId,
  // Explicit subaccount ID
    orderHash,                          // Returned hash from SendOrder
)

	// Handle shutdown
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)
	<-stopCh
	log.Info().Msg("Shutting down")
}