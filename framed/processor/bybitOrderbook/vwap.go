package bybitorderbook

import (
    "encoding/json"
    "strconv"
    "sync"
    "time"

    "github.com/souravmenon1999/trade-engine/framed/types"
    bybitWS "github.com/souravmenon1999/trade-engine/framed/exchange/net/websockets/bybit"
    zerologlog "github.com/rs/zerolog/log"
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
            zerologlog.Error().Err(err).Str("price", update[0]).Msg("Error parsing price")
            continue
        }
        qtyFloat, err := strconv.ParseFloat(update[1], 64)
        if err != nil {
            zerologlog.Error().Err(err).Str("quantity", update[1]).Msg("Error parsing quantity")
            continue
        }
        priceVal := int64(priceFloat * 1_000_000)
        quantityVal := int64(qtyFloat * 1_000_000)
        price := types.NewPrice(priceVal)
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
    wsClient          *bybitWS.BybitWSClient
    lastReconnect     time.Time
    reconnectMu       sync.Mutex
    pendingDeltas     []BybitOrderbook
    deltasMu         sync.Mutex
}

// NewBybitVWAPProcessor creates a new BybitVWAPProcessor with a callback for processed data.
func NewBybitVWAPProcessor(processedCallback func(*types.OrderBookWithVWAP), symbol string, instrument *types.Instrument, wsClient *bybitWS.BybitWSClient) *BybitVWAPProcessor {
    processor := &BybitVWAPProcessor{
        processedCallback: processedCallback,
        symbol:            symbol,
        wsClient:          wsClient,
        orderBookState: &types.OrderBook{
            Asks:       make(map[*types.Price]*types.Quantity),
            Bids:       make(map[*types.Price]*types.Quantity),
            Instrument: instrument,
        },
        pendingDeltas: make([]BybitOrderbook, 0),
    }
    return processor
}

