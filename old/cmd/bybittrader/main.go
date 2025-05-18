package main

import (
    "context"
    "flag"
    "log/slog"
    "os"
    "os/signal"
    "syscall"

    "github.com/spf13/viper"
    "github.com/souravmenon1999/trade-engine/internal/config"
    "github.com/souravmenon1999/trade-engine/internal/exchange/bybit"
    "github.com/souravmenon1999/trade-engine/internal/logging"
    "github.com/souravmenon1999/trade-engine/internal/processor"
    "github.com/souravmenon1999/trade-engine/internal/strategy/bybittrader"
)

// TempBybitConfig for YAML parsing to capture hedge_mode
type TempBybitConfig struct {
    config.BybitConfig
    TradeWSURL  string `mapstructure:"trade_ws_url"`
    StatusWSURL string `mapstructure:"status_ws_url"`
    HedgeMode   bool   `mapstructure:"hedge_mode"`
}

func main() {
    configPath := flag.String("config", "configs/bybittrader_config.yaml", "path to config file")
    flag.Parse()

    // Initialize logger with default level
    logging.InitLogger("debug")
    logger := slog.Default()
    slog.SetDefault(logger)

    // Load config using viper
    viper.SetConfigFile(*configPath)
    viper.SetConfigType("yaml")
    if err := viper.ReadInConfig(); err != nil {
        logger.Error("Failed to read config file", "error", err)
        os.Exit(1)
    }

    var cfg config.Config
    if err := viper.Unmarshal(&cfg); err != nil {
        logger.Error("Failed to unmarshal config", "error", err)
        os.Exit(1)
    }

    // Parse extended Bybit config
    var tempBybit TempBybitConfig
    if err := viper.UnmarshalKey("bybit", &tempBybit); err != nil {
        logger.Error("Failed to unmarshal bybit config", "error", err)
        os.Exit(1)
    }

    // Override logger level with config
    if cfg.Log.Level != "" {
        logging.InitLogger(cfg.Log.Level)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Create BybitExtendedConfig for trading client using cfg.Bybit for core fields
    bybitExtendedCfg := &bybit.BybitExtendedConfig{
        BybitConfig: cfg.Bybit,
        TradeWSURL:  tempBybit.TradeWSURL,
        StatusWSURL: tempBybit.StatusWSURL,
        HedgeMode:   tempBybit.HedgeMode,
    }

    // Log config values for debugging
    logger.Debug("BybitExtendedConfig",
        "Symbol", bybitExtendedCfg.Symbol,
        "TradingURL", bybitExtendedCfg.TradingURL,
        "Category", bybitExtendedCfg.Category,
        "APIKey", bybitExtendedCfg.APIKey,
        "APISecret", bybitExtendedCfg.APISecret,
        "TradeWSURL", bybitExtendedCfg.TradeWSURL,
        "StatusWSURL", bybitExtendedCfg.StatusWSURL,
        "HedgeMode", bybitExtendedCfg.HedgeMode)

    bybitDataClient := bybit.NewClient(ctx, &cfg.Bybit)
    defer bybitDataClient.Close()

    bybitTradingClient, err := bybit.NewTradingClient(ctx, bybitExtendedCfg)
    if err != nil {
        logger.Error("Failed to initialize Bybit trading client", "error", err)
        os.Exit(1)
    }
    defer bybitTradingClient.Close()

    priceProcessor := processor.NewPriceProcessor(ctx, &cfg.Processor, &cfg.Order)
    strategy := bybittrader.NewStrategy(ctx, &cfg, bybitDataClient, bybitTradingClient, priceProcessor)

    go strategy.Run()

    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    <-sigChan

    logger.Info("Received shutdown signal")
    if err := strategy.Close(); err != nil {
        logger.Error("Failed to close strategy", "error", err)
    }
}