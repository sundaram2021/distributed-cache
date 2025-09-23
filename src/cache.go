package src

import (
	"bytes"
	"compress/gzip"
	"container/heap"
	"container/list"
	"io"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// EvictionPolicy defines the eviction strategy
type EvictionPolicy int

const (
	LRU EvictionPolicy = iota
	LFU
	TTL
	Random
)

// CacheItem stores the value and metadata of a cache entry
type CacheItem struct {
	Value        []byte
	Compressed   bool
	ExpiryTime   time.Time
	CreatedTime  time.Time
	LastAccessed time.Time
	AccessCount  int64
	Size         int64
}

// entry is a helper struct for LRU/LFU eviction
type entry struct {
	key   string
	value *CacheItem
	freq  int64 // for LFU
}

// PriorityQueue implements heap.Interface for LFU eviction
type PriorityQueue []*entry

func (pq PriorityQueue) Len() int            { return len(pq) }
func (pq PriorityQueue) Less(i, j int) bool  { return pq[i].freq < pq[j].freq }
func (pq PriorityQueue) Swap(i, j int)       { pq[i], pq[j] = pq[j], pq[i] }
func (pq *PriorityQueue) Push(x interface{}) { *pq = append(*pq, x.(*entry)) }
func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}

// CacheStats holds statistics about cache performance
type CacheStats struct {
	Hits             int64
	Misses           int64
	Evictions        int64
	Expired          int64
	Sets             int64
	Gets             int64
	Deletes          int64
	CurrentSize      int64
	CurrentItems     int64
	MemoryUsage      int64
	CompressionRatio float64
}

// Cache represents a thread-safe in-memory cache with advanced features
type Cache struct {
	mu                 sync.RWMutex
	items              map[string]*list.Element
	lruList            *list.List
	lfuHeap            PriorityQueue
	config             *CacheConfig
	stats              *CacheStats
	evictionPolicy     EvictionPolicy
	maxSize            int64
	maxItems           int
	currentSize        int64
	cleanupTicker      *time.Ticker
	stopCleanup        chan bool
	compressionLevel   int
	memoryThreshold    float64
	persistenceManager *PersistenceManager
}

// NewCache creates a new advanced cache
func NewCache(capacity int) *Cache {
	config := &CacheConfig{
		MaxSize:              100 * 1024 * 1024, // 100MB
		MaxItems:             capacity,
		DefaultTTL:           1 * time.Hour,
		EvictionPolicy:       "LRU",
		CleanupInterval:      1 * time.Minute,
		EnableCompression:    true,
		CompressionThreshold: 1024,
	}

	policy := LRU
	switch config.EvictionPolicy {
	case "LFU":
		policy = LFU
	case "TTL":
		policy = TTL
	case "Random":
		policy = Random
	}

	cache := &Cache{
		items:            make(map[string]*list.Element),
		lruList:          list.New(),
		lfuHeap:          make(PriorityQueue, 0),
		config:           config,
		stats:            &CacheStats{},
		evictionPolicy:   policy,
		maxSize:          config.MaxSize,
		maxItems:         config.MaxItems,
		compressionLevel: 6,
		memoryThreshold:  0.8,
		stopCleanup:      make(chan bool),
	}

	// Initialize persistence if enabled
	defaultPersistence := PersistenceConfig{
		Enabled:          false,
		DataDir:          "./data",
		WALDir:           "./wal",
		SyncInterval:     5 * time.Second,
		CompactionSize:   10 * 1024 * 1024,
		SnapshotEnabled:  true,
		SnapshotInterval: 30 * time.Minute,
	}
	pm, err := NewPersistenceManager(&defaultPersistence)
	if err != nil {
		log.Printf("Failed to initialize persistence: %v", err)
	}
	cache.persistenceManager = pm

	heap.Init(&cache.lfuHeap)
	cache.startCleanup()

	// Load from WAL if persistence is enabled
	if pm != nil {
		if err := pm.LoadFromWAL(cache); err != nil {
			log.Printf("Failed to load from WAL: %v", err)
		}
	}

	return cache
}

