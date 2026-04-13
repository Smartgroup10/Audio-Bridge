package bridge

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/smartgroup/audio-bridge/internal/ami"
	"github.com/smartgroup/audio-bridge/internal/audiosocket"
	"github.com/smartgroup/audio-bridge/internal/config"
	"github.com/smartgroup/audio-bridge/internal/db"
	"github.com/smartgroup/audio-bridge/internal/models"
	"github.com/smartgroup/audio-bridge/internal/recording"
	"github.com/smartgroup/audio-bridge/internal/webhook"
	"github.com/smartgroup/audio-bridge/internal/wssclient"
)

// Audio playback constants (8kHz, 16-bit mono SLIN)
const (
	slinFrameSize  = 320 // 20ms at 8kHz, 16-bit = 160 samples * 2 bytes
	frameDuration  = 20 * time.Millisecond
	preBufferBytes = slinFrameSize * 3 // 60ms pre-buffer before starting playback
	maxBufferSize  = 512 * 1024        // 512KB max buffer (~32 seconds of audio at 8kHz 16-bit)
)

// audioBuffer is a thread-safe FIFO for TTS audio with jitter buffering
type audioBuffer struct {
	mu       sync.Mutex
	buf      []byte
	started  bool
	finished bool
}

func (ab *audioBuffer) Write(data []byte) {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	if len(ab.buf)+len(data) > maxBufferSize {
		// Drop oldest data to make room
		overflow := len(ab.buf) + len(data) - maxBufferSize
		if overflow > len(ab.buf) {
			ab.buf = ab.buf[:0]
		} else {
			ab.buf = ab.buf[overflow:]
		}
	}
	ab.buf = append(ab.buf, data...)
}

func (ab *audioBuffer) ReadFrame() []byte {
	ab.mu.Lock()
	defer ab.mu.Unlock()

	if !ab.started {
		if len(ab.buf) < preBufferBytes {
			return nil
		}
		ab.started = true
	}

	if len(ab.buf) >= slinFrameSize {
		frame := make([]byte, slinFrameSize)
		copy(frame, ab.buf[:slinFrameSize])
		ab.buf = ab.buf[slinFrameSize:]
		return frame
	}

	if ab.finished {
		if len(ab.buf) > 0 {
			frame := make([]byte, slinFrameSize)
			copy(frame, ab.buf)
			ab.buf = nil
			return frame
		}
		return nil
	}

	return make([]byte, slinFrameSize)
}

func (ab *audioBuffer) MarkFinished() {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	ab.finished = true
}

func (ab *audioBuffer) IsStarted() bool {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	return ab.started
}

func (ab *audioBuffer) Reset() {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	ab.buf = ab.buf[:0]
	ab.started = false
	ab.finished = false
}

// Bridge orchestrates the audio flow between Asterisk and the AI module
type Bridge struct {
	cfg       *config.Config
	tenants   *config.TenantRegistry
	calls     *models.CallRegistry
	ami       *ami.Client
	db        *db.DB
	sseHub    *models.SSEHub
	webhook   *webhook.Client
	lakimiHub *wssclient.LakimiHub
	logger    *zap.Logger
}

func New(cfg *config.Config, tenants *config.TenantRegistry, calls *models.CallRegistry, amiClient *ami.Client, database *db.DB, sseHub *models.SSEHub, webhookClient *webhook.Client, lakimiHub *wssclient.LakimiHub, logger *zap.Logger) *Bridge {
	return &Bridge{
		cfg:       cfg,
		tenants:   tenants,
		calls:     calls,
		ami:       amiClient,
		db:        database,
		sseHub:    sseHub,
		webhook:   webhookClient,
		lakimiHub: lakimiHub,
		logger:    logger,
	}
}

// AMI returns the AMI client for dialplan provisioning
func (b *Bridge) AMI() *ami.Client {
	return b.ami
}

// SSEHub returns the SSE hub for the API server
func (b *Bridge) SSEHub() *models.SSEHub {
	return b.sseHub
}

// DB returns the database for the API server
func (b *Bridge) Database() *db.DB {
	return b.db
}

// Calls returns the call registry for the API server
func (b *Bridge) Calls() *models.CallRegistry {
	return b.calls
}

