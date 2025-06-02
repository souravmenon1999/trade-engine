package strategy

import (
    "github.com/souravmenon1999/trade-engine/framed/types"
    "github.com/souravmenon1999/trade-engine/framed/exchange"
    "github.com/souravmenon1999/trade-engine/framed/config"
    "github.com/rs/zerolog/log"
    "fmt"
)

// ArbitrageStrategy defines the structure for the arbitrage strategy
type ArbitrageStrategy struct {
    exchanges map[types.ExchangeID]exchange.Exchange
    cfg       *config.Config
}

// NewArbitrageStrategy initializes a new ArbitrageStrategy instance
func NewArbitrageStrategy(bybitEx exchange.Exchange, injectiveEx exchange.Exchange, cfg *config.Config) *ArbitrageStrategy {
    exchanges := make(map[types.ExchangeID]exchange.Exchange)
    exchanges[types.ExchangeIDBybit] = bybitEx
    exchanges[types.ExchangeIDInjective] = injectiveEx

    strat := &ArbitrageStrategy{
        exchanges: exchanges,
        cfg:       cfg,
    }

    // Register the callback for Injective order updates
    injectiveEx.RegisterOrderUpdateCallback(strat.HandleOrderUpdate)

    return strat
}

// HandleOrderUpdate processes order updates from exchanges
func (s *ArbitrageStrategy) HandleOrderUpdate(update *types.OrderUpdate) {
    // Log the incoming order update for debugging
    log.Info().Interface("update", update).Msg("Received order update")

    // Only trigger a reverse order if the status is "filled" AND filled quantity > 0
    if update.Status == types.OrderStatusFilled && update.FilledQuantity.ToFloat64() > 0 {
        // Determine the reverse side
        var reverseSide string
        if update.Order.Side == "buy" {
            reverseSide = "sell"
        } else if update.Order.Side == "sell" {
            reverseSide = "buy"
        } else {
            log.Error().Str("side", update.Order.Side).Msg("Unknown order side")
            return
        }

        // Create the reverse order for Bybit
        reverseOrder := &types.Order{
            ExchangeID: types.ExchangeIDBybit,
            Instrument: update.Order.Instrument,
            Price:      update.Order.Price,
            Quantity:   update.FilledQuantity,
            Side:       reverseSide,
        }

        // Send the reverse order
        _, err := s.SendOrder(reverseOrder)
        if err != nil {
            log.Error().Err(err).Msg("Failed to place reverse order on Bybit")
            return
        }
        log.Info().Msg("Reverse order placed on Bybit successfully")
    } else {
        log.Info().
            Str("status", string(update.Status)).
            Float64("filled_quantity", update.FilledQuantity.ToFloat64()).
            Msg("Order update does not require action")
    }
}

// SendOrder sends an order to the specified exchange
func (s *ArbitrageStrategy) SendOrder(order *types.Order) (string, error) {
    ex, ok := s.exchanges[order.ExchangeID]
    if !ok {
        return "", fmt.Errorf("no exchange found for ID: %s", order.ExchangeID)
    }
    return ex.SendOrder(order)
}

// CancelOrder cancels an order on the specified exchange
func (s *ArbitrageStrategy) CancelOrder(exchangeID types.ExchangeID, orderHash string) error {
    ex, ok := s.exchanges[exchangeID]
    if !ok {
        return fmt.Errorf("exchange not found: %s", exchangeID)
    }
    return ex.CancelOrder(orderHash)
}

// Start begins the arbitrage strategy execution
func (s *ArbitrageStrategy) Start() {
    log.Info().Msg("Arbitrage strategy started")

    // Access symbol from configuration
    symbol := s.cfg.BybitOrderbook.Symbol
    log.Info().Str("symbol", symbol).Msg("Using symbol from config")

    // Place an initial order on Injective
    order := &types.Order{
        ExchangeID: types.ExchangeIDInjective,
        Instrument: &types.Instrument{
            Symbol:        s.cfg.BybitOrderbook.Symbol,
            BaseCurrency:  s.cfg.BybitOrderbook.BaseCurrency,
            QuoteCurrency: s.cfg.BybitOrderbook.QuoteCurrency,
            MinLotSize:    types.NewQuantity(0.01),
            ContractType:  "Perpetual",
        },
        Price:    types.NewPrice(2000), // Example price, adjust as needed
        Quantity: types.NewQuantity(0.01),
        Side:     "buy", // Initial order side
    }

    orderID, err := s.SendOrder(order)
    if err != nil {
        log.Error().Err(err).Msg("Failed to place initial order on Injective")
        return
    }
    log.Info().Str("orderID", orderID).Msg("Initial order placed on Injective successfully")
}