package bybit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"sync"
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
	fundingRateHandler exchange.FundingRateHandler
	accountHandler   exchange.AccountHandler
	apiKey           string
	apiSecret        string
	onReadyCallback  func() // Callback to trigger connection
    connectionMu     sync.Mutex
    isConnected      bool
    connectionCallbacks []func() // Callbacks for connection established
}

func NewBybitClient(cfg *config.Config, onReadyCallback func()) *BybitClient {
	publicWS := baseWS.NewBaseWSClient(cfg.BybitOrderbook.WSUrl, "", "")
	tradingWS := baseWS.NewBaseWSClient(cfg.BybitExchangeClient.TradingWSUrl, cfg.BybitExchangeClient.APIKey, cfg.BybitExchangeClient.APISecret)
	updatesWS := baseWS.NewBaseWSClient(cfg.BybitExchangeClient.UpdatesWSUrl, cfg.BybitExchangeClient.APIKey, cfg.BybitExchangeClient.APISecret)

	client := &BybitClient{
		publicWS:   publicWS,
		tradingWS:  tradingWS,
		updatesWS:  updatesWS,
		symbol:     cfg.BybitOrderbook.Symbol,
		apiKey:     cfg.BybitExchangeClient.APIKey,
		apiSecret:  cfg.BybitExchangeClient.APISecret,
		 onReadyCallback:  onReadyCallback,
        isConnected:      false,
        connectionCallbacks: []func(){},

		
	}

	// Register handlers
	mdHandler := NewBybitMDHandler(client, client.symbol)
	publicWS.RegisterHandler(fmt.Sprintf("orderbook.%d.%s", cfg.BybitOrderbook.OrderbookDepth, client.symbol), mdHandler)
	publicWS.RegisterHandler(fmt.Sprintf("tickers.%s", client.symbol), mdHandler)

	tradingHandler := NewBybitTradingHandler(client.tradingHandler)
	tradingWS.RegisterHandler("order", tradingHandler)

	privateHandler := NewBybitPrivateHandler(client)
	updatesWS.RegisterHandler("position", privateHandler)
	updatesWS.RegisterHandler("execution.fast", privateHandler)
	updatesWS.RegisterHandler("wallet", privateHandler)
	updatesWS.RegisterHandler("order", privateHandler)



	

	return client
}


// SetOnReadyCallback allows setting the callback after creation
func (c *BybitClient) SetOnReadyCallback(callback func()) {
    c.onReadyCallback = callback
}

func (c *BybitClient) Connect() error {
    const maxRetries = 5
    const retryDelay = 2 * time.Second

    for i := 0; i < maxRetries; i++ {
        err := c.connectAttempt()
        if err == nil {
            c.connectionMu.Lock()
            c.isConnected = true
            callbacks := c.connectionCallbacks
            c.connectionMu.Unlock()
            for _, cb := range callbacks {
                cb()
            }
            log.Info().Msg("Bybit WebSockets connected")
            return nil
        }
        log.Error().Err(err).Int("attempt", i+1).Msg("Failed to connect Bybit WebSockets")
        if i < maxRetries-1 {
            time.Sleep(retryDelay)
        }
    }
    return fmt.Errorf("failed to connect Bybit WebSockets after %d attempts", maxRetries)
}

func (c *BybitClient) connectAttempt() error {
    if err := c.publicWS.Connect(); err != nil {
        return fmt.Errorf("failed to connect public WS: %w", err)
    }
    if err := c.tradingWS.Connect(); err != nil {
        c.publicWS.Close()
        return fmt.Errorf("failed to connect trading WS: %w", err)
    }
    if err := c.updatesWS.Connect(); err != nil {
        c.publicWS.Close()
        c.tradingWS.Close()
        return fmt.Errorf("failed to connect updates WS: %w", err)
    }
    return nil
}

func (c *BybitClient) SubscribeAll(cfg *config.Config) error {

	log.Info().Str("symbol", c.symbol).Msg("Subscribing to Bybit public topics")
    // Subscribe publicWS to orderbook
   publicSub := map[string]interface{}{

		"op": "subscribe",
		"args": []interface{}{
			fmt.Sprintf("orderbook.%d.%s", cfg.BybitOrderbook.OrderbookDepth, c.symbol),
			fmt.Sprintf("tickers.%s", c.symbol), 
		},
	}
    if err := c.publicWS.Subscribe(publicSub); err != nil {
        return fmt.Errorf("failed to subscribe to public topic: %w", err)
    }

    // Subscribe updatesWS to private topics
    privateSub := map[string]interface{}{
        "op":   "subscribe",
        "args": []interface{}{
            "position",
            "execution",
            "wallet",
            "order",
        },
    }
    if err := c.updatesWS.Subscribe(privateSub); err != nil {
        return fmt.Errorf("failed to subscribe to private topics: %w", err)
    }

    return nil
}

// StartReading begins reading WebSocket messages
func (c *BybitClient) StartReading() {
    c.publicWS.Start()
    c.tradingWS.Start()
    c.updatesWS.Start()
}

func (c *BybitClient) GetOnReadyCallback() func() {
    return c.onReadyCallback
}

// IsConnected implements ConnectionState
func (c *BybitClient) IsConnected() bool {
    c.connectionMu.Lock()
    defer c.connectionMu.Unlock()
    return c.isConnected && c.publicWS.IsConnected() && c.tradingWS.IsConnected() && c.updatesWS.IsConnected()
}

// RegisterConnectionCallback implements ConnectionState
func (c *BybitClient) RegisterConnectionCallback(callback func()) {
    c.connectionMu.Lock()
    defer c.connectionMu.Unlock()
    if c.isConnected {
        callback() // Call immediately if already connected
    } else {
        c.connectionCallbacks = append(c.connectionCallbacks, callback)
    }
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

func (c *BybitClient) SetFundingRateHandler(handler exchange.FundingRateHandler) {
	c.fundingRateHandler = handler
	log.Info().Msg("Set funding rate handler for Bybit")
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