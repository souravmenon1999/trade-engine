package bybitws

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

// BybitExchangeClient manages a WebSocket connection to Bybit for trading and updates.
type BybitExchangeClient struct {
	conn      *websocket.Conn
	url       string
	apiKey    string
	apiSecret string
}

// NewBybitExchangeClient initializes a new Bybit WebSocket client.
func NewBybitExchangeClient(url, apiKey, apiSecret string) *BybitExchangeClient {
	return &BybitExchangeClient{
		url:       url,
		apiKey:    apiKey,
		apiSecret: apiSecret,
	}
}

// Connect establishes the WebSocket connection and authenticates for private data.
func (c *BybitExchangeClient) Connect() error {
	var err error
	c.conn, _, err = websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to Bybit WebSocket: %w", err)
	}
	if err := c.authenticate(); err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}
	return nil
}

// authenticate sends the authentication message for private data access.
func (c *BybitExchangeClient) authenticate() error {
	timestamp := time.Now().UnixMilli()
	signature := c.generateSignature(timestamp)
	authMsg := map[string]interface{}{
		"op": "auth",
		"args": []interface{}{
			c.apiKey,
			timestamp,
			signature,
		},
	}
	return c.conn.WriteJSON(authMsg)
}

// generateSignature creates the HMAC-SHA256 signature for authentication.
func (c *BybitExchangeClient) generateSignature(timestamp int64) string {
	toSign := fmt.Sprintf("GET/realtime%d", timestamp)
	h := hmac.New(sha256.New, []byte(c.apiSecret))
	h.Write([]byte(toSign))
	return hex.EncodeToString(h.Sum(nil))
}

// Close shuts down the WebSocket connection.
func (c *BybitExchangeClient) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}