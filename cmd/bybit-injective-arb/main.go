// cmd/bybit-injective-arb/main.go - Updated main
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
	"github.com/souravmenon1999/trade-engine/internal/config"
	"github.com/souravmenon1999/trade-engine/internal/exchange/bybit" // Import concrete type for NewClient
	"github.com/souravmenon1999/trade-engine/internal/exchange/injective" // Import concrete type for NewClient
	"github.com/souravmenon1999/trade-engine/internal/logging"
	"github.com/souravmenon1999/trade-engine/internal/processor"
	"github.com/souravmenon1999/trade-engine/internal/strategy/bybitinjective" // Import strategy
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig("configs/config.yaml")
	if err != nil {
		fmt.Printf("Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logging
	logging.InitLogger(cfg.Log.Level)
	logger := logging.GetLogger()

	logger.Info("Trading system starting...")
	logger.Debug("Configuration loaded", "config", cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure application context is cancelled on exit

	// --- Initialize Exchange Clients ---
	// Initialize Bybit Client (M2) - cast to interface when passing to strategy
	bybitClient := bybit.NewClient(ctx, &cfg.Bybit)
	err = bybitClient.SubscribeOrderbook(ctx, cfg.Bybit.Symbol)
	if err != nil {
		logger.Error("Failed to start Bybit orderbook subscription", "error", err)
		// Decide if this is fatal
	} else {
        logger.Info("Bybit orderbook subscription initiated.")
    }

	// Initialize Injective Client (M3 - now functional) - cast to interface when passing to strategy
	// Ensure private key is handled securely (e.g., from env var) in config loading or here
	// For demonstration, assuming cfg.Injective.PrivateKey holds the value
	injectiveClient, err := injective.NewClient(ctx, &cfg.Injective)
	if err != nil {
		logger.Error("Failed to initialize Injective client", "error", err)
		os.Exit(1) // Injective trading is core functionality
	} else {
		logger.Info("Injective client initialized.")
	}

	// --- Initialize Price Processor (M4) ---
	priceProcessor := processor.NewPriceProcessor(ctx, &cfg.Processor, &cfg.Order)
	logger.Info("Price Processor initialized.")

	// --- Initialize and Run Strategy (M5) ---
	// Pass clients as interfaces
	strategy := bybitinjective.NewStrategy(ctx, cfg, bybitClient, injectiveClient, priceProcessor)

	// Start the strategy's main execution loop in a goroutine
	go strategy.Run()
	logger.Info("Bybit-Injective Strategy started in background.")


	// --- Keep the application running ---
	// Wait for interrupt signal to gracefully shut down
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	<-stop // Block until a signal is received

	logger.Info("Shutting down gracefully...")

	// --- Clean up resources ---
	// Cancel the root context.
	cancel()

	// Give goroutines time to stop after context cancellation.
	// A WaitGroup is more robust, but a sleep is simpler for demonstration.
    time.Sleep(time.Millisecond * 500)

	// Close components explicitly
	// Close Strategy first as it uses clients and processor
	err = strategy.Close()
	if err != nil {
		logger.Error("Error during Strategy shutdown", "error", err)
	} else {
		logger.Info("Strategy shut down successfully.")
	}

	// Close Price Processor (no real resources, but good practice)
	err = priceProcessor.Close()
	if err != nil {
		logger.Error("Error during Price Processor shutdown", "error", err)
	} else {
		logger.Info("Price Processor shut down successfully.")
	}

	// Close Clients
	err = injectiveClient.Close()
	if err != nil {
		logger.Error("Error during Injective client shutdown", "error", err)
	} else {
		logger.Info("Injective client shut down successfully.")
	}

	err = bybitClient.Close()
	if err != nil {
		logger.Error("Error during Bybit client shutdown", "error", err)
	} else {
		logger.Info("Bybit client shut down successfully.")
	}

	logger.Info("Trading system stopped.")
}