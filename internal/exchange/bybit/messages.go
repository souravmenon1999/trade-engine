// internal/exchange/bybit/messages.go
package bybit

import "encoding/json"
import "fmt" // Import fmt for Error method
import "github.com/shopspring/decimal" 

// Bybit WebSocket message types and constants
const (
    TopicOrderbook = "orderbook.50." // Example: orderbook.50.ETHUSDT
    // Add other topics if needed
)

// WebSocket message structure received from Bybit
type WSMessage struct {
    Topic      string          `json:"topic"`
    Type       string          `json:"type"` // "snapshot", "delta"
    TS         int64           `json:"ts"`   // Timestamp in milliseconds
    Data       json.RawMessage `json:"data"` // Orderbook data payload (unmarshal further based on topic/type)
    CS         int64           `json:"cs"`   // Checksum (for snapshot/delta validation)
    Sequence   int64           `json:"seq"`  // Sequence number for delta updates (Bybit V5 uses 'u')
    CrossSeq   int64          `json:"cross_seq"` // Cross sequence number (V5 specific)
    Error      int             `json:"code,omitempty"` // Error code for responses
    ErrorMsg   string          `json:"msg,omitempty"`  // Error message for responses
    RequestID  string          `json:"req_id,omitempty"` // Request ID for responses
    Success    bool            `json:"success"`        // For subscription/command responses
    ConnectionID string        `json:"conn_id,omitempty"` // Connection ID
    UpdateID int64 `json:"u,omitempty"` // Bybit V5 Update ID (used as sequence for orderbook)
    // Add other fields if needed based on other topics
}

// OrderbookData represents the structure within the "data" field for orderbook topics
type OrderbookData struct {
    Symbol   string         `json:"s"` // Symbol
    Bids     [][]json.RawMessage `json:"b"` // Bids: [[price, quantity], ...] (prices/quantities are strings)
    Asks     [][]json.RawMessage `json:"a"` // Asks: [[price, quantity], ...] (prices/quantities are strings)
    UpdateID int64          `json:"u"` // Update ID for delta updates (used as sequence)
    Seq      int64          `json:"seq"` // Sequence number (V5 uses u primarily for orderbook)
    // Add other orderbook specific fields if any
}

// WSSubscribe message structure sent to Bybit
type WSSubscribe struct {
    Op    string   `json:"op"` // "subscribe"
    Args  []string `json:"args"` // List of topics
    ReqID string   `json:"req_id,omitempty"` // Optional request ID
}

// Bybit REST API Response Structures (common fields)
// These are used to parse the initial part of any Bybit REST API response.
// Added IsSuccess and Error methods.
type BybitAPIResponse struct {
	RetCode int    `json:"retCode"` // 0 on success, non-zero on error
	RetMsg  string `json:"retMsg"`  // Success or error message
	// Result json.RawMessage `json:"result"` // Raw message to be unmarshaled by caller
	// Add other common fields like time, etc.
}

// IsSuccess checks if the API response indicates success based on RetCode.
func (r *BybitAPIResponse) IsSuccess() bool {
	return r.RetCode == 0
}

// Error returns a formatted error string if the API response indicates an error.
func (r *BybitAPIResponse) Error() error {
	if r.IsSuccess() {
		return nil // Not an error
	}
	return fmt.Errorf("Bybit API Error %d: %s", r.RetCode, r.RetMsg)
}


// Bybit REST API Request/Response Structures for Trading Endpoints
// Define structs for the specific endpoints used: create, cancel, amend, cancel-all.

// PlaceOrderRequest body for POST /v5/order/create
type PlaceOrderRequest struct {
	Category      string `json:"category"`      // Product type, e.g., "spot", "linear", "inverse"
	Symbol        string `json:"symbol"`        // Trading pair, e.g., "BTCUSDT"
	Side          string `json:"side"`          // "Buy" or "Sell"
	OrderType     string `json:"orderType"`     // "Limit", "Market"
	Qty           string `json:"qty"`           // Order quantity (string, formatted)
	Price         string `json:"price"`         // Order price (string, formatted)
	ClientOrderID string `json:"clientOrderId,omitempty"` // Optional client order ID
	TimeInForce   string `json:"timeInForce,omitempty"` // "GTC", "IOC", "FOK", etc.
	// Add other fields as needed (e.g., isLeverage, orderFilter, reduceOnly)
}

// PlaceOrderResponse result for POST /v5/order/create (inside the 'result' field)
type PlaceOrderResponse struct {
	RetCode int    `json:"retCode"` // Common fields included here for convenience
	RetMsg  string `json:"retMsg"`
	Result  struct {
		OrderId       string `json:"orderId"`       // Exchange order ID
		ClientOrderId string `json:"clientOrderId"` // Client order ID if sent
	} `json:"result"`
	// Add other common fields
}

