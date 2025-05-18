package bybit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
"strings"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
	"github.com/souravmenon1999/trade-engine/internal/config"
	"github.com/souravmenon1999/trade-engine/internal/exchange"
	"github.com/souravmenon1999/trade-engine/internal/logging"
	"github.com/souravmenon1999/trade-engine/internal/types"
)

// BybitExtendedConfig extends BybitConfig with trading and status WebSocket URLs and HedgeMode.
type BybitExtendedConfig struct {
    config.BybitConfig
    TradeWSURL  string `yaml:"trade_ws_url"`
    StatusWSURL string `yaml:"status_ws_url"`
    HedgeMode   bool   `yaml:"hedge_mode"`
}

// BybitMarketInfo holds market information for a symbol.
type BybitMarketInfo struct {
    Symbol        string          `json:"symbol"`
    BaseCoin      string          `json:"baseCoin"`
    QuoteCoin     string          `json:"quoteCoin"`
    PriceScale    string          `json:"priceScale"`
    QuantityScale string          `json:"qtyScale"`
    MinQuantity   decimal.Decimal `json:"minQty"`
    MaxQuantity   decimal.Decimal `json:"maxQty"`
}

// ParsedMarketInfo holds the parsed integer scales for internal use.
type ParsedMarketInfo struct {
    Symbol        string
    BaseCoin      string
    QuoteCoin     string
    PriceScale    int
    QuantityScale int
    MinQuantity   decimal.Decimal
    MaxQuantity   decimal.Decimal
}

// OrderInfo holds order details for tracking.
type OrderInfo struct {
    ClientOrderID string
    OrderID       string
    Side          types.Side
    Price         uint64
    Quantity      uint64
    Status        string
    CreatedAt     int64
}

// TradingClient implements the ExchangeClient interface for Bybit trading.
type TradingClient struct {
    cfg            *BybitExtendedConfig
    instrument     *types.Instrument
    logger         *slog.Logger
    ctx            context.Context
    cancel         context.CancelFunc
    marketInfoMu   sync.RWMutex
    marketInfo     map[string]*ParsedMarketInfo
    tradingWSConn  *websocket.Conn
    statusWSConn   *websocket.Conn
    wsMu           sync.Mutex
    httpClient     *http.Client
    ordersMu       sync.RWMutex
    activeOrders   map[string]*OrderInfo // clientOrderId -> OrderInfo
    shutdownComplete chan struct{}
}

func (c *TradingClient) GetMarketInfo(symbol string) *ParsedMarketInfo {
    return c.getMarketInfo(symbol)
}

// NewTradingClient initializes the trading client with WebSocket connections.
func NewTradingClient(ctx context.Context, cfg *BybitExtendedConfig) (*TradingClient, error) {
    if cfg == nil || cfg.Symbol == "" || cfg.TradingURL == "" || cfg.Category == "" || cfg.APIKey == "" || cfg.APISecret == "" {
        return nil, fmt.Errorf("Bybit config incomplete")
    }

    logger := logging.GetLogger().With("exchange", "bybit_trading")
    instrument := &types.Instrument{Symbol: cfg.Symbol}
    clientCtx, cancel := context.WithCancel(ctx)

    client := &TradingClient{
        cfg:          cfg,
        instrument:   instrument,
        httpClient:   &http.Client{Timeout: 15 * time.Second},
        logger:       logger,
        ctx:          clientCtx,
        cancel:       cancel,
        marketInfo:   make(map[string]*ParsedMarketInfo),
        activeOrders: make(map[string]*OrderInfo),
        shutdownComplete: make(chan struct{}),
    }

    // Fetch market info
    for attempt := 1; attempt <= 3; attempt++ {
        err := client.fetchMarketInfo(clientCtx, cfg.Symbol, cfg.Category)
        if err == nil {
            break
        }
        client.logger.Warn("Failed to fetch market info", "attempt", attempt, "error", err)
        if attempt < 3 {
            time.Sleep(time.Second * time.Duration(attempt))
        } else {
            client.logger.Error("All attempts to fetch market info failed", "error", err)
            client.marketInfoMu.Lock()
            client.marketInfo[cfg.Symbol] = &ParsedMarketInfo{
                Symbol:        cfg.Symbol,
                BaseCoin:      "ETH",
                QuoteCoin:     "USDT",
                PriceScale:    2,
                QuantityScale: 3,
                MinQuantity:   decimal.NewFromFloat(0.001),
                MaxQuantity:   decimal.NewFromFloat(1000),
            }
            client.marketInfoMu.Unlock()
            client.logger.Warn("Using default market info for ETHUSDT")
        }
    }

    info := client.getMarketInfo(cfg.Symbol)
    if info != nil {
        client.instrument.BaseCurrency = types.Currency(info.BaseCoin)
        client.instrument.QuoteCurrency = types.Currency(info.QuoteCoin)
        minQtyFloat, _ := info.MinQuantity.Mul(decimal.NewFromInt(1e6)).Float64()
        client.instrument.MinLotSize.Store(uint64(math.Round(minQtyFloat)))
    }

    // Connect trading WebSocket
    if err := client.connectTradingWebSocket(); err != nil {
        client.logger.Error("Failed to initialize trading WebSocket", "error", err)
        cancel()
        return nil, err
    }

    // Connect status WebSocket
    if err := client.connectStatusWebSocket(); err != nil {
        client.logger.Error("Failed to initialize status WebSocket", "error", err)
        cancel()
        return nil, err
    }

    // Initialize active orders
    client.initializeActiveOrders()

    client.logger.Info("Bybit trading client initialized")
    return client, nil
}

