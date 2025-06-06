package types

import(
	"sync"
	"sync/atomic"
	"hash/fnv"

)

const (
	NUM_SHARDS   = 32
)


type Price struct {
	atomic.Int64
}

// Quantity is a struct embedding atomic.Int64 for thread-safe quantity values.
type Quantity struct {
	atomic.Int64
}

// NewPrice creates a new Price with an initial value and returns a pointer.
func NewPrice(val int64) *Price {
	p := &Price{}
	p.Store(val)
	return p
}

// NewQuantity creates a new Quantity with an initial value and returns a pointer.
func NewQuantity(val float64) *Quantity {
	q := &Quantity{}
	q.Store(int64(val * SCALE_FACTOR))
	return q
}

func (q *Quantity) ToFloat64() float64 {
	return float64(q.Load()) / SCALE_FACTOR
}


func scale(value float64) int64 { return int64(value * SCALE_FACTOR_F64) }
func unscale(value int64) float64 { return float64(value) / SCALE_FACTOR_F64 }




type PriceLevel struct {
	Price      int64  
	Quantity   int64  
	OrderCount uint32 
}

func NewPriceLevel(price, quantity int64, orderCount uint32) *PriceLevel {
	return &PriceLevel{Price: price, Quantity: quantity, OrderCount: orderCount}
}

// GetPrice returns the price of the level as an unscaled float64.
func (pl *PriceLevel) GetPrice() float64 { return float64(pl.Price) / SCALE_FACTOR_F64 }

// GetQuantity returns the total quantity at the level as an unscaled float64.
func (pl *PriceLevel) GetQuantity() float64 { return float64(pl.Quantity) / SCALE_FACTOR_F64 }

// GetOrderCount returns the number of orders at the level.
func (pl *PriceLevel) GetOrderCount() uint32 { return pl.OrderCount }

// OrderBookUpdate represents a single update event for a price level in an order book.
type OrderBookUpdate struct {
	Price      int64  
	Quantity   int64  
	Side       Side   
	OrderCount uint32 
	
}

func NewOrderBookUpdate(price, quantity float64, side Side, orderCount uint32) *OrderBookUpdate {
	return &OrderBookUpdate{Price: scale(price), Quantity: scale(quantity), Side: side, OrderCount: orderCount}
}

// IsRemoval checks if this update signifies the removal of a price level.
func (obu *OrderBookUpdate) IsRemoval() bool { return obu.Quantity == 0 }

// BidUpdate is a convenience function to create a Buy-side OrderBookUpdate.
func BidUpdate(price, quantity float64, orderCount uint32) *OrderBookUpdate { return NewOrderBookUpdate(price, quantity, SideBuy, orderCount) }

// AskUpdate is a convenience function to create a Sell-side OrderBookUpdate.
func AskUpdate(price, quantity float64, orderCount uint32) *OrderBookUpdate { return NewOrderBookUpdate(price, quantity, SideSell, orderCount) }

// Shard holds a segment of the order book, protecting its price levels with a mutex.
type Shard struct {
	levels map[int64]*PriceLevel 
	mu     sync.RWMutex         
}

func getShardIndex(price int64) uint32 {
	h := fnv.New32a()
	var b [8]byte
	for i := 0; i < 8; i++ { b[i] = byte(price >> (8 * i)) }
	h.Write(b[:])
	return h.Sum32() % NUM_SHARDS
}


type OrderBook struct {
    Instrument     *Instrument
    AsksShards     []*Shard 
    BidsShards     []*Shard 
    LastUpdateTime atomic.Int64
    Sequence       atomic.Int64
    Exchange       *ExchangeID
}

func NewOrderBook(instrument *Instrument, exchange *ExchangeID) *OrderBook {
	ob := &OrderBook{
		Instrument: instrument,
		Exchange:   exchange,
		AsksShards: make([]*Shard, NUM_SHARDS), 
		BidsShards: make([]*Shard, NUM_SHARDS), 
	}
	for i := 0; i < NUM_SHARDS; i++ {
		ob.AsksShards[i] = &Shard{levels: make(map[int64]*PriceLevel)}
		ob.BidsShards[i] = &Shard{levels: make(map[int64]*PriceLevel)}
	}
	return ob
}

// GetSymbol returns the instrument symbol.
func (ob *OrderBook) GetSymbol() string { return ob.Instrument.Symbol }

// SetLastUpdateTime sets last update time atomically.
func (ob *OrderBook) SetLastUpdateTime(timestamp int64) { ob.LastUpdateTime.Store(timestamp) }

// GetLastUpdateTime gets last update time.
func (ob *OrderBook) GetLastUpdateTime() (int64, bool) {
	time := ob.LastUpdateTime.Load()
	return time, time != UNSET_VALUE
}

// SetSequence sets sequence number atomically.
func (ob *OrderBook) SetSequence(seq int64) { ob.Sequence.Store(seq) }

// GetSequence gets sequence number.
func (ob *OrderBook) GetSequence() (int64, bool) {
	seq := ob.Sequence.Load()
	return seq, seq != UNSET_VALUE
}

// ApplyUpdate applies a single order book update with sharding.
func (ob *OrderBook) ApplyUpdate(update *OrderBookUpdate) {
	var targetShards []*Shard
	if update.Side == SideBuy { 
		targetShards = ob.BidsShards
	} else { 
		targetShards = ob.AsksShards
	}

	shardIndex := getShardIndex(update.Price)
	shard := targetShards[shardIndex]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if update.IsRemoval() {
		delete(shard.levels, update.Price)
	} else {
		if pl, ok := shard.levels[update.Price]; ok {
			pl.Quantity = update.Quantity
			pl.OrderCount = update.OrderCount
		} else {
			shard.levels[update.Price] = NewPriceLevel(update.Price, update.Quantity, update.OrderCount)
		}
	}
}

// ApplyUpdates applies multiple order book updates sequentially.
func (ob *OrderBook) ApplyUpdates(updates []*OrderBookUpdate) {
	for _, update := range updates {
		ob.ApplyUpdate(update)
	}
}