// TransferCall transfers an active call to a destination via AMI.
// Can be called from processAIMessages (AI-initiated) or from the HTTP API (Lakimi-initiated).
func (b *Bridge) TransferCall(callID, destination, destType string) error {
	call, ok := b.calls.Get(callID)
	if !ok {
		return fmt.Errorf("call %s not found", callID)
	}

	call.SetState(models.CallStateTransfer)
	call.TransferDest = destination

	// Log transfer event
	if b.db != nil {
		b.db.InsertLog(db.InteractionLog{
			CallID:    callID,
			Timestamp: time.Now().Format(time.RFC3339),
			Direction: "ai",
			Content:   fmt.Sprintf("Transfer to %s (%s)", destination, destType),
			EventType: "transfer",
		})
	}

	// SSE: broadcast transfer
	b.sseHub.Broadcast(models.SSEEvent{
		Type: "call_transfer",
		Data: map[string]interface{}{
			"call_id":     callID,
			"destination": destination,
			"dest_type":   destType,
		},
	})

	if call.AsteriskChannel == "" {
		return fmt.Errorf("call %s has no Asterisk channel", callID)
	}

	transferCtx := "from-users"
	if err := b.ami.Transfer(call.AsteriskChannel, destination, transferCtx); err != nil {
		b.logger.Error("Transfer failed",
			zap.String("call_id", callID),
			zap.String("destination", destination),
			zap.Error(err))
		return fmt.Errorf("AMI transfer failed: %w", err)
	}

	call.Complete("transfer")
	b.logger.Info("Call transferred",
		zap.String("call_id", callID),
		zap.String("destination", destination))
	return nil
}

// HangupCall hangs up an active call via AMI.
// Can be called from processAIMessages (AI-initiated) or from the HTTP API (Lakimi-initiated).
func (b *Bridge) HangupCall(callID, reason string) error {
	call, ok := b.calls.Get(callID)
	if !ok {
		return fmt.Errorf("call %s not found", callID)
	}

	if call.AsteriskChannel != "" {
		b.ami.Hangup(call.AsteriskChannel)
	}

	call.Complete(reason)

	if b.db != nil {
		b.db.InsertLog(db.InteractionLog{
			CallID:    callID,
			Timestamp: time.Now().Format(time.RFC3339),
			Direction: "ai",
			Content:   fmt.Sprintf("Hangup: %s", reason),
			EventType: "hangup",
		})
	}

	b.logger.Info("Call hung up",
		zap.String("call_id", callID),
		zap.String("reason", reason))
	return nil
}