// fetchMarketInfo fetches market info via REST API.
func (c *TradingClient) fetchMarketInfo(ctx context.Context, symbol, category string) error {
    url := fmt.Sprintf("%s/v5/market/instruments-info?category=%s&symbol=%s", c.cfg.TradingURL, category, symbol)
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return fmt.Errorf("failed to create request: %w", err)
    }

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return fmt.Errorf("failed to fetch market info: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("API returned non-200 status: %d", resp.StatusCode)
    }

    bodyBytes, err := io.ReadAll(resp.Body)
    if err != nil {
        return fmt.Errorf("failed to read response body: %w", err)
    }

    var result struct {
        RetCode int    `json:"retCode"`
        RetMsg  string `json:"retMsg"`
        Result  struct {
            List []BybitMarketInfo `json:"list"`
        } `json:"result"`
    }
    if err := json.Unmarshal(bodyBytes, &result); err != nil {
        c.logger.Error("Failed to decode market info response", "raw_response", string(bodyBytes), "error", err)
        return fmt.Errorf("failed to decode response: %w", err)
    }

    if result.RetCode != 0 {
        c.logger.Error("API error response", "raw_response", string(bodyBytes), "retCode", result.RetCode, "retMsg", result.RetMsg)
        return fmt.Errorf("API error: %s (code: %d)", result.RetMsg, result.RetCode)
    }

    c.marketInfoMu.Lock()
    defer c.marketInfoMu.Unlock()
    for _, info := range result.Result.List {
        priceScaleStr := info.PriceScale
        if priceScaleStr == "" {
            priceScaleStr = "2"
            c.logger.Warn("Empty priceScale, using default", "symbol", info.Symbol, "default", priceScaleStr)
        }
        quantityScaleStr := info.QuantityScale
        if quantityScaleStr == "" {
            quantityScaleStr = "3"
            c.logger.Warn("Empty qtyScale, using default", "symbol", info.Symbol, "default", quantityScaleStr)
        }

        priceScale, err := strconv.Atoi(priceScaleStr)
        if err != nil {
            c.logger.Error("Invalid priceScale", "symbol", info.Symbol, "value", priceScaleStr, "raw_response", string(bodyBytes), "error", err)
            return fmt.Errorf("invalid priceScale for %s: %w", info.Symbol, err)
        }
        quantityScale, err := strconv.Atoi(quantityScaleStr)
        if err != nil {
            c.logger.Error("Invalid qtyScale", "symbol", info.Symbol, "value", quantityScaleStr, "raw_response", string(bodyBytes), "error", err)
            return fmt.Errorf("invalid qtyScale for %s: %w", info.Symbol, err)
        }
        c.marketInfo[info.Symbol] = &ParsedMarketInfo{
            Symbol:        info.Symbol,
            BaseCoin:      info.BaseCoin,
            QuoteCoin:     info.QuoteCoin,
            PriceScale:    priceScale,
            QuantityScale: quantityScale,
            MinQuantity:   info.MinQuantity,
            MaxQuantity:   info.MaxQuantity,
        }
    }
    c.logger.Info("Successfully fetched market info for symbol", "symbol", symbol)
    return nil
}

