package strategy

import (
    "fmt"
    "sync"
    "sync/atomic"
    "time"

    "github.com/google/uuid"
    "github.com/rs/zerolog/log"
    "github.com/souravmenon1999/trade-engine/framedCopy/config"
    "github.com/souravmenon1999/trade-engine/framedCopy/exchange"
    "github.com/souravmenon1999/trade-engine/framedCopy/types"
)

type ArbitrageStrategy struct {
    Exchanges        map[types.ExchangeID]exchange.Exchange
    Orders           sync.Map
    Cfg              *config.Config
    connectionsMu    sync.Mutex
    connectionsReady map[types.ExchangeID]bool
    allConnected     bool
}

func NewArbitrageStrategy(bybitEx exchange.Exchange, injectiveEx exchange.Exchange, cfg *config.Config) *ArbitrageStrategy {
    strat := &ArbitrageStrategy{
    	Exchanges: map[types.ExchangeID]exchange.Exchange{
    		types.ExchangeIDBybit:     bybitEx,
    		types.ExchangeIDInjective: injectiveEx,
    	},
    	Orders:           sync.Map{},
    	Cfg:              cfg,
    	connectionsReady: make(map[types.ExchangeID]bool),
    	allConnected:     false,
    	connectionsMu:    sync.Mutex{},
    }

    // Set handlers BEFORE WebSocket operations
    injectiveEx.SetAccountHandler(strat)
    injectiveEx.SetTradingHandler(strat)
    injectiveEx.SetExecutionHandler(strat)
    bybitEx.SetTradingHandler(strat)
    bybitEx.SetExecutionHandler(strat)
    bybitEx.SetAccountHandler(strat)

    

    // Register connection callbacks
    for exchangeID, ex := range strat.Exchanges {
        exID := exchangeID
        ex.RegisterConnectionCallback(func() {
            strat.onExchangeConnected(exID)
        })
    }

    // Connect and subscribe after handlers are set
    for _, ex := range strat.Exchanges {
        callback := func() {
            if err := ex.Connect(); err != nil {
                log.Error().Err(err).Msg("Failed to connect WebSocket")
                return
            }
            if err := ex.SubscribeAll(cfg); err != nil {
                log.Error().Err(err).Msg("Failed to subscribe to topics")
                return
            }
            ex.StartReading()
        }
        ex.SetOnReadyCallback(callback)
        callback() // Now safe to call
    }

    return strat
}

func (s *ArbitrageStrategy) onExchangeConnected(exchangeID types.ExchangeID) {
    s.connectionsMu.Lock()
    s.connectionsReady[exchangeID] = true
    allReady := true
    for exID, ex := range s.Exchanges {
        if !s.connectionsReady[exID] || !ex.IsConnected() {
            allReady = false
            break
        }
    }
    if allReady && !s.allConnected {
        s.allConnected = true
        s.connectionsMu.Unlock()
        go s.startStrategy()
    } else {
        s.connectionsMu.Unlock()
    }
    log.Info().Str("exchange", string(exchangeID)).Msg("Exchange connected")
}

func (s *ArbitrageStrategy) startStrategy() {
    log.Info().Msg("Arbitrage strategy execution begun")
    // Add your arbitrage logic here, e.g., calling Start or other operations
    s.Start()
}

func (s *ArbitrageStrategy) OnOrderUpdate(update *types.OrderUpdate) {
    log.Info().Interface("update", update).Msg("Received order update")

    if update.RequestID == nil {
        log.Error().Msg("Order update missing RequestID")
        return
    }
    clientOrderID := *update.RequestID
    orderInterface, ok := s.Orders.Load(clientOrderID)
    if !ok {
        log.Error().Str("clientOrderID", clientOrderID).Msg("Order not found in map")
        return
    }
    order := orderInterface.(*types.Order)

    order.ApplyUpdate(update)

    if order.GetStatus() == types.OrderStatusFilled && order.GetFilledQuantity() > 0 {
        reverseSide := types.SideSell
        if order.Side == types.SideBuy {
            reverseSide = types.SideBuy
        }

        reverseOrder := &types.Order{
            ClientOrderID: uuid.New(),
            Instrument:    order.Instrument,
            Side:          reverseSide,
            OrderType:     types.OrderTypeLimit,
            ExchangeID:    types.ExchangeIDBybit,
            TimeInForce:   types.TimeInForceGTC,
            Quantity:      atomic.Int64{},
            CreatedAt:     atomic.Int64{},
            UpdatedAt:     atomic.Int64{},
        }
        reverseOrder.Price.Store(order.Price.Load())
        reverseOrder.CreatedAt.Store(time.Now().UnixMilli())
        reverseOrder.UpdatedAt.Store(time.Now().UnixMilli())
        reverseOrder.Quantity.Store(int64(order.GetFilledQuantity() * types.SCALE_FACTOR_F64))

        _, err := s.SendOrder(reverseOrder)
        if err != nil {
            log.Error().Err(err).Str("clientOrderID", reverseOrder.ClientOrderID.String()).Msg("Failed to place reverse order on Bybit")
            return
        }
        log.Info().Str("clientOrderID", reverseOrder.ClientOrderID.String()).Msg("Reverse order placed on Bybit successfully")
    } else {
        log.Info().
            Str("status", fmt.Sprintf("%v", order.GetStatus())).
            Float64("filled_quantity", order.GetFilledQuantity()).
            Msg("Order update does not require action")
    }
}

