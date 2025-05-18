package main

import (
    "context"
    "flag"
    "log/slog"
    "os"
    "os/signal"
    "syscall"

    "github.com/souravmenon1999/trade-engine/internal/config"
    "github.com/souravmenon1999/trade-engine/internal/exchange/bybit"
    "github.com/souravmenon1999/trade-engine/internal/exchange/injective"
    "github.com/souravmenon1999/trade-engine/internal/logging"
    "github.com/souravmenon1999/trade-engine/internal/processor"
    "github.com/souravmenon1999/trade-engine/internal/strategy/bybitinjective"
)

func main() {
    configPath := flag.String("config", "configs/config.yaml", "path to config file")
    flag.Parse()

    // Initialize logger
    logging.InitLogger("debug")
    logger := slog.Default()
    slog.SetDefault(logger)
    logger.Info("Starting Bybit-Injective arbitrage bot", "config_path", *configPath)

    // Load config
    logger.Debug("Loading configuration")
    cfg, err := config.LoadConfig(*configPath)
    if err != nil {
        logger.Error("Failed to load config", "error", err)
        os.Exit(1)
    }
    logger.Info("Configuration loaded successfully", 
        "bybit_symbol", cfg.Bybit.Symbol,
        "injective_market_id", cfg.Injective.MarketID,
        "order_quantity", cfg.Order.Quantity,
        "spread_pct", cfg.Order.SpreadPct)

    // Override logger level with config
    if cfg.Log.Level != "" {
        logger.Debug("Updating logger level", "level", cfg.Log.Level)
        logging.InitLogger(cfg.Log.Level)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    logger.Debug("Context initialized")

    // Initialize Bybit data client
    logger.Debug("Initializing Bybit data client", "ws_url", cfg.Bybit.WSURL)
    bybitDataClient := bybit.NewClient(ctx, &cfg.Bybit)
    defer bybitDataClient.Close()
    logger.Info("Bybit data client initialized", "symbol", cfg.Bybit.Symbol)

    // Subscribe to Bybit orderbook
    logger.Debug("Subscribing to Bybit orderbook", "symbol", cfg.Bybit.Symbol)
    if err := bybitDataClient.SubscribeOrderbook(ctx, cfg.Bybit.Symbol); err != nil {
        logger.Error("Failed to subscribe to Bybit orderbook", "error", err)
        os.Exit(1)
    }
    logger.Info("Subscribed to Bybit orderbook", "symbol", cfg.Bybit.Symbol)

    // Initialize Injective trading client
    logger.Debug("Initializing Injective trading client", "market_id", cfg.Injective.MarketID)
    injectiveClient, err := injective.NewClient(ctx, &cfg.Injective)
    if err != nil {
        logger.Error("Failed to initialize Injective client", "error", err)
        os.Exit(1)
    }
    defer injectiveClient.Close()
    logger.Info("Injective trading client initialized", "market_id", cfg.Injective.MarketID)

    // Initialize price processor
    logger.Debug("Initializing price processor", 
        "requote_cooldown", cfg.Processor.RequoteCooldown,
        "requote_threshold", cfg.Processor.RequoteThreshold)
    priceProcessor := processor.NewPriceProcessor(ctx, &cfg.Processor, &cfg.Order)
    logger.Info("Price processor initialized")

    // Initialize strategy
    logger.Debug("Initializing Bybit-Injective strategy")
    strategy := bybitinjective.NewStrategy(ctx, cfg, bybitDataClient, injectiveClient, priceProcessor)
    logger.Info("Strategy initialized")

    // Run strategy
    logger.Info("Starting strategy")
    go strategy.Run()

    // Handle shutdown
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    logger.Debug("Waiting for shutdown signal")
    <-sigChan

    logger.Info("Received shutdown signal")
    logger.Debug("Closing strategy")
    if err := strategy.Close(); err != nil {
        logger.Error("Failed to close strategy", "error", err)
    }
    logger.Info("Bot shutdown complete")
}