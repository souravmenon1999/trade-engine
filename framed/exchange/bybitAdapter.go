package exchange

import (
    "github.com/souravmenon1999/trade-engine/framed/exchange/bybit"
    "github.com/souravmenon1999/trade-engine/framed/config"
    "github.com/souravmenon1999/trade-engine/framed/types"
    "github.com/rs/zerolog/log"
)

type BybitAdapter struct {
    client *bybit.BybitClient
}

func NewBybitAdapter(cfg *config.Config) *BybitAdapter {
    client := bybit.NewBybitClient(cfg) // Initializes, connects, and subscribes
    return &BybitAdapter{client: client}
}



func (b *BybitAdapter) PlaceOrder(order *types.Order) (string, error) {
    err := b.client.SendOrder(order)
    if err != nil {
        return "", err
    }
    return "bybit-order-id", nil // Replace with actual order ID from response if available
}

func (b *BybitAdapter) CancelOrder(orderID string) error {
    // Uncomment and use when CancelOrder is implemented in BybitClient
    // return b.client.CancelOrder(orderID, order.Instrument.Symbol)
    log.Warn().Msg("Bybit CancelOrder not implemented yet")
    return nil
}

func (b *BybitAdapter) Close() error {
    b.client.Close()
    return nil
}