// getMarketInfo retrieves market info.
func (c *TradingClient) getMarketInfo(symbol string) *ParsedMarketInfo {
    c.marketInfoMu.RLock()
    defer c.marketInfoMu.RUnlock()
    return c.marketInfo[symbol]
}

// formatPrice formats price based on scale.
func (c *TradingClient) formatPrice(price uint64, symbol string) (string, error) {
    info := c.getMarketInfo(symbol)
    if info == nil {
        return "", fmt.Errorf("no market info for %s", symbol)
    }
    // Convert from micros (1e6) to actual value, then apply price scale
    priceDec := decimal.NewFromInt(int64(price)).Div(decimal.NewFromInt(1e6))
    priceDec = priceDec.Round(int32(info.PriceScale))
    return priceDec.StringFixed(int32(info.PriceScale)), nil
}

func (c *TradingClient) formatQuantity(quantity uint64, symbol string) (string, error) {
    info := c.getMarketInfo(symbol)
    if info == nil {
        return "", fmt.Errorf("no market info for %s", symbol)
    }
    // Convert from micros (1e6) to actual value, then apply quantity scale
    qtyDec := decimal.NewFromInt(int64(quantity)).Div(decimal.NewFromInt(1e6))
    qtyDec = qtyDec.Round(int32(info.QuantityScale))
    return qtyDec.StringFixed(int32(info.QuantityScale)), nil
}

// connectTradingWebSocket establishes the trading WebSocket connection.
func (c *TradingClient) connectTradingWebSocket() error {
    conn, _, err := websocket.DefaultDialer.DialContext(c.ctx, c.cfg.TradeWSURL, nil)
    if err != nil {
        return fmt.Errorf("trading WebSocket connection failed: %w", err)
    }
    c.tradingWSConn = conn

    expires := time.Now().UnixMilli() + 10000
    signature := c.signWebSocketAuth(expires)
    authMsg := map[string]interface{}{
    "op": "auth",
    "args": []interface{}{
        c.cfg.APIKey,
        fmt.Sprintf("%d", expires), // Convert expires to string
        signature,
    },
}
    if err := c.sendTradingWebSocketMessage(authMsg); err != nil {
        conn.Close()
        return fmt.Errorf("trading WebSocket auth failed: %w", err)
    }

    return nil
}

// connectStatusWebSocket establishes the status WebSocket connection.
func (c *TradingClient) connectStatusWebSocket() error {
    conn, _, err := websocket.DefaultDialer.DialContext(c.ctx, c.cfg.StatusWSURL, nil)
    if err != nil {
        return fmt.Errorf("status WebSocket connection failed: %w", err)
    }
    c.statusWSConn = conn

    expires := time.Now().UnixMilli() + 10000
    signature := c.signWebSocketAuth(expires)
  authMsg := map[string]interface{}{
    "op": "auth",
    "args": []interface{}{
        c.cfg.APIKey,
        fmt.Sprintf("%d", expires), // Ensure timestamp is string
        signature,
    },
}
    if err := c.sendStatusWebSocketMessage(authMsg); err != nil {
        conn.Close()
        return fmt.Errorf("status WebSocket auth failed: %w", err)
    }

   // In trading_client.go:
// In connectStatusWebSocket(), change the subscription to:
subMsg := map[string]interface{}{
    "op": "subscribe",
    "args": []string{
        "order",     // Private order updates
        "execution", // Private execution updates
    },
}
    if err := c.sendStatusWebSocketMessage(subMsg); err != nil {
        conn.Close()
        return fmt.Errorf("status WebSocket subscription failed: %w", err)
    }

    go c.readStatusWebSocketMessages()
    return nil
}

