package bybit

import (
	"fmt"
	"github.com/souravmenon1999/trade-engine/framedCopy/config"
	exchange"github.com/souravmenon1999/trade-engine/framedCopy/exchange"
	"github.com/souravmenon1999/trade-engine/framedCopy/new/websockets"
	"github.com/rs/zerolog/log"
)

type BybitClient struct {
	publicWS         *websockets.BaseWSClient
	tradingWS        *websockets.BaseWSClient
	updatesWS        *websockets.BaseWSClient
	symbol           string
	tradingHandler   exchange.TradingHandler
	executionHandler exchange.ExecutionHandler
	orderbookHandler exchange.OrderbookHandler
	accountHandler   exchange.AccountHandler
	apiKey           string
	apiSecret        string
}

func NewBybitClient(cfg *config.Config) *BybitClient {
	publicWS := websockets.NewBaseWSClient(cfg.BybitOrderbook.WSUrl)
	tradingWS := websockets.NewBaseWSClient(cfg.BybitExchangeClient.TradingWSUrl)
	updatesWS := websockets.NewBaseWSClient(cfg.BybitExchangeClient.UpdatesWSUrl)

	client := &BybitClient{
		publicWS:   publicWS,
		tradingWS:  tradingWS,
		updatesWS:  updatesWS,
		symbol:     cfg.BybitOrderbook.Symbol,
		apiKey:     cfg.BybitExchangeClient.APIKey,
		apiSecret:  cfg.BybitExchangeClient.APISecret,
	}

	// Register handlers
	mdHandler := NewBybitMDHandler(client.orderbookHandler, client.symbol)
	publicWS.RegisterHandler(fmt.Sprintf("orderbook.%d.%s", cfg.BybitOrderbook.OrderbookDepth, client.symbol), mdHandler)

	tradingHandler := NewBybitTradingHandler(client.tradingHandler)
	tradingWS.RegisterHandler("order", tradingHandler)

	privateHandler := NewBybitPrivateHandler(client.executionHandler, client.accountHandler)
	updatesWS.RegisterHandler("position", privateHandler)
	updatesWS.RegisterHandler("execution.fast", privateHandler)
	updatesWS.RegisterHandler("wallet", privateHandler)

	// Connect and subscribe
	if err := client.Connect(); err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Bybit WebSockets")
	}
	if err := client.SubscribeAll(cfg); err != nil {
		log.Fatal().Err(err).Msg("Failed to subscribe to Bybit topics")
	}
	client.StartReading()

	return client
}

func (c *BybitClient) Connect() error {
	if err := c.publicWS.Connect(); err != nil {
		return fmt.Errorf("failed to connect public WS: %w", err)
	}
	if err := c.tradingWS.Connect(); err != nil {
		return fmt.Errorf("failed to connect trading WS: %w", err)
	}
	if err := c.updatesWS.Connect(); err != nil {
		return fmt.Errorf("failed to connect updates WS: %w", err)
	}
	// Add authentication logic using apiKey and apiSecret if needed
	return nil
}

func (c *BybitClient) SubscribeAll(cfg *config.Config) error {
	publicSub := map[string]interface{}{
		"op":   "subscribe",
		"args": []interface{}{fmt.Sprintf("orderbook.%d.%s", cfg.BybitOrderbook.OrderbookDepth, c.symbol)},
	}
	if err := c.publicWS.Subscribe(publicSub); err != nil {
		return fmt.Errorf("failed to subscribe to public topic: %w", err)
	}
	// Add subscriptions for trading and updates WS as needed
	return nil
}

func (c *BybitClient) StartReading() {
	c.publicWS.Start()
	c.tradingWS.Start()
	c.updatesWS.Start()
}

func (c *BybitClient) SetTradingHandler(handler exchange.TradingHandler) {
	c.tradingHandler = handler
	log.Info().Msg("Set trading handler for Bybit")
}

func (c *BybitClient) SetExecutionHandler(handler exchange.ExecutionHandler) {
	c.executionHandler = handler
	log.Info().Msg("Set execution handler for Bybit")
}

func (c *BybitClient) SetOrderbookHandler(handler exchange.OrderbookHandler) {
	c.orderbookHandler = handler
	log.Info().Msg("Set orderbook handler for Bybit")
}

func (c *BybitClient) SetAccountHandler(handler exchange.AccountHandler) {
	c.accountHandler = handler
	log.Info().Msg("Set account handler for Bybit")
}

// Add SendOrder, CancelOrder, AmendOrder, etc., as needed