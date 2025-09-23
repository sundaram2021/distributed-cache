package src

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SecurityManager handles authentication, authorization, and encryption
type SecurityManager struct {
	apiKeys    map[string]*APIKey
	sessions   map[string]*Session
	encryptKey []byte
	mu         sync.RWMutex
	config     *SecurityConfig
}

// APIKey represents an API key with permissions
type APIKey struct {
	Key         string
	Name        string
	Permissions []string
	CreatedAt   time.Time
	LastUsed    time.Time
	Enabled     bool
}

// Session represents a user session
type Session struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
	IPAddress string
}

// NewSecurityManager creates a new security manager
func NewSecurityManager(config *SecurityConfig) (*SecurityManager, error) {
	if !config.Enabled {
		return nil, nil
	}

	var encryptKey []byte
	if config.EnableEncryption {
		if config.EncryptionKey != "" {
			key, err := hex.DecodeString(config.EncryptionKey)
			if err != nil {
				return nil, fmt.Errorf("invalid encryption key: %v", err)
			}
			encryptKey = key
		} else {
			// Generate a random key
			encryptKey = make([]byte, 32)
			if _, err := rand.Read(encryptKey); err != nil {
				return nil, fmt.Errorf("failed to generate encryption key: %v", err)
			}
		}
	}

	sm := &SecurityManager{
		apiKeys:    make(map[string]*APIKey),
		sessions:   make(map[string]*Session),
		encryptKey: encryptKey,
		config:     config,
	}

	// Initialize with configured API keys
	for _, token := range config.AuthTokens {
		sm.CreateAPIKey(token, "default", []string{"read", "write"})
	}

	return sm, nil
}

// CreateAPIKey creates a new API key
func (sm *SecurityManager) CreateAPIKey(key, name string, permissions []string) {
	if sm == nil {
		return
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.apiKeys[key] = &APIKey{
		Key:         key,
		Name:        name,
		Permissions: permissions,
		CreatedAt:   time.Now(),
		Enabled:     true,
	}
}

// ValidateAPIKey validates an API key and returns permissions
func (sm *SecurityManager) ValidateAPIKey(key string) ([]string, bool) {
	if sm == nil || !sm.config.RequireAuth {
		return []string{"read", "write"}, true
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	apiKey, exists := sm.apiKeys[key]
	if !exists || !apiKey.Enabled {
		return nil, false
	}

	// Update last used time
	apiKey.LastUsed = time.Now()
	return apiKey.Permissions, true
}

// CreateSession creates a new session
func (sm *SecurityManager) CreateSession(userID, ipAddress string) string {
	if sm == nil {
		return ""
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	sessionID := generateRandomString(32)
	sm.sessions[sessionID] = &Session{
		ID:        sessionID,
		UserID:    userID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		IPAddress: ipAddress,
	}

	return sessionID
}

// ValidateSession validates a session
func (sm *SecurityManager) ValidateSession(sessionID, ipAddress string) (string, bool) {
	if sm == nil {
		return "", false
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[sessionID]
	if !exists || time.Now().After(session.ExpiresAt) {
		return "", false
	}

	// Optional IP validation
	if session.IPAddress != ipAddress {
		return "", false
	}

	return session.UserID, true
}

// Encrypt encrypts data if encryption is enabled
func (sm *SecurityManager) Encrypt(data []byte) ([]byte, error) {
	if sm == nil || !sm.config.EnableEncryption {
		return data, nil
	}

	// Simple XOR encryption for demonstration
	// In production, use proper encryption like AES-GCM
	encrypted := make([]byte, len(data))
	for i, b := range data {
		encrypted[i] = b ^ sm.encryptKey[i%len(sm.encryptKey)]
	}

	return encrypted, nil
}

// Decrypt decrypts data if encryption is enabled
func (sm *SecurityManager) Decrypt(data []byte) ([]byte, error) {
	if sm == nil || !sm.config.EnableEncryption {
		return data, nil
	}

	// Simple XOR decryption
	decrypted := make([]byte, len(data))
	for i, b := range data {
		decrypted[i] = b ^ sm.encryptKey[i%len(sm.encryptKey)]
	}

	return decrypted, nil
}

// AuthMiddleware provides HTTP authentication middleware
func (sm *SecurityManager) AuthMiddleware(next http.HandlerFunc, requiredPermissions []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sm == nil || !sm.config.RequireAuth {
			next(w, r)
			return
		}

		// Check API key in header
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			// Check Authorization header
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				apiKey = strings.TrimPrefix(auth, "Bearer ")
			}
		}

		if apiKey == "" {
			http.Error(w, "API key required", http.StatusUnauthorized)
			return
		}

		permissions, valid := sm.ValidateAPIKey(apiKey)
		if !valid {
			http.Error(w, "Invalid API key", http.StatusUnauthorized)
			return
		}

		// Check permissions
		if !sm.hasPermission(permissions, requiredPermissions) {
			http.Error(w, "Insufficient permissions", http.StatusForbidden)
			return
		}

		next(w, r)
	}
}

// CORSMiddleware handles CORS
func (sm *SecurityManager) CORSMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sm == nil {
			next(w, r)
			return
		}

		origin := r.Header.Get("Origin")
		if sm.isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
		w.Header().Set("Access-Control-Max-Age", "3600")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

// Helper functions

func (sm *SecurityManager) hasPermission(userPermissions, requiredPermissions []string) bool {
	if len(requiredPermissions) == 0 {
		return true
	}

	permissionSet := make(map[string]bool)
	for _, perm := range userPermissions {
		permissionSet[perm] = true
	}

	for _, required := range requiredPermissions {
		if !permissionSet[required] {
			return false
		}
	}

	return true
}

func (sm *SecurityManager) isAllowedOrigin(origin string) bool {
	if len(sm.config.AllowedOrigins) == 0 {
		return false
	}

	for _, allowed := range sm.config.AllowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}

	return false
}

func generateRandomString(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return base64.URLEncoding.EncodeToString(bytes)[:length]
}

// HashPassword hashes a password using SHA256
func HashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return hex.EncodeToString(hash[:])
}

// ComparePassword compares a password with its hash
func ComparePassword(password, hash string) bool {
	passwordHash := HashPassword(password)
	return subtle.ConstantTimeCompare([]byte(passwordHash), []byte(hash)) == 1
}
