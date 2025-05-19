package bybitorderbook

import (
	"encoding/json"
	"log"
	"strconv"

	"github.com/souravmenon1999/trade-engine/framed/types"
)

type BybitVWAPProcessor struct {
	RawMessageCh    <-chan []byte
	ProcessedDataCh chan *types.OrderBookWithVWAP
	orderBookState  *types.OrderBook
	symbol          string
}

func NewBybitVWAPProcessor(rawMsgCh <-chan []byte, processedCh chan *types.OrderBookWithVWAP, symbol string, instrument *types.Instrument) *BybitVWAPProcessor {
	return &BybitVWAPProcessor{
		RawMessageCh:    rawMsgCh,
		ProcessedDataCh: processedCh,
		symbol:          symbol,
		orderBookState: &types.OrderBook{
			Asks:       make(map[types.Price]types.Quantity),
			Bids:       make(map[types.Price]types.Quantity),
			Instrument: instrument,
		},
	}
}

func (p *BybitVWAPProcessor) StartProcessing() {
	go func() {
		for rawMessage := range p.RawMessageCh {
			p.processAndApplyMessage(rawMessage)
			if p.orderBookState.Sequence > 0 {
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

func (p *BybitVWAPProcessor) processAndApplyMessage(rawMessage []byte) {
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

	var bybitMsg BybitOrderBookMessage
	if err := json.Unmarshal(rawMessage, &bybitMsg); err != nil {
		log.Printf("Error unmarshalling Bybit message: %v", err)
		return
	}

	if bybitMsg.Data.S != p.symbol {
		return
	}

	p.orderBookState.Mu.Lock()
	defer p.orderBookState.Mu.Unlock()

	switch bybitMsg.Type {
	case "snapshot":
		log.Printf("Received snapshot for %s, update ID %d", p.symbol, bybitMsg.Data.U)
		p.orderBookState.Asks = make(map[types.Price]types.Quantity)
		p.orderBookState.Bids = make(map[types.Price]types.Quantity)
		p.applyUpdates(bybitMsg.Data.B, p.orderBookState.Bids)
		p.applyUpdates(bybitMsg.Data.A, p.orderBookState.Asks)
		p.orderBookState.Sequence = uint64(bybitMsg.Data.U)
		p.orderBookState.LastUpdateTime = uint64(bybitMsg.Ts)

	case "delta":
		expectedSeq := p.orderBookState.Sequence
		if p.orderBookState.Sequence == 0 {
			return
		} else if uint64(bybitMsg.Data.U) <= expectedSeq {
			log.Printf("Received old or duplicate delta for %s: current update ID %d, received %d", p.symbol, expectedSeq, bybitMsg.Data.U)
			return
		} else if uint64(bybitMsg.Data.U) > expectedSeq+1 {
			log.Printf("Sequence gap detected for %s! Current update ID %d, received %d. Resync needed.", p.symbol, expectedSeq, bybitMsg.Data.U)
			p.orderBookState.Asks = make(map[types.Price]types.Quantity)
			p.orderBookState.Bids = make(map[types.Price]types.Quantity)
			p.orderBookState.Sequence = 0
			return
		}

		p.applyUpdates(bybitMsg.Data.B, p.orderBookState.Bids)
		p.applyUpdates(bybitMsg.Data.A, p.orderBookState.Asks)
		p.orderBookState.Sequence = uint64(bybitMsg.Data.U)
		p.orderBookState.LastUpdateTime = uint64(bybitMsg.Ts)

	default:
		log.Printf("Received unknown message type: %s, topic: %s", bybitMsg.Type, bybitMsg.Topic)
	}
}

func (p *BybitVWAPProcessor) applyUpdates(updates [][]string, orderBookMap map[types.Price]types.Quantity) {
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

		price := types.Price(priceFloat * 1_000_000)
		quantity := types.Quantity(qtyFloat * 1_000_000)

		if quantity == 0 {
			delete(orderBookMap, price)
		} else {
			orderBookMap[price] = quantity
		}
	}
}

func (p *BybitVWAPProcessor) calculateVWAP() types.Price {
	p.orderBookState.Mu.RLock()
	defer p.orderBookState.Mu.RUnlock()

	var sumPQ, sumQ int64
	for price, qty := range p.orderBookState.Bids {
		sumPQ += int64(price) * int64(qty)
		sumQ += int64(qty)
	}
	for price, qty := range p.orderBookState.Asks {
		sumPQ += int64(price) * int64(qty)
		sumQ += int64(qty)
	}

	if sumQ == 0 {
		return 0
	}
	return types.Price(sumPQ / sumQ)
}