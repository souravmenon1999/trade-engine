package bybit

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"github.com/souravmenon1999/trade-engine/framedCopy/types"
)

type BybitMDHandler struct {
	client *BybitClient
	symbol string
}

func NewBybitMDHandler(client *BybitClient, symbol string) *BybitMDHandler {
	return &BybitMDHandler{
		client: client,
		symbol: symbol,
	}
}

func (h *BybitMDHandler) Handle(message []byte) error {
	var msg struct {
		Topic string `json:"topic"`
		Type  string `json:"type"`
	}
	if err := json.Unmarshal(message, &msg); err != nil {
		return fmt.Errorf("failed to unmarshal message: %w", err)
	}

	if strings.HasPrefix(msg.Topic, "orderbook.") {
		var resp struct {
			Data struct {
				B   [][]string `json:"b"`
				A   [][]string `json:"a"`
				Seq int64      `json:"seq"`
			} `json:"data"`
			Ts  int64  `json:"ts"`
			Type string `json:"type"` // "snapshot" or "delta"
		}
		if err := json.Unmarshal(message, &resp); err != nil {
			return fmt.Errorf("failed to unmarshal orderbook data: %w", err)
		}

		sequence := resp.Data.Seq

		// Handle snapshot
		if resp.Type == "snapshot" {
			// Rely on the orderbook to clear itself based on sequence or type if needed
			// Apply bids from snapshot
			for _, b := range resp.Data.B {
				price, err := strconv.ParseFloat(b[0], 64)
				if err != nil {
					return fmt.Errorf("invalid bid price: %v", err)
				}
				quantity, err := strconv.ParseFloat(b[1], 64)
				if err != nil {
					return fmt.Errorf("invalid bid quantity: %v", err)
				}
				update := types.NewOrderBookUpdate(price, quantity, types.SideBuy, 1, sequence)
				if h.client.orderbookHandler != nil {
					h.client.orderbookHandler.OnOrderbookUpdate(types.ExchangeIDBybit, update)
				}
			}

			// Apply asks from snapshot
			for _, a := range resp.Data.A {
				price, err := strconv.ParseFloat(a[0], 64)
				if err != nil {
					return fmt.Errorf("invalid ask price: %v", err)
				}
				quantity, err := strconv.ParseFloat(a[1], 64)
				if err != nil {
					return fmt.Errorf("invalid ask quantity: %v", err)
				}
				update := types.NewOrderBookUpdate(price, quantity, types.SideSell, 1, sequence)
				if h.client.orderbookHandler != nil {
					h.client.orderbookHandler.OnOrderbookUpdate(types.ExchangeIDBybit, update)
				}
			}
		} else { // Handle delta
			// Apply delta updates for bids
			for _, b := range resp.Data.B {
				price, err := strconv.ParseFloat(b[0], 64)
				if err != nil {
					return fmt.Errorf("invalid bid price: %v", err)
				}
				quantity, err := strconv.ParseFloat(b[1], 64)
				if err != nil {
					return fmt.Errorf("invalid bid quantity: %v", err)
				}
				update := types.NewOrderBookUpdate(price, quantity, types.SideBuy, 1, sequence)
				if quantity == 0 {
					update.OrderCount = 0 // Removal
				}
				if h.client.orderbookHandler != nil {
					h.client.orderbookHandler.OnOrderbookUpdate(types.ExchangeIDBybit, update)
				}
			}

			// Apply delta updates for asks
			for _, a := range resp.Data.A {
				price, err := strconv.ParseFloat(a[0], 64)
				if err != nil {
					return fmt.Errorf("invalid ask price: %v", err)
				}
				quantity, err := strconv.ParseFloat(a[1], 64)
				if err != nil {
					return fmt.Errorf("invalid ask quantity: %v", err)
				}
				update := types.NewOrderBookUpdate(price, quantity, types.SideSell, 1, sequence)
				if quantity == 0 {
					update.OrderCount = 0 // Removal
				}
				if h.client.orderbookHandler != nil {
					h.client.orderbookHandler.OnOrderbookUpdate(types.ExchangeIDBybit, update)
				}
			}
		}

		return nil
	} else if strings.HasPrefix(msg.Topic, "tickers.") {
		fundingRate, err := parseBybitFundingRate(message, h.symbol)
		if err != nil {
			if h.client.fundingRateHandler != nil {
				h.client.fundingRateHandler.OnFundingRateError(fmt.Sprintf("Failed to parse funding rate: %v", err))
			}
			return err
		}
		if fundingRate != nil && h.client.fundingRateHandler != nil {
			h.client.fundingRateHandler.OnFundingRateUpdate(types.ExchangeIDBybit, fundingRate)
		}
		return nil
	}

	return fmt.Errorf("unknown topic: %s", msg.Topic)
}

type BybitFundingRateResponse struct {
	Topic string `json:"topic"`
	Data  struct {
		Symbol          string `json:"symbol"`
		FundingRate     string `json:"fundingRate"`
		NextFundingTime string `json:"nextFundingTime"`
	} `json:"data"`
	Ts int64 `json:"ts"`
}

func parseBybitFundingRate(data []byte, symbol string) (*types.FundingRate, error) {
	var resp BybitFundingRateResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal funding rate data: %w", err)
	}
	if resp.Data.Symbol != symbol {
		return nil, fmt.Errorf("unexpected symbol: got %s, want %s", resp.Data.Symbol, symbol)
	}
	if resp.Data.FundingRate == "" {
		return nil, nil
	}
	rate, err := strconv.ParseFloat(resp.Data.FundingRate, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid funding rate: %v", err)
	}
	var lastUpdated int64
	if resp.Data.NextFundingTime != "" {
		nextTime, err := strconv.ParseInt(resp.Data.NextFundingTime, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid next funding time: %v", err)
		}
		lastUpdated = nextTime/1000 - 8*60*60 // Adjust for 8-hour funding interval
	} else {
		lastUpdated = resp.Ts / 1000 // Fallback to message timestamp
	}
	return &types.FundingRate{
		Rate:        rate,
		Interval:    8 * time.Hour,
		LastUpdated: lastUpdated,
	}, nil
}