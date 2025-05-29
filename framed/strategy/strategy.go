package strategy

import (
    "github.com/souravmenon1999/trade-engine/framed/ordermanager"
    "github.com/souravmenon1999/trade-engine/framed/types"
	"github.com/souravmenon1999/trade-engine/framed/config"
    "github.com/rs/zerolog/log"
)

type ArbitrageStrategy struct {
    om  *ordermanager.OrderManager // Order manager for placing orders
    cfg *config.Config             // Configuration settings
}

// NewArbitrageStrategy creates a new instance of ArbitrageStrategy.
func NewArbitrageStrategy(om *ordermanager.OrderManager, cfg *config.Config) *ArbitrageStrategy {
    return &ArbitrageStrategy{
        om:  om,
        cfg: cfg,
    }
}

// Start begins the arbitrage strategy execution.
func (s *ArbitrageStrategy) Start() {


    
    // log.Info().Msg("Arbitrage strategy started")
    // // Example: Access symbol from configuration
    // symbol := s.cfg.BybitOrderbook.Symbol
    // log.Info().Str("symbol", symbol).Msg("Using symbol from config")

    // // // Example: Place an order using configuration values
    // order := &types.Order{
    //   ExchangeID: types.ExchangeIDBybit, // Set ExchangeID here
    //     Instrument: &types.Instrument{
    //         Symbol:        s.cfg.BybitOrderbook.Symbol,
    //         BaseCurrency:  s.cfg.BybitOrderbook.BaseCurrency,
    //         QuoteCurrency: s.cfg.BybitOrderbook.QuoteCurrency,
    //         MinLotSize:    types.NewQuantity(.01),
    //         ContractType:  "Perpetual",
    //     },
    //     Price:    types.NewPrice(2000),
    //     Quantity: types.NewQuantity(.01),
    //     Side:     "Buy",
    // }
    // orderID, err := s.om.PlaceOrder(order)
    // if err != nil {
    //     log.Error().Err(err).Msg("Failed to place order")
    //     return
    // }
    // log.Info().Str("orderID", orderID).Msg("Order placed successfully")

    orderHash := "f426bdf8-1a19-4a76-8690-bf188df78439"
    err := s.om.CancelOrder(types.ExchangeIDBybit, orderHash) 
   if err != nil {
        log.Error().Err(err).Str("orderHash", orderHash).Msg("Failed to cancel order")
        return
    }
     log.Info().Str("orderHash", orderHash).Msg("Order cancellation requested successfully")


}