// cmd/main/main.go
package main

import (
	"os"
	"os/signal"
	"syscall"

	"your_module_path/config"                               // Import the config package to load settings
	vwapProcessor "your_module_path/processor/bybitorderbook/vwap" // Import the VWAP processor package
	"your_module_path/types"                                 // Import the types package for shared data structures
	bybitws "your_module_path/websocket/bybit"               // Import the Bybit websocket client package

	"github.com/rs/zerolog"      // Import zerolog for structured logging
	"github.com/rs/zerolog/log" // Import log from zerolog
)

func main() {
	// Configure zerolog for console output
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Load configuration from config.yaml
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		// Log a fatal error and exit if config loading fails
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	// Initialize Bybit websocket client
	bybitWSClient := bybitws.NewBybitOrderBookWSClient(cfg.Bybit.WSUrl)
	// Connect to the Bybit websocket server
	if err := bybitWSClient.Connect(); err != nil {
		// Log a fatal error and exit if connection fails
		log.Fatal().Err(err).Msg("Failed to connect to Bybit websocket")
	}
	// Ensure the websocket connection is closed when the main function exits.
	// This defer will also trigger the closing of the RawMessageCh inside the client.
	defer bybitWSClient.Close()

	// Create a channel to receive processed data (OrderBook + VWAP) from the processor.
	// The buffer size (10) helps handle bursts of data.
	processedDataChannel := make(chan *types.OrderBookWithVWAP, 10)

	// Initialize the Bybit VWAP processor.
	// Pass the raw message channel from the websocket client as input,
	// the processedDataChannel as its output, and the symbol.
	vwapProcessor := vwapProcessor.NewBybitVWAPProcessor(bybitWSClient.RawMessageCh, processedDataChannel, cfg.Bybit.Symbol)
	// Start the goroutine that listens for raw messages, processes them,
	// maintains the order book state, calculates VWAP, and sends the results.
	vwapProcessor.StartProcessing()

	// Subscribe to the orderbook stream on the websocket.
	// The topic format needs to match Bybit's API documentation for v5 public linear.
	orderBookTopic := "orderbook.50." + cfg.Bybit.Symbol // Example topic format, VERIFY with Bybit API docs
	if err := bybitWSClient.Subscribe(orderBookTopic); err != nil {
		// Log a fatal error and exit if subscription fails
		log.Fatal().Err(err).Msgf("Failed to subscribe to Bybit orderbook for %s", cfg.Bybit.Symbol)
	}

	// Set up signal handling to gracefully shut down the application.
	// Listen for interrupt (Ctrl+C) and termination signals.
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	// Start a goroutine to process incoming data (OrderBook + VWAP) from the processor's channel.
	// This keeps the main goroutine free to wait for the stop signal.
	go func() {
		// Loop indefinitely, receiving processed data as it becomes available
		// from the processedDataChannel.
		// The loop will automatically exit when the processedDataChannel is closed
		// by the BybitVWAPProcessor when the RawMessageCh is closed.
		for data := range processedDataChannel {
			// Extract the OrderBook and VWAP from the received struct
			ob := data.OrderBook
			vwap := data.VWAP

			// Log the calculated VWAP. This is the primary output from the processor.
			log.Info().Int64("vwap", int64(vwap)).Msg("Calculated VWAP")
			// You can also inspect the updated OrderBook state here if needed:
			// log.Debug().Uint64("sequence", ob.Sequence).Int("bids_count", len(ob.Bids)).Int("asks_count", len(ob.Asks)).Msg("Received updated OrderBook state")


			// --- Trading logic using 'vwap' and 'ob' goes here ---
			// This is where you would implement your strategy based on the VWAP
			// and potentially other data from the OrderBook (ob).
			// Example: If VWAP is calculated (not 0), simulate placing a buy order
			// if vwap > 0 {
			//     // Create an Order struct based on the processed data and config
			//     order := &types.Order{
			//         Instrument: ob.Instrument, // Use the instrument from the orderbook update
			//         Side:       "buy",         // Example: Always place a buy order
			//         // Use the quantity from config, scaled by 1,000,000
			//         Quantity:   types.Quantity(cfg.Order.Quantity * 1_000_000),
			//         Price:      vwap,          // Use the calculated VWAP as the limit price
			//         OrderType:  "limit",       // Example: Place a limit order
			//         // Add other necessary order parameters for your target exchange API
			//     }
			//     // Log the details of the simulated order
			//     log.Info().Interface("order", order).Msg("Pseudo: Attempting to place buy order")
			//     // TODO: Implement actual order placement logic to Injective or other exchange
			//     // This would involve calling methods on an Injective client instance,
			//     // handling responses, errors, etc.
			// }
			// --- End of Trading Logic Pseudo-code ---
		}
		// This log indicates that the processedDataChannel has been closed and the loop finished.
		log.Println("Main processing goroutine finished.")
	}()

	// The main goroutine waits here until a termination signal is received (e.g., Ctrl+C).
	// This keeps the application running until explicitly stopped.
	<-stopCh
	log.Info().Msg("Shutting down...")

	// The defer bybitWSClient.Close() will execute here, closing the websocket
	// connection and its associated channels, allowing other goroutines to finish.
}
