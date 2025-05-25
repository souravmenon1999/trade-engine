package injective

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"
"strings"
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

type InjectiveClient struct {
	tradeClient   chainclient.ChainClient
	updatesClient exchangeclient.ExchangeClient
	senderAddress string
	clientCtx     client.Context
	ctx           context.Context
	cancel        context.CancelFunc
}

func NewInjectiveClient(
	networkName, lb, privKey, marketId, subaccountId string, 
	callback func([]byte),
) (*InjectiveClient, error) {
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

	// Initialize trade client with confirmation
	tradeClient, err := chainclient.NewChainClient(
		clientCtx, network, common.OptionGasPrices("0.0005inj"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create trade client: %w", err)
	}
	log.Println("✅ Trade client initialized successfully")

	// Initialize updates client with confirmation
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

	// Add final initialization log
	log.Printf("🚀 Injective client fully initialized (Sender: %s)", senderAddress.String())

	if callback != nil {
		go client.SubscribeOrderHistory(marketId, subaccountId, callback)
	}

	return client, nil
}

func (c *InjectiveClient) GetSenderAddress() string {
	return c.senderAddress
}

// SendOrder - This is the missing method that creates and sends a derivative order
func (c *InjectiveClient) SendOrder(
	marketId, subaccountId, side string,
	price, quantity, leverage decimal.Decimal,
) error {
	log.Printf("📝 Creating %s order: Price=%s, Qty=%s, Leverage=%s", 
		side, price.String(), quantity.String(), leverage.String())

	// Set up markets assistant for order creation
	marketsAssistant, err := chainclient.NewMarketsAssistant(c.ctx, c.tradeClient)
	if err != nil {
		return fmt.Errorf("failed to create markets assistant: %w", err)
	}

	// Determine order type based on side
	var orderType exchangetypes.OrderType
	switch side {
	case "buy":
		orderType = exchangetypes.OrderType_BUY
	case "sell":
		orderType = exchangetypes.OrderType_SELL
	default:
		return fmt.Errorf("invalid order side: %s (must be 'buy' or 'sell')", side)
	}

	// Set gas price with adjustment
	gasPrice := c.tradeClient.CurrentChainGasPrice()
	gasPrice = int64(float64(gasPrice) * 1.1) // 10% buffer
	c.tradeClient.SetGasPrice(gasPrice)

	// Parse subaccount ID to proper Hash format
	senderAddress, err := sdk.AccAddressFromBech32(c.senderAddress)
	if err != nil {
		return fmt.Errorf("failed to parse sender address: %w", err)
	}
	defaultSubaccountID := c.tradeClient.DefaultSubaccount(senderAddress)

	// Create the derivative order
	order := c.tradeClient.CreateDerivativeOrder(
		defaultSubaccountID,
		&chainclient.DerivativeOrderData{
			OrderType:    orderType,
			Quantity:     quantity,
			Price:        price,
			Leverage:     leverage,
			FeeRecipient: c.senderAddress,
			MarketId:     marketId,
			IsReduceOnly: false, // Set to true if you want reduce-only orders
			Cid:          uuid.NewString(),
		},
		marketsAssistant,
	)

	// Create the message
	msg := &exchangetypes.MsgCreateDerivativeLimitOrder{
		Sender: c.senderAddress,
		Order:  exchangetypes.DerivativeOrder(*order),
	}

	// Simulate the transaction first
	log.Println("🔄 Simulating transaction...")
	simRes, err := c.tradeClient.SimulateMsg(c.clientCtx, msg)
	if err != nil {
		return fmt.Errorf("simulation failed: %w", err)
	}

	// Parse simulation response
	msgCreateDerivativeLimitOrderResponse := exchangetypes.MsgCreateDerivativeLimitOrderResponse{}
	err = msgCreateDerivativeLimitOrderResponse.Unmarshal(simRes.Result.MsgResponses[0].Value)
	if err != nil {
		return fmt.Errorf("failed to unmarshal simulation response: %w", err)
	}

	log.Printf("✅ Simulation successful, Order Hash: %s", msgCreateDerivativeLimitOrderResponse.OrderHash)

	// Broadcast the transaction
	log.Println("📡 Broadcasting transaction...")
	err = c.tradeClient.QueueBroadcastMsg(msg)
	if err != nil {
		return fmt.Errorf("failed to broadcast message: %w", err)
	}

	// Wait a bit for transaction processing
	time.Sleep(time.Second * 3)

	// Get gas fee information
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
// CancelOrder cancels an existing derivative order with proper logging
// CancelOrder cancels an existing derivative order with full validation
func (c *InjectiveClient) CancelOrder(
    marketId string, 
    orderHash string,
) error {
    log.Println("📝 Starting order cancellation")
    
    // Convert hash to lowercase
    orderHash = strings.ToLower(orderHash)

    // Get subaccount ID (same method as SendOrder)
    senderAddress, err := sdk.AccAddressFromBech32(c.senderAddress)
    if err != nil {
        return fmt.Errorf("failed to parse sender address: %w", err)
    }
    subaccountId := c.tradeClient.DefaultSubaccount(senderAddress).Hex()

    // Set gas price with buffer (mirroring SendOrder)
    gasPrice := c.tradeClient.CurrentChainGasPrice()
    gasPrice = int64(float64(gasPrice) * 1.1)
    c.tradeClient.SetGasPrice(gasPrice)

    // Create cancellation message
    msg := &exchangetypes.MsgCancelDerivativeOrder{
        Sender:       c.senderAddress,
        MarketId:     marketId,
        SubaccountId: subaccountId,
        OrderHash:    orderHash,
    }

    // Simulate transaction
    log.Println("🔄 Simulating cancellation...")
    simRes, err := c.tradeClient.SimulateMsg(c.clientCtx, msg)
    if err != nil {
        log.Printf("❌ Simulation failed: %v", err)
        return fmt.Errorf("simulation failed: %w", err)
    }

    // Check simulation result (new addition)
    if simRes.GasInfo.GasUsed == 0 {
        return fmt.Errorf("invalid simulation result - gas used cannot be zero")
    }

    // Broadcast transaction
    log.Println("📡 Broadcasting cancellation...")
    err = c.tradeClient.QueueBroadcastMsg(msg)
    if err != nil {
        log.Printf("🔥 Broadcast failed: %v", err)
        return fmt.Errorf("broadcast failed: %w", err)
    }

    // Wait for processing (same as SendOrder)
    time.Sleep(time.Second * 2)

    // Get gas fee info (mirroring SendOrder)
    gasFee, err := c.tradeClient.GetGasFee()
    if err != nil {
        log.Printf("⚠️  Could not retrieve gas fee: %v", err)
    } else {
        log.Printf("⛽ Gas fee: %s INJ", gasFee)
    }

    log.Println("🎉 Cancellation successful!")
    return nil
}

func (c *InjectiveClient) SubscribeOrderHistory(
	marketId, subaccountId string, 
	callback func([]byte),
) {
	req := &derivativeExchangePB.StreamOrdersHistoryRequest{
		MarketId:     marketId,
		SubaccountId: subaccountId,
		Direction:    "buy",
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

// Close properly shuts down the client
func (c *InjectiveClient) Close() {
	if c.cancel != nil {
		c.cancel()
	}
}