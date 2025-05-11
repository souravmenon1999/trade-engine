// internal/exchange/injective/messages.go - Using Injective SDK types (Corrected)
package injective

import (
	

	cosmostypes "github.com/cosmos/cosmos-sdk/types" // For AccAddress, etc.
	// --- Corrected Injective Trading Type Imports ---
	// Use the exchange/types path for trading messages and enums
	exchangetypes "github.com/InjectiveLabs/sdk-go/chain/exchange/types" // For MsgBatchUpdateOrders, SpotOrder, DerivativeOrder, OrderType enum

)

// Note on Scaling and Decimals:
// ... (Scaling comment remains the same - this needs real implementation) ...

// translateOrderDataToSpotLimitOrderData converts a types.Order to a chainclient.SpotOrderData.
// This is an intermediate step before creating the SDK SpotOrder message using chainClient.CreateSpotOrder.
// This function contains the core scaling logic (currently simplified placeholder).
// Returns *chainclient.SpotOrderData as expected by chainClient.CreateSpotOrder.
// Note: Moved this translation function into client.go as it depends on the client's knowledge
// (like marketsAssistant for real scaling). It's better as a method on the Client struct.
// This messages.go file will now primarily hold the createBatchUpdateMessage function
// and potentially other message-related helpers that don't rely on client state.


// createBatchUpdateMessage builds the MsgBatchUpdateOrders message.
// It takes the sender's address, subaccount ID, market IDs to cancel all,
// and lists of orders to create.
// This returns a cosmostypes.Msg which is the expected input for QueueBroadcastMsg.
func createBatchUpdateMessage(
	senderAddress string,
	subaccountID string,
	spotMarketIDsToCancelAll []string,
	derivativeMarketIDsToCancelAll []string,
	spotOrdersToCreate []*exchangetypes.SpotOrder,
	derivativeOrdersToCreate []*exchangetypes.DerivativeOrder,
) cosmostypes.Msg {
	msg := &exchangetypes.MsgBatchUpdateOrders{
		Sender:                        senderAddress,
		SubaccountId:                  subaccountID,
		SpotMarketIdsToCancelAll:      spotMarketIDsToCancelAll,
		DerivativeMarketIdsToCancelAll: derivativeMarketIDsToCancelAll, // Corrected variable name
		SpotOrdersToCreate:            spotOrdersToCreate,
		DerivativeOrdersToCreate:      derivativeOrdersToCreate,
	}

	return msg
}