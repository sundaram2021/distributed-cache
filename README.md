# Distributed Cache

A production-ready, enterprise-grade distributed cache implementation in Go featuring advanced consistent hashing, multiple eviction policies, data compression, configurable replication strategies, comprehensive monitoring, persistence layer, security features, and cluster management.

## 1. Installation

### Prerequisites
- Go 1.19 or later
- Git for cloning the repository

### Quick Start

```bash
# Clone the repository
git clone <repository-url>
cd distributed-cache

# Run a single node
go run cmd/main.go --port=:8080 --node-id=node1

# Run a cluster (in separate terminals)
go run cmd/main.go --port=:8080 --node-id=node1 --peers=http://localhost:8081,http://localhost:8082
go run cmd/main.go --port=:8081 --node-id=node2 --peers=http://localhost:8080,http://localhost:8082
go run cmd/main.go --port=:8082 --node-id=node3 --peers=http://localhost:8080,http://localhost:8081
```

### Command Line Options

| Flag | Description | Default | Example |
|------|-------------|---------|----------|
| `--port` | HTTP server port | `:8080` | `--port=:9000` |
| `--node-id` | Unique node identifier | `""` | `--node-id=cache-node-1` |
| `--peers` | Comma-separated peer addresses | `""` | `--peers=http://node1:8080,http://node2:8081` |
| `--config` | Configuration file path | `""` | `--config=./config.json` |

### Building from Source

```bash
# Build the main server
go build -o distributed-cache cmd/main.go

# Build the benchmark tool
go build -o benchmark cmd/benchmark/main.go

# Run tests
go test ./src/... -v

# Run benchmarks (if implemented)
go test ./src -bench=. -benchmem
```

## 2. Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Distributed Cache Cluster                           │
├─────────────────────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐         │
│  │   Cache Node 1  │    │   Cache Node 2  │    │   Cache Node 3  │         │
│  │    (Leader)     │◄──►│   (Follower)    │◄──►│   (Follower)    │         │
│  │                 │    │                 │    │                 │         │
│  │ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │         │
│  │ │Security Mgr │ │    │ │Security Mgr │ │    │ │Security Mgr │ │         │
│  │ │Rate Limiter │ │    │ │Rate Limiter │ │    │ │Rate Limiter │ │         │
│  │ │Metrics Coll │ │    │ │Metrics Coll │ │    │ │Metrics Coll │ │         │
│  │ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │         │
│  │                 │    │                 │    │                 │         │
│  │ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │         │
│  │ │Advanced     │ │    │ │Advanced     │ │    │ │Advanced     │ │         │
│  │ │Cache Engine │ │    │ │Cache Engine │ │    │ │Cache Engine │ │         │
│  │ │- LRU/LFU    │ │    │ │- LRU/LFU    │ │    │ │- LRU/LFU    │ │         │
│  │ │- Compression│ │    │ │- Compression│ │    │ │- Compression│ │         │
│  │ │- TTL        │ │    │ │- TTL        │ │    │ │- TTL        │ │         │
│  │ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │         │
│  │                 │    │                 │    │                 │         │
│  │ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │         │
│  │ │Persistence  │ │    │ │Persistence  │ │    │ │Persistence  │ │         │
│  │ │- WAL        │ │    │ │- WAL        │ │    │ │- WAL        │ │         │
│  │ │- Snapshots  │ │    │ │- Snapshots  │ │    │ │- Snapshots  │ │         │
│  │ │- Recovery   │ │    │ │- Recovery   │ │    │ │- Recovery   │ │         │
│  │ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │         │
│  │                 │    │                 │    │                 │         │
│  │ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │         │
│  │ │Hash Ring    │ │    │ │Hash Ring    │ │    │ │Hash Ring    │ │         │
│  │ │- Virt Nodes │ │    │ │- Virt Nodes │ │    │ │- Virt Nodes │ │         │
│  │ │- Health     │ │    │ │- Health     │ │    │ │- Health     │ │         │
│  │ │- Replication│ │    │ │- Replication│ │    │ │- Replication│ │         │
│  │ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │         │
│  └─────────────────┘    └─────────────────┘    └─────────────────┘         │
│           │                       │                       │                 │
│           └───────────────────────┼───────────────────────┘                 │
│                                   │                                         │
│            Consistent Hash Ring with Virtual Nodes & Replication            │
│                   Leader Election & Cluster Management                      │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 3. Features

### Core Cache Features
- **Advanced Consistent Hashing**: Virtual nodes for optimal load distribution across cluster
- **Multiple Eviction Policies**: LRU (Least Recently Used), LFU (Least Frequently Used), TTL-based, and Random
- **Data Compression**: Automatic GZIP compression for values above configurable threshold
- **Configurable Replication**: Synchronous/asynchronous replication with consistency levels (ONE, QUORUM, ALL)
- **Memory Management**: Configurable size-based and item-based limits with automatic eviction
- **TTL Support**: Per-key time-to-live with background clean-up processes
- **Thread Safety**: Full concurrent access support with optimized locking strategies

### Persistence & Durability
- **Write-Ahead Logging (WAL)**: All operations logged for crash recovery and durability
- **Automatic Recovery**: Cache rebuilds from WAL on server restart
- **Snapshot Support**: Point-in-time cache snapshots for backup and recovery
- **Configurable Sync**: Adjustable sync intervals for performance vs durability trade-offs
- **Data Compaction**: Automatic WAL compaction to manage disk space

