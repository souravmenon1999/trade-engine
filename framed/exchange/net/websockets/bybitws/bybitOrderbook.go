package bybitws

import (
	"log"
	"time"

	"github.com/gorilla/websocket"
)

// BybitOrderBookWSClient manages the WebSocket connection to Bybit.
type BybitOrderBookWSClient struct {
	conn     *websocket.Conn
	url      string
	callback func([]byte) // Callback to process messages
	topic    string
}

// NewBybitOrderBookWSClient creates a new client with a callback for message handling.
func NewBybitOrderBookWSClient(url string, callback func([]byte)) *BybitOrderBookWSClient {
	return &BybitOrderBookWSClient{
		url:      url,
		callback: callback,
	}
}

func (c *BybitOrderBookWSClient) Connect() error {
	log.Printf("Connecting to Bybit orderbook websocket: %s", c.url)
	conn, _, err := websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		log.Printf("Websocket connection error: %v", err)
		return err
	}
	c.conn = conn
	log.Println("Connected to Bybit orderbook websocket")
	return nil
}

func (c *BybitOrderBookWSClient) Subscribe(topic string) error {
	c.topic = topic
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
		log.Println("Bybit orderbook websocket connection closed")
	}
}

func (c *BybitOrderBookWSClient) StartReading() {
	defer c.Close()
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("Read message error: %v", err)
			c.reconnect()
			return
		}
		if c.callback != nil {
			c.callback(message)
		}
	}
}

func (c *BybitOrderBookWSClient) reconnect() {
	for {
		log.Println("Attempting to reconnect to Bybit orderbook websocket...")
		if err := c.Connect(); err != nil {
			log.Printf("Reconnect failed: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		if c.topic != "" {
			if err := c.Subscribe(c.topic); err != nil {
				log.Printf("Re-subscribe failed: %v", err)
				c.Close()
				continue
			}
		}
		go c.StartReading() // Restart reading in a new goroutine
		break
	}
}