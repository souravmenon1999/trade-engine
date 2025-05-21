package main

import (
	"compress/gzip"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"time"

	"github.com/souravmenon1999/trade-engine/framed/types" // Assuming this path is correct for your setup
	"sync/atomic"
)

// processWithCallback reads and processes records from the CSV file, sending them in batches via a callback.
// The batchSize parameter determines how many Trade records are sent in a single "burst".
func processWithCallback(filePath string, cb func([]*types.Trade), batchSize int) { // batchSize parameter added
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		log.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gzipReader.Close()

	reader := csv.NewReader(gzipReader)
	// Skip header (uncomment if your CSV has a header)
	// _, err = reader.Read()
	// if err != nil && err != io.EOF {
	// 	log.Printf("Error skipping header: %v", err)
	// }

	batch := make([]*types.Trade, 0, batchSize) // Initialize batch slice with dynamic capacity
	for {
		record, err := reader.Read()
		if err == io.EOF {
			// If there are any remaining trades in the batch at EOF, send them
			if len(batch) > 0 {
				cb(batch)
			}
			break // Exit loop on end of file
		}
		if err != nil {
			log.Printf("Error reading record: %v", err)
			continue
		}
		if len(record) < 4 { // Ensure enough columns for trade data
			continue
		}

		t := &types.Trade{
			Symbol: record[1],
		}
		if ts, err := strconv.ParseInt(record[0], 10, 64); err == nil {
			t.Timestamp.Store(ts)
		}
		if price, err := strconv.ParseFloat(record[2], 64); err == nil {
			t.Price.Store(int64(price * 1_000_000))
		}
		if quantity, err := strconv.ParseFloat(record[3], 64); err == nil {
			t.Quantity.Store(int64(quantity * 1_000_000))
		}

		batch = append(batch, t) // Add trade to current batch

		// If batch is full, send it and reset
		if len(batch) == batchSize {
			cb(batch)
			batch = make([]*types.Trade, 0, batchSize) // Create a new empty batch
		}
	}
}

// processWithChannels reads and processes records from the CSV file, sending them in batches to a channel.
// The batchSize parameter determines how many Trade records are sent in a single "burst".
func processWithChannels(filePath string, ch chan<- []*types.Trade, batchSize int) { // batchSize parameter added
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		log.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gzipReader.Close()

	reader := csv.NewReader(gzipReader)
	// Skip header (uncomment if your CSV has a header)
	// _, err = reader.Read()
	// if err != nil && err != io.EOF {
	// 	log.Printf("Error skipping header: %v", err)
	// }

	batch := make([]*types.Trade, 0, batchSize) // Initialize batch slice with dynamic capacity
	for {
		record, err := reader.Read()
		if err == io.EOF {
			// If there are any remaining trades in the batch at EOF, send them
			if len(batch) > 0 {
				ch <- batch
			}
			break // Exit loop on end of file
		}
		if err != nil {
			log.Printf("Error reading record: %v", err)
			continue
		}
		if len(record) < 4 { // Ensure enough columns for trade data
			continue
		}

		t := &types.Trade{
			Symbol: record[1],
		}
		if ts, err := strconv.ParseInt(record[0], 10, 64); err == nil {
			t.Timestamp.Store(ts)
		}
		if price, err := strconv.ParseFloat(record[2], 64); err == nil {
			t.Price.Store(int64(price * 1_000_000))
		}
		if quantity, err := strconv.ParseFloat(record[3], 64); err == nil {
			t.Quantity.Store(int64(quantity * 1_000_000))
		}

		batch = append(batch, t) // Add trade to current batch

		// If batch is full, send it and reset
		if len(batch) == batchSize {
			ch <- batch
			batch = make([]*types.Trade, 0, batchSize) // Create a new empty batch
		}
	}
	close(ch) // Close the channel to signal no more data
}

// processChannelData consumes Trade record batches from a channel.
func processChannelData(ch <-chan []*types.Trade, totalPrice *atomic.Int64) {
	for batch := range ch {
		for _, t := range batch { // Iterate over the trades in the batch
			totalPrice.Add(t.Price.Load())
		}
	}
}

// processCallbackData is the callback function for processing a batch of Trade records.
func processCallbackData(batch []*types.Trade, totalPrice *atomic.Int64) {
	for _, t := range batch { // Iterate over the trades in the batch
		totalPrice.Add(t.Price.Load())
	}
}

// computeStats calculates min, max, mean, median, and standard deviation for a slice of float64 values.
func computeStats(values []float64) (min, max, mean, median, std float64) {
	if len(values) == 0 {
		return 0, 0, 0, 0, 0
	}
	sort.Float64s(values) // Sort for min, max, and median
	min = values[0]
	max = values[len(values)-1]

	sum := 0.0
	for _, v := range values {
		sum += v
	}
	mean = sum / float64(len(values))

	// Median calculation
	if len(values)%2 == 0 {
		// Even number of elements, median is average of two middle elements
		median = (values[len(values)/2-1] + values[len(values)/2]) / 2
	} else {
		// Odd number of elements, median is the middle element
		median = values[len(values)/2]
	}

	// Standard deviation calculation
	var variance float64
	for _, v := range values {
		variance += (v - mean) * (v - mean)
	}
	variance /= float64(len(values)) // Population standard deviation (or len(values)-1 for sample)
	std = math.Sqrt(variance)
	return
}

