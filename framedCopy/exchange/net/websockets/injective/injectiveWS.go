package injectivews

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// InjectiveWSClient manages a WebSocket connection to the Injective Chain
type InjectiveWSClient struct {
	conn     *websocket.Conn
	url      string
	ctx      context.Context
	cancel   context.CancelFunc
	gasPrice atomic.Int64 // Latest gas price, updated atomically
	wg       sync.WaitGroup
	tracker  *gasPriceTracker
}

type gasPriceTracker struct {
	prices   []int64
	index    int
	size     int
	capacity int
	sum      int64
	mutex    sync.Mutex
}

func newGasPriceTracker(capacity int) *gasPriceTracker {
	return &gasPriceTracker{
		prices:   make([]int64, capacity),
		capacity: capacity,
	}
}

func (t *gasPriceTracker) add(price int64) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if t.size < t.capacity {
		t.prices[t.index] = price
		t.index = (t.index + 1) % t.capacity
		t.size++
		t.sum += price
	} else {
		old := t.prices[t.index]
		t.prices[t.index] = price
		t.index = (t.index + 1) % t.capacity
		t.sum = t.sum - old + price
	}
}

func (t *gasPriceTracker) average() int64 {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if t.size == 0 {
		return 0
	}
	return t.sum / int64(t.size)
}

// NewInjectiveWSClient creates a new WebSocket client
func NewInjectiveWSClient(url string) *InjectiveWSClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &InjectiveWSClient{
		url:     url,
		ctx:     ctx,
		cancel:  cancel,
		tracker: newGasPriceTracker(100), // Track last 100 transactions
	}
}

// Connect establishes the WebSocket connection with retry logic
func (c *InjectiveWSClient) Connect() error {
	log.Info().Str("url", c.url).Msg("Connecting to Injective WebSocket")
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(c.url, nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to connect to Injective WebSocket")
		return err
	}
	c.conn = conn
	log.Info().Msg("Injective WebSocket connected")
	return nil
}

// SubscribeTxs subscribes to transaction events and starts processing
func (c *InjectiveWSClient) SubscribeTxs(callback func(int64)) error {
	for attempt := 1; attempt <= 5; attempt++ {
		if c.conn == nil {
			if err := c.Connect(); err != nil {
				log.Error().Err(err).Int("attempt", attempt).Msg("Connection attempt failed")
				time.Sleep(2 * time.Second)
				continue
			}
		}

		// Send subscription message
		subMsg := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "subscribe",
			"id":      1,
			"params": map[string]interface{}{
				"query": "tm.event='Tx'",
			},
		}
		if err := c.conn.WriteJSON(subMsg); err != nil {
			log.Error().Err(err).Msg("Failed to subscribe to transaction events")
			c.conn = nil // Force reconnect
			time.Sleep(2 * time.Second)
			continue
		}
		log.Info().Msg("Subscribed to transaction events")

		// Start the readLoop goroutine
		c.wg.Add(1)
		go c.readLoop(callback)
		return nil
	}
	return fmt.Errorf("failed to subscribe after 5 attempts")
}

// readLoop reads WebSocket messages and processes them
func (c *InjectiveWSClient) readLoop(callback func(int64)) {
	defer c.wg.Done()
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			if c.conn == nil {
				return // Exit to trigger reconnect
			}
			_, message, err := c.conn.ReadMessage()
			if err != nil {
				log.Error().Err(err).Msg("Error reading WebSocket message")
				c.conn = nil // Trigger reconnect
				return
			}
			go c.processTxMessage(message, callback)
		}
	}
}

// TxEvent represents the structure of a transaction event
type TxEvent struct {
	Result struct {
		Data struct {
			Value struct {
				TxResult struct {
					Result struct {
						GasUsed string `json:"gas_used"`
						Events  []struct {
							Type       string `json:"type"`
							Attributes []struct {
								Key   string `json:"key"`
								Value string `json:"value"`
							} `json:"attributes"`
						} `json:"events"`
					} `json:"result"`
				} `json:"TxResult"`
			} `json:"value"`
		} `json:"data"`
	} `json:"result"`
}

// processTxMessage processes transaction messages to extract gas prices
func (c *InjectiveWSClient) processTxMessage(message []byte, callback func(int64)) {
	var event TxEvent
	if err := json.Unmarshal(message, &event); err != nil {
		log.Error().Err(err).Str("message", string(message)).Msg("Failed to unmarshal TX event")
		return
	}

	// Extract gas used
	gasUsed, err := strconv.ParseInt(event.Result.Data.Value.TxResult.Result.GasUsed, 10, 64)
	if err != nil || gasUsed == 0 {
		log.Error().Err(err).Msg("Invalid gas_used value")
		return
	}

	// Find fee in events
	var totalFee int64
	for _, e := range event.Result.Data.Value.TxResult.Result.Events {
		if e.Type == "tx" {
			for _, attr := range e.Attributes {
				if attr.Key == "fee" {
					feeStr := strings.ReplaceAll(attr.Value, " ", "") // Remove spaces
					coins := strings.Split(feeStr, ",")
					for _, coin := range coins {
						if strings.HasSuffix(coin, "inj") {
							amountStr := strings.TrimSuffix(coin, "inj")
							fee, err := strconv.ParseInt(amountStr, 10, 64)
							if err == nil {
								totalFee += fee
							}
						}
					}
				}
			}
		}
	}

	if totalFee == 0 {
		log.Warn().Msg("No fee found in transaction events")
		return
	}

	// Calculate gas price for this transaction (in nanoINJ per gas unit)
	txGasPrice := totalFee / gasUsed
	// log.Info().
	// 	Int64("fee", totalFee).
	// 	Int64("gas_used", gasUsed).
	// 	Int64("tx_gas_price", txGasPrice).
	// 	Msg("Calculated gas price from transaction")

	// Add to tracker and get new average
	c.tracker.add(txGasPrice)
	avgGasPrice := c.tracker.average()
	c.gasPrice.Store(avgGasPrice)

	// Call callback with new average
	if callback != nil {
		callback(avgGasPrice)
	}
}

// GetLatestGasPrice retrieves the latest gas price
func (c *InjectiveWSClient) GetLatestGasPrice() int64 {
	return c.gasPrice.Load()
}

// Close shuts down the WebSocket connection
func (c *InjectiveWSClient) Close() {
	c.cancel()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.wg.Wait()
	log.Info().Msg("Injective WebSocket closed")
}