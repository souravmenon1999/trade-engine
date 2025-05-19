package injective

import (
	"github.com/InjectiveLabs/sdk-go/client"
	"github.com/InjectiveLabs/sdk-go/client/chain"
	"github.com/InjectiveLabs/sdk-go/client/common"
	"github.com/InjectiveLabs/sdk-go/client/exchange"
	chainclient "github.com/InjectiveLabs/sdk-go/client/chain"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
)

// InjectiveTradeClient handles trading operations on Injective (Helix).
type InjectiveTradeClient struct {
	chainClient    chain.ChainClient
	exchangeClient exchange.ExchangeClient
	tmClient       *rpchttp.HTTP
}

// NewInjectiveTradeClient initializes a trading client using a private key.
func NewInjectiveTradeClient(networkName, lb, privKey string) (*InjectiveTradeClient, error) {
	network := common.LoadNetwork(networkName, lb)
	tmClient, err := rpchttp.New(network.TmEndpoint, "/websocket")
	if err != nil {
		return nil, err
	}

	// Use private key only, no keyring
	senderAddress, cosmosKeyring, err := chainclient.InitCosmosKeyring(
		"",        // keyringHome
		"",        // backend
		"",        // keyringName
		"",        // password
		privKey,   // private key
		"inj",     // account prefix
		false,     // useLedger
	)
	if err != nil {
		return nil, err
	}

	clientCtx, err := chainclient.NewClientContext(
		network.ChainId,
		senderAddress.String(),
		cosmosKeyring,
	)
	if err != nil {
		return nil, err
	}
	clientCtx = clientCtx.WithNodeURI(network.TmEndpoint).WithClient(tmClient)

	chainClient, err := chainclient.NewChainClient(
		clientCtx,
		network,
		common.OptionGasPrices(client.DefaultGasPriceWithDenom),
	)
	if err != nil {
		return nil, err
	}

	exchangeClient, err := exchange.NewExchangeClient(network)
	if err != nil {
		return nil, err
	}

	return &InjectiveTradeClient{
		chainClient:    chainClient,
		exchangeClient: exchangeClient,
		tmClient:       tmClient,
	}, nil
}