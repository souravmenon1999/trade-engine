package bybit

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/souravmenon1999/trade-engine/framed/types"
	"github.com/souravmenon1999/trade-engine/framed/exchange/net/websockets/bybit"
)

type BybitClient struct {
	wsClient  *bybit.BybitWSClient
	subs      map[string]func([]byte)
	subsMu    sync.Mutex
	apiKey    string
	apiSecret string
}

func NewBybitClient(wsURL, apiKey, apiSecret string) *BybitClient {
	return &BybitClient{
		wsClient:  bybit.NewBybitWSClient(wsURL, apiKey, apiSecret),
		subs:      make(map[string]func([]byte)),
		apiKey:    apiKey,
		apiSecret: apiSecret,
	}
}

func (c *BybitClient) Connect() error {
	if err := c.wsClient.Connect(); err != nil {
		return fmt.Errorf("failed to connect to Bybit: %w", err)
	}
	log.Println("Bybit client connected")
	return nil
}

func (c *BybitClient) Subscribe(topic string, callback func([]byte)) error {
	c.subsMu.Lock()
	defer c.subsMu.Unlock()
	if _, exists := c.subs[topic]; exists {
		return fmt.Errorf("already subscribed to topic: %s", topic)
	}
	c.subs[topic] = callback
	if err := c.wsClient.Subscribe(topic); err != nil {
		delete(c.subs, topic)
		return err
	}
	log.Printf("Subscribed to %s", topic)
	return nil
}

func (c *BybitClient) SendOrder(order *types.Order) error {
	orderMsg := map[string]interface{}{
		"op": "order.create",
		"args": map[string]interface{}{
			"symbol":     order.Instrument.BaseCurrency + order.Instrument.QuoteCurrency,
			"side":       "Buy", // Adjust based on order.Side
			"order_type": "Limit",
			"qty":        order.Quantity.Load(),
			"price":      order.Price.Load(),
		},
	}
	if err := c.wsClient.SendJSON(orderMsg); err != nil {
		return fmt.Errorf("failed to send order: %w", err)
	}
	log.Printf("Order sent for %s", order.Instrument.BaseCurrency+order.Instrument.QuoteCurrency)
	return nil
}

func (c *BybitClient) StartReading() {
	go func() {
		for {
			message, err := c.wsClient.ReadMessage()
			if err != nil {
				log.Printf("Error reading message: %v", err)
				continue
			}
			c.handleMessage(message)
		}
	}()
}

func (c *BybitClient) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("Error unmarshaling message: %v", err)
		return
	}
	if op, ok := msg["op"].(string); ok && op == "auth" {
		if success, ok := msg["success"].(bool); ok && success {
			log.Println("Authentication successful")
		} else {
			log.Println("Authentication failed")
		}
		return
	}
	topic, ok := msg["topic"].(string)
	if !ok {
		log.Println("Message without topic")
		return
	}
	c.subsMu.Lock()
	callback, exists := c.subs[topic]
	c.subsMu.Unlock()
	if exists {
		callback(message)
	} else {
		log.Printf("No callback for topic: %s", topic)
	}
}

func (c *BybitClient) Close() {
	c.wsClient.Close()
	log.Println("Bybit client closed")
}