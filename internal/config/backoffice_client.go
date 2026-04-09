package config

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// cachedTenant holds a cached tenant config with expiration
type cachedTenant struct {
	tenant    *TenantConfig
	expiresAt time.Time
}

// BackofficeClient queries the CTN backoffice API for tenant configuration.
// Currently a skeleton — the HTTP call will be implemented when the CTN spec is available.
type BackofficeClient struct {
	cfg    BackofficeConfig
	cache  map[string]*cachedTenant
	mu     sync.RWMutex
	logger *zap.Logger
}

// NewBackofficeClient creates a new backoffice client
func NewBackofficeClient(cfg BackofficeConfig, logger *zap.Logger) *BackofficeClient {
	return &BackofficeClient{
		cfg:    cfg,
		cache:  make(map[string]*cachedTenant),
		logger: logger,
	}
}

// GetByDDI looks up a tenant by DDI (phone number) from the backoffice.
// First checks the local cache, then falls back to the HTTP API (not yet implemented).
func (c *BackofficeClient) GetByDDI(ddi string) (*TenantConfig, error) {
	// Check cache first
	c.mu.RLock()
	if cached, ok := c.cache[ddi]; ok && time.Now().Before(cached.expiresAt) {
		c.mu.RUnlock()
		c.logger.Debug("Backoffice cache hit", zap.String("ddi", ddi))
		return cached.tenant, nil
	}
	c.mu.RUnlock()

	// TODO: implement HTTP call to CTN backoffice API
	// Expected: GET {cfg.URL}/tenants?ddi={ddi} with X-API-Key header
	// Response: tenant config JSON that maps to TenantConfig
	return nil, fmt.Errorf("backoffice lookup not yet implemented for DDI %s", ddi)
}

// PutCache stores a tenant in the cache (useful for pre-warming or after successful lookups)
func (c *BackofficeClient) PutCache(ddi string, tenant *TenantConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[ddi] = &cachedTenant{
		tenant:    tenant,
		expiresAt: time.Now().Add(time.Duration(c.cfg.CacheTTL) * time.Second),
	}
}
