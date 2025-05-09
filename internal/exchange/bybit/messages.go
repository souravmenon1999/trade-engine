// internal/exchange/bybit/messages.go
package bybit

import "encoding/json"

// Bybit WebSocket message types
const (
	TopicOrderbook = "orderbook.50." // Example: orderbook.50.ETHUSDT
)

// WebSocket message structure
type WSMessage struct {
	Topic      string          `json:"topic"`
	Type       string          `json:"type"` // "snapshot", "delta"
	TS         int64           `json:"ts"`   // Timestamp in milliseconds
	Data       json.RawMessage `json:"data"` // Orderbook data payload
	CS         int64           `json:"cs"`   // Checksum (for snapshot/delta validation)
	Sequence   int64           `json:"seq"`  // Sequence number for delta updates
	CrossSeq   int64           `json:"cross_seq"` // Cross sequence number
	Error      int             `json:"code,omitempty"` // Error code
	ErrorMsg   string          `json:"msg,omitempty"`  // Error message
	RequestID  string          `json:"req_id,omitempty"` // Request ID for responses
	Success    bool            `json:"success"`        // For subscription/command responses
	ConnectionID string        `json:"conn_id,omitempty"` // Connection ID
}

// OrderbookData represents the structure within the "data" field
type OrderbookData struct {
	Symbol   string         `json:"s"` // Symbol
	Bids     [][]json.RawMessage `json:"b"` // Bids: [[price, quantity], ...]
	Asks     [][]json.RawMessage `json:"a"` // Asks: [[price, quantity], ...]
	UpdateID int64          `json:"u"` // Update ID for delta updates
	Seq      int64          `json:"seq"` // Sequence number for delta updates
}

// Subscription message structure
type WSSubscribe struct {
	Op    string   `json:"op"` // "subscribe"
	Args  []string `json:"args"` // List of topics
	ReqID string   `json:"req_id,omitempty"` // Optional request ID
}

// Bybit price levels in messages are strings, need conversion
// Helper struct for unmarshalling price/quantity arrays
type priceQuantity struct {
	Price    string
	Quantity string
}