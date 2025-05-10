package injective

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/souravmenon1999/trade-engine/internal/config"
	"github.com/souravmenon1999/trade-engine/internal/logging"
	"github.com/souravmenon1999/trade-engine/internal/types"
	"log/slog"
	"github.com/shopspring/decimal"
	"github.com/google/uuid"
	"github.com/InjectiveLabs/sdk-go/client/common"
	exchangetypes "github.com/InjectiveLabs/sdk-go/chain/exchange/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/cosmos/gogoproto/types"
	"cosmossdk.io/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/btcsuite/btcutil/bech32"
)

// Client implements the ExchangeClient interface for Injective.
type Client struct {
	cfg            *config.InjectiveConfig
	logger         *slog.Logger
	httpClient     *http.Client
	accountAddress string
	subaccountID   string
	ctx            context.Context
	cancel         context.CancelFunc
	orderbook      *types.Orderbook
	privKey        *ecdsa.PrivateKey
	chainID        string
}

// NewClient creates a new Injective client without Cosmos SDK dependencies.
func NewClient(ctx context.Context, cfg *config.InjectiveConfig) (*Client, error) {
	clientCtx, cancel := context.WithCancel(ctx)

	client := &Client{
		cfg:        cfg,
		logger:     logging.GetLogger().With("exchange", "injective"),
		httpClient: &http.Client{Timeout: 10 * time.Second},
		ctx:        clientCtx,
		cancel:     cancel,
		orderbook:  nil,
	}

	// Load Network configuration
	network := common.LoadNetwork(cfg.ChainConfig, "lb")
	client.chainID = network.ChainId

	// Derive address from private key
	privKey, err := crypto.HexToECDSA(cfg.PrivateKey)
	if err != nil {
		client.logger.Error("Failed to parse private key", "error", err)
		return nil, types.TradingError{
			Code:    types.ErrConnectionFailed,
			Message: "Failed to parse private key",
			Wrapped: err,
		}
	}
	client.privKey = privKey

	// Derive Bech32 address (inj prefix)
	pubKey := privKey.PublicKey
	addrBytes := crypto.PubkeyToAddress(pubKey).Bytes()
	converted, err := bech32.ConvertBits(addrBytes, 8, 5, true)
	if err != nil {
		client.logger.Error("Failed to convert address bytes for Bech32", "error", err)
		return nil, types.TradingError{
			Code:    types.ErrConnectionFailed,
			Message: "Failed to convert address for Bech32 encoding",
			Wrapped: err,
		}
	}
	accAddress, err := bech32.Encode("inj", converted)
	if err != nil {
		client.logger.Error("Failed to encode Bech32 address", "error", err)
		return nil, types.TradingError{
			Code:    types.ErrConnectionFailed,
			Message: "Failed to encode Bech32 address",
			Wrapped: err,
		}
	}
	client.accountAddress = accAddress
	client.logger.Info("Injective wallet initialized", "address", accAddress)

	// Derive Default Subaccount ID
	client.subaccountID = deriveSubaccountID(accAddress)
	client.logger.Info("Using default subaccount", "subaccount_id", client.subaccountID)

	client.logger.Info("Injective client initialization complete.")
	return client, nil
}

// deriveSubaccountID derives the default subaccount ID from the account address.
func deriveSubaccountID(addr string) string {
	return fmt.Sprintf("%s000000000000000000000000", addr)
}

// GetExchangeType returns the type of this exchange client.
func (c *Client) GetExchangeType() types.ExchangeType {
	return types.ExchangeInjective
}

// GetOrderbook returns nil for the Injective client in this setup.
func (c *Client) GetOrderbook() *types.Orderbook {
	return c.orderbook
}

// SubscribeOrderbook is not implemented for the Injective trading client.
func (c *Client) SubscribeOrderbook(ctx context.Context, symbol string) error {
	c.logger.Warn("SubscribeOrderbook is not implemented for the Injective client.")
	return nil
}