// signWebSocketAuth generates the authentication signature.
// Replace the existing signWebSocketAuth function
func (c *TradingClient) signWebSocketAuth(expires int64) string {
    // Correct format: "GET/realtime" + expires (as string)
    val := fmt.Sprintf("GET/realtime%d", expires)
    h := hmac.New(sha256.New, []byte(c.cfg.APISecret))
    h.Write([]byte(val))
    return hex.EncodeToString(h.Sum(nil))
}

// sendTradingWebSocketMessage sends a message via trading WebSocket.
func (c *TradingClient) sendTradingWebSocketMessage(msg interface{}) error {
    c.wsMu.Lock()
    defer c.wsMu.Unlock()
    if c.tradingWSConn == nil {
        return fmt.Errorf("trading WebSocket not connected")
    }
    return c.tradingWSConn.WriteJSON(msg)
}

// sendStatusWebSocketMessage sends a message via status WebSocket.
func (c *TradingClient) sendStatusWebSocketMessage(msg interface{}) error {
    c.wsMu.Lock()
    defer c.wsMu.Unlock()
    if c.statusWSConn == nil {
        return fmt.Errorf("status WebSocket not connected")
    }
    return c.statusWSConn.WriteJSON(msg)
}

// readStatusWebSocketMessages processes status WebSocket messages.
func (c *TradingClient) readStatusWebSocketMessages() {
    defer c.logger.Info("Status WebSocket read loop stopped")
    for {
        select {


        case <-c.ctx.Done():
            c.logger.Info("Status WebSocket read loop received context done")
            return
        
        

        default:
            c.logger.Debug("Status WebSocket read loop: Waiting for message")

            // Read raw message
            _, message, err := c.statusWSConn.ReadMessage()
            if err != nil {
                c.logger.Error("Status WebSocket read error", "error", err)
                return
            }

            // Log raw message
            c.logger.Debug("Raw status message", "message", string(message))

            // Parse JSON message
            var msg map[string]interface{}
            if err := json.Unmarshal(message, &msg); err != nil {
                c.logger.Error("Failed to parse WebSocket message", 
                    "error", err, 
                    "raw", string(message))
                continue
            }

            // Handle operational messages (auth, subscribe responses)
            if op, ok := msg["op"].(string); ok {
                success, _ := msg["success"].(bool)
                retMsg, _ := msg["ret_msg"].(string)
                
                if success {
                    c.logger.Debug("WebSocket operation succeeded",
                        "op", op,
                        "message", retMsg)
                } else {
                    c.logger.Error("WebSocket operation failed",
                        "op", op,
                        "error", retMsg)
                }
                
                // Handle pong responses
                if op == "pong" {
                    c.logger.Debug("Received pong response")
                }
                continue
            }

            // Handle topic-based messages
            topic, ok := msg["topic"].(string)
            if !ok {
                c.logger.Warn("Received message without topic", "msg", msg)
                continue
            }

            // Process order updates
          if topic == "order" {
    c.logger.Debug("Processing order update", "topic", topic)

                // Extract order data array
                data, ok := msg["data"].([]interface{})
                if !ok {
                    c.logger.Error("Invalid order data format", "data", msg["data"])
                    continue
                }

                c.logger.Debug("Received order updates", "count", len(data))

                // Process each order in the data array
                for _, item := range data {
                    order, ok := item.(map[string]interface{})
                    if !ok {
                        c.logger.Error("Invalid order format", "item", item)
                        continue
                    }

                    // Validate required fields
                    clientOrderID, _ := order["orderLinkId"].(string)
                    orderID, okID := order["orderId"].(string)
                    status, okStatus := order["orderStatus"].(string)
                    
                    if !okID || orderID == "" {
                        c.logger.Error("Missing order ID in update",
                            "clientOrderID", clientOrderID,
                            "raw", order)
                        continue
                    }

                    if !okStatus {
                        c.logger.Error("Missing order status",
                            "orderID", orderID,
                            "clientOrderID", clientOrderID)
                        continue
                    }

                    // --- Order processing logic ---
                    c.ordersMu.Lock()
                    
                    // Check if order exists in active orders
                    existingOrder, exists := c.activeOrders[clientOrderID]

                    // Handle terminal states
                    terminalStates := map[string]bool{
                        "Filled":    true,
                        "Cancelled": true,
                        "Rejected":  true,
                    }

                    if terminalStates[status] {
                        if exists {
                            c.logger.Info("Removing terminal order",
                                "clientOrderID", clientOrderID,
                                "orderID", orderID,
                                "status", status)
                            delete(c.activeOrders, clientOrderID)
                        }
                        c.ordersMu.Unlock()
                        continue
                    }

                    // Update or create order
                    if exists {
                        // Update existing order
                        existingOrder.OrderID = orderID
                        existingOrder.Status = status
                        c.logger.Debug("Updated active order",
                            "clientOrderID", clientOrderID,
                            "orderID", orderID,
                            "status", status)
                    } else {
                        // Create new order entry
                        priceStr, _ := order["price"].(string)
                        qtyStr, _ := order["qty"].(string)
                        sideStr, _ := order["side"].(string)
                        
                        price := parseDecimal(priceStr, c.logger)
                        quantity := parseDecimal(qtyStr, c.logger)
                        
                        c.activeOrders[clientOrderID] = &OrderInfo{
                            ClientOrderID: clientOrderID,
                            OrderID:       orderID,
                            Side:          parseSide(sideStr),
                            Price:         price,
                            Quantity:      quantity,
                            Status:        status,
                            CreatedAt:     parseTimestamp(order["createdTime"]),
                        }
                        c.logger.Info("Added new order from WS",
                            "clientOrderID", clientOrderID,
                            "orderID", orderID)
                    }
                    c.ordersMu.Unlock()
                }
            } else {
                c.logger.Debug("Ignoring non-order topic", "topic", topic)
            }
        }
    }
}

