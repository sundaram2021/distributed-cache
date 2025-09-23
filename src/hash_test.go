package src

import (
	"testing"
)

func TestHashInitialization(t *testing.T) {
	hashRing := NewHash(100)
	if hashRing == nil {
		t.Errorf("NewHash() = %v, want non-nil", hashRing)
	}
}

func TestHashAddNode(t *testing.T) {
	hashRing := NewHash(100)
	node := Node{ID: "node1", Addr: "localhost:8080"}
	hashRing.AddNode(node)

	if len(hashRing.nodes) != 1 {
		t.Errorf("len(hash.nodes) = %v, want %v", len(hashRing.nodes), 1)
	}
}

func TestHashRemoveNode(t *testing.T) {
	hashRing := NewHash(100)
	node := Node{ID: "node1", Addr: "localhost:8080"}
	hashRing.AddNode(node)
	hashRing.RemoveNode("node1")

	if len(hashRing.nodes) != 0 {
		t.Errorf("len(hash.nodes) = %v, want %v", len(hashRing.nodes), 0)
	}
}

func TestHashGetNode(t *testing.T) {
	hashRing := NewHash(100)
	node1 := Node{ID: "node1", Addr: "localhost:8080"}
	node2 := Node{ID: "node2", Addr: "localhost:8081"}
	hashRing.AddNode(node1)
	hashRing.AddNode(node2)

	node := hashRing.GetNode("key1")
	if node == nil {
		t.Errorf("GetNode() returned nil")
	}
	if node.ID != "node1" && node.ID != "node2" {
		t.Errorf("GetNode() = %v, want either %v or %v", node.ID, "node1", "node2")
	}
}

func TestHashGetNodeWithEmptyRing(t *testing.T) {
	hashRing := NewHash(100)
	node := hashRing.GetNode("key1")

	if node != nil {
		t.Errorf("GetNode() = %v, want nil", node)
	}
}