// translateOrderDataToSpotLimitOrderData converts a types.Order to an Injective SpotOrder.
func (c *Client) translateOrderDataToSpotLimitOrderData(
	order types.Order,
	senderAddress string,
	marketID string,
) (*exchangetypes.SpotOrder, error) {
	if order.Exchange != types.ExchangeInjective {
		return nil, fmt.Errorf("order is not for Injective")
	}
	if order.Instrument == nil {
		return nil, fmt.Errorf("order is missing instrument details")
	}

	// Simplified scaling (replace with market metadata in production)
	priceDecimal := decimal.NewFromBigInt(new(big.Int).SetUint64(order.Price), 0)
	quantityDecimal := decimal.NewFromBigInt(new(big.Int).SetUint64(order.Quantity), 0)

	// Convert to math.LegacyDec
	priceLegacyDec, err := math.LegacyNewDecFromStr(priceDecimal.String())
	if err != nil {
		return nil, fmt.Errorf("failed to convert price to LegacyDec: %w", err)
	}
	quantityLegacyDec, err := math.LegacyNewDecFromStr(quantityDecimal.String())
	if err != nil {
		return nil, fmt.Errorf("failed to convert quantity to LegacyDec: %w", err)
	}

	// Determine Injective OrderType
	injOrderType := exchangetypes.OrderType_BUY
	switch order.Side {
	case types.Buy:
		injOrderType = exchangetypes.OrderType_BUY
	case types.Sell:
		injOrderType = exchangetypes.OrderType_SELL
	default:
		return nil, fmt.Errorf("unsupported order side for Injective: %s", order.Side)
	}

	clientOrderID := uuid.New().String()

	return &exchangetypes.SpotOrder{
		MarketId: marketID,
		OrderInfo: exchangetypes.OrderInfo{
			SubaccountId: c.subaccountID,
			FeeRecipient: senderAddress,
			Price:        priceLegacyDec,
			Quantity:     quantityLegacyDec,
			Cid:          clientOrderID,
		},
		OrderType: injOrderType,
	}, nil
}

// SubmitOrder sends a single order to the Injective chain via REST.
func (c *Client) SubmitOrder(ctx context.Context, order types.Order) (string, error) {
	if order.Exchange != types.ExchangeInjective {
		return "", types.TradingError{
			Code:    types.ErrOrderSubmissionFailed,
			Message: "Order is not intended for Injective",
			Wrapped: fmt.Errorf("order target exchange: %s", order.Exchange),
		}
	}

	marketID := c.cfg.MarketID
	c.logger.Info("Submitting single order to Injective via REST...", "order", order, "market_id", marketID)

	spotOrder, err := c.translateOrderDataToSpotLimitOrderData(
		order,
		c.accountAddress,
		marketID,
	)
	if err != nil {
		c.logger.Error("Failed to translate single order data", "error", err)
		return "", types.TradingError{
			Code:    types.ErrOrderSubmissionFailed,
			Message: "Failed to translate order data",
			Wrapped: err,
		}
	}

	msg := createBatchUpdateMessage(
		c.accountAddress,
		c.subaccountID,
		nil,
		nil,
		[]*exchangetypes.SpotOrder{spotOrder},
		nil,
	)

	txHash, err := c.broadcastTxViaREST(ctx, msg)
	if err != nil {
		c.logger.Error("Failed to broadcast single order", "error", err, "order", order, "market_id", marketID)
		return "", types.TradingError{
			Code:    types.ErrOrderSubmissionFailed,
			Message: "Failed to broadcast transaction for single order",
			Wrapped: err,
		}
	}

	c.logger.Info("Single order broadcast completed", "order", order, "client_order_id", spotOrder.OrderInfo.Cid, "market_id", marketID, "txhash", txHash)
	return spotOrder.OrderInfo.Cid, nil
}

