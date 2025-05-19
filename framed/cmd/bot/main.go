package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	derivativeExchangePB "github.com/InjectiveLabs/sdk-go/exchange/derivative_exchange_rpc/pb"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/souravmenon1999/trade-engine/framed/config"
	"github.com/souravmenon1999/trade-engine/framed/injective"
	"github.com/souravmenon1999/trade-engine/framed/processor/bybitorderbook"
	"github.com/souravmenon1999/trade-engine/framed/types"
	bybitws "github.com/souravmenon1999/trade-engine/framed/websockets/bybitws"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "yamls/config.yaml", "Path to config file")
	flag.Parse()

	// Initialize logger
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	// Initialize Bybit WebSocket client
	bybitOrderbookWSClient := bybitws.NewBybitOrderBookWSClient(cfg.BybitOrderbook.WSUrl)
	if err := bybitOrderbookWSClient.Connect(); err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Bybit orderbook websocket")
	}
	defer bybitOrderbookWSClient.Close()

	// Channels for processed data and Injective updates
	processedDataChannel := make(chan *types.OrderBookWithVWAP, 10)
	injectiveUpdatesChannel := make(chan *derivativeExchangePB.StreamOrdersHistoryResponse, 10)

	// Define instrument for trading
	instrument := &types.Instrument{
		BaseCurrency:  cfg.BybitOrderbook.BaseCurrency,
		QuoteCurrency: cfg.BybitOrderbook.QuoteCurrency,
		MinLotSize:    types.Quantity(100000),
		ContractType:  "Perpetual",
	}

	// Start VWAP processor for Bybit orderbook
	vwapProcessor := bybitorderbook.NewBybitVWAPProcessor(bybitOrderbookWSClient.RawMessageCh, processedDataChannel, cfg.BybitOrderbook.Symbol, instrument)
	vwapProcessor.StartProcessing()

	// Subscribe to Bybit orderbook updates
	orderBookTopic := "orderbook.50." + cfg.BybitOrderbook.Symbol
	if err := bybitOrderbookWSClient.Subscribe(orderBookTopic); err != nil {
		log.Fatal().Err(err).Msgf("Failed to subscribe to Bybit orderbook for %s", cfg.BybitOrderbook.Symbol)
	}

	// Initialize Injective Trade Client
	tradeClient, err := injective.InitTradeClient(
		cfg.InjectiveExchange.NetworkName,
		cfg.InjectiveExchange.Lb,
		cfg.InjectiveExchange.PrivKey,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize Injective trade client")
	}
	log.Info().Msg("Injective trade client connection established successfully")

	// Initialize Injective Updates Client using trade client's exchangeClient
	updatesClient := injective.InitUpdatesClient(tradeClient.GetExchangeClient())
	log.Info().Msg("Injective updates client initialized successfully")

	// Context for cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Stream order history updates from Injective
	go func() {
		err := updatesClient.StreamOrderHistory(ctx, cfg.InjectiveExchange.MarketId, cfg.InjectiveExchange.SubaccountId, func(res *derivativeExchangePB.StreamOrdersHistoryResponse) {
			order := res.Order
			log.Info().
				Str("order_hash", order.OrderHash).
				Str("state", order.State).
				Str("direction", order.Direction).
				Str("price", order.Price).
				Str("quantity", order.Quantity).
				Msg("Order update received")
			if order.State == "filled" {
				// Order filled on Injective, prepare reverse order on Bybit
				price := order.Price
				quantity := order.Quantity
				direction := order.Direction
				reverseDirection := "sell"
				if direction == "sell" {
					reverseDirection = "buy"
				}
				log.Info().
					Str("price", price).
					Str("quantity", quantity).
					Str("reverse_direction", reverseDirection).
					Msg("Order filled, preparing Bybit reverse order")
				// TODO: Implement Bybit reverse order logic
				// Example: If Injective buy at 0.9, place Bybit sell at 1.1 (use ob.Asks)
			}
			// Send update to channel
			select {
			case injectiveUpdatesChannel <- res:
			default:
				log.Warn().Msg("Injective updates channel full, dropping message")
			}
		})
		if err != nil {
			log.Error().Err(err).Msg("Failed to stream order history")
		}
	}()

	// Signal handling for graceful shutdown
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	// Process VWAP and place orders on Injective
	go func() {
		for data := range processedDataChannel {
			ob := data.OrderBook
			vwap := data.VWAP
			log.Info().Int64("vwap", int64(vwap)).Msg("Calculated VWAP")
			// TODO: Implement logic to place order on Injective using tradeClient
			// Example: Place buy order at VWAP or strategic price
			// Check orderbook for pricing (e.g., BUY 1, SELL 1.1)
			_ = ob
		}
		log.Info().Msg("Main processing goroutine finished")
	}()

	// Process Injective updates (optional consumer)
	go func() {
		for update := range injectiveUpdatesChannel {
			log.Info().Str("order_hash", update.Order.OrderHash).Msg("Processed update from channel")
			// TODO: Additional processing if needed
		}
		log.Info().Msg("Injective updates processing goroutine finished")
	}()

	<-stopCh
	log.Info().Msg("Shutting down...")
}
