// internal/exchange/injective/messages.go - Using Injective SDK types (Corrected)
package injective

import (
	cosmostypes "github.com/cosmos/cosmos-sdk/types" // For AccAddress, etc.
	// --- Corrected Injective Trading Type Imports ---
	// Use the exchange/types path for trading messages and enums
	exchangetypes "github.com/InjectiveLabs/sdk-go/chain/exchange/types" // For MsgBatchUpdateOrders, SpotOrder, DerivativeOrder, OrderType enum
)

// Note on Scaling and Decimals:
// createBatchUpdateMessage builds the MsgBatchUpdateOrders message.

func createBatchUpdateMessage(
    senderAddress string,
    subaccountID string,
    spotMarketIDsToCancelAll []string,
    derivativeMarketIDsToCancelAll []string,
    spotOrdersToCreate []*exchangetypes.SpotOrder,
    derivativeOrdersToCreate []*exchangetypes.DerivativeOrder,
) cosmostypes.Msg {
    if len(spotMarketIDsToCancelAll) > 0 || len(derivativeMarketIDsToCancelAll) > 0 ||
       len(spotOrdersToCreate) > 0 || len(derivativeOrdersToCreate) > 0 {
        if subaccountID == "" {
            panic("subaccountID is required when canceling all orders or creating orders")
        }
    }

    msg := &exchangetypes.MsgBatchUpdateOrders{
        Sender:                         senderAddress,
        SubaccountId:                   subaccountID,
        SpotMarketIdsToCancelAll:       spotMarketIDsToCancelAll,
        DerivativeMarketIdsToCancelAll: derivativeMarketIDsToCancelAll,
        SpotOrdersToCreate:             spotOrdersToCreate,
        DerivativeOrdersToCreate:       derivativeOrdersToCreate,
    }

    return msg
}
