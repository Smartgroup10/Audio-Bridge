package webhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/smartgroup/audio-bridge/internal/config"
)

// Event types
const (
	EventCallCompleted = "call.completed" // AI call finished
	EventCallRouted    = "call.routed"    // Call routed without AI (VIP, after hours)
)

// Payload is the webhook body sent to CTN
type Payload struct {
	Event         string  `json:"event"`
	InteractionID string  `json:"interaction_id"`
	NotariaID     string  `json:"notaria_id"`
	CallerID      string  `json:"caller_id"`
	DDI           string  `json:"ddi"`
	Direction     string  `json:"direction"`
	Duration      float64 `json:"duration_seconds"`
	Result        string  `json:"result"`        // end_reason: "normal", "transfer", "caller_hangup", etc.
	TransferDest  string  `json:"transfer_dest,omitempty"`
	RecordingURL  string  `json:"recording_url,omitempty"` // Download URL for mixed MP3
	Schedule      string  `json:"schedule,omitempty"`
	Reason        string  `json:"reason,omitempty"` // For call.routed: "vip", "after_hours"
	Timestamp     string  `json:"timestamp"`
}

// Client sends webhook notifications to CTN
type Client struct {
	cfg    config.WebhookConfig
	http   *http.Client
	logger *zap.Logger
}

// NewClient creates a webhook client. Returns nil if webhooks are disabled.
func NewClient(cfg config.WebhookConfig, logger *zap.Logger) *Client {
	if !cfg.Enabled || cfg.URL == "" {
		logger.Info("Webhooks disabled")
		return nil
	}
	return &Client{
		cfg: cfg,
		http: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSec) * time.Second,
		},
		logger: logger,
	}
}

// Send dispatches a webhook payload asynchronously (fire-and-forget with retry)
func (c *Client) Send(payload Payload) {
	if c == nil {
		return
	}
	go c.sendWithRetry(payload)
}

func (c *Client) sendWithRetry(payload Payload) {
	body, err := json.Marshal(payload)
	if err != nil {
		c.logger.Error("Failed to marshal webhook payload", zap.Error(err))
		return
	}

	backoff := time.Second
	for attempt := 1; attempt <= c.cfg.RetryCount; attempt++ {
		req, err := http.NewRequest("POST", c.cfg.URL, bytes.NewReader(body))
		if err != nil {
			c.logger.Error("Failed to create webhook request", zap.Error(err))
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Event-Type", payload.Event)
		if c.cfg.APIKey != "" {
			req.Header.Set("X-API-Key", c.cfg.APIKey)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			c.logger.Warn("Webhook request failed",
				zap.Int("attempt", attempt),
				zap.Error(err))
			if attempt < c.cfg.RetryCount {
				time.Sleep(backoff)
				backoff *= 2
			}
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			c.logger.Info("Webhook sent",
				zap.String("event", payload.Event),
				zap.String("interaction_id", payload.InteractionID),
				zap.Int("status", resp.StatusCode))
			return
		}

		// Don't retry on 4xx (client error) except 429 (rate limit)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429 {
			c.logger.Warn("Webhook rejected (no retry)",
				zap.String("event", payload.Event),
				zap.Int("status", resp.StatusCode))
			return
		}

		c.logger.Warn("Webhook failed, retrying",
			zap.Int("attempt", attempt),
			zap.Int("status", resp.StatusCode))

		if attempt < c.cfg.RetryCount {
			time.Sleep(backoff)
			backoff *= 2
		}
	}

	c.logger.Error("Webhook exhausted all retries",
		zap.String("event", payload.Event),
		zap.String("interaction_id", payload.InteractionID),
		zap.String("url", c.cfg.URL),
		zap.Int("retries", c.cfg.RetryCount))
}

// BuildRecordingURL constructs the public download URL for a call's recording
func BuildRecordingURL(publicURL, callID string) string {
	if publicURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/api/v1/calls/%s/recording?format=mp3&channel=mixed", publicURL, callID)
}
