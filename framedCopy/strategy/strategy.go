package strategy

import (
	"fmt"
	"sync"
	"sync/atomic"
	"strings"
	"time"
    "math"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/souravmenon1999/trade-engine/framedCopy/config"
	"github.com/souravmenon1999/trade-engine/framedCopy/exchange"
	"github.com/souravmenon1999/trade-engine/framedCopy/types"
)

type ArbitrageStrategy struct {
	Exchanges          map[types.ExchangeID]exchange.Exchange
	Orders             sync.Map
	Cfg                *config.Config
	connectionsMu      sync.Mutex
	connectionsReady   map[types.ExchangeID]bool
	allConnected       bool
	BybitOrderbook     *types.OrderBook
	InjectiveOrderbook *types.OrderBook
	FundingRates       map[types.ExchangeID]*types.FundingRate
	FundingRatesMu     sync.RWMutex
}

func NewArbitrageStrategy(bybitEx exchange.Exchange, injectiveEx exchange.Exchange, cfg *config.Config) *ArbitrageStrategy {
	// Initialize separate OrderBook instances for each exchange
	bybitExchangeID := types.ExchangeIDBybit
	injectiveExchangeID := types.ExchangeIDInjective
	bybitOrderbook := types.NewOrderBook(&types.Instrument{Symbol: cfg.BybitOrderbook.Symbol}, &bybitExchangeID)
	injectiveOrderbook := types.NewOrderBook(&types.Instrument{Symbol: cfg.InjectiveExchange.MarketId}, &injectiveExchangeID)

	strat := &ArbitrageStrategy{
		Exchanges: map[types.ExchangeID]exchange.Exchange{
			types.ExchangeIDBybit:     bybitEx,
			types.ExchangeIDInjective: injectiveEx,
		},
		Orders:             sync.Map{},
		Cfg:                cfg,
		connectionsReady:   make(map[types.ExchangeID]bool),
		allConnected:       false,
		BybitOrderbook:     bybitOrderbook,
		InjectiveOrderbook: injectiveOrderbook,
		FundingRates:       make(map[types.ExchangeID]*types.FundingRate),
	}

	injectiveEx.SetAccountHandler(strat)
	injectiveEx.SetTradingHandler(strat)
	injectiveEx.SetExecutionHandler(strat)
	injectiveEx.SetOrderbookHandler(strat)
	injectiveEx.SetFundingRateHandler(strat)
	bybitEx.SetTradingHandler(strat)
	bybitEx.SetExecutionHandler(strat)
	bybitEx.SetAccountHandler(strat)
	bybitEx.SetOrderbookHandler(strat)
	bybitEx.SetFundingRateHandler(strat)

	for exchangeID, ex := range strat.Exchanges {
		exID := exchangeID
		ex.RegisterConnectionCallback(func() {
			strat.onExchangeConnected(exID)
		})
	}

	for _, ex := range strat.Exchanges {
		callback := func() {
			if err := ex.Connect(); err != nil {
				log.Error().Err(err).Msg("Failed to connect WebSocket")
				return
			}
			if err := ex.SubscribeAll(cfg); err != nil {
				log.Error().Err(err).Msg("Failed to subscribe to topics")
				return
			}
			ex.StartReading()
		}
		ex.SetOnReadyCallback(callback)
		callback()
	}

	return strat
}

func (s *ArbitrageStrategy) onExchangeConnected(exchangeID types.ExchangeID) {
	s.connectionsMu.Lock()
	s.connectionsReady[exchangeID] = true
	allReady := true
	for exID, ex := range s.Exchanges {
		if !s.connectionsReady[exID] || !ex.IsConnected() {
			allReady = false
			break
		}
	}
	if allReady && !s.allConnected {
		s.allConnected = true
		s.connectionsMu.Unlock()
		go s.startStrategy()
	} else {
		s.connectionsMu.Unlock()
	}
	log.Info().Str("exchange", string(exchangeID)).Msg("Exchange connected")
}

func (s *ArbitrageStrategy) startStrategy() {
	log.Info().Msg("Arbitrage strategy execution begun")
	s.Start()
}


