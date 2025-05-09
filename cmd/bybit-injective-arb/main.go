// cmd/bybit-injective-arb/main.go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
	"github.com/souravmenon1999/trade-engine/internal/config"
	"github.com/souravmenon1999/trade-engine/internal/exchange/bybit"
	"github.com/souravmenon1999/trade-engine/internal/exchange/injective"
	"github.com/souravmenon1999/trade-engine/internal/logging"
	"github.com/souravmenon1999/trade-engine/internal/processor"
	"github.com/souravmenon1999/trade-engine/internal/strategy/bybitinjective" // Import the strategy
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

	// Initialize Bybit Client 
	bybitClient := bybit.NewClient(ctx, &cfg.Bybit)
	err = bybitClient.SubscribeOrderbook(ctx, cfg.Bybit.Symbol)
	if err != nil {
		logger.Error("Failed to start Bybit orderbook subscription", "error", err)
		// App can technically run without Bybit data, but trading won't occur.
		// Decide if this is a fatal error for your specific bot.
	} else {
        logger.Info("Bybit orderbook subscription initiated.")
    }


	// Initialize Injective Client 
	injectiveClient, err := injective.NewClient(ctx, &cfg.Injective)
	if err != nil {
		logger.Error("Failed to initialize Injective client", "error", err)
		os.Exit(1) // Injective trading is core functionality
	} else {
		logger.Info("Injective client initialized.")
	}

	// Initialize Price Processor 
	priceProcessor := processor.NewPriceProcessor(ctx, &cfg.Processor, &cfg.Order)
	logger.Info("Price Processor initialized.")

	// Initialize and Run Strategy 
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
	// Cancel the root context. This signals cancellation to all components
	// created with derived contexts (Strategy, Clients, Processor).
	cancel()

	// Give goroutines a moment to respond to context cancellation and finish
	// their current tasks (like processing a final message or logging).
    // The strategy's Run loop and client read loops should stop shortly after context cancellation.
    // We might need to wait for goroutines to finish explicitly with a sync.WaitGroup
    // in a more robust shutdown, but for now, a short sleep is a simple way
    // to allow cleanup logging to occur.
    time.Sleep(time.Millisecond * 500) // Adjust as needed

	// Close components explicitly if they have specific Close methods (they should)
	// These Close methods should ideally handle nil checks and be safe to call even if
	// context cancellation has already started cleanup.
	err = strategy.Close()
	if err != nil {
		logger.Error("Error during Strategy shutdown", "error", err)
	} else {
		logger.Info("Strategy shut down successfully.")
	}

	err = priceProcessor.Close()
	if err != nil {
		logger.Error("Error during Price Processor shutdown", "error", err)
	} else {
		logger.Info("Price Processor shut down successfully.")
	}

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