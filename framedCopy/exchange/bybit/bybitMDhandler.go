package bybit

import (
	"encoding/json"
	"fmt"
	"strconv"
	exchange"github.com/souravmenon1999/trade-engine/framedCopy/exchange"
	"github.com/souravmenon1999/trade-engine/framedCopy/types"
)

type BybitMDHandler struct {
	orderbookHandler exchange.OrderbookHandler
	symbol           string
}

func NewBybitMDHandler(orderbookHandler exchange.OrderbookHandler, symbol string) *BybitMDHandler {
	return &BybitMDHandler{
		orderbookHandler: orderbookHandler,
		symbol:           symbol,
	}
}

func (h *BybitMDHandler) Handle(message []byte) error {
	orderbook, err := parseBybitOrderbook(message, h.symbol)
	if err != nil {
		if h.orderbookHandler != nil {
			h.orderbookHandler.OnOrderbookError(fmt.Sprintf("Failed to parse orderbook: %v", err))
		}
		return err
	}
	if h.orderbookHandler != nil {
		h.orderbookHandler.OnOrderbook(orderbook)
	}
	// log.Info().Str("symbol", h.symbol).Msg("7:26AM INF Orderbook update processed successfully")
	return nil
}

type BybitOrderbookResponse struct {
	Topic string `json:"topic"`
	Type  string `json:"type"`
	Data  struct {
		S   string     `json:"s"`
		B   [][]string `json:"b"`
		A   [][]string `json:"a"`
		U   int64      `json:"u"`
		Seq int64      `json:"seq"`
	} `json:"data"`
	Ts int64 `json:"ts"`
}

func parseBybitOrderbook(data []byte, symbol string) (*types.OrderBook, error) {
	var resp BybitOrderbookResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal orderbook data: %w", err)
	}
	instrument := &types.Instrument{Symbol: symbol}
	exchange := types.ExchangeIDBybit
	orderbook := types.NewOrderBook(instrument, &exchange)

	for _, b := range resp.Data.B {
		price, err := strconv.ParseFloat(b[0], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid bid price: %v", err)
		}
		quantity, err := strconv.ParseFloat(b[1], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid bid quantity: %v", err)
		}
		update := types.BidUpdate(price, quantity, 1)
		orderbook.ApplyUpdate(update)
	}

	for _, a := range resp.Data.A {
		price, err := strconv.ParseFloat(a[0], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid ask price: %v", err)
		}
		quantity, err := strconv.ParseFloat(a[1], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid ask quantity: %v", err)
		}
		update := types.AskUpdate(price, quantity, 1)
		orderbook.ApplyUpdate(update)
	}
	
	orderbook.SetLastUpdateTime(resp.Ts)
	orderbook.SetSequence(resp.Data.Seq)
	return orderbook, nil
}