func (s *ArbitrageStrategy) Start() {
	log.Info().Msg("Arbitrage strategy started")

	symbol := s.Cfg.BybitOrderbook.Symbol
	log.Info().Str("symbol", symbol).Msg("Using symbol from config")

	instrument := types.NewPerpetualInstrument(
		s.Cfg.BybitOrderbook.Symbol,
		s.Cfg.BybitOrderbook.BaseCurrency,
		s.Cfg.BybitOrderbook.QuoteCurrency,
		s.Cfg.BybitOrderbook.MinLotSize,
	)
	order := types.NewLimitOrder(
		instrument,
		types.SideBuy,
		types.ExchangeIDInjective,
		2000.0,
		s.Cfg.BybitOrderbook.MinLotSize,
	)

	clientOrderID, err := s.SendOrder(order)
	if err != nil {
		log.Error().Err(err).Msg("Failed to place initial order on Injective")
		return
	}
	log.Info().Str("clientOrderID", clientOrderID).Msg("Initial order placed on Injective successfully")

	// Periodic logging of orderbook and funding rate data
	go func() {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    for range ticker.C {
        for exID := range s.Exchanges {
            ob := s.GetOrderbook(exID)
            if ob != nil {
                midPrice, ok := ob.GetMidPrice()
                var bidLines, askLines []string
                ob.Bids.Range(func(key, value interface{}) bool {
                    price := float64(key.(int64)) / types.SCALE_FACTOR_F64
                    pl := value.(*types.PriceLevel)
                    bidLines = append(bidLines, fmt.Sprintf("Price: %.2f, Qty: %.2f, Orders: %d", price, float64(pl.Quantity)/types.SCALE_FACTOR_F64, pl.OrderCount))
                    return true
                })
                ob.Asks.Range(func(key, value interface{}) bool {
                    price := float64(key.(int64)) / types.SCALE_FACTOR_F64
                    pl := value.(*types.PriceLevel)
                    askLines = append(askLines, fmt.Sprintf("Price: %.2f, Qty: %.2f, Orders: %d", price, float64(pl.Quantity)/types.SCALE_FACTOR_F64, pl.OrderCount))
                    return true
                })
                log.Info().
                    Str("exchange", string(exID)).
                    Str("symbol", ob.Instrument.Symbol).
                    Float64("mid_price", midPrice).
                    Bool("mid_price_valid", ok).
                    Str("bids", fmt.Sprintf("\n\t%s", strings.Join(bidLines, "\n\t"))).
                    Str("asks", fmt.Sprintf("\n\t%s", strings.Join(askLines, "\n\t"))).
                    Int64("timestamp_ms", ob.LastUpdateTime.Load()).
                    Int64("sequence", ob.Sequence.Load()).
                    Str("address", fmt.Sprintf("%p", ob)).
                    Msg("Orderbook snapshot")
            } else {
                log.Warn().
                    Str("exchange", string(exID)).
                    Msg("No orderbook data available")
            }
            fr := s.GetFundingRate(exID)
            if fr != nil && fr.Rate != 0 {
                log.Info().
                    Str("exchange", string(exID)).
                    Float64("funding_rate", fr.Rate).
                    Str("interval", fr.Interval.String()).
                    Int64("last_updated_unix_s", fr.LastUpdated).
                    Time("last_updated", time.Unix(fr.LastUpdated, 0)).
                    Msg("Periodic funding rate check")
            } else {
                log.Warn().
                    Str("exchange", string(exID)).
                    Msg("No valid funding rate data in periodic check")
            }
        }
    }
}()
}


func (s *ArbitrageStrategy) SendOrder(order *types.Order) (string, error) {
	ex, ok := s.Exchanges[order.ExchangeID]
	if !ok {
		return "", fmt.Errorf("no exchange found for ID: %s", order.ExchangeID)
	}

	if order.ClientOrderID == uuid.Nil {
		order.ClientOrderID = uuid.New()
	}
	if order.CreatedAt.Load() == 0 {
		now := time.Now().UnixMilli()
		order.CreatedAt.Store(now)
		order.UpdatedAt.Store(now)
	}
	order.UpdateStatus(types.OrderStatusSubmitted)

	s.Orders.Store(order.ClientOrderID.String(), order)

	clientOrderID, err := ex.SendOrder(order)
	if err != nil {
		s.Orders.Delete(order.ClientOrderID.String())
		return "", err
	}

	log.Info().Str("clientOrderID", clientOrderID).Msg("Order sent successfully")
	return clientOrderID, nil
}


