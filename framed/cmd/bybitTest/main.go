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
    log.Info().Msgf("Config loaded: %+v", cfg)

    // Initialize BybitClient (automatically connects, subscribes, and starts reading)
    bybitClient := bybit.NewBybitClient(cfg)

    // Define a hardcoded order for testing (ETHUSDT perpetual market)
    order := &types.Order{
        Instrument: &types.Instrument{
            BaseCurrency:  cfg.BybitOrderbook.BaseCurrency,  // e.g., "ETH"
            QuoteCurrency: cfg.BybitOrderbook.QuoteCurrency, // e.g., "USDT"
            ContractType:  "Perpetual",
        },
        Side:     "Buy",               // Buy order
        Quantity: types.NewQuantity(100000), // Small quantity (e.g., 0.001 ETH, assuming 10^8 precision)
        Price:    types.NewPrice(2500000000), // Hardcoded price (e.g., $2500, assuming 10^8 precision)
    }

    // Send the order
    log.Info().Msg("Attempting to send order")
    if err := bybitClient.SendOrder(order); err != nil {
        log.Fatal().Err(err).Msg("Failed to send order")
    }
    log.Info().Msg("Order sent successfully, waiting for confirmation via WebSocket")

    // Wait for shutdown signal to keep the program running and receive WebSocket updates
    stopCh := make(chan os.Signal, 1)
    signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

    defer bybitClient.Close()

    <-stopCh
    log.Info().Msg("Shutting down...")
}