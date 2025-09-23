package src

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

// RateLimiter implements a simple token bucket rate limiter
type RateLimiter struct {
	tokens     int
	maxTokens  int
	refillRate int
	lastRefill time.Time
	mu         sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(maxTokens, refillRate int) *RateLimiter {
	return &RateLimiter{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

// Allow checks if a request is allowed
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastRefill)
	tokensToAdd := int(elapsed.Seconds()) * rl.refillRate

	if tokensToAdd > 0 {
		rl.tokens += tokensToAdd
		if rl.tokens > rl.maxTokens {
			rl.tokens = rl.maxTokens
		}
		rl.lastRefill = now
	}

	if rl.tokens > 0 {
		rl.tokens--
		return true
	}
	return false
}

const replicationHeader = "X-Replication-Request"
const consistencyHeader = "X-Consistency-Level"

type ConsistencyLevel int

const (
	ONE ConsistencyLevel = iota
	QUORUM
	ALL
)

type CacheServer struct {
	cache       *Cache
	peers       []string
	hash        *Hash
	config      *Config
	mu          sync.RWMutex
	health      map[string]bool
	metrics     *MetricsCollector
	rateLimiter *RateLimiter
	security    *SecurityManager
	cluster     *ClusterManager
}

func NewCacheServer(peers []string) *CacheServer {
	config := DefaultConfig()
	cs := &CacheServer{
		cache:       NewCache(config.Cache.MaxItems),
		peers:       peers,
		hash:        NewHash(config.Cluster.VirtualNodes),
		config:      config,
		health:      make(map[string]bool),
		rateLimiter: NewRateLimiter(config.Server.RateLimitRPS, config.Server.RateLimitRPS),
	}

	// Initialize metrics collector
	cs.metrics = NewMetricsCollector(cs.cache)

	// Initialize security manager
	securityManager, err := NewSecurityManager(&config.Security)
	if err != nil {
		log.Printf("Failed to initialize security: %v", err)
	}
	cs.security = securityManager

	// Initialize cluster manager
	cs.cluster = NewClusterManager(config.Cluster.NodeID, &config.Cluster)

	for _, peer := range peers {
		node := Node{
			ID:     peer,
			Addr:   peer,
			Weight: 1,
		}
		cs.hash.AddNode(node)
		cs.health[peer] = true
	}

	// Start health monitoring
	go cs.startHealthMonitoring()

	// Start cluster management
	go cs.cluster.Start()

	// Add configured peers to cluster
	for _, peer := range peers {
		if peer != "self" {
			cs.cluster.AddPeer(peer, peer)
		}
	}

	return cs
}

// SecureSetHandler wraps SetHandler with authentication
func (cs *CacheServer) SecureSetHandler(w http.ResponseWriter, r *http.Request) {
	handler := cs.security.AuthMiddleware(cs.SetHandler, []string{"write"})
	corsHandler := cs.security.CORSMiddleware(handler)
	corsHandler(w, r)
}

// SecureGetHandler wraps GetHandler with authentication
func (cs *CacheServer) SecureGetHandler(w http.ResponseWriter, r *http.Request) {
	handler := cs.security.AuthMiddleware(cs.GetHandler, []string{"read"})
	corsHandler := cs.security.CORSMiddleware(handler)
	corsHandler(w, r)
}

func (cs *CacheServer) SetHandler(w http.ResponseWriter, r *http.Request) {
	// Rate limiting
	if !cs.rateLimiter.Allow() {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	start := time.Now()
	defer func() {
		cs.metrics.RecordHTTPRequest(r.Method, "/set", time.Since(start), 200)
	}()

	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
		TTL   int    `json:"ttl,omitempty"` // TTL in seconds
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Determine TTL
	ttl := cs.config.Cache.DefaultTTL
	if req.TTL > 0 {
		ttl = time.Duration(req.TTL) * time.Second
	}

	// Get consistency level
	consistencyLevel := cs.parseConsistencyLevel(r.Header.Get(consistencyHeader))

	targetNodes := cs.hash.GetNodes(req.Key, cs.config.Cluster.ReplicationFactor)
	if len(targetNodes) == 0 {
		http.Error(w, "No available nodes", http.StatusServiceUnavailable)
		return
	}

	// Check if current node should handle this key
	handleLocally := false
	for _, node := range targetNodes {
		if node.Addr == "self" {
			handleLocally = true
			break
		}
	}

	if handleLocally {
		log.Printf("Setting key %q with value %q on current node", req.Key, req.Value)
		cs.cache.Set(req.Key, req.Value, ttl)

		// Replicate based on consistency level
		if r.Header.Get(replicationHeader) == "" {
			go cs.replicateSet(req.Key, req.Value, ttl, consistencyLevel)
		}
	} else {
		// Forward to primary node
		primaryNode := targetNodes[0]
		log.Printf("Forwarding set request for key %q to node %q", req.Key, primaryNode.Addr)
		cs.forwardSetRequest(primaryNode, r)
	}
	w.WriteHeader(http.StatusOK)
}

func (cs *CacheServer) replicateSet(key, value string, ttl time.Duration, consistencyLevel ConsistencyLevel) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	req := struct {
		Key   string `json:"key"`
		Value string `json:"value"`
		TTL   int    `json:"ttl"`
	}{
		Key:   key,
		Value: value,
		TTL:   int(ttl.Seconds()),
	}

	data, _ := json.Marshal(req)
	successCount := 0
	requiredSuccesses := cs.getRequiredSuccesses(consistencyLevel)

	// Replicate to all peers
	for _, peer := range cs.peers {
		if peer != "self" {
			go func(peer string) {
				client := &http.Client{Timeout: 5 * time.Second}
				req, err := http.NewRequest("POST", peer+"/set", bytes.NewReader(data))
				if err != nil {
					log.Printf("Failed to create replication request: %v", err)
					return
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set(replicationHeader, "true")

				resp, err := client.Do(req)
				if err != nil {
					log.Printf("Failed to replicate to peer %s: %v", peer, err)
					cs.hash.UpdateNodeHealth(peer, false)
					cs.health[peer] = false
				} else {
					resp.Body.Close()
					successCount++
					cs.hash.UpdateNodeHealth(peer, true)
					cs.health[peer] = true
				}
			}(peer)
		}
	}

	// For synchronous consistency, wait for required successes
	if consistencyLevel != ONE {
		// In a real implementation, you'd use channels to wait for responses
		time.Sleep(100 * time.Millisecond) // Simple wait
		if successCount < requiredSuccesses {
			log.Printf("Replication failed: only %d of %d required successes", successCount, requiredSuccesses)
		}
	}
}

func (cs *CacheServer) GetHandler(w http.ResponseWriter, r *http.Request) {
	// Rate limiting
	if !cs.rateLimiter.Allow() {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	start := time.Now()
	defer func() {
		cs.metrics.RecordHTTPRequest(r.Method, "/get", time.Since(start), 200)
	}()

	key := r.URL.Query().Get("key")
	consistencyLevel := cs.parseConsistencyLevel(r.Header.Get(consistencyHeader))

	targetNodes := cs.hash.GetNodes(key, cs.config.Cluster.ReplicationFactor)
	if len(targetNodes) == 0 {
		http.Error(w, "No available nodes", http.StatusServiceUnavailable)
		return
	}

	// Check if current node has the data
	handleLocally := false
	for _, node := range targetNodes {
		if node.Addr == "self" {
			handleLocally = true
			break
		}
	}

	if handleLocally {
		value, found := cs.cache.Get(key)
		if !found {
			// Try to get from replicas based on consistency level
			if consistencyLevel != ONE {
				value, found = cs.getFromReplicas(key, targetNodes)
			}
			if !found {
				http.NotFound(w, r)
				return
			}
		}
		err := json.NewEncoder(w).Encode(map[string]string{"value": value})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	} else {
		// Forward to primary node
		primaryNode := targetNodes[0]
		cs.forwardGetRequest(primaryNode, r, w)
	}
}

// Helper methods

func (cs *CacheServer) parseConsistencyLevel(header string) ConsistencyLevel {
	switch header {
	case "ONE":
		return ONE
	case "QUORUM":
		return QUORUM
	case "ALL":
		return ALL
	default:
		return ONE // Default to ONE
	}
}

func (cs *CacheServer) getRequiredSuccesses(level ConsistencyLevel) int {
	switch level {
	case ONE:
		return 1
	case QUORUM:
		return (len(cs.peers) / 2) + 1
	case ALL:
		return len(cs.peers)
	default:
		return 1
	}
}

func (cs *CacheServer) forwardSetRequest(targetNode *Node, r *http.Request) {
	cs.forwardRequest(targetNode, r)
}

func (cs *CacheServer) forwardGetRequest(targetNode *Node, r *http.Request, w http.ResponseWriter) {
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 100,
		},
		Timeout: 5 * time.Second,
	}

	getURL := fmt.Sprintf("%s%s?%s", targetNode.Addr, r.URL.Path, r.URL.RawQuery)
	req, err := http.NewRequest("GET", getURL, nil)
	if err != nil {
		log.Printf("Failed to create forward request: %v", err)
		http.Error(w, "Forward request failed", http.StatusInternalServerError)
		return
	}

	req.Header = r.Header
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to forward request to node %s: %v", targetNode.Addr, err)
		http.Error(w, "Forward request failed", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	// Copy response
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (cs *CacheServer) getFromReplicas(key string, nodes []*Node) (string, bool) {
	// Try to get from other replica nodes
	for _, node := range nodes {
		if node.Addr != "self" && cs.health[node.ID] {
			client := &http.Client{Timeout: 2 * time.Second}
			resp, err := client.Get(fmt.Sprintf("%s/get?key=%s", node.Addr, key))
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var result map[string]string
					if json.NewDecoder(resp.Body).Decode(&result) == nil {
						return result["value"], true
					}
				}
			}
		}
	}
	return "", false
}

func (cs *CacheServer) startHealthMonitoring() {
	ticker := time.NewTicker(cs.config.Cluster.HealthCheckInterval)
	for range ticker.C {
		cs.checkNodeHealth()
	}
}

func (cs *CacheServer) checkNodeHealth() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for _, peer := range cs.peers {
		if peer != "self" {
			go func(peer string) {
				client := &http.Client{Timeout: 3 * time.Second}
				resp, err := client.Get(peer + "/health")
				healthy := err == nil && resp.StatusCode == http.StatusOK
				if resp != nil {
					resp.Body.Close()
				}

				cs.mu.Lock()
				cs.health[peer] = healthy
				cs.hash.UpdateNodeHealth(peer, healthy)
				cs.mu.Unlock()
			}(peer)
		}
	}
}
func (cs *CacheServer) forwardRequest(targetNode *Node, r *http.Request) {
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 100,
		},
		Timeout: 5 * time.Second,
	}

	// Create a new request based on the method
	var req *http.Request
	var err error

	switch r.Method {
	case http.MethodGet:
		// Forward GET request with query parameters
		getURL := fmt.Sprintf("%s%s?%s", targetNode.Addr, r.URL.Path, r.URL.RawQuery)
		req, err = http.NewRequest(r.Method, getURL, nil)
	case http.MethodPost:
		// Forward POST request with body
		postURL := fmt.Sprintf("%s%s", targetNode.Addr, r.URL.Path)
		req, err = http.NewRequest(r.Method, postURL, r.Body)
	}

	if err != nil {
		log.Printf("Failed to create forward request: %v", err)
		return
	}

	// Copy the headers
	req.Header = r.Header

	// Perform the request
	resp, err := client.Do(req)
	if err != nil {
		// Check for a "connection refused" error
		var urlErr *url.Error
		if errors.As(err, &urlErr) && urlErr.Err != nil {
			var opErr *net.OpError
			if errors.As(urlErr.Err, &opErr) && opErr.Op == "dial" {
				var sysErr *os.SyscallError
				if errors.As(opErr.Err, &sysErr) && sysErr.Syscall == "connect" {
					log.Printf("Connection refused to node %s: %v", targetNode.Addr, err)
					cs.hash.UpdateNodeHealth(targetNode.ID, false)
					return
				}
			}
		}
		log.Printf("Failed to forward request to node %s: %v", targetNode.Addr, err)
		return
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
}

