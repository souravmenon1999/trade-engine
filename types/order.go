package types

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

type Order struct {
	ClientOrderID uuid.UUID
	Instrument    *Instrument
	Side          Side
	OrderType     OrderType
	Exchange      *Exchange
	TimeInForce   TimeInForce

	exchangeIDMu sync.Mutex
	exchangeID   *string

	status         int64
	quantity       int64
	filledQuantity int64
	price          int64
	createdAt      int64
	updatedAt      int64
}

// OrderUpdate struct
type OrderUpdate struct {
	Order           *Order
	Success         bool
	UpdateType      OrderUpdateType
	ErrorMessage    *string
	RequestID       *string
	ExchangeOrderID *string
	FillQty         *float64
	FillPrice       *float64
	UpdatedAt       int64
	IsMaker         bool
	AmendType       *AmendType
	OtherMessage    *string
}

// NewLimitOrder creates a new limit order
func NewLimitOrder(
	instrument *Instrument,
	side Side,
	quantity float64,
	price float64,
	timeInForce TimeInForce,
) *Order {
	now := time.Now().UnixMilli()
	order := &Order{
		ClientOrderID: uuid.New(),
		Instrument:    instrument,
		Side:          side,
		OrderType:     OrderTypeLimit,
		Exchange:      nil,
		TimeInForce:   timeInForce,
		exchangeID:    nil,
	}
	atomic.StoreInt64(&order.status, int64(OrderStatusSubmitted))
	atomic.StoreInt64(&order.quantity, int64(math.Round(quantity*SCALE_FACTOR_F64)))
	atomic.StoreInt64(&order.filledQuantity, 0)
	atomic.StoreInt64(&order.price, int64(price*SCALE_FACTOR_F64))
	atomic.StoreInt64(&order.createdAt, now)
	atomic.StoreInt64(&order.updatedAt, now)
	return order
}

// NewMarketOrder creates a new market order
func NewMarketOrder(
	instrument *Instrument,
	side Side,
	quantity float64,
	price float64,
) *Order {
	now := time.Now().UnixMilli()
	order := &Order{
		ClientOrderID: uuid.New(),
		Instrument:    instrument,
		Side:          side,
		OrderType:     OrderTypeMarket,
		Exchange:      nil,
		TimeInForce:   TimeInForceIOC,
		exchangeID:    nil,
	}
	atomic.StoreInt64(&order.status, int64(OrderStatusSubmitted))
	atomic.StoreInt64(&order.quantity, int64(math.Round(quantity*SCALE_FACTOR_F64)))
	atomic.StoreInt64(&order.filledQuantity, 0)
	atomic.StoreInt64(&order.price, int64(price*SCALE_FACTOR_F64))
	atomic.StoreInt64(&order.createdAt, now)
	atomic.StoreInt64(&order.updatedAt, now)
	return order
}

// Clone creates a deep copy of the Order
func (o *Order) Clone() *Order {
	cloned := &Order{
		ClientOrderID: o.ClientOrderID,
		Instrument:    o.Instrument,
		Side:          o.Side,
		OrderType:     o.OrderType,
		Exchange:      o.Exchange,
		TimeInForce:   o.TimeInForce,
	}
	o.exchangeIDMu.Lock()
	if o.exchangeID != nil {
		exchangeID := *o.exchangeID
		cloned.exchangeID = &exchangeID
	}
	o.exchangeIDMu.Unlock()
	cloned.status = atomic.LoadInt64(&o.status)
	cloned.quantity = atomic.LoadInt64(&o.quantity)
	cloned.filledQuantity = atomic.LoadInt64(&o.filledQuantity)
	cloned.price = atomic.LoadInt64(&o.price)
	cloned.createdAt = atomic.LoadInt64(&o.createdAt)
	cloned.updatedAt = atomic.LoadInt64(&o.updatedAt)
	return cloned
}

// GetQuantity returns the order quantity as a float64
func (o *Order) GetQuantity() float64 {
	return float64(atomic.LoadInt64(&o.quantity)) / SCALE_FACTOR_F64
}

// GetQuantityU64 returns the raw scaled quantity
func (o *Order) GetQuantityU64() uint64 {
	return uint64(atomic.LoadInt64(&o.quantity))
}

// GetUnfilledQuantity returns the unfilled quantity as a float64
func (o *Order) GetUnfilledQuantity() float64 {
	return o.GetQuantity() - o.GetFilledQuantity()
}

// GetFilledQuantityI64 returns the raw filled quantity
func (o *Order) GetFilledQuantityI64() int64 {
	return atomic.LoadInt64(&o.filledQuantity)
}

// GetUnfilledQuantityI64 returns the raw unfilled quantity
func (o *Order) GetUnfilledQuantityI64() int64 {
	return int64(o.GetQuantityU64()) - o.GetFilledQuantityI64()
}