// HandleAudioSocket is called for each new AudioSocket connection from Asterisk
func (b *Bridge) HandleAudioSocket(ctx context.Context, asConn *audiosocket.Connection) {
	callID := asConn.UUID()
	logger := b.logger.With(zap.String("call_id", callID))

	// Global call timeout to prevent zombie calls
	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Minute)
	defer cancelTimeout()

	if b.calls.ActiveCount() >= b.cfg.Server.MaxConcurrent {
		logger.Warn("Max concurrent calls reached, rejecting")
		asConn.Close()
		return
	}

	call, exists := b.calls.Get(callID)
	if !exists {
		call = &models.Call{
			ID:        callID,
			Direction: models.CallInbound,
			State:     models.CallStateConnected,
			StartTime: time.Now(),
			CallType:  "inbound",
		}
		b.calls.Add(call)
	}

	now := time.Now()
	call.AnswerTime = &now
	call.SetState(models.CallStateStreaming)
	logger.Info("Call streaming started", zap.String("notaria_id", call.NotariaID))

	// SSE: broadcast call_started
	b.sseHub.Broadcast(models.SSEEvent{
		Type: "call_started",
		Data: map[string]interface{}{
			"call_id":    callID,
			"caller_id":  call.CallerID,
			"notaria_id": call.NotariaID,
			"direction":  call.Direction,
			"start_time": call.StartTime.Format(time.RFC3339),
		},
	})

	// Recording setup
	var rec *recording.Recorder
	if b.cfg.Recording.Enabled {
		var err error
		rec, err = recording.NewRecorder(b.cfg.Recording.Path, callID, logger.Named("rec"))
		if err != nil {
			logger.Error("Failed to create recorder, continuing without recording", zap.Error(err))
			rec = nil
		}
	}

	defer func() {
		if call.GetState() != models.CallStateCompleted {
			call.Complete("normal")
		}

		// Close recorder and store paths
		if rec != nil {
			callerPath, aiPath := rec.Close()
			call.RecordingCaller = callerPath
			call.RecordingAI = aiPath
		}

		// Persist call to DB
		b.persistCall(call)

		// SSE: broadcast call_ended
		b.sseHub.Broadcast(models.SSEEvent{
			Type: "call_ended",
			Data: map[string]interface{}{
				"call_id":    callID,
				"end_reason": call.EndReason,
				"duration":   call.Duration,
			},
		})

		go func() {
			time.Sleep(5 * time.Minute)
			b.calls.Remove(callID)
		}()
		logger.Info("Call session ended",
			zap.String("end_reason", call.EndReason),
			zap.Float64("duration", call.Duration))
	}()

	schedule := "business_hours"
	call.Schedule = schedule

	tenant, hasTenant := b.tenants.LookupByID(call.NotariaID)
	if !hasTenant && call.DDI != "" {
		tenant, hasTenant = b.tenants.LookupByDDI(call.DDI)
		if hasTenant {
			call.NotariaID = tenant.NotariaID
		}
	}

	connectParams := wssclient.ConnectParams{
		NotariaID:     call.NotariaID,
		CallerID:      call.CallerID,
		InteractionID: callID,
		CallType:      call.CallType,
		Schedule:      schedule,
		DDIOrigin:     call.DDI,
		ContextID:     call.ContextID,
		ContextData:   call.ContextData,
	}

	var aiClient wssclient.AIClient
	switch b.cfg.AI.Type {
	case "openai":
		oaiCfg := wssclient.OpenAIConfig{
			APIKey:       b.cfg.AI.APIKey,
			Model:        b.cfg.AI.Model,
			Voice:        b.cfg.AI.Voice,
			Instructions: b.cfg.AI.Instructions,
			Language:     b.cfg.AI.Language,
		}
		// Per-tenant AI overrides
		if hasTenant {
			if tenant.Voice != "" {
				oaiCfg.Voice = tenant.Voice
			}
			if tenant.Language != "" {
				oaiCfg.Language = tenant.Language
			}
			if tenant.Instructions != "" {
				oaiCfg.Instructions = tenant.Instructions
			}
		}
		oaiClient := wssclient.NewOpenAIRealtimeClient(oaiCfg, logger)

		// Set transcript callbacks for DB + SSE
		oaiClient.OnUserTranscript = func(text string) {
			call.AppendTranscriptUser(text)
			b.sseHub.Broadcast(models.SSEEvent{
				Type: "transcript_user",
				Data: map[string]interface{}{
					"call_id": callID,
					"text":    text,
				},
			})
			if b.db != nil {
				b.db.InsertLog(db.InteractionLog{
					CallID:    callID,
					Timestamp: time.Now().Format(time.RFC3339),
					Direction: "user",
					Content:   text,
					EventType: "speech",
				})
			}
		}
		oaiClient.OnAITranscript = func(text string) {
			call.AppendTranscriptAI(text)
			b.sseHub.Broadcast(models.SSEEvent{
				Type: "transcript_ai",
				Data: map[string]interface{}{
					"call_id": callID,
					"text":    text,
				},
			})
			if b.db != nil {
				b.db.InsertLog(db.InteractionLog{
					CallID:    callID,
					Timestamp: time.Now().Format(time.RFC3339),
					Direction: "ai",
					Content:   text,
					EventType: "speech",
				})
			}
		}

		if err := oaiClient.Connect(ctx, connectParams, oaiCfg); err != nil {
			logger.Error("Failed to connect to OpenAI Realtime", zap.Error(err))
			b.fallbackTransfer(call, tenant, logger)
			asConn.Close()
			return
		}
		aiClient = oaiClient

	case "lakimi":
		if b.lakimiHub == nil {
			logger.Error("Lakimi hub not initialized")
			b.fallbackTransfer(call, tenant, logger)
			asConn.Close()
			return
		}
		session := b.lakimiHub.NewSession(callID, connectParams)

		// Set transcript callbacks (same pattern as OpenAI)
		session.OnUserTranscript = func(text string) {
			call.AppendTranscriptUser(text)
			b.sseHub.Broadcast(models.SSEEvent{
				Type: "transcript_user",
				Data: map[string]interface{}{
					"call_id": callID,
					"text":    text,
				},
			})
			if b.db != nil {
				b.db.InsertLog(db.InteractionLog{
					CallID:    callID,
					Timestamp: time.Now().Format(time.RFC3339),
					Direction: "user",
					Content:   text,
					EventType: "speech",
				})
			}
		}
		session.OnAITranscript = func(text string) {
			call.AppendTranscriptAI(text)
			b.sseHub.Broadcast(models.SSEEvent{
				Type: "transcript_ai",
				Data: map[string]interface{}{
					"call_id": callID,
					"text":    text,
				},
			})
			if b.db != nil {
				b.db.InsertLog(db.InteractionLog{
					CallID:    callID,
					Timestamp: time.Now().Format(time.RFC3339),
					Direction: "ai",
					Content:   text,
					EventType: "speech",
				})
			}
		}
		aiClient = session

	default:
		wssClient := wssclient.NewClient(b.cfg.AI, logger)
		if err := wssClient.Connect(ctx, connectParams); err != nil {
			logger.Error("Failed to connect to AI module", zap.Error(err))
			b.fallbackTransfer(call, tenant, logger)
			asConn.Close()
			return
		}
		aiClient = wssClient
	}
	defer aiClient.Close()

	callCtx, cancelCall := context.WithCancel(ctx)
	defer cancelCall()

	abuf := &audioBuffer{}

	// Goroutine 1: AudioSocket -> AI (caller audio to AI)
	go b.streamCallerToAI(callCtx, cancelCall, asConn, aiClient, rec, logger)

	// Goroutine 2: Jitter buffer -> AudioSocket (constant-rate playback)
	go b.playbackLoop(callCtx, asConn, abuf, logger)

	// Main: AI events -> buffer/control actions
	b.processAIMessages(callCtx, cancelCall, asConn, aiClient, abuf, call, tenant, rec, logger)
}

