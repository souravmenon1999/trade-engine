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
    // "github.com/souravmenon1999/trade-engine/framed/processor/bybitorderbook"
    // "github.com/souravmenon1999/trade-engine/framed/types"
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

    // instrument := &types.Instrument{
    //     BaseCurrency:  cfg.BybitOrderbook.BaseCurrency,
    //     QuoteCurrency: cfg.BybitOrderbook.QuoteCurrency,
    //     MinLotSize:    types.NewQuantity(100000),
    //     ContractType:  "Perpetual",
    // }

    // Initialize BybitClient, which automatically connects, subscribes, and starts reading
    bybitClient := bybit.NewBybitClient(cfg)

    // Set up VWAP processor and assign callback after client initialization
    // vwapProcessor := bybitorderbook.NewBybitVWAPProcessor(
    //     func(data *types.OrderBookWithVWAP) {
    //         ob := data.OrderBook
    //         vwap := data.VWAP
    //         log.Info().Int64("vwap", vwap.Load()).Msg("Calculated VWAP")
    //         _ = ob
    //     },
    //     cfg.BybitOrderbook.Symbol,
    //     instrument,
    //     bybitClient.PublicWS(),
    // )

    // Subscribe to order book topic (callback set after client initialization)
    // orderBookTopic := "orderbook.50." + cfg.BybitOrderbook.Symbol
    // if err := bybitClient.SubscribePublic(orderBookTopic, vwapProcessor.ProcessAndApplyMessage); err != nil {
    //     log.Fatal().Err(err).Msgf("Failed to subscribe to %s", orderBookTopic)
    // }
    // log.Info().Str("topic", orderBookTopic).Msg("VWAP processor subscribed to order book topic")

    // Injective client setup (unchanged)
    // orderUpdateCallback := func(data []byte) {
    //     log.Info().Msgf("Received Injective order history update: %s", string(data))
    // }
    injectiveClient, err := injective.NewInjectiveClient(
        cfg.InjectiveExchange.NetworkName,
        cfg.InjectiveExchange.Lb,
        cfg.InjectiveExchange.PrivKey,
        cfg.InjectiveExchange.MarketId,
        cfg.InjectiveExchange.SubaccountId,
    
    )
    if err != nil {
        log.Fatal().Err(err).Msg("Failed to initialize Injective client")
    }
    log.Info().
        Str("market_id", cfg.InjectiveExchange.MarketId).
        Msg("Injective client operational - listening for order updates")
    _ = injectiveClient.GetSenderAddress()

    // Wait for shutdown signal
    stopCh := make(chan os.Signal, 1)
    signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

    defer bybitClient.Close()

    <-stopCh
    log.Info().Msg("Shutting down...")
}