// ProcessAndApplyMessage processes a raw message and applies updates to the order book.
func (p *BybitVWAPProcessor) ProcessAndApplyMessage(rawMessage []byte) {
    var bybitMsg BybitOrderbook
    if err := json.Unmarshal(rawMessage, &bybitMsg); err != nil {
        zerologlog.Error().Err(err).Msg("Error unmarshaling Bybit message")
        return
    }

    if bybitMsg.Data.S != p.symbol {
        return
    }

    zerologlog.Debug().
        Str("symbol", p.symbol).
        Str("type", bybitMsg.Type).
        Int64("update_id", bybitMsg.Data.U).
        Int64("timestamp", bybitMsg.Ts).
        Msg("Processing order book message")

    p.deltasMu.Lock()
    switch bybitMsg.Type {
    case "snapshot":
        zerologlog.Info().Str("symbol", p.symbol).Int64("update_id", bybitMsg.Data.U).Msg("Received snapshot")
        p.orderBookState = bybitMsg.ToOrderbook()
        // Apply pending deltas that are newer than the snapshot
        validDeltas := make([]BybitOrderbook, 0)
        for _, delta := range p.pendingDeltas {
            if delta.Data.U > p.orderBookState.Sequence.Load() {
                validDeltas = append(validDeltas, delta)
            }
        }
        p.pendingDeltas = validDeltas
        for _, delta := range p.pendingDeltas {
            if delta.Data.U == p.orderBookState.Sequence.Load()+1 {
                delta.applyUpdates(delta.Data.B, p.orderBookState.Bids)
                delta.applyUpdates(delta.Data.A, p.orderBookState.Asks)
                p.orderBookState.Sequence.Store(delta.Data.U)
                p.orderBookState.LastUpdateTime.Store(delta.Ts)
            }
        }
    case "delta":
        expectedSeq := p.orderBookState.Sequence.Load()
        if expectedSeq == 0 {
            zerologlog.Warn().Str("symbol", p.symbol).Msg("Ignoring delta: no snapshot received yet")
            p.pendingDeltas = append(p.pendingDeltas, bybitMsg)
            p.deltasMu.Unlock()
            return
        }
        if bybitMsg.Data.U <= expectedSeq {
            zerologlog.Warn().
                Str("symbol", p.symbol).
                Int64("current_id", expectedSeq).
                Int64("received_id", bybitMsg.Data.U).
                Msg("Received old or duplicate delta")
            p.deltasMu.Unlock()
            return
        }
        if bybitMsg.Data.U > expectedSeq+100 {
            zerologlog.Error().
                Str("symbol", p.symbol).
                Int64("current_id", expectedSeq).
                Int64("received_id", bybitMsg.Data.U).
                Msg("Sequence gap detected! Reconnecting WebSocket")
            p.pendingDeltas = append(p.pendingDeltas, bybitMsg)
            p.deltasMu.Unlock()
            p.reconnect()
            return
        }
        bybitMsg.applyUpdates(bybitMsg.Data.B, p.orderBookState.Bids)
        bybitMsg.applyUpdates(bybitMsg.Data.A, p.orderBookState.Asks)
        p.orderBookState.Sequence.Store(bybitMsg.Data.U)
        p.orderBookState.LastUpdateTime.Store(bybitMsg.Ts)
    default:
    //     zerologlog.Warn().
    //         Str("symbol", p.symbol).
    //         Str("type", bybitMsg.Type).
    //         Str("topic", bybitMsg.Topic).
    //         Msg("Received unknown message type")
    // }
    p.deltasMu.Unlock()

    // Calculate VWAP and invoke callback
    if p.orderBookState.Sequence.Load() > 0 {
        vwap := p.calculateVWAP()
        processedData := &types.OrderBookWithVWAP{
            OrderBook: p.orderBookState,
            VWAP:      vwap,
        }
        if p.processedCallback != nil {
            p.processedCallback(processedData)
        }
    }
}

// reconnect handles WebSocket reconnection with throttling.
func (p *BybitVWAPProcessor) reconnect() {
    p.reconnectMu.Lock()
    defer p.reconnectMu.Unlock()

    // Throttle reconnections (wait 5 seconds since last reconnect)
    if time.Since(p.lastReconnect) < 5*time.Second {
        zerologlog.Debug().Str("symbol", p.symbol).Msg("Throttling WebSocket reconnection")
        return
    }

    p.orderBookState = &types.OrderBook{
        Asks:       make(map[*types.Price]*types.Quantity),
        Bids:       make(map[*types.Price]*types.Quantity),
        Instrument: p.orderBookState.Instrument,
    }
    p.orderBookState.Sequence.Store(0)

    // Reconnect WebSocket with retries
    for i := 0; i < 3; i++ {
        p.wsClient.Close()
        if err := p.wsClient.Connect(); err != nil {
            zerologlog.Error().Err(err).Int("attempt", i+1).Msg("Failed to reconnect WebSocket")
            time.Sleep(time.Duration(1<<i) * time.Second) // Exponential backoff
            continue
        }
        topic := "orderbook.50." + p.symbol
        if err := p.wsClient.Subscribe(topic); err != nil {
            zerologlog.Error().Err(err).Str("topic", topic).Int("attempt", i+1).Msg("Failed to re-subscribe")
            time.Sleep(time.Duration(1<<i) * time.Second)
            continue
        }
        zerologlog.Info().Str("topic", topic).Msg("Re-subscribed after reconnect")
        p.lastReconnect = time.Now()
        break
    }
}

// calculateVWAP calculates the volume-weighted average price using atomic operations.
func (p *BybitVWAPProcessor) calculateVWAP() *types.Price {
    var sumPQ atomic.Int64
    var sumQ atomic.Int64

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

    totalQty := sumQ.Load()
    if totalQty == 0 {
        return types.NewPrice(0)
    }
    return types.NewPrice(sumPQ.Load() / totalQty)
}