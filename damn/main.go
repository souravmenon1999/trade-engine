package main

import (
    "context"
    "fmt"
   	chainclient "github.com/InjectiveLabs/sdk-go/client/chain"

)

func main() {
    // Set up the public network
    publicNetwork := chainclient.Network{
        ChainGrpcEndpoint: "grpc.injective.network:9900",
        ChainTLSCert:      nil, // No TLS cert needed for public endpoint
    }

    // Create a client for the public endpoint
    publicClient, err := chainclient.NewChainClient(publicNetwork)
    if err != nil {
        panic(err)
    }

    // Get the chain ID
    chainID := publicClient.GetNetwork().ChainId
    fmt.Println("Public Chain ID:", chainID)

    // Get the latest block height
    latestBlock, err := publicClient.FetchLatestBlock(context.Background())
    if err != nil {
        panic(err)
    }
    fmt.Println("Public Block Height:", latestBlock.Block.Header.Height)
}