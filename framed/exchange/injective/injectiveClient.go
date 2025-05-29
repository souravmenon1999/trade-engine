package injective

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
	chainclient "github.com/InjectiveLabs/sdk-go/client/chain"
	exchangeclient "github.com/InjectiveLabs/sdk-go/client/exchange"
	"github.com/InjectiveLabs/sdk-go/client/common"
	exchangetypes "github.com/InjectiveLabs/sdk-go/chain/exchange/types"
	 derivativeExchangePB "github.com/InjectiveLabs/sdk-go/exchange/derivative_exchange_rpc/pb"
	"github.com/cometbft/cometbft/rpc/client/http"
	"github.com/shopspring/decimal"
	"github.com/google/uuid"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/client"
)

// InjectiveClient manages connections and subscriptions for Injective Protocol
type InjectiveClient struct {
	tradeClient   chainclient.ChainClient
	updatesClient exchangeclient.ExchangeClient
	senderAddress string
	clientCtx     client.Context
	ctx           context.Context
	cancel        context.CancelFunc
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

	client := &InjectiveClient{
		tradeClient:   tradeClient,
		updatesClient: updatesClient,
		senderAddress: senderAddress.String(),
		clientCtx:     clientCtx,
		ctx:           ctx,
		cancel:        cancel,
	}

	log.Printf("🚀 Injective client fully initialized (Sender: %s)", senderAddress.String())

	// Automatically start subscriptions
	 client.SubscribeAll(marketId, subaccountId)
// log.Printf("🚀 clienttttttt (Sender: %s)", client)
	return client, nil
}