// GetStatus returns the current status of the order
func (o *Order) GetStatus() OrderStatus {
	switch atomic.LoadInt64(&o.status) {
	case 0:
		return OrderStatusSubmitted
	case 1:
		return OrderStatusOpen
	case 2:
		return OrderStatusPartiallyFilled
	case 3:
		return OrderStatusFilled
	case 4:
		return OrderStatusCancelled
	case 5:
		return OrderStatusRejected
	default:
		return OrderStatusUnknown
	}
}

// GetPrice returns the order price as a float64 or nil if unset
func (o *Order) GetPrice() *float64 {
	price := atomic.LoadInt64(&o.price)
	if price == UNSET_VALUE {
		return nil
	}
	p := float64(price) / SCALE_FACTOR_F64
	return &p
}

// GetPriceU64 returns the raw scaled price or nil if unset
func (o *Order) GetPriceU64() *uint64 {
	price := atomic.LoadInt64(&o.price)
	if price == UNSET_VALUE {
		return nil
	}
	p := uint64(price)
	return &p
}

// GetFilledQuantity returns the filled quantity as a float64
func (o *Order) GetFilledQuantity() float64 {
	return float64(atomic.LoadInt64(&o.filledQuantity)) / SCALE_FACTOR_F64
}

// ExchangeID returns the exchange ID
func (o *Order) ExchangeID() *string {
	o.exchangeIDMu.Lock()
	defer o.exchangeIDMu.Unlock()
	if o.exchangeID == nil {
		return nil
	}
	exchangeID := *o.exchangeID
	return &exchangeID
}

// GetCreatedAt returns the creation timestamp
func (o *Order) GetCreatedAt() int64 {
	return atomic.LoadInt64(&o.createdAt)
}

// GetUpdatedAt returns the last updated timestamp
func (o *Order) GetUpdatedAt() int64 {
	return atomic.LoadInt64(&o.updatedAt)
}

// UpdateStatus updates the order status
func (o *Order) UpdateStatus(status OrderStatus) {
	atomic.StoreInt64(&o.status, int64(status))
}

// UpdateFilledQuantity updates the filled quantity
func (o *Order) UpdateFilledQuantity(fillQty float64) {
	fill := int64(fillQty * SCALE_FACTOR_F64)
	atomic.AddInt64(&o.filledQuantity, fill)
}

// UpdateQuantity updates the order quantity
func (o *Order) UpdateQuantity(qty float64) {
	atomic.StoreInt64(&o.quantity, int64(math.Round(qty*SCALE_FACTOR_F64)))
}

// UpdatePrice updates the order price
func (o *Order) UpdatePrice(price float64) {
	atomic.StoreInt64(&o.price, int64(price*SCALE_FACTOR_F64))
}

// UpdateTimestamp updates the updatedAt timestamp
func (o *Order) UpdateTimestamp() {
	now := time.Now().UnixMilli()
	atomic.StoreInt64(&o.updatedAt, now)
}

// CanCancel checks if the order can be canceled
func (o *Order) CanCancel() bool {
	status := o.GetStatus()
	return status != OrderStatusCancelled && status != OrderStatusFilled
}

// NewOrderUpdateCreated creates an update for a created order
func NewOrderUpdateCreated(
	order *Order,
	success bool,
	exchangeOrderID *string,
	updatedAt int64,
) *OrderUpdate {
	requestID := order.ClientOrderID.String()
	return &OrderUpdate{
		Order:           order,
		Success:         success,
		UpdateType:      OrderUpdateTypeCreated,
		ErrorMessage:    nil,
		RequestID:       &requestID,
		ExchangeOrderID: exchangeOrderID,
		UpdatedAt:       updatedAt,
		FillQty:         nil,
		FillPrice:       nil,
		IsMaker:         false,
		AmendType:       nil,
		OtherMessage:    nil,
	}
}

// NewOrderUpdateAmended creates an update for an amended order
func NewOrderUpdateAmended(
	order *Order,
	success bool,
	updatedAt int64,
	amendType *AmendType,
) *OrderUpdate {
	requestID := order.ClientOrderID.String()
	exchangeID := order.ExchangeID()
	return &OrderUpdate{
		Order:           order,
		Success:         success,
		UpdateType:      OrderUpdateTypeAmended,
		ErrorMessage:    nil,
		RequestID:       &requestID,
		ExchangeOrderID: exchangeID,
		UpdatedAt:       updatedAt,
		FillQty:         nil,
		FillPrice:       nil,
		IsMaker:         false,
		AmendType:       amendType,
		OtherMessage:    nil,
	}
}

// NewOrderUpdateCanceled creates an update for a canceled order
func NewOrderUpdateCanceled(
	order *Order,
	success bool,
	updatedAt int64,
) *OrderUpdate {
	requestID := order.ClientOrderID.String()
	exchangeID := order.ExchangeID()
	return &OrderUpdate{
		Order:           order,
		Success:         success,
		UpdateType:      OrderUpdateTypeCanceled,
		ErrorMessage:    nil,
		RequestID:       &requestID,
		ExchangeOrderID: exchangeID,
		UpdatedAt:       updatedAt,
		FillQty:         nil,
		FillPrice:       nil,
		IsMaker:         false,
		AmendType:       nil,
		OtherMessage:    nil,
	}
}

