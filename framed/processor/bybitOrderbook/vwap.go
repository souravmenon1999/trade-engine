// processor/bybitorderbook/vwap/processor.go
package vwap

import (
	"encoding/json"
	"log"
	"strconv"
	"time" // Required for time operations if needed, though not strictly for parsing here

	"your_module_path/types" // Replace with your actual module path
)

// BybitVWAPProcessor processes raw Bybit websocket messages,
// maintains the OrderBook state, calculates VWAP, and outputs the result.
type BybitVWAPProcessor struct {
	// Input channel for raw messages from the websocket client
	RawMessageCh <-chan []byte
	// Output channel for processed OrderBook data including VWAP
	ProcessedDataCh chan *types.OrderBookWithVWAP

	// Internal state for the order book
	orderBookState *types.OrderBook
	symbol         string // The symbol this processor is handling
}

// NewBybitVWAPProcessor creates a new BybitVWAPProcessor.
// It takes the raw message channel, the output channel, and the symbol.
func NewBybitVWAPProcessor(rawMsgCh <-chan []byte, processedCh chan *types.OrderBookWithVWAP, symbol string) *BybitVWAPProcessor {
	return &BybitVWAPProcessor{
		RawMessageCh: rawMsgCh,
		ProcessedDataCh: processedCh,
		symbol:         symbol,
		// Initialize the internal order book state
		orderBookState: &types.OrderBook{
			Asks: make(map[types.Price]types.Quantity),
			Bids: make(map[types.Price]types.Quantity),
			// Instrument can be populated here or from the first snapshot
			Instrument: &types.Instrument{
				// Placeholder - ideally populated from exchange info or config
				BaseCurrency:  "ETH",
				QuoteCurrency: "USDT",
				MinLotSize:    types.Quantity(100000),
				ContractType:  "Perpetual",
			},
		},
	}
}

// StartProcessing begins listening for raw messages, processing them,
// maintaining the OrderBook state, calculating VWAP, and sending results.
// This function should run in a goroutine.
func (p *BybitVWAPProcessor) StartProcessing() {
	go func() {
		// Loop and read raw messages from the input channel.
		// The loop will automatically exit when the RawMessageCh is closed.
		for rawMessage := range p.RawMessageCh {
			// Process the raw message and update the internal order book state
			p.processAndApplyMessage(rawMessage)

			// If the order book state is valid (e.g., after receiving a snapshot),
			// calculate VWAP and send the updated state.
			// A simple check: if sequence is > 0, we assume we have a snapshot or applied updates.
			if p.orderBookState.Sequence > 0 {
				// Calculate VWAP for the current internal OrderBook state
				vwap := p.calculateVWAP()

				// Create the output struct with the current state and calculated VWAP
				processedData := &types.OrderBookWithVWAP{
					OrderBook: p.orderBookState, // Send a pointer to the internal state
					VWAP:      vwap,
				}

				// Send the processed data to the output channel
				p.ProcessedDataCh <- processedData
			}
		}
		// This log indicates that the RawMessageCh has been closed and the loop finished.
		log.Println("BybitVWAPProcessor goroutine finished.")
		// Close the output channel when processing is done.
		close(p.ProcessedDataCh)
	}()
}

