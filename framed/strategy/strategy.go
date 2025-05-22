package strategy

import (
	"fmt"
	"github.com/rs/zerolog/log"
	bybit "github.com/souravmenon1999/trade-engine/framed/exchange/bybit"
	"github.com/souravmenon1999/trade-engine/framed/exchange/injective"
	"github.com/souravmenon1999/trade-engine/framed/processor/bybitorderbook"
	"github.com/souravmenon1999/trade-engine/framed/types"
	"github.com/souravmenon1999/trade-engine/framed/config"
)

// Strategy holds the dependencies for the trading strategy
type Strategy struct {
	bybitClient     *bybit.BybitClient
	injectiveClient *injective.InjectiveClient
	cfg             *config.Config
}

// InitStrategy initializes the strategy with the provided clients and config
func InitStrategy(bClient *bybit.BybitClient, iClient *injective.InjectiveClient, cfg *config.Config) {
	s := &Strategy{
		bybitClient:     bClient,
		injectiveClient: iClient,
		cfg:             cfg,
	}
	s.start()
}

// start sets up the Bybit subscription and VWAP processing
func (s *Strategy) start() {
	// Define the instrument for Bybit
	instrument := &types.Instrument{
		BaseCurrency:  s.cfg.BybitOrderbook.BaseCurrency,
		QuoteCurrency: s.cfg.BybitOrderbook.QuoteCurrency,
		MinLotSize:    types.NewQuantity(100000), // 0.1 in micro units
		ContractType:  "Perpetual",
	}

	// Initialize VWAP processor
	vwapProcessor := bybitorderbook.NewBybitVWAPProcessor(
		func(data *types.OrderBookWithVWAP) {
			ob := data.OrderBook
			vwap := data.VWAP
			log.Info().Int64("vwap", vwap.Load()).Msg("Calculated VWAP")
			// Generate order book for Injective based on VWAP
			s.generateOrderBook(ob, vwap)
		},
		s.cfg.BybitOrderbook.Symbol,
		instrument,
	)

	// Subscribe to Bybit order book
	orderBookTopic := "orderbook.50." + s.cfg.BybitOrderbook.Symbol
	if err := s.bybitClient.Subscribe(orderBookTopic, vwapProcessor.ProcessAndApplyMessage); err != nil {
		log.Fatal().Err(err).Msgf("Failed to subscribe to %s", orderBookTopic)
	}
}

// generateOrderBook creates an order book for Injective based on VWAP
func (s *Strategy) generateOrderBook(ob *types.OrderBook, vwap *types.Price) {
	// Extract VWAP value
	vwapValue := vwap.Load()
	spread := int64(100000) // Example spread: 0.1 in micro units

	// Generate bids (buy orders) below VWAP
	bidPrice := types.NewPrice(vwapValue - spread)
	bidQuantity := types.NewQuantity(1000000) // 1.0 in micro units
	bids := map[*types.Price]*types.Quantity{
		bidPrice: bidQuantity,
	}

	// Generate asks (sell orders) above VWAP
	askPrice := types.NewPrice(vwapValue + spread)
	askQuantity := types.NewQuantity(1000000) // 1.0 in micro units
	asks := map[*types.Price]*types.Quantity{
		askPrice: askQuantity,
	}

	// Create the order book for Injective
	injectiveOrderBook := &types.OrderBook{
		Instrument: &types.Instrument{
			BaseCurrency:  s.cfg.InjectiveExchange.MarketId[:3], // e.g., "INJ" (adjust based on config)
			QuoteCurrency: s.cfg.InjectiveExchange.MarketId[4:], // e.g., "USDT" (adjust based on config)
			MinLotSize:    types.NewQuantity(100000),            // Example value
			ContractType:  "Spot",                               // Adjust as needed
		},
		Bids: bids,
		Asks: asks,
	}

	// Log the generated order book (for now; later, this will feed into the Injective connector)
	log.Info().
		Str("market_id", s.cfg.InjectiveExchange.MarketId).
		Str("base_currency", injectiveOrderBook.Instrument.BaseCurrency).
		Str("quote_currency", injectiveOrderBook.Instrument.QuoteCurrency).
		Interface("bids", map[string]int64{
			fmt.Sprintf("%.6f", float64(bidPrice.Load())/1_000_000): bidQuantity.Load() / 1_000_000,
		}).
		Interface("asks", map[string]int64{
			fmt.Sprintf("%.6f", float64(askPrice.Load())/1_000_000): askQuantity.Load() / 1_000_000,
		}).
		Msg("Generated and logged Injective order book")
}