package strategy


import (
    "github.com/souravmenon1999/trade-engine/framed/types"
    "github.com/souravmenon1999/trade-engine/framed/exchange"
    "github.com/souravmenon1999/trade-engine/framed/config"
    "github.com/rs/zerolog/log"
    "fmt"
)

type ArbitrageStrategy struct {
    exchanges map[types.ExchangeID]exchange.Exchange
    cfg       *config.Config
}

func NewArbitrageStrategy(bybitEx exchange.Exchange, injectiveEx exchange.Exchange, cfg *config.Config) *ArbitrageStrategy {
    exchanges := make(map[types.ExchangeID]exchange.Exchange)
    exchanges[types.ExchangeIDBybit] = bybitEx
    exchanges[types.ExchangeIDInjective] = injectiveEx
    return &ArbitrageStrategy{
        exchanges: exchanges,
        cfg:       cfg,
    }
}

func (s *ArbitrageStrategy) SendOrder(order *types.Order) (string, error) {
    ex, ok := s.exchanges[order.ExchangeID]
    if !ok {
        return "", fmt.Errorf("no exchange found for ID: %s", order.ExchangeID)
    }
    return ex.SendOrder(order)
}
func (s *ArbitrageStrategy) CancelOrder(exchangeID types.ExchangeID, orderHash string) error {
    ex, ok := s.exchanges[exchangeID]
    if !ok {
        return fmt.Errorf("exchange not found: %s", exchangeID)
    }
    return ex.CancelOrder(orderHash)
}
// Start begins the arbitrage strategy execution.
func (s *ArbitrageStrategy) Start() {
//     log.Info().Msg("Arbitrage strategy started")
//     // Example: Access symbol from configuration
//     symbol := s.cfg.BybitOrderbook.Symbol
//     log.Info().Str("symbol", symbol).Msg("Using symbol from config")

//     // // Example: Place an order using configuration values
//     order := &types.Order{
//         ExchangeID: types.ExchangeIDInjective, // Set ExchangeID here
//         Instrument: &types.Instrument{
//             Symbol:        s.cfg.BybitOrderbook.Symbol,
//             BaseCurrency:  s.cfg.BybitOrderbook.BaseCurrency,
//             QuoteCurrency: s.cfg.BybitOrderbook.QuoteCurrency,
//             MinLotSize:    types.NewQuantity(.01),
//             ContractType:  "Perpetual",
//         },
//         Price:    types.NewPrice(2000),
//         Quantity: types.NewQuantity(.01),
//         Side:     "Buy",
//     }
//    orderID, err := s.SendOrder( order)
//     if err != nil {
//         log.Error().Err(err).Msg("Failed to place order")
//         return
//     }
//     log.Info().Str("orderID", orderID).Msg("Order placed successfully")

    orderHash := "0xcacf40ac4dd34d15073f597f6059d46ea0cb197bff18ecb28f16ae9f6a53aa98"
    err := s.CancelOrder(types.ExchangeIDInjective, orderHash)
    if err != nil {
        log.Error().Err(err).Str("orderHash", orderHash).Msg("Failed to cancel order")
        return
    }
    log.Info().Str("orderHash", orderHash).Msg("Order cancellation requested successfully")
}