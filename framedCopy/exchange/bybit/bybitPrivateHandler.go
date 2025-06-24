package bybit

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	//"github.com/rs/zerolog/log"

	//exchange"github.com/souravmenon1999/trade-engine/framedCopy/exchange"
	"github.com/souravmenon1999/trade-engine/framedCopy/types"
)

type BybitPrivateHandler struct {
    client *BybitClient // Reference to BybitClient for accessing current handlers
}

func NewBybitPrivateHandler(client *BybitClient) *BybitPrivateHandler {
    return &BybitPrivateHandler{
        client: client,
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
            if h.client.accountHandler != nil {
                h.client.accountHandler.OnPositionError(err.Error())
            }
            return err
        }
        if h.client.accountHandler != nil {
            h.client.accountHandler.OnPositionUpdate(positionUpdate)
        }
    case "execution.fast":
        updates, err := parseExecutionUpdates(message)
        if err != nil {
            if h.client.executionHandler != nil {
                h.client.executionHandler.OnExecutionError(err.Error())
            }
            return err
        }
        if h.client.executionHandler != nil {
            h.client.executionHandler.OnExecutionUpdate(updates)
        }
    case "wallet":
        walletUpdate, err := parseBybitWalletUpdate(message)
        if err != nil {
            if h.client.accountHandler != nil {
                h.client.accountHandler.OnPositionError(err.Error()) // Consider a specific error method
            }
            return err
        }
        if h.client.accountHandler != nil {
            h.client.accountHandler.OnAccountUpdate(walletUpdate)
        }
    case "order":
        orderUpdate, err := parseOrderUpdate(message)
        if err != nil {
            if h.client.tradingHandler != nil {
                h.client.tradingHandler.OnOrderError(err.Error())
            }
            return err
        }
        if h.client.tradingHandler != nil {
            h.client.tradingHandler.OnOrderUpdate(orderUpdate)
        }
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
            Symbol        string `json:"symbol"`
            Side          string `json:"side"`
            Size          string `json:"size"`
            EntryPrice    string `json:"entryPrice"`
            UnrealizedPnL string `json:"unrealisedPnl"`
        } `json:"data"`
    }
    if err := json.Unmarshal(data, &positionData); err != nil {
        return nil, fmt.Errorf("failed to unmarshal position data: %w", err)
    }
    if len(positionData.Data) == 0 {
        return nil, fmt.Errorf("no position data found")
    }
    pos := positionData.Data[0]

    // Parse string fields to float64
    size, err := strconv.ParseFloat(pos.Size, 64)
    if err != nil {
        return nil, fmt.Errorf("failed to parse size: %w", err)
    }
    entryPrice, err := strconv.ParseFloat(pos.EntryPrice, 64)
    if err != nil {
        return nil, fmt.Errorf("failed to parse entryPrice: %w", err)
    }
    unrealizedPnL, err := strconv.ParseFloat(pos.UnrealizedPnL, 64)
    if err != nil {
        return nil, fmt.Errorf("failed to parse unrealisedPnl: %w", err)
    }

    instrument := &types.Instrument{Symbol: pos.Symbol}
    side := types.Side(strings.ToUpper(pos.Side))
    exchangeID := types.ExchangeIDBybit
    return &types.Position{
        Exchange:      &exchangeID,
        Instrument:    instrument,
        Side:          side,
        Quantity:      size,
        EntryPrice:    entryPrice,
        UnrealizedPnL: unrealizedPnL,
    }, nil
}

// mapBybitStatusToOrderStatus converts Bybit's string status to types.OrderStatus
func mapBybitStatusToOrderStatus(status string) types.OrderStatus {
    switch status {
    case "Created", "New":
        return types.OrderStatusSubmitted
    case "PartiallyFilled":
        return types.OrderStatusPartiallyFilled
    case "Filled":
        return types.OrderStatusFilled
    case "Cancelled":
        return types.OrderStatusCancelled
    case "Rejected":
        return types.OrderStatusRejected
    default:
        return types.OrderStatusUnknown
    }
}

// parseOrderUpdate parses a Bybit order update from WebSocket data
func parseOrderUpdate(data []byte) (*types.OrderUpdate, error) {
    var msg struct {
        Data []struct {
            OrderID    string `json:"orderId"`
            Price      string `json:"price"`
            Qty        string `json:"qty"`
            Status     string `json:"orderStatus"`
            Timestamp  int64  `json:"updatedTime"`
            OrderLinkID string `json:"orderLinkId"` // Added to capture ClientOrderID
        } `json:"data"`
    }
    if err := json.Unmarshal(data, &msg); err != nil {
        return nil, fmt.Errorf("failed to unmarshal order data: %w", err)
    }
    if len(msg.Data) == 0 {
        return nil, fmt.Errorf("no order data found")
    }
    order := msg.Data[0]

    // Parse string fields to float64
    price, err := strconv.ParseFloat(order.Price, 64)
    if err != nil {
        return nil, fmt.Errorf("failed to parse price: %w", err)
    }
    qty, err := strconv.ParseFloat(order.Qty, 64)
    if err != nil {
        return nil, fmt.Errorf("failed to parse qty: %w", err)
    }

    // Map Bybit status to our OrderStatus
    status := mapBybitStatusToOrderStatus(order.Status)

    // Determine update type based on status
    var updateType types.OrderUpdateType
    switch order.Status {
    case "Created", "New":
        updateType = types.OrderUpdateTypeCreated
    case "Filled", "PartiallyFilled":
        updateType = types.OrderUpdateTypeFill
    case "Cancelled":
        updateType = types.OrderUpdateTypeCanceled
    case "Rejected":
        updateType = types.OrderUpdateTypeRejected
    default:
        updateType = types.OrderUpdateTypeOther
    }

    // Build the OrderUpdate with parsed values
    update := &types.OrderUpdate{
        Success:         true,
        UpdateType:      updateType,
        Status:          status,
        ExchangeOrderID: &order.OrderID,
        FillQty:         &qty,
        FillPrice:       &price,
        UpdatedAt:       order.Timestamp,
    }

    // If orderLinkId is present, set it as RequestID
    if order.OrderLinkID != "" {
        update.RequestID = &order.OrderLinkID
    }

    return update, nil
}