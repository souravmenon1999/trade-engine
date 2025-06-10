package bybit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	zerologlog "github.com/rs/zerolog/log"
	"github.com/souravmenon1999/trade-engine/framedCopy/config"
	bybitWS "github.com/souravmenon1999/trade-engine/framedCopy/exchange/net/websockets/bybit"
	"github.com/souravmenon1999/trade-engine/framedCopy/exchange"
	"github.com/souravmenon1999/trade-engine/framedCopy/types"
)

// BybitClient manages WebSocket connections for Bybit exchange
type BybitClient struct {
	publicWS         *bybitWS.BybitWSClient
	tradingWS        *bybitWS.BybitWSClient
	updatesWS        *bybitWS.BybitWSClient
	subs             sync.Map
	symbol           string
	tradingHandler   exchange.TradingHandler
	executionHandler exchange.ExecutionHandler
	orderbookHandler exchange.OrderbookHandler 
	accountHandler   exchange.AccountHandler
}

func (c *BybitClient) SetTradingHandler(handler exchange.TradingHandler) {
	c.tradingHandler = handler
	zerologlog.Info().Msg("Set trading handler for Bybit")
}

func (c *BybitClient) SetExecutionHandler(handler exchange.ExecutionHandler) {
	c.executionHandler = handler
	zerologlog.Info().Msg("Set execution handler for Bybit")
}

func (c *BybitClient) SetOrderbookHandler(handler exchange.OrderbookHandler) {
    c.orderbookHandler = handler
    zerologlog.Info().Msg("Set orderbook handler for Bybit")
}

func (c *BybitClient) SetAccountHandler(handler exchange.AccountHandler) {
    c.accountHandler = handler
    zerologlog.Info().Msg("Set account handler for Bybit")
}

// NewBybitClient creates and initializes a Bybit client
func NewBybitClient(cfg *config.Config) *BybitClient {
	client := &BybitClient{
		publicWS:  bybitWS.NewBybitWSClient(cfg.BybitOrderbook.WSUrl, "", "", "public"),
		tradingWS: bybitWS.NewBybitWSClient(cfg.BybitExchangeClient.TradingWSUrl, cfg.BybitExchangeClient.APIKey, cfg.BybitExchangeClient.APISecret, "trading"),
		updatesWS: bybitWS.NewBybitWSClient(cfg.BybitExchangeClient.UpdatesWSUrl, cfg.BybitExchangeClient.APIKey, cfg.BybitExchangeClient.APISecret, "updates"),
		subs:      sync.Map{},
		symbol:    cfg.BybitOrderbook.Symbol,
	}
	zerologlog.Info().Msg("BybitClient initialized with WebSocket configurations")

	if err := client.Connect(); err != nil {
		zerologlog.Fatal().Err(err).Msg("Failed to connect to Bybit WebSockets during initialization")
	}

	if err := client.SubscribeAll(cfg); err != nil {
		zerologlog.Fatal().Err(err).Msg("Failed to subscribe to topics during initialization")
	}

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
	if c.executionHandler != nil {
		c.executionHandler.OnExecutionConnect()
	}
	return nil
}