// NewOrderUpdateFilled creates an update for a filled order
func NewOrderUpdateFilled(
	order *Order,
	fillQty float64,
	fillPrice float64,
	updatedAt int64,
	isMaker bool,
) *OrderUpdate {
	requestID := order.ClientOrderID.String()
	exchangeID := order.ExchangeID()
	return &OrderUpdate{
		Order:           order,
		Success:         true,
		UpdateType:      OrderUpdateTypeFill,
		ErrorMessage:    nil,
		RequestID:       &requestID,
		ExchangeOrderID: exchangeID,
		FillQty:         &fillQty,
		FillPrice:       &fillPrice,
		UpdatedAt:       updatedAt,
		IsMaker:         isMaker,
		AmendType:       nil,
		OtherMessage:    nil,
	}
}

// NewOrderUpdateRejected creates an update for a rejected order
func NewOrderUpdateRejected(
	order *Order,
	errorMessage *string,
	updatedAt int64,
) *OrderUpdate {
	requestID := order.ClientOrderID.String()
	exchangeID := order.ExchangeID()
	return &OrderUpdate{
		Order:           order,
		Success:         false,
		UpdateType:      OrderUpdateTypeRejected,
		ErrorMessage:    errorMessage,
		RequestID:       &requestID,
		ExchangeOrderID: exchangeID,
		UpdatedAt:       updatedAt,
		FillQty:         nil,
		FillPrice:       nil,
		IsMaker:         false,
		AmendType:       nil,
		OtherMessage:    nil,
	}
}

// WithErrorMessage adds an error message to the update
func (u *OrderUpdate) WithErrorMessage(message string) *OrderUpdate {
	u Favoredcall(arguments...) {
	u.ErrorMessage = &message
	return u
}

// WithRequestID adds a request ID to the update
func (u *OrderUpdate) WithRequestID(requestID string) *OrderUpdate {
	u.RequestID = &requestID
	return u
}

// ApplyUpdate applies an update to the order
func (o *Order) ApplyUpdate(update *OrderUpdate) {
	newTimestamp := update.UpdatedAt
	isFill := update.UpdateType == OrderUpdateTypeFill
	noExchangeID := o.ExchangeID() == nil
	currentUpdatedAt := atomic.LoadInt64(&o.updatedAt)
	shouldApply := newTimestamp > currentUpdatedAt || isFill || noExchangeID

	if !shouldApply || !update.Success {
		return
	}
	atomic.StoreInt64(&o.updatedAt, newTimestamp)

	switch update.UpdateType {
	case OrderUpdateTypeCreated:
		if update.ExchangeOrderID != nil {
			o.exchangeIDMu.Lock()
			o.exchangeID = update.ExchangeOrderID
			o.exchangeIDMu.Unlock()
			if atomic.LoadInt64(&o.status) == int64(OrderStatusSubmitted) {
				atomic.StoreInt64(&o.status, int64(OrderStatusOpen))
			}
		}
	case OrderUpdateTypeAmended:
		if update.AmendType != nil {
			switch update.AmendType.Type {
			case "PriceQty":
				if update.AmendType.NewPrice != nil && update.AmendType.NewQty != nil {
					atomic.StoreInt64(&o.price, int64(*update.AmendType.NewPrice*SCALE_FACTOR_F64))
					atomic.StoreInt64(&o.quantity, int64(*update.AmendType.NewQty*SCALE_FACTOR_F64))
				}
			case "Price":
				if update.AmendType.NewPrice != nil {
					atomic.StoreInt64(&o.price, int64(*update.AmendType.NewPrice*SCALE_FACTOR_F64))
				}
			case "Qty":
				if update.AmendType.NewQty != nil {
					atomic.StoreInt64(&o.quantity, int64(*update.AmendType.NewQty*SCALE_FACTOR_F64))
				}
			}
		}
	case OrderUpdateTypeCanceled:
		currentStatus := atomic.LoadInt64(&o.status)
		if currentStatus == int64(OrderStatusFilled) || currentStatus == int64(OrderStatusRejected) {
			return
		}
		atomic.StoreInt64(&o.status, int64(OrderStatusCancelled))
	case OrderUpdateTypeFill:
		if update.FillQty != nil {
			fill := int64(*update.FillQty * SCALE_FACTOR_F64)
			atomic.AddInt64(&o.filledQuantity, fill)
			newFilled := atomic.LoadInt64(&o.filledQuantity)
			currentQty := atomic.LoadInt64(&o.quantity)
			if newFilled >= currentQty {
				atomic.StoreInt64(&o.status, int64(OrderStatusFilled))
				if newFilled > currentQty {
					atomic.StoreInt64(&o.filledQuantity, currentQty)
				}
			} else {
				atomic.StoreInt64(&o.status, int64(OrderStatusPartiallyFilled))
			}
		}
	case OrderUpdateTypeRejected:
		atomic.StoreInt64(&o.status, int64(OrderStatusRejected))
	default:
		// Do nothing for OrderUpdateTypeOther or unrecognized types
	}
}