package main

import (
    "flag"
    "os"
    "os/signal"
    "syscall"

    "github.com/rs/zerolog"
    "github.com/rs/zerolog/log"
    "github.com/souravmenon1999/trade-engine/framed/exchange/bybit"
    "github.com/souravmenon1999/trade-engine/framed/exchange/injective"
    "github.com/souravmenon1999/trade-engine/framed/config"      // Adjust to your config package path
    "github.com/souravmenon1999/trade-engine/framed/ordermanager" // Adjust to your ordermanager package path
    "github.com/souravmenon1999/trade-engine/framed/strategy"
	"github.com/souravmenon1999/trade-engine/framed/types"    // Adjust to your strategy package path
)

func main() {
    // Parse command-line flags
    configPath := flag.String("config", "yamls/config.yaml", "Path to config file")
    flag.Parse()

    // Set up logging
    log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

    // Load configuration
    cfg, err := config.LoadConfig(*configPath)
    if err != nil {
        log.Fatal().Err(err).Msg("Failed to load config")
    }
    log.Info().Msgf("Config loaded: %+v", cfg)

    // Initialize OrderManager
    om := ordermanager.NewOrderManager()

  

    bybitClient := bybit.NewBybitClient(cfg)
    om.RegisterExchange(types.ExchangeIDBybit, bybitClient)
    log.Info().Msg("Bybit exchange registered")

     // Register Injective exchange
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
    om.RegisterExchange(types.ExchangeIDInjective, injectiveClient)
    log.Info().Msg("Injective exchange registered")

  

    // Initialize strategy with OrderManager and Config
    strat := strategy.NewArbitrageStrategy(om, cfg)
    go strat.Start()

    // Handle shutdown
    stopCh := make(chan os.Signal, 1)
    signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

   defer bybitClient.Close()
    defer injectiveClient.Close()

    <-stopCh
    log.Info().Msg("Shutting down...")
}