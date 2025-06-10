package bybit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	log "github.com/rs/zerolog/log"
	"github.com/souravmenon1999/trade-engine/framedCopy/config"
	exchange "github.com/souravmenon1999/trade-engine/framedCopy/exchange"
	"github.com/souravmenon1999/trade-engine/framedCopy/new"
	"github.com/souravmenon1999/trade-engine/framedCopy/types"
)

type BybitClient struct {
	publicWS         *baseWS.BaseWSClient
	tradingWS        *baseWS.BaseWSClient
	updatesWS        *baseWS.BaseWSClient
	symbol           string
	tradingHandler   exchange.TradingHandler
	executionHandler exchange.ExecutionHandler
	orderbookHandler exchange.OrderbookHandler
	accountHandler   exchange.AccountHandler
	apiKey           string
	apiSecret        string
}

func NewBybitClient(cfg *config.Config) *BybitClient {
	publicWS := baseWS.NewBaseWSClient(cfg.BybitOrderbook.WSUrl, cfg.BybitExchangeClient.APIKey, cfg.BybitExchangeClient.APISecret)
	tradingWS := baseWS.NewBaseWSClient(cfg.BybitExchangeClient.TradingWSUrl, cfg.BybitExchangeClient.APIKey, cfg.BybitExchangeClient.APISecret)
	updatesWS := baseWS.NewBaseWSClient(cfg.BybitExchangeClient.UpdatesWSUrl, cfg.BybitExchangeClient.APIKey, cfg.BybitExchangeClient.APISecret)

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

func (c *BybitClient) SendOrder(order *types.Order) (string, error) {
	clientOrderID := order.ClientOrderID.String()
	go func() {
		symbol := order.Instrument.BaseCurrency + order.Instrument.QuoteCurrency
		side := strings.Title(string(order.Side))
		price := order.GetPrice()
		quantity := order.GetQuantity()

		timestamp := time.Now().UnixMilli()
		recvWindow := 5000
		signature := c.generateSignature(fmt.Sprintf("%d%d", timestamp, recvWindow))

		orderMsg := map[string]interface{}{
			"op": "order.create",
			"args": []interface{}{
				map[string]interface{}{
					"symbol":      symbol,
					"side":        side,
					"orderType":   string(order.OrderType),
					"qty":         fmt.Sprintf("%f", quantity),
					"price":       fmt.Sprintf("%f", price),
					"category":    "linear",
					"timeInForce": string(order.TimeInForce),
					"orderLinkId": clientOrderID,
				},
			},
			"header": map[string]interface{}{
				"X-BAPI-TIMESTAMP":   timestamp,
				"X-BAPI-RECV-WINDOW": recvWindow,
				"X-BAPI-SIGN":        signature,
				"X-BAPI-API-KEY":     c.tradingWS.GetApiKey(),
			},
		}

		if err := c.tradingWS.SendJSON(orderMsg); err != nil {
			log.Error().Err(err).Msg("Failed to send order")
		} else {
			log.Info().Msgf("Order sent for %s with clientOrderID: %s", symbol, clientOrderID)
		}
	}()
	return clientOrderID, nil
}

func (c *BybitClient) CancelOrder(orderID string) error {
	go func(id string) {
		symbol := c.symbol
		timestamp := time.Now().UnixMilli()
		recvWindow := 5000
		signature := c.generateSignature(fmt.Sprintf("%d%d", timestamp, recvWindow))

		cancelMsg := struct {
			OP     string        `json:"op"`
			Args   []interface{} `json:"args"`
			Header map[string]interface{} `json:"header"`
		}{
			OP: "order.cancel",
			Args: []interface{}{
				map[string]interface{}{
					"orderId":  id,
					"category": "linear",
					"symbol":   symbol,
				},
			},
			Header: map[string]interface{}{
				"X-BAPI-TIMESTAMP":   timestamp,
				"X-BAPI-RECV-WINDOW": recvWindow,
				"X-BAPI-SIGN":        signature,
				"X-BAPI-API-KEY":     c.tradingWS.GetApiKey(),
			},
		}

		if err := c.tradingWS.SendJSON(cancelMsg); err != nil {
			log.Error().Err(err).Msg("Failed to send cancel order")
		} else {
			log.Info().Str("orderId", id).Str("symbol", symbol).Msg("Cancel order request sent")
		}
	}(orderID)
	return nil
}

func (c *BybitClient) AmendOrder(symbol, orderId string, newPrice, newQty int64) error {
	go func(sym, oid string, np, nq int64) {
		timestamp := time.Now().UnixMilli()
		recvWindow := 5000
		signature := c.generateSignature(fmt.Sprintf("%d%d", timestamp, recvWindow))

		type AmendArg struct {
			OrderID  string `json:"orderId"`
			Category string `json:"category"`
			Symbol   string `json:"symbol"`
			Price    string `json:"price,omitempty"`
			Qty      string `json:"qty,omitempty"`
		}

		amendMsg := struct {
			OP     string        `json:"op"`
			Args   []AmendArg    `json:"args"`
			Header map[string]interface{} `json:"header"`
		}{
			OP: "order.amend",
			Args: []AmendArg{
				{
					OrderID:  oid,
					Category: "linear",
					Symbol:   sym,
					Price:    fmt.Sprintf("%d", np),
					Qty:      fmt.Sprintf("%d", nq),
				},
			},
			Header: map[string]interface{}{
				"X-BAPI-TIMESTAMP":   timestamp,
				"X-BAPI-RECV-WINDOW": recvWindow,
				"X-BAPI-SIGN":        signature,
				"X-BAPI-API-KEY":     c.tradingWS.GetApiKey(),
			},
		}

		if err := c.tradingWS.SendJSON(amendMsg); err != nil {
			log.Error().Err(err).Msg("Failed to send amend order")
		} else {
			log.Info().Str("orderId", oid).Str("symbol", sym).Int64("newPrice", np).Int64("newQty", nq).Msg("Amend order request sent")
		}
	}(symbol, orderId, newPrice, newQty)
	return nil
}

func (c *BybitClient) Close() {
	c.publicWS.Close()
	c.tradingWS.Close()
	c.updatesWS.Close()
	log.Info().Msg("7:36AM INF All Bybit WebSockets closed successfully")
}

func (c *BybitClient) generateSignature(toSign string) string {
	apiSecret := c.tradingWS.GetApiSecret()
	h := hmac.New(sha256.New, []byte(apiSecret))
	h.Write([]byte(toSign))
	return hex.EncodeToString(h.Sum(nil))
}