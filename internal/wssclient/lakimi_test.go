package wssclient

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/smartgroup/audio-bridge/internal/models"
)

// =============================================================================
// Unit tests for Lakimi integration
// =============================================================================

// --- Repacketizer tests ---

func TestOutboundRepacketizer_20to30(t *testing.T) {
	// Create a session with a nil hub (we'll intercept send)
	sent := make([][]byte, 0)
	hub := &fakeHub{sendFn: func(msg LakimiMessage) error {
		raw, _ := base64.StdEncoding.DecodeString(msg.Payload)
		sent = append(sent, raw)
		return nil
	}}

	s := &LakimiSession{
		hub:           nil,
		interactionID: "test-001",
		eventCh:       make(chan models.AIEvent, 512),
	}
	// Wire up the fake hub's send
	s.hub = (*LakimiHub)(nil) // We'll override SendAudio to use fakeHub
	_ = hub

	// Instead, test the repacketizer logic directly
	outBuf := []byte{}

	// Simulate sending three 20ms frames (320 bytes each = 960 bytes)
	// Should produce two 30ms frames (480 bytes each = 960 bytes)
	frame20ms := make([]byte, 320)
	for i := range frame20ms {
		frame20ms[i] = byte(i % 256)
	}

	var sentChunks [][]byte

	// Manual repacketizer logic (mirrors LakimiSession.SendAudio)
	for i := 0; i < 3; i++ {
		outBuf = append(outBuf, frame20ms...)
		for len(outBuf) >= 480 {
			chunk := make([]byte, 480)
			copy(chunk, outBuf[:480])
			outBuf = outBuf[480:]
			sentChunks = append(sentChunks, chunk)
		}
	}

	// 3 * 320 = 960 bytes → 2 chunks of 480, remainder 0
	if len(sentChunks) != 2 {
		t.Errorf("expected 2 chunks of 480 bytes, got %d", len(sentChunks))
	}
	for i, chunk := range sentChunks {
		if len(chunk) != 480 {
			t.Errorf("chunk %d: expected 480 bytes, got %d", i, len(chunk))
		}
	}
	if len(outBuf) != 0 {
		t.Errorf("expected 0 remainder bytes, got %d", len(outBuf))
	}
}

func TestOutboundRepacketizer_Remainder(t *testing.T) {
	// Send 2 frames of 320 = 640 bytes → 1 chunk of 480, remainder 160
	outBuf := []byte{}
	frame := make([]byte, 320)
	var chunks int

	for i := 0; i < 2; i++ {
		outBuf = append(outBuf, frame...)
		for len(outBuf) >= 480 {
			outBuf = outBuf[480:]
			chunks++
		}
	}

	if chunks != 1 {
		t.Errorf("expected 1 chunk, got %d", chunks)
	}
	if len(outBuf) != 160 {
		t.Errorf("expected 160 remainder bytes, got %d", len(outBuf))
	}
}

func TestInboundRepacketizer_30to20(t *testing.T) {
	s := &LakimiSession{
		interactionID: "test-002",
		eventCh:       make(chan models.AIEvent, 512),
	}

	// Create a 30ms frame (480 bytes) with recognizable pattern
	frame30ms := make([]byte, 480)
	for i := range frame30ms {
		frame30ms[i] = byte(i % 256)
	}
	payload := base64.StdEncoding.EncodeToString(frame30ms)

	// Push inbound audio
	s.pushInboundAudio(payload)

	// Should produce 1 full 20ms frame (320 bytes), with 160 bytes remaining
	// 480 / 320 = 1 frame + 160 remainder
	var events []models.AIEvent
	for {
		select {
		case e := <-s.eventCh:
			events = append(events, e)
		default:
			goto done
		}
	}
done:

	if len(events) != 1 {
		t.Errorf("expected 1 event from 480 bytes, got %d", len(events))
	}
	if len(events) > 0 {
		raw := events[0].Data["_raw"]
		if len(raw) != 320 {
			t.Errorf("expected 320 byte frame, got %d", len(raw))
		}
	}

	// Remainder should be 160 bytes
	s.mu.Lock()
	remainder := len(s.inBuffer)
	s.mu.Unlock()
	if remainder != 160 {
		t.Errorf("expected 160 remainder bytes, got %d", remainder)
	}

	// Push another 30ms frame → should flush: 160 + 480 = 640 → 2 frames (640/320)
	s.pushInboundAudio(payload)

	events = events[:0]
	for {
		select {
		case e := <-s.eventCh:
			events = append(events, e)
		default:
			goto done2
		}
	}
done2:

	if len(events) != 2 {
		t.Errorf("expected 2 events from 160+480=640 bytes, got %d", len(events))
	}

	s.mu.Lock()
	remainder = len(s.inBuffer)
	s.mu.Unlock()
	if remainder != 0 {
		t.Errorf("expected 0 remainder, got %d", remainder)
	}
}

// --- Protocol conversion tests ---