// CancelAllOrders cancels all open orders for the configured market and subaccount via REST.
func (c *Client) CancelAllOrders(ctx context.Context, instrument *types.Instrument) error {
	if instrument == nil {
		return fmt.Errorf("instrument is nil")
	}
	marketID := c.cfg.MarketID
	c.logger.Info("Attempting to cancel all orders for market via REST...", "market_id", marketID, "subaccount_id", c.subaccountID)

	msg := createBatchUpdateMessage(
		c.accountAddress,
		c.subaccountID,
		[]string{marketID},
		nil,
		nil,
		nil,
	)

	txHash, err := c.broadcastTxViaREST(ctx, msg)
	if err != nil {
		c.logger.Error("Failed to broadcast cancel all orders", "error", err, "market_id", marketID)
		return types.TradingError{
			Code:    types.ErrOrderSubmissionFailed,
			Message: "Failed to broadcast transaction for cancelling all orders",
			Wrapped: err,
		}
	}

	c.logger.Info("Cancel all orders broadcast completed", "market_id", marketID, "txhash", txHash)
	return nil
}

// ReplaceQuotes atomically cancels existing quotes and places new orders via REST.
func (c *Client) ReplaceQuotes(ctx context.Context, instrument *types.Instrument, ordersToPlace []*types.Order) ([]string, error) {
	if instrument == nil {
		return nil, fmt.Errorf("instrument is nil")
	}
	marketID := c.cfg.MarketID

	if len(ordersToPlace) == 0 {
		c.logger.Info("ReplaceQuotes called with no orders to place. Will only cancel existing.", "market_id", marketID)
	}

	c.logger.Info("Attempting to replace quotes via REST...",
		"market_id", marketID, "subaccount_id", c.subaccountID, "orders_to_place_count", len(ordersToPlace))

	spotOrdersToCreate := make([]*exchangetypes.SpotOrder, 0, len(ordersToPlace))
	newOrderCids := []string{}

	for _, order := range ordersToPlace {
		if order == nil {
			c.logger.Warn("Skipping nil order in ReplaceQuotes list")
			continue
		}
		if order.Exchange != types.ExchangeInjective {
			c.logger.Error("Skipping order not intended for Injective in ReplaceQuotes list", "order", order)
		 continue
		}

		spotOrder, err := c.translateOrderDataToSpotLimitOrderData(
			*order,
			c.accountAddress,
			marketID,
		)
		if err != nil {
			c.logger.Error("Failed to translate order data for batch update", "error", err, "order", order)
			continue
		}

		spotOrdersToCreate = append(spotOrdersToCreate, spotOrder)
		newOrderCids = append(newOrderCids, spotOrder.OrderInfo.Cid)
	}

	if len(spotOrdersToCreate) == 0 && len(ordersToPlace) > 0 {
		return nil, types.TradingError{
			Code:    types.ErrOrderSubmissionFailed,
			Message: "Failed to translate/create any valid orders for batch update",
		}
	}

	msg := createBatchUpdateMessage(
		c.accountAddress,
		c.subaccountID,
		[]string{marketID},
		nil,
		spotOrdersToCreate,
		nil,
	)

	txHash, err := c.broadcastTxViaREST(ctx, msg)
	if err != nil {
		c.logger.Error("Failed to broadcast batch update", "error", err, "market_id", marketID, "orders_count", len(spotOrdersToCreate))
		return nil, types.TradingError{
			Code:    types.ErrOrderSubmissionFailed,
			Message: "Failed to broadcast transaction for batch order update",
			Wrapped: err,
		}
	}

	c.logger.Info("Batch update (cancel all + place new) broadcast completed",
		"market_id", marketID, "orders_broadcast_count", len(spotOrdersToCreate), "new_order_cids", newOrderCids, "txhash", txHash)
	return newOrderCids, nil
}