// Helper functions
func parseDecimal(s string, logger *slog.Logger) uint64 {
    dec, err := decimal.NewFromString(s)
    if err != nil {
        logger.Error("Failed to parse decimal", "value", s, "error", err)
        return 0
    }
    scaled := dec.Mul(decimal.NewFromInt(1e6))
    return uint64(scaled.IntPart())
}

func parseSide(sideStr string) types.Side {
    if strings.EqualFold(sideStr, "sell") {
        return types.Sell
    }
    return types.Buy
}

func parseTimestamp(ts interface{}) int64 {
    if tsStr, ok := ts.(string); ok {
        if ts, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
            return ts
        }
    }
    return time.Now().UnixMilli()
}

// Helper functions for logging map keys/types (optional but helpful)
// (Keep these functions if you use the new logs)
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func getMapTypes(m map[string]interface{}) map[string]string {
	typesMap := make(map[string]string, len(m))
	for k, v := range m {
		typesMap[k] = fmt.Sprintf("%T", v)
	}
	return typesMap
}

// initializeActiveOrders fetches and cleans up orders on startup.
func (c *TradingClient) initializeActiveOrders() {
    orders := c.GetActiveOrders()
    var latestBuy, latestSell *OrderInfo
    for _, order := range orders {
        if order.Side == types.Buy {
            if latestBuy == nil || order.CreatedAt > latestBuy.CreatedAt {
                latestBuy = order
            }
        } else {
            if latestSell == nil || order.CreatedAt > latestSell.CreatedAt {
                latestSell = order
            }
        }
    }

    for _, order := range orders {
        if (order.Side == types.Buy && order != latestBuy) || (order.Side == types.Sell && order != latestSell) {
            c.CancelOrder(c.ctx, order.Side, order.ClientOrderID)
        }
    }

    c.ordersMu.Lock()
    if latestBuy != nil {
        c.activeOrders[latestBuy.ClientOrderID] = latestBuy
    }
    if latestSell != nil {
        c.activeOrders[latestSell.ClientOrderID] = latestSell
    }
    c.ordersMu.Unlock()
}

// GetActiveOrders returns current active orders.
func (c *TradingClient) GetActiveOrders() map[types.Side]*OrderInfo {
    c.ordersMu.RLock()
    defer c.ordersMu.RUnlock()
    result := make(map[types.Side]*OrderInfo)
    for _, order := range c.activeOrders {
        if order.Status == "New" || order.Status == "PartiallyFilled" {
            result[order.Side] = order
        }
    }
    return result
}

