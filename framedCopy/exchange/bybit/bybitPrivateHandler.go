package bybit

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	exchange"github.com/souravmenon1999/trade-engine/framedCopy/exchange"
	"github.com/souravmenon1999/trade-engine/framedCopy/types"
)

type BybitPrivateHandler struct {
	executionHandler exchange.ExecutionHandler
	accountHandler   exchange.AccountHandler
}

func NewBybitPrivateHandler(executionHandler exchange.ExecutionHandler, accountHandler exchange.AccountHandler) *BybitPrivateHandler {
	return &BybitPrivateHandler{
		executionHandler: executionHandler,
		accountHandler:   accountHandler,
	}
}

func (h *BybitPrivateHandler) Handle(message []byte) error {
	var msg struct {
		Topic string        `json:"topic"`
		Data  []interface{} `json:"data"`
	}
	if err := json.Unmarshal(message, &msg); err != nil {
		return fmt.Errorf("failed to unmarshal private message: %w", err)
	}
	switch msg.Topic {
	case "position":
		positionUpdate, err := parseBybitPositionUpdate(message)
		if err != nil {
			h.accountHandler.OnPositionError(err.Error())
			return err
		}
		h.accountHandler.OnPositionUpdate(positionUpdate)
	case "execution.fast":
		updates, err := parseExecutionUpdates(message)
		if err != nil {
			h.executionHandler.OnExecutionError(err.Error())
			return err
		}
		h.executionHandler.OnExecutionUpdate(updates)
	case "wallet":
		walletUpdate, err := parseBybitWalletUpdate(message)
		if err != nil {
			h.accountHandler.OnPositionError(err.Error())
			return err
		}
		h.accountHandler.OnAccountUpdate(walletUpdate)
	default:
		return fmt.Errorf("unknown topic: %s", msg.Topic)
	}
	return nil
}

func parseExecutionUpdates(data []byte) ([]*types.OrderUpdate, error) {
	var msg struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	var updates []*types.OrderUpdate
	for _, execData := range msg.Data {
		orderID, _ := execData["orderId"].(string)
		priceStr, _ := execData["price"].(string)
		qtyStr, _ := execData["qty"].(string)
		isMaker, _ := execData["isMaker"].(bool)
		timestamp, _ := execData["timestamp"].(float64)

		price, _ := strconv.ParseFloat(priceStr, 64)
		qty, _ := strconv.ParseFloat(qtyStr, 64)

		update := &types.OrderUpdate{
			Success:         true,
			UpdateType:      types.OrderUpdateTypeFill,
			Status:          types.OrderStatusFilled,
			ErrorMessage:    nil,
			RequestID:       nil,
			ExchangeOrderID: &orderID,
			FillQty:         &qty,
			FillPrice:       &price,
			UpdatedAt:       int64(timestamp),
			IsMaker:         isMaker,
			AmendType:       "",
			NewPrice:        nil,
			NewQty:          nil,
		}
		updates = append(updates, update)
	}
	return updates, nil
}

func parseBybitWalletUpdate(data []byte) (*types.AccountUpdate, error) {
	var walletData struct {
		Data []struct {
			WalletBalance    float64 `json:"walletBalance"`
			AvailableBalance float64 `json:"availableBalance"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &walletData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal wallet data: %w", err)
	}
	if len(walletData.Data) == 0 {
		return nil, fmt.Errorf("no wallet data found")
	}
	exchangeID := types.ExchangeIDBybit
	return &types.AccountUpdate{
		Exchange:  &exchangeID,
		AccountIM: walletData.Data[0].WalletBalance,
		AccountMM: walletData.Data[0].AvailableBalance,
	}, nil
}

func parseBybitPositionUpdate(data []byte) (*types.Position, error) {
	var positionData struct {
		Data []struct {
			Symbol        string  `json:"symbol"`
			Side          string  `json:"side"`
			Size          float64 `json:"size"`
			EntryPrice    float64 `json:"entryPrice"`
			UnrealizedPnL float64 `json:"unrealisedPnl"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &positionData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal position data: %w", err)
	}
	if len(positionData.Data) == 0 {
		return nil, fmt.Errorf("no position data found")
	}
	pos := positionData.Data[0]
	instrument := &types.Instrument{Symbol: pos.Symbol}
	side := types.Side(strings.ToUpper(pos.Side))
	exchangeID := types.ExchangeIDBybit
	return &types.Position{
		Exchange:      &exchangeID,
		Instrument:    instrument,
		Side:          side,
		Quantity:      pos.Size,
		EntryPrice:    pos.EntryPrice,
		UnrealizedPnL: pos.UnrealizedPnL,
	}, nil
}