// broadcastTxViaREST broadcasts a transaction via Injective's REST API.
func (c *Client) broadcastTxViaREST(ctx context.Context, msg *exchangetypes.MsgBatchUpdateOrders) (string, error) {
	// Hardcoded values for demo (query in production)
	sequence := uint64(0)      // Replace with actual sequence
	gasLimit := uint64(200000) // Hardcoded gas limit
	feeAmount := []*types.Coin{
		{
			Denom:  "inj",
			Amount: "500000000",
		},
	}

	// Serialize message as Protobuf Any
	msgBytes, err := proto.Marshal(msg)
	if err != nil {
		return "", fmt.Errorf("failed to marshal message: %w", err)
	}
	anyMsg := &types.Any{
		TypeUrl: "/injective.exchange.v1beta1.MsgBatchUpdateOrders",
		Value:   msgBytes,
	}

	// Create transaction body
	txBody := &types.TxBody{
		Messages: []*types.Any{anyMsg},
	}

	// Create signer info
	pubKeyBytes := crypto.FromECDSAPub(&c.privKey.PublicKey)
	signerInfo := &types.SignerInfo{
		PublicKey: &types.Any{
			TypeUrl: "/cosmos.crypto.secp256k1.PubKey",
			Value:   pubKeyBytes, // Simplified; requires proper serialization
		},
		ModeInfo: &types.ModeInfo{
			Sum: &types.ModeInfo_Single_{
				Single: &types.ModeInfo_Single{
					Mode: types.SignMode_SIGN_MODE_DIRECT,
				},
			},
		},
		Sequence: sequence,
	}

	// Create fee
	fee := &types.Fee{
		Amount:   feeAmount,
		GasLimit: gasLimit,
	}

	// Create auth info
	authInfo := &types.AuthInfo{
		SignerInfos: []*types.SignerInfo{signerInfo},
		Fee:         fee,
	}

	// Serialize transaction components
	txBodyBytes, err := proto.Marshal(txBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal tx body: %w", err)
	}
	authInfoBytes, err := proto.Marshal(authInfo)
	if err != nil {
		return "", fmt.Errorf("failed to marshal auth info: %w", err)
	}

	// Sign transaction (simplified; use Injective's sign bytes in production)
	signBytes := append(txBodyBytes, authInfoBytes...) // Simplified
	hash := crypto.Keccak256(signBytes)               // Simplified hash
	sig, err := crypto.Sign(hash, c.privKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction: %w", err)
	}

	// Create raw transaction
	txRaw := &types.TxRaw{
		BodyBytes:     txBodyBytes,
		AuthInfoBytes: authInfoBytes,
		Signatures:    [][]byte{sig},
	}

	// Serialize raw transaction
	txRawBytes, err := proto.Marshal(txRaw)
	if err != nil {
		return "", fmt.Errorf("failed to marshal tx raw: %w", err)
	}

	// Prepare REST request
	txRequest := map[string]interface{}{
		"tx_bytes": txRawBytes,
		"mode":     "BROADCAST_MODE_SYNC",
	}
	reqBytes, err := json.Marshal(txRequest)
	if err != nil {
		return "", fmt.Errorf("failed to marshal tx request: %w", err)
	}

	// Send REST request
	req, err := http.NewRequestWithContext(ctx, "POST", "https://public.api.injective.network/cosmos/tx/v1beta1/txs", bytes.NewReader(reqBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create REST request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to broadcast transaction: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("REST API returned non-200 status: %d", resp.StatusCode)
	}

	// Parse response
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	txResponse, ok := result["tx_response"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid tx_response format")
	}
	txHash, ok := txResponse["txhash"].(string)
	if !ok {
		return "", fmt.Errorf("txhash not found in response")
	}

	return txHash, nil
}

// Close cleans up the Injective client resources.
func (c *Client) Close() error {
	c.httpClient.CloseIdleConnections()
	c.cancel()
	c.logger.Info("Injective client shutting down...")
	time.Sleep(1 * time.Second)
	c.logger.Info("Injective client shut down complete.")
	return nil
}

var _ExchangeClient = (*Client)(nil)