func main() {
	filePath := flag.String("file", "", "Path to your csv.gz file")
	flag.Parse()

	if *filePath == "" {
		fmt.Println("Please provide a file path using the -file flag, e.g., -file=../data/bitmex_derivative_ticker_2020-04-01_ETHUSD.csv.gz")
		os.Exit(1)
	}

	if _, err := os.Stat(*filePath); os.IsNotExist(err) {
		log.Fatalf("File does not exist: %s", *filePath)
	}

	// Define the different batch sizes to test (your "burst rates")
batchSizes := []int{
    1, 2, 5, 10, 20, 50, 100, 200, 500, 1000,
    2500, 5000, 7500, 10000, 15000, 20000, 25000,
    30000, 40000, 50000, 75000, 100000, 150000,
    200000, 250000, 300000, 400000, 500000, 750000, 1000000,
}
	numRuns := 500 // Number of times to run each method for statistical averaging

	log.Printf("Starting benchmark for processing the ENTIRE file with varying BATCH_SIZE: %s", *filePath)

	// Outer loop to iterate through different batch sizes
	for _, currentBatchSize := range batchSizes {
		log.Printf("\n--- Benchmarking with BATCH_SIZE = %d ---", currentBatchSize)

		// --- Channels Method (with Batching) ---
		var channelTimes []time.Duration
		var channelMems []uint64
		for run := 0; run < numRuns; run++ {
			start := time.Now()
			runtime.GC() // Force garbage collection to get cleaner memory stats
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			startMem := m.Alloc // Memory allocated before the run

			var totalPrice atomic.Int64 // Accumulator for total price
			// Adjust channel buffer size: A small buffer (e.g., 2-5) is often sufficient for batches
			// as the batch itself provides internal buffering.
			ch := make(chan []*types.Trade, 5) // A small buffer for batches

			go processWithChannels(*filePath, ch, currentBatchSize) // Pass currentBatchSize
			processChannelData(ch, &totalPrice)                     // Start consumer in current goroutine

			duration := time.Since(start) // Time taken for this run
			runtime.ReadMemStats(&m)
			memUsed := m.Alloc - startMem // Memory used during this run

			channelTimes = append(channelTimes, duration)
			channelMems = append(channelMems, memUsed)
			// log.Printf("[Run %d] Channels (Batch %d): Time=%s, Mem=%d bytes, TotalPrice=%d", run+1, currentBatchSize, duration, memUsed, totalPrice.Load())
		}

		// Convert times to milliseconds and memory to float64 for stats
		var channelTimeFloats []float64
		for _, t := range channelTimes {
			channelTimeFloats = append(channelTimeFloats, float64(t.Milliseconds()))
		}
		var channelMemFloats []float64
		for _, mem := range channelMems {
			channelMemFloats = append(channelMemFloats, float64(mem))
		}

		// Compute and print statistics for Channels Method
		timeMin, timeMax, timeMean, timeMedian, timeStd := computeStats(channelTimeFloats)
		memMin, memMax, memMean, memMedian, memStd := computeStats(channelMemFloats)

		log.Printf("  --- Channels Method ---")
		log.Printf("    Time (ms): min=%.2f, max=%.2f, mean=%.2f, median=%.2f, std=%.2f",
			timeMin, timeMax, timeMean, timeMedian, timeStd)
		log.Printf("    Memory (bytes): min=%.0f, max=%.0f, mean=%.0f, median=%.0f, std=%.0f",
			memMin, memMax, memMean, memMedian, memStd)

		// --- Callbacks Method (with Batching) ---
		var callbackTimes []time.Duration
		var callbackMems []uint64
		for run := 0; run < numRuns; run++ {
			start := time.Now()
			runtime.GC() // Force garbage collection
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			startMem := m.Alloc

			var totalPriceCB atomic.Int64 // Accumulator for total price
			processWithCallback(*filePath, func(batch []*types.Trade) { // Callback receives a slice
				processCallbackData(batch, &totalPriceCB)
			}, currentBatchSize) // Pass currentBatchSize

			duration := time.Since(start)
			runtime.ReadMemStats(&m)
			memUsed := m.Alloc - startMem

			callbackTimes = append(callbackTimes, duration)
			callbackMems = append(callbackMems, memUsed)
			// log.Printf("[Run %d] Callbacks (Batch %d): Time=%s, Mem=%d bytes, TotalPrice=%d", run+1, currentBatchSize, duration, memUsed, totalPriceCB.Load())
		}

		// Convert times to milliseconds and memory to float64 for stats
		var callbackTimeFloats []float64
		for _, t := range callbackTimes {
			callbackTimeFloats = append(callbackTimeFloats, float64(t.Milliseconds()))
		}
		var callbackMemFloats []float64
		for _, mem := range callbackMems {
			callbackMemFloats = append(callbackMemFloats, float64(mem))
		}

		// Compute and print statistics for Callbacks Method
		timeMin, timeMax, timeMean, timeMedian, timeStd = computeStats(callbackTimeFloats)
		memMin, memMax, memMean, memMedian, memStd = computeStats(callbackMemFloats)

		log.Printf("  --- Callbacks Method ---")
		log.Printf("    Time (ms): min=%.2f, max=%.2f, mean=%.2f, median=%.2f, std=%.2f",
			timeMin, timeMax, timeMean, timeMedian, timeStd)
		log.Printf("    Memory (bytes): min=%.0f, max=%.0f, mean=%.0f, median=%.0f, std=%.0f",
			memMin, memMax, memMean, memMedian, memStd)
	}

	log.Println("\nBenchmark complete.")
}