package main

import (
	"compress/gzip"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/souravmenon1999/trade-engine/framed/types"

	"sync/atomic"
)

// processWithCallback reads and processes the CSV file using a callback.
func processWithCallback(filePath string, cb func(*types.Trade)) {
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	// Decompress the gzip file
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		log.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gzipReader.Close()

	reader := csv.NewReader(gzipReader)
	// Skip header (uncomment if your CSV has a header)
	// reader.Read()

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Error reading record: %v", err)
			continue
		}
		// Parse record into Trade (adjust indices based on your CSV)
		if len(record) < 4 {
			continue
		}
		t := &types.Trade{
			Symbol: record[1], // e.g., ETHUSD
		}
		if ts, err := strconv.ParseInt(record[0], 10, 64); err == nil {
			t.Timestamp.Store(ts)
		}
		if price, err := strconv.ParseFloat(record[2], 64); err == nil {
			t.Price.Store(int64(price * 1_000_000)) // Scale to avoid floats
		}
		if quantity, err := strconv.ParseFloat(record[3], 64); err == nil {
			t.Quantity.Store(int64(quantity * 1_000_000))
		}
		cb(t)
	}
}

// processWithChannels reads the CSV file and sends records to a channel for processing.
func processWithChannels(filePath string, ch chan<- *types.Trade) {
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	// Decompress the gzip file
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		log.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gzipReader.Close()

	reader := csv.NewReader(gzipReader)
	// Skip header (uncomment if your CSV has a header)
	// reader.Read()

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Error reading record: %v", err)
			continue
		}
		// Parse record into Trade (adjust indices based on your CSV)
		if len(record) < 4 {
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
		ch <- t
	}
	close(ch)
}

// processChannelData consumes Trade records from a channel.
func processChannelData(ch <-chan *types.Trade, totalPrice *atomic.Int64) {
	for t := range ch {
		// Example processing: Sum prices
		totalPrice.Add(t.Price.Load())
	}
}

// processCallbackData is the callback function for processing Trade records.
func processCallbackData(t *types.Trade, totalPrice *atomic.Int64) {
	// Example processing: Sum prices
	totalPrice.Add(t.Price.Load())
}

func main() {
	// Parse command-line flag for file path
	filePath := flag.String("file", "", "Path to your csv.gz file")
	flag.Parse()

	if *filePath == "" {
		fmt.Println("Please provide a file path using the -file flag, e.g., -file=../data/bitmex_derivative_ticker_2020-04-01_ETHUSD.csv.gz")
		os.Exit(1)
	}

	// Ensure file exists
	if _, err := os.Stat(*filePath); os.IsNotExist(err) {
		log.Fatalf("File does not exist: %s", *filePath)
	}

	// Test Channels Method
	log.Println("Testing Channels Method...")
	start := time.Now()
	runtime.GC() // Clear garbage before measuring
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	startMem := m.Alloc

	var totalPrice atomic.Int64
	ch := make(chan *types.Trade, 100) // Buffered channel
	go processWithChannels(*filePath, ch)
	processChannelData(ch, &totalPrice)

	duration := time.Since(start)
	runtime.ReadMemStats(&m)
	log.Printf("Channels Method - Time: %v, Memory Used: %v bytes", duration, m.Alloc-startMem)

	// Test Callbacks Method
	log.Println("Testing Callbacks Method...")
	start = time.Now()
	runtime.GC()
	runtime.ReadMemStats(&m)
	startMem = m.Alloc

	var totalPriceCB atomic.Int64
	processWithCallback(*filePath, func(t *types.Trade) {
		processCallbackData(t, &totalPriceCB)
	})

	duration = time.Since(start)
	runtime.ReadMemStats(&m)
	log.Printf("Callbacks Method - Time: %v, Memory Used: %v bytes", duration, m.Alloc-startMem)
}