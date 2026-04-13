package wssclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/smartgroup/audio-bridge/internal/config"
)

// =============================================================================
// LakimiHub — Singleton multiplexed WSS client
//
// Maintains a single persistent WSS connection to Lakimi.
// All concurrent calls share this connection, discriminated by interaction_id.
// =============================================================================

// LakimiHub manages the shared WSS connection and routes messages to sessions.
type LakimiHub struct {
	conn      *websocket.Conn
	cfg       config.LakimiConfig
	logger    *zap.Logger
	mu        sync.RWMutex          // protects sessions map
	sessions  map[string]*LakimiSession
	writeMu   sync.Mutex            // serializes WSS writes
	connected bool
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewLakimiHub creates a new hub (not yet connected).
func NewLakimiHub(cfg config.LakimiConfig, logger *zap.Logger) *LakimiHub {
	return &LakimiHub{
		cfg:      cfg,
		logger:   logger.Named("lakimi-hub"),
		sessions: make(map[string]*LakimiSession),
	}
}

// Connect establishes the mTLS WSS connection and starts background loops.
func (h *LakimiHub) Connect(ctx context.Context) error {
	h.ctx, h.cancel = context.WithCancel(ctx)

	if err := h.dial(); err != nil {
		return fmt.Errorf("initial Lakimi connect: %w", err)
	}

	go h.readLoop()
	go h.pingLoop()

	h.logger.Info("Lakimi hub connected",
		zap.String("endpoint", h.cfg.Endpoint))
	return nil
}

// dial performs the actual WSS handshake with mTLS.
func (h *LakimiHub) dial() error {
	tlsCfg, err := h.buildTLSConfig()
	if err != nil {
		return fmt.Errorf("building TLS config: %w", err)
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		TLSClientConfig:  tlsCfg,
	}

	headers := http.Header{}
	headers.Set("User-Agent", "SmartGroup-AudioBridge/1.0")

	conn, _, err := dialer.DialContext(h.ctx, h.cfg.Endpoint, headers)
	if err != nil {
		return fmt.Errorf("WSS dial to Lakimi: %w", err)
	}

	h.writeMu.Lock()
	h.conn = conn
	h.connected = true
	h.writeMu.Unlock()

	return nil
}

// buildTLSConfig constructs mTLS configuration from certificate paths.
func (h *LakimiHub) buildTLSConfig() (*tls.Config, error) {
	// If no cert files configured, use default (no mTLS) — useful for testing
	if h.cfg.TLSCert == "" && h.cfg.TLSKey == "" && h.cfg.TLSCA == "" {
		h.logger.Warn("No mTLS certificates configured, using default TLS")
		return &tls.Config{}, nil
	}

	// Load client certificate + key
	cert, err := tls.LoadX509KeyPair(h.cfg.TLSCert, h.cfg.TLSKey)
	if err != nil {
		return nil, fmt.Errorf("loading client cert/key: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	// Load CA certificate for server verification
	if h.cfg.TLSCA != "" {
		caCert, err := os.ReadFile(h.cfg.TLSCA)
		if err != nil {
			return nil, fmt.Errorf("reading CA cert: %w", err)
		}
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsCfg.RootCAs = caPool
	}

	return tlsCfg, nil
}

// NewSession creates and registers a new per-call session on the shared WSS.
func (h *LakimiHub) NewSession(interactionID string, params ConnectParams) *LakimiSession {
	session := newLakimiSession(h, interactionID)

	h.mu.Lock()
	h.sessions[interactionID] = session
	h.mu.Unlock()

	// Send "start" event to Lakimi
	startMsg := NewLakimiStartMessage(params, DefaultAudioFormat(h.cfg.FrameSizeMs))
	if err := h.send(startMsg); err != nil {
		h.logger.Error("Failed to send start event",
			zap.String("interaction_id", interactionID),
			zap.Error(err))
	}

	h.logger.Info("Lakimi session registered",
		zap.String("interaction_id", interactionID),
		zap.String("notaria_id", params.NotariaID),
		zap.String("caller_id", params.CallerID))

	return session
}

// RemoveSession unregisters a session and sends "stop" to Lakimi.
func (h *LakimiHub) RemoveSession(interactionID, reason string, duration float64) {
	h.mu.Lock()
	delete(h.sessions, interactionID)
	h.mu.Unlock()

	stopMsg := NewLakimiStopMessage(interactionID, reason, duration)
	if err := h.send(stopMsg); err != nil {
		h.logger.Error("Failed to send stop event",
			zap.String("interaction_id", interactionID),
			zap.Error(err))
	}

	h.logger.Info("Lakimi session removed",
		zap.String("interaction_id", interactionID),
		zap.String("reason", reason))
}

// send serializes a LakimiMessage as JSON and writes it to the shared WSS.
func (h *LakimiHub) send(msg LakimiMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling Lakimi message: %w", err)
	}

	h.writeMu.Lock()
	defer h.writeMu.Unlock()

	if !h.connected || h.conn == nil {
		return fmt.Errorf("Lakimi WSS not connected")
	}

	h.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return h.conn.WriteMessage(websocket.TextMessage, data)
}

// getSession safely retrieves a session by interaction_id.
func (h *LakimiHub) getSession(interactionID string) (*LakimiSession, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[interactionID]
	return s, ok
}

// readLoop continuously reads from the shared WSS and dispatches to sessions.
func (h *LakimiHub) readLoop() {
	for {
		select {
		case <-h.ctx.Done():
			return
		default:
		}

		h.writeMu.Lock()
		conn := h.conn
		h.writeMu.Unlock()

		if conn == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, rawMsg, err := conn.ReadMessage()
		if err != nil {
			select {
			case <-h.ctx.Done():
				return
			default:
			}

			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				h.logger.Info("Lakimi WSS closed normally")
			} else {
				h.logger.Error("Lakimi WSS read error", zap.Error(err))
			}

			h.handleDisconnect()
			return
		}

		// Parse the message
		var msg LakimiMessage
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			h.logger.Error("Failed to parse Lakimi message",
				zap.Error(err),
				zap.String("raw", string(rawMsg)))
			continue
		}

		// Pong is hub-level, not per-session
		if msg.Event == LakimiEventPong {
			continue
		}

		// Route to session
		if msg.InteractionID == "" {
			h.logger.Warn("Lakimi message without interaction_id",
				zap.String("event", msg.Event))
			continue
		}

		session, ok := h.getSession(msg.InteractionID)
		if !ok {
			h.logger.Warn("Lakimi message for unknown session",
				zap.String("interaction_id", msg.InteractionID),
				zap.String("event", msg.Event))
			continue
		}

		// Handle audio specially: split 30ms frames into 20ms for jitter buffer
		if msg.Event == LakimiEventAudio {
			session.pushInboundAudio(msg.Payload)
			continue
		}

		// Handle transcript callbacks
		if msg.Event == LakimiEventTranscript {
			if msg.Role == "user" && session.OnUserTranscript != nil {
				session.OnUserTranscript(msg.Transcript)
			} else if msg.Role == "ai" && session.OnAITranscript != nil {
				session.OnAITranscript(msg.Transcript)
			}
			// Don't push to eventCh — transcripts are handled via callbacks
			continue
		}

		// Convert to AIEvent and push to session
		aiEvent := msg.ToAIEvent()
		select {
		case session.eventCh <- aiEvent:
		default:
			h.logger.Warn("Session event channel full, dropping event",
				zap.String("interaction_id", msg.InteractionID),
				zap.String("event", msg.Event))
		}
	}
}