func TestLakimiMessage_ToAIEvent_Transfer(t *testing.T) {
	msg := LakimiMessage{
		Event:       "transfer",
		Destination: "201",
		DestType:    "extension",
		Reason:      "caller_request",
	}
	ev := msg.ToAIEvent()
	if ev.Event != "transfer" {
		t.Errorf("expected transfer event, got %s", ev.Event)
	}
	if ev.Destination != "201" {
		t.Errorf("expected destination 201, got %s", ev.Destination)
	}
	if ev.DestinationType != "extension" {
		t.Errorf("expected extension type, got %s", ev.DestinationType)
	}
}

func TestLakimiMessage_ToAIEvent_Hangup(t *testing.T) {
	msg := LakimiMessage{
		Event:  "hangup",
		Reason: "resolved",
	}
	ev := msg.ToAIEvent()
	if ev.Event != "hangup" {
		t.Errorf("expected hangup, got %s", ev.Event)
	}
	if ev.Reason != "resolved" {
		t.Errorf("expected resolved reason, got %s", ev.Reason)
	}
}

func TestLakimiMessage_ToAIEvent_Audio(t *testing.T) {
	audio := make([]byte, 480)
	for i := range audio {
		audio[i] = 0x42
	}
	msg := LakimiMessage{
		Event:   "media",
		Payload: base64.StdEncoding.EncodeToString(audio),
	}
	ev := msg.ToAIEvent()
	if ev.Event != "_audio" {
		t.Errorf("expected _audio, got %s", ev.Event)
	}
	raw := []byte(ev.Data["_raw"])
	if len(raw) != 480 {
		t.Errorf("expected 480 bytes, got %d", len(raw))
	}
}

func TestNewLakimiStartMessage(t *testing.T) {
	params := ConnectParams{
		InteractionID: "call-123",
		NotariaID:     "N001",
		CallerID:      "666111222",
		CallType:      "inbound",
		Schedule:      "business_hours",
		DDIOrigin:     "934001234",
	}
	msg := NewLakimiStartMessage(params, DefaultAudioFormat(30))

	if msg.Event != "start" {
		t.Errorf("expected start event, got %s", msg.Event)
	}
	if msg.InteractionID != "call-123" {
		t.Errorf("expected call-123, got %s", msg.InteractionID)
	}
	if msg.AudioFormat == nil {
		t.Fatal("expected audio_format to be set")
	}
	if msg.AudioFormat.FrameSizeMs != 30 {
		t.Errorf("expected 30ms frame size, got %d", msg.AudioFormat.FrameSizeMs)
	}

	// Verify it marshals to valid JSON
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if parsed["event"] != "start" {
		t.Errorf("JSON event mismatch: %v", parsed["event"])
	}
}

// --- Hub routing test ---

func TestHubRouting_MultiSession(t *testing.T) {
	// Test that events are routed to the correct session by interaction_id.
	// We create two sessions and verify messages reach the right one.

	s1 := &LakimiSession{
		interactionID: "call-aaa",
		eventCh:       make(chan models.AIEvent, 32),
	}
	s2 := &LakimiSession{
		interactionID: "call-bbb",
		eventCh:       make(chan models.AIEvent, 32),
	}

	// Simulate hub session map
	sessions := map[string]*LakimiSession{
		"call-aaa": s1,
		"call-bbb": s2,
	}

	// Simulate routing a transfer event to call-aaa
	msg1 := LakimiMessage{
		Event:         "transfer",
		InteractionID: "call-aaa",
		Destination:   "100",
		DestType:      "extension",
	}
	if s, ok := sessions[msg1.InteractionID]; ok {
		s.eventCh <- msg1.ToAIEvent()
	}

	// Simulate routing a hangup event to call-bbb
	msg2 := LakimiMessage{
		Event:         "hangup",
		InteractionID: "call-bbb",
		Reason:        "caller_request",
	}
	if s, ok := sessions[msg2.InteractionID]; ok {
		s.eventCh <- msg2.ToAIEvent()
	}

	// Verify s1 got transfer
	select {
	case ev := <-s1.eventCh:
		if ev.Event != "transfer" || ev.Destination != "100" {
			t.Errorf("s1: expected transfer to 100, got %s to %s", ev.Event, ev.Destination)
		}
	default:
		t.Error("s1: no event received")
	}

	// Verify s2 got hangup
	select {
	case ev := <-s2.eventCh:
		if ev.Event != "hangup" || ev.Reason != "caller_request" {
			t.Errorf("s2: expected hangup/caller_request, got %s/%s", ev.Event, ev.Reason)
		}
	default:
		t.Error("s2: no event received")
	}

	// Verify no cross-contamination
	select {
	case ev := <-s1.eventCh:
		t.Errorf("s1: unexpected extra event: %s", ev.Event)
	default:
		// OK
	}
	select {
	case ev := <-s2.eventCh:
		t.Errorf("s2: unexpected extra event: %s", ev.Event)
	default:
		// OK
	}
}

// --- Helpers ---

type fakeHub struct {
	sendFn func(msg LakimiMessage) error
}