// streamCallerToAI reads audio from AudioSocket and sends to AI via WSS
func (b *Bridge) streamCallerToAI(ctx context.Context, cancel context.CancelFunc, asConn *audiosocket.Connection, wss wssclient.AIClient, rec *recording.Recorder, logger *zap.Logger) {
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Panic in streamCallerToAI", zap.Any("panic", r))
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		audio, err := asConn.ReadAudio()
		if err != nil {
			if err == io.EOF {
				logger.Info("Caller hung up (AudioSocket EOF)")
				wss.SendEvent(models.AIEvent{
					Event:  "call_ended",
					Reason: "caller_hangup",
				})
			} else {
				logger.Error("AudioSocket read error", zap.Error(err))
			}
			return
		}

		// Record caller audio
		if rec != nil {
			rec.WriteCallerAudio(audio)
		}

		if err := wss.SendAudio(audio); err != nil {
			// Check if WSS is reconnecting (OpenAI adapter)
			if oai, ok := wss.(*wssclient.OpenAIRealtimeClient); ok && oai.IsReconnecting() {
				logger.Debug("WSS reconnecting, buffering caller audio")
				time.Sleep(100 * time.Millisecond)
				continue
			}
			logger.Error("Failed to send audio to AI", zap.Error(err))
			return
		}
	}
}

// playbackLoop drains the jitter buffer at a constant 20ms rate and writes to AudioSocket
func (b *Bridge) playbackLoop(ctx context.Context, asConn *audiosocket.Connection, abuf *audioBuffer, logger *zap.Logger) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Panic in playbackLoop", zap.Any("panic", r))
		}
	}()
	ticker := time.NewTicker(frameDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			frame := abuf.ReadFrame()
			if frame == nil {
				continue
			}
			if err := asConn.WriteAudio(frame); err != nil {
				logger.Error("Failed to write audio frame to AudioSocket", zap.Error(err))
				return
			}
		}
	}
}