// CancelOrder cancels a specific order.
func (c *TradingClient) CancelOrder(ctx context.Context, side types.Side, clientOrderID string) error {
    
    c.ordersMu.RLock()
    order, exists := c.activeOrders[clientOrderID]
    c.ordersMu.RUnlock()
    if !exists || order.Status != "New" && order.Status != "PartiallyFilled" {
        return fmt.Errorf("order %s not active", clientOrderID)
    }
     if order.Status == "Filled" || order.Status == "Cancelled" {
        return fmt.Errorf("order %s in terminal state: %s", clientOrderID, order.Status)
    }

    reqID := uuid.New().String()
    bybitSide := "Buy"
    if side == types.Sell {
        bybitSide = "Sell"
    }
    positionIdx := 0
    if c.cfg.HedgeMode {
        positionIdx = 1 // Buy
        if side == types.Sell {
            positionIdx = 2 // Sell
        }
    }
    msg := map[string]interface{}{
        "reqId": reqID,
        "header": map[string]interface{}{
            "X-BAPI-TIMESTAMP":   fmt.Sprintf("%d", time.Now().UnixMilli()),
            "X-BAPI-RECV-WINDOW": "5000",
        },
        "op": "order.cancel",
        "args": []map[string]interface{}{
            {
                "category":      c.cfg.Category,
                "symbol":        c.cfg.Symbol,
              "orderLinkId": clientOrderID,
                "side":          bybitSide,
                "positionIdx":   positionIdx,
            },
        },
    }
    c.logger.Debug("Sending order.cancel message", "msg", msg)
    if err := c.sendTradingWebSocketMessage(msg); err != nil {
        return types.TradingError{
            Code:    types.ErrConnectionFailed,
            Message: "Failed to send order.cancel",
            Wrapped: err,
        }
    }
    c.logger.Info("Sent order.cancel command", "client_order_id", clientOrderID, "req_id", reqID)
    return nil
}

// CancelAllOrders sends an order.cancel command for all active orders.
func (c *TradingClient) CancelAllOrders(ctx context.Context, instrument *types.Instrument) error {
    if instrument == nil {
        return fmt.Errorf("instrument is nil")
    }
    orders := c.GetActiveOrders()
    for _, order := range orders {
        if err := c.CancelOrder(ctx, order.Side, order.ClientOrderID); err != nil {
            c.logger.Error("Failed to cancel order", "client_order_id", order.ClientOrderID, "error", err)
        }
    }
    return nil
}

// SubmitOrder sends an order.create command via trading WebSocket.
func (c *TradingClient) SubmitOrder(ctx context.Context, order types.Order) (string, error) {
    if order.Instrument == nil {
        return "", fmt.Errorf("order instrument is nil")
    }
    info := c.getMarketInfo(order.Instrument.Symbol)
    if info == nil {
        return "", fmt.Errorf("market info not found for %s", order.Instrument.Symbol)
    }

    priceStr, err := c.formatPrice(order.Price, order.Instrument.Symbol)
    if err != nil {
        return "", types.TradingError{Code: types.ErrOrderSubmissionFailed, Message: "Failed to format price", Wrapped: err}
    }
    qtyStr, err := c.formatQuantity(order.Quantity, order.Instrument.Symbol)
    if err != nil {
        return "", types.TradingError{Code: types.ErrOrderSubmissionFailed, Message: "Failed to format quantity", Wrapped: err}
    }

    bybitSide := "Buy"
    if order.Side == types.Sell {
        bybitSide = "Sell"
    }
    clientOrderID := uuid.New().String()
    reqID := uuid.New().String()
    positionIdx := 0
    if c.cfg.HedgeMode {
        positionIdx = 1 // Buy
        if order.Side == types.Sell {
            positionIdx = 2 // Sell
        }
    }
    msg := map[string]interface{}{
        "reqId": reqID,
        "header": map[string]interface{}{
            "X-BAPI-TIMESTAMP":   fmt.Sprintf("%d", time.Now().UnixMilli()),
            "X-BAPI-RECV-WINDOW": "5000",
        },
        "op": "order.create",
        "args": []map[string]interface{}{
            {
                "category":      c.cfg.Category,
                "symbol":        order.Instrument.Symbol,
                "side":          bybitSide,
                "orderType":     "Limit",
                "qty":           qtyStr,
                "price":         priceStr,
               "orderLinkId": clientOrderID,
                "timeInForce":   "GTC",
                "positionIdx":   positionIdx,
            },
        },
    }
    c.logger.Debug("Sending order.create message", "msg", msg)
    if err := c.sendTradingWebSocketMessage(msg); err != nil {
        return "", types.TradingError{
            Code:    types.ErrConnectionFailed,
            Message: "Failed to send order.create",
            Wrapped: err,
        }
    }
    c.ordersMu.Lock()
    c.activeOrders[clientOrderID] = &OrderInfo{
        ClientOrderID: clientOrderID,
        Side:          order.Side,
        Price:         order.Price,
        Quantity:      order.Quantity,
        Status:        "New",
        CreatedAt:     time.Now().UnixMilli(),
    }
    c.ordersMu.Unlock()
    c.logger.Info("Sent order.create command", "client_order_id", clientOrderID)
    return clientOrderID, nil
}

