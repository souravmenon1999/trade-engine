// cmd/bybittrader/main.go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/souravmenon1999/trade-engine/internal/config"
	"github.com/souravmenon1999/trade-engine/internal/exchange/bybit" // Import concrete Bybit clients
	"github.com/souravmenon1999/trade-engine/internal/logging"
	"github.com/souravmenon1999/trade-engine/internal/processor"
	"github.com/souravmenon1999/trade-engine/internal/strategy/bybittrader" // Import Bybit Trader strategy
)

func main() {
	// Load configuration
	// This main assumes a config.yaml file is present and structured correctly.
	cfg, err := config.LoadConfig("configs/config.yaml")
	if err != nil {
		fmt.Printf("Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logging
	logging.InitLogger(cfg.Log.Level)
	logger := logging.GetLogger()

	logger.Info("Bybit Trader system starting...")
	logger.Debug("Configuration loaded", "config", cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure application context is cancelled on exit

	// --- Initialize Bybit Data Client ---
	// This client provides the orderbook data feed from Bybit.
	// Use bybit.NewClient from client.go
	bybitDataClient, err := bybit.NewClient(ctx, &cfg.Bybit) // Use bybit.NewClient from client.go
	if err != nil {
		logger.Error("Failed to initialize Bybit data client", "error", err)
		os.Exit(1) // Data feed is essential
	} else {
		logger.Info("Bybit data client initialized.")
	}

	// Start the orderbook subscription (price data source)
	err = bybitDataClient.SubscribeOrderbook(ctx, cfg.Bybit.Symbol)
	if err != nil {
		logger.Error("Failed to start Bybit orderbook subscription", "error", err)
		// If the price feed fails, the strategy cannot operate.
		// Decide if this should be a fatal error or if reconnect logic is sufficient.
		// For now, log error and let the app continue, but trading won't happen without data.
	} else {
		logger.Info("Bybit orderbook subscription initiated.")
	}

	// --- Initialize Bybit Trading Client ---
	// This client handles trading operations via REST API.
	// We need to pass the trading category here since it's not in config.BybitConfig
	// Assuming the category is hardcoded or available elsewhere (e.g., env var).
	// For this example, let's hardcode "linear" or get it from a non-config source.
	// A better approach would be to add it to config.BybitConfig if possible.
	// Let's assume we can get it from an environment variable or hardcode for now.
	// Example: tradingCategory := os.Getenv("BYBIT_TRADING_CATEGORY")
	// If not using env var, hardcode:
	tradingCategory := "linear" // <-- Hardcoded trading category, replace as needed

	// Use bybit.NewTradingClient
	bybitTradingClient, err := bybit.NewTradingClient(ctx, &cfg.Bybit, tradingCategory) // Use bybit.NewTradingClient
	if err != nil {
		logger.Error("Failed to initialize Bybit trading client", "error", err)
		os.Exit(1) // Trading is essential for this bot
	} else {
		logger.Info("Bybit trading client initialized.")
	}

	// --- Initialize Price Processor ---
	// It needs config for spread, cooldown, threshold.
	// Note: processor.ProcessOrderbook will return orders with Exchange: types.ExchangeInjective
	// due to the constraint on processor.go. The BybitTradingClient will ignore this.
	priceProcessor := processor.NewPriceProcessor(ctx, &cfg.Processor, &cfg.Order)
	logger.Info("Price Processor initialized.")

	// --- Initialize and Run Bybit Trader Strategy ---
	// Pass both the data client and the trading client.
	strategy := bybittrader.NewStrategy(ctx, cfg, bybitDataClient, bybitTradingClient, priceProcessor) // Pass both clients

	// Start the strategy's main execution loop in a goroutine
	go strategy.Run()
	logger.Info("Bybit Trader Strategy started in background.")

	// --- Keep the application running ---
	// Wait for interrupt signal (Ctrl+C or SIGTERM) to gracefully shut down.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	<-stop // Block until a signal is received

	logger.Info("Shutting down gracefully...")

	// --- Clean up resources ---
	// Cancel the root context. This signals cancellation to all components
	// created with derived contexts (Strategy, Clients, Processor).
	cancel()

	// Give goroutines time to stop after context cancellation.
	// Use a WaitGroup for more robust shutdown, but a sleep is simpler for demonstration.
	time.Sleep(time.Millisecond * 500)

	// Close components explicitly in reverse order of dependency where applicable.
	// Close Strategy first (it uses clients/processor).
	err = strategy.Close()
	if err != nil {
		logger.Error("Error during Strategy shutdown", "error", err)
	} else {
		logger.Info("Strategy shut down successfully.")
	}

	// Close Price Processor (no real resources, but good practice).
	err = priceProcessor.Close()
	if err != nil {
		logger.Error("Error during Price Processor shutdown", "error", err)
	} else {
		logger.Info("Price Processor shut down successfully.")
	}

	// Close Bybit Trading Client first (it might have open connections).
	// Call close on the concrete trading client type
	err = bybitTradingClient.Close()
	if err != nil {
		logger.Error("Error during Bybit trading client shutdown", "error", err)
	} else {
		logger.Info("Bybit trading client shut down successfully.")
	}

	// Close Bybit Data Client last.
	// Call close on the concrete data client type
	err = bybitDataClient.Close()
	if err != nil {
		logger.Error("Error during Bybit data client shutdown", "error", err)
	} else {
		logger.Info("Bybit data client shut down successfully.")
	}

	logger.Info("Bybit Trader system stopped.")
}