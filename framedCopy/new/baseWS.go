package baseWS

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
	"github.com/gorilla/websocket" // Import for WebSocket functionality
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"github.com/rs/zerolog/log"    // Import for logging (optional, adjust if you use a different logger)
)

// WSClient interface defines the methods required for WebSocket clients
type WSClient interface {
	Connect() error
	Subscribe(subMsg interface{}) error
	SendMessage(msg interface{}) error
	HandleMessage(message []byte) error
	Close() error
	Start()
	RegisterHandler(topic string, handler MessageHandler)
	SetDefaultHandler(handler MessageHandler)
}

// MessageHandler interface for handling WebSocket messages
type MessageHandler interface {
	Handle(message []byte) error
}

type authResponseHandler struct {
    authCh chan error
}

// BaseWSClient struct is the base WebSocket client implementation
type BaseWSClient struct {
	conn           *websocket.Conn   // WebSocket connection
	url            string            // WebSocket server URL
	apiKey         string            // API key for authentication
	apiSecret      string            // API secret for authentication
	handlers       map[string]MessageHandler // Message handlers by topic
	defaultHandler MessageHandler    // Default handler for unhandled messages
	mu             sync.Mutex        // Mutex for thread safety
	wg             sync.WaitGroup    // WaitGroup for goroutine synchronization
	authFunc       func() error
	subscriptions  []interface{}
}

// NewBaseWSClient creates a new instance of BaseWSClient
func NewBaseWSClient(url, apiKey, apiSecret string) *BaseWSClient {
    client := &BaseWSClient{
        url:       url,
        apiKey:    apiKey,
        apiSecret: apiSecret,
        handlers:  make(map[string]MessageHandler),
    }

    // If API key and secret are provided, set up Bybit authentication
    if apiKey != "" && apiSecret != "" {
     client.authFunc = func() error {
    timestamp := time.Now().UnixMilli()
    expires := timestamp + 10000
    toSign := fmt.Sprintf("GET/realtime%d", expires)
    signature := generateSignature(apiSecret, toSign)
    authMsg := map[string]interface{}{
        "op": "auth",
        "args": []interface{}{
            apiKey,
            expires,
            signature,
        },
    }
    log.Debug().Interface("authMsg", authMsg).Msg("Sending authentication message")
    return client.SendJSON(authMsg)
}
    }
    // If no credentials, authFunc remains nil (no authentication needed)

    return client
}

// Connect establishes a connection to the WebSocket server
func (c *BaseWSClient) Connect() error {
    dialer := websocket.DefaultDialer
    conn, _, err := dialer.Dial(c.url, nil)
    if err != nil {
        log.Error().Err(err).Str("url", c.url).Msg("Failed to connect to WebSocket")
        return err
    }
    c.conn = conn
    log.Info().Str("url", c.url).Msg("WebSocket connected")

    if c.authFunc != nil {
        if err := c.authFunc(); err != nil {
            c.conn.Close()
            c.conn = nil
            log.Error().Err(err).Str("url", c.url).Msg("Authentication failed")
            return err
        }
        log.Info().Str("url", c.url).Msg("WebSocket authenticated")
    }
    return nil
}

// Subscribe sends a subscription message
func (c *BaseWSClient) Subscribe(subMsg interface{}) error {
	return c.SendMessage(subMsg)
}

// SendMessage sends a message over the WebSocket connection
func (c *BaseWSClient) SendMessage(msg interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("websocket connection is nil")
	}
	// Store subscription messages
    if m, ok := msg.(map[string]interface{}); ok {
        if op, ok := m["op"].(string); ok && op == "subscribe" {
            c.subscriptions = append(c.subscriptions, msg)
        }
    }
	err := c.conn.WriteJSON(msg)
	if err != nil {
		log.Error().Err(err).Str("url", c.url).Msg("Failed to send message")
		return err
	}
	return nil
}

// HandleMessage processes incoming WebSocket messages
func (c *BaseWSClient) HandleMessage(message []byte) error {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		return fmt.Errorf("failed to unmarshal message: %w", err)
	}
	topic, ok := msg["topic"].(string)
	if ok {
		handler, ok := c.handlers[topic]
		if ok {
			return handler.Handle(message)
		}
	}
	if c.defaultHandler != nil {
		return c.defaultHandler.Handle(message)
	}
	return fmt.Errorf("no handler for message: %s", string(message))
}

// Close terminates the WebSocket connection
func (c *BaseWSClient) Close() error {
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.conn != nil {
        err := c.conn.Close()
        c.conn = nil
        c.subscriptions = nil // Clear subscriptions
        c.wg.Wait()
        log.Info().Str("url", c.url).Msg("WebSocket closed")
        return err
    }
    return nil
}

// Start begins listening for WebSocket messages
func (c *BaseWSClient) Start() {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
			if c.conn == nil {
				return
			}
			_, message, err := c.conn.ReadMessage()
			if err != nil {
				log.Error().Err(err).Str("url", c.url).Msg("Error reading message")
				c.reconnect()
				continue
			}
			if err := c.HandleMessage(message); err != nil {
				log.Warn().Err(err).Str("message", string(message)).Msg("Handler error")
			}
		}
	}()
}

// reconnect attempts to reconnect to the WebSocket server
func (c *BaseWSClient) reconnect() {
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.conn != nil {
        c.conn.Close()
    }
    for i := 0; i < 5; i++ {
        err := c.Connect()
        if err == nil {
            log.Info().Str("url", c.url).Msg("Reconnected successfully")
            for _, sub := range c.subscriptions {
                if err := c.SendMessage(sub); err != nil {
                    log.Error().Err(err).Msg("Failed to resend subscription")
                }
            }
            return
        }
        log.Error().Err(err).Int("attempt", i+1).Msg("Reconnect attempt failed")
        time.Sleep(2 * time.Second)
    }
}

// RegisterHandler associates a handler with a topic
func (c *BaseWSClient) RegisterHandler(topic string, handler MessageHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[topic] = handler
}

// SetDefaultHandler sets the default message handler
func (c *BaseWSClient) SetDefaultHandler(handler MessageHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.defaultHandler = handler
}

// GetApiKey returns the API key
func (c *BaseWSClient) GetApiKey() string {
	return c.apiKey
}

// GetApiSecret returns the API secret
func (c *BaseWSClient) GetApiSecret() string {
	return c.apiSecret
}

// SendJSON sends a JSON message over the WebSocket connection
func (c *BaseWSClient) SendJSON(v interface{}) error {
	return c.SendMessage(v) // Reuses SendMessage for consistency
}

func generateSignature(apiSecret, toSign string) string {
    h := hmac.New(sha256.New, []byte(apiSecret))
    h.Write([]byte(toSign))
    return hex.EncodeToString(h.Sum(nil))
}

func (c *BaseWSClient) IsConnected() bool {
    return c.conn != nil
}

func (h *authResponseHandler) Handle(message []byte) error {
    var resp map[string]interface{}
    if err := json.Unmarshal(message, &resp); err != nil {
        h.authCh <- fmt.Errorf("failed to unmarshal auth response: %w", err)
        return err
    }
    if op, ok := resp["op"].(string); ok && op == "auth" {
        if success, ok := resp["success"].(bool); ok && success {
            h.authCh <- nil
        } else {
            h.authCh <- fmt.Errorf("authentication failed: %v", resp)
        }
    }
    return nil
}