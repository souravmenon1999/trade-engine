package bybitws

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

type BybitWSClient struct {
	conn      *websocket.Conn
	url       string
	apiKey    string
	apiSecret string
}

func NewBybitWSClient(url, apiKey, apiSecret string) *BybitWSClient {
	return &BybitWSClient{
		url:       url,
		apiKey:    apiKey,
		apiSecret: apiSecret,
	}
}

func (c *BybitWSClient) Connect() error {
	var err error
	c.conn, _, err = websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to Bybit WebSocket: %w", err)
	}
	if err := c.authenticate(); err != nil {
		c.conn.Close()
		return fmt.Errorf("failed to authenticate: %w", err)
	}
	log.Println("Bybit WebSocket connected")
	return nil
}

func (c *BybitWSClient) authenticate() error {
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
	return c.SendJSON(authMsg)
}

func (c *BybitWSClient) generateSignature(timestamp int64) string {
	toSign := fmt.Sprintf("GET/realtime%d", timestamp)
	h := hmac.New(sha256.New, []byte(c.apiSecret))
	h.Write([]byte(toSign))
	return hex.EncodeToString(h.Sum(nil))
}

func (c *BybitWSClient) Subscribe(topic string) error {
	subscribeMsg := map[string]interface{}{
		"op":   "subscribe",
		"args": []string{topic},
	}
	if err := c.SendJSON(subscribeMsg); err != nil {
		return fmt.Errorf("subscription to %s failed: %w", topic, err)
	}
	log.Printf("Subscription sent for topic: %s", topic)
	return nil
}

func (c *BybitWSClient) SendJSON(v interface{}) error {
	if c.conn == nil {
		return fmt.Errorf("no WebSocket connection")
	}
	return c.conn.WriteJSON(v)
}

func (c *BybitWSClient) ReadMessage() ([]byte, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("no WebSocket connection")
	}
	_, message, err := c.conn.ReadMessage()
	return message, err
}

func (c *BybitWSClient) Close() {
	if c.conn != nil {
		c.conn.Close()
		log.Println("Bybit WebSocket closed")
	}
}