//SubscribeAll subscribes to order history and funding rates if callbacks are provided
func (c *InjectiveClient) SubscribeAll(marketId, subaccountId string) {
	// Internal handler for order history
	orderHandler := func(data []byte) {
		log.Printf("Order history update: %s", string(data))
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
func (c *InjectiveClient) SubscribeOrderHistory(
	marketId, subaccountId string,
	callback func([]byte),
) {
	log.Printf("Starting order history for Market: %s, Subaccount: %s", marketId, subaccountId)
	
	req := &derivativeExchangePB.StreamOrdersHistoryRequest{
		MarketId:     marketId,      // Capital M
		SubaccountId: subaccountId,  // Capital S
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
			data, err := json.Marshal(res)
			if err != nil {
				log.Printf("Error marshaling: %v", err)
				continue
			}
			callback(data)
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
func (c *InjectiveClient) SendOrder(
	marketId, subaccountId, side string,
	price, quantity, leverage decimal.Decimal,
) error {
	log.Printf("📝 Creating %s order: Price=%s, Qty=%s, Leverage=%s",
		side, price.String(), quantity.String(), leverage.String())

	marketsAssistant, err := chainclient.NewMarketsAssistant(c.ctx, c.tradeClient)
	if err != nil {
		return fmt.Errorf("failed to create markets assistant: %w", err)
	}

	var orderType exchangetypes.OrderType
	switch side {
	case "buy":
		orderType = exchangetypes.OrderType_BUY
	case "sell":
		orderType = exchangetypes.OrderType_SELL
	default:
		return fmt.Errorf("invalid order side: %s (must be 'buy' or 'sell')", side)
	}

	gasPrice := c.tradeClient.CurrentChainGasPrice()
	gasPrice = int64(float64(gasPrice) * 1.1)
	c.tradeClient.SetGasPrice(gasPrice)

	senderAddress, err := sdk.AccAddressFromBech32(c.senderAddress)
	if err != nil {
		return fmt.Errorf("failed to parse sender address: %w", err)
	}
	defaultSubaccountID := c.tradeClient.DefaultSubaccount(senderAddress)

	order := c.tradeClient.CreateDerivativeOrder(
		defaultSubaccountID,
		&chainclient.DerivativeOrderData{
			OrderType:    orderType,
			Quantity:     quantity,
			Price:        price,
			Leverage:     leverage,
			FeeRecipient: c.senderAddress,
			MarketId:     marketId,
			IsReduceOnly: false,
			Cid:          uuid.NewString(),
		},
		marketsAssistant,
	)

	msg := &exchangetypes.MsgCreateDerivativeLimitOrder{
		Sender: c.senderAddress,
		Order:  exchangetypes.DerivativeOrder(*order),
	}

	log.Println("🔄 Simulating transaction...")
	simRes, err := c.tradeClient.SimulateMsg(c.clientCtx, msg)
	if err != nil {
		return fmt.Errorf("simulation failed: %w", err)
	}

	msgCreateDerivativeLimitOrderResponse := exchangetypes.MsgCreateDerivativeLimitOrderResponse{}
	err = msgCreateDerivativeLimitOrderResponse.Unmarshal(simRes.Result.MsgResponses[0].Value)
	if err != nil {
		return fmt.Errorf("failed to unmarshal simulation response: %w", err)
	}

	log.Printf("✅ Simulation successful, Order Hash: %s", msgCreateDerivativeLimitOrderResponse.OrderHash)

	log.Println("📡 Broadcasting transaction...")
	err = c.tradeClient.QueueBroadcastMsg(msg)
	if err != nil {
		return fmt.Errorf("failed to broadcast message: %w", err)
	}

	time.Sleep(time.Second * 3)

	gasFee, err := c.tradeClient.GetGasFee()
	if err != nil {
		log.Printf("⚠️  Could not retrieve gas fee: %v", err)
	} else {
		log.Printf("⛽ Gas fee: %s INJ", gasFee)
	}

	log.Println("🎉 Order sent successfully!")
	return nil
}


// CancelOrder cancels an existing derivative order
func (c *InjectiveClient) CancelOrder(
	marketId string,
	orderHash string,
) error {
	log.Println("📝 Starting order cancellation")

	orderHash = strings.ToLower(orderHash)

	senderAddress, err := sdk.AccAddressFromBech32(c.senderAddress)
	if err != nil {
		return fmt.Errorf("failed to parse sender address: %w", err)
	}
	subaccountId := c.tradeClient.DefaultSubaccount(senderAddress).Hex()

	gasPrice := c.tradeClient.CurrentChainGasPrice()
	gasPrice = int64(float64(gasPrice) * 1.1)
	c.tradeClient.SetGasPrice(gasPrice)

	msg := &exchangetypes.MsgCancelDerivativeOrder{
		Sender:       c.senderAddress,
		MarketId:     marketId,
		SubaccountId: subaccountId,
		OrderHash:    orderHash,
	}

	log.Println("🔄 Simulating cancellation...")
	simRes, err := c.tradeClient.SimulateMsg(c.clientCtx, msg)
	if err != nil {
		log.Printf("❌ Simulation failed: %v", err)
		return fmt.Errorf("simulation failed: %w", err)
	}

	if simRes.GasInfo.GasUsed == 0 {
		return fmt.Errorf("invalid simulation result - gas used cannot be zero")
	}

	log.Println("📡 Broadcasting cancellation...")
	err = c.tradeClient.QueueBroadcastMsg(msg)
	if err != nil {
		log.Printf("🔥 Broadcast failed: %v", err)
		return fmt.Errorf("broadcast failed: %w", err)
	}

	time.Sleep(time.Second * 2)

	gasFee, err := c.tradeClient.GetGasFee()
	if err != nil {
		log.Printf("⚠️  Could not retrieve gas fee: %v", err)
	} else {
		log.Printf("⛽ Gas fee: %s INJ", gasFee)
	}

	log.Println("🎉 Cancellation successful!")
	return nil
}

// SubscribeOrderHistory subscribes to order history updates and calls the callback with order data


// Close properly shuts down the client
func (c *InjectiveClient) Close() {
	if c.cancel != nil {
		c.cancel()
	}
}