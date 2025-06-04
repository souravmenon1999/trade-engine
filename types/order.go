package types

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

type Order struct {
    clientOrderID   uuid.UUID
    instrument      *Instrument // Pointer to assumed Instrument type
    side            Side
    orderType       OrderType
    exchange        *Exchange // Pointer to assumed Exchange type (nil for None)
    timeInForce     TimeInForce

    // Thread-safe fields
    exchangeIDMutex sync.RWMutex
    exchangeID      *string
    status          atomic.Int64
    quantity        atomic.Int64
    filledQuantity  atomic.Int64
    price           atomic.Int64 // Stored as price * 1_000_000
    createdAt       atomic.Int64 // Timestamp in milliseconds
    updatedAt       atomic.Int64 // Timestamp in milliseconds
}

// Clone method (approximated since Go doesn’t have direct equivalent)
func (o *Order) Clone() *Order {
    order := &Order{
        clientOrderID: o.clientOrderID,
        instrument:    o.instrument,
        side:          o.side,
        orderType:     o.orderType,
        exchange:      o.exchange,
        timeInForce:   o.timeInForce,
    }
    o.exchangeIDMutex.RLock()
    if o.exchangeID != nil {
        id := *o.exchangeID
        order.exchangeID = &id
    }
    o.exchangeIDMutex.RUnlock()
    order.status.Store(o.status.Load())
    order.quantity.Store(o.quantity.Load())
    order.filledQuantity.Store(o.filledQuantity.Load())
    order.price.Store(o.price.Load())
    order.createdAt.Store(o.createdAt.Load())
    order.updatedAt.Store(o.updatedAt.Load())
    return order
}

// NewLimit creates a new limit order
func NewLimit(instrument *Instrument, side Side, quantity float64, price float64, timeInForce TimeInForce) *Order {
    now := time.Now().UnixMilli()
    order := &Order{
        clientOrderID: uuid.New(),
        instrument:    instrument,
        side:          side,
        orderType:     OrderTypeLimit,
        exchange:      nil,
        timeInForce:   timeInForce,
        exchangeID:    nil,
    }
    order.status.Store(int64(OrderStatusSubmitted))
    order.quantity.Store(int64(math.Round(quantity * SCALE_FACTOR_F64)))
    order.filledQuantity.Store(0)
    order.price.Store(int64(math.Round(price * SCALE_FACTOR_F64)))
    order.createdAt.Store(now)
    order.updatedAt.Store(now)
    return order
}

// GetQuantity returns quantity as float64
func (o *Order) GetQuantity() float64 {
    return float64(o.quantity.Load()) / SCALE_FACTOR_F64
}

// GetQuantityU64 returns quantity as uint64
func (o *Order) GetQuantityU64() uint64 {
    return uint64(o.quantity.Load())
}

// GetUnfilledQuantity returns unfilled quantity as float64
func (o *Order) GetUnfilledQuantity() float64 {
    return o.GetQuantity() - o.GetFilledQuantity()
}

// GetFilledQuantityI64 returns filled quantity as int64
func (o *Order) GetFilledQuantityI64() int64 {
    return o.filledQuantity.Load()
}

// GetUnfilledQuantityI64 returns unfilled quantity as int64
func (o *Order) GetUnfilledQuantityI64() int64 {
    return int64(o.GetQuantityU64()) - o.GetFilledQuantityI64()
}