// processAIMessages reads events from the AI and dispatches audio to the buffer
func (b *Bridge) processAIMessages(ctx context.Context, cancel context.CancelFunc, asConn *audiosocket.Connection, wss wssclient.AIClient, abuf *audioBuffer, call *models.Call, tenant *config.TenantConfig, rec *recording.Recorder, logger *zap.Logger) {
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Panic in processAIMessages", zap.Any("panic", r))
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-wss.Events():
			if !ok {
				logger.Info("AI WSS event channel closed")
				return
			}

			switch event.Event {
			case "_audio":
				if rawData, ok := event.Data["_raw"]; ok {
					audioBytes := []byte(rawData)
					abuf.Write(audioBytes)
					// Record AI audio
					if rec != nil {
						rec.WriteAIAudio(audioBytes)
					}
				}

			case "_audio_done":
				abuf.MarkFinished()

			case "_audio_reset":
				abuf.Reset()

			case "transfer":
				logger.Info("AI requested transfer",
					zap.String("destination", event.Destination),
					zap.String("type", event.DestinationType),
					zap.String("via", event.Via))

				if err := b.TransferCall(call.ID, event.Destination, event.DestinationType); err != nil {
					logger.Error("Transfer failed", zap.Error(err))
					wss.SendEvent(models.AIEvent{
						Event:  "transfer_completed",
						Status: "failed",
					})
				} else {
					wss.SendEvent(models.AIEvent{
						Event:       "transfer_completed",
						Destination: event.Destination,
						Status:      "connected",
					})
				}
				return

			case "hangup":
				logger.Info("AI requested hangup", zap.String("reason", event.Reason))
				b.HangupCall(call.ID, event.Reason)
				asConn.Close()
				return

			case "hold":
				logger.Info("AI requested hold", zap.String("action", event.Action))

			default:
				logger.Warn("Unknown AI event", zap.String("event", event.Event))
			}
		}
	}
}

// persistCall saves the call data to the database
func (b *Bridge) persistCall(call *models.Call) {
	if b.db == nil {
		return
	}

	var answerTime, endTime *string
	if call.AnswerTime != nil {
		t := call.AnswerTime.Format(time.RFC3339)
		answerTime = &t
	}
	if call.EndTime != nil {
		t := call.EndTime.Format(time.RFC3339)
		endTime = &t
	}

	record := db.CallRecord{
		ID:              call.ID,
		CallerID:        call.CallerID,
		DDI:             call.DDI,
		NotariaID:       call.NotariaID,
		Direction:       string(call.Direction),
		State:           string(call.GetState()),
		CallType:        call.CallType,
		Schedule:        call.Schedule,
		StartTime:       call.StartTime.Format(time.RFC3339),
		AnswerTime:      answerTime,
		EndTime:         endTime,
		DurationSeconds: call.Duration,
		EndReason:       call.EndReason,
		TransferDest:    call.TransferDest,
		AsteriskChannel: call.AsteriskChannel,
		RecordingCaller: call.RecordingCaller,
		RecordingAI:     call.RecordingAI,
		TranscriptUser:  call.TranscriptUser,
		TranscriptAI:    call.TranscriptAI,
	}

	if err := b.db.InsertCall(record); err != nil {
		b.logger.Error("Failed to persist call to DB", zap.Error(err), zap.String("call_id", call.ID))
	} else {
		b.logger.Debug("Call persisted to DB", zap.String("call_id", call.ID))
		// Post-call processing: MP3 conversion + webhook (async, non-blocking)
		go b.postCallProcessing(call)
	}
}