// AmendOrder amends an existing order.
func (c *TradingClient) AmendOrder(ctx context.Context, exchangeOrderID string, instrument *types.Instrument, price uint64, quantity uint64) (string, error) {
    if instrument == nil {
        return "", fmt.Errorf("instrument is nil")
    }
    c.ordersMu.RLock()
    order, exists := c.activeOrders[exchangeOrderID]
    c.ordersMu.RUnlock()
    if !exists || (order.Status != "New" && order.Status != "PartiallyFilled") {
        return "", fmt.Errorf("order %s not active", exchangeOrderID)
    }
c.logger.Debug(fmt.Sprintf("price1: %v", price))
    priceStr, err := c.formatPrice(price, instrument.Symbol)
    if err != nil {
        return "", types.TradingError{Code: types.ErrOrderSubmissionFailed, Message: "Failed to format price", Wrapped: err}
    }
    qtyStr, err := c.formatQuantity(quantity, instrument.Symbol)
    if err != nil {
        return "", types.TradingError{Code: types.ErrOrderSubmissionFailed, Message: "Failed to format quantity", Wrapped: err}
    }

    reqID := uuid.New().String()
    msg := map[string]interface{}{
        "reqId": reqID,
        "header": map[string]interface{}{
            "X-BAPI-TIMESTAMP":   fmt.Sprintf("%d", time.Now().UnixMilli()),
            "X-BAPI-RECV-WINDOW": "5000",
        },
        "op": "order.amend",
        "args": []map[string]interface{}{
            {
                "category":      c.cfg.Category,
                "symbol":        instrument.Symbol,
              "orderLinkId": exchangeOrderID,
                "price":         priceStr,
                "qty":           qtyStr,
            },
        },
    }
    c.logger.Debug("Sending order.amend message", "msg", msg)
    if err := c.sendTradingWebSocketMessage(msg); err != nil {
        return "", types.TradingError{
            Code:    types.ErrConnectionFailed,
            Message: "Failed to send order.amend",
            Wrapped: err,
        }
    }
    c.ordersMu.Lock()
    c.activeOrders[exchangeOrderID].Price = price
    c.activeOrders[exchangeOrderID].Quantity = quantity
    c.activeOrders[exchangeOrderID].Status = "New"
    c.ordersMu.Unlock()
    c.logger.Info("Sent order.amend command", "client_order_id", exchangeOrderID)
    return exchangeOrderID, nil
}

// GetExchangeType returns the exchange type.
func (c *TradingClient) GetExchangeType() types.ExchangeType {
    return types.ExchangeBybit
}

// SubscribeOrderbook is not supported.
func (c *TradingClient) SubscribeOrderbook(ctx context.Context, symbol string) error {
    return fmt.Errorf("SubscribeOrderbook not supported")
}

// GetOrderbook returns nil.
func (c *TradingClient) GetOrderbook() *types.Orderbook {
    return nil
}

