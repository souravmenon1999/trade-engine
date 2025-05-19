package bybit

import (
	"log"
	"time"

	"github.com/gorilla/websocket"
)

type BybitOrderBookWSClient struct {
	conn         *websocket.Conn
	url          string
	RawMessageCh chan []byte
	topic        string // Store topic for resubscription
}

func NewBybitOrderBookWSClient(url string) *BybitOrderBookWSClient {
	return &BybitOrderBookWSClient{
		url:          url,
		RawMessageCh: make(chan []byte, 100),
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

	go c.readMessages()
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
	close(c.RawMessageCh)
}

func (c *BybitOrderBookWSClient) readMessages() {
	defer c.Close()
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("Read message error: %v", err)
			// Attempt reconnect
			c.reconnect()
			return
		}
		c.RawMessageCh <- message
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
		// Re-subscribe to the topic
		if c.topic != "" {
			if err := c.Subscribe(c.topic); err != nil {
				log.Printf("Re-subscribe failed: %v", err)
				c.Close()
				continue
			}
		}
		break
	}
}