package wssclient

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/smartgroup/audio-bridge/internal/models"
)

// =============================================================================
// OpenAI Realtime API Adapter
//
// Audio format: G.711 mu-law (audio/pcmu) at 8kHz
// AudioSocket sends SLIN 16-bit 8kHz; we convert to/from mu-law for OpenAI
// =============================================================================

// OpenAI event types (client -> server)
const (
	OAIEventSessionUpdate          = "session.update"
	OAIEventAudioAppend            = "input_audio_buffer.append"
	OAIEventAudioCommit            = "input_audio_buffer.commit"
	OAIEventAudioClear             = "input_audio_buffer.clear"
	OAIEventResponseCreate         = "response.create"
	OAIEventResponseCancel         = "response.cancel"
	OAIEventConversationItemCreate = "conversation.item.create"
)

// OpenAI event types (server -> client)
const (
	OAIEventSessionCreated          = "session.created"
	OAIEventSessionUpdated          = "session.updated"
	OAIEventResponseAudioDelta      = "response.output_audio.delta"
	OAIEventResponseAudioDone       = "response.output_audio.done"
	OAIEventResponseDone            = "response.done"
	OAIEventError                   = "error"
	OAIEventAudioTranscriptDelta    = "response.output_audio_transcript.delta"
	OAIEventAudioTranscriptDone     = "response.output_audio_transcript.done"
	OAIEventInputAudioTranscription = "conversation.item.input_audio_transcription.completed"
	OAIEventSpeechStarted           = "input_audio_buffer.speech_started"
	OAIEventSpeechStopped           = "input_audio_buffer.speech_stopped"
	OAIEventResponseCreated         = "response.created"
)

// Reconnection constants
const (
	maxReconnectAttempts = 3
	reconnectBaseDelay   = 500 * time.Millisecond
)

// =============================================================================
// G.711 mu-law encoding/decoding (ITU-T G.711)
// =============================================================================

const (
	mulawBias = 0x84
	mulawMax  = 0x7FFF
)

func slinToMulaw(slin []byte) []byte {
	numSamples := len(slin) / 2
	mulaw := make([]byte, numSamples)
	for i := 0; i < numSamples; i++ {
		sample := int16(binary.LittleEndian.Uint16(slin[i*2 : i*2+2]))
		mulaw[i] = linearToMulaw(sample)
	}
	return mulaw
}

func mulawToSlin(mulaw []byte) []byte {
	slin := make([]byte, len(mulaw)*2)
	for i, b := range mulaw {
		sample := mulawToLinear(b)
		binary.LittleEndian.PutUint16(slin[i*2:i*2+2], uint16(sample))
	}
	return slin
}

func linearToMulaw(sample int16) byte {
	sign := 0
	if sample < 0 {
		sign = 0x80
		if sample == math.MinInt16 {
			sample = math.MaxInt16
		} else {
			sample = -sample
		}
	}
	if sample > mulawMax {
		sample = mulawMax
	}
	sample += mulawBias
	exp := 7
	for mask := int16(0x4000); (sample&mask) == 0 && exp > 0; exp-- {
		mask >>= 1
	}
	mantissa := (sample >> (uint(exp) + 3)) & 0x0F
	mulawByte := byte(sign | (exp << 4) | int(mantissa))
	return ^mulawByte
}

func mulawToLinear(mulaw byte) int16 {
	mulaw = ^mulaw
	sign := mulaw & 0x80
	exp := int((mulaw >> 4) & 0x07)
	mantissa := int(mulaw & 0x0F)
	sample := (mantissa<<3 + mulawBias) << uint(exp) - mulawBias
	if sign != 0 {
		return int16(-sample)
	}
	return int16(sample)
}

// =============================================================================
// OpenAI Realtime API data structures
// =============================================================================

type OAIGenericEvent struct {
	Type    string          `json:"type"`
	EventID string          `json:"event_id,omitempty"`
	RawData json.RawMessage `json:"-"`
}

type OAISessionUpdate struct {
	Type    string     `json:"type"`
	Session OAISession `json:"session"`
}

type OAISession struct {
	Type         string          `json:"type"`
	Model        string          `json:"model,omitempty"`
	Instructions string          `json:"instructions,omitempty"`
	Audio        *OAIAudioConfig `json:"audio,omitempty"`
	Tools        []OAITool       `json:"tools,omitempty"`
}

