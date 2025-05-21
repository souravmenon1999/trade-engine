package bybitws

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// BybitExchangeClient manages a WebSocket connection to Bybit for trading and updates.
type BybitExchangeClient struct {
	conn          *websocket.Conn
	url           string
	apiKey        string
	apiSecret     string
	OrderBookCh   chan []byte // Channel for orderbook updates
	OrderUpdateCh chan []byte // Channel for order updates
	mu            sync.Mutex  // For thread-safe operations
}

// NewBybitExchangeClient initializes a new Bybit WebSocket client.
func NewBybitExchangeClient(url, apiKey, apiSecret string) *BybitExchangeClient {
	return &BybitExchangeClient{
		url:           url,
		apiKey:        apiKey,
		apiSecret:     apiSecret,
		OrderBookCh:   make(chan []byte, 100),
		OrderUpdateCh: make(chan []byte, 100),
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

// StartReading starts reading messages from the WebSocket and dispatches them to channels.
func (c *BybitExchangeClient) StartReading() {
	go func() {
		for {
			_, message, err := c.conn.ReadMessage()
			if err != nil {
				log.Printf("Error reading message: %v", err)
				return
			}
			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err != nil {
				log.Printf("Error unmarshaling message: %v", err)
				continue
			}
			topic, ok := msg["topic"].(string)
			if !ok {
				continue
			}
			if strings.HasPrefix(topic, "orderbook") {
				select {
				case c.OrderBookCh <- message:
				default:
					log.Printf("OrderBookCh full, dropping message")
				}
			} else if topic == "order" {
				select {
				case c.OrderUpdateCh <- message:
				default:
					log.Printf("OrderUpdateCh full, dropping message")
				}
			}
		}
	}()
}

// Subscribe sends a subscription message to the specified topic.
func (c *BybitExchangeClient) Subscribe(topic string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	subscribeMsg := map[string]interface{}{
		"op":   "subscribe",
		"args": []string{topic},
	}
	return c.conn.WriteJSON(subscribeMsg)
}

// Close shuts down the WebSocket connection and channels.
func (c *BybitExchangeClient) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
	close(c.OrderBookCh)
	close(c.OrderUpdateCh)
}