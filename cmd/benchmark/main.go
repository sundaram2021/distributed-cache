package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type BenchmarkConfig struct {
	ServerURL   string
	Concurrency int
	Duration    time.Duration
	KeyCount    int
	ValueSize   int
	ReadRatio   float64 // 0.0 = all writes, 1.0 = all reads
}

type BenchmarkResults struct {
	TotalRequests    int64
	SuccessfulReads  int64
	SuccessfulWrites int64
	FailedRequests   int64
	AverageLatency   time.Duration
	MinLatency       time.Duration
	MaxLatency       time.Duration
	RequestsPerSec   float64
	Duration         time.Duration
}

func main() {
	var (
		serverURL   = flag.String("url", "http://localhost:8080", "Cache server URL")
		concurrency = flag.Int("c", 10, "Number of concurrent clients")
		duration    = flag.Duration("d", 30*time.Second, "Test duration")
		keyCount    = flag.Int("keys", 1000, "Number of unique keys")
		valueSize   = flag.Int("size", 100, "Value size in bytes")
		readRatio   = flag.Float64("read", 0.7, "Read ratio (0.0-1.0)")
	)
	flag.Parse()

	config := BenchmarkConfig{
		ServerURL:   *serverURL,
		Concurrency: *concurrency,
		Duration:    *duration,
		KeyCount:    *keyCount,
		ValueSize:   *valueSize,
		ReadRatio:   *readRatio,
	}

	fmt.Printf("Starting benchmark with config:\n")
	fmt.Printf("  Server: %s\n", config.ServerURL)
	fmt.Printf("  Concurrency: %d\n", config.Concurrency)
	fmt.Printf("  Duration: %s\n", config.Duration)
	fmt.Printf("  Keys: %d\n", config.KeyCount)
	fmt.Printf("  Value Size: %d bytes\n", config.ValueSize)
	fmt.Printf("  Read Ratio: %.1f%%\n", config.ReadRatio*100)
	fmt.Println()

	// Pre-populate cache with some data
	fmt.Println("Pre-populating cache...")
	populateCache(config)

	// Run benchmark
	fmt.Println("Running benchmark...")
	results := runBenchmark(config)

	// Print results
	printResults(results)

	// Get server stats
	fmt.Println("\nServer Statistics:")
	getServerStats(config.ServerURL)
}

func populateCache(config BenchmarkConfig) {
	client := &http.Client{Timeout: 5 * time.Second}
	value := generateValue(config.ValueSize)

	for i := 0; i < config.KeyCount/2; i++ {
		key := fmt.Sprintf("key_%d", i)
		setKey(client, config.ServerURL, key, value)
	}
}

func runBenchmark(config BenchmarkConfig) BenchmarkResults {
	var results BenchmarkResults
	var latencies []time.Duration
	var latencyMutex sync.Mutex

	startTime := time.Now()
	endTime := startTime.Add(config.Duration)

	var wg sync.WaitGroup
	wg.Add(config.Concurrency)

	// Start concurrent workers
	for i := 0; i < config.Concurrency; i++ {
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: 5 * time.Second}
			rand := rand.New(rand.NewSource(time.Now().UnixNano()))

			for time.Now().Before(endTime) {
				start := time.Now()

				if rand.Float64() < config.ReadRatio {
					// Perform read
					key := fmt.Sprintf("key_%d", rand.Intn(config.KeyCount))
					if getKey(client, config.ServerURL, key) {
						atomic.AddInt64(&results.SuccessfulReads, 1)
					} else {
						atomic.AddInt64(&results.FailedRequests, 1)
					}
				} else {
					// Perform write
					key := fmt.Sprintf("key_%d", rand.Intn(config.KeyCount))
					value := generateValue(config.ValueSize)
					if setKey(client, config.ServerURL, key, value) {
						atomic.AddInt64(&results.SuccessfulWrites, 1)
					} else {
						atomic.AddInt64(&results.FailedRequests, 1)
					}
				}

				latency := time.Since(start)
				latencyMutex.Lock()
				latencies = append(latencies, latency)
				latencyMutex.Unlock()

				atomic.AddInt64(&results.TotalRequests, 1)
			}
		}()
	}

	wg.Wait()
	actualDuration := time.Since(startTime)

	// Calculate latency statistics
	if len(latencies) > 0 {
		var totalLatency time.Duration
		minLatency := latencies[0]
		maxLatency := latencies[0]

		for _, lat := range latencies {
			totalLatency += lat
			if lat < minLatency {
				minLatency = lat
			}
			if lat > maxLatency {
				maxLatency = lat
			}
		}

		results.AverageLatency = totalLatency / time.Duration(len(latencies))
		results.MinLatency = minLatency
		results.MaxLatency = maxLatency
	}

	results.Duration = actualDuration
	results.RequestsPerSec = float64(results.TotalRequests) / actualDuration.Seconds()

	return results
}

func setKey(client *http.Client, serverURL, key, value string) bool {
	reqBody := map[string]interface{}{
		"key":   key,
		"value": value,
		"ttl":   3600,
	}
	jsonBody, _ := json.Marshal(reqBody)

	resp, err := client.Post(serverURL+"/set", "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

func getKey(client *http.Client, serverURL, key string) bool {
	resp, err := client.Get(fmt.Sprintf("%s/get?key=%s", serverURL, key))
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

func generateValue(size int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, size)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func printResults(results BenchmarkResults) {
	fmt.Printf("Benchmark Results:\n")
	fmt.Printf("==================\n")
	fmt.Printf("Duration:           %s\n", results.Duration)
	fmt.Printf("Total Requests:     %d\n", results.TotalRequests)
	fmt.Printf("Successful Reads:   %d\n", results.SuccessfulReads)
	fmt.Printf("Successful Writes:  %d\n", results.SuccessfulWrites)
	fmt.Printf("Failed Requests:    %d\n", results.FailedRequests)
	fmt.Printf("Requests/sec:       %.2f\n", results.RequestsPerSec)
	fmt.Printf("Average Latency:    %s\n", results.AverageLatency)
	fmt.Printf("Min Latency:        %s\n", results.MinLatency)
	fmt.Printf("Max Latency:        %s\n", results.MaxLatency)

	successRate := float64(results.SuccessfulReads+results.SuccessfulWrites) / float64(results.TotalRequests) * 100
	fmt.Printf("Success Rate:       %.2f%%\n", successRate)
}

func getServerStats(serverURL string) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(serverURL + "/metrics/json")
	if err != nil {
		log.Printf("Failed to get server stats: %v", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read response: %v", err)
		return
	}

	var stats map[string]interface{}
	if err := json.Unmarshal(body, &stats); err != nil {
		log.Printf("Failed to parse stats: %v", err)
		return
	}

	// Pretty print key metrics
	if cache, ok := stats["cache"].(map[string]interface{}); ok {
		fmt.Printf("Cache Hits:         %.0f\n", cache["Hits"])
		fmt.Printf("Cache Misses:       %.0f\n", cache["Misses"])
		fmt.Printf("Cache Items:        %.0f\n", cache["CurrentItems"])
		fmt.Printf("Cache Size:         %.0f bytes\n", cache["CurrentSize"])
	}

	if httpStats, ok := stats["http"].(map[string]interface{}); ok {
		fmt.Printf("HTTP Requests:      %.0f\n", httpStats["request_count"])
		fmt.Printf("HTTP Errors:        %.0f\n", httpStats["error_count"])
		fmt.Printf("Avg Response Time:  %.0f ms\n", httpStats["avg_response_time_ms"])
	}
}