// Set adds or updates a cache entry with the specified key, value, and TTL
func (c *Cache) Set(key, value string, ttl time.Duration) {
	valueBytes := []byte(value)
	c.SetBytes(key, valueBytes, ttl)
}

// SetBytes adds or updates a cache entry with byte value
func (c *Cache) SetBytes(key string, value []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	atomic.AddInt64(&c.stats.Sets, 1)

	// Compress if needed
	compressed := false
	processedValue := value
	if c.config.EnableCompression && len(value) > c.config.CompressionThreshold {
		var err error
		processedValue, err = c.compress(value)
		if err == nil {
			compressed = true
		}
	}

	item := &CacheItem{
		Value:        processedValue,
		Compressed:   compressed,
		ExpiryTime:   time.Now().Add(ttl),
		CreatedTime:  time.Now(),
		LastAccessed: time.Now(),
		AccessCount:  0,
		Size:         int64(len(processedValue)),
	}

	// Remove existing item
	if elem, found := c.items[key]; found {
		c.removeElement(elem)
	}

	// Enforce capacity
	c.enforceCapacity(item.Size)

	// Add new item
	elem := c.lruList.PushFront(&entry{key: key, value: item})
	c.items[key] = elem
	c.currentSize += item.Size
	atomic.AddInt64(&c.stats.CurrentItems, 1)
	atomic.StoreInt64(&c.stats.CurrentSize, c.currentSize)

	// Log to WAL if persistence is enabled
	if c.persistenceManager != nil {
		if err := c.persistenceManager.WriteEntry("SET", key, processedValue, ttl); err != nil {
			log.Printf("Failed to write to WAL: %v", err)
		}
	}
}

// Get retrieves a cache entry by its key. It returns the value and a boolean indicating whether the key was found
func (c *Cache) Get(key string) (string, bool) {
	valueBytes, found := c.GetBytes(key)
	if !found {
		return "", false
	}
	return string(valueBytes), true
}

// GetBytes retrieves a cache entry as bytes
func (c *Cache) GetBytes(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	atomic.AddInt64(&c.stats.Gets, 1)

	elem, found := c.items[key]
	if !found {
		atomic.AddInt64(&c.stats.Misses, 1)
		return nil, false
	}

	entry := elem.Value.(*entry)
	item := entry.value

	// Check expiration
	if time.Now().After(item.ExpiryTime) {
		c.removeElement(elem)
		atomic.AddInt64(&c.stats.Expired, 1)
		atomic.AddInt64(&c.stats.Misses, 1)
		return nil, false
	}

	// Update access metadata
	item.LastAccessed = time.Now()
	atomic.AddInt64(&item.AccessCount, 1)
	entry.freq = item.AccessCount

	// Update position based on eviction policy
	switch c.evictionPolicy {
	case LRU:
		c.lruList.MoveToFront(elem)
	case LFU:
		heap.Fix(&c.lfuHeap, c.findInHeap(key))
	}

	atomic.AddInt64(&c.stats.Hits, 1)

	// Decompress if needed
	value := item.Value
	if item.Compressed {
		var err error
		value, err = c.decompress(item.Value)
		if err != nil {
			return nil, false
		}
	}

	return value, true
}

// Delete removes an item from the cache
func (c *Cache) Delete(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	atomic.AddInt64(&c.stats.Deletes, 1)

	if elem, found := c.items[key]; found {
		c.removeElement(elem)

		// Log to WAL if persistence is enabled
		if c.persistenceManager != nil {
			if err := c.persistenceManager.WriteEntry("DELETE", key, nil, 0); err != nil {
				log.Printf("Failed to write DELETE to WAL: %v", err)
			}
		}

		return true
	}
	return false
}

// Helper methods

func (c *Cache) removeElement(elem *list.Element) {
	entry := elem.Value.(*entry)
	delete(c.items, entry.key)
	c.lruList.Remove(elem)
	c.currentSize -= entry.value.Size
	atomic.AddInt64(&c.stats.CurrentItems, -1)
	atomic.StoreInt64(&c.stats.CurrentSize, c.currentSize)
}

