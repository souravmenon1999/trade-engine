package exchange

import (
    "github.com/souravmenon1999/trade-engine/framed/exchange/injective"
    "github.com/souravmenon1999/trade-engine/framed/config"
    "github.com/souravmenon1999/trade-engine/framed/types"
    "github.com/shopspring/decimal"
    "github.com/rs/zerolog/log"
    "strings"
	
)

type InjectiveAdapter struct {
    client *injective.InjectiveClient
    cfg    *config.Config // Store config for market/subaccount IDs
}

func NewInjectiveAdapter(cfg *config.Config) *InjectiveAdapter {
    log.Info().Msg("Starting Injective adapter initialization")
    client, err := injective.NewInjectiveClient(
        cfg.InjectiveExchange.NetworkName,
        cfg.InjectiveExchange.Lb,
        cfg.InjectiveExchange.PrivKey,
        cfg.InjectiveExchange.MarketId,
        cfg.InjectiveExchange.SubaccountId,
    )
    if err != nil {
        log.Fatal().Err(err).Msg("Failed to initialize Injective client")
    }
    return &InjectiveAdapter{client: client, cfg: cfg}
}



func (i *InjectiveAdapter) PlaceOrder(order *types.Order) (string, error) {
    price := decimal.NewFromInt(order.Price.Load())
    quantity := decimal.NewFromFloat(order.Quantity.ToFloat64())
    leverage := decimal.NewFromInt(1) // Adjust as needed
    err := i.client.SendOrder(
        i.cfg.InjectiveExchange.MarketId,
        i.cfg.InjectiveExchange.SubaccountId,
        strings.ToLower(order.Side),
        price, quantity, leverage,
    )
    if err != nil {
        return "", err
    }
    return "injective-order-id", nil // Replace with actual order ID if available
}

func (i *InjectiveAdapter) CancelOrder(orderID string) error {
    return i.client.CancelOrder(i.cfg.InjectiveExchange.MarketId, orderID)
}

func (i *InjectiveAdapter) Close() error {
    i.client.Close()
    return nil
}