// GetStatus returns the current order status
func (o *Order) GetStatus() OrderStatus {
    switch o.status.Load() {
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

// GetPrice returns price as *float64 (nil if unset)
func (o *Order) GetPrice() *float64 {
    price := o.price.Load()
    if price == UNSET_VALUE {
        return nil
    }
    p := float64(price) / SCALE_FACTOR_F64
    return &p
}

// GetPriceU64 returns price as *uint64 (nil if unset)
func (o *Order) GetPriceU64() *uint64 {
    price := o.price.Load()
    if price == UNSET_VALUE {
        return nil
    }
    p := uint64(price)
    return &p
}

// GetFilledQuantity returns filled quantity as float64
func (o *Order) GetFilledQuantity() float64 {
    return float64(o.filledQuantity.Load()) / SCALE_FACTOR_F64
}

// NewMarket creates a new market order
func NewMarket(instrument *Instrument, side Side, quantity float64, price float64) *Order {
    now := time.Now().UnixMilli()
    order := &Order{
        clientOrderID: uuid.New(),
        instrument:    instrument,
        side:          side,
        orderType:     OrderTypeMarket,
        exchange:      nil,
        timeInForce:   TimeInForceIOC,
        exchangeID:    nil,
    }
    order.status.Store(int64(OrderStatusSubmitted))
    order.price.Store(int64(math.Round(price * SCALE_FACTOR_F64)))
    order.quantity.Store(int64(math.Round(quantity * SCALE_FACTOR_F64)))
    order.filledQuantity.Store(0)
    order.createdAt.Store(now)
    order.updatedAt.Store(now)
    return order
}

// UpdateStatus updates the order status
func (o *Order) UpdateStatus(status OrderStatus) {
    o.status.Store(int64(status))
}

// UpdateFilledQuantity updates the filled quantity
func (o *Order) UpdateFilledQuantity(fillQty float64) {
    fill := int64(math.Round(fillQty * SCALE_FACTOR_F64))
    o.filledQuantity.Add(fill)
}

// UpdateQuantity updates the quantity
func (o *Order) UpdateQuantity(qty float64) {
    o.quantity.Store(int64(math.Round(qty * SCALE_FACTOR_F64)))
}

// UpdatePrice updates the price
func (o *Order) UpdatePrice(price float64) {
    o.price.Store(int64(math.Round(price * SCALE_FACTOR_F64)))
}

// ExchangeID returns the exchange ID
func (o *Order) ExchangeID() *string {
    o.exchangeIDMutex.RLock()
    defer o.exchangeIDMutex.RUnlock()
    if o.exchangeID == nil {
        return nil
    }
    id := *o.exchangeID
    return &id
}

// UpdateTimestamp updates the updated_at timestamp
func (o *Order) UpdateTimestamp() {
    now := time.Now().UnixMilli()
    o.updatedAt.Store(now)
}

// GetCreatedAt returns the created_at timestamp
func (o *Order) GetCreatedAt() int64 {
    return o.createdAt.Load()
}

// GetUpdatedAt returns the updated_at timestamp
func (o *Order) GetUpdatedAt() int64 {
    return o.updatedAt.Load()
}

// CanCancel checks if the order can be canceled
func (o *Order) CanCancel() bool {
    status := o.GetStatus()
    return status != OrderStatusCancelled && status != OrderStatusFilled
}

// ApplyUpdate applies an update to the order
func (o *Order) ApplyUpdate(update *OrderUpdate) {
    newTimestamp := update.updatedAt
    isFill := update.updateType == "Fill"
    noExchangeID := func() bool {
        o.exchangeIDMutex.RLock()
        defer o.exchangeIDMutex.RUnlock()
        return o.exchangeID == nil
    }()
    shouldApply := newTimestamp > o.updatedAt.Load() || isFill || noExchangeID
    if !shouldApply || !update.success {
        return
    }
    o.updatedAt.Store(newTimestamp)

    switch update.updateType {
    case "Created":
        if update.exchangeOrderID != nil {
            o.exchangeIDMutex.Lock()
            o.exchangeID = new(string)
            *o.exchangeID = *update.exchangeOrderID
            o.exchangeIDMutex.Unlock()
            if o.status.Load() == int64(OrderStatusSubmitted) {
                o.status.Store(int64(OrderStatusOpen))
            }
        }
    case "Amended":
        switch update.amendType {
        case AmendTypePriceQty:
            if update.newPrice != nil && update.newQty != nil {
                o.price.Store(int64(math.Round(*update.newPrice * SCALE_FACTOR_F64)))
                o.quantity.Store(int64(math.Round(*update.newQty * SCALE_FACTOR_F64)))
            }
        case AmendTypePrice:
            if update.newPrice != nil {
                o.price.Store(int64(math.Round(*update.newPrice * SCALE_FACTOR_F64)))
            }
        case AmendTypeQty:
            if update.newQty != nil {
                o.quantity.Store(int64(math.Round(*update.newQty * SCALE_FACTOR_F64)))
            }
        }
    case "Canceled":
        currentStatus := o.status.Load()
        if currentStatus == int64(OrderStatusFilled) || currentStatus == int64(OrderStatusRejected) {
            return
        }
        o.status.Store(int64(OrderStatusCancelled))
    case "Fill":
        if update.fillQty != nil {
            fill := int64(math.Round(*update.fillQty * SCALE_FACTOR_F64))
            o.filledQuantity.Add(fill)
            newFilled := o.filledQuantity.Load()
            currentQty := o.quantity.Load()
            if newFilled >= currentQty {
                o.status.Store(int64(OrderStatusFilled))
                if newFilled > currentQty {
                    o.filledQuantity.Store(currentQty)
                }
            } else {
                o.status.Store(int64(OrderStatusPartiallyFilled))
            }
        }
    case "Rejected":
        o.status.Store(int64(OrderStatusRejected))
    }
}

// OrderUpdateType constants
const (
    OrderUpdateTypeCreated  = "Created"
    OrderUpdateTypeAmended  = "Amended"
    OrderUpdateTypeCanceled = "Canceled"
    OrderUpdateTypeFill     = "Fill"
    OrderUpdateTypeRejected = "Rejected"
    OrderUpdateTypeOther    = "Other"
)

// AmendType constants (assumed from Rust)
const (
    AmendTypePriceQty = iota
    AmendTypePrice
    AmendTypeQty
)

// OrderUpdate struct
type OrderUpdate struct {
    order           *Order
    success         bool
    updateType      string
    errorMessage    *string
    requestID       *string
    exchangeOrderID *string
    fillQty         *float64
    fillPrice       *float64
    updatedAt       int64
    isMaker         bool
    amendType       int     // For Amended type
    newPrice        *float64 // For Amended type
    newQty          *float64 // For Amended type
}

// OrderUpdate methods
func NewCreated(order *Order, success bool, exchangeOrderID *string, updatedAt int64) *OrderUpdate {
    requestID := order.clientOrderID.String()
    return &OrderUpdate{
        order:           order,
        success:         success,
        updateType:      OrderUpdateTypeCreated,
        errorMessage:    nil,
        requestID:       &requestID,
        exchangeOrderID: exchangeOrderID,
        updatedAt:       updatedAt,
    }
}

func NewAmended(order *Order, success bool, updatedAt int64, amendType int, newPrice, newQty *float64) *OrderUpdate {
    requestID := order.clientOrderID.String()
    return &OrderUpdate{
        order:           order,
        success:         success,
        updateType:      OrderUpdateTypeAmended,
        errorMessage:    nil,
        requestID:       &requestID,
        exchangeOrderID: order.ExchangeID(),
        updatedAt:       updatedAt,
        amendType:       amendType,
        newPrice:        newPrice,
        newQty:          newQty,
    }
}

func NewCanceled(order *Order, success bool, updatedAt int64) *OrderUpdate {
    requestID := order.clientOrderID.String()
    return &OrderUpdate{
        order:           order,
        success:         success,
        updateType:      OrderUpdateTypeCanceled,
        errorMessage:    nil,
        requestID:       &requestID,
        exchangeOrderID: order.ExchangeID(),
        updatedAt:       updatedAt,
    }
}

func NewFilled(order *Order, fillQty, fillPrice float64, updatedAt int64, isMaker bool) *OrderUpdate {
    requestID := order.clientOrderID.String()
    fQty := fillQty
    fPrice := fillPrice
    return &OrderUpdate{
        order:           order,
        success:         true,
        updateType:      OrderUpdateTypeFill,
        errorMessage:    nil,
        requestID:       &requestID,
        exchangeOrderID: order.ExchangeID(),
        fillQty:         &fQty,
        fillPrice:       &fPrice,
        updatedAt:       updatedAt,
        isMaker:         isMaker,
    }
}

func NewRejected(order *Order, errorMessage *string, updatedAt int64) *OrderUpdate {
    requestID := order.clientOrderID.String()
    return &OrderUpdate{
        order:           order,
        success:         false,
        updateType:      OrderUpdateTypeRejected,
        errorMessage:    errorMessage,
        requestID:       &requestID,
        exchangeOrderID: order.ExchangeID(),
        updatedAt:       updatedAt,
    }
}

func (u *OrderUpdate) WithErrorMessage(message string) *OrderUpdate {
    u.errorMessage = &message
    return u
}

func (u *OrderUpdate) WithRequestID(requestID string) *OrderUpdate {
    u.requestID = &requestID
    return u
}

func (u *OrderUpdate) GetFillQuantity() *float64 {
    return u.fillQty
}

func (u *OrderUpdate) GetFillPrice() *float64 {
    return u.fillPrice
}