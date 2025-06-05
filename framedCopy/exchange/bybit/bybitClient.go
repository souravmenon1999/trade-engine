package bybit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
	"strconv"
	"github.com/google/uuid"

	zerologlog "github.com/rs/zerolog/log"
	"github.com/souravmenon1999/trade-engine/framedCopy/config"
	bybitWS "github.com/souravmenon1999/trade-engine/framedCopy/exchange/net/websockets/bybit"
	"github.com/souravmenon1999/trade-engine/framedCopy/types"
)

// BybitClient manages WebSocket connections for Bybit exchange
type BybitClient struct {
	publicWS  *bybitWS.BybitWSClient
	tradingWS *bybitWS.BybitWSClient
	updatesWS *bybitWS.BybitWSClient
	subs      sync.Map
	symbol    string
	orderUpdateCallback func(*types.OrderUpdate) 
}

func (c *BybitClient) RegisterOrderUpdateCallback(callback func(*types.OrderUpdate)) {
	c.orderUpdateCallback = callback
	zerologlog.Info().Msg("Registered order update callback for Bybit")
}

// NewBybitClient creates and initializes a Bybit client, automatically connecting,
// subscribing to topics, and starting message reading
func NewBybitClient(cfg *config.Config) *BybitClient {
	// Initialize WebSocket clients
	client := &BybitClient{
		publicWS:  bybitWS.NewBybitWSClient(cfg.BybitOrderbook.WSUrl, "", "", "public"),
		tradingWS: bybitWS.NewBybitWSClient(cfg.BybitExchangeClient.TradingWSUrl, cfg.BybitExchangeClient.APIKey, cfg.BybitExchangeClient.APISecret, "trading"),
		updatesWS: bybitWS.NewBybitWSClient(cfg.BybitExchangeClient.UpdatesWSUrl, cfg.BybitExchangeClient.APIKey, cfg.BybitExchangeClient.APISecret, "updates"),
		subs:      sync.Map{},
		symbol:    cfg.BybitOrderbook.Symbol,
	}
	zerologlog.Info().Msg("BybitClient initialized with WebSocket configurations")

	// Connect to WebSockets
	if err := client.Connect(); err != nil {
		zerologlog.Fatal().Err(err).Msg("Failed to connect to Bybit WebSockets during initialization")
	}

	// Subscribe to topics
	if err := client.SubscribeAll(cfg); err != nil {
		zerologlog.Fatal().Err(err).Msg("Failed to subscribe to topics during initialization")
	}

	// Start reading messages
	client.StartReading()
	zerologlog.Info().Msg("Started reading messages from all Bybit WebSocket clients")

	return client
}

// Connect establishes connections to all WebSocket clients
func (c *BybitClient) Connect() error {
	if err := c.publicWS.Connect(); err != nil {
		return fmt.Errorf("failed to connect public WS: %w", err)
	}
	zerologlog.Info().Msg("Public Bybit WebSocket connected")
	if err := c.tradingWS.Connect(); err != nil {
		return fmt.Errorf("failed to connect trading WS: %w", err)
	}
	zerologlog.Info().Msg("Trading Bybit WebSocket connected")
	if err := c.updatesWS.Connect(); err != nil {
		return fmt.Errorf("failed to connect updates WS: %w", err)
	}
	zerologlog.Info().Msg("Updates Bybit WebSocket connected")
	return nil
}

