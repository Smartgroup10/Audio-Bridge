package wssclient

import "github.com/smartgroup/audio-bridge/internal/models"

// AIClient is the interface that both the generic WSS client and the OpenAI
// Realtime adapter implement. The bridge uses this to abstract the AI backend.
type AIClient interface {
	SendAudio(audio []byte) error
	SendEvent(event models.AIEvent) error
	Events() <-chan models.AIEvent
	Close() error
}