// SubscribeAll subscribes to predefined topics for all WebSocket clients
func (c *BybitClient) SubscribeAll(cfg *config.Config) error {
	depth := cfg.BybitOrderbook.OrderbookDepth
    if depth <= 0 {
        depth = 50
    }
    orderBookTopic := fmt.Sprintf("orderbook.%d.%s", depth, cfg.BybitOrderbook.Symbol)
    if err := c.SubscribePublic(orderBookTopic, func(data []byte) {
        if c.orderbookHandler != nil {
            orderbook, err := parseBybitOrderbook(data, cfg.BybitOrderbook.Symbol)
            if err != nil {
                c.orderbookHandler.OnOrderbookError(fmt.Sprintf("Failed to parse orderbook: %v", err))
                return
            }
            c.orderbookHandler.OnOrderbook(orderbook)
        }
    }); err != nil {
        return fmt.Errorf("failed to subscribe to public topic %s: %w", orderBookTopic, err)
    }
    zerologlog.Info().Str("topic", orderBookTopic).Msg("Subscribed to public WebSocket topic")

	if err := c.SubscribeTrading("order", func(data []byte) {
		zerologlog.Info().Msgf("Received order update: %s", string(data))
	}); err != nil {
		return fmt.Errorf("failed to subscribe to trading topic order: %w", err)
	}
	zerologlog.Info().Str("topic", "order").Msg("Subscribed to trading WebSocket topic")

	if err := c.SubscribeUpdates("position", func(data []byte) {
		zerologlog.Info().Msgf("Received position update: %s", string(data))
	}); err != nil {
		return fmt.Errorf("failed to subscribe to updates topic position: %w", err)
	}
	zerologlog.Info().Str("topic", "position").Msg("Subscribed to updates WebSocket topic")

	if err := c.SubscribeUpdates("execution.fast", func(data []byte) {
		zerologlog.Info().Msgf("Received execution.fast update: %s", string(data))
	}); err != nil {
		return fmt.Errorf("failed to subscribe to execution.fast topic: %w", err)
	}
	zerologlog.Info().Str("topic", "execution.fast").Msg("Subscribed to execution.fast WebSocket topic")

	if err := c.SubscribeUpdates("wallet", func(data []byte) {
		zerologlog.Info().Msgf("Received account margin update: %s", string(data))
	}); err != nil {
		return fmt.Errorf("failed to subscribe to wallet topic: %w", err)
	}
	zerologlog.Info().Str("topic", "wallet").Msg("Subscribed to account margin updates")



	return nil
}

func parseBybitOrderbook(data []byte, symbol string) (*types.OrderBook, error) {
    var bybitData struct {
        Topic string `json:"topic"`
        Type  string `json:"type"`
        Data  struct {
            S   string     `json:"s"`
            B   [][]string `json:"b"`
            A   [][]string `json:"a"`
            U   int64      `json:"u"`
            Seq int64      `json:"seq"`
        } `json:"data"`
        Ts int64 `json:"ts"`
    }
    if err := json.Unmarshal(data, &bybitData); err != nil {
        return nil, fmt.Errorf("failed to unmarshal orderbook data: %w", err)
    }

    instrument := &types.Instrument{Symbol: symbol}
    exchange := types.ExchangeIDBybit // Assuming ExchangeIDBybit is defined
    orderbook := types.NewOrderBook(instrument, &exchange)

    // Parse bids
    for _, b := range bybitData.Data.B {
        price, err := strconv.ParseFloat(b[0], 64)
        if err != nil {
            return nil, fmt.Errorf("invalid bid price: %v", err)
        }
        quantity, err := strconv.ParseFloat(b[1], 64)
        if err != nil {
            return nil, fmt.Errorf("invalid bid quantity: %v", err)
        }
        update := types.BidUpdate(price, quantity, 1) // Assuming 1 order per level
        orderbook.ApplyUpdate(update)
    }

    // Parse asks
    for _, a := range bybitData.Data.A {
        price, err := strconv.ParseFloat(a[0], 64)
        if err != nil {
            return nil, fmt.Errorf("invalid ask price: %v", err)
        }
        quantity, err := strconv.ParseFloat(a[1], 64)
        if err != nil {
            return nil, fmt.Errorf("invalid ask quantity: %v", err)
        }
        update := types.AskUpdate(price, quantity, 1) // Assuming 1 order per level
        orderbook.ApplyUpdate(update)
    }

    orderbook.SetLastUpdateTime(bybitData.Ts)
    orderbook.SetSequence(bybitData.Data.Seq)
    return orderbook, nil
}



