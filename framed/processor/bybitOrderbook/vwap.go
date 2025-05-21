package bybitorderbook

import (
	"encoding/json"
	"log"
	"strconv"

	"github.com/souravmenon1999/trade-engine/framed/types"
	"sync/atomic"
)

// BybitOrderbook defines the structure of a Bybit order book message.
type BybitOrderbook struct {
	Topic string `json:"topic"`
	Type  string `json:"type"`
	Data  struct {
		S   string     `json:"s"`
		B   [][]string `json:"b"` // Bid price and quantity pairs
		A   [][]string `json:"a"` // Ask price and quantity pairs
		U   int64      `json:"u"` // Update ID as int64 for deserialization
	} `json:"data"`
	Ts int64 `json:"ts"` // Timestamp as int64 for deserialization
}

// ToOrderbook converts BybitOrderbook to the internal Orderbook struct.
func (b *BybitOrderbook) ToOrderbook() *types.OrderBook {
	ob := &types.OrderBook{
		Asks:           make(map[*types.Price]*types.Quantity),
		Bids:           make(map[*types.Price]*types.Quantity),
		Instrument:     nil, // Set if needed
		LastUpdateTime: *new(atomic.Int64),
		Sequence:       *new(atomic.Int64),
	}
	b.applyUpdates(b.Data.B, ob.Bids)
	b.applyUpdates(b.Data.A, ob.Asks)
	ob.Sequence.Store(b.Data.U)
	ob.LastUpdateTime.Store(b.Ts)
	return ob
}

// applyUpdates is a helper to apply bid/ask updates to the orderbook map.
func (b *BybitOrderbook) applyUpdates(updates [][]string, orderBookMap map[*types.Price]*types.Quantity) {
	for _, update := range updates {
		if len(update) != 2 {
			continue
		}
		priceFloat, err := strconv.ParseFloat(update[0], 64)
		if err != nil {
			log.Printf("Error parsing price '%s': %v", update[0], err)
			continue
		}
		qtyFloat, err := strconv.ParseFloat(update[1], 64)
		if err != nil {
			log.Printf("Error parsing quantity '%s': %v", update[1], err)
			continue
		}
		priceVal := int64(priceFloat * 1_000_000)
		quantityVal := int64(qtyFloat * 1_000_000)
		var price *types.Price
		for existingPrice := range orderBookMap {
			if existingPrice.Load() == priceVal {
				price = existingPrice
				break
			}
		}
		if price == nil {
			price = types.NewPrice(priceVal)
		}
		if quantityVal == 0 {
			delete(orderBookMap, price)
		} else {
			quantity, exists := orderBookMap[price]
			if !exists {
				quantity = types.NewQuantity(quantityVal)
				orderBookMap[price] = quantity
			} else {
				quantity.Store(quantityVal)
			}
		}
	}
}

// BybitVWAPProcessor processes Bybit order book messages and calculates VWAP.
type BybitVWAPProcessor struct {
	processedCallback func(*types.OrderBookWithVWAP)
	orderBookState    *types.OrderBook
	symbol            string
}

// NewBybitVWAPProcessor creates a new BybitVWAPProcessor with a callback for processed data.
func NewBybitVWAPProcessor(processedCallback func(*types.OrderBookWithVWAP), symbol string, instrument *types.Instrument) *BybitVWAPProcessor {
    return &BybitVWAPProcessor{
        processedCallback: processedCallback,
        symbol:            symbol,
        orderBookState: &types.OrderBook{
            Asks:       make(map[*types.Price]*types.Quantity),
            Bids:       make(map[*types.Price]*types.Quantity),
            Instrument: instrument,
        },
    }
}

// ProcessAndApplyMessage processes a raw message and applies updates to the order book.
func (p *BybitVWAPProcessor) ProcessAndApplyMessage(rawMessage []byte) {
	var bybitMsg BybitOrderbook
	if err := json.Unmarshal(rawMessage, &bybitMsg); err != nil {
		log.Printf("Error unmarshalling Bybit message: %v", err)
		return
	}

	if bybitMsg.Data.S != p.symbol {
		return
	}

	switch bybitMsg.Type {
	case "snapshot":
		log.Printf("Received snapshot for %s, update ID %d", p.symbol, bybitMsg.Data.U)
		p.orderBookState = bybitMsg.ToOrderbook()
	case "delta":
		expectedSeq := p.orderBookState.Sequence.Load()
		if expectedSeq == 0 {
			return // Ignore delta if no snapshot received yet
		} else if bybitMsg.Data.U <= expectedSeq {
			log.Printf("Received old or duplicate delta for %s: current update ID %d, received %d", p.symbol, expectedSeq, bybitMsg.Data.U)
			return
		} else if bybitMsg.Data.U > expectedSeq+1 {
			log.Printf("Sequence gap detected for %s! Current update ID %d, received %d. Resync needed.", p.symbol, expectedSeq, bybitMsg.Data.U)
			p.orderBookState = &types.OrderBook{
				Asks:       make(map[*types.Price]*types.Quantity),
				Bids:       make(map[*types.Price]*types.Quantity),
				Instrument: p.orderBookState.Instrument,
			}
			p.orderBookState.Sequence.Store(0)
			return
		}
		bybitMsg.applyUpdates(bybitMsg.Data.B, p.orderBookState.Bids)
		bybitMsg.applyUpdates(bybitMsg.Data.A, p.orderBookState.Asks)
		p.orderBookState.Sequence.Store(bybitMsg.Data.U)
		p.orderBookState.LastUpdateTime.Store(bybitMsg.Ts)
	default:
		log.Printf("Received unknown message type: %s, topic: %s", bybitMsg.Type, bybitMsg.Topic)
	}

	// After updating orderbook, calculate VWAP and invoke callback
	if p.orderBookState.Sequence.Load() > 0 {
        vwap := p.calculateVWAP() // Assuming this method exists
        processedData := &types.OrderBookWithVWAP{
            OrderBook: p.orderBookState,
            VWAP:      vwap,
        }
        if p.processedCallback != nil {
            p.processedCallback(processedData)
        }
    }
}

// calculateVWAP calculates the volume-weighted average price using atomic operations.
func (p *BybitVWAPProcessor) calculateVWAP() *types.Price {
	var sumPQ atomic.Int64
	var sumQ atomic.Int64

	// Aggregate bids
	for price, qty := range p.orderBookState.Bids {
		pVal := price.Load()
		qVal := qty.Load()
		sumPQ.Add(pVal * qVal)
		sumQ.Add(qVal)
	}
	// Aggregate asks
	for price, qty := range p.orderBookState.Asks {
		pVal := price.Load()
		qVal := qty.Load()
		sumPQ.Add(pVal * qVal)
		sumQ.Add(qVal)
	}

	totalQty := sumQ.Load()
	if totalQty == 0 {
		return types.NewPrice(0)
	}
	return types.NewPrice(sumPQ.Load() / totalQty)
}