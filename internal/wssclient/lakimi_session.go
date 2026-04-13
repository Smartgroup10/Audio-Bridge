package wssclient

import (
	"encoding/base64"
	"sync"

	"github.com/smartgroup/audio-bridge/internal/models"
)

// =============================================================================
// LakimiSession — Per-call wrapper implementing AIClient
//
// Handles audio repacketization (20ms ↔ 30ms) and routes through the shared hub.
// The bridge's 3 goroutines interact with this exactly as they do with OpenAI.
// =============================================================================

// Frame sizes in bytes (PCM 16-bit mono 8kHz)
const (
	lakimiOutFrameSize = 480 // 30ms: 240 samples * 2 bytes
	lakimiInFrameSize  = 480 // 30ms from Lakimi
	slinFrame20ms      = 320 // 20ms: 160 samples * 2 bytes (Asterisk native)
)

// LakimiSession is a per-call AIClient backed by the shared LakimiHub.
type LakimiSession struct {
	hub           *LakimiHub
	interactionID string
	eventCh       chan models.AIEvent
	mu            sync.Mutex
	closed        bool

	// Outbound repacketizer: accumulates 20ms frames → sends 30ms frames
	outBuffer []byte

	// Inbound repacketizer: accumulates leftover bytes when splitting 30ms → 20ms
	inBuffer []byte

	// Transcript callbacks (set by bridge before goroutines start)
	OnUserTranscript func(text string)
	OnAITranscript   func(text string)
}

// newLakimiSession creates a session (called by LakimiHub.NewSession).
func newLakimiSession(hub *LakimiHub, interactionID string) *LakimiSession {
	return &LakimiSession{
		hub:           hub,
		interactionID: interactionID,
		eventCh:       make(chan models.AIEvent, 512),
	}
}

// --- AIClient interface implementation ---

// SendAudio receives 20ms SLIN frames from the bridge's streamCallerToAI,
// repacketizes to 30ms frames, and sends via the hub.
func (s *LakimiSession) SendAudio(pcm []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.outBuffer = append(s.outBuffer, pcm...)

	// Flush complete 30ms (480-byte) chunks
	for len(s.outBuffer) >= lakimiOutFrameSize {
		chunk := make([]byte, lakimiOutFrameSize)
		copy(chunk, s.outBuffer[:lakimiOutFrameSize])
		s.outBuffer = s.outBuffer[lakimiOutFrameSize:]

		msg := NewLakimiMediaMessage(s.interactionID, chunk)
		if err := s.hub.send(msg); err != nil {
			return err
		}
	}

	return nil
}

// SendEvent translates an AIEvent to a LakimiMessage and sends via the hub.
func (s *LakimiSession) SendEvent(event models.AIEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	switch event.Event {
	case "call_ended":
		// Will be handled by Close() → RemoveSession
		return nil

	case "dtmf_received":
		msg := NewLakimiDTMFMessage(s.interactionID, event.Digit)
		return s.hub.send(msg)

	case "transfer_completed":
		msg := NewLakimiTransferResultMessage(s.interactionID, event.Destination, event.Status)
		return s.hub.send(msg)

	default:
		// Forward as generic event
		msg := LakimiMessage{
			Event:         event.Event,
			InteractionID: s.interactionID,
			Reason:        event.Reason,
			Status:        event.Status,
		}
		return s.hub.send(msg)
	}
}

// Events returns the channel the bridge reads AI events from.
func (s *LakimiSession) Events() <-chan models.AIEvent {
	return s.eventCh
}

// Close unregisters the session from the hub and cleans up.
func (s *LakimiSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	s.hub.RemoveSession(s.interactionID, "session_closed", 0)
	close(s.eventCh)
	return nil
}

// closeInternal closes without notifying hub (called by hub during shutdown).
func (s *LakimiSession) closeInternal() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.eventCh)
	}
}

// --- Inbound audio repacketization (30ms → 20ms) ---

// pushInboundAudio is called by the hub's readLoop when a "media" message arrives.
// Lakimi sends 30ms frames (480 bytes). The bridge's playback loop expects events
// carrying audio that the audioBuffer can consume in any size. We split into
// 20ms chunks (320 bytes) so the jitter buffer aligns with Asterisk's frame timing.
func (s *LakimiSession) pushInboundAudio(b64Payload string) {
	audioBytes, err := base64.StdEncoding.DecodeString(b64Payload)
	if err != nil {
		return
	}

	s.mu.Lock()
	s.inBuffer = append(s.inBuffer, audioBytes...)
	s.mu.Unlock()

	// Emit 20ms chunks as _audio events
	for {
		s.mu.Lock()
		if len(s.inBuffer) < slinFrame20ms {
			s.mu.Unlock()
			break
		}
		frame := make([]byte, slinFrame20ms)
		copy(frame, s.inBuffer[:slinFrame20ms])
		s.inBuffer = s.inBuffer[slinFrame20ms:]
		s.mu.Unlock()

		select {
		case s.eventCh <- models.AIEvent{
			Event: "_audio",
			Data:  map[string]string{"_raw": string(frame)},
		}:
		default:
			// Channel full, drop frame
		}
	}
}

// transferFallbackEvent builds an AIEvent that signals the bridge to fallback-transfer.
func (s *LakimiSession) transferFallbackEvent() models.AIEvent {
	return models.AIEvent{
		Event:  "transfer",
		Reason: "ai_reconnect_failed",
	}
}

// FlushOutBuffer forces any remaining outbound audio to be sent (padding with silence).
// Called before session close to avoid losing trailing audio.
func (s *LakimiSession) FlushOutBuffer() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.outBuffer) == 0 || s.closed {
		return
	}

	// Pad to 30ms boundary with silence (zeros)
	padded := make([]byte, lakimiOutFrameSize)
	copy(padded, s.outBuffer)
	s.outBuffer = nil

	msg := NewLakimiMediaMessage(s.interactionID, padded)
	s.hub.send(msg)
}
