package bybitorderbook

import (
    "encoding/json"
    "log"
    "strconv"
    "github.com/souravmenon1999/trade-engine/framed/types"
)

// BybitOrderBookMessage defines the structure of a Bybit order book message.
// U and Ts are int64 for JSON unmarshalling, then converted to types.Price.
type BybitOrderBookMessage struct {
    Topic string `json:"topic"`
    Type  string `json:"type"`
    Data  struct {
        S   string     `json:"s"`
        B   [][]string `json:"b"`
        A   [][]string `json:"a"`
        U   int64      `json:"u"`
    } `json:"data"`
    Ts int64 `json:"ts"`
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
// This runs in a single goroutine, ensuring thread safety without mutexes by
// processing messages sequentially, preventing concurrent access to Asks and Bids.
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
        // Clear existing maps
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
            return
        } else if bybitMsg.Data.U <= expectedSeq {
            log.Printf("Received old or duplicate delta for %s: current update ID %d, received %d", p.symbol, expectedSeq, bybitMsg.Data.U)
            return
        } else if bybitMsg.Data.U > expectedSeq+1 {
            log.Printf("Sequence gap detected for %s! Current update ID %d, received %d. Resync needed.", p.symbol, expectedSeq, bybitMsg.Data.U)
            // Clear maps
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

        price := types.NewPrice(int64(priceFloat * 1_000_000))
        quantity := types.NewQuantity(int64(qtyFloat * 1_000_000))

        if quantity.Load() == 0 {
            delete(orderBookMap, price)
        } else {
            orderBookMap[price] = quantity
        }
    }
}

// calculateVWAP calculates the volume-weighted average price.
func (p *BybitVWAPProcessor) calculateVWAP() *types.Price {
    var sumPQ types.Price
    var sumQ types.Quantity
    sumPQ.Store(0)
    sumQ.Store(0)

    for price, qty := range p.orderBookState.Bids {
        pVal := price.Load()
        qVal := qty.Load()
        sumPQ.Add(pVal * qVal)
        sumQ.Add(qVal)
    }
    for price, qty := range p.orderBookState.Asks {
        pVal := price.Load()
        qVal := qty.Load()
        sumPQ.Add(pVal * qVal)
        sumQ.Add(qVal)
    }

    if sumQ.Load() == 0 {
        return types.NewPrice(0)
    }
    return types.NewPrice(sumPQ.Load() / sumQ.Load())
}