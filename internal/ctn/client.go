package ctn

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/smartgroup/audio-bridge/internal/config"
)

// HandleRequest is the body for POST /handle
type HandleRequest struct {
	SiteID        string `json:"siteId"`
	CallingNumber string `json:"callingNumber"`
}

// HandleResponse is the response from POST /handle
type HandleResponse struct {
	Action string   `json:"action"` // "reject", "vip-transfer", "progress"
	VIP    *VIPInfo `json:"vip,omitempty"`
}

// VIPInfo holds VIP transfer details from CTN
type VIPInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	PhoneNumber string `json:"phoneNumber"` // extension or E.164
}

// CallStartedRequest is the body for POST /call-started
type CallStartedRequest struct {
	SiteID        string `json:"siteId"`
	CallDirection string `json:"callDirection"` // "inbound" or "outbound"
	Timestamp     int64  `json:"timestamp"`     // epoch ms
	CallingNumber string `json:"callingNumber"` // E.164 or anonymous/unknown/restricted/unavailable
	CalledNumber  string `json:"calledNumber"`  // E.164
}

// CallEndedRequest is the body for POST /call-ended
type CallEndedRequest struct {
	SiteID    string `json:"siteId"`
	Timestamp int64  `json:"timestamp"` // epoch ms
}

// Client calls the CTN AVN Call Control API
type Client struct {
	token      *TokenProvider
	baseURL    string
	http       *http.Client
	retryCount int
	logger     *zap.Logger
}

// NewClient creates a CTN API client. Returns nil if CTN is disabled.
func NewClient(token *TokenProvider, cfg config.CTNConfig, logger *zap.Logger) *Client {
	if !cfg.Enabled || cfg.BaseURL == "" {
		logger.Info("CTN integration disabled")
		return nil
	}
	return &Client{
		token:      token,
		baseURL:    cfg.BaseURL,
		retryCount: cfg.RetryCount,
		http: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSec) * time.Second,
		},
		logger: logger,
	}
}

// Handle asks CTN what action to take for an incoming call.
// Returns the action (reject, vip-transfer, progress) and optional VIP info.
func (c *Client) Handle(callID, siteID, callingNumber string) (*HandleResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("CTN client is nil")
	}

	url := fmt.Sprintf("%s/avn-call-control/api/v1/private/handle?callId=%s", c.baseURL, callID)
	body := HandleRequest{
		SiteID:        siteID,
		CallingNumber: callingNumber,
	}

	var result HandleResponse
	if err := c.doWithRetry("POST", url, body, &result); err != nil {
		return nil, fmt.Errorf("CTN handle: %w", err)
	}

	c.logger.Info("CTN handle response",
		zap.String("call_id", callID),
		zap.String("action", result.Action))

	return &result, nil
}

// CallStarted notifies CTN that a call has started
func (c *Client) CallStarted(callID, siteID, direction string, timestamp int64, callingNumber, calledNumber string) error {
	if c == nil {
		return nil
	}

	url := fmt.Sprintf("%s/avn-call-control/api/v1/private/call-started?callId=%s", c.baseURL, callID)
	body := CallStartedRequest{
		SiteID:        siteID,
		CallDirection: direction,
		Timestamp:     timestamp,
		CallingNumber: callingNumber,
		CalledNumber:  calledNumber,
	}

	if err := c.doWithRetry("POST", url, body, nil); err != nil {
		return fmt.Errorf("CTN call-started: %w", err)
	}

	c.logger.Info("CTN call-started sent",
		zap.String("call_id", callID),
		zap.String("site_id", siteID))

	return nil
}

// CallEnded notifies CTN that a call has ended
func (c *Client) CallEnded(callID, siteID string, timestamp int64) error {
	if c == nil {
		return nil
	}

	url := fmt.Sprintf("%s/avn-call-control/api/v1/private/call-ended?callId=%s", c.baseURL, callID)
	body := CallEndedRequest{
		SiteID:    siteID,
		Timestamp: timestamp,
	}

	if err := c.doWithRetry("POST", url, body, nil); err != nil {
		return fmt.Errorf("CTN call-ended: %w", err)
	}

	c.logger.Info("CTN call-ended sent",
		zap.String("call_id", callID),
		zap.String("site_id", siteID))

	return nil
}

// doWithRetry executes an authenticated HTTP request with exponential backoff retry.
// If result is non-nil, the response body is decoded into it.
func (c *Client) doWithRetry(method, url string, body interface{}, result interface{}) error {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	backoff := time.Second
	for attempt := 1; attempt <= c.retryCount; attempt++ {
		err = c.doOnce(method, url, jsonBody, result)
		if err == nil {
			return nil
		}

		c.logger.Warn("CTN request failed, retrying",
			zap.String("url", url),
			zap.Int("attempt", attempt),
			zap.Error(err))

		if attempt < c.retryCount {
			time.Sleep(backoff)
			backoff *= 2
		}
	}

	return fmt.Errorf("exhausted %d retries: %w", c.retryCount, err)
}

func (c *Client) doOnce(method, url string, jsonBody []byte, result interface{}) error {
	token, err := c.token.Token()
	if err != nil {
		return fmt.Errorf("getting OAuth token: %w", err)
	}

	req, err := http.NewRequest(method, url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// 2xx = success
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if result != nil && resp.StatusCode != http.StatusNoContent {
			respBody, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("reading response: %w", err)
			}
			if err := json.Unmarshal(respBody, result); err != nil {
				return fmt.Errorf("parsing response: %w", err)
			}
		}
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
}
