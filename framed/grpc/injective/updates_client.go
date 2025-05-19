package injective

import (
	"github.com/cometbft/cometbft/rpc/client/http"
)

// InjectiveUpdatesClient handles event subscriptions on Injective.
type InjectiveUpdatesClient struct {
	tmClient *http.HTTP
}

// NewInjectiveUpdatesClient initializes an updates client for event subscriptions.
func NewInjectiveUpdatesClient(tmEndpoint string) (*InjectiveUpdatesClient, error) {
	tmClient, err := http.New(tmEndpoint, "/websocket")
	if err != nil {
		return nil, err
	}
	return &InjectiveUpdatesClient{
		tmClient: tmClient,
	}, nil
}