### Monitoring & Metrics
- **Prometheus Integration**: `/metrics` endpoint with Prometheus-compatible format
- **Detailed JSON Metrics**: `/metrics/json` for comprehensive system statistics
- **HTTP Request Tracking**: Response times, request counts, error rates per endpoint
- **System Monitoring**: Memory usage, goroutines, garbage collection statistics
- **Cache Performance**: Hit/miss ratios, eviction counts, compression ratios

### Security & Authentication
- **API Key Authentication**: Configurable API keys with role-based permissions
- **CORS Support**: Cross-origin resource sharing with configurable allowed origins
- **TLS Encryption**: Optional HTTPS support with certificate configuration
- **Request Authorization**: Fine-grained permissions (read, write, admin)
- **Data Encryption**: Optional client-side data encryption

### Cluster Management
- **Leader Election**: Automatic leader election using modified Raft algorithm
- **Health Monitoring**: Continuous node health checks with automatic failover
- **Gossip Protocol**: Distributed node discovery and membership management
- **Load Balancing**: Intelligent request distribution based on node health and load
- **Fault Tolerance**: Automatic node failure detection and recovery

### Performance Features
- **Rate Limiting**: Token bucket algorithm with configurable request limits
- **Connection Pooling**: Optimized HTTP client connections for inter-node communication
- **Benchmarking Tools**: Built-in load testing utilities for performance analysis
- **Request Classification**: Separate metrics tracking per endpoint type
- **Graceful Shutdown**: Clean server termination with signal handling

## 4. Caching Techniques

### Eviction Policies

#### 1. LRU (Least Recently Used)
- **Default policy** - Most commonly used
- Removes items that haven't been accessed recently
- Optimal for temporal locality patterns
- Time complexity: O(1) for all operations

#### 2. LFU (Least Frequently Used)
- Removes items with the lowest access frequency
- Better for workloads with strong frequency patterns
- Uses a min-heap for efficient frequency tracking
- Time complexity: O(log n) for eviction

#### 3. TTL (Time-To-Live)
- Removes items based on expiration time
- Evicts items with the earliest expiry first
- Ideal for time-sensitive data
- Automatic clean-up of expired items

#### 4. Random
- Removes random items when capacity is reached
- Fastest eviction strategy
- Useful when no particular access pattern exists
- O(1) eviction time

### Data Compression

Automatic GZIP compression with the following features:

- **Threshold-based**: Only compresses values larger than configured threshold (default: 1KB)
- **Configurable level**: Compression level 1-9 (default: 6, balanced speed/ratio)
- **Transparent**: Automatic compression on set, decompression on get
- **Statistics**: Tracks compression ratios in metrics
- **Memory efficient**: Reduces memory footprint for large values

### Memory Management

Advanced memory management with multiple constraints:

#### Size-Based Limits
- **max_size_bytes**: Total cache size limit
- **Automatic eviction**: When adding new items would exceed limit
- **Real-time tracking**: Current size updated on every operation

#### Item-Based Limits
- **max_items**: Maximum number of cached items
- **Prevents memory fragmentation**: Limits total object count
- **Fast enforcement**: O(1) item count checking

#### Memory Threshold Monitoring
- **Runtime monitoring**: Tracks Go runtime memory statistics
- **Automatic clean-up**: Triggers eviction at configurable memory usage
- **GC integration**: Works with Go garbage collector

### Consistency Levels 

#### ONE (Fastest)
- **Write**: Succeeds when written to one replica
- **Read**: Returns data from first available replica
- **Latency**: Lowest latency, highest performance
- **Consistency**: Eventual consistency
- **Use case**: High-performance caching, session storage

#### QUORUM (Balanced)
- **Write**: Succeeds when written to majority of replicas
- **Read**: Returns data confirmed by majority
- **Latency**: Moderate latency
- **Consistency**: Strong consistency for most operations
- **Use case**: General-purpose distributed caching

#### ALL (Strongest)
- **Write**: Succeeds only when written to all replicas
- **Read**: Confirms data from all available replicas
- **Latency**: Highest latency
- **Consistency**: Strongest consistency guarantee
- **Use case**: Critical data requiring maximum consistency

## 5. Command Examples

### Core Operations

#### Set Key-Value Pair

```bash
curl -X POST http://localhost:8080/set \
  -H "Content-Type: application/json" \
  -H "X-Consistency-Level: QUORUM" \
  -d '{"key": "user:123", "value": "John Doe", "ttl": 3600}'
```

#### Get Value by Key

```bash
curl "http://localhost:8080/get?key=user:123" \
  -H "X-Consistency-Level: QUORUM"
```

### Monitoring & Health

#### Health Check

```bash
curl http://localhost:8080/health
```

#### Cache Statistics

```bash
curl http://localhost:8080/stats
```

#### Prometheus Metrics

```bash
curl http://localhost:8080/metrics
```

#### Detailed JSON Metrics

```bash
curl http://localhost:8080/metrics/json
```

### Cluster Management

#### Cluster Status

```bash
curl http://localhost:8080/cluster/status
```

### Benchmarking

#### Run Default Benchmark

```bash
go run cmd/benchmark/main.go
```

#### Custom Benchmark

```bash
go run cmd/benchmark/main.go \
  -url http://localhost:8080 \
  -c 50 \
  -d 60s \
  -keys 10000 \
  -size 500 \
  -read 0.8
```