func (c *BybitClient) generateSignature(toSign string) string {
	apiSecret := c.tradingWS.GetApiSecret()
	h := hmac.New(sha256.New, []byte(apiSecret))
	h.Write([]byte(toSign))
	return hex.EncodeToString(h.Sum(nil))
}

func (c *BybitClient) SubscribePublic(topic string, callback func([]byte)) error {
	return c.subscribe(c.publicWS, topic, callback)
}

func (c *BybitClient) SubscribeTrading(topic string, callback func([]byte)) error {
	return c.subscribe(c.tradingWS, topic, callback)
}

func (c *BybitClient) SubscribeUpdates(topic string, callback func([]byte)) error {
	return c.subscribe(c.updatesWS, topic, callback)
}

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

func (c *BybitClient) StartReading() {
	go c.readLoop(c.publicWS, "public")
	go c.readLoop(c.tradingWS, "trading")
	go c.readLoop(c.updatesWS, "updates")
}

func (c *BybitClient) readLoop(ws *bybitWS.BybitWSClient, wsType string) {
	for {
		message, err := ws.ReadMessage()
		if err != nil {
			zerologlog.Error().Err(err).Str("ws_type", wsType).Msg("Error reading message")
			if wsType == "trading" && c.tradingHandler != nil {
				c.tradingHandler.OnOrderDisconnect()
			}
			if wsType == "updates" && c.executionHandler != nil {
				c.executionHandler.OnExecutionDisconnect()
			}
			zerologlog.Info().Str("ws_type", wsType).Msg("Attempting to reconnect...")
			for i := 0; i < 5; i++ {
				if err := ws.Connect(); err == nil {
					zerologlog.Info().Str("ws_type", wsType).Msg("Reconnected successfully")
					if wsType == "trading" && c.tradingHandler != nil {
						c.tradingHandler.OnOrderConnect()
					}
					if wsType == "updates" && c.executionHandler != nil {
						c.executionHandler.OnExecutionConnect()
					}
					c.subs.Range(func(key, value interface{}) bool {
						topic := key.(string)
						if (ws == c.publicWS && strings.HasPrefix(topic, "orderbook.")) ||
							(ws == c.tradingWS && topic == "order") ||
							(ws == c.updatesWS && (topic == "position" || topic == "execution.fast" || topic == "wallet")) {
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



func (c *BybitClient) handleMessage(message []byte, wsType string) {
    var msg map[string]interface{}
    if err := json.Unmarshal(message, &msg); err != nil {
        zerologlog.Error().Err(err).Str("ws_type", wsType).Str("raw_message", string(message)).Msg("Error unmarshaling message")
        if wsType == "trading" && c.tradingHandler != nil {
            c.tradingHandler.OnOrderError(fmt.Sprintf("Error unmarshaling message: %v", err))
        }
        if wsType == "updates" && c.executionHandler != nil {
            c.executionHandler.OnExecutionError(fmt.Sprintf("Error unmarshaling message: %v", err))
        }
        return
    }

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

    topic, ok := msg["topic"].(string)
    if !ok {
        zerologlog.Warn().Str("ws_type", wsType).Str("raw_message", string(message)).Msg("Message without topic")
        return
    }

    if topic == "order" && c.tradingHandler != nil {
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
            case "Filled", "PartiallyFilled":
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

            var orderStatus types.OrderStatus
            switch status {
            case "New", "Created":
                orderStatus = types.OrderStatusOpen
            case "PartiallyFilled":
                orderStatus = types.OrderStatusPartiallyFilled
            case "Filled":
                orderStatus = types.OrderStatusFilled
            case "Cancelled":
                orderStatus = types.OrderStatusCancelled
            case "Rejected":
                orderStatus = types.OrderStatusRejected
            default:
                orderStatus = types.OrderStatusUnknown
            }

            update := &types.OrderUpdate{
                Success:         success,
                UpdateType:      updateType,
                Status:          orderStatus,
                ErrorMessage:    nil,
                RequestID:       &clientOrderID,
                ExchangeOrderID: &orderID,
                FillQty:         &fillQty,
                FillPrice:       &fillPrice,
                UpdatedAt:       time.Now().UnixMilli(),
                IsMaker:         orderData["isMaker"] == true,
                AmendType:       "",
                NewPrice:        nil,
                NewQty:          nil,
            }

            c.tradingHandler.OnOrderUpdate(update)
            zerologlog.Debug().Str("clientOrderID", clientOrderID).Str("updateType", string(updateType)).Msg("Dispatched order update")
        }
        return
    }

    if topic == "execution.fast" && c.executionHandler != nil {
        data, ok := msg["data"].([]interface{})
        if !ok || len(data) == 0 {
            zerologlog.Warn().Str("ws_type", wsType).Msg("Invalid execution.fast data")
            return
        }

        var updates []*types.OrderUpdate
        for _, item := range data {
            execData, ok := item.(map[string]interface{})
            if !ok {
                zerologlog.Warn().Str("ws_type", wsType).Msg("Invalid execution data item")
                continue
            }

            orderID, _ := execData["orderId"].(string)
            priceStr, _ := execData["price"].(string)
            qtyStr, _ := execData["qty"].(string)
            isMaker, _ := execData["isMaker"].(bool)
            timestamp, _ := execData["timestamp"].(float64)

            price, err := strconv.ParseFloat(priceStr, 64)
            if err != nil {
                zerologlog.Warn().Err(err).Str("price", priceStr).Msg("Invalid price")
                continue
            }
            qty, err := strconv.ParseFloat(qtyStr, 64)
            if err != nil {
                zerologlog.Warn().Err(err).Str("qty", qtyStr).Msg("Invalid quantity")
                continue
            }

            var requestID *string

            update := &types.OrderUpdate{
                Success:         true,
                UpdateType:      types.OrderUpdateTypeFill,
                Status:          types.OrderStatusFilled,
                ErrorMessage:    nil,
                RequestID:       requestID,
                ExchangeOrderID: &orderID,
                FillQty:         &qty,
                FillPrice:       &price,
                UpdatedAt:       int64(timestamp),
                IsMaker:         isMaker,
                AmendType:       "",
                NewPrice:        nil,
                NewQty:          nil,
            }

            updates = append(updates, update)
        }

        if len(updates) > 0 {
            c.executionHandler.OnExecutionUpdate(updates)
            zerologlog.Debug().Int("count", len(updates)).Msg("Dispatched execution updates")
        }
        return
    }

    // Handle wallet updates
    if topic == "wallet" && c.accountHandler != nil {
        walletUpdate, err := parseBybitWalletUpdate(message)
        if err != nil {
            zerologlog.Error().Err(err).Str("ws_type", wsType).Msg("Failed to parse wallet update")
            c.accountHandler.OnPositionError(fmt.Sprintf("Failed to parse wallet update: %v", err))
            return
        }
        c.accountHandler.OnAccountUpdate(walletUpdate)
        zerologlog.Debug().Msg("Dispatched wallet update")
    }

    // Handle position updates
    if topic == "position" && c.accountHandler != nil {
        positionUpdate, err := parseBybitPositionUpdate(message)
        if err != nil {
            zerologlog.Error().Err(err).Str("ws_type", wsType).Msg("Failed to parse position update")
            c.accountHandler.OnPositionError(fmt.Sprintf("Failed to parse position update: %v", err))
            return
        }
        c.accountHandler.OnPositionUpdate(positionUpdate)
        zerologlog.Debug().Msg("Dispatched position update")
    }

    if callback, ok := c.subs.Load(topic); ok {
        go callback.(func([]byte))(message)
    } else {
        zerologlog.Warn().Str("ws_type", wsType).Str("topic", topic).Msg("No callback for topic")
    }
}



func (c *BybitClient) SendOrder(order *types.Order) (string, error) {
	clientOrderID := order.ClientOrderID.String()
	go func() {
		symbol := order.Instrument.BaseCurrency + order.Instrument.QuoteCurrency
		side := strings.Title(string(order.Side)) // Capitalize "buy" to "Buy", "sell" to "Sell"
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

		cancelJSON, err := json.Marshal(cancelMsg)
		if err != nil {
			zerologlog.Error().Err(err).Msg("Failed to marshal cancel order message")
			return
		}

		zerologlog.Debug().Str("cancel_message", string(cancelJSON)).Msg("Sending cancel order to Bybit")
		if err := c.tradingWS.SendJSON(cancelJSON); err != nil {
			zerologlog.Error().Err(err).Msg("Failed to send cancel order")
		} else {
			zerologlog.Info().Str("orderId", id).Str("symbol", symbol).Msg("Cancel order request sent")
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

		amendJSON, err := json.Marshal(amendMsg)
		if err != nil {
			zerologlog.Error().Err(err).Msg("Failed to marshal amend order message")
			return
		}

		zerologlog.Debug().Str("amend_message", string(amendJSON)).Msg("Sending amend order to Bybit")
		if err := c.tradingWS.SendJSON(amendJSON); err != nil {
			zerologlog.Error().Err(err).Msg("Failed to send amend order")
		} else {
			zerologlog.Info().Str("orderId", oid).Str("symbol", sym).Int64("newPrice", np).Int64("newQty", nq).Msg("Amend order request sent")
		}
	}(symbol, orderId, newPrice, newQty)
	return nil
}

func (c *BybitClient) Close() {
	c.publicWS.Close()
	c.tradingWS.Close()
	c.updatesWS.Close()
	zerologlog.Info().Msg("All Bybit WebSockets closed")
}

func (c *BybitClient) PublicWS() *bybitWS.BybitWSClient {
	return c.publicWS
}

func parseBybitWalletUpdate(data []byte) (*types.AccountUpdate, error) {
    var walletData struct {
        Data []struct {
            WalletBalance float64 `json:"walletBalance"`
            AvailableBalance float64 `json:"availableBalance"`
        } `json:"data"`
    }
    if err := json.Unmarshal(data, &walletData); err != nil {
        return nil, fmt.Errorf("failed to unmarshal wallet data: %w", err)
    }
    if len(walletData.Data) == 0 {
        return nil, fmt.Errorf("no wallet data found")
    }
   exchangeID := types.ExchangeIDBybit // Create a local variable with the constant value
return &types.AccountUpdate{
    Exchange:   &exchangeID, // Use the address of the local variable
    AccountIM:  walletData.Data[0].WalletBalance,      // Initial margin as total balance
    AccountMM:  walletData.Data[0].AvailableBalance,   // Maintenance margin as available
}, nil
}

func parseBybitPositionUpdate(data []byte) (*types.Position, error) {
    var positionData struct {
        Data []struct {
            Symbol        string  `json:"symbol"`
            Side          string  `json:"side"`
            Size          float64 `json:"size"`
            EntryPrice    float64 `json:"entryPrice"`
            UnrealizedPnL float64 `json:"unrealisedPnl"`
        } `json:"data"`
    }
    if err := json.Unmarshal(data, &positionData); err != nil {
        return nil, fmt.Errorf("failed to unmarshal position data: %w", err)
    }
    if len(positionData.Data) == 0 {
        return nil, fmt.Errorf("no position data found")
    }
   pos := positionData.Data[0]
instrument := &types.Instrument{Symbol: pos.Symbol}
side := types.Side(strings.ToUpper(pos.Side))
exchangeID := types.ExchangeIDBybit // Create a local variable with the constant value
return &types.Position{
    Exchange:      &exchangeID, // Use the address of the local variable
    Instrument:    instrument,
    Side:          side,
    Quantity:      pos.Size,
    EntryPrice:    pos.EntryPrice,
    UnrealizedPnL: pos.UnrealizedPnL,
}, nil
}