package injective

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"
	"strconv"
	exchangetypes "github.com/InjectiveLabs/sdk-go/chain/exchange/types"
	chainclient "github.com/InjectiveLabs/sdk-go/client/chain"
	"github.com/InjectiveLabs/sdk-go/client/common"
	exchangeclient "github.com/InjectiveLabs/sdk-go/client/exchange"
	derivativeExchangePB "github.com/InjectiveLabs/sdk-go/exchange/derivative_exchange_rpc/pb"
	"github.com/cometbft/cometbft/rpc/client/http"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"
	"github.com/souravmenon1999/trade-engine/framedCopy/exchange"
	"github.com/souravmenon1999/trade-engine/framedCopy/new" // Use common WebSocket client
	"github.com/souravmenon1999/trade-engine/framedCopy/types"
	logs "github.com/rs/zerolog/log"

)

// InjectiveClient manages connections and subscriptions for Injective Protocol
type InjectiveClient struct {
	tradeClient      chainclient.ChainClient
	updatesClient    exchangeclient.ExchangeClient
	senderAddress    string
	wsClient         *baseWS.BaseWSClient
	clientCtx        client.Context
	marketId         string
	subaccountId     string
	latestGasPrice   atomic.Int64
	tradingHandler   exchange.TradingHandler
	executionHandler exchange.ExecutionHandler
	orderFillTracker map[string]float64
	orderbookHandler exchange.OrderbookHandler
	accountHandler   exchange.AccountHandler
	orderCancel      context.CancelFunc
	fundingCancel    context.CancelFunc
	accountCancel    context.CancelFunc
	bookCancel       context.CancelFunc
}

