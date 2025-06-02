package injective

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync/atomic"

	exchangetypes "github.com/InjectiveLabs/sdk-go/chain/exchange/types"
	chainclient "github.com/InjectiveLabs/sdk-go/client/chain"
	"github.com/InjectiveLabs/sdk-go/client/common"
	exchangeclient "github.com/InjectiveLabs/sdk-go/client/exchange"
	derivativeExchangePB "github.com/InjectiveLabs/sdk-go/exchange/derivative_exchange_rpc/pb"
	"github.com/cometbft/cometbft/rpc/client/http"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/google/uuid"
	zerolog "github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"
	"github.com/souravmenon1999/trade-engine/framed/exchange/net/websockets/injective"
	"github.com/souravmenon1999/trade-engine/framed/types"
)

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
	orderUpdateCallback func(*types.OrderUpdate)
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
log.Printf("reached beofre chainclient")
	clientCtx, err := chainclient.NewClientContext(
		network.ChainId, senderAddress.String(), cosmosKeyring,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create client context: %w", err)
	}
	clientCtx = clientCtx.WithNodeURI(network.TmEndpoint).WithClient(tmClient)
log.Printf("reached beof tradeclient")
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

	// Initialize WebSocket client
        wsClient := injectivews.NewInjectiveWSClient("wss://sentry.tm.injective.network:443/websocket")
        if err := wsClient.Connect(); err != nil {
            log.Printf("Failed to connect to WebSocket: %v", err)
        }

       

	client := &InjectiveClient{
		tradeClient:   tradeClient,
		updatesClient: updatesClient,
		wsClient:            wsClient,
		senderAddress: senderAddress.String(),
		clientCtx:     clientCtx,
		ctx:           ctx,
		cancel:        cancel,
		marketId:      marketId,     // Add this
    	subaccountId:  subaccountId,
		orderUpdateCallback: nil,

	}

     if client.wsClient != nil {
        err = client.wsClient.SubscribeTxs(client.handleGasPriceUpdate)
        if err != nil {
            log.Printf("Failed to subscribe to gas prices: %v", err)
        }
    }

	

    log.Printf("🚀 Injective client fully initialized (Sender: %s)", senderAddress.String())

	log.Printf("🚀 Injective client fully initialized (Sender: %s)", senderAddress.String())

	// Automatically start subscriptions
	 client.SubscribeAll(marketId, subaccountId)
// log.Printf("🚀 clienttttttt (Sender: %s)", client)
	return client, nil
}

func (c *InjectiveClient) handleGasPriceUpdate(gasPrice int64) {
    c.latestGasPrice.Store(gasPrice)
    zerolog.Info().Int64("gas_price", gasPrice).Msg("Updated gas price")
}

// RegisterOrderUpdateCallback sets the callback for order updates
func (c *InjectiveClient) RegisterOrderUpdateCallback(callback func(*types.OrderUpdate)) {
    c.orderUpdateCallback = callback
    log.Printf("Registered order update callback")
}

//SubscribeAll subscribes to order history and funding rates if callbacks are provided
func (c *InjectiveClient) SubscribeAll(marketId, subaccountId string) {
	// Internal handler for order history
	// Internal handler for order history
    orderHandler := func(update *types.OrderUpdate) {
        if c.orderUpdateCallback != nil {
            c.orderUpdateCallback(update)
        } else {
            log.Printf("Order history update received but no callback registered: %+v", update)
        }
    }

	// Internal handler for funding rates
	fundingHandler := func(data []byte) {
		log.Printf("Funding rate update: %s", string(data))
	}

	// Start subscriptions
	
	go c.SubscribeOrderHistory(marketId, subaccountId, orderHandler)
	if err := c.SubscribeFundingRates(marketId, fundingHandler); err != nil {
		log.Printf("Failed to subscribe to funding rates: %v", err)
	}
}

