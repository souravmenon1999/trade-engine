package injective

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	chainclient "github.com/InjectiveLabs/sdk-go/client/chain"
	exchangeclient "github.com/InjectiveLabs/sdk-go/client/exchange"
	"github.com/InjectiveLabs/sdk-go/client/common"
	derivativeExchangePB "github.com/InjectiveLabs/sdk-go/exchange/derivative_exchange_rpc/pb"
	"github.com/cometbft/cometbft/rpc/client/http"
)

type InjectiveClient struct {
	tradeClient   chainclient.ChainClient // Changed to value type
	updatesClient exchangeclient.ExchangeClient // Changed to value type (interface)
	senderAddress string
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
    return "" // Empty string since we just need to use the client
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

