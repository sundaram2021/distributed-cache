package src

import (
	"encoding/json"
	"os"
	"time"
)

// Config holds all configuration for the distributed cache
type Config struct {
	// Server configuration
	Server ServerConfig `json:"server"`

	// Cache configuration
	Cache CacheConfig `json:"cache"`

	// Cluster configuration
	Cluster ClusterConfig `json:"cluster"`

	// Persistence configuration
	Persistence PersistenceConfig `json:"persistence"`

	// Security configuration
	Security SecurityConfig `json:"security"`

	// Monitoring configuration
	Monitoring MonitoringConfig `json:"monitoring"`
}

type ServerConfig struct {
	Port             string        `json:"port"`
	ReadTimeout      time.Duration `json:"read_timeout"`
	WriteTimeout     time.Duration `json:"write_timeout"`
	MaxHeaderBytes   int           `json:"max_header_bytes"`
	CompressionLevel int           `json:"compression_level"`
	RateLimitRPS     int           `json:"rate_limit_rps"`
}

type CacheConfig struct {
	MaxSize              int64             `json:"max_size_bytes"`
	MaxItems             int               `json:"max_items"`
	DefaultTTL           time.Duration     `json:"default_ttl"`
	EvictionPolicy       string            `json:"eviction_policy"` // LRU, LFU, TTL
	CleanupInterval      time.Duration     `json:"cleanup_interval"`
	EnableCompression    bool              `json:"enable_compression"`
	CompressionThreshold int               `json:"compression_threshold"`
	Persistence          PersistenceConfig `json:"persistence"`
}

type ClusterConfig struct {
	NodeID              string        `json:"node_id"`
	Peers               []string      `json:"peers"`
	VirtualNodes        int           `json:"virtual_nodes"`
	HealthCheckInterval time.Duration `json:"health_check_interval"`
	ReplicationFactor   int           `json:"replication_factor"`
	ConsistencyLevel    string        `json:"consistency_level"` // ONE, QUORUM, ALL
	EnableAutoDiscovery bool          `json:"enable_auto_discovery"`
	GossipPort          string        `json:"gossip_port"`
}

type PersistenceConfig struct {
	Enabled          bool          `json:"enabled"`
	DataDir          string        `json:"data_dir"`
	WALDir           string        `json:"wal_dir"`
	SyncInterval     time.Duration `json:"sync_interval"`
	CompactionSize   int64         `json:"compaction_size"`
	SnapshotEnabled  bool          `json:"snapshot_enabled"`
	SnapshotInterval time.Duration `json:"snapshot_interval"`
}

type SecurityConfig struct {
	Enabled          bool     `json:"enabled"`
	TLSCertFile      string   `json:"tls_cert_file"`
	TLSKeyFile       string   `json:"tls_key_file"`
	AllowedOrigins   []string `json:"allowed_origins"`
	RequireAuth      bool     `json:"require_auth"`
	AuthTokens       []string `json:"auth_tokens"`
	EnableEncryption bool     `json:"enable_encryption"`
	EncryptionKey    string   `json:"encryption_key"`
}

type MonitoringConfig struct {
	Enabled         bool          `json:"enabled"`
	MetricsPort     string        `json:"metrics_port"`
	LogLevel        string        `json:"log_level"`
	EnableProfiling bool          `json:"enable_profiling"`
	StatsInterval   time.Duration `json:"stats_interval"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:             ":8080",
			ReadTimeout:      30 * time.Second,
			WriteTimeout:     30 * time.Second,
			MaxHeaderBytes:   1 << 20, // 1MB
			CompressionLevel: 6,
			RateLimitRPS:     1000,
		},
		Cache: CacheConfig{
			MaxSize:              100 * 1024 * 1024, // 100MB
			MaxItems:             10000,
			DefaultTTL:           1 * time.Hour,
			EvictionPolicy:       "LRU",
			CleanupInterval:      1 * time.Minute,
			EnableCompression:    true,
			CompressionThreshold: 1024, // 1KB
		},
		Cluster: ClusterConfig{
			NodeID:              "",
			Peers:               []string{},
			VirtualNodes:        100,
			HealthCheckInterval: 10 * time.Second,
			ReplicationFactor:   2,
			ConsistencyLevel:    "QUORUM",
			EnableAutoDiscovery: false,
			GossipPort:          ":7946",
		},
		Persistence: PersistenceConfig{
			Enabled:          false,
			DataDir:          "./data",
			WALDir:           "./wal",
			SyncInterval:     5 * time.Second,
			CompactionSize:   10 * 1024 * 1024, // 10MB
			SnapshotEnabled:  true,
			SnapshotInterval: 30 * time.Minute,
		},
		Security: SecurityConfig{
			Enabled:          false,
			TLSCertFile:      "",
			TLSKeyFile:       "",
			AllowedOrigins:   []string{"*"},
			RequireAuth:      false,
			AuthTokens:       []string{},
			EnableEncryption: false,
			EncryptionKey:    "",
		},
		Monitoring: MonitoringConfig{
			Enabled:         true,
			MetricsPort:     ":9090",
			LogLevel:        "INFO",
			EnableProfiling: false,
			StatsInterval:   10 * time.Second,
		},
	}
}

// LoadConfig loads configuration from a file
func LoadConfig(filename string) (*Config, error) {
	config := DefaultConfig()

	if filename == "" {
		return config, nil
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		return config, err
	}

	err = json.Unmarshal(data, config)
	if err != nil {
		return config, err
	}

	return config, nil
}

// SaveConfig saves configuration to a file
func (c *Config) SaveConfig(filename string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}
