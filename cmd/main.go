package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	src "github.com/sundaram2021/distributed-cache/src"
)

var port string
var peers string
var configFile string
var nodeID string

func main() {
	flag.StringVar(&port, "port", ":8080", "HTTP server port")
	flag.StringVar(&peers, "peers", "", "Comma-separated list of peer addresses")
	flag.StringVar(&configFile, "config", "", "Configuration file path")
	flag.StringVar(&nodeID, "node-id", "", "Unique node identifier")
	flag.Parse()

	// Load configuration
	config, err := src.LoadConfig(configFile)
	if err != nil {
		log.Printf("Failed to load config: %v, using defaults", err)
		config = src.DefaultConfig()
	}

	// Override config with command line flags
	if port != ":8080" {
		config.Server.Port = port
	}
	if nodeID != "" {
		config.Cluster.NodeID = nodeID
	}

	// Parse peers
	var peerList []string
	if peers != "" {
		peerList = strings.Split(peers, ",")
	} else {
		peerList = config.Cluster.Peers
	}

	// Add self to peer list
	peerList = append(peerList, "self")

	// Create cache server
	cs := src.NewCacheServer(peerList)

	// Setup HTTP routes
	http.HandleFunc("/set", cs.SecureSetHandler)
	http.HandleFunc("/get", cs.SecureGetHandler)
	http.HandleFunc("/health", cs.HealthHandler)
	http.HandleFunc("/stats", cs.StatsHandler)
	http.HandleFunc("/metrics", cs.MetricsHandler)
	http.HandleFunc("/metrics/json", cs.DetailedMetricsHandler)
	http.HandleFunc("/cluster/status", cs.ClusterStatusHandler)
	http.HandleFunc("/cluster/vote", cs.ClusterVoteHandler)
	http.HandleFunc("/cluster/heartbeat", cs.ClusterHeartbeatHandler)

	// Setup graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// Start server in goroutine
	go func() {
		log.Printf("Starting cache server on %s with %d peers", config.Server.Port, len(peerList)-1)
		log.Printf("Node ID: %s", config.Cluster.NodeID)
		log.Printf("Cache config: MaxSize=%d bytes, MaxItems=%d, EvictionPolicy=%s",
			config.Cache.MaxSize, config.Cache.MaxItems, config.Cache.EvictionPolicy)

		server := &http.Server{
			Addr:           config.Server.Port,
			ReadTimeout:    config.Server.ReadTimeout,
			WriteTimeout:   config.Server.WriteTimeout,
			MaxHeaderBytes: config.Server.MaxHeaderBytes,
		}

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()

	// Print startup banner
	fmt.Println("\n=== Distributed Cache Server ===")
	fmt.Printf("Port: %s\n", config.Server.Port)
	fmt.Printf("Peers: %v\n", peerList)
	fmt.Printf("Virtual Nodes: %d\n", config.Cluster.VirtualNodes)
	fmt.Printf("Replication Factor: %d\n", config.Cluster.ReplicationFactor)
	fmt.Printf("Consistency Level: %s\n", config.Cluster.ConsistencyLevel)
	fmt.Println("==================================\n")

	// Wait for shutdown signal
	<-c
	log.Println("\nShutting down server...")

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	server := &http.Server{Addr: config.Server.Port}
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