// HealthHandler provides a health check endpoint
func (cs *CacheServer) HealthHandler(w http.ResponseWriter, r *http.Request) {
	stats := cs.cache.GetStats()
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"stats":     stats,
		"nodes":     cs.hash.GetHealthyNodeCount(),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

// StatsHandler provides cache statistics
func (cs *CacheServer) StatsHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		cs.metrics.RecordHTTPRequest(r.Method, "/stats", time.Since(start), 200)
	}()

	stats := cs.cache.GetStats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// MetricsHandler provides Prometheus-style metrics
func (cs *CacheServer) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	cs.metrics.MetricsHandler(w, r)
}

// DetailedMetricsHandler provides detailed JSON metrics
func (cs *CacheServer) DetailedMetricsHandler(w http.ResponseWriter, r *http.Request) {
	cs.metrics.JSONMetricsHandler(w, r)
}

// ClusterStatusHandler provides cluster status
func (cs *CacheServer) ClusterStatusHandler(w http.ResponseWriter, r *http.Request) {
	cs.cluster.ClusterStatusHandler(w, r)
}

// ClusterVoteHandler handles cluster vote requests
func (cs *CacheServer) ClusterVoteHandler(w http.ResponseWriter, r *http.Request) {
	cs.cluster.VoteHandler(w, r)
}

// ClusterHeartbeatHandler handles cluster heartbeat messages
func (cs *CacheServer) ClusterHeartbeatHandler(w http.ResponseWriter, r *http.Request) {
	cs.cluster.HeartbeatHandler(w, r)
}