func (s *ArbitrageStrategy) CancelOrder(exchangeID types.ExchangeID, orderHash string) error {
	ex, ok := s.Exchanges[exchangeID]
	if !ok {
		return fmt.Errorf("exchange not found: %s", exchangeID)
	}
	return ex.CancelOrder(orderHash)
}


// Enhanced OnOrderbook with detailed logging
func (s *ArbitrageStrategy) OnOrderbookUpdate(exchangeID types.ExchangeID, update *types.OrderBookUpdate) {
	var ob *types.OrderBook
	switch exchangeID {
	case types.ExchangeIDBybit:
		ob = s.BybitOrderbook
	case types.ExchangeIDInjective:
		ob = s.InjectiveOrderbook
	default:
		log.Error().Str("exchange", string(exchangeID)).Msg("Unknown exchange ID")
		return
	}
	if ob == nil {
		log.Error().Str("exchange", string(exchangeID)).Msg("OrderBook is nil")
		return
	}
	ob.ApplyUpdate(update)
	log.Debug().
		Str("exchange", string(exchangeID)).
		Float64("price", float64(update.Price)/types.SCALE_FACTOR_F64).
		Float64("quantity", float64(update.Quantity)/types.SCALE_FACTOR_F64).
		Str("side", string(update.Side)).
		Int64("sequence", update.Sequence).
		Msg("Orderbook update applied")
}


func (s *ArbitrageStrategy) OnOrderbookDisconnect() {
	log.Warn().Msg("Orderbook stream disconnected")
}

func (s *ArbitrageStrategy) OnOrderbookError(error string) {
	log.Error().Str("error", error).Msg("Orderbook error received")
}

func (s *ArbitrageStrategy) OnOrderbookConnect() {
	log.Info().Msg("Orderbook stream connected")
}

// Enhanced OnFundingRateUpdate with detailed logging
func (s *ArbitrageStrategy) OnFundingRateUpdate(exchange types.ExchangeID, fundingRate *types.FundingRate) {
	s.FundingRatesMu.Lock()
	previousRate := 0.0
	if prevFr, ok := s.FundingRates[exchange]; ok && prevFr != nil {
		previousRate = prevFr.Rate
	}
	s.FundingRates[exchange] = fundingRate
	s.FundingRatesMu.Unlock()

	rateChange := 0.0
	if previousRate != 0 {
		rateChange = math.Abs(fundingRate.Rate-previousRate) / math.Abs(previousRate) * 100
	}

	log.Info().
		Str("exchange", string(exchange)).
		Float64("funding_rate", fundingRate.Rate).
		Float64("previous_rate", previousRate).
		Float64("rate_change_percent", rateChange).
		Str("interval", fundingRate.Interval.String()).
		Int64("last_updated_unix_s", fundingRate.LastUpdated).
		Time("last_updated", time.Unix(fundingRate.LastUpdated, 0)).
		Int64("next_funding_timestamp", fundingRate.NextFundingTimestamp).
		Msg("Funding rate update received")
}

func (s *ArbitrageStrategy) OnFundingRateError(error string) {
	log.Error().Str("error", error).Msg("Funding rate error received")
}

func (s *ArbitrageStrategy) OnFundingRateConnect() {
	log.Info().Msg("Funding rate stream connected")
}

func (s *ArbitrageStrategy) OnFundingRateDisconnect() {
	log.Warn().Msg("Funding rate stream disconnected")
}

func (s *ArbitrageStrategy) GetFundingRate(exchange types.ExchangeID) *types.FundingRate {
	s.FundingRatesMu.RLock()
	defer s.FundingRatesMu.RUnlock()
	if fr, ok := s.FundingRates[exchange]; ok {
		return fr
	}
	return &types.FundingRate{}
}
func (s *ArbitrageStrategy) GetOrderbook(exchange types.ExchangeID) *types.OrderBook {
	switch exchange {
	case types.ExchangeIDBybit:
		if s.BybitOrderbook == nil {
			log.Error().Msg("BybitOrderbook is nil")
			return nil
		}
		return s.BybitOrderbook
	case types.ExchangeIDInjective:
		if s.InjectiveOrderbook == nil {
			log.Error().Msg("InjectiveOrderbook is nil")
			return nil
		}
		return s.InjectiveOrderbook
	default:
		log.Warn().Str("exchange", string(exchange)).Msg("Unknown exchange ID")
		return nil
	}
}

