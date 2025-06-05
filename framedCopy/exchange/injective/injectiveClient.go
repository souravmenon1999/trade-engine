package injective

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"
	exchangetypes "github.com/InjectiveLabs/sdk-go/chain/exchange/types"
	chainclient "github.com/InjectiveLabs/sdk-go/client/chain"
	"github.com/InjectiveLabs/sdk-go/client/common"
	exchangeclient "github.com/InjectiveLabs/sdk-go/client/exchange"
	derivativeExchangePB "github.com/InjectiveLabs/sdk-go/exchange/derivative_exchange_rpc/pb"
	"github.com/cometbft/cometbft/rpc/client/http"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/souravmenon1999/trade-engine/framedCopy/exchange/net/websockets/injective"
	"github.com/souravmenon1999/trade-engine/framedCopy/types"
	"github.com/souravmenon1999/trade-engine/framedCopy/exchange"

)


// TradingHandler defines the interface for handling trading events


// InjectiveClient manages connections and subscriptions for Injective Protocol
type InjectiveClient struct {
	tradeClient   chainclient.ChainClient
	updatesClient exchangeclient.ExchangeClient
	senderAddress string
	wsClient      *injectivews.InjectiveWSClient
	clientCtx     client.Context
	ctx           context.Context
	cancel        context.CancelFunc
	marketId      string
    subaccountId  string
	latestGasPrice      atomic.Int64
	tradingHandler exchange.TradingHandler
	executionHandler exchange.ExecutionHandler
}

// NewInjectiveClient creates and initializes a new Injective client with callbacks for order history and funding rates
func NewInjectiveClient(
	networkName, lb, privKey, marketId, subaccountId string,
) (*InjectiveClient, error) {
	log.Printf("started")
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
	log.Printf("reached before chainclient")
	clientCtx, err := chainclient.NewClientContext(
		network.ChainId, senderAddress.String(), cosmosKeyring,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create client context: %w", err)
	}
	clientCtx = clientCtx.WithNodeURI(network.TmEndpoint).WithClient(tmClient)
	log.Printf("reached before tradeclient")
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

	ctx, cancel := context.WithCancel(context.Background())

	wsClient := injectivews.NewInjectiveWSClient("wss://sentry.tm.injective.network:443/websocket")
	if err := wsClient.Connect(); err != nil {
		log.Printf("Failed to connect to WebSocket: %v", err)
	}

	client := &InjectiveClient{
		tradeClient:      tradeClient,
		updatesClient:    updatesClient,
		wsClient:         wsClient,
		senderAddress:    senderAddress.String(),
		clientCtx:        clientCtx,
		ctx:              ctx,
		cancel:           cancel,
		marketId:         marketId,
		subaccountId:     subaccountId,
		latestGasPrice:   atomic.Int64{},
		tradingHandler:   nil,
		executionHandler: nil,
	}

	if client.wsClient != nil {
		err = client.wsClient.SubscribeTxs(client.handleGasPriceUpdate)
		if err != nil {
			log.Printf("Failed to subscribe to gas prices: %v", err)
		}
	}

	log.Printf("🚀 Injective client fully initialized (Sender: %s)", senderAddress.String())

	client.SubscribeAll(marketId, subaccountId)
	return client, nil
}

func (c *InjectiveClient) handleGasPriceUpdate(gasPrice int64) {
    c.latestGasPrice.Store(gasPrice)
    //  zerolog.Info().Int64("gas_price", gasPrice).Msg("Updated gas price")
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

// SubscribeAll subscribes to order history and funding rates
func (c *InjectiveClient) SubscribeAll(marketId, subaccountId string) {
	go c.subscribeOrderHistoryWithRetry(marketId, subaccountId)
	if err := c.SubscribeFundingRates(marketId, func(data []byte) {
		log.Printf("Funding rate update: %s", string(data))
	}); err != nil {
		log.Printf("Failed to subscribe to funding rates: %v", err)
	}
}

// subscribeOrderHistoryWithRetry handles the subscription with retry logic
func (c *InjectiveClient) subscribeOrderHistoryWithRetry(marketId, subaccountId string) {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			if err := c.SubscribeOrderHistory(marketId, subaccountId); err != nil {
				if c.tradingHandler != nil {
					c.tradingHandler.OnOrderError(fmt.Sprintf("Failed to subscribe to order history: %v", err))
				}
				if c.executionHandler != nil {
					c.executionHandler.OnExecutionError(fmt.Sprintf("Failed to subscribe to order history: %v", err))
				}
				time.Sleep(5 * time.Second)
				continue
			}
			return
		}
	}
}