// SubscribeAll subscribes to predefined topics for all WebSocket clients
// SubscribeAll subscribes to predefined topics for all WebSocket clients
func (c *BybitClient) SubscribeAll(cfg *config.Config) error {
	depth := cfg.BybitOrderbook.OrderbookDepth
    if depth <= 0 {
        depth = 50 // Default to 50 if not set or invalid
    }
	// Public WebSocket: Order book
	orderBookTopic := fmt.Sprintf("orderbook.%d.%s", depth, cfg.BybitOrderbook.Symbol)
	if err := c.SubscribePublic(orderBookTopic, func(data []byte) {
		// zerologlog.Info().Msgf("Received order book update: %s", string(data))
	}); err != nil {
		return fmt.Errorf("failed to subscribe to public topic %s: %w", orderBookTopic, err)
	}
	zerologlog.Info().Str("topic", orderBookTopic).Msg("Subscribed to public WebSocket topic with logging callback")
	
	// tickersTopic := "tickers." + cfg.BybitOrderbook.Symbol
    // if err := c.SubscribePublic(tickersTopic, func(data []byte) {
    //     zerologlog.Info().Msgf("Received funding rate update for %s: %s", cfg.BybitOrderbook.Symbol, string(data))
    // }); err != nil {
    //     return fmt.Errorf("failed to subscribe to %s: %w", tickersTopic, err)
    // }
    // zerologlog.Info().Str("topic", tickersTopic).Msg("Subscribed to funding rate updates")


	// Trading WebSocket: Order updates
	if err := c.SubscribeTrading("order", func(data []byte) {
		zerologlog.Info().Msgf("Received order update: %s", string(data))
	}); err != nil {
		return fmt.Errorf("failed to subscribe to trading topic order: %w", err)
	}
	zerologlog.Info().Str("topic", "order").Msg("Subscribed to trading WebSocket topic")

	// Updates WebSocket: Position updates
	if err := c.SubscribeUpdates("position", func(data []byte) {
		zerologlog.Info().Msgf("Received position update: %s", string(data))
	}); err != nil {
		return fmt.Errorf("failed to subscribe to updates topic position: %w", err)
	}
	zerologlog.Info().Str("topic", "position").Msg("Subscribed to updates WebSocket topic")


	 // New subscription: execution.fast
    if err := c.SubscribeUpdates("execution.fast", func(data []byte) {
        zerologlog.Info().Msgf("Received execution.fast update: %s", string(data))
    }); err != nil {
        return fmt.Errorf("failed to subscribe to execution.fast topic: %w", err)
    }
    zerologlog.Info().Str("topic", "execution.fast").Msg("Subscribed to execution.fast WebSocket topic")


	// New subscription: Wallet (account margin updates)
    if err := c.SubscribeUpdates("wallet", func(data []byte) {
        zerologlog.Info().Msgf("Received account margin update: %s", string(data))
    }); err != nil {
        return fmt.Errorf("failed to subscribe to wallet topic: %w", err)
    }
    zerologlog.Info().Str("topic", "wallet").Msg("Subscribed to account margin updates")

	return nil
}

func (c *BybitClient) generateSignature(toSign string) string {
	apiSecret := c.tradingWS.GetApiSecret() // Use the getter method
	h := hmac.New(sha256.New, []byte(apiSecret))
	h.Write([]byte(toSign))
	return hex.EncodeToString(h.Sum(nil))
}

// SubscribePublic subscribes to a public WebSocket topic
func (c *BybitClient) SubscribePublic(topic string, callback func([]byte)) error {
	return c.subscribe(c.publicWS, topic, callback)
}

// SubscribeTrading subscribes to a trading WebSocket topic
func (c *BybitClient) SubscribeTrading(topic string, callback func([]byte)) error {
	return c.subscribe(c.tradingWS, topic, callback)
}

// SubscribeUpdates subscribes to an updates WebSocket topic
func (c *BybitClient) SubscribeUpdates(topic string, callback func([]byte)) error {
	return c.subscribe(c.updatesWS, topic, callback)
}

// subscribe is a helper to subscribe to a topic on a specific WebSocket
func (c *BybitClient) subscribe(ws *bybitWS.BybitWSClient, topic string, callback func([]byte)) error {
	if _, loaded := c.subs.LoadOrStore(topic, callback); loaded {
		return fmt.Errorf("already subscribed to %s", topic)
	}
	if err := ws.Subscribe(topic); err != nil {
		c.subs.Delete(topic)
		return err
	}
	zerologlog.Debug().Str("topic", topic).Str("wsType", ws.Type()).Msg("Subscription request sent")
	return nil
}

// StartReading launches goroutines to read messages from all WebSocket clients
func (c *BybitClient) StartReading() {
	go c.readLoop(c.publicWS, "public")
	go c.readLoop(c.tradingWS, "trading")
	go c.readLoop(c.updatesWS, "updates")
}

// readLoop continuously reads messages from a WebSocket
func (c *BybitClient) readLoop(ws *bybitWS.BybitWSClient, wsType string) {
	for {
		message, err := ws.ReadMessage()
		if err != nil {
			zerologlog.Error().Err(err).Str("ws_type", wsType).Msg("Error reading message")
			zerologlog.Info().Str("ws_type", wsType).Msg("Attempting to reconnect...")
			for i := 0; i < 5; i++ {
				if err := ws.Connect(); err == nil {
					zerologlog.Info().Str("ws_type", wsType).Msg("Reconnected successfully")
					// Re-subscribe to topics
					c.subs.Range(func(key, value interface{}) bool {
						topic := key.(string)
						if (ws == c.publicWS && topic == "orderbook.50.ETHUSDT") ||
							(ws == c.tradingWS && topic == "order") ||
							(ws == c.updatesWS && topic == "position") {
							if err := ws.Subscribe(topic); err != nil {
								zerologlog.Error().Err(err).Str("topic", topic).Msg("Failed to re-subscribe after reconnect")
							} else {
								zerologlog.Info().Str("topic", topic).Msg("Re-subscribed after reconnect")
							}
						}
						return true
					})
					break
				}
				zerologlog.Error().Err(err).Str("ws_type", wsType).Int("attempt", i+1).Msg("Reconnect attempt failed")
				time.Sleep(2 * time.Second)
			}
			continue
		}
		c.handleMessage(message, wsType)
	}
}