func (s *ArbitrageStrategy) OnOrderUpdate(update *types.OrderUpdate) {
	log.Info().Interface("update", update).Msg("Received order update")

	if update.RequestID == nil {
		log.Error().Msg("Order update missing RequestID")
		return
	}
	clientOrderID := *update.RequestID
	orderInterface, ok := s.Orders.Load(clientOrderID)
	if !ok {
		log.Error().Str("clientOrderID", clientOrderID).Msg("Order not found in map")
		return
	}
	order := orderInterface.(*types.Order)

	order.ApplyUpdate(update)

	if order.GetStatus() == types.OrderStatusFilled && order.GetFilledQuantity() > 0 {
		reverseSide := types.SideSell
		if order.Side == types.SideBuy {
			reverseSide = types.SideBuy
		}

		reverseOrder := &types.Order{
			ClientOrderID: uuid.New(),
			Instrument:    order.Instrument,
			Side:          reverseSide,
			OrderType:     types.OrderTypeLimit,
			ExchangeID:    types.ExchangeIDBybit,
			TimeInForce:   types.TimeInForceGTC,
			Quantity:      atomic.Int64{},
			CreatedAt:     atomic.Int64{},
			UpdatedAt:     atomic.Int64{},
		}
		reverseOrder.Price.Store(order.Price.Load())
		reverseOrder.CreatedAt.Store(time.Now().UnixMilli())
		reverseOrder.UpdatedAt.Store(time.Now().UnixMilli())
		reverseOrder.Quantity.Store(int64(order.GetFilledQuantity() * types.SCALE_FACTOR_F64))

		_, err := s.SendOrder(reverseOrder)
		if err != nil {
			log.Error().Err(err).Str("clientOrderID", reverseOrder.ClientOrderID.String()).Msg("Failed to place reverse order on Bybit")
			return
		}
		log.Info().Str("clientOrderID", reverseOrder.ClientOrderID.String()).Msg("Reverse order placed on Bybit successfully")
	} else {
		log.Info().
			Str("status", fmt.Sprintf("%v", order.GetStatus())).
			Float64("filled_quantity", order.GetFilledQuantity()).
			Msg("Order update does not require action")
	}
}

func (s *ArbitrageStrategy) OnForceCancel(order *types.Order) {
	log.Info().Str("order_id", order.ClientOrderID.String()).Msg("Force cancel received")
}

func (s *ArbitrageStrategy) OnOrderDisconnect() {
	log.Warn().Msg("Order stream disconnected")
}

func (s *ArbitrageStrategy) OnOrderError(error string) {
	log.Error().Str("error", error).Msg("Order error received")
}

func (s *ArbitrageStrategy) OnOrderConnect() {
	log.Info().Msg("Order stream connected")
}

func (s *ArbitrageStrategy) OnExecutionUpdate(updates []*types.OrderUpdate) {
	for _, update := range updates {
		var clientOrderID string
		if update.RequestID != nil {
			clientOrderID = *update.RequestID
		} else {
			clientOrderID = "unknown"
		}
		log.Info().
			Str("clientOrderID", clientOrderID).
			Float64("fillQty", *update.FillQty).
			Float64("fillPrice", *update.FillPrice).
			Msg("Execution update received")
	}
}

func (s *ArbitrageStrategy) OnExecutionConnect() {
	log.Info().Msg("Execution stream connected")
}

func (s *ArbitrageStrategy) OnExecutionDisconnect() {
	log.Warn().Msg("Execution stream disconnected")
}

func (s *ArbitrageStrategy) OnExecutionError(error string) {
	log.Error().Str("error", error).Msg("Execution error received")
}


func (s *ArbitrageStrategy) OnAccountUpdate(update *types.AccountUpdate) {
	log.Info().Msgf("Account update received: %+v", update)
}

func (s *ArbitrageStrategy) OnPositionUpdate(position *types.Position) {
	log.Info().Msgf("Position update received: %+v", position)
}

func (s *ArbitrageStrategy) OnPositionDisconnect() {
	log.Warn().Msg("Position stream disconnected")
}

func (s *ArbitrageStrategy) OnPositionError(error string) {
	log.Error().Msgf("Position error: %s", error)
}

func (s *ArbitrageStrategy) OnPositionConnect() {
	log.Info().Msg("Position stream connected")
}