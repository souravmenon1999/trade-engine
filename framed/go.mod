module github.com/souravmenon1999/trade-engine

go 1.22

require (
	github.com/InjectiveLabs/sdk-go v1.15.0
	github.com/cometbft/cometbft v0.38.11
	github.com/gorilla/websocket v1.5.3
	github.com/spf13/viper v1.20.0
	github.com/rs/zerolog v1.33.0
	github.com/gogo/protobuf v1.3.2
)

require (
	cosmossdk.io/store v1.1.0 // indirect
	github.com/CosmWasm/wasmd v0.40.2 // indirect
)

replace (
	cosmossdk.io/store => github.com/InjectiveLabs/cosmos-sdk/store v0.50.6-inj-0
	github.com/CosmWasm/wasmd => github.com/InjectiveLabs/wasmd v0.53.2-inj.2
	github.com/cometbft/cometbft => github.com/InjectiveLabs/cometbft v0.38.17-inj-0
	github.com/cosmos/cosmos-sdk => github.com/InjectiveLabs/cosmos-sdk v0.50.9-inj-4
	github.com/gogo/protobuf => github.com/gogo/protobuf v1.3.2
)