// ReplaceQuotes is not implemented.
func (c *TradingClient) ReplaceQuotes(ctx context.Context, instrument *types.Instrument, ordersToPlace []*types.Order) ([]string, error) {
    if instrument == nil {
        return nil, fmt.Errorf("instrument is nil")
    }
    if len(ordersToPlace) != 2 {
        return nil, fmt.Errorf("expected exactly 2 orders (buy and sell), got %d", len(ordersToPlace))
    }

    c.logger.Debug("Starting ReplaceQuotes", "symbol", instrument.Symbol, "order_count", len(ordersToPlace))

    // Step 1: Get and cancel all active orders for the instrument
    activeOrders := c.GetActiveOrders()
    for side, order := range activeOrders {
        if order.OrderID == "" {
            c.logger.Warn("Skipping order with empty OrderID", "client_order_id", order.ClientOrderID)
            continue
        }
        c.logger.Debug("Cancelling existing order", "client_order_id", order.ClientOrderID, "side", side)
        if err := c.CancelOrder(ctx, side, order.ClientOrderID); err != nil {
            c.logger.Error("Failed to cancel order", "client_order_id", order.ClientOrderID, "side", side, "error", err)
            return nil, fmt.Errorf("failed to cancel order %s: %w", order.ClientOrderID, err)
        }
    }

    // Step 2: Submit new orders
    var newOrderIDs []string
    for _, order := range ordersToPlace {
        if order.Instrument == nil || order.Instrument.Symbol != instrument.Symbol {
            return nil, fmt.Errorf("invalid order instrument: %v", order.Instrument)
        }
        clientOrderID, err := c.SubmitOrder(ctx, *order)
        if err != nil {
            c.logger.Error("Failed to submit order", "side", order.Side, "price", order.Price, "quantity", order.Quantity, "error", err)
            return nil, fmt.Errorf("failed to submit order: %w", err)
        }
        c.logger.Info("Submitted new order", "client_order_id", clientOrderID, "side", order.Side, "price", order.Price, "quantity", order.Quantity)
        newOrderIDs = append(newOrderIDs, clientOrderID)
    }

    c.logger.Debug("ReplaceQuotes completed", "new_order_ids", newOrderIDs)
    return newOrderIDs, nil
}

// Close shuts down the client.
func (c *TradingClient) Close() error {
    // Step 1: Initiate orderly shutdown
    c.cancel()
    
    // Step 2: Cancel all active orders with confirmation
    c.logger.Info("Starting shutdown cleanup...")
    
    // Get copy of active orders using clientOrderID as keys
    c.ordersMu.RLock()
    orders := make(map[string]*OrderInfo, len(c.activeOrders))
    for clientID, order := range c.activeOrders {
        orders[clientID] = order
    }
    c.ordersMu.RUnlock()
    
    if len(orders) > 0 {
        c.logger.Info("Cancelling active orders", "count", len(orders))
        
        // Create cancellation tracker
        cancellationCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        
        // Track order cancellations
        remaining := make(map[string]struct{})
        for clientID := range orders {
            remaining[clientID] = struct{}{}
        }
        
        // Set up cancellation confirmation handler
        go func() {
            ticker := time.NewTicker(100 * time.Millisecond)
            defer ticker.Stop()
            
            for {
                select {
                case <-cancellationCtx.Done():
                    return
                case <-ticker.C:
                    c.ordersMu.RLock()
                    for clientID := range remaining {
                        if _, exists := c.activeOrders[clientID]; !exists {
                            delete(remaining, clientID)
                        }
                    }
                    c.ordersMu.RUnlock()
                    
                    if len(remaining) == 0 {
                        return
                    }
                }
            }
        }()
        
        // Send cancellation requests with proper side parameter
        for clientID, order := range orders {
            go func(id string, side types.Side) {
                err := c.CancelOrder(cancellationCtx, side, id)
                if err != nil {
                    c.logger.Error("Failed to cancel order", 
                        "client_order_id", id,
                        "error", err)
                }
            }(clientID, order.Side)  // Pass order.Side here
        }
        
        // Wait for cancellations or timeout
        <-cancellationCtx.Done()
        if len(remaining) > 0 {
            c.logger.Error("Failed to cancel all orders", 
                "remaining", len(remaining))
        }
    }
    
    // Step 3: Close connections
    c.wsMu.Lock()
    if c.tradingWSConn != nil {
        c.tradingWSConn.Close()
    }
    if c.statusWSConn != nil {
        c.statusWSConn.Close()
    }
    c.wsMu.Unlock()
    
    c.logger.Info("Shutdown complete")
    return nil
}

// Ensure interface compliance
var _ exchange.ExchangeClient = (*TradingClient)(nil)