func (s *ArbitrageStrategy) OnForceCancel(order *types.Order) {
    log.Info().Str("order_id", order.ClientOrderID.String()).Msg("Force cancel received")
}

func (s *ArbitrageStrategy) OnOrderDisconnect() {
    log.Warn().Msg("Order stream disconnected")
}

func (s *ArbitrageStrategy) OnOrderError(error string) {
    log.Error().Str("error", error).Msg("Order error received")
}

func (s *ArbitrageStrategy) OnOrderConnect() {
    log.Info().Msg("Order stream connected")
}

func (s *ArbitrageStrategy) OnExecutionUpdate(updates []*types.OrderUpdate) {
    for _, update := range updates {
        var clientOrderID string
        if update.RequestID != nil {
            clientOrderID = *update.RequestID
        } else {
            clientOrderID = "unknown"
        }
        log.Info().
            Str("clientOrderID", clientOrderID).
            Float64("fillQty", *update.FillQty).
            Float64("fillPrice", *update.FillPrice).
            Msg("Execution update received")
    }
}

func (s *ArbitrageStrategy) OnExecutionConnect() {
    log.Info().Msg("Execution stream connected")
}

func (s *ArbitrageStrategy) OnExecutionDisconnect() {
    log.Warn().Msg("Execution stream disconnected")
}

func (s *ArbitrageStrategy) OnExecutionError(error string) {
    log.Error().Str("error", error).Msg("Execution error received")
}

func (s *ArbitrageStrategy) OnOrderbook(orderbook *types.OrderBook) {
    log.Info().Str("exchange", string(*orderbook.Exchange)).Msg("Received orderbook update")
}

func (s *ArbitrageStrategy) OnOrderbookDisconnect() {
    log.Warn().Msg("Orderbook stream disconnected")
}

func (s *ArbitrageStrategy) OnOrderbookError(error string) {
    log.Error().Str("error", error).Msg("Orderbook error received")
}

func (s *ArbitrageStrategy) OnOrderbookConnect() {
    log.Info().Msg("Orderbook stream connected")
}

func (s *ArbitrageStrategy) SendOrder(order *types.Order) (string, error) {
    ex, ok := s.Exchanges[order.ExchangeID]
    if !ok {
        return "", fmt.Errorf("no exchange found for ID: %s", order.ExchangeID)
    }

    if order.ClientOrderID == uuid.Nil {
        order.ClientOrderID = uuid.New()
    }
    if order.CreatedAt.Load() == 0 {
        now := time.Now().UnixMilli()
        order.CreatedAt.Store(now)
        order.UpdatedAt.Store(now)
    }
    order.UpdateStatus(types.OrderStatusSubmitted)

    s.Orders.Store(order.ClientOrderID.String(), order)

    clientOrderID, err := ex.SendOrder(order)
    if err != nil {
        s.Orders.Delete(order.ClientOrderID.String())
        return "", err
    }

    log.Info().Str("clientOrderID", clientOrderID).Msg("Order sent successfully")
    return clientOrderID, nil
}

func (s *ArbitrageStrategy) OnAccountUpdate(update *types.AccountUpdate) {
    log.Info().Msgf("Account update received: %+v", update)
}

func (s *ArbitrageStrategy) OnPositionUpdate(position *types.Position) {
    log.Info().Msgf("Position update received: %+v", position)
}

func (s *ArbitrageStrategy) OnPositionDisconnect() {
    log.Warn().Msg("Position stream disconnected")
}

func (s *ArbitrageStrategy) OnPositionError(error string) {
    log.Error().Msgf("Position error: %s", error)
}

func (s *ArbitrageStrategy) OnPositionConnect() {
    log.Info().Msg("Position stream connected")
}

func (s *ArbitrageStrategy) CancelOrder(exchangeID types.ExchangeID, orderHash string) error {
    ex, ok := s.Exchanges[exchangeID]
    if !ok {
        return fmt.Errorf("exchange not found: %s", exchangeID)
    }
    return ex.CancelOrder(orderHash)
}

func (s *ArbitrageStrategy) Start() {
    log.Info().Msg("Arbitrage strategy started")

    symbol := s.Cfg.BybitOrderbook.Symbol
    log.Info().Str("symbol", symbol).Msg("Using symbol from config")

    instrument := types.NewPerpetualInstrument(
        s.Cfg.BybitOrderbook.Symbol,
        s.Cfg.BybitOrderbook.BaseCurrency,
        s.Cfg.BybitOrderbook.QuoteCurrency,
        s.Cfg.BybitOrderbook.MinLotSize,
    )
    order := types.NewLimitOrder(
        instrument,
        types.SideBuy,
        types.ExchangeIDBybit,
        2000.0,
        s.Cfg.BybitOrderbook.MinLotSize,
    )

    clientOrderID, err := s.SendOrder(order)
    if err != nil {
        log.Error().Err(err).Msg("Failed to place initial order on Injective")
        return
    }
    log.Info().Str("clientOrderID", clientOrderID).Msg("Initial order placed on Injective successfully")
}