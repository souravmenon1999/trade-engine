package grpc

import (
	"fmt"
	"sync/atomic"

	"github.com/InjectiveLabs/sdk-go/client/chain"
	chainclient "github.com/InjectiveLabs/sdk-go/client/chain"
	"github.com/InjectiveLabs/sdk-go/client/common"
	"github.com/InjectiveLabs/sdk-go/client/exchange"
	"github.com/cometbft/cometbft/rpc/client/http"
)

// InjectiveGRPCClient manages the initialization of Injective clients
type InjectiveGRPCClient struct {
	network atomic.Value // Thread-safe network config storage
}

// NewInjectiveGRPCClient creates a new instance with the specified network
func NewInjectiveGRPCClient(networkName, lb string) (*InjectiveGRPCClient, error) {
	network := common.LoadNetwork(networkName, lb)
	client := &InjectiveGRPCClient{}
	client.network.Store(network)
	return client, nil
}

// GetTradeClient initializes and returns the chain client for trading
func (c *InjectiveGRPCClient) GetTradeClient(privKey string) (*chain.ChainClient, error) {
	network, ok := c.network.Load().(common.Network)
	if !ok {
		return nil, fmt.Errorf("network not loaded")
	}

	tmClient, err := http.New(network.TmEndpoint, "/websocket")
	if err != nil {
		return nil, fmt.Errorf("failed to create Tendermint client: %w", err)
	}

	senderAddress, cosmosKeyring, err := chainclient.InitCosmosKeyring(
		"", "injective", "memory", "default", "", privKey, false,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to init cosmos keyring: %w", err)
	}

	clientCtx, err := chainclient.NewClientContext(network.ChainId, senderAddress.String(), cosmosKeyring)
	if err != nil {
		return nil, fmt.Errorf("failed to create client context: %w", err)
	}
	clientCtx = clientCtx.WithNodeURI(network.TmEndpoint).WithClient(tmClient)

	tradeClient, err := chainclient.NewChainClient(clientCtx, network, common.OptionGasPrices("0.0005inj"))
	if err != nil {
		return nil, fmt.Errorf("failed to create trade client: %w", err)
	}

	return &tradeClient, nil
}

// GetUpdatesClient initializes and returns the exchange client for updates
func (c *InjectiveGRPCClient) GetUpdatesClient() (*exchange.ExchangeClient, error) {
	network, ok := c.network.Load().(common.Network)
	if !ok {
		return nil, fmt.Errorf("network not loaded")
	}

	updatesClient, err := exchange.NewExchangeClient(network)
	if err != nil {
		return nil, fmt.Errorf("failed to create updates client: %w", err)
	}
	return &updatesClient, nil
}