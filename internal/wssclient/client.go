package wssclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/smartgroup/audio-bridge/internal/config"
	"github.com/smartgroup/audio-bridge/internal/models"
)

// Client manages the WSS connection to the AI module for a single call
type Client struct {
	conn       *websocket.Conn
	aiCfg      config.AIConfig
	logger     *zap.Logger
	mu         sync.Mutex
	closed     bool
	eventCh    chan models.AIEvent // events received from AI
}

// ConnectParams holds the metadata sent in the WSS handshake
type ConnectParams struct {
	NotariaID     string
	CallerID      string
	InteractionID string
	CallType      string // "inbound", "callback"
	Schedule      string // "business_hours", "after_hours"
	DDIOrigin     string
	ContextID     string
	ContextData   map[string]string
}

// NewClient creates a new WSS client (not yet connected)
func NewClient(aiCfg config.AIConfig, logger *zap.Logger) *Client {
	return &Client{
		aiCfg:   aiCfg,
		logger:  logger,
		eventCh: make(chan models.AIEvent, 32),
	}
}

// Connect establishes the WSS connection to the AI module with call metadata
func (c *Client) Connect(ctx context.Context, params ConnectParams) error {
	// Build URL with query parameters (metadata in handshake)
	u, err := url.Parse(c.aiCfg.Endpoint)
	if err != nil {
		return fmt.Errorf("parsing AI endpoint: %w", err)
	}

	q := u.Query()
	q.Set("notaria_id", params.NotariaID)
	q.Set("caller_id", params.CallerID)
	q.Set("interaction_id", params.InteractionID)
	q.Set("call_type", params.CallType)
	q.Set("schedule", params.Schedule)
	q.Set("ddi_origin", params.DDIOrigin)
	if params.ContextID != "" {
		q.Set("context_id", params.ContextID)
	}
	for k, v := range params.ContextData {
		q.Set("ctx_"+k, v)
	}
	u.RawQuery = q.Encode()

	// Build headers with authentication
	headers := http.Header{}
	switch c.aiCfg.AuthType {
	case "api_key":
		headers.Set("X-API-Key", c.aiCfg.APIKey)
	case "bearer":
		headers.Set("Authorization", "Bearer "+c.aiCfg.BearerToken)
	}

	// Dial with timeout
	dialer := websocket.Dialer{
		HandshakeTimeout: time.Duration(c.aiCfg.TimeoutSec) * time.Second,
	}

	c.logger.Info("Connecting to AI module",
		zap.String("url", u.String()),
		zap.String("interaction_id", params.InteractionID))

	conn, _, err := dialer.DialContext(ctx, u.String(), headers)
	if err != nil {
		return fmt.Errorf("WSS dial to AI: %w", err)
	}

	c.conn = conn

	// Start reading events from AI in background
	go c.readLoop(ctx)

	c.logger.Info("Connected to AI module",
		zap.String("interaction_id", params.InteractionID))

	return nil
}

// SendAudio sends a binary audio frame to the AI module
func (c *Client) SendAudio(pcm []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("WSS connection closed")
	}
	c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.conn.WriteMessage(websocket.BinaryMessage, pcm)
}

// SendEvent sends a JSON control event to the AI module
func (c *Client) SendEvent(event models.AIEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("WSS connection closed")
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshaling event: %w", err)
	}
	c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

// Events returns the channel of events received from the AI module
func (c *Client) Events() <-chan models.AIEvent {
	return c.eventCh
}

// ReadAudio blocks until a binary (audio) message is received from the AI
// This is used by the bridge to get TTS audio to play back to the caller
func (c *Client) ReadAudio() ([]byte, error) {
	// Note: audio comes through readLoop and is NOT sent to eventCh
	// This method is not used directly - audio is handled in readLoop
	// via a separate audio channel. See bridge.go for the actual flow.
	return nil, fmt.Errorf("use the audio channel from bridge instead")
}

// Close gracefully closes the WSS connection
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	close(c.eventCh)
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// readLoop continuously reads messages from the AI module
// Binary messages = TTS audio, Text messages = JSON control events
func (c *Client) readLoop(ctx context.Context) {
	defer func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		msgType, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				c.logger.Info("AI WSS connection closed normally")
			} else {
				select {
				case <-ctx.Done():
					return
				default:
					c.logger.Error("AI WSS read error", zap.Error(err))
				}
			}
			return
		}

		switch msgType {
		case websocket.BinaryMessage:
			// TTS audio from AI - send as a special event with audio data
			// The bridge will intercept this and write to AudioSocket
			select {
			case c.eventCh <- models.AIEvent{
				Event: "_audio",
				Data:  map[string]string{"_raw": string(data)},
			}:
			default:
				c.logger.Warn("Event channel full, dropping audio frame")
			}

		case websocket.TextMessage:
			// JSON control event from AI
			var event models.AIEvent
			if err := json.Unmarshal(data, &event); err != nil {
				c.logger.Error("Failed to parse AI event", zap.Error(err), zap.String("raw", string(data)))
				continue
			}
			c.logger.Info("Received AI event",
				zap.String("event", event.Event),
				zap.String("destination", event.Destination))

			select {
			case c.eventCh <- event:
			default:
				c.logger.Warn("Event channel full, dropping event", zap.String("event", event.Event))
			}

		default:
			c.logger.Warn("Unknown WSS message type from AI", zap.Int("type", msgType))
		}
	}
}
