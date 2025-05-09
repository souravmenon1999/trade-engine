// internal/exchange/bybit/ws.go
package bybit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"trading-system/internal/logging"

	"nhooyr.io/websocket" // Recommended modern WS library
)

// WSConnection manages the Bybit WebSocket connection.
type WSConnection struct {
	url     string
	conn    *websocket.Conn
	mu      sync.Mutex // Protects the conn field
	logger  *slog.Logger
	ctx     context.Context
	cancel  context.CancelFunc
	msgChan chan WSMessage // Channel to receive incoming messages
}

// NewWSConnection creates a new WSConnection manager.
func NewWSConnection(ctx context.Context, url string) *WSConnection {
	ctx, cancel := context.WithCancel(ctx)
	return &WSConnection{
		url:     url,
		logger:  logging.GetLogger().With("component", "bybit_ws"),
		ctx:     ctx,
		cancel:  cancel,
		msgChan: make(chan WSMessage, 100), // Buffered channel
	}
}

// Connect establishes the WebSocket connection.
func (w *WSConnection) Connect() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.logger.Info("Connecting to WebSocket", "url", w.url)
	ctx, cancel := context.WithTimeout(w.ctx, 10*time.Second) // Use context with timeout
	defer cancel()

	c, _, err := websocket.Dial(ctx, w.url, nil)
	if err != nil {
		w.logger.Error("Failed to connect to WebSocket", "error", err)
		return err
	}

	w.conn = c
	w.logger.Info("WebSocket connected successfully")

	// Start the read loop in a goroutine
	go w.readMessages()

	return nil
}

// Close closes the WebSocket connection and cancels the context.
func (w *WSConnection) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn == nil {
		return nil // Already closed or never connected
	}

	w.cancel() // Signal read loop to stop
	err := w.conn.Close(websocket.StatusNormalClosure, "closing")
	w.conn = nil // Mark as closed
	w.logger.Info("WebSocket connection closed")
	return err
}

// Send sends a message over the WebSocket.
func (w *WSConnection) Send(msg interface{}) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn == nil {
		return fmt.Errorf("websocket not connected")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		w.logger.Error("Failed to marshal message", "error", err)
		return err
	}

	// Use context with timeout for writing
	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Second)
	defer cancel()

	err = w.conn.Write(ctx, websocket.MessageText, data)
	if err != nil {
		w.logger.Error("Failed to send message", "error", err)
		return err
	}
	w.logger.Debug("Message sent", "message", string(data))
	return nil
}

// readMessages is a goroutine that reads messages from the WebSocket.
func (w *WSConnection) readMessages() {
	w.logger.Info("Starting WebSocket read loop")
	defer w.logger.Info("WebSocket read loop stopped")

	for {
		select {
		case <-w.ctx.Done():
			return // Context cancelled, stop the loop
		default:
			// Use context with a timeout for reading to prevent blocking indefinitely
			// and allow context cancellation to be checked.
			ctx, cancel := context.WithTimeout(w.ctx, time.Minute) // Adjust read timeout as needed
			msgType, data, err := w.conn.Read(ctx)
			cancel() // Cancel the read context once read is done

			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					// Read timeout, potentially send a ping if protocol supports it
					// Or just continue, the server might send pings
					w.logger.Debug("WebSocket read timeout")
					continue
				}
				// Other read error (connection closed, etc.)
				w.logger.Error("WebSocket read error", "error", err)
				// Handle reconnection logic outside this loop, triggered by the client detecting connection state
				// For now, break the read loop on error
				return
			}

			if msgType != websocket.MessageText {
				w.logger.Warn("Received non-text message type", "type", msgType)
				continue
			}

			var wsMsg WSMessage
			if err := json.Unmarshal(data, &wsMsg); err != nil {
				w.logger.Error("Failed to unmarshal WebSocket message", "error", err, "data", string(data))
				continue // Skip unprocessable message
			}

			// Send the unmarshaled message to the message channel
			select {
			case w.msgChan <- wsMsg:
				w.logger.Debug("Message received and sent to channel", "topic", wsMsg.Topic, "type", wsMsg.Type)
			case <-w.ctx.Done():
				return // Context cancelled while trying to send to channel
			default:
				// Channel is full, drop the message and log a warning
				w.logger.Warn("WebSocket message channel is full, dropping message", "topic", wsMsg.Topic)
			}
		}
	}
}

// Messages returns the channel where incoming messages can be received.
func (w *WSConnection) Messages() <-chan WSMessage {
	return w.msgChan
}

// Add dependencies for nhooyr.io/websocket
// go get nhooyr.io/websocket
