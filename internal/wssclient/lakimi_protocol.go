package wssclient

import (
	"encoding/base64"
	"time"

	"github.com/smartgroup/audio-bridge/internal/models"
)

// =============================================================================
// Lakimi/CTN Multiplexed WSS Protocol
//
// All calls share a single WSS connection. Messages are routed by interaction_id.
// Audio format: PCM signed 16-bit little-endian, 8kHz, mono, 30ms frames (480 bytes).
// =============================================================================

// LakimiMessage is the bidirectional JSON envelope for the Lakimi protocol.
type LakimiMessage struct {
	Event         string            `json:"event"`
	InteractionID string            `json:"interaction_id"`
	Payload       string            `json:"payload,omitempty"`         // base64-encoded audio
	NotariaID     string            `json:"notaria_id,omitempty"`
	CallerID      string            `json:"caller_id,omitempty"`
	CallType      string            `json:"call_type,omitempty"`
	Schedule      string            `json:"schedule,omitempty"`
	DDIOrigin     string            `json:"ddi_origin,omitempty"`
	ContextID     string            `json:"context_id,omitempty"`
	ContextData   map[string]string `json:"context_data,omitempty"`
	AudioFormat   *LakimiAudioFmt   `json:"audio_format,omitempty"`
	Destination   string            `json:"destination,omitempty"`
	DestType      string            `json:"destination_type,omitempty"`
	Reason        string            `json:"reason,omitempty"`
	Status        string            `json:"status,omitempty"`
	Duration      float64           `json:"duration_seconds,omitempty"`
	Digit         string            `json:"digit,omitempty"`
	Timestamp     string            `json:"timestamp,omitempty"`
	Transcript    string            `json:"transcript,omitempty"`
	Role          string            `json:"role,omitempty"` // "user" or "ai"
}

// LakimiAudioFmt describes the audio encoding for a session.
type LakimiAudioFmt struct {
	Codec       string `json:"codec"`         // "pcm_s16le"
	SampleRate  int    `json:"sample_rate"`   // 8000
	Channels    int    `json:"channels"`      // 1
	FrameSizeMs int    `json:"frame_size_ms"` // 30
}

// DefaultAudioFormat returns the agreed-upon PCM format for Lakimi.
func DefaultAudioFormat(frameSizeMs int) *LakimiAudioFmt {
	if frameSizeMs == 0 {
		frameSizeMs = 30
	}
	return &LakimiAudioFmt{
		Codec:       "pcm_s16le",
		SampleRate:  8000,
		Channels:    1,
		FrameSizeMs: frameSizeMs,
	}
}

// --- Lakimi event names (outbound: bridge → Lakimi) ---
const (
	LakimiEventStart          = "start"
	LakimiEventStop           = "stop"
	LakimiEventMedia          = "media"
	LakimiEventDTMF           = "dtmf"
	LakimiEventTransferResult = "transfer_completed"
)

// --- Lakimi event names (inbound: Lakimi → bridge) ---
const (
	LakimiEventAudio      = "media"
	LakimiEventTransfer   = "transfer"
	LakimiEventHangup     = "hangup"
	LakimiEventHold       = "hold"
	LakimiEventTranscript = "transcript"
	LakimiEventPong       = "pong"
	LakimiEventError      = "error"
)

// ToAIEvent converts a Lakimi inbound message to a models.AIEvent
// that the bridge's processAIMessages loop can handle.
func (m LakimiMessage) ToAIEvent() models.AIEvent {
	switch m.Event {
	case LakimiEventAudio:
		audioBytes, _ := base64.StdEncoding.DecodeString(m.Payload)
		return models.AIEvent{
			Event: "_audio",
			Data:  map[string]string{"_raw": string(audioBytes)},
		}

	case LakimiEventTransfer:
		return models.AIEvent{
			Event:           "transfer",
			Destination:     m.Destination,
			DestinationType: m.DestType,
			Reason:          m.Reason,
		}

	case LakimiEventHangup:
		return models.AIEvent{
			Event:  "hangup",
			Reason: m.Reason,
		}

	case LakimiEventHold:
		return models.AIEvent{
			Event:  "hold",
			Action: "hold",
		}

	case LakimiEventTranscript:
		return models.AIEvent{
			Event: "transcript",
			Data: map[string]string{
				"text": m.Transcript,
				"role": m.Role,
			},
		}

	case LakimiEventError:
		return models.AIEvent{
			Event:  "error",
			Reason: m.Reason,
		}

	default:
		return models.AIEvent{
			Event: m.Event,
			Data:  map[string]string{"raw_event": m.Event},
		}
	}
}

// NewLakimiStartMessage builds a "start" message to register a new call session.
func NewLakimiStartMessage(params ConnectParams, audioFmt *LakimiAudioFmt) LakimiMessage {
	return LakimiMessage{
		Event:         LakimiEventStart,
		InteractionID: params.InteractionID,
		NotariaID:     params.NotariaID,
		CallerID:      params.CallerID,
		CallType:      params.CallType,
		Schedule:      params.Schedule,
		DDIOrigin:     params.DDIOrigin,
		ContextID:     params.ContextID,
		ContextData:   params.ContextData,
		AudioFormat:   audioFmt,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
}

// NewLakimiStopMessage builds a "stop" message to unregister a call session.
func NewLakimiStopMessage(interactionID, reason string, duration float64) LakimiMessage {
	return LakimiMessage{
		Event:         LakimiEventStop,
		InteractionID: interactionID,
		Reason:        reason,
		Duration:      duration,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
}

// NewLakimiMediaMessage builds a "media" message with base64-encoded audio.
func NewLakimiMediaMessage(interactionID string, pcm []byte) LakimiMessage {
	return LakimiMessage{
		Event:         LakimiEventMedia,
		InteractionID: interactionID,
		Payload:       base64.StdEncoding.EncodeToString(pcm),
	}
}

// NewLakimiDTMFMessage builds a "dtmf" message.
func NewLakimiDTMFMessage(interactionID, digit string) LakimiMessage {
	return LakimiMessage{
		Event:         LakimiEventDTMF,
		InteractionID: interactionID,
		Digit:         digit,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
}

// NewLakimiTransferResultMessage builds a "transfer_completed" status message.
func NewLakimiTransferResultMessage(interactionID, destination, status string) LakimiMessage {
	return LakimiMessage{
		Event:         LakimiEventTransferResult,
		InteractionID: interactionID,
		Destination:   destination,
		Status:        status,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
}
