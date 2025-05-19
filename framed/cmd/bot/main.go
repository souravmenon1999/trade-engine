package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/souravmenon1999/trade-engine/framed/config"
	vwapProcessor "github.com/souravmenon1999/trade-engine/framed/processor/bybitOrderbook/vwap"
	"github.com/souravmenon1999/trade-engine/framed/types"
	bybitws "github.com/souravmenon1999/trade-engine/framed/websockets/bybit/bybitOrderbook"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	bybitOrderbookWSClient := bybitws.NewBybitOrderBookWSClient(cfg.BybitOrderbook.WSUrl)
	if err := bybitOrderbookWSClient.Connect(); err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Bybit orderbook websocket")
	}
	defer bybitOrderbookWSClient.Close()

	processedDataChannel := make(chan *types.OrderBookWithVWAP, 10)

	// Instrument is a pointer to share the same instance across processor and orders
	instrument := &types.Instrument{
		BaseCurrency:  cfg.BybitOrderbook.BaseCurrency,
		QuoteCurrency: cfg.BybitOrderbook.QuoteCurrency,
		MinLotSize:    types.Quantity(100000),
		ContractType:  "Perpetual",
	}

	vwapProcessor := vwapProcessor.NewBybitVWAPProcessor(bybitOrderbookWSClient.RawMessageCh, processedDataChannel, cfg.BybitOrderbook.Symbol, instrument)
	vwapProcessor.StartProcessing()

	orderBookTopic := "orderbook.50." + cfg.BybitOrderbook.Symbol
	if err := bybitOrderbookWSClient.Subscribe(orderBookTopic); err != nil {
		log.Fatal().Err(err).Msgf("Failed to subscribe to Bybit orderbook for %s", cfg.BybitOrderbook.Symbol)
	}

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for data := range processedDataChannel {
			ob := data.OrderBook
			vwap := data.VWAP
			log.Info().Int64("vwap", int64(vwap)).Msg("Calculated VWAP")
			// Strategy logic here
		}
		log.Println("Main processing goroutine finished.")
	}()

	<-stopCh
	log.Info().Msg("Shutting down...")
}