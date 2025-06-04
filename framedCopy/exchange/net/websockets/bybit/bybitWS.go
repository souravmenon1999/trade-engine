package bybit

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "sync"
    "time"

    "github.com/gorilla/websocket"
    zerologlog "github.com/rs/zerolog/log"
)

// BybitWSClient manages a single WebSocket connection
type BybitWSClient struct {
    conn      *websocket.Conn
    url       string
    apiKey    string
    apiSecret string
    wsType    string
    mu        sync.Mutex
}

// NewBybitWSClient creates a new WebSocket client
func NewBybitWSClient(url, apiKey, apiSecret, wsType string) *BybitWSClient {
    client := &BybitWSClient{
        url:       url,
        apiKey:    apiKey,
        apiSecret: apiSecret,
        wsType:    wsType,
    }
    zerologlog.Info().Str("wsType", wsType).Str("url", url).Msg("BybitWSClient created")
    return client
}

// Type returns the WebSocket type for logging
func (c *BybitWSClient) Type() string {
    return c.wsType
}

// Connect establishes the WebSocket connection
func (c *BybitWSClient) Connect() error {
    zerologlog.Info().Str("wsType", c.wsType).Str("url", c.url).Msg("Attempting to connect to Bybit WebSocket")
    dialer := websocket.DefaultDialer
    conn, _, err := dialer.Dial(c.url, nil)
    if err != nil {
        zerologlog.Error().Err(err).Str("wsType", c.wsType).Msg("Failed to connect to Bybit WebSocket")
        return err
    }
    c.conn = conn
    zerologlog.Info().Str("wsType", c.wsType).Msg("Bybit WebSocket connected successfully")

    if c.apiKey != "" && c.apiSecret != "" {
        if err := c.authenticate(); err != nil {
            return err
        }
    }
    return nil
}

// authenticate sends an authentication message
func (c *BybitWSClient) authenticate() error {
    zerologlog.Info().Str("wsType", c.wsType).Msg("Attempting WebSocket authentication")
    expires := time.Now().UnixMilli() + 1000
    signature := c.generateSignature(expires)
    authMsg := map[string]interface{}{
        "op": "auth",
        "args": []interface{}{
            c.apiKey,
            expires,
            signature,
        },
    }
    zerologlog.Debug().Str("wsType", c.wsType).Interface("auth_message", authMsg).Msg("Sending authentication message")
    err := c.SendJSON(authMsg)
    if err != nil {
        zerologlog.Error().Err(err).Str("wsType", c.wsType).Msg("Failed to send authentication message")
        return err
    }
    return nil
}

// generateSignature creates an HMAC-SHA256 signature
func (c *BybitWSClient) generateSignature(expires int64) string {
    toSign := fmt.Sprintf("GET/realtime%d", expires)
    h := hmac.New(sha256.New, []byte(c.apiSecret))
    h.Write([]byte(toSign))
    return hex.EncodeToString(h.Sum(nil))
}

// Subscribe sends a subscription message
func (c *BybitWSClient) Subscribe(topic string) error {
    subMsg := map[string]interface{}{
        "op":   "subscribe",
        "args": []interface{}{topic},
    }
    err := c.SendJSON(subMsg)
    if err != nil {
        zerologlog.Error().Err(err).Str("topic", topic).Str("wsType", c.wsType).Msg("Failed to subscribe")
        return err
    }
    zerologlog.Info().Str("topic", topic).Str("wsType", c.wsType).Msg("Subscription sent")
    return nil
}

// SendJSON sends a JSON message
func (c *BybitWSClient) SendJSON(v interface{}) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.conn == nil {
        return fmt.Errorf("websocket connection is nil")
    }
    err := c.conn.WriteJSON(v)
    if err != nil {
        zerologlog.Error().Err(err).Str("wsType", c.wsType).Msg("Failed to send JSON message")
        return err
    }
    return nil
}

// ReadMessage reads a message from the WebSocket
func (c *BybitWSClient) ReadMessage() ([]byte, error) {
    if c.conn == nil {
        return nil, fmt.Errorf("websocket connection is nil")
    }
    _, message, err := c.conn.ReadMessage()
    if err != nil {
        zerologlog.Error().Err(err).Str("wsType", c.wsType).Msg("Failed to read WebSocket message")
        return nil, err
    }
    // zerologlog.Debug().Str("wsType", c.wsType).Msg("Successfully read WebSocket message")
    return message, nil
}

func (c *BybitWSClient) GetApiKey() string {
    return c.apiKey
}

// GetApiSecret returns the API secret stored in BybitWSClient.
func (c *BybitWSClient) GetApiSecret() string {
    return c.apiSecret
}


// Close terminates the WebSocket connection
func (c *BybitWSClient) Close() {
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.conn != nil {
        c.conn.Close()
        c.conn = nil
        zerologlog.Info().Str("wsType", c.wsType).Msg("Bybit WebSocket closed")
    }
}