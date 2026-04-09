package models

import (
	"sync"
	"time"
)

// CallState represents the current state of an active call
type CallState string

const (
	CallStateRinging   CallState = "ringing"
	CallStateConnected CallState = "connected"
	CallStateStreaming  CallState = "streaming"
	CallStateTransfer  CallState = "transferring"
	CallStateCompleted CallState = "completed"
	CallStateFailed    CallState = "failed"
)

// CallDirection indicates if the call is inbound or outbound
type CallDirection string

const (
	CallInbound  CallDirection = "inbound"
	CallOutbound CallDirection = "outbound"
)

// Call holds all state for a single call session
type Call struct {
	mu              sync.RWMutex
	ID              string            `json:"id"`
	AsteriskChannel string            `json:"asterisk_channel"`
	CallerID        string            `json:"caller_id"`
	DDI             string            `json:"ddi"`
	NotariaID       string            `json:"notaria_id"`
	Direction       CallDirection     `json:"direction"`
	State           CallState         `json:"state"`
	Schedule        string            `json:"schedule"`
	CallType        string            `json:"call_type"`
	ContextID       string            `json:"context_id"`
	ContextData     map[string]string `json:"context_data"`
	StartTime       time.Time         `json:"start_time"`
	AnswerTime      *time.Time        `json:"answer_time,omitempty"`
	EndTime         *time.Time        `json:"end_time,omitempty"`
	Duration        float64           `json:"duration_seconds"`
	EndReason       string            `json:"end_reason"`
	TransferDest    string            `json:"transfer_dest,omitempty"`
	RecordingCaller   string            `json:"recording_caller,omitempty"`
	RecordingAI       string            `json:"recording_ai,omitempty"`
	RecordingCallerMP3 string           `json:"recording_caller_mp3,omitempty"`
	RecordingAIMP3    string            `json:"recording_ai_mp3,omitempty"`
	RecordingMixedMP3 string            `json:"recording_mixed_mp3,omitempty"`
	TranscriptUser    string            `json:"transcript_user,omitempty"`
	TranscriptAI      string            `json:"transcript_ai,omitempty"`
}

func (c *Call) SetState(s CallState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.State = s
}

func (c *Call) GetState() CallState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.State
}

func (c *Call) Complete(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.State = CallStateCompleted
	c.EndTime = &now
	c.EndReason = reason
	if c.AnswerTime != nil {
		c.Duration = now.Sub(*c.AnswerTime).Seconds()
	}
}

func (c *Call) AppendTranscriptUser(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.TranscriptUser != "" {
		c.TranscriptUser += "\n"
	}
	c.TranscriptUser += text
}

func (c *Call) AppendTranscriptAI(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.TranscriptAI != "" {
		c.TranscriptAI += "\n"
	}
	c.TranscriptAI += text
}

// AIEvent represents a control event from or to the AI module
type AIEvent struct {
	Event           string            `json:"event"`
	Destination     string            `json:"destination,omitempty"`
	DestinationType string            `json:"destination_type,omitempty"`
	NotariaID       string            `json:"notaria_id,omitempty"`
	Via             string            `json:"via,omitempty"`
	Announce        string            `json:"announce,omitempty"`
	Reason          string            `json:"reason,omitempty"`
	Action          string            `json:"action,omitempty"`
	MOH             bool              `json:"moh,omitempty"`
	Digit           string            `json:"digit,omitempty"`
	Status          string            `json:"status,omitempty"`
	DurationSeconds float64           `json:"duration_seconds,omitempty"`
	Data            map[string]string `json:"data,omitempty"`
}

// OutboundRequest represents an API request to originate an outbound call
type OutboundRequest struct {
	Destination          string            `json:"destination" binding:"required"`
	NotariaID            string            `json:"notaria_id" binding:"required"`
	CallType             string            `json:"call_type"`
	ContextID            string            `json:"context_id"`
	ContextData          map[string]string `json:"context_data"`
	MaxRetries           int               `json:"max_retries"`
	RetryIntervalMinutes int               `json:"retry_interval_minutes"`
}

// OutboundResponse is the API response after requesting an outbound call
type OutboundResponse struct {
	CallID  string `json:"call_id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// CallStatusResponse is the API response for call status queries
type CallStatusResponse struct {
	CallID          string    `json:"call_id"`
	State           CallState `json:"state"`
	Direction       string    `json:"direction"`
	CallerID        string    `json:"caller_id"`
	NotariaID       string    `json:"notaria_id"`
	StartTime       string    `json:"start_time"`
	DurationSeconds float64   `json:"duration_seconds"`
	EndReason       string    `json:"end_reason,omitempty"`
}

// CallRegistry provides thread-safe access to active calls
type CallRegistry struct {
	mu    sync.RWMutex
	calls map[string]*Call
}

func NewCallRegistry() *CallRegistry {
	return &CallRegistry{
		calls: make(map[string]*Call),
	}
}

func (r *CallRegistry) Add(call *Call) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls[call.ID] = call
}

func (r *CallRegistry) Get(id string) (*Call, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.calls[id]
	return c, ok
}

func (r *CallRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.calls, id)
}

func (r *CallRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.calls)
}

// ActiveCount returns only non-completed calls (for concurrency limits)
func (r *CallRegistry) ActiveCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := 0
	for _, c := range r.calls {
		if c.GetState() != CallStateCompleted {
			n++
		}
	}
	return n
}

func (r *CallRegistry) List() []*Call {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]*Call, 0, len(r.calls))
	for _, c := range r.calls {
		list = append(list, c)
	}
	return list
}

// SSEEvent represents an event to broadcast via Server-Sent Events
type SSEEvent struct {
	Type string      `json:"type"` // call_started, call_ended, transcript_user, transcript_ai
	Data interface{} `json:"data"`
}

// SSEHub manages SSE client connections and broadcasts events
type SSEHub struct {
	mu      sync.RWMutex
	clients map[chan SSEEvent]struct{}
}

func NewSSEHub() *SSEHub {
	return &SSEHub{
		clients: make(map[chan SSEEvent]struct{}),
	}
}

func (h *SSEHub) Subscribe() chan SSEEvent {
	ch := make(chan SSEEvent, 64)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *SSEHub) Unsubscribe(ch chan SSEEvent) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *SSEHub) Broadcast(event SSEEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- event:
		default:
			// client too slow, drop event
		}
	}
}