// NewInjectiveClient creates and initializes a new Injective client
func NewInjectiveClient(
	networkName, lb, privKey, marketId, subaccountId string,
) (*InjectiveClient, error) {
	log.Printf("Starting InjectiveClient initialization")
	network := common.LoadNetwork(networkName, lb)

	tmClient, err := http.New(network.TmEndpoint, "/websocket")
	if err != nil {
		return nil, fmt.Errorf("failed to create Tendermint client: %w", err)
	}

	senderAddress, cosmosKeyring, err := chainclient.InitCosmosKeyring(
		"", "injective", "memory", "default", "", privKey, false,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to init keyring: %w", err)
	}

	clientCtx, err := chainclient.NewClientContext(
		network.ChainId, senderAddress.String(), cosmosKeyring,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create client context: %w", err)
	}
	clientCtx = clientCtx.WithNodeURI(network.TmEndpoint).WithClient(tmClient)

	tradeClient, err := chainclient.NewChainClient(
		clientCtx, network, common.OptionGasPrices("0.0005inj"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create trade client: %w", err)
	}
	log.Println("✅ Trade client initialized successfully")

	updatesClient, err := exchangeclient.NewExchangeClient(network)
	if err != nil {
		return nil, fmt.Errorf("failed to create updates client: %w", err)
	}
	log.Println("📊 Orderbook updates client ready")

	// Use BaseWSClient instead of InjectiveWSClient
	// wsClient := baseWS.NewBaseWSClient("wss://sentry.tm.injective.network:443/websocket", "", "") // No API key/secret for public stream

	client := &InjectiveClient{
		tradeClient:      tradeClient,
		updatesClient:    updatesClient,
		wsClient:          baseWS.NewBaseWSClient("wss://sentry.tm.injective.network:443/websocket", "", ""),		senderAddress:    senderAddress.String(),
		clientCtx:        clientCtx,
		marketId:         marketId,
		subaccountId:     subaccountId,
		latestGasPrice:   atomic.Int64{},
		orderFillTracker: make(map[string]float64),
	}

	// Register gas price handler
gasHandler := NewGasPriceHandler(func(gasPrice int64) {
        client.latestGasPrice.Store(gasPrice)
        logs.Info().Int64("gasPrice", gasPrice).Msg("Updated gas price")
    })
    client.wsClient.SetDefaultHandler(gasHandler)

    // Connect to the WebSocket
    if err := client.wsClient.Connect(); err != nil {
        log.Printf("Failed to connect to WebSocket: %v", err)
        return nil, err
    }

	log.Printf("🚀 Injective client fully initialized (Sender: %s)", senderAddress.String())

	client.SubscribeAll(marketId, subaccountId)
	return client, nil
}

// handleGasPriceUpdate updates the latest gas price
func (c *InjectiveClient) handleGasPriceUpdate(gasPrice int64) {
	if gasPrice <= 0 {
		log.Printf("Invalid gas price received: %d, skipping update", gasPrice)
		return
	}
	c.latestGasPrice.Store(gasPrice)
}

// SetTradingHandler sets the trading handler
func (c *InjectiveClient) SetTradingHandler(handler exchange.TradingHandler) {
	c.tradingHandler = handler
	log.Printf("Set trading handler for Injective")
}

// SetExecutionHandler sets the execution handler
func (c *InjectiveClient) SetExecutionHandler(handler exchange.ExecutionHandler) {
	c.executionHandler = handler
	log.Printf("Set execution handler for Injective")
}

func (c *InjectiveClient) SetAccountHandler(handler exchange.AccountHandler) {
	c.accountHandler = handler
	log.Printf("Set account handler for Injective")
}

// SubscribeAll subscribes to order history, funding rates, account updates, and orderbook
func (c *InjectiveClient) SubscribeAll(marketId, subaccountId string) {
	// Order history subscription
	orderCtx, orderCancel := context.WithCancel(context.Background())
	go c.subscribeOrderHistoryWithRetry(marketId, subaccountId, orderCtx)
	c.orderCancel = orderCancel

	// Funding rates subscription
	fundingCtx, fundingCancel := context.WithCancel(context.Background())
	if err := c.SubscribeFundingRates(marketId, func(data []byte) {
		log.Printf("Funding rate update: %s", string(data))
	}, fundingCtx); err != nil {
		log.Printf("Failed to subscribe to funding rates: %v", err)
	}
	c.fundingCancel = fundingCancel

	// Account updates subscription
	accountCtx, accountCancel := context.WithCancel(context.Background())
	go func() {
		if err := c.SubscribeAccountUpdates(subaccountId, accountCtx); err != nil {
			log.Printf("Failed to subscribe to account updates: %v", err)
		}
	}()
	c.accountCancel = accountCancel

	// Orderbook subscription
	bookCtx, bookCancel := context.WithCancel(context.Background())
	go func() {
		if err := c.SubscribeOrderbook(marketId, bookCtx); err != nil {
			log.Printf("Failed to subscribe to orderbook: %v", err)
		}
	}()
	c.bookCancel = bookCancel

	// Subscribe to transaction events via WebSocket
	subMsg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "subscribe",
		"id":      1,
		"params": map[string]interface{}{
			"query": "tm.event='Tx'",
		},
	}
	if err := c.wsClient.Subscribe(subMsg); err != nil {
		log.Printf("Failed to subscribe to transaction events: %v", err)
	}
	c.wsClient.Start()
}

func (c *InjectiveClient) SubscribeAccountUpdates(subaccountId string, ctx context.Context) error {
	stream, err := c.updatesClient.StreamSubaccountBalance(ctx, subaccountId)
	if err != nil {
		return fmt.Errorf("failed to stream account updates: %w", err)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				if c.accountHandler != nil {
					c.accountHandler.OnPositionDisconnect()
				}
				return
			default:
				res, err := stream.Recv()
				if err != nil {
					log.Printf("Account updates stream error: %v", err)
					time.Sleep(5 * time.Second)
					continue
				}
				accountUpdate := parseAccountUpdate(res)
				if accountUpdate != nil && c.accountHandler != nil {
					c.accountHandler.OnAccountUpdate(accountUpdate)
				}
			}
		}
	}()

	if c.accountHandler != nil {
		c.accountHandler.OnPositionConnect()
	}
	return nil
}