// handleMessage processes incoming WebSocket messages
func (c *BybitClient) handleMessage(message []byte, wsType string) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		zerologlog.Error().Err(err).Str("ws_type", wsType).Str("raw_message", string(message)).Msg("Error unmarshaling message")
		return
	}

	// Handle operation messages (auth, subscribe, ping)
	if op, ok := msg["op"].(string); ok {
		switch op {
		case "auth":
			zerologlog.Debug().Str("ws_type", wsType).Interface("auth_response", msg).Msg("Received authentication response")
			if retCode, ok := msg["retCode"].(float64); ok {
				if retCode == 0 {
					zerologlog.Info().Str("ws_type", wsType).Msg("Authentication successful")
				} else {
					zerologlog.Error().Str("ws_type", wsType).Interface("auth_response", msg).Msg("Authentication failed")
				}
			} else if success, ok := msg["success"].(bool); ok {
				if success {
					zerologlog.Info().Str("ws_type", wsType).Msg("Authentication successful")
				} else {
					zerologlog.Error().Str("ws_type", wsType).Interface("auth_response", msg).Msg("Authentication failed")
				}
			}
		case "subscribe":
			zerologlog.Info().Str("ws_type", wsType).Interface("subscribe_response", msg).Msg("Received subscription confirmation")
		case "ping":
			zerologlog.Debug().Str("ws_type", wsType).Msg("Received ping")
		default:
			zerologlog.Warn().Str("ws_type", wsType).Str("op", op).Str("raw_message", string(message)).Msg("Unknown operation")
		}
		return
	}

	// Handle topic-based messages
	topic, ok := msg["topic"].(string)
	if !ok {
		zerologlog.Warn().Str("ws_type", wsType).Str("raw_message", string(message)).Msg("Message without topic")
		return
	}

	if topic == "order" && c.orderUpdateCallback != nil {
		data, ok := msg["data"].([]interface{})
		if !ok || len(data) == 0 {
			zerologlog.Warn().Str("ws_type", wsType).Msg("Invalid order update data")
			return
		}

		for _, item := range data {
			orderData, ok := item.(map[string]interface{})
			if !ok {
				zerologlog.Warn().Str("ws_type", wsType).Msg("Invalid order data item")
				continue
			}

			clientOrderID, ok := orderData["orderLinkId"].(string)
			if !ok || clientOrderID == "" {
				zerologlog.Warn().Str("ws_type", wsType).Msg("Missing or invalid orderLinkId")
				continue
			}

			// Create a minimal Order for the update (strategy.go will look up the full Order)
			minimalOrder := &types.Order{
				ClientOrderID: uuid.MustParse(clientOrderID),
				ExchangeID:    types.ExchangeIDBybit,
			}

			orderID, _ := orderData["orderId"].(string)
			status, _ := orderData["orderStatus"].(string)
			price, _ := orderData["price"].(string)
			filledQty, _ := orderData["cumExecQty"].(string)

			var updateType types.OrderUpdateType
			var success bool
			switch status {
			case "New", "Created":
				updateType = types.OrderUpdateTypeCreated
				success = true
			case "Filled":
				updateType = types.OrderUpdateTypeFill
				success = true
			case "PartiallyFilled":
				updateType = types.OrderUpdateTypeFill
				success = true
			case "Cancelled":
				updateType = types.OrderUpdateTypeCanceled
				success = true
			case "Rejected":
				updateType = types.OrderUpdateTypeRejected
				success = false
			default:
				zerologlog.Warn().Str("status", status).Msg("Unknown order status")
				continue
			}

			fillQty, err := strconv.ParseFloat(filledQty, 64)
			if err != nil {
				zerologlog.Warn().Err(err).Str("filledQty", filledQty).Msg("Invalid filled quantity")
				continue
			}
			fillPrice, err := strconv.ParseFloat(price, 64)
			if err != nil {
				zerologlog.Warn().Err(err).Str("price", price).Msg("Invalid price")
				continue
			}
			requestID := clientOrderID

			update := types.NewOrderUpdate(
				minimalOrder,
				updateType,
				success,
				time.Now().UnixMilli(),
			)
			update.ExchangeOrderID = &orderID
			update.FillQty = &fillQty
			update.FillPrice = &fillPrice
			update.RequestID = &requestID
			update.IsMaker = orderData["isMaker"] == true

			c.orderUpdateCallback(update)
			zerologlog.Debug().Str("clientOrderID", clientOrderID).Str("updateType", string(updateType)).Msg("Dispatched order update")
		}
		return
	}

	// Handle other topics via subscription callbacks
	if callback, ok := c.subs.Load(topic); ok {
		go callback.(func([]byte))(message)
		// zerologlog.Debug().Str("ws_type", wsType).Str("topic", topic).Msg("Dispatched message to callback")
	} else {
		zerologlog.Warn().Str("ws_type", wsType).Str("topic", topic).Msg("No callback for topic")
	}
}

