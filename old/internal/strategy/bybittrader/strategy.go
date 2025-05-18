package bybittrader

import (
    "context"
    "log/slog"
    "sync"
    "time"
    "sort"

    "github.com/shopspring/decimal"
    "github.com/souravmenon1999/trade-engine/internal/config"
    "github.com/souravmenon1999/trade-engine/internal/exchange"
    "github.com/souravmenon1999/trade-engine/internal/logging"
    "github.com/souravmenon1999/trade-engine/internal/processor"
    "github.com/souravmenon1999/trade-engine/internal/types"
)

// Strategy encapsulates the Bybit trading strategy.
type Strategy struct {
    ctx               context.Context
    cancel            context.CancelFunc
    cfg               *config.Config
    dataClient        exchange.ExchangeClient
    tradingClient     exchange.ExchangeClient
    priceProcessor    *processor.PriceProcessor
    logger            *slog.Logger
    mu                sync.Mutex
    currentBuyOrderID string
    currentSellOrderID string
}

// NewStrategy creates a new instance of the trading strategy.
func NewStrategy(ctx context.Context, cfg *config.Config, dataClient, tradingClient exchange.ExchangeClient, priceProcessor *processor.PriceProcessor) *Strategy {
    clientCtx, cancel := context.WithCancel(ctx)
    return &Strategy{
        ctx:            clientCtx,
        cancel:         cancel,
        cfg:            cfg,
        dataClient:     dataClient,
        tradingClient:  tradingClient,
        priceProcessor: priceProcessor,
        logger:         logging.GetLogger().With("component", "bybit_trader_strategy"),
    }
}

// Run starts the trading strategy, subscribing to orderbook updates and managing orders.
func (s *Strategy) Run() {
    s.logger.Debug("Entering Run method")

    // Subscribe to orderbook updates
    s.logger.Debug("Subscribing to orderbook", "symbol", s.cfg.Bybit.Symbol)
    if err := s.dataClient.SubscribeOrderbook(s.ctx, s.cfg.Bybit.Symbol); err != nil {
        s.logger.Error("Failed to subscribe to orderbook", "error", err)
        return
    }
    s.logger.Debug("Orderbook subscription initiated")

    // Wait briefly for subscription to establish
    time.Sleep(2 * time.Second)

    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-s.ctx.Done():
            s.logger.Info("Strategy Run loop stopped")
            return
        case <-ticker.C:
            s.logger.Debug("Processing orderbook update")
            orderbook := s.dataClient.GetOrderbook()
            if orderbook == nil {
                s.logger.Warn("Orderbook is nil")
                continue
            }
            if orderbook.Bids == nil || orderbook.Asks == nil {
                s.logger.Warn("Orderbook data incomplete", "bids", orderbook.Bids, "asks", orderbook.Asks)
                continue
            }
            // Count bids/asks using sync.Map
            bidCount := 0
            orderbook.Bids.Range(func(_, _ interface{}) bool {
                bidCount++
                return true
            })
            askCount := 0
            orderbook.Asks.Range(func(_, _ interface{}) bool {
                askCount++
                return true
            })
            s.logger.Debug("Orderbook state", "bid_count", bidCount, "ask_count", askCount)
            s.submitAndTrackOrder(orderbook)
            s.logger.Debug("Completed submitAndTrackOrder")
        }
    }
}

// submitAndTrackOrder submits or updates orders based on orderbook data.
func (s *Strategy) submitAndTrackOrder(orderbook *types.Orderbook) {
    s.logger.Debug("Starting submitAndTrackOrder")
    s.mu.Lock()
    defer s.mu.Unlock()

    // Convert sync.Map to sorted slices for best bid/ask
    var bids, asks []*types.PriceLevel
    orderbook.Bids.Range(func(key, value interface{}) bool {
        priceLevel := value.(*types.PriceLevel)
        bids = append(bids, priceLevel)
        return true
    })
    orderbook.Asks.Range(func(key, value interface{}) bool {
        priceLevel := value.(*types.PriceLevel)
        asks = append(asks, priceLevel)
        return true
    })

    // Sort bids (descending) and asks (ascending)
    sort.Slice(bids, func(i, j int) bool {
        return bids[i].Price > bids[j].Price
    })
    sort.Slice(asks, func(i, j int) bool {
        return asks[i].Price < asks[j].Price
    })

    bestBid := uint64(0)
    bestAsk := uint64(0)
    if len(bids) > 0 {
        bestBid = bids[0].Price
    }
    if len(asks) > 0 {
        bestAsk = asks[0].Price
    }

    if bestBid == 0 || bestAsk == 0 {
        s.logger.Warn("Invalid orderbook data", "best_bid", bestBid, "best_ask", bestAsk)
        return
    }

    spreadPct := decimal.NewFromFloat(s.cfg.Order.SpreadPct)
    quantity := uint64(s.cfg.Order.Quantity * 1e6)

   buyPrice := decimal.NewFromInt(int64(bestBid)).Mul(decimal.NewFromInt(100).Sub(spreadPct)).Div(decimal.NewFromInt(100))
sellPrice := decimal.NewFromInt(int64(bestAsk)).Mul(decimal.NewFromInt(100).Add(spreadPct)).Div(decimal.NewFromInt(100))

    instrument := &types.Instrument{Symbol: s.cfg.Bybit.Symbol}

    // Prepare orders (one buy, one sell)
   ordersToPlace := []*types.Order{
    {
        Instrument: instrument,
        Side:       types.Buy,
        Price:      uint64(buyPrice.IntPart()), // Remove 1e6 multiplication
        Quantity:   quantity,
    },
    {
        Instrument: instrument,
        Side:       types.Sell,
        Price:      uint64(sellPrice.IntPart()), // Remove 1e6 multiplication
        Quantity:   quantity,
    },
}

    s.logger.Debug("Submitting orders via ReplaceQuotes", "buy_price", buyPrice, "sell_price", sellPrice, "quantity", quantity)
    newOrderIDs, err := s.tradingClient.ReplaceQuotes(s.ctx, instrument, ordersToPlace)
    if err != nil {
        s.logger.Error("Failed to replace quotes", "error", err)
        return
    }

    // Update order IDs
    if len(newOrderIDs) >= 2 {
        s.currentBuyOrderID = newOrderIDs[0]
        s.currentSellOrderID = newOrderIDs[1]
        s.logger.Info("Orders replaced", "buy_order_id", s.currentBuyOrderID, "sell_order_id", s.currentSellOrderID)
    } else {
        s.logger.Warn("Unexpected number of order IDs returned", "count", len(newOrderIDs))
    }
}

// Close shuts down the strategy.
func (s *Strategy) Close() error {
    s.logger.Info("Closing strategy")
    s.cancel()
    return s.tradingClient.Close()
}