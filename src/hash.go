package src

import (
	"crypto/sha1"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Node represents a physical node in the system
type Node struct {
	ID          string
	Addr        string
	Weight      int
	LastSeen    time.Time
	Healthy     bool
	Connections int
	Load        float64
}

// VirtualNode represents a virtual node on the hash ring
type VirtualNode struct {
	Hash    uint32
	NodeID  string
	VNodeID int
}

// Hash represents the consistent hash ring with virtual nodes
type Hash struct {
	nodes        map[string]*Node
	virtualNodes []VirtualNode
	virtualCount int
	replicas     int
	lock         sync.RWMutex
}

// NewHash creates a new Hash with configurable virtual nodes
func NewHash(virtualNodes int) *Hash {
	return &Hash{
		nodes:        make(map[string]*Node),
		virtualNodes: make([]VirtualNode, 0),
		virtualCount: virtualNodes,
		replicas:     3, // default replicas
	}
}

// AddNode adds a node to the hash ring with virtual nodes
func (h *Hash) AddNode(node Node) {
	h.lock.Lock()
	defer h.lock.Unlock()

	node.LastSeen = time.Now()
	node.Healthy = true
	if node.Weight == 0 {
		node.Weight = 1
	}
	h.nodes[node.ID] = &node

	// Create virtual nodes
	vNodeCount := h.virtualCount * node.Weight
	for i := 0; i < vNodeCount; i++ {
		vNodeKey := fmt.Sprintf("%s-%d", node.ID, i)
		hash := h.hash(vNodeKey)
		vNode := VirtualNode{
			Hash:    hash,
			NodeID:  node.ID,
			VNodeID: i,
		}
		h.virtualNodes = append(h.virtualNodes, vNode)
	}

	// Sort virtual nodes by hash
	sort.Slice(h.virtualNodes, func(i, j int) bool {
		return h.virtualNodes[i].Hash < h.virtualNodes[j].Hash
	})
}

// RemoveNode removes a node and all its virtual nodes from the hash ring
func (h *Hash) RemoveNode(nodeID string) {
	h.lock.Lock()
	defer h.lock.Unlock()

	delete(h.nodes, nodeID)

	// Remove all virtual nodes for this physical node
	newVNodes := make([]VirtualNode, 0)
	for _, vNode := range h.virtualNodes {
		if vNode.NodeID != nodeID {
			newVNodes = append(newVNodes, vNode)
		}
	}
	h.virtualNodes = newVNodes
}

// GetNode returns the node responsible for the given key
func (h *Hash) GetNode(key string) *Node {
	if len(h.virtualNodes) == 0 {
		return nil
	}

	h.lock.RLock()
	defer h.lock.RUnlock()

	hash := h.hash(key)
	index := sort.Search(len(h.virtualNodes), func(i int) bool {
		return h.virtualNodes[i].Hash >= hash
	})

	if index == len(h.virtualNodes) {
		index = 0
	}

	nodeID := h.virtualNodes[index].NodeID
	node := h.nodes[nodeID]

	// Check if node is healthy
	if node != nil && node.Healthy {
		return node
	}

	// Find next healthy node
	for i := 1; i < len(h.virtualNodes); i++ {
		nextIndex := (index + i) % len(h.virtualNodes)
		nextNodeID := h.virtualNodes[nextIndex].NodeID
		nextNode := h.nodes[nextNodeID]
		if nextNode != nil && nextNode.Healthy {
			return nextNode
		}
	}

	return nil
}

// GetNodes returns multiple nodes for replication
func (h *Hash) GetNodes(key string, count int) []*Node {
	if len(h.virtualNodes) == 0 || count <= 0 {
		return nil
	}

	h.lock.RLock()
	defer h.lock.RUnlock()

	hash := h.hash(key)
	index := sort.Search(len(h.virtualNodes), func(i int) bool {
		return h.virtualNodes[i].Hash >= hash
	})

	if index == len(h.virtualNodes) {
		index = 0
	}

	nodes := make([]*Node, 0, count)
	seen := make(map[string]bool)

	for i := 0; i < len(h.virtualNodes) && len(nodes) < count; i++ {
		currentIndex := (index + i) % len(h.virtualNodes)
		nodeID := h.virtualNodes[currentIndex].NodeID

		if !seen[nodeID] {
			node := h.nodes[nodeID]
			if node != nil && node.Healthy {
				nodes = append(nodes, node)
				seen[nodeID] = true
			}
		}
	}

	return nodes
}

// UpdateNodeHealth updates the health status of a node
func (h *Hash) UpdateNodeHealth(nodeID string, healthy bool) {
	h.lock.Lock()
	defer h.lock.Unlock()

	if node, exists := h.nodes[nodeID]; exists {
		node.Healthy = healthy
		node.LastSeen = time.Now()
	}
}

// GetAllNodes returns all nodes in the ring
func (h *Hash) GetAllNodes() map[string]*Node {
	h.lock.RLock()
	defer h.lock.RUnlock()

	nodes := make(map[string]*Node)
	for id, node := range h.nodes {
		nodes[id] = node
	}
	return nodes
}

// GetHealthyNodeCount returns the number of healthy nodes
func (h *Hash) GetHealthyNodeCount() int {
	h.lock.RLock()
	defer h.lock.RUnlock()

	count := 0
	for _, node := range h.nodes {
		if node.Healthy {
			count++
		}
	}
	return count
}

func (h *Hash) hash(key string) uint32 {
	hsh := sha1.New()
	hsh.Write([]byte(key))
	return h.bytesToUint32(hsh.Sum(nil))
}

func (h *Hash) bytesToUint32(b []byte) uint32 {
	return (uint32(b[0]) << 24) | (uint32(b[1]) << 16) | (uint32(b[2]) << 8) | uint32(b[3])
}