// postCallProcessing converts recordings to MP3 and sends webhook notification to CTN
func (b *Bridge) postCallProcessing(call *models.Call) {
	defer func() {
		if r := recover(); r != nil {
			b.logger.Error("Panic in postCallProcessing", zap.Any("panic", r))
		}
	}()

	logger := b.logger.With(zap.String("call_id", call.ID))

	// 1. Convert WAV → MP3
	if call.RecordingCaller != "" || call.RecordingAI != "" {
		mp3Paths, err := recording.ConvertCallRecordings(
			call.RecordingCaller, call.RecordingAI,
			call.NotariaID, call.CallerID, call.ID,
			logger.Named("mp3"),
		)
		if err != nil {
			logger.Warn("MP3 conversion failed", zap.Error(err))
		} else {
			// Update model
			call.RecordingCallerMP3 = mp3Paths.CallerMP3
			call.RecordingAIMP3 = mp3Paths.AIMP3
			call.RecordingMixedMP3 = mp3Paths.MixedMP3

			// Update DB
			if b.db != nil {
				if err := b.db.UpdateCallMP3(call.ID, mp3Paths.CallerMP3, mp3Paths.AIMP3, mp3Paths.MixedMP3); err != nil {
					logger.Error("Failed to update MP3 paths in DB", zap.Error(err))
				}
			}
		}
	}

	// 2. Send webhook call.completed
	if b.webhook != nil {
		recordingURL := webhook.BuildRecordingURL(b.cfg.API.PublicURL, call.ID)

		b.webhook.Send(webhook.Payload{
			Event:         webhook.EventCallCompleted,
			InteractionID: call.ID,
			NotariaID:     call.NotariaID,
			CallerID:      call.CallerID,
			DDI:           call.DDI,
			Direction:     string(call.Direction),
			Duration:      call.Duration,
			Result:        call.EndReason,
			TransferDest:  call.TransferDest,
			RecordingURL:  recordingURL,
			Schedule:      call.Schedule,
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// fallbackTransfer transfers the call directly to the notary when AI is unavailable
func (b *Bridge) fallbackTransfer(call *models.Call, tenant *config.TenantConfig, logger *zap.Logger) {
	if tenant == nil || call.AsteriskChannel == "" {
		logger.Warn("Cannot fallback transfer: no tenant or channel info")
		call.Complete("error_no_fallback")
		return
	}

	dest := tenant.Transfers.Default
	if dest == "" {
		logger.Warn("No default transfer destination for tenant", zap.String("notaria_id", tenant.NotariaID))
		call.Complete("error_no_fallback")
		return
	}

	logger.Info("Executing fallback transfer to notary",
		zap.String("destination", dest),
		zap.String("notaria_id", tenant.NotariaID))

	if err := b.ami.Transfer(call.AsteriskChannel, dest, "from-internal"); err != nil {
		logger.Error("Fallback transfer failed", zap.Error(err))
		call.Complete("error_fallback_failed")
		return
	}
	call.Complete("fallback_transfer")
}

// OriginateOutbound creates an outbound call and connects it to the AI
func (b *Bridge) OriginateOutbound(req models.OutboundRequest) (*models.Call, error) {
	callID := uuid.New().String()

	call := &models.Call{
		ID:          callID,
		CallerID:    req.Destination,
		NotariaID:   req.NotariaID,
		Direction:   models.CallOutbound,
		State:       models.CallStateRinging,
		CallType:    req.CallType,
		ContextID:   req.ContextID,
		ContextData: req.ContextData,
		StartTime:   time.Now(),
	}
	if call.CallType == "" {
		call.CallType = "callback"
	}
	b.calls.Add(call)

	variables := map[string]string{
		"NOTARIA_ID": req.NotariaID,
		"CALL_TYPE":  call.CallType,
		"CONTEXT_ID": req.ContextID,
		"CALL_UUID":  callID,
	}

	bridgeAddr := b.cfg.Server.AudioSocketAddr
	callerID := fmt.Sprintf("Notaria <%s>", req.NotariaID)

	// Use trunk from tenant config if available
	sipTrunk := ""
	if tenant, ok := b.tenants.LookupByID(req.NotariaID); ok {
		sipTrunk = tenant.SIPTrunk
	}

	err := b.ami.OriginateWithRetry(
		req.Destination, callerID, callID, bridgeAddr, sipTrunk, variables,
		b.cfg.AI.OriginateRetries, b.cfg.AI.OriginateRetryIntervalSec,
	)
	if err != nil {
		call.Complete("originate_failed")
		return call, fmt.Errorf("originate failed: %w", err)
	}

	b.logger.Info("Outbound call originated",
		zap.String("call_id", callID),
		zap.String("destination", req.Destination),
		zap.String("notaria_id", req.NotariaID))

	return call, nil
}
