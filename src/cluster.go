package src

import (
	"bytes"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

// ClusterManager handles cluster membership and leader election
type ClusterManager struct {
	nodeID            string
	peers             map[string]*ClusterNode
	isLeader          bool
	leaderID          string
	lastHeartbeat     time.Time
	electionTimeout   time.Duration
	heartbeatInterval time.Duration
	mu                sync.RWMutex
	config            *ClusterConfig
	stopChan          chan bool
}

// ClusterNode represents a node in the cluster
type ClusterNode struct {
	ID            string    `json:"id"`
	Address       string    `json:"address"`
	LastSeen      time.Time `json:"last_seen"`
	IsHealthy     bool      `json:"is_healthy"`
	Role          string    `json:"role"` // leader, follower, candidate
	Term          int64     `json:"term"`
	VoteCount     int       `json:"vote_count"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// ClusterState represents the current cluster state
type ClusterState struct {
	Leader    string                  `json:"leader"`
	Term      int64                   `json:"term"`
	Nodes     map[string]*ClusterNode `json:"nodes"`
	Timestamp time.Time               `json:"timestamp"`
}

// NewClusterManager creates a new cluster manager
func NewClusterManager(nodeID string, config *ClusterConfig) *ClusterManager {
	cm := &ClusterManager{
		nodeID:            nodeID,
		peers:             make(map[string]*ClusterNode),
		isLeader:          false,
		electionTimeout:   time.Duration(rand.Intn(5)+5) * time.Second, // 5-10 seconds
		heartbeatInterval: config.HealthCheckInterval,
		config:            config,
		stopChan:          make(chan bool),
	}

	// Add self to peers
	cm.peers[nodeID] = &ClusterNode{
		ID:            nodeID,
		Address:       "self",
		LastSeen:      time.Now(),
		IsHealthy:     true,
		Role:          "follower",
		Term:          0,
		LastHeartbeat: time.Now(),
	}

	return cm
}

// Start starts the cluster manager
func (cm *ClusterManager) Start() {
	log.Printf("Starting cluster manager for node %s", cm.nodeID)

	// Start leader election process
	go cm.startElectionProcess()

	// Start heartbeat process
	go cm.startHeartbeatProcess()

	// Start node discovery if enabled
	if cm.config.EnableAutoDiscovery {
		go cm.startNodeDiscovery()
	}
}

// Stop stops the cluster manager
func (cm *ClusterManager) Stop() {
	close(cm.stopChan)
}

// AddPeer adds a peer to the cluster
func (cm *ClusterManager) AddPeer(nodeID, address string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.peers[nodeID]; !exists {
		cm.peers[nodeID] = &ClusterNode{
			ID:            nodeID,
			Address:       address,
			LastSeen:      time.Now(),
			IsHealthy:     true,
			Role:          "follower",
			Term:          0,
			LastHeartbeat: time.Now(),
		}
		log.Printf("Added peer %s at %s", nodeID, address)
	}
}

// RemovePeer removes a peer from the cluster
func (cm *ClusterManager) RemovePeer(nodeID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.peers, nodeID)
	log.Printf("Removed peer %s", nodeID)
}

// GetLeader returns the current leader
func (cm *ClusterManager) GetLeader() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.leaderID
}

// IsLeader returns whether this node is the leader
func (cm *ClusterManager) IsLeader() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.isLeader
}

// GetClusterState returns the current cluster state
func (cm *ClusterManager) GetClusterState() ClusterState {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Create a copy of peers
	nodesCopy := make(map[string]*ClusterNode)
	for id, node := range cm.peers {
		nodeCopy := *node
		nodesCopy[id] = &nodeCopy
	}

	return ClusterState{
		Leader:    cm.leaderID,
		Term:      cm.getCurrentTerm(),
		Nodes:     nodesCopy,
		Timestamp: time.Now(),
	}
}

// startElectionProcess starts the leader election process
func (cm *ClusterManager) startElectionProcess() {
	ticker := time.NewTicker(cm.electionTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !cm.isLeader && cm.shouldStartElection() {
				go cm.startElection()
			}
		case <-cm.stopChan:
			return
		}
	}
}

// shouldStartElection determines if this node should start an election
func (cm *ClusterManager) shouldStartElection() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Start election if no leader or leader is unhealthy
	if cm.leaderID == "" {
		return true
	}

	if leader, exists := cm.peers[cm.leaderID]; exists {
		return !leader.IsHealthy || time.Since(leader.LastHeartbeat) > cm.electionTimeout
	}

	return true
}

// startElection starts a new election
func (cm *ClusterManager) startElection() {
	cm.mu.Lock()

	// Increment term and vote for self
	currentTerm := cm.getCurrentTerm() + 1
	self := cm.peers[cm.nodeID]
	self.Term = currentTerm
	self.Role = "candidate"
	self.VoteCount = 1 // Vote for self

	log.Printf("Node %s starting election for term %d", cm.nodeID, currentTerm)
	cm.mu.Unlock()

	// Request votes from peers
	votes := 1 // Self vote
	requiredVotes := (len(cm.peers) / 2) + 1

	for peerID, peer := range cm.peers {
		if peerID != cm.nodeID && peer.IsHealthy {
			go func(peerID, peerAddr string) {
				if cm.requestVote(peerID, peerAddr, currentTerm) {
					cm.mu.Lock()
					votes++
					cm.mu.Unlock()
				}
			}(peerID, peer.Address)
		}
	}

	// Wait for votes or timeout
	time.Sleep(2 * time.Second)

	cm.mu.Lock()
	defer cm.mu.Unlock()

	if votes >= requiredVotes {
		cm.becomeLeader(currentTerm)
	} else {
		self.Role = "follower"
		log.Printf("Node %s failed to win election (got %d of %d required votes)", cm.nodeID, votes, requiredVotes)
	}
}

// requestVote requests a vote from a peer
func (cm *ClusterManager) requestVote(peerID, peerAddr string, term int64) bool {
	if peerAddr == "self" {
		return false
	}

	client := &http.Client{Timeout: 2 * time.Second}
	voteReq := map[string]interface{}{
		"candidate_id": cm.nodeID,
		"term":         term,
	}

	reqBody, _ := json.Marshal(voteReq)
	resp, err := client.Post(peerAddr+"/cluster/vote", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		log.Printf("Failed to request vote from %s: %v", peerID, err)
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// becomeLeader makes this node the leader
func (cm *ClusterManager) becomeLeader(term int64) {
	cm.isLeader = true
	cm.leaderID = cm.nodeID

	self := cm.peers[cm.nodeID]
	self.Role = "leader"
	self.Term = term

	log.Printf("Node %s became leader for term %d", cm.nodeID, term)

	// Send heartbeats to maintain leadership
	go cm.sendHeartbeats()
}

// startHeartbeatProcess starts the heartbeat monitoring process
func (cm *ClusterManager) startHeartbeatProcess() {
	ticker := time.NewTicker(cm.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cm.checkNodeHealth()
		case <-cm.stopChan:
			return
		}
	}
}

// sendHeartbeats sends heartbeats to all peers (leader only)
func (cm *ClusterManager) sendHeartbeats() {
	ticker := time.NewTicker(cm.heartbeatInterval / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !cm.isLeader {
				return
			}
			cm.broadcastHeartbeat()
		case <-cm.stopChan:
			return
		}
	}
}

// broadcastHeartbeat sends heartbeat to all peers
func (cm *ClusterManager) broadcastHeartbeat() {
	cm.mu.RLock()
	currentTerm := cm.getCurrentTerm()
	peers := make(map[string]*ClusterNode)
	for id, node := range cm.peers {
		peers[id] = node
	}
	cm.mu.RUnlock()

	for peerID, peer := range peers {
		if peerID != cm.nodeID && peer.Address != "self" {
			go func(peerID, peerAddr string) {
				client := &http.Client{Timeout: 2 * time.Second}
				heartbeat := map[string]interface{}{
					"leader_id": cm.nodeID,
					"term":      currentTerm,
					"timestamp": time.Now().Unix(),
				}

				reqBody, _ := json.Marshal(heartbeat)
				resp, err := client.Post(peerAddr+"/cluster/heartbeat", "application/json", bytes.NewReader(reqBody))
				if err != nil {
					cm.markNodeUnhealthy(peerID)
				} else {
					resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						cm.markNodeHealthy(peerID)
					}
				}
			}(peerID, peer.Address)
		}
	}
}

// checkNodeHealth checks the health of all nodes
func (cm *ClusterManager) checkNodeHealth() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, node := range cm.peers {
		if time.Since(node.LastHeartbeat) > cm.electionTimeout {
			node.IsHealthy = false
			if node.ID == cm.leaderID {
				cm.leaderID = ""
				cm.isLeader = false
				log.Printf("Leader %s became unhealthy", node.ID)
			}
		}
	}
}

// markNodeHealthy marks a node as healthy
func (cm *ClusterManager) markNodeHealthy(nodeID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if node, exists := cm.peers[nodeID]; exists {
		node.IsHealthy = true
		node.LastHeartbeat = time.Now()
	}
}

// markNodeUnhealthy marks a node as unhealthy
func (cm *ClusterManager) markNodeUnhealthy(nodeID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if node, exists := cm.peers[nodeID]; exists {
		node.IsHealthy = false
		log.Printf("Node %s marked as unhealthy", nodeID)
	}
}

// startNodeDiscovery starts automatic node discovery
func (cm *ClusterManager) startNodeDiscovery() {
	// This would implement gossip protocol or service discovery
	// For now, it's a placeholder
	log.Printf("Auto-discovery enabled for node %s", cm.nodeID)
}

// getCurrentTerm returns the current term
func (cm *ClusterManager) getCurrentTerm() int64 {
	maxTerm := int64(0)
	for _, node := range cm.peers {
		if node.Term > maxTerm {
			maxTerm = node.Term
		}
	}
	return maxTerm
}

// HTTP Handlers for cluster operations

// ClusterStatusHandler returns cluster status
func (cm *ClusterManager) ClusterStatusHandler(w http.ResponseWriter, r *http.Request) {
	state := cm.GetClusterState()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

// VoteHandler handles vote requests
func (cm *ClusterManager) VoteHandler(w http.ResponseWriter, r *http.Request) {
	var voteReq struct {
		CandidateID string `json:"candidate_id"`
		Term        int64  `json:"term"`
	}

	if err := json.NewDecoder(r.Body).Decode(&voteReq); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Simple vote logic - grant vote if term is higher
	cm.mu.RLock()
	currentTerm := cm.getCurrentTerm()
	cm.mu.RUnlock()

	if voteReq.Term > currentTerm {
		w.WriteHeader(http.StatusOK)
		log.Printf("Granted vote to %s for term %d", voteReq.CandidateID, voteReq.Term)
	} else {
		w.WriteHeader(http.StatusConflict)
	}
}

// HeartbeatHandler handles heartbeat messages
func (cm *ClusterManager) HeartbeatHandler(w http.ResponseWriter, r *http.Request) {
	var heartbeat struct {
		LeaderID  string `json:"leader_id"`
		Term      int64  `json:"term"`
		Timestamp int64  `json:"timestamp"`
	}

	if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Update leader information
	cm.leaderID = heartbeat.LeaderID
	cm.isLeader = (heartbeat.LeaderID == cm.nodeID)

	if leader, exists := cm.peers[heartbeat.LeaderID]; exists {
		leader.LastHeartbeat = time.Now()
		leader.IsHealthy = true
		leader.Term = heartbeat.Term
	}

	w.WriteHeader(http.StatusOK)
}