func parseAccountUpdate(res interface{}) *types.AccountUpdate {
	data, err := json.Marshal(res)
	if err != nil {
		log.Printf("Error marshaling response to JSON: %v", err)
		return nil
	}

	log.Printf("Received subaccount balance update: %s", string(data))

	type CustomBalanceResponse struct {
		Balance struct {
			SubaccountID   string `json:"subaccount_id"`
			AccountAddress string `json:"account_address"`
			Denom          string `json:"denom"`
			Deposit        struct {
				TotalBalance     string `json:"total_balance"`
				AvailableBalance string `json:"available_balance"`
			} `json:"deposit"`
		} `json:"balance"`
		Timestamp int64 `json:"timestamp"`
	}

	var customRes CustomBalanceResponse
	if err := json.Unmarshal(data, &customRes); err != nil {
		log.Printf("Error unmarshaling JSON: %v", err)
		return nil
	}

	totalBalance, err := decimal.NewFromString(customRes.Balance.Deposit.TotalBalance)
	if err != nil {
		log.Printf("Error parsing total balance: %v", err)
		return nil
	}

	var availableBalance decimal.Decimal
	if customRes.Balance.Deposit.AvailableBalance == "" {
		log.Printf("Warning: Available balance is missing, defaulting to 0")
		availableBalance = decimal.Zero
	} else {
		availableBalance, err = decimal.NewFromString(customRes.Balance.Deposit.AvailableBalance)
		if err != nil {
			log.Printf("Error parsing available balance: %v", err)
			return nil
		}
	}

	totalFloat, _ := totalBalance.Float64()
	availableFloat, _ := availableBalance.Float64()
	exchangeID := types.ExchangeIDInjective

	log.Printf("Parsed subaccount balance update: Exchange=%s, TotalBalance=%.2f, AvailableBalance=%.2f", exchangeID, totalFloat, availableFloat)
	return &types.AccountUpdate{
		Exchange:  &exchangeID,
		AccountIM: totalFloat,
		AccountMM: availableFloat,
	}
}

func (c *InjectiveClient) SubscribeOrderbook(marketId string, ctx context.Context) error {
	stream, err := c.updatesClient.StreamDerivativeOrderbookUpdate(ctx, []string{marketId})
	if err != nil {
		return fmt.Errorf("failed to stream orderbook updates: %w", err)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				if c.orderbookHandler != nil {
					c.orderbookHandler.OnOrderbookDisconnect()
				}
				return
			default:
				res, err := stream.Recv()
				if err != nil {
					log.Printf("Orderbook stream error: %v", err)
					time.Sleep(5 * time.Second)
					continue
				}
				orderbook, err := parseInjectiveOrderbook(res, marketId)
				if err != nil {
					if c.orderbookHandler != nil {
						c.orderbookHandler.OnOrderbookError(fmt.Sprintf("Failed to parse orderbook: %v", err))
					}
					continue
				}
				if c.orderbookHandler != nil {
					c.orderbookHandler.OnOrderbook(orderbook)
				}
			}
		}
	}()

	if c.orderbookHandler != nil {
		c.orderbookHandler.OnOrderbookConnect()
	}
	return nil
}

func parseInjectiveOrderbook(res *derivativeExchangePB.StreamOrderbookUpdateResponse, marketId string) (*types.OrderBook, error) {
	if res == nil || res.OrderbookLevelUpdates == nil {
		return nil, fmt.Errorf("nil response or orderbook updates")
	}

	instrument := &types.Instrument{Symbol: marketId}
	exchange := types.ExchangeIDInjective
	orderbook := types.NewOrderBook(instrument, &exchange)

	for _, b := range res.OrderbookLevelUpdates.Buys {
		price, err := strconv.ParseFloat(b.Price, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid buy price: %v", err)
		}
		quantity, err := strconv.ParseFloat(b.Quantity, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid buy quantity: %v", err)
		}
		if b.IsActive {
			update := types.BidUpdate(price, quantity, 1)
			orderbook.ApplyUpdate(update)
		} else {
			update := types.BidUpdate(price, 0, 0)
			orderbook.ApplyUpdate(update)
		}
	}

	for _, a := range res.OrderbookLevelUpdates.Sells {
		price, err := strconv.ParseFloat(a.Price, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid sell price: %v", err)
		}
		quantity, err := strconv.ParseFloat(a.Quantity, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid sell quantity: %v", err)
		}
		if a.IsActive {
			update := types.AskUpdate(price, quantity, 1)
			orderbook.ApplyUpdate(update)
		} else {
			update := types.AskUpdate(price, 0, 0)
			orderbook.ApplyUpdate(update)
		}
	}

	orderbook.SetLastUpdateTime(res.Timestamp)
	orderbook.SetSequence(int64(res.OrderbookLevelUpdates.Sequence))
	return orderbook, nil
}