func (c *Cache) enforceCapacity(newItemSize int64) {
	// Check memory threshold
	if c.getMemoryUsageRatio() > c.memoryThreshold {
		c.evictItems(1)
	}

	// Check item count
	for len(c.items) >= c.maxItems {
		c.evictItems(1)
	}

	// Check size limit
	for c.currentSize+newItemSize > c.maxSize {
		c.evictItems(1)
	}
}

func (c *Cache) evictItems(count int) {
	for i := 0; i < count && len(c.items) > 0; i++ {
		switch c.evictionPolicy {
		case LRU:
			c.evictLRU()
		case LFU:
			c.evictLFU()
		case TTL:
			c.evictTTL()
		case Random:
			c.evictRandom()
		}
		atomic.AddInt64(&c.stats.Evictions, 1)
	}
}

func (c *Cache) evictLRU() {
	if elem := c.lruList.Back(); elem != nil {
		c.removeElement(elem)
	}
}

func (c *Cache) evictLFU() {
	if c.lfuHeap.Len() > 0 {
		entry := heap.Pop(&c.lfuHeap).(*entry)
		if elem, found := c.items[entry.key]; found {
			c.removeElement(elem)
		}
	}
}

func (c *Cache) evictTTL() {
	// Find item with earliest expiry
	var oldestElem *list.Element
	var oldestTime time.Time

	for _, elem := range c.items {
		entry := elem.Value.(*entry)
		if oldestElem == nil || entry.value.ExpiryTime.Before(oldestTime) {
			oldestElem = elem
			oldestTime = entry.value.ExpiryTime
		}
	}

	if oldestElem != nil {
		c.removeElement(oldestElem)
	}
}

func (c *Cache) evictRandom() {
	for _, elem := range c.items {
		c.removeElement(elem)
		break // Remove first (random) item
	}
}

func (c *Cache) findInHeap(key string) int {
	for i, entry := range c.lfuHeap {
		if entry.key == key {
			return i
		}
	}
	return -1
}

func (c *Cache) compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, c.compressionLevel)
	if err != nil {
		return nil, err
	}

	_, err = writer.Write(data)
	if err != nil {
		return nil, err
	}

	err = writer.Close()
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (c *Cache) decompress(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

func (c *Cache) getMemoryUsage() int64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return int64(m.Alloc)
}

func (c *Cache) getMemoryUsageRatio() float64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.Alloc) / float64(m.Sys)
}

func (c *Cache) startCleanup() {
	c.cleanupTicker = time.NewTicker(c.config.CleanupInterval)
	go func() {
		for {
			select {
			case <-c.cleanupTicker.C:
				c.cleanupExpired()
			case <-c.stopCleanup:
				c.cleanupTicker.Stop()
				return
			}
		}
	}()
}

func (c *Cache) cleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	toRemove := make([]*list.Element, 0)

	for _, elem := range c.items {
		entry := elem.Value.(*entry)
		if now.After(entry.value.ExpiryTime) {
			toRemove = append(toRemove, elem)
		}
	}

	for _, elem := range toRemove {
		c.removeElement(elem)
		atomic.AddInt64(&c.stats.Expired, 1)
	}
}

// GetStats returns cache statistics
func (c *Cache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := *c.stats
	stats.CurrentItems = int64(len(c.items))
	stats.CurrentSize = c.currentSize
	stats.MemoryUsage = c.getMemoryUsage()

	if stats.Sets > 0 {
		stats.CompressionRatio = float64(stats.CurrentSize) / float64(stats.Sets)
	}

	return stats
}

// Stop stops the cache cleanup goroutine
func (c *Cache) Stop() {
	close(c.stopCleanup)
	if c.persistenceManager != nil {
		c.persistenceManager.Close()
	}
}

// getAllItems returns all cache items for persistence
func (c *Cache) getAllItems() map[string]*CacheItem {
	c.mu.RLock()
	defer c.mu.RUnlock()

	items := make(map[string]*CacheItem)
	for _, elem := range c.items {
		entry := elem.Value.(*entry)
		items[entry.key] = entry.value
	}
	return items
}