// pingLoop sends keepalive pings at a configurable interval.
func (h *LakimiHub) pingLoop() {
	interval := time.Duration(h.cfg.PingInterval) * time.Second
	if interval == 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.writeMu.Lock()
			if h.connected && h.conn != nil {
				h.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				err := h.conn.WriteMessage(websocket.PingMessage, nil)
				h.writeMu.Unlock()
				if err != nil {
					h.logger.Warn("Lakimi ping failed", zap.Error(err))
				}
			} else {
				h.writeMu.Unlock()
			}
		}
	}
}

// handleDisconnect triggers reconnection with exponential backoff.
func (h *LakimiHub) handleDisconnect() {
	h.writeMu.Lock()
	h.connected = false
	if h.conn != nil {
		h.conn.Close()
		h.conn = nil
	}
	h.writeMu.Unlock()

	maxRetries := h.cfg.ReconnectMaxRetries
	if maxRetries == 0 {
		maxRetries = 5
	}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		select {
		case <-h.ctx.Done():
			return
		default:
		}

		delay := time.Duration(1<<uint(attempt-1)) * time.Second // 1s, 2s, 4s, 8s, 16s
		h.logger.Info("Attempting Lakimi reconnection",
			zap.Int("attempt", attempt),
			zap.Int("max", maxRetries),
			zap.Duration("delay", delay))

		time.Sleep(delay)

		if err := h.dial(); err != nil {
			h.logger.Error("Lakimi reconnection failed",
				zap.Int("attempt", attempt),
				zap.Error(err))
			continue
		}

		h.logger.Info("Lakimi reconnected, re-registering sessions",
			zap.Int("attempt", attempt))

		// Re-register all active sessions
		h.reRegisterSessions()

		// Restart readLoop
		go h.readLoop()
		return
	}

	// All retries exhausted — signal all sessions to fallback
	h.logger.Error("All Lakimi reconnection attempts failed, triggering fallback")
	h.mu.RLock()
	for id, session := range h.sessions {
		select {
		case session.eventCh <- session.transferFallbackEvent():
		default:
			h.logger.Warn("Could not send fallback to session",
				zap.String("interaction_id", id))
		}
	}
	h.mu.RUnlock()
}

// reRegisterSessions sends a "start" event for each active session after reconnection.
func (h *LakimiHub) reRegisterSessions() {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for id, session := range h.sessions {
		startMsg := LakimiMessage{
			Event:         LakimiEventStart,
			InteractionID: id,
			AudioFormat:   DefaultAudioFormat(h.cfg.FrameSizeMs),
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
		}
		if err := h.send(startMsg); err != nil {
			h.logger.Error("Failed to re-register session",
				zap.String("interaction_id", id),
				zap.Error(err))
		} else {
			_ = session // session is kept alive
			h.logger.Info("Session re-registered after reconnect",
				zap.String("interaction_id", id))
		}
	}
}

// IsConnected returns true if the hub has an active WSS connection.
func (h *LakimiHub) IsConnected() bool {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	return h.connected
}

// SessionCount returns the number of active sessions.
func (h *LakimiHub) SessionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.sessions)
}

// Close shuts down the hub and all sessions.
func (h *LakimiHub) Close() error {
	h.cancel()

	h.mu.Lock()
	for id, session := range h.sessions {
		session.closeInternal()
		delete(h.sessions, id)
	}
	h.mu.Unlock()

	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	h.connected = false
	if h.conn != nil {
		return h.conn.Close()
	}
	return nil
}