func (c *InjectiveClient) subscribeOrderHistoryWithRetry(marketId, subaccountId string, ctx context.Context) {
	attempts := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if err := c.SubscribeOrderHistory(marketId, subaccountId, ctx); err != nil {
				if c.tradingHandler != nil {
					c.tradingHandler.OnOrderError(fmt.Sprintf("Failed to subscribe to order history: %v", err))
				}
				if c.executionHandler != nil {
					c.executionHandler.OnExecutionError(fmt.Sprintf("Failed to subscribe to order history: %v", err))
				}
				attempts++
				delay := time.Duration(5*attempts) * time.Second
				if delay > 30*time.Second {
					delay = 30 * time.Second
				}
				log.Printf("Retrying order history subscription after %v due to error: %v", delay, err)
				time.Sleep(delay)
				continue
			}
			return
		}
	}
}

func (c *InjectiveClient) SubscribeOrderHistory(marketId, subaccountId string, ctx context.Context) error {
	log.Printf("Starting order history for Market: %s, Subaccount: %s", marketId, subaccountId)

	req := &derivativeExchangePB.StreamOrdersHistoryRequest{
		MarketId:     marketId,
		SubaccountId: subaccountId,
	}

	stream, err := c.updatesClient.StreamHistoricalDerivativeOrders(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to stream orders: %w", err)
	}

	if c.tradingHandler != nil {
		c.tradingHandler.OnOrderConnect()
	}
	if c.executionHandler != nil {
		c.executionHandler.OnExecutionConnect()
	}

	for {
		select {
		case <-ctx.Done():
			if c.tradingHandler != nil {
				c.tradingHandler.OnOrderDisconnect()
			}
			if c.executionHandler != nil {
				c.executionHandler.OnExecutionDisconnect()
			}
			return nil
		default:
			res, err := stream.Recv()
			if err != nil {
				log.Printf("Order history stream error: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}
			log.Printf("RAW %v", res)
			if res.Order != nil {
				c.handleOrderUpdate(res.Order, res.Timestamp, res.OperationType)
			}
		}
	}
}

func (c *InjectiveClient) handleOrderUpdate(raw *derivativeExchangePB.DerivativeOrderHistory, timestamp int64, operationType string) {
	orderUpdate := c.parseOrderUpdate(raw, timestamp, operationType)
	if orderUpdate == nil {
		return
	}
	  // Fetch the original order using RequestID (clientOrderID)
    if orderUpdate.RequestID != nil {
        originalOrder, ok := exchange.GlobalOrderStore.GetOrder(*orderUpdate.RequestID)
        if ok {
            orderUpdate.Order = originalOrder
        } else {
            logs.Warn().Str("clientOrderID", *orderUpdate.RequestID).Msg("No order found for update")
        }
    }
	if c.tradingHandler != nil {
		c.tradingHandler.OnOrderUpdate(orderUpdate)
	}
	if c.executionHandler != nil && orderUpdate.UpdateType == types.OrderUpdateTypeFill {
		c.handleExecutionUpdate(orderUpdate)
	}
}

func (c *InjectiveClient) parseOrderUpdate(raw *derivativeExchangePB.DerivativeOrderHistory, timestamp int64, operationType string) *types.OrderUpdate {
	var updateType types.OrderUpdateType
	switch operationType {
	case "insert":
		updateType = types.OrderUpdateTypeCreated
	case "update":
		if raw.State == "partial_filled" || raw.State == "filled" {
			updateType = types.OrderUpdateTypeFill
		} else if raw.State == "canceled" {
			updateType = types.OrderUpdateTypeCanceled
		} else {
			updateType = types.OrderUpdateTypeOther
		}
	case "replace":
		updateType = types.OrderUpdateTypeAmended
	case "invalidate":
		updateType = types.OrderUpdateTypeRejected
	default:
		updateType = types.OrderUpdateTypeOther
	}

	var status types.OrderStatus
	switch raw.State {
	case "booked":
		status = types.OrderStatusOpen
	case "partial_filled":
		status = types.OrderStatusPartiallyFilled
	case "filled":
		status = types.OrderStatusFilled
	case "canceled":
		status = types.OrderStatusCancelled
	default:
		status = types.OrderStatusUnknown
	}

	filledQuantityDec, err := decimal.NewFromString(raw.FilledQuantity)
	if err != nil {
		log.Printf("Invalid filled quantity: %v", err)
		return nil
	}
	filledQuantity, _ := filledQuantityDec.Float64()

	priceDec, err := decimal.NewFromString(raw.Price)
	if err != nil {
		log.Printf("Invalid price: %v", err)
		return nil
	}
	price, _ := priceDec.Float64()

	update := &types.OrderUpdate{
		Success:         true,
		UpdateType:      updateType,
		Status:          status,
		ErrorMessage:    nil,
		RequestID:       &raw.Cid,
		ExchangeOrderID: &raw.OrderHash,
		FillQty:         &filledQuantity,
		FillPrice:       &price,
		UpdatedAt:       raw.UpdatedAt,
		IsMaker:         false,
		AmendType:       "",
		NewPrice:        nil,
		NewQty:          nil,
	}

	return update
}

func (c *InjectiveClient) handleExecutionUpdate(orderUpdate *types.OrderUpdate) {
	if orderUpdate.FillQty != nil && *orderUpdate.FillQty > 0 {
		c.orderFillTracker[*orderUpdate.ExchangeOrderID] = *orderUpdate.FillQty
		c.executionHandler.OnExecutionUpdate([]*types.OrderUpdate{orderUpdate})
	}
}

func (c *InjectiveClient) SubscribeFundingRates(marketId string, callback func([]byte), ctx context.Context) error {
	stream, err := c.updatesClient.StreamDerivativeMarket(ctx, []string{marketId})
	if err != nil {
		return fmt.Errorf("failed to subscribe to derivative market stream: %w", err)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				res, err := stream.Recv()
				if err != nil {
					log.Printf("Funding rates stream error: %v", err)
					time.Sleep(5 * time.Second)
					continue
				}
				fundingData := struct {
					PerpetualMarketInfo    interface{} `json:"perpetual_market_info"`
					PerpetualMarketFunding interface{} `json:"perpetual_market_funding"`
				}{
					PerpetualMarketInfo:    res.Market.PerpetualMarketInfo,
					PerpetualMarketFunding: res.Market.PerpetualMarketFunding,
				}
				data, err := json.Marshal(fundingData)
				if err != nil {
					log.Printf("Error marshaling funding data: %v", err)
					continue
				}
				callback(data)
			}
		}
	}()

	log.Printf("Subscribed to funding rate updates for market: %s", marketId)
	return nil
}

