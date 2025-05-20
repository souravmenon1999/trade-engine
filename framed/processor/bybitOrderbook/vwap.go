package bybitorderbook

import (
    "encoding/json"
    "log"
    "strconv"
    "github.com/souravmenon1999/trade-engine/framed/types"
	"sync/atomic"
)

// BybitOrderBookMessage defines the structure of a Bybit order book message.
// U and Ts use int64 for JSON unmarshalling, then stored as atomic types.
type BybitOrderBookMessage struct {
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

// BybitVWAPProcessor processes Bybit order book messages and calculates VWAP.
type BybitVWAPProcessor struct {
    RawMessageCh    <-chan []byte
    ProcessedDataCh chan *types.OrderBookWithVWAP
    orderBookState  *types.OrderBook
    symbol          string
}

// NewBybitVWAPProcessor creates a new BybitVWAPProcessor.
func NewBybitVWAPProcessor(rawMsgCh <-chan []byte, processedCh chan *types.OrderBookWithVWAP, symbol string, instrument *types.Instrument) *BybitVWAPProcessor {
    return &BybitVWAPProcessor{
        RawMessageCh:    rawMsgCh,
        ProcessedDataCh: processedCh,
        symbol:          symbol,
        orderBookState: &types.OrderBook{
            Asks:       make(map[*types.Price]*types.Quantity),
            Bids:       make(map[*types.Price]*types.Quantity),
            Instrument: instrument,
        },
    }
}

// StartProcessing starts processing raw messages and sends processed data.
// Runs in a single goroutine to process messages sequentially, avoiding concurrent map access.
func (p *BybitVWAPProcessor) StartProcessing() {
    go func() {
        for rawMessage := range p.RawMessageCh {
            p.processAndApplyMessage(rawMessage)
            if p.orderBookState.Sequence.Load() > 0 {
                vwap := p.calculateVWAP()
                processedData := &types.OrderBookWithVWAP{
                    OrderBook: p.orderBookState,
                    VWAP:      vwap,
                }
                p.ProcessedDataCh <- processedData
            }
        }
        log.Println("BybitVWAPProcessor goroutine finished.")
        close(p.ProcessedDataCh)
    }()
}

// processAndApplyMessage processes a raw message and applies updates to the order book.
func (p *BybitVWAPProcessor) processAndApplyMessage(rawMessage []byte) {
    var bybitMsg BybitOrderBookMessage
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
        // Clear existing maps for a fresh snapshot
        for k := range p.orderBookState.Asks {
            delete(p.orderBookState.Asks, k)
        }
        for k := range p.orderBookState.Bids {
            delete(p.orderBookState.Bids, k)
        }
        p.applyUpdates(bybitMsg.Data.B, p.orderBookState.Bids)
        p.applyUpdates(bybitMsg.Data.A, p.orderBookState.Asks)
        p.orderBookState.Sequence.Store(bybitMsg.Data.U)
        p.orderBookState.LastUpdateTime.Store(bybitMsg.Ts)

    case "delta":
        expectedSeq := p.orderBookState.Sequence.Load()
        if expectedSeq == 0 {
            return // Ignore delta if no snapshot received yet
        } else if bybitMsg.Data.U <= expectedSeq {
            log.Printf("Received old or duplicate delta for %s: current update ID %d, received %d", p.symbol, expectedSeq, bybitMsg.Data.U)
            return
        } else if bybitMsg.Data.U > expectedSeq+1 {
            log.Printf("Sequence gap detected for %s! Current update ID %d, received %d. Resync needed.", p.symbol, expectedSeq, bybitMsg.Data.U)
            // Clear maps and reset sequence for resync
            for k := range p.orderBookState.Asks {
                delete(p.orderBookState.Asks, k)
            }
            for k := range p.orderBookState.Bids {
                delete(p.orderBookState.Bids, k)
            }
            p.orderBookState.Sequence.Store(0)
            return
        }

        p.applyUpdates(bybitMsg.Data.B, p.orderBookState.Bids)
        p.applyUpdates(bybitMsg.Data.A, p.orderBookState.Asks)
        p.orderBookState.Sequence.Store(bybitMsg.Data.U)
        p.orderBookState.LastUpdateTime.Store(bybitMsg.Ts)

    default:
        log.Printf("Received unknown message type: %s, topic: %s", bybitMsg.Type, bybitMsg.Topic)
    }
}

// applyUpdates applies price and quantity updates to the order book map.
// Individual price levels are updated atomically, avoiding locks on the entire book.
func (p *BybitVWAPProcessor) applyUpdates(updates [][]string, orderBookMap map[*types.Price]*types.Quantity) {
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

        // Convert to int64 with scaling factor to avoid floating-point precision issues
        priceVal := int64(priceFloat * 1_000_000)
        quantityVal := int64(qtyFloat * 1_000_000)

        // Check if price already exists in the map
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
            delete(orderBookMap, price) // Remove price level if quantity is zero
        } else {
            quantity, exists := orderBookMap[price]
            if !exists {
                quantity = types.NewQuantity(quantityVal)
                orderBookMap[price] = quantity
            } else {
                quantity.Store(quantityVal) // Atomically update existing quantity
            }
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