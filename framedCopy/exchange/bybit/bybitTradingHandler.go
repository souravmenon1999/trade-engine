package bybit

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
	exchange"github.com/souravmenon1999/trade-engine/framedCopy/exchange"
	"github.com/souravmenon1999/trade-engine/framedCopy/types"
	"github.com/rs/zerolog/log"


)

type BybitTradingHandler struct {
	tradingHandler exchange.TradingHandler
}

func NewBybitTradingHandler(tradingHandler exchange.TradingHandler) *BybitTradingHandler {
	return &BybitTradingHandler{tradingHandler: tradingHandler}
}

func (h *BybitTradingHandler) Handle(message []byte) error {
	var msg struct {
		Topic string        `json:"topic"`
		Data  []interface{} `json:"data"`
	}
	if err := json.Unmarshal(message, &msg); err != nil {
		return fmt.Errorf("failed to unmarshal trading message: %w", err)
	}
	if msg.Topic != "order" {
		return fmt.Errorf("unexpected topic: %s", msg.Topic)
	}
	for _, item := range msg.Data {
		orderData, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		clientOrderID, _ := orderData["orderLinkId"].(string)
		orderID, _ := orderData["orderId"].(string)
		status, _ := orderData["orderStatus"].(string)
		price, _ := orderData["price"].(string)
		filledQty, _ := orderData["cumExecQty"].(string)

		var updateType types.OrderUpdateType
		var success bool
		switch status {
		case "New", "Created":
			updateType = types.OrderUpdateTypeCreated
			success = true
		case "Filled", "PartiallyFilled":
			updateType = types.OrderUpdateTypeFill
			success = true
		case "Cancelled":
			updateType = types.OrderUpdateTypeCanceled
			success = true
		case "Rejected":
			updateType = types.OrderUpdateTypeRejected
			success = false
		default:
			continue
		}

		fillQty, _ := strconv.ParseFloat(filledQty, 64)
		fillPrice, _ := strconv.ParseFloat(price, 64)

		var orderStatus types.OrderStatus
		switch status {
		case "New", "Created":
			orderStatus = types.OrderStatusOpen
		case "PartiallyFilled":
			orderStatus = types.OrderStatusPartiallyFilled
		case "Filled":
			orderStatus = types.OrderStatusFilled
		case "Cancelled":
			orderStatus = types.OrderStatusCancelled
		case "Rejected":
			orderStatus = types.OrderStatusRejected
		default:
			orderStatus = types.OrderStatusUnknown
		}

		update := &types.OrderUpdate{
			Success:         success,
			UpdateType:      updateType,
			Status:          orderStatus,
			ErrorMessage:    nil,
			RequestID:       &clientOrderID,
			ExchangeOrderID: &orderID,
			FillQty:         &fillQty,
			FillPrice:       &fillPrice,
			UpdatedAt:       time.Now().UnixMilli(),
			IsMaker:         orderData["isMaker"] == true,
			AmendType:       "",
			NewPrice:        nil,
			NewQty:          nil,
		}

		h.tradingHandler.OnOrderUpdate(update)
	}

	log.Info().Str("topic", "order").Msg("7:26AM INF Trading update processed successfully")
	return nil
}