func (c *InjectiveClient) SendOrder(order *types.Order) (string, error) {
	clientOrderID := order.ClientOrderID.String()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		price := decimal.NewFromFloat(order.GetPrice())
		quantity := decimal.NewFromFloat(order.GetQuantity())
		leverage := decimal.NewFromInt(1)

		marketsAssistant, err := chainclient.NewMarketsAssistant(ctx, c.tradeClient)
		if err != nil {
			log.Printf("SendOrder failed (markets assistant): %v", err)
			return
		}

		orderType := exchangetypes.OrderType_BUY
		if order.Side == "sell" {
			orderType = exchangetypes.OrderType_SELL
		}

		gasPrice := c.latestGasPrice.Load()
		if gasPrice == 0 {
			gasPrice = c.tradeClient.CurrentChainGasPrice()
		}
		c.tradeClient.SetGasPrice(gasPrice)

		senderAddress, err := sdk.AccAddressFromBech32(c.senderAddress)
		if err != nil {
			log.Printf("SendOrder failed (address parsing): %v", err)
			return
		}
		defaultSubaccountID := c.tradeClient.DefaultSubaccount(senderAddress)

		orderData := c.tradeClient.CreateDerivativeOrder(
			defaultSubaccountID,
			&chainclient.DerivativeOrderData{
				OrderType:    orderType,
				Quantity:     quantity,
				Price:        price,
				Leverage:     leverage,
				FeeRecipient: c.senderAddress,
				MarketId:     c.marketId,
				IsReduceOnly: false,
				Cid:          clientOrderID,
			},
			marketsAssistant,
		)

		msg := &exchangetypes.MsgCreateDerivativeLimitOrder{
			Sender: c.senderAddress,
			Order:  exchangetypes.DerivativeOrder(*orderData),
		}

		if err := c.tradeClient.QueueBroadcastMsg(msg); err != nil {
			log.Printf("SendOrder failed (broadcast): %v", err)
			return
		}

		log.Printf("Order sent successfully, Order Hash: ")
	}()

	return clientOrderID, nil
}

