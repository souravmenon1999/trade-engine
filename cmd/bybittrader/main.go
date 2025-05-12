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
	cfg, err := config.LoadConfig("configs/bybittrader_config.yaml") // Load the Bybit Trader config
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
	// FIX: bybit.NewClient returns only ONE value (*Client), not two.
	bybitDataClient := bybit.NewClient(ctx, &cfg.Bybit) // Assign to one variable
	// We should ideally check for initialization errors, but based on your client.go
	// definition, errors are returned by methods like SubscribeOrderbook.
	logger.Info("Bybit data client initialized.")

	// Start the orderbook subscription (price data source)
	// Declare 'err' here for subsequent assignments if not already declared
	// (If you had other err declarations above, remove 'var' or ':=' as appropriate)
	// Since we used ':=' for cfg, err above, we reuse err here:
	err = bybitDataClient.SubscribeOrderbook(ctx, cfg.Bybit.Symbol) // Assign error here
	if err != nil {
		logger.Error("Failed to start Bybit orderbook subscription", "error", err)
		// If the price feed fails, the strategy cannot operate.
		// Decide if this is a fatal error or if reconnect logic is sufficient.
		// For now, exit if subscription fails initially.
		os.Exit(1) // Exit if subscription fails
	} else {
		logger.Info("Bybit orderbook subscription initiated.")
	}

	// --- Initialize Bybit Trading Client ---
	// This client handles trading operations via REST API.
	// It gets necessary config (API keys, URL, Category) from the loaded config.
	// Remove the separate tradingCategory argument from the call if you put it back.
	// Ensure NewTradingClient also returns (*TradingClient, error) if you haven't already fixed that.
	bybitTradingClient, err := bybit.NewTradingClient(ctx, &cfg.Bybit) // This call should return 2 values
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
