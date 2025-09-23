package src

import (
	"bufio"
	"encoding/gob"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// WALEntry represents a write-ahead log entry
type WALEntry struct {
	Timestamp time.Time
	Operation string // SET, DELETE
	Key       string
	Value     []byte
	TTL       time.Duration
}

// PersistenceManager handles data persistence with WAL
type PersistenceManager struct {
	dataDir    string
	walDir     string
	walFile    *os.File
	walWriter  *bufio.Writer
	walEncoder *gob.Encoder
	mu         sync.RWMutex
	syncTicker *time.Ticker
	stopSync   chan bool
	config     *PersistenceConfig
}

// NewPersistenceManager creates a new persistence manager
func NewPersistenceManager(config *PersistenceConfig) (*PersistenceManager, error) {
	if !config.Enabled {
		return nil, nil
	}

	// Create directories
	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %v", err)
	}

	if err := os.MkdirAll(config.WALDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %v", err)
	}

	pm := &PersistenceManager{
		dataDir:  config.DataDir,
		walDir:   config.WALDir,
		config:   config,
		stopSync: make(chan bool),
	}

	// Initialize WAL
	if err := pm.initWAL(); err != nil {
		return nil, fmt.Errorf("failed to initialize WAL: %v", err)
	}

	// Start sync routine
	pm.startSyncRoutine()

	return pm, nil
}

// initWAL initializes the write-ahead log
func (pm *PersistenceManager) initWAL() error {
	walPath := filepath.Join(pm.walDir, "cache.wal")

	file, err := os.OpenFile(walPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	pm.walFile = file
	pm.walWriter = bufio.NewWriter(file)
	pm.walEncoder = gob.NewEncoder(pm.walWriter)

	return nil
}

// WriteEntry writes an entry to the WAL
func (pm *PersistenceManager) WriteEntry(operation, key string, value []byte, ttl time.Duration) error {
	if pm == nil {
		return nil
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	entry := WALEntry{
		Timestamp: time.Now(),
		Operation: operation,
		Key:       key,
		Value:     value,
		TTL:       ttl,
	}

	return pm.walEncoder.Encode(entry)
}

// Sync forces a sync of the WAL to disk
func (pm *PersistenceManager) Sync() error {
	if pm == nil {
		return nil
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	if err := pm.walWriter.Flush(); err != nil {
		return err
	}

	return pm.walFile.Sync()
}

// LoadFromWAL loads entries from WAL and applies them to cache
func (pm *PersistenceManager) LoadFromWAL(cache *Cache) error {
	if pm == nil {
		return nil
	}

	walPath := filepath.Join(pm.walDir, "cache.wal")

	file, err := os.Open(walPath)
	if os.IsNotExist(err) {
		return nil // No WAL file exists yet
	}
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)

	for {
		var entry WALEntry
		err := decoder.Decode(&entry)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Error reading WAL entry: %v", err)
			continue
		}

		// Apply entry to cache
		switch entry.Operation {
		case "SET":
			expiryTime := entry.Timestamp.Add(entry.TTL)
			if time.Now().Before(expiryTime) {
				cache.SetBytes(entry.Key, entry.Value, time.Until(expiryTime))
			}
		case "DELETE":
			cache.Delete(entry.Key)
		}
	}

	log.Printf("Loaded cache data from WAL")
	return nil
}

// CreateSnapshot creates a snapshot of the cache
func (pm *PersistenceManager) CreateSnapshot(cache *Cache) error {
	if pm == nil || !pm.config.SnapshotEnabled {
		return nil
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	snapshotPath := filepath.Join(pm.dataDir, fmt.Sprintf("snapshot_%d.gob", time.Now().Unix()))

	file, err := os.Create(snapshotPath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := gob.NewEncoder(file)

	// Get all cache items (this would need to be implemented in cache)
	stats := cache.GetStats()

	snapshotData := map[string]interface{}{
		"timestamp": time.Now(),
		"stats":     stats,
		"items":     cache.getAllItems(), // This method needs to be added to cache
	}

	return encoder.Encode(snapshotData)
}

// startSyncRoutine starts the background sync routine
func (pm *PersistenceManager) startSyncRoutine() {
	pm.syncTicker = time.NewTicker(pm.config.SyncInterval)

	go func() {
		for {
			select {
			case <-pm.syncTicker.C:
				if err := pm.Sync(); err != nil {
					log.Printf("WAL sync error: %v", err)
				}
			case <-pm.stopSync:
				pm.syncTicker.Stop()
				return
			}
		}
	}()
}

// Close closes the persistence manager
func (pm *PersistenceManager) Close() error {
	if pm == nil {
		return nil
	}

	close(pm.stopSync)

	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.walWriter != nil {
		pm.walWriter.Flush()
	}

	if pm.walFile != nil {
		return pm.walFile.Close()
	}

	return nil
}

// CompactWAL compacts the WAL file by removing old entries
func (pm *PersistenceManager) CompactWAL() error {
	if pm == nil {
		return nil
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Implementation would involve reading WAL, filtering recent entries,
	// and writing to a new file
	log.Printf("WAL compaction not yet implemented")
	return nil
}