func (c *InjectiveClient) CancelOrder(orderHash string) error {
	if orderHash == "" {
		return fmt.Errorf("orderHash cannot be empty")
	}

	go func(hash string) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		log.Println(ctx)
		log.Println("📝 Starting ASYNC order cancellation")

		hash = strings.ToLower(hash)

		senderAddress, err := sdk.AccAddressFromBech32(c.senderAddress)
		if err != nil {
			log.Printf("❌ Failed to parse sender address: %v", err)
			return
		}
		subaccountId := c.tradeClient.DefaultSubaccount(senderAddress).Hex()

		gasPrice := c.latestGasPrice.Load()
		if gasPrice == 0 {
			gasPrice = c.tradeClient.CurrentChainGasPrice()
		}
		c.tradeClient.SetGasPrice(gasPrice)

		msg := &exchangetypes.MsgCancelDerivativeOrder{
			Sender:       c.senderAddress,
			MarketId:     c.marketId,
			SubaccountId: subaccountId,
			OrderHash:    hash,
		}

		log.Println("🔄 Simulating cancellation...")
		simRes, err := c.tradeClient.SimulateMsg(c.clientCtx, msg)
		if err != nil {
			log.Printf("❌ Simulation failed: %v", err)
			return
		}

		if simRes.GasInfo.GasUsed == 0 {
			log.Println("❌ Invalid simulation result - gas used cannot be zero")
			return
		}

		log.Println("📡 Broadcasting cancellation...")
		if err := c.tradeClient.QueueBroadcastMsg(msg); err != nil {
			log.Printf("🔥 Broadcast failed: %v", err)
			return
		}

		gasFee, err := c.tradeClient.GetGasFee()
		if err != nil {
			log.Printf("⚠️ Could not retrieve gas fee: %v", err)
		} else {
			log.Printf("⛽ Gas fee: %s INJ", gasFee)
		}

		log.Println("🎉 Cancellation successful!")
	}(orderHash)

	return nil
}

func (c *InjectiveClient) Close() {
	if c.orderCancel != nil {
		c.orderCancel()
	}
	if c.fundingCancel != nil {
		c.fundingCancel()
	}
	if c.accountCancel != nil {
		c.accountCancel()
	}
	if c.bookCancel != nil {
		c.bookCancel()
	}
	if c.wsClient != nil {
		c.wsClient.Close()
	}
	log.Println("🛑 Injective client shut down at 08:00AM INF")
}