// processAndApplyMessage unmarshals the raw JSON message from Bybit
// and applies the updates to the internal order book state.
// Handles snapshot and delta messages and sequence checking.
func (p *BybitVWAPProcessor) processAndApplyMessage(rawMessage []byte) {
	// Define a struct to match Bybit's actual websocket message format for orderbook updates.
	// **IMPORTANT: VERIFY THIS STRUCTURE AGAINST BYBIT'S OFFICIAL API DOCUMENTATION**
	type BybitOrderBookMessage struct {
		Topic string `json:"topic"`
		Type  string `json:"type"` // "snapshot" or "delta"
		Data  struct {
			S string `json:"s"` // Symbol
			B [][]string `json:"b"` // Bids [Price, Quantity] - Note: Bybit often sends numbers as strings
			A [][]string `json:"a"` // Asks [Price, Quantity] - Note: Bybit often sends numbers as strings
			U int64 `json:"u"` // Update ID (used for delta application range)
			Seq int64 `json:"seq"` // Sequence number of the update
		} `json:"data"`
		Ts int64 `json:"ts"` // Timestamp (often in milliseconds)
	}

	var bybitMsg BybitOrderBookMessage
	if err := json.Unmarshal(rawMessage, &bybitMsg); err != nil {
		log.Printf("Error unmarshalling Bybit message: %v", err)
		return // Stop processing this message
	}

	// Ensure the message is for the symbol this processor is handling
	if bybitMsg.Data.S != p.symbol {
		// log.Printf("Received message for wrong symbol: %s, expected %s", bybitMsg.Data.S, p.symbol) // Uncomment for verbose logging
		return // Ignore messages for other symbols
	}

	// Lock the order book state for writing before applying updates
	p.orderBookState.mu.Lock()
	defer p.orderBookState.mu.Unlock() // Ensure unlock

	switch bybitMsg.Type {
	case "snapshot":
		log.Printf("Received snapshot for %s, sequence %d", p.symbol, bybitMsg.Data.Seq)
		// Clear the current order book state
		p.orderBookState.Asks = make(map[types.Price]types.Quantity)
		p.orderBookState.Bids = make(map[types.Price]types.Quantity)

		// Populate with snapshot data
		p.applyUpdates(bybitMsg.Data.B, p.orderBookState.Bids)
		p.applyUpdates(bybitMsg.Data.A, p.orderBookState.Asks)

		// Update sequence number and timestamp
		p.orderBookState.Sequence = uint64(bybitMsg.Data.Seq)
		p.orderBookState.LastUpdateTime = uint64(bybitMsg.Ts)

		// Note: After a snapshot, you might need to request deltas starting from seq + 1.
		// Bybit's v5 stream might handle this automatically, but it's worth verifying docs.

	case "delta":
		// Check sequence number for consistency
		expectedSeq := p.orderBookState.Sequence + 1
		if p.orderBookState.Sequence == 0 {
			// We haven't received a snapshot yet, ignore deltas
			// log.Printf("Received delta before snapshot for %s, sequence %d", p.symbol, bybitMsg.Data.Seq) // Uncomment for verbose logging
			return
		} else if uint64(bybitMsg.Data.Seq) < expectedSeq {
			// Received an old delta, ignore
			log.Printf("Received old delta for %s: current sequence %d, received %d", p.symbol, p.orderBookState.Sequence, bybitMsg.Data.Seq)
			return
		} else if uint64(bybitMsg.Data.Seq) > expectedSeq {
			// Sequence gap detected! Need to resync.
			log.Printf("Sequence gap detected for %s! Current sequence %d, received %d. Resync needed.", p.symbol, p.orderBookState.Sequence, bybitMsg.Data.Seq)
			// Clear the order book state to indicate it's invalid
			p.orderBookState.Asks = make(map[types.Price]types.Quantity)
			p.orderBookState.Bids = make(map[types.Price]types.Quantity)
			p.orderBookState.Sequence = 0 // Reset sequence to indicate need for snapshot
			// In a real bot, you would now trigger a resync process, e.g.,
			// close the connection and reconnect to get a new snapshot.
			return // Stop processing this delta and subsequent ones until resynced
		}

		// If we reached here, the sequence number is the expected one. Apply delta updates.
		p.applyUpdates(bybitMsg.Data.B, p.orderBookState.Bids)
		p.applyUpdates(bybitMsg.Data.A, p.orderBookState.Asks)

		// Update sequence number and timestamp
		p.orderBookState.Sequence = uint64(bybitMsg.Data.Seq)
		p.orderBookState.LastUpdateTime = uint64(bybitMsg.Ts)

	default:
		// Handle other message types if necessary (e.g., command responses, errors)
		// log.Printf("Received unknown message type: %s, topic: %s", bybitMsg.Type, bybitMsg.Topic) // Uncomment for verbose logging
	}
}

// applyUpdates applies a list of updates ([Price, Quantity] pairs) to a map (Bids or Asks).
// Quantity "0" means delete the price level.
func (p *BybitVWAPProcessor) applyUpdates(updates [][]string, orderBookMap map[types.Price]types.Quantity) {
	for _, update := range updates {
		if len(update) == 2 {
			priceFloat, err := strconv.ParseFloat(update[0], 64)
			if err != nil {
				log.Printf("Error parsing price '%s' in update: %v", update[0], err)
				continue
			}
			qtyFloat, err := strconv.ParseFloat(update[1], 64)
			if err != nil {
				log.Printf("Error parsing quantity '%s' in update: %v", update[1], err)
				continue
			}

			price := types.Price(priceFloat * 1_000_000)
			quantity := types.Quantity(qtyFloat * 1_000_000)

			if quantity == 0 {
				// If quantity is 0, remove the price level
				delete(orderBookMap, price)
			} else {
				// Otherwise, add or update the price level
				orderBookMap[price] = quantity
			}
		}
	}
}

// calculateVWAP calculates the Volume Weighted Average Price for the internal OrderBook state.
func (p *BybitVWAPProcessor) calculateVWAP() types.Price {
	// Use RLock for reading data safely from the internal OrderBook struct's mutex.
	p.orderBookState.mu.RLock()
	defer p.orderBookState.mu.RUnlock() // Ensure unlock happens

	var sumPQ, sumQ int64 // sum of (Price * Quantity) and sum of Quantity

	// Calculate sum of (Price * Quantity) and sum of Quantity for Bids
	for price, qty := range p.orderBookState.Bids {
		sumPQ += int64(price) * int64(qty)
		sumQ += int64(qty)
	}

	// Calculate sum of (Price * Quantity) and sum of Quantity for Asks
	for price, qty := range p.orderBookState.Asks {
		sumPQ += int64(price) * int64(qty)
		sumQ += int64(qty)
	}

	// Avoid division by zero if there are no bids or asks
	if sumQ == 0 {
		return 0 // Return 0 VWAP if total quantity is zero
	}

	// Calculate VWAP: (Sum of Price * Quantity) / (Sum of Quantity)
	return types.Price(sumPQ / sumQ)
}