// SubscribeOrderHistory subscribes to order history updates and processes both trading and execution events
func (c *InjectiveClient) SubscribeOrderHistory(marketId, subaccountId string) error {
	log.Printf("Starting order history for Market: %s, Subaccount: %s", marketId, subaccountId)

	req := &derivativeExchangePB.StreamOrdersHistoryRequest{
		MarketId:     marketId,
		SubaccountId: subaccountId,
	}

	stream, err := c.updatesClient.StreamHistoricalDerivativeOrders(c.ctx, req)
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
		case <-c.ctx.Done():
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
				if c.tradingHandler != nil {
					c.tradingHandler.OnOrderError(fmt.Sprintf("Error receiving order: %v", err))
					c.tradingHandler.OnOrderDisconnect()
				}
				if c.executionHandler != nil {
					c.executionHandler.OnExecutionError(fmt.Sprintf("Error receiving order: %v", err))
					c.executionHandler.OnExecutionDisconnect()
				}
				return err
			}
			log.Printf("order raw: %v", res)
			go func(raw *derivativeExchangePB.StreamOrdersHistoryResponse) {
				update, err := parseOrderUpdate(raw)
				if err != nil {
					if c.tradingHandler != nil {
						c.tradingHandler.OnOrderError(fmt.Sprintf("Error parsing order update: %v", err))
					}
					if c.executionHandler != nil {
						c.executionHandler.OnExecutionError(fmt.Sprintf("Error parsing order update: %v", err))
					}
					return
				}
				if c.tradingHandler != nil {
					c.tradingHandler.OnOrderUpdate(update)
				}
				if update.UpdateType == types.OrderUpdateTypeFill && c.executionHandler != nil {
					c.executionHandler.OnExecutionUpdate([]*types.OrderUpdate{update})
				}
			}(res)
		}
	}
}
 
// SubscribeFundingRates subscribes to funding rate updates
func (c *InjectiveClient) SubscribeFundingRates(marketId string, callback func([]byte)) error {
	stream, err := c.updatesClient.StreamDerivativeMarket(c.ctx, []string{marketId})
	if err != nil {
		return fmt.Errorf("failed to subscribe to derivative market stream: %w", err)
	}

	go func() {
		for {
			select {
			case <-c.ctx.Done():
				return
			default:
				res, err := stream.Recv()
				if err != nil {
					log.Printf("Error receiving market update: %v", err)
					return
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

// GetSenderAddress returns the sender's address
func (c *InjectiveClient) GetSenderAddress() string {
	return c.senderAddress
}




// SendOrder creates and sends a derivative order
func (c *InjectiveClient) SendOrder(order *types.Order) (string, error) {
	clientOrderID := order.ClientOrderID.String()
	go func() {
		price := decimal.NewFromFloat(order.GetPrice())
		quantity := decimal.NewFromFloat(order.GetQuantity())
		leverage := decimal.NewFromInt(1)

		marketsAssistant, err := chainclient.NewMarketsAssistant(c.ctx, c.tradeClient)
		if err != nil {
			log.Printf("SendOrder failed (markets assistant): %v", err)
			return
		}

		orderType := exchangetypes.OrderType_BUY
		if order.Side == "sell" {
			orderType = exchangetypes.OrderType_SELL
		}

		// gasPrice := c.latestGasPrice.Load()
        // if gasPrice == 0 {
        //     gasPrice = c.tradeClient.CurrentChainGasPrice()
        // }
      
        // badPrice := c.tradeClient.CurrentChainGasPrice()
		gasPrice := c.tradeClient.CurrentChainGasPrice()
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

// CancelOrder cancels an existing derivative order
func (c *InjectiveClient) CancelOrder(orderHash string) error {
	if orderHash == "" {
		return fmt.Errorf("orderHash cannot be empty")
	}

	go func(hash string) {
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

// Close properly shuts down the client
func (c *InjectiveClient) Close() {
	if c.cancel != nil {
		c.cancel()
	}
}

func parseOrderUpdate(raw *derivativeExchangePB.StreamOrdersHistoryResponse) (*types.OrderUpdate, error) {
	order := raw.Order
	if order == nil {
		return nil, fmt.Errorf("nil order in response")
	}

	clientOrderID := order.Cid
	if clientOrderID == "" {
		return nil, fmt.Errorf("missing client order ID")
	}

	minimalOrder := &types.Order{
		ClientOrderID: uuid.MustParse(clientOrderID),
		Instrument: &types.Instrument{
			Symbol: order.MarketId,
		},
		Side: types.Side(strings.ToLower(order.Direction)),
	}

	var updateType types.OrderUpdateType
	var success bool
	switch order.State {
	case "booked":
		updateType = types.OrderUpdateTypeCreated
		success = true
	case "partial_filled":
		updateType = types.OrderUpdateTypeFill
		success = true
	case "filled":
		updateType = types.OrderUpdateTypeFill
		success = true
	case "canceled":
		updateType = types.OrderUpdateTypeCanceled
		success = true
	default:
		updateType = types.OrderUpdateTypeCreated
		success = true
	}

	priceDec, err := decimal.NewFromString(order.Price)
	if err != nil {
		return nil, fmt.Errorf("invalid price: %v", err)
	}
	filledQuantityDec, err := decimal.NewFromString(order.FilledQuantity)
	if err != nil {
		return nil, fmt.Errorf("invalid filled quantity: %v", err)
	}

	fillQty, _ := filledQuantityDec.Float64()
	fillPrice, _ := priceDec.Float64()
	orderHash := order.OrderHash
	requestID := clientOrderID

	update := types.NewOrderUpdate(
		minimalOrder,
		updateType,
		success,
		raw.Timestamp,
	)
	update.ExchangeOrderID = &orderHash
	update.FillQty = &fillQty
	update.FillPrice = &fillPrice
	update.RequestID = &requestID

	return update, nil
}