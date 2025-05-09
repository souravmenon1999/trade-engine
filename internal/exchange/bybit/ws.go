// internal/exchange/bybit/ws.go - Using github.com/gorilla/websocket
package bybit

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket" // Import gorilla websocket
	"github.com/souravmenon1999/trade-engine/internal/logging"
	"log/slog" 
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 8192 // Adjust as needed for Bybit messages
)

// WSConnection manages the Bybit WebSocket connection using gorilla/websocket.
type WSConnection struct {
	url     string
	conn    *websocket.Conn
	mu      sync.Mutex // Protects the conn field and write operations
	logger  *slog.Logger
	ctx     context.Context // Parent context
	cancel  context.CancelFunc // Function to cancel the parent context
	msgChan chan WSMessage // Channel to receive incoming messages from the read loop

	// Add a done channel or atomic flag to signal when read/write loops should stop
	// Context cancellation is often sufficient, but explicit done signals can be clearer
	// done chan struct{}
}

// NewWSConnection creates a new WSConnection manager.
// It requires a parent context for cancellation.
func NewWSConnection(ctx context.Context, url string) *WSConnection {
	// Create a derived context for the WS connection's goroutines
	connCtx, cancel := context.WithCancel(ctx)
	return &WSConnection{
		url:     url,
		logger:  logging.GetLogger().With("component", "bybit_ws"),
		ctx:     connCtx,
		cancel:  cancel,
		msgChan: make(chan WSMessage, 100), // Buffered channel
		// done: make(chan struct{}),
	}
}

// Connect establishes the WebSocket connection.
func (w *WSConnection) Connect() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// If conn is not nil, it might indicate a connection is already active or failed to close cleanly.
	// In a robust system, you'd add more state checks here. For simplicity now, assume nil means not connected.
	if w.conn != nil {
		w.logger.Warn("Attempted to connect while connection might be active.")
		// Optionally close the existing one or return an error
		// w.Close() // Attempt to clean up
	}

	w.logger.Info("Connecting to WebSocket", "url", w.url)

	// Use context with timeout for the initial dial
	dialer := websocket.Dialer{} // Configure dialer options if needed (e.g., timeouts)
	ctx, cancel := context.WithTimeout(w.ctx, 10*time.Second)
	defer cancel()

	c, _, err := dialer.DialContext(ctx, w.url, nil)
	if err != nil {
		w.logger.Error("Failed to connect to WebSocket", "error", err)
		w.conn = nil // Ensure conn is nil on failure
		return err
	}

	w.conn = c
	w.logger.Info("WebSocket connected successfully")

	// Configure the connection
	c.SetReadLimit(maxMessageSize)
	// Set a read deadline to ensure the read loop doesn't block indefinitely
	c.SetReadDeadline(time.Now().Add(pongWait))

	// Set handler for pong messages to reset the read deadline
	c.SetPongHandler(func(string) error {
		w.conn.SetReadDeadline(time.Now().Add(pongWait))
		w.logger.Debug("Received pong, reset read deadline")
		return nil
	})

	// Start the read loop and write ping loop in goroutines
	go w.readMessages()
	go w.writePings()


	return nil
}

// Close closes the WebSocket connection and cancels the context.
func (w *WSConnection) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn == nil {
		return nil // Already closed or never connected
	}

	// Send a close message to the peer
	err := w.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil && err != websocket.ErrCloseSent {
		w.logger.Error("Error sending close message", "error", err)
		// Continue with physical close even if close message failed
	}

	// Cancel the context to signal goroutines to stop
	w.cancel()

	// Physically close the connection
	closeErr := w.conn.Close()
	w.conn = nil // Mark as closed
	w.logger.Info("WebSocket connection closed")

	// Prioritize the physical close error
	if closeErr != nil {
		return closeErr
	}
	return err // Return the error from sending the close message if applicable
}

// Send sends a message over the WebSocket. This method is thread-safe.
func (w *WSConnection) Send(msg interface{}) error {
	w.mu.Lock() // Protect writes
	defer w.mu.Unlock()

	if w.conn == nil {
		return fmt.Errorf("websocket not connected")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		w.logger.Error("Failed to marshal message", "error", err)
		return err
	}

	// Set a write deadline
	w.conn.SetWriteDeadline(time.Now().Add(writeWait))

	err = w.conn.WriteMessage(websocket.TextMessage, data)
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
	defer func() {
		w.logger.Info("WebSocket read loop stopped")
		// When read loop stops due to error or context cancellation,
		// the connection is likely broken. Signal this via channel closure.
		// Ensure the channel is only closed once.
		// Note: Closing the connection itself might cause read to return error.
		// This goroutine exiting might imply the connection needs reconnecting.
		// close(w.msgChan) // Closing the channel signals the receiver
	}()

	// Gorilla requires setting read deadline periodically via pong handler or manually
	// We set SetReadDeadline in Connect and SetPongHandler.

	for {
		select {
		case <-w.ctx.Done():
			w.logger.Debug("Read loop received context done")
			return // Context cancelled, stop the loop
		default:
			// Blocking read - will unblock on message, error, or deadline
			msgType, data, err := w.conn.ReadMessage()
			if err != nil {
				// Handle specific errors like close error or network error
				w.logger.Error("WebSocket read error", "error", err)
				// Read error often means the connection is broken.
				// Trigger the client's reconnect logic.
				// The client detecting a closed channel from Messages() can trigger this.
				// Or Close() is called which cancels the context.
				return // Exit read loop on error
			}

			if msgType != websocket.TextMessage {
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
				w.logger.Debug("Context done while sending to message channel")
				return // Context cancelled while trying to send to channel
			default:
				// Channel is full, drop the message and log a warning
				w.logger.Warn("WebSocket message channel is full, dropping message", "topic", wsMsg.Topic)
			}
		}
	}
}

// writePings sends ping messages periodically.
func (w *WSConnection) writePings() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		w.logger.Info("WebSocket ping loop stopped")
	}()
	w.logger.Info("Starting WebSocket ping loop")

	for {
		select {
		case <-w.ctx.Done():
			w.logger.Debug("Ping loop received context done")
			return // Context cancelled, stop the loop
		case <-ticker.C:
			// Send ping message
			w.mu.Lock() // Use the same mutex as Send for writing
			if w.conn == nil {
				w.mu.Unlock()
				w.logger.Debug("Connection is nil during ping attempt")
				return // Connection is gone
			}
			w.conn.SetWriteDeadline(time.Now().Add(writeWait))
			err := w.conn.WriteMessage(websocket.PingMessage, nil)
			w.mu.Unlock() // Unlock after write operation

			if err != nil {
				w.logger.Error("Failed to send ping", "error", err)
				// Ping failed, connection might be broken.
				// This error will likely cause the read loop to fail and exit,
				// which will then trigger reconnect logic.
				return // Exit ping loop on write error
			}
			w.logger.Debug("Ping sent")
		}
	}
}


// Messages returns the channel where incoming messages can be received.
func (w *WSConnection) Messages() <-chan WSMessage {
	return w.msgChan
}