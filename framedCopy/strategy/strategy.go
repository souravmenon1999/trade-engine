package strategy

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/souravmenon1999/trade-engine/framedCopy/config"
	"github.com/souravmenon1999/trade-engine/framedCopy/exchange"
	"github.com/souravmenon1999/trade-engine/framedCopy/types"
)

// ArbitrageStrategy defines the structure for the arbitrage strategy
type ArbitrageStrategy struct {
	Exchanges map[types.ExchangeID]exchange.Exchange
	Orders    sync.Map // Key: ClientOrderID (string), Value: *types.Order
	Cfg       *config.Config
}

// NewArbitrageStrategy initializes a new ArbitrageStrategy instance
func NewArbitrageStrategy(bybitEx exchange.Exchange, injectiveEx exchange.Exchange, cfg *config.Config) *ArbitrageStrategy {
	exchanges := make(map[types.ExchangeID]exchange.Exchange)
	exchanges[types.ExchangeIDBybit] = bybitEx
	exchanges[types.ExchangeIDInjective] = injectiveEx

	strat := &ArbitrageStrategy{
		Exchanges: exchanges,
		Orders:    sync.Map{},
		Cfg:       cfg,
	}

	// Register both trading and execution handlers for both exchanges
	injectiveEx.SetTradingHandler(strat)
	injectiveEx.SetExecutionHandler(strat)
	bybitEx.SetTradingHandler(strat)
	bybitEx.SetExecutionHandler(strat)
	return strat
}

// TradingHandler Interface Implementation

// OnOrderUpdate processes order updates from exchanges
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

// OnForceCancel handles force cancel events
func (s *ArbitrageStrategy) OnForceCancel(order *types.Order) {
	log.Info().Str("order_id", order.ClientOrderID.String()).Msg("Force cancel received")
}

// OnOrderDisconnect handles disconnection events
func (s *ArbitrageStrategy) OnOrderDisconnect() {
	log.Warn().Msg("Order stream disconnected")
}

// OnOrderError handles error events
func (s *ArbitrageStrategy) OnOrderError(error string) {
	log.Error().Str("error", error).Msg("Order error received")
}

// OnOrderConnect handles connection events
func (s *ArbitrageStrategy) OnOrderConnect() {
	log.Info().Msg("Order stream connected")
}

// ExecutionHandler Interface Implementation

// OnExecutionUpdate processes execution updates
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

// OnExecutionConnect handles execution stream connection events
func (s *ArbitrageStrategy) OnExecutionConnect() {
	log.Info().Msg("Execution stream connected")
}

// OnExecutionDisconnect handles execution stream disconnection events
func (s *ArbitrageStrategy) OnExecutionDisconnect() {
	log.Warn().Msg("Execution stream disconnected")
}

// OnExecutionError handles execution stream error events
func (s *ArbitrageStrategy) OnExecutionError(error string) {
	log.Error().Str("error", error).Msg("Execution error received")
}

// SendOrder sends an order to the specified exchange
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

// CancelOrder cancels an order on the specified exchange
func (s *ArbitrageStrategy) CancelOrder(exchangeID types.ExchangeID, orderHash string) error {
	ex, ok := s.Exchanges[exchangeID]
	if !ok {
		return fmt.Errorf("exchange not found: %s", exchangeID)
	}
	return ex.CancelOrder(orderHash)
}

// Start begins the arbitrage strategy execution
func (s *ArbitrageStrategy) Start() {
	log.Info().Msg("Arbitrage strategy started")

	symbol := s.Cfg.BybitOrderbook.Symbol
	log.Info().Str("symbol", symbol).Msg("Using symbol from config")

	order := &types.Order{
		ClientOrderID: uuid.New(),
		ExchangeID:    types.ExchangeIDInjective,
		Instrument: &types.Instrument{
			Symbol:        s.Cfg.BybitOrderbook.Symbol,
			BaseCurrency:  s.Cfg.BybitOrderbook.BaseCurrency,
			QuoteCurrency: s.Cfg.BybitOrderbook.QuoteCurrency,
			MinLotSize:    types.NewQuantity(0.01),
			ContractType:  "Perpetual",
		},
		Side:        types.SideBuy,
		OrderType:   types.OrderTypeLimit,
		TimeInForce: types.TimeInForceGTC,
		Price:       atomic.Int64{},
		Quantity:    atomic.Int64{},
		CreatedAt:   atomic.Int64{},
		UpdatedAt:   atomic.Int64{},
	}
	order.Price.Store(2000 * types.SCALE_FACTOR_F64)
	order.CreatedAt.Store(time.Now().UnixMilli())
	order.UpdatedAt.Store(time.Now().UnixMilli())
	order.Quantity.Store(int64(0.01 * types.SCALE_FACTOR_F64))

	clientOrderID, err := s.SendOrder(order)
	if err != nil {
		log.Error().Err(err).Msg("Failed to place initial order on Injective")
		return
	}
	log.Info().Str("clientOrderID", clientOrderID).Msg("Initial order placed on Injective successfully")
}