type OAIAudioConfig struct {
	Input  *OAIAudioInput  `json:"input,omitempty"`
	Output *OAIAudioOutput `json:"output,omitempty"`
}

type OAIAudioInput struct {
	Format        *OAIAudioFormat   `json:"format,omitempty"`
	TurnDetection *OAITurnDet       `json:"turn_detection,omitempty"`
	Transcription *OAITranscription `json:"transcription,omitempty"`
}

type OAIAudioFormat struct {
	Type string `json:"type"`
	Rate int    `json:"rate,omitempty"`
}

type OAIAudioOutput struct {
	Voice  string          `json:"voice,omitempty"`
	Format *OAIAudioFormat `json:"format,omitempty"`
}

type OAITurnDet struct {
	Type              string  `json:"type"`
	Threshold         float64 `json:"threshold,omitempty"`
	PrefixPaddingMs   int     `json:"prefix_padding_ms,omitempty"`
	SilenceDurationMs int     `json:"silence_duration_ms,omitempty"`
}

type OAITranscription struct {
	Model string `json:"model,omitempty"`
}

type OAITool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type OAIAudioAppend struct {
	Type  string `json:"type"`
	Audio string `json:"audio"`
}

type OAIResponseAudioDelta struct {
	Type    string `json:"type"`
	Delta   string `json:"delta"`
	EventID string `json:"event_id"`
}

type OAIResponseDone struct {
	Type     string      `json:"type"`
	Response OAIResponse `json:"response"`
}

type OAIResponse struct {
	ID     string      `json:"id"`
	Status string      `json:"status"`
	Output []OAIOutput `json:"output"`
}

type OAIOutput struct {
	Type    string       `json:"type"`
	Role    string       `json:"role"`
	Content []OAIContent `json:"content"`
}

type OAIContent struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	Transcript string `json:"transcript,omitempty"`
}

type OAIFunctionCallItem struct {
	Type string  `json:"type"`
	Item OAIItem `json:"item"`
}

type OAIItem struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

// =============================================================================
// OpenAI Realtime Client (with reconnection support)
// =============================================================================

type OpenAIRealtimeClient struct {
	conn    *websocket.Conn
	apiKey  string
	model   string
	logger  *zap.Logger
	mu      sync.Mutex
	closed  bool
	eventCh chan models.AIEvent

	// Reconnection state
	reconnecting   bool
	reconnectMu    sync.Mutex
	connectParams  ConnectParams
	oaiCfg         OpenAIConfig
	sessionUpdate  []byte // cached session.update payload for reconnection

	// Transcript accumulation (exposed via callbacks)
	OnUserTranscript func(text string)
	OnAITranscript   func(text string)
}

type OpenAIConfig struct {
	APIKey       string
	Model        string
	Voice        string
	Instructions string
	Language     string
}

func NewOpenAIRealtimeClient(cfg OpenAIConfig, logger *zap.Logger) *OpenAIRealtimeClient {
	if cfg.Model == "" {
		cfg.Model = "gpt-realtime"
	}
	if cfg.Voice == "" {
		cfg.Voice = "coral"
	}
	return &OpenAIRealtimeClient{
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		logger:  logger,
		eventCh: make(chan models.AIEvent, 512),
		oaiCfg:  cfg,
	}
}

// Connect establishes WSS connection to OpenAI Realtime API
func (c *OpenAIRealtimeClient) Connect(ctx context.Context, params ConnectParams, oaiCfg OpenAIConfig) error {
	c.connectParams = params
	c.oaiCfg = oaiCfg
	return c.connectInternal(ctx)
}

func (c *OpenAIRealtimeClient) connectInternal(ctx context.Context) error {
	url := fmt.Sprintf("wss://api.openai.com/v1/realtime?model=%s", c.model)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+c.apiKey)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	c.logger.Info("Connecting to OpenAI Realtime API",
		zap.String("model", c.model),
		zap.String("interaction_id", c.connectParams.InteractionID))

	conn, _, err := dialer.DialContext(ctx, url, headers)
	if err != nil {
		return fmt.Errorf("OpenAI WSS dial: %w", err)
	}

	// Wait for session.created
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("waiting for session.created: %w", err)
	}
	var created OAIGenericEvent
	json.Unmarshal(msg, &created)
	if created.Type != OAIEventSessionCreated {
		conn.Close()
		return fmt.Errorf("expected session.created, got %s", created.Type)
	}
	conn.SetReadDeadline(time.Time{})

	c.logger.Info("OpenAI session created")

	// Build instructions
	instructions := c.oaiCfg.Instructions
	if instructions == "" {
		instructions = buildDefaultInstructions(c.connectParams, c.oaiCfg.Language)
	}

	// Configure session
	sessionUpdate := OAISessionUpdate{
		Type: OAIEventSessionUpdate,
		Session: OAISession{
			Type:         "realtime",
			Instructions: instructions,
			Audio: &OAIAudioConfig{
				Input: &OAIAudioInput{
					Format: &OAIAudioFormat{Type: "audio/pcmu"},
					TurnDetection: &OAITurnDet{
						Type:              "server_vad",
						Threshold:         0.5,
						PrefixPaddingMs:   300,
						SilenceDurationMs: 500,
					},
					Transcription: &OAITranscription{
						Model: "gpt-4o-transcribe",
					},
				},
				Output: &OAIAudioOutput{
					Voice:  c.oaiCfg.Voice,
					Format: &OAIAudioFormat{Type: "audio/pcmu"},
				},
			},
			Tools: buildTransferTools(),
		},
	}

	data, _ := json.Marshal(sessionUpdate)
	c.sessionUpdate = data // Cache for reconnection

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		conn.Close()
		return fmt.Errorf("sending session.update: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.closed = false
	c.mu.Unlock()

	c.logger.Info("OpenAI session configured",
		zap.String("voice", c.oaiCfg.Voice),
		zap.String("audio_format", "audio/pcmu"),
		zap.String("notaria_id", c.connectParams.NotariaID))

	// Start read loop
	go c.readLoop(ctx)

	return nil
}

// reconnect attempts to re-establish the WSS connection with exponential backoff
func (c *OpenAIRealtimeClient) reconnect(ctx context.Context) bool {
	c.reconnectMu.Lock()
	if c.reconnecting {
		c.reconnectMu.Unlock()
		return false
	}
	c.reconnecting = true
	c.reconnectMu.Unlock()

	defer func() {
		c.reconnectMu.Lock()
		c.reconnecting = false
		c.reconnectMu.Unlock()
	}()

	for attempt := 1; attempt <= maxReconnectAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		delay := reconnectBaseDelay * time.Duration(1<<uint(attempt-1)) // 500ms, 1s, 2s
		c.logger.Info("Attempting WSS reconnection",
			zap.Int("attempt", attempt),
			zap.Duration("delay", delay))

		time.Sleep(delay)

		if err := c.connectInternal(ctx); err != nil {
			c.logger.Error("Reconnection attempt failed",
				zap.Int("attempt", attempt),
				zap.Error(err))
			continue
		}

		c.logger.Info("WSS reconnection successful", zap.Int("attempt", attempt))
		return true
	}

	c.logger.Error("All reconnection attempts failed")
	return false
}

// IsReconnecting returns true if a reconnection is in progress
func (c *OpenAIRealtimeClient) IsReconnecting() bool {
	c.reconnectMu.Lock()
	defer c.reconnectMu.Unlock()
	return c.reconnecting
}

// SendAudio converts SLIN 16-bit to mu-law and sends to OpenAI
func (c *OpenAIRealtimeClient) SendAudio(audio []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("connection closed")
	}

	mulawAudio := slinToMulaw(audio)
	b64Audio := base64.StdEncoding.EncodeToString(mulawAudio)

	event := OAIAudioAppend{
		Type:  OAIEventAudioAppend,
		Audio: b64Audio,
	}
	data, _ := json.Marshal(event)

	c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

// SendEvent translates our internal events to OpenAI format
func (c *OpenAIRealtimeClient) SendEvent(event models.AIEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("connection closed")
	}

	switch event.Event {
	case "call_ended":
		c.logger.Info("Sending call_ended to OpenAI")
		return c.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "call ended"))

	case "dtmf_received":
		item := map[string]interface{}{
			"type": "conversation.item.create",
			"item": map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "input_text", "text": fmt.Sprintf("[DTMF tone: %s]", event.Digit)},
				},
			},
		}
		data, _ := json.Marshal(item)
		return c.conn.WriteMessage(websocket.TextMessage, data)
	}

	return nil
}

// Events returns the channel of translated events
func (c *OpenAIRealtimeClient) Events() <-chan models.AIEvent {
	return c.eventCh
}

// Close closes the connection
func (c *OpenAIRealtimeClient) Close() error {
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

// readLoop reads OpenAI events and translates them to our internal format
func (c *OpenAIRealtimeClient) readLoop(ctx context.Context) {
	defer func() {
		c.mu.Lock()
		wasClosed := c.closed
		c.mu.Unlock()

		if !wasClosed {
			// Unexpected disconnect — try reconnection
			select {
			case <-ctx.Done():
				return
			default:
			}

			c.logger.Warn("OpenAI WSS disconnected unexpectedly, attempting reconnection")
			if !c.reconnect(ctx) {
				// All retries failed — signal fallback transfer
				select {
				case c.eventCh <- models.AIEvent{
					Event:  "transfer",
					Reason: "ai_reconnect_failed",
				}:
				default:
				}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				c.logger.Info("OpenAI WSS closed normally")
			} else {
				select {
				case <-ctx.Done():
					return
				default:
					c.logger.Error("OpenAI WSS read error", zap.Error(err))
				}
			}
			return
		}

		var generic OAIGenericEvent
		if err := json.Unmarshal(msg, &generic); err != nil {
			c.logger.Error("Failed to parse OpenAI event", zap.Error(err))
			continue
		}

		switch generic.Type {

		case OAIEventResponseAudioDelta:
			var audioDelta OAIResponseAudioDelta
			json.Unmarshal(msg, &audioDelta)

			mulawBytes, err := base64.StdEncoding.DecodeString(audioDelta.Delta)
			if err != nil {
				c.logger.Error("Failed to decode audio from OpenAI", zap.Error(err))
				continue
			}

			slinBytes := mulawToSlin(mulawBytes)

			select {
			case c.eventCh <- models.AIEvent{
				Event: "_audio",
				Data:  map[string]string{"_raw": string(slinBytes)},
			}:
			default:
				c.logger.Warn("Event channel full, dropping audio frame")
			}

		case OAIEventResponseAudioDone:
			select {
			case c.eventCh <- models.AIEvent{Event: "_audio_done"}:
			default:
			}

		case OAIEventResponseDone:
			var respDone OAIResponseDone
			json.Unmarshal(msg, &respDone)
			for _, output := range respDone.Response.Output {
				if output.Type == "function_call" {
					c.handleFunctionCall(msg)
				}
			}

		case OAIEventError:
			var errEvent map[string]interface{}
			json.Unmarshal(msg, &errEvent)
			c.logger.Error("OpenAI error event", zap.Any("error", errEvent))

		case OAIEventSpeechStarted:
			c.logger.Debug("User speech started (VAD)")
			select {
			case c.eventCh <- models.AIEvent{Event: "_audio_reset"}:
			default:
			}

		case OAIEventSpeechStopped:
			c.logger.Debug("User speech stopped (VAD)")

		case OAIEventInputAudioTranscription:
			var transcription map[string]interface{}
			json.Unmarshal(msg, &transcription)
			if text, ok := transcription["transcript"].(string); ok && text != "" {
				c.logger.Info("User said", zap.String("transcript", text))
				if c.OnUserTranscript != nil {
					c.OnUserTranscript(text)
				}
			}

		case OAIEventAudioTranscriptDone:
			var delta map[string]interface{}
			json.Unmarshal(msg, &delta)
			if text, ok := delta["transcript"].(string); ok && text != "" {
				c.logger.Info("AI said", zap.String("transcript", text))
				if c.OnAITranscript != nil {
					c.OnAITranscript(text)
				}
			}

		case OAIEventAudioTranscriptDelta:
			// Partial transcript, ignore (we use the Done event for full text)

		case OAIEventSessionUpdated:
			c.logger.Info("OpenAI session updated successfully")
			// Trigger initial greeting — AI speaks first without waiting for user
			greeting := map[string]string{"type": "response.create"}
			gData, _ := json.Marshal(greeting)
			c.mu.Lock()
			if !c.closed && c.conn != nil {
				c.conn.WriteMessage(websocket.TextMessage, gData)
			}
			c.mu.Unlock()
			c.logger.Info("Initial greeting triggered")

		case OAIEventResponseCreated:
			c.logger.Debug("OpenAI response started")

		default:
			c.logger.Debug("Unhandled OpenAI event", zap.String("type", generic.Type))
		}
	}
}

func (c *OpenAIRealtimeClient) handleFunctionCall(msg []byte) {
	var raw map[string]interface{}
	json.Unmarshal(msg, &raw)

	resp, _ := raw["response"].(map[string]interface{})
	outputs, _ := resp["output"].([]interface{})

	for _, out := range outputs {
		outMap, _ := out.(map[string]interface{})
		if outMap["type"] != "function_call" {
			continue
		}
		name, _ := outMap["name"].(string)
		args, _ := outMap["arguments"].(string)
		callID, _ := outMap["call_id"].(string)

		var argsMap map[string]string
		json.Unmarshal([]byte(args), &argsMap)

		c.logger.Info("OpenAI function call",
			zap.String("function", name),
			zap.String("args", args))

		switch name {
		case "transfer_call":
			select {
			case c.eventCh <- models.AIEvent{
				Event:           "transfer",
				Destination:     argsMap["destination"],
				DestinationType: argsMap["destination_type"],
				NotariaID:       argsMap["notaria_id"],
				Via:             "sip_trunk",
			}:
			default:
			}

		case "hangup_call":
			select {
			case c.eventCh <- models.AIEvent{
				Event:  "hangup",
				Reason: argsMap["reason"],
			}:
			default:
			}
		}

		c.sendFunctionResult(callID, name, "ok")
	}
}

func (c *OpenAIRealtimeClient) sendFunctionResult(callID, funcName, result string) {
	item := OAIFunctionCallItem{
		Type: "conversation.item.create",
		Item: OAIItem{
			Type:   "function_call_output",
			CallID: callID,
			Output: fmt.Sprintf(`{"status": "%s"}`, result),
		},
	}
	data, _ := json.Marshal(item)
	c.conn.WriteMessage(websocket.TextMessage, data)

	trigger := map[string]string{"type": "response.create"}
	triggerData, _ := json.Marshal(trigger)
	c.conn.WriteMessage(websocket.TextMessage, triggerData)
}

func buildDefaultInstructions(params ConnectParams, language string) string {
	lang := "Spanish"
	if language != "" && language != "es" {
		lang = language
	}

	return fmt.Sprintf(`You are a professional virtual assistant for a Spanish notary office.

CONTEXT:
- Notaria ID: %s
- Caller phone: %s
- Call type: %s
- Schedule: %s

BEHAVIOR:
- Always speak in %s
- Be polite, professional and concise
- You can help with: appointment scheduling, document status inquiries, general information about notarial services, office hours and directions
- If the caller needs to speak with a specific person or you cannot resolve their query, use the transfer_call function
- If the conversation is resolved, say goodbye politely and use the hangup_call function
- Never invent information about specific documents or cases

IMPORTANT:
- Keep responses short (1-3 sentences) since this is a phone call
- Speak naturally as in a phone conversation
- Start by greeting and asking how you can help`, params.NotariaID, params.CallerID, params.CallType, params.Schedule, lang)
}

func buildTransferTools() []OAITool {
	transferParams, _ := json.Marshal(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"destination": map[string]interface{}{
				"type":        "string",
				"description": "Extension number, phone number, or group to transfer to",
			},
			"destination_type": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"extension", "number", "group"},
				"description": "Type of destination",
			},
			"notaria_id": map[string]interface{}{
				"type":        "string",
				"description": "ID of the notaria to transfer to",
			},
			"reason": map[string]interface{}{
				"type":        "string",
				"description": "Brief reason for the transfer",
			},
		},
		"required": []string{"destination", "reason"},
	})

	hangupParams, _ := json.Marshal(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"reason": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"resolved", "caller_request", "no_match"},
				"description": "Reason for ending the call",
			},
		},
		"required": []string{"reason"},
	})

	return []OAITool{
		{
			Type:        "function",
			Name:        "transfer_call",
			Description: "Transfer the call to a specific person, extension, or department. Use when the caller needs human assistance or you cannot resolve their query.",
			Parameters:  transferParams,
		},
		{
			Type:        "function",
			Name:        "hangup_call",
			Description: "End the call after the conversation is resolved and the caller has been helped. Always say goodbye before calling this function.",
			Parameters:  hangupParams,
		},
	}
}