// SendOrder sends a trading order (unchanged)
func (c *BybitClient) SendOrder(order *types.Order) (string, error) {
	clientOrderID := order.ClientOrderID.String()
	go func() {
		symbol := order.Instrument.BaseCurrency + order.Instrument.QuoteCurrency
		side := string(order.Side)
		price := order.GetPrice() // Unscaled price
		quantity := order.GetQuantity() // Unscaled quantity

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

		orderJSON, err := json.Marshal(orderMsg)
		if err != nil {
			zerologlog.Error().Err(err).Msg("Failed to marshal order message")
			return
		}
		zerologlog.Debug().Str("order_message", string(orderJSON)).Msg("Sending order to Bybit")

		if err := c.tradingWS.SendJSON(orderMsg); err != nil {
			zerologlog.Error().Err(err).Msg("Failed to send order")
		} else {
			zerologlog.Info().Msgf("Order sent for %s with clientOrderID: %s", symbol, clientOrderID)
		}
	}()

	return clientOrderID, nil
}

// CancelOrder cancels an order asynchronously (fire-and-forget)
func (c *BybitClient) CancelOrder(orderID string) error {
    // Launch cancellation in goroutine
    go func(id string) {
        // Copy necessary values to avoid closure issues
        symbol := c.symbol
        
        timestamp := time.Now().UnixMilli()
        recvWindow := 5000
        toSign := fmt.Sprintf("%d%d", timestamp, recvWindow)
        signature := c.generateSignature(toSign)

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

        cancelJSON, err := json.Marshal(cancelMsg)
        if err != nil {
            zerologlog.Error().Err(err).Msg("Failed to marshal cancel order message")
            return
        }

        zerologlog.Debug().Str("cancel_message", string(cancelJSON)).Msg("Sending cancel order to Bybit")
        
        if err := c.tradingWS.SendJSON(cancelMsg); err != nil {
            zerologlog.Error().Err(err).Msg("Failed to send cancel order")
        } else {
            zerologlog.Info().Str("orderId", id).Str("symbol", symbol).Msg("Cancel order request sent")
        }
    }(orderID)  // Pass orderID as parameter

    return nil
}

// AmendOrder amends an order asynchronously (fire-and-forget)
func (c *BybitClient) AmendOrder(symbol, orderId string, newPrice, newQty int64) error {
    // Launch amendment in goroutine
    go func(sym, oid string, np, nq int64) {
        timestamp := time.Now().UnixMilli()
        recvWindow := 5000
        toSign := fmt.Sprintf("%d%d", timestamp, recvWindow)
        signature := c.generateSignature(toSign)

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
                    Qty:      ".01",
                },
            },
            Header: map[string]interface{}{
                "X-BAPI-TIMESTAMP":   timestamp,
                "X-BAPI-RECV-WINDOW": recvWindow,
                "X-BAPI-SIGN":        signature,
                "X-BAPI-API-KEY":     c.tradingWS.GetApiKey(),
            },
        }

        amendJSON, err := json.Marshal(amendMsg)
        if err != nil {
            zerologlog.Error().Err(err).Msg("Failed to marshal amend order message")
            return
        }

        zerologlog.Debug().Str("amend_message", string(amendJSON)).Msg("Sending amend order to Bybit")
        
        if err := c.tradingWS.SendJSON(amendMsg); err != nil {
            zerologlog.Error().Err(err).Msg("Failed to send amend order")
        } else {
            zerologlog.Info().Str("orderId", oid).Str("symbol", sym).Int64("newPrice", np).Int64("newQty", nq).Msg("Amend order request sent")
        }
    }(symbol, orderId, newPrice, newQty)  // Pass parameters

    return nil
}


// Close shuts down all WebSocket connections
func (c *BybitClient) Close() {
	c.publicWS.Close()
	c.tradingWS.Close()
	c.updatesWS.Close()
	zerologlog.Info().Msg("All Bybit WebSockets closed")
}

// PublicWS returns the public WebSocket client
func (c *BybitClient) PublicWS() *bybitWS.BybitWSClient {
	return c.publicWS
}
