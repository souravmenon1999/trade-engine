package strategy

import (
	"fmt"
	"sync"
	"time"
	"sync/atomic"
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

	// Register order update callbacks for both exchanges
	injectiveEx.RegisterOrderUpdateCallback(strat.HandleOrderUpdate)
	bybitEx.RegisterOrderUpdateCallback(strat.HandleOrderUpdate)

	return strat
}

// HandleOrderUpdate processes order updates from exchanges
func (s *ArbitrageStrategy) HandleOrderUpdate(update *types.OrderUpdate) {
	log.Info().Interface("update", update).Msg("Received order update")

	// Validate the order reference
	if update.Order == nil {
		log.Error().Msg("Order update missing order reference")
		return
	}

	// Look up the order in the map using RequestID (which stores ClientOrderID)
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

	// Apply the update to the order
	order.ApplyUpdate(update)

	// Check if the order is filled and place a reverse order if so
	if order.GetStatus() == types.OrderStatusFilled && order.GetFilledQuantity() > 0 {
		reverseSide := types.SideSell
		if order.Side == types.SideBuy {
			reverseSide = types.SideBuy
		}

		reverseOrder := &types.Order{
			ClientOrderID:   uuid.New(),
			Instrument:      order.Instrument,
			Side:            reverseSide,
			OrderType:       types.OrderTypeLimit,
			ExchangeID:      types.ExchangeIDBybit,
			TimeInForce:     types.TimeInForceGTC,
			Quantity:        types.NewQuantity(order.GetFilledQuantity()),
			CreatedAt:       atomic.Int64{},
			UpdatedAt:       atomic.Int64{},
		}
		reverseOrder.Price.Store(order.Price.Load())
		reverseOrder.CreatedAt.Store(time.Now().UnixMilli())
		reverseOrder.UpdatedAt.Store(time.Now().UnixMilli())

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

// SendOrder sends an order to the specified exchange
func (s *ArbitrageStrategy) SendOrder(order *types.Order) (string, error) {
	ex, ok := s.Exchanges[order.ExchangeID]
	if !ok {
		return "", fmt.Errorf("no exchange found for ID: %s", order.ExchangeID)
	}

	// Ensure ClientOrderID and timestamps are set
	if order.ClientOrderID == uuid.Nil {
		order.ClientOrderID = uuid.New()
	}
	if order.CreatedAt.Load() == 0 {
		now := time.Now().UnixMilli()
		order.CreatedAt.Store(now)
		order.UpdatedAt.Store(now)
	}
	order.UpdateStatus(types.OrderStatusSubmitted)

	// Store the order in the map
	s.Orders.Store(order.ClientOrderID.String(), order)

	// Send the order to the exchange
	clientOrderID, err := ex.SendOrder(order)
	if err != nil {
		s.Orders.Delete(order.ClientOrderID.String()) // Remove on failure
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

	// Place an initial order on Injective
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
		Quantity:    types.NewQuantity(0.01),
		CreatedAt:   atomic.Int64{},
		UpdatedAt:   atomic.Int64{},
	}
	order.Price.Store(2000 * types.SCALE_FACTOR_F64)
	order.CreatedAt.Store(time.Now().UnixMilli())
	order.UpdatedAt.Store(time.Now().UnixMilli())

	clientOrderID, err := s.SendOrder(order)
	if err != nil {
		log.Error().Err(err).Msg("Failed to place initial order on Injective")
		return
	}
	log.Info().Str("clientOrderID", clientOrderID).Msg("Initial order placed on Injective successfully")
}