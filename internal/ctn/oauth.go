package ctn

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/smartgroup/audio-bridge/internal/config"
)

// tokenResponse is the OAuth2 token endpoint response
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"` // seconds
	TokenType   string `json:"token_type"`
}

// TokenProvider manages OAuth2 client_credentials tokens with automatic refresh
type TokenProvider struct {
	cfg    config.CTNConfig
	http   *http.Client
	logger *zap.Logger

	mu        sync.RWMutex
	token     string
	expiresAt time.Time
}

// NewTokenProvider creates a new OAuth2 token provider for KeyCloak
func NewTokenProvider(cfg config.CTNConfig, logger *zap.Logger) *TokenProvider {
	return &TokenProvider{
		cfg: cfg,
		http: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSec) * time.Second,
		},
		logger: logger,
	}
}

// Token returns a valid access token, refreshing if necessary.
// Thread-safe: multiple goroutines can call this concurrently.
func (tp *TokenProvider) Token() (string, error) {
	tp.mu.RLock()
	if tp.token != "" && time.Now().Before(tp.expiresAt) {
		token := tp.token
		tp.mu.RUnlock()
		return token, nil
	}
	tp.mu.RUnlock()

	// Need to refresh — acquire write lock
	tp.mu.Lock()
	defer tp.mu.Unlock()

	// Double-check after acquiring write lock (another goroutine may have refreshed)
	if tp.token != "" && time.Now().Before(tp.expiresAt) {
		return tp.token, nil
	}

	return tp.refresh()
}

// refresh fetches a new token from the KeyCloak token endpoint.
// Must be called with tp.mu write lock held.
func (tp *TokenProvider) refresh() (string, error) {
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {tp.cfg.ClientID},
		"client_secret": {tp.cfg.ClientSecret},
	}

	req, err := http.NewRequest("POST", tp.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := tp.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("parsing token response: %w", err)
	}

	if tok.AccessToken == "" {
		return "", fmt.Errorf("token endpoint returned empty access_token")
	}

	// Cache with 30s safety margin
	tp.token = tok.AccessToken
	tp.expiresAt = time.Now().Add(time.Duration(tok.ExpiresIn)*time.Second - 30*time.Second)

	tp.logger.Info("OAuth2 token refreshed",
		zap.Int("expires_in", tok.ExpiresIn),
		zap.String("token_type", tok.TokenType))

	return tp.token, nil
}