// SubscribeFundingRates subscribes to funding rate updates for a given market ID and calls the callback with funding data
// SubscribeOrderHistory subscribes to order history updates
func (c *InjectiveClient) SubscribeOrderHistory(
	marketId, subaccountId string,
	callback func(*types.OrderUpdate),
) {
	log.Printf("Starting order history for Market: %s, Subaccount: %s", marketId, subaccountId)

	req := &derivativeExchangePB.StreamOrdersHistoryRequest{
		MarketId:     marketId,
		SubaccountId: subaccountId,
	}

	stream, err := c.updatesClient.StreamHistoricalDerivativeOrders(c.ctx, req)
	if err != nil {
		log.Printf("Failed to stream orders: %v", err)
		return
	}

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			res, err := stream.Recv()
			if err != nil {
				log.Printf("Error receiving order: %v", err)
				return
			}
			log.Printf("order raw: %v", res)
			go func(raw *derivativeExchangePB.StreamOrdersHistoryResponse) {
					
				update, err := parseOrderUpdate(raw)
				if err != nil {
					log.Printf("Error parsing order update: %v", err)
					return
				}
				callback(update)
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
    // Immediately launch goroutine without any result tracking
     func() {
        price := decimal.NewFromInt(order.Price.Load())
        quantity := decimal.NewFromFloat(order.Quantity.ToFloat64())
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

	    gasPrice := c.latestGasPrice.Load()
        if gasPrice == 0 {
            gasPrice = c.tradeClient.CurrentChainGasPrice()
        }
        c.tradeClient.SetGasPrice(gasPrice)
        zerolog.Info().Int64("gas", gasPrice).Msg("123send")

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
                Cid:          uuid.NewString(),
            },
            marketsAssistant,
        )

        msg := &exchangetypes.MsgCreateDerivativeLimitOrder{
            Sender: c.senderAddress,
            Order:  exchangetypes.DerivativeOrder(*orderData),
        }

        // simRes, err := c.tradeClient.SimulateMsg(c.clientCtx, msg)
        // if err != nil {
        //     log.Printf("SendOrder failed (simulation): %v", err)
        //     return
        // }

        // var response exchangetypes.MsgCreateDerivativeLimitOrderResponse
        // if err := response.Unmarshal(simRes.Result.MsgResponses[0].Value); err != nil {
        //     log.Printf("SendOrder failed (unmarshal): %v", err)
        //     return
        // }

        if err := c.tradeClient.QueueBroadcastMsg(msg); err != nil {
            log.Printf("SendOrder failed (broadcast): %v", err)
            return
        }

        log.Printf("Order sent successfully, Order Hash: ")
    }()

    // Immediate return for fire-and-forget
    return "", nil
}

// CancelOrder cancels an existing derivative order
func (c *InjectiveClient) CancelOrder(orderHash string) error {
    // Validate input immediately
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
        zerolog.Info().Int64("gas", gasPrice).Msg("123send")
        if gasPrice == 0 {
            gasPrice = c.tradeClient.CurrentChainGasPrice()
        }
        gasPrice = int64(float64(gasPrice))
        
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
    }(orderHash) // Pass orderHash to avoid data race

    return nil // Return nil for now, as the operation is async
}


// SubscribeOrderHistory subscribes to order history updates and calls the callback with order data


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

    // Map Injective state to OrderStatus
    var status types.OrderStatus
    switch order.State {
    case "booked":
        status = types.OrderStatusOpen
    case "partial_filled":
        status = types.OrderStatusPartiallyFilled
    case "filled":
        status = types.OrderStatusFilled
    case "canceled":
        status = types.OrderStatusCancelled
    default:
        status = types.OrderStatusSubmitted
    }

    // Parse decimal values
    priceDec, err := decimal.NewFromString(order.Price)
    if err != nil {
        return nil, fmt.Errorf("invalid price: %v", err)
    }
    quantityDec, err := decimal.NewFromString(order.Quantity)
    if err != nil {
        return nil, fmt.Errorf("invalid quantity: %v", err)
    }
    filledQuantityDec, err := decimal.NewFromString(order.FilledQuantity)
    if err != nil {
        return nil, fmt.Errorf("invalid filled quantity: %v", err)
    }

    // Convert decimals to float64
    quantityFloat, _ := quantityDec.Float64()
    filledQtyFloat, _ := filledQuantityDec.Float64()

    // Create the OrderUpdate struct
    update := &types.OrderUpdate{
        Order: &types.Order{
            ExchangeID: types.ExchangeIDInjective,
            Instrument: &types.Instrument{
                Symbol: order.MarketId,
            },
            Price:    types.NewPrice(priceDec.Mul(decimal.NewFromInt(types.SCALE_FACTOR)).IntPart()),
            Quantity: types.NewQuantity(quantityFloat),
            Side:     strings.ToLower(order.Direction),
        },
        Status:         status,
        FilledQuantity: types.NewQuantity(filledQtyFloat),
    }

    return update, nil
}