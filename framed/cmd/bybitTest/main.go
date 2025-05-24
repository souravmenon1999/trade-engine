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
        Side:     "Buy",
        Quantity: types.NewQuantity(10000), // 0.0001 ETH (assuming 10^8 precision)
        Price:    types.NewPrice(1999),    // Hardcoded price: $2000
    }

    // Send the order
    log.Info().Msg("Attempting to send order")
    if err := bybitClient.SendOrder(order); err != nil {
        log.Fatal().Err(err).Msg("Failed to send order")
    }
    log.Info().Msg("Order sent successfully, waiting for confirmation via WebSocket")

    // Wait briefly to ensure WebSocket processes the order creation response
   
    // Cancel the order
    // cancelOrderId := "ddssdssd" // Replace with actual ID
    // cancelSymbol := cfg.BybitOrderbook.BaseCurrency + cfg.BybitOrderbook.QuoteCurrency // e.g., "ETHUSDT"
    // log.Info().Str("orderId", cancelOrderId).Str("symbol", cancelSymbol).Msg("Attempting to cancel order")
    // if cancelErr := bybitClient.CancelOrder(cancelSymbol, cancelOrderId); cancelErr != nil {
    //     log.Fatal().Err(cancelErr).Msg("Failed to cancel order")
    // }
    // log.Info().Msg("Cancel order request sent, waiting for confirmation via WebSocket")

    // Wait briefly to ensure WebSocket processes the cancellation response
   

    //Amend the order
    amendOrderId := "7e6de2ce-941b-4c36-8b1a-ee5d2335e34b" // Same ID as cancellation
    amendSymbol := cfg.BybitOrderbook.BaseCurrency + cfg.BybitOrderbook.QuoteCurrency // e.g., "ETHUSDT"
    newPrice := int64(1900)                                  // New price: $2100
    newQty := int64(15000)                                   // New quantity: 0.00015 ETH
    log.Info().Str("orderId", amendOrderId).Str("symbol", amendSymbol).Int64("newPrice", newPrice).Int64("newQty", newQty).Msg("Attempting to amend order")
    if amendErr := bybitClient.AmendOrder(amendSymbol, amendOrderId, newPrice, newQty); amendErr != nil {
        log.Fatal().Err(amendErr).Msg("Failed to amend order")
    }
    log.Info().Msg("Amend order request sent, waiting for confirmation via WebSocket")

    // Wait for shutdown signal to keep the program running and receive WebSocket updates
    stopCh := make(chan os.Signal, 1)
    signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

    defer bybitClient.Close()

    <-stopCh
    log.Info().Msg("Shutting down...")
}