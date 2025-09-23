package src

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// MetricsCollector collects and exposes cache metrics
type MetricsCollector struct {
	cache       *Cache
	startTime   time.Time
	httpMetrics *HTTPMetrics
	mu          sync.RWMutex
}

// HTTPMetrics tracks HTTP request metrics
type HTTPMetrics struct {
	RequestCount   int64 `json:"request_count"`
	ResponseTime   int64 `json:"avg_response_time_ms"`
	ErrorCount     int64 `json:"error_count"`
	SetRequests    int64 `json:"set_requests"`
	GetRequests    int64 `json:"get_requests"`
	HealthRequests int64 `json:"health_requests"`
	StatusRequests int64 `json:"status_requests"`
}

// DetailedMetrics provides comprehensive system metrics
type DetailedMetrics struct {
	Cache     CacheStats    `json:"cache"`
	HTTP      HTTPMetrics   `json:"http"`
	System    SystemMetrics `json:"system"`
	Uptime    string        `json:"uptime"`
	Timestamp int64         `json:"timestamp"`
}

// SystemMetrics provides system-level metrics
type SystemMetrics struct {
	MemoryUsage   int64   `json:"memory_usage_bytes"`
	MemoryPercent float64 `json:"memory_usage_percent"`
	GoRoutines    int     `json:"goroutines"`
	CPUUsage      float64 `json:"cpu_usage_percent"`
	GCPauseTotal  int64   `json:"gc_pause_total_ns"`
	GCCount       uint32  `json:"gc_count"`
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(cache *Cache) *MetricsCollector {
	return &MetricsCollector{
		cache:       cache,
		startTime:   time.Now(),
		httpMetrics: &HTTPMetrics{},
	}
}

// RecordHTTPRequest records HTTP request metrics
func (mc *MetricsCollector) RecordHTTPRequest(method, path string, duration time.Duration, statusCode int) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	atomic.AddInt64(&mc.httpMetrics.RequestCount, 1)

	// Update response time (simple moving average approximation)
	currentAvg := atomic.LoadInt64(&mc.httpMetrics.ResponseTime)
	newAvg := (currentAvg + duration.Milliseconds()) / 2
	atomic.StoreInt64(&mc.httpMetrics.ResponseTime, newAvg)

	if statusCode >= 400 {
		atomic.AddInt64(&mc.httpMetrics.ErrorCount, 1)
	}

	// Track request types
	switch path {
	case "/set":
		atomic.AddInt64(&mc.httpMetrics.SetRequests, 1)
	case "/get":
		atomic.AddInt64(&mc.httpMetrics.GetRequests, 1)
	case "/health":
		atomic.AddInt64(&mc.httpMetrics.HealthRequests, 1)
	case "/stats":
		atomic.AddInt64(&mc.httpMetrics.StatusRequests, 1)
	}
}

// GetDetailedMetrics returns comprehensive metrics
func (mc *MetricsCollector) GetDetailedMetrics() DetailedMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	cacheStats := mc.cache.GetStats()

	systemMetrics := SystemMetrics{
		MemoryUsage:   int64(m.Alloc),
		MemoryPercent: float64(m.Alloc) / float64(m.Sys) * 100,
		GoRoutines:    runtime.NumGoroutine(),
		GCPauseTotal:  int64(m.PauseTotalNs),
		GCCount:       m.NumGC,
	}

	uptime := time.Since(mc.startTime)

	return DetailedMetrics{
		Cache:     cacheStats,
		HTTP:      *mc.httpMetrics,
		System:    systemMetrics,
		Uptime:    uptime.String(),
		Timestamp: time.Now().Unix(),
	}
}

// MetricsHandler provides Prometheus-style metrics endpoint
func (mc *MetricsCollector) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		mc.RecordHTTPRequest(r.Method, "/metrics", time.Since(start), 200)
	}()

	metrics := mc.GetDetailedMetrics()

	w.Header().Set("Content-Type", "text/plain")

	// Prometheus format metrics
	fmt.Fprintf(w, "# HELP cache_hits_total Total number of cache hits\n")
	fmt.Fprintf(w, "# TYPE cache_hits_total counter\n")
	fmt.Fprintf(w, "cache_hits_total %d\n", metrics.Cache.Hits)

	fmt.Fprintf(w, "# HELP cache_misses_total Total number of cache misses\n")
	fmt.Fprintf(w, "# TYPE cache_misses_total counter\n")
	fmt.Fprintf(w, "cache_misses_total %d\n", metrics.Cache.Misses)

	fmt.Fprintf(w, "# HELP cache_items_current Current number of items in cache\n")
	fmt.Fprintf(w, "# TYPE cache_items_current gauge\n")
	fmt.Fprintf(w, "cache_items_current %d\n", metrics.Cache.CurrentItems)

	fmt.Fprintf(w, "# HELP cache_size_bytes Current cache size in bytes\n")
	fmt.Fprintf(w, "# TYPE cache_size_bytes gauge\n")
	fmt.Fprintf(w, "cache_size_bytes %d\n", metrics.Cache.CurrentSize)

	fmt.Fprintf(w, "# HELP http_requests_total Total HTTP requests\n")
	fmt.Fprintf(w, "# TYPE http_requests_total counter\n")
	fmt.Fprintf(w, "http_requests_total %d\n", metrics.HTTP.RequestCount)

	fmt.Fprintf(w, "# HELP system_memory_bytes System memory usage in bytes\n")
	fmt.Fprintf(w, "# TYPE system_memory_bytes gauge\n")
	fmt.Fprintf(w, "system_memory_bytes %d\n", metrics.System.MemoryUsage)

	fmt.Fprintf(w, "# HELP system_goroutines Number of goroutines\n")
	fmt.Fprintf(w, "# TYPE system_goroutines gauge\n")
	fmt.Fprintf(w, "system_goroutines %d\n", metrics.System.GoRoutines)
}

// JSONMetricsHandler provides JSON format metrics
func (mc *MetricsCollector) JSONMetricsHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		mc.RecordHTTPRequest(r.Method, "/metrics/json", time.Since(start), 200)
	}()

	metrics := mc.GetDetailedMetrics()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// HTTPMiddleware wraps HTTP handlers to collect metrics
func (mc *MetricsCollector) HTTPMiddleware(next http.HandlerFunc, path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap ResponseWriter to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: 200}

		next(wrapped, r)

		mc.RecordHTTPRequest(r.Method, path, time.Since(start), wrapped.statusCode)
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
