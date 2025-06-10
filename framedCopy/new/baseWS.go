package websocket

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

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

type MessageHandler interface {
	Handle(message []byte) error
}

type BaseWSClient struct {
	conn           *websocket.Conn
	url            string
	handlers       map[string]MessageHandler
	defaultHandler MessageHandler
	mu             sync.Mutex
	wg             sync.WaitGroup
}

func NewBaseWSClient(url string) *BaseWSClient {
	return &BaseWSClient{
		url:      url,
		handlers: make(map[string]MessageHandler),
	}
}

func (c *BaseWSClient) Connect() error {
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(c.url, nil)
	if err != nil {
		log.Error().Err(err).Str("url", c.url).Msg("Failed to connect to WebSocket")
		return err
	}
	c.conn = conn
	log.Info().Str("url", c.url).Msg("WebSocket connected")
	return nil
}

func (c *BaseWSClient) Subscribe(subMsg interface{}) error {
	return c.SendMessage(subMsg)
}

func (c *BaseWSClient) SendMessage(msg interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("websocket connection is nil")
	}
	err := c.conn.WriteJSON(msg)
	if err != nil {
		log.Error().Err(err).Str("url", c.url).Msg("Failed to send message")
		return err
	}
	return nil
}

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

func (c *BaseWSClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		c.wg.Wait()
		log.Info().Str("url", c.url).Msg("WebSocket closed")
		return err
	}
	return nil
}

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

func (c *BaseWSClient) reconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
	}
	for i := 0; i < 5; i++ {
		if err := c.Connect(); err == nil {
			log.Info().Str("url", c.url).Msg("Reconnected successfully")
			return
		}
		log.Error().Err(err).Int("attempt", i+1).Msg("Reconnect attempt failed")
		time.Sleep(2 * time.Second)
	}
}

func (c *BaseWSClient) RegisterHandler(topic string, handler MessageHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[topic] = handler
}

func (c *BaseWSClient) SetDefaultHandler(handler MessageHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.defaultHandler = handler
}