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
	"github.com/souravmenon1999/trade-engine/internal/logging"
	"github.com/souravmenon1999/trade-engine/internal/processor"
	"github.com/souravmenon1999/trade-engine/internal/strategy/bybittrader"
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig("configs/bybittrader_config.yaml")
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
	defer cancel()

	// Initialize Bybit Trading Client with Retry
	logger.Debug("Starting Bybit trading client initialization...")
	var bybitTradingClient *bybit.TradingClient
	const maxRetries = 5
	for attempt := 0; attempt <= maxRetries; attempt++ {
		logger.Info("Attempting to initialize Bybit trading client", "attempt", attempt+1)
		bybitTradingClient, err = bybit.NewTradingClient(ctx, &cfg.Bybit)
		if err == nil {
			logger.Info("Bybit trading client initialized successfully")
			break
		}

		logger.Error("Failed to initialize Bybit trading client", "error", err, "attempt", attempt+1)
		if attempt == maxRetries {
			logger.Error("Max retries reached, unable to initialize trading client")
		}

		// Exponential backoff: 1s, 2s, 4s, 8s, 16s
		delay := time.Second * time.Duration(1<<attempt)
		logger.Info("Retrying after delay", "delay", delay)
		time.Sleep(delay)
	}

	// Initialize Bybit data client
	logger.Debug("initializing data client")
	bybitDataClient := bybit.NewClient(ctx, &cfg.Bybit)
	logger.Debug("Bybit data client initialized.")

	// Start the orderbook subscription
	const maxSubRetries = 3
	for attempt := 0; attempt <= maxSubRetries; attempt++ {
		logger.Info("Attempting to subscribe to orderbook", "exchange", "bybit", "symbol", cfg.Bybit.Symbol, "attempt", attempt+1)
		err = bybitDataClient.SubscribeOrderbook(ctx, cfg.Bybit.Symbol)
		if err == nil {
			logger.Info("Bybit orderbook subscription initiated.")
			break
		}

		logger.Error("Failed to start Bybit orderbook subscription", "error", err, "attempt", attempt+1)
		if attempt == maxSubRetries {
			logger.Error("Max retries reached for orderbook subscription")
		}

		// Exponential backoff: 1s, 2s, 4s
		delay := time.Second * time.Duration(1<<attempt)
		logger.Info("Retrying subscription after delay", "delay", delay)
		time.Sleep(delay)
	}

	// Initialize Price Processor
	priceProcessor := processor.NewPriceProcessor(ctx, &cfg.Processor, &cfg.Order)
	logger.Info("Price Processor initialized.")

	// Initialize and Run Bybit Trader Strategy
	strategy := bybittrader.NewStrategy(ctx, cfg, bybitDataClient, bybitTradingClient, priceProcessor)

	// Start the strategy in a goroutine
	go strategy.Run()
	logger.Info("Bybit Trader Strategy started in background.")

	// Wait for interrupt signal to gracefully shut down
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	<-stop
	logger.Info("Shutting down gracefully...")

	// Clean up resources
	cancel()
	time.Sleep(time.Millisecond * 500)

	// Close Strategy
	if err = strategy.Close(); err != nil {
		logger.Error("Error during Strategy shutdown", "error", err)
	} else {
		logger.Info("Strategy shut down successfully.")
	}

	// Close Price Processor
	if err = priceProcessor.Close(); err != nil {
		logger.Error("Error during Price Processor shutdown", "error", err)
	} else {
		logger.Info("Price Processor shut down successfully.")
	}

	// Close Bybit Trading Client
	if err = bybitTradingClient.Close(); err != nil {
		logger.Error("Error during Bybit trading client shutdown", "error", err)
	} else {
		logger.Info("Bybit trading client shut down successfully.")
	}

	// Close Bybit Data Client
	if err = bybitDataClient.Close(); err != nil {
		logger.Error("Error during Bybit data client shutdown", "error", err)
	} else {
		logger.Info("Bybit data client shut down successfully.")
	}

	logger.Info("Bybit Trader system stopped.")
}