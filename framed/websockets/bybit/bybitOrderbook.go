// websocket/bybit/bybitorderbookws.go
package bybit

import (
	"log"
	"time"

	"github.com/gorilla/websocket"
)

type BybitOrderBookWSClient struct {
	conn *websocket.Conn
	url  string
	RawMessageCh chan []byte
}

func NewBybitOrderBookWSClient(url string) *BybitOrderBookWSClient {
	return &BybitOrderBookWSClient{
		url: url,
		RawMessageCh: make(chan []byte, 100),
	}
}

func (c *BybitOrderBookWSClient) Connect() error {
	log.Printf("Connecting to Bybit websocket: %s", c.url)
	conn, _, err := websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		log.Printf("Websocket connection error: %v", err)
		return err
	}
	c.conn = conn
	log.Println("Connected to Bybit websocket")

	go c.readMessages()

	return nil
}

func (c *BybitOrderBookWSClient) Subscribe(topic string) error {
	log.Printf("Subscribing to topic: %s", topic)
	subscribeMsg := map[string]interface{}{
		"op":   "subscribe",
		"args": []string{topic},
	}
	err := c.conn.WriteJSON(subscribeMsg)
	if err != nil {
		log.Printf("Subscription error: %v", err)
		return err
	}
	log.Printf("Subscription message sent for topic: %s", topic)
	return nil
}

func (c *BybitOrderBookWSClient) Close() {
	if c.conn != nil {
		c.conn.Close()
		log.Println("Bybit websocket connection closed")
	}
	// Closing the RawMessageCh signals the processor to stop
	close(c.RawMessageCh)
}

func (c *BybitOrderBookWSClient) readMessages() {
	// Ensure the connection and channel are closed if the goroutine exits
	defer c.Close()

	for {
		messageType, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("Read message error: %v", err)
			// In a real application, you would implement reconnect logic here
			return
		}

		if messageType == websocket.TextMessage {
			c.RawMessageCh <- message
		} else {
			log.Printf("Received non-text message type: %d", messageType)
		}
	}
}

// Note: A robust client would handle ping/pong, reconnection, and potentially
// authentication or other control messages. This version focuses on raw data streaming.