// CancelOrderRequest body for POST /v5/order/cancel
type CancelOrderRequest struct {
	Category      string `json:"category"`      // Product type
	Symbol        string `json:"symbol"`        // Trading pair
	OrderId       string `json:"orderId,omitempty"`       // Exchange order ID (either OrderId or ClientOrderId is required)
	ClientOrderId string `json:"clientOrderId,omitempty"` // Client order ID
}

// CancelOrderResponse result for POST /v5/order/cancel (inside the 'result' field)
type CancelOrderResponse struct {
	RetCode int    `json:"retCode"` // Common fields
	RetMsg  string `json:"retMsg"`
	Result  struct {
		OrderId       string `json:"orderId"`       // Exchange order ID of the cancelled order
		ClientOrderId string `json:"clientOrderId"` // Client order ID
	} `json:"result"`
	// Add other common fields
}

// AmendOrderRequest body for POST /v5/order/amend
type AmendOrderRequest struct {
	Category      string `json:"category"`      // Product type
	Symbol        string `json:"symbol"`        // Trading pair
	OrderId       string `json:"orderId,omitempty"`       // Exchange order ID (either OrderId or ClientOrderId is required)
	ClientOrderId string `json:"clientOrderId,omitempty"` // Client order ID
	Qty           string `json:"qty,omitempty"`           // New quantity (string, formatted), omitempty if not changing
	Price         string `json:"price,omitempty"`         // New price (string, formatted), omitempty if not changing
	// Add other amendable fields like TakeProfit, StopLoss, etc.
}

// AmendOrderResponse result for POST /v5/order/amend (inside the 'result' field)
type AmendOrderResponse struct {
	RetCode int    `json:"retCode"` // Common fields
	RetMsg  string `json:"retMsg"`
	Result  struct {
		OrderId       string `json:"orderId"`       // Exchange order ID of the amended order
		ClientOrderId string `json:"clientOrderId"` // Client order ID
	} `json:"result"`
	// Add other common fields
}

// CancelAllOrdersRequest body for POST /v5/order/cancel-all
type CancelAllOrdersRequest struct {
	Category string `json:"category"` // Product type
	Symbol   string `json:"symbol,omitempty"`   // Trading pair (optional, cancels all for category if omitted)
	BaseCoin string `json:"baseCoin,omitempty"` // Optional, cancels all for base coin in category
	SettleCoin string `json:"settleCoin,omitempty"` // Optional, cancels all for settle coin in category
}

// CancelAllOrdersResponse result for POST /v5/order/cancel-all (inside the 'result' field)
type CancelAllOrdersResponse struct {
	RetCode int    `json:"retCode"` // Common fields
	RetMsg  string `json:"retMsg"`
	Result  struct {
		List []struct { // List of orders that were attempted to cancel
			OrderId       string `json:"orderId"`
			ClientOrderId string `json:"clientOrderId"`
		} `json:"list"`
	} `json:"result"`
	// Add other common fields
}

type BybitInstrumentsInfoResponse struct {
    BybitAPIResponse // Embed the common response structure
    Result struct {
        List []struct { // List of market instruments
            Symbol string `json:"symbol"`
            Status string `json:"status"` // e.g., "Trading"
            BaseCoin string `json:"baseCoin"`
            QuoteCoin string `json:"quoteCoin"`
             PriceFilter struct {
                MinPrice string `json:"minPrice"`
                MaxPrice string `json:"maxPrice"`
                TickSize string `json:"tickSize"`
            } `json:"priceFilter"`
            LotSizeFilter struct {
                MinQuantity string `json:"minOrderQty"`
                MaxQuantity string `json:"maxOrderQty"`
                QuantityStep string `json:"qtyStep"`
            } `json:"lotSizeFilter"`
            // Add other fields like contractType if needed
        } `json:"list"`
    } `json:"result"`
    // Add other common fields if they exist outside RetCode/RetMsg/Result
}

type BybitPriceFilter struct {
    MinPrice decimal.Decimal `json:"minPrice"`
    MaxPrice decimal.Decimal `json:"maxPrice"`
    TickSize decimal.Decimal `json:"tickSize"`
}

type BybitLotSizeFilter struct {
    MinQuantity decimal.Decimal `json:"minOrderQty"`
    MaxQuantity decimal.Decimal `json:"maxOrderQty"`
    QuantityStep decimal.Decimal `json:"qtyStep"`
}