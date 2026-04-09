package recording

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// WAV format constants for 8kHz, 16-bit, mono (SLIN)
const (
	wavSampleRate  = 8000
	wavBitsPerSamp = 16
	wavChannels    = 1
	wavHeaderSize  = 44
)

// Recorder writes PCM audio to WAV files (thread-safe)
type Recorder struct {
	mu         sync.Mutex
	callerFile *os.File
	aiFile     *os.File
	callerPath string
	aiPath     string
	callerSize uint32
	aiSize     uint32
	closed     bool
	logger     *zap.Logger
}

// NewRecorder creates a recorder that writes separate WAV files for caller and AI audio.
// Files are created in basePath/{YYYY-MM-DD}/{callID}_caller.wav and _ai.wav
func NewRecorder(basePath, callID string, logger *zap.Logger) (*Recorder, error) {
	dateDir := time.Now().Format("2006-01-02")
	dir := filepath.Join(basePath, dateDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating recording dir %s: %w", dir, err)
	}

	callerPath := filepath.Join(dir, callID+"_caller.wav")
	aiPath := filepath.Join(dir, callID+"_ai.wav")

	callerFile, err := os.Create(callerPath)
	if err != nil {
		return nil, fmt.Errorf("creating caller WAV %s: %w", callerPath, err)
	}

	aiFile, err := os.Create(aiPath)
	if err != nil {
		callerFile.Close()
		return nil, fmt.Errorf("creating AI WAV %s: %w", aiPath, err)
	}

	r := &Recorder{
		callerFile: callerFile,
		aiFile:     aiFile,
		callerPath: callerPath,
		aiPath:     aiPath,
		logger:     logger,
	}

	// Write placeholder WAV headers (will be updated on Close)
	if err := r.writeWAVHeader(callerFile, 0); err != nil {
		r.cleanup()
		return nil, err
	}
	if err := r.writeWAVHeader(aiFile, 0); err != nil {
		r.cleanup()
		return nil, err
	}

	logger.Info("Recording started",
		zap.String("caller_wav", callerPath),
		zap.String("ai_wav", aiPath))

	return r, nil
}

// WriteCallerAudio appends PCM audio from the caller to the WAV file
func (r *Recorder) WriteCallerAudio(pcm []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.callerFile == nil {
		return
	}
	n, err := r.callerFile.Write(pcm)
	if err != nil {
		r.logger.Error("Failed to write caller audio", zap.Error(err))
		return
	}
	r.callerSize += uint32(n)
}

// WriteAIAudio appends PCM audio from the AI to the WAV file
func (r *Recorder) WriteAIAudio(pcm []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.aiFile == nil {
		return
	}
	n, err := r.aiFile.Write(pcm)
	if err != nil {
		r.logger.Error("Failed to write AI audio", zap.Error(err))
		return
	}
	r.aiSize += uint32(n)
}

// Close finalizes both WAV files (updates headers with correct sizes) and closes them
func (r *Recorder) Close() (callerPath, aiPath string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return r.callerPath, r.aiPath
	}
	r.closed = true

	// Finalize caller WAV
	if r.callerFile != nil {
		r.finalizeWAV(r.callerFile, r.callerSize)
		r.callerFile.Close()
	}

	// Finalize AI WAV
	if r.aiFile != nil {
		r.finalizeWAV(r.aiFile, r.aiSize)
		r.aiFile.Close()
	}

	r.logger.Info("Recording finalized",
		zap.String("caller_wav", r.callerPath),
		zap.Uint32("caller_bytes", r.callerSize),
		zap.String("ai_wav", r.aiPath),
		zap.Uint32("ai_bytes", r.aiSize))

	return r.callerPath, r.aiPath
}

// CallerPath returns the path to the caller WAV file
func (r *Recorder) CallerPath() string { return r.callerPath }

// AIPath returns the path to the AI WAV file
func (r *Recorder) AIPath() string { return r.aiPath }

func (r *Recorder) cleanup() {
	if r.callerFile != nil {
		r.callerFile.Close()
		os.Remove(r.callerPath)
	}
	if r.aiFile != nil {
		r.aiFile.Close()
		os.Remove(r.aiPath)
	}
}

// writeWAVHeader writes a standard RIFF/WAV header for PCM audio
// dataSize = 0 means placeholder (will be updated on close)
func (r *Recorder) writeWAVHeader(f *os.File, dataSize uint32) error {
	byteRate := wavSampleRate * wavChannels * wavBitsPerSamp / 8
	blockAlign := wavChannels * wavBitsPerSamp / 8

	header := make([]byte, wavHeaderSize)

	// RIFF chunk
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], dataSize+36) // file size - 8
	copy(header[8:12], "WAVE")

	// fmt sub-chunk
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)                   // sub-chunk size
	binary.LittleEndian.PutUint16(header[20:22], 1)                    // PCM format
	binary.LittleEndian.PutUint16(header[22:24], uint16(wavChannels))  // channels
	binary.LittleEndian.PutUint32(header[24:28], uint32(wavSampleRate))// sample rate
	binary.LittleEndian.PutUint32(header[28:32], uint32(byteRate))     // byte rate
	binary.LittleEndian.PutUint16(header[32:34], uint16(blockAlign))   // block align
	binary.LittleEndian.PutUint16(header[34:36], uint16(wavBitsPerSamp))// bits per sample

	// data sub-chunk
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], dataSize)

	_, err := f.Write(header)
	return err
}

// finalizeWAV seeks back to the header and updates with actual data size
func (r *Recorder) finalizeWAV(f *os.File, dataSize uint32) {
	// Update RIFF chunk size (offset 4)
	f.Seek(4, 0)
	sizeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeBytes, dataSize+36)
	f.Write(sizeBytes)

	// Update data sub-chunk size (offset 40)
	f.Seek(40, 0)
	binary.LittleEndian.PutUint32(sizeBytes, dataSize)
	f.Write(sizeBytes)
}

// =============================================================================
// MP3 Conversion (requires ffmpeg installed on the system)
// =============================================================================

// ConvertToMP3 converts a WAV file to MP3 using ffmpeg
func ConvertToMP3(wavPath, mp3Path string) error {
	cmd := exec.Command("ffmpeg", "-y", "-i", wavPath, "-codec:a", "libmp3lame", "-b:a", "64k", "-ac", "1", mp3Path)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg convert %s: %w — %s", wavPath, err, string(output))
	}
	return nil
}

// MixAndConvert mixes two mono WAV files into a single stereo MP3 using amix filter
func MixAndConvert(callerWAV, aiWAV, outputMP3 string) error {
	cmd := exec.Command("ffmpeg", "-y",
		"-i", callerWAV, "-i", aiWAV,
		"-filter_complex", "amix=inputs=2:duration=longest:dropout_transition=2",
		"-codec:a", "libmp3lame", "-b:a", "64k", "-ac", "1",
		outputMP3)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg mix %s + %s: %w — %s", callerWAV, aiWAV, err, string(output))
	}
	return nil
}

// MixToWAV mixes two mono WAV files into a single mono WAV (for serving WAV mixed)
func MixToWAV(callerWAV, aiWAV, outputWAV string) error {
	cmd := exec.Command("ffmpeg", "-y",
		"-i", callerWAV, "-i", aiWAV,
		"-filter_complex", "amix=inputs=2:duration=longest:dropout_transition=2",
		"-ac", "1",
		outputWAV)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg mix WAV %s + %s: %w — %s", callerWAV, aiWAV, err, string(output))
	}
	return nil
}

// MP3Paths holds the paths to all generated MP3 files
type MP3Paths struct {
	CallerMP3 string
	AIMP3     string
	MixedMP3  string
}

// ConvertCallRecordings converts all WAV recordings for a call to MP3.
// Returns the MP3 file paths. Naming: {notariaID}_{callerID}_{date}_{time}_{callIDshort}_{channel}.mp3
func ConvertCallRecordings(callerWAV, aiWAV, notariaID, callerID, callID string, logger *zap.Logger) (*MP3Paths, error) {
	if callerWAV == "" && aiWAV == "" {
		return nil, fmt.Errorf("no WAV recordings to convert")
	}

	dir := filepath.Dir(callerWAV)
	if dir == "." && aiWAV != "" {
		dir = filepath.Dir(aiWAV)
	}

	// Build filename prefix: {notariaID}_{callerID}_{YYYYMMDD}_{HHMMSS}_{callIDshort}
	now := time.Now()
	shortID := callID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	// Sanitize caller ID for filename
	safeCallerID := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, callerID)

	prefix := fmt.Sprintf("%s_%s_%s_%s",
		notariaID, safeCallerID,
		now.Format("20060102"), now.Format("150405"))
	prefix = fmt.Sprintf("%s_%s", prefix, shortID)

	paths := &MP3Paths{}
	var firstErr error

	// Convert caller WAV to MP3
	if callerWAV != "" {
		paths.CallerMP3 = filepath.Join(dir, prefix+"_caller.mp3")
		if err := ConvertToMP3(callerWAV, paths.CallerMP3); err != nil {
			logger.Error("Failed to convert caller WAV to MP3", zap.Error(err))
			if firstErr == nil {
				firstErr = err
			}
			paths.CallerMP3 = ""
		}
	}

	// Convert AI WAV to MP3
	if aiWAV != "" {
		paths.AIMP3 = filepath.Join(dir, prefix+"_ai.mp3")
		if err := ConvertToMP3(aiWAV, paths.AIMP3); err != nil {
			logger.Error("Failed to convert AI WAV to MP3", zap.Error(err))
			if firstErr == nil {
				firstErr = err
			}
			paths.AIMP3 = ""
		}
	}

	// Mix both channels to a single MP3
	if callerWAV != "" && aiWAV != "" {
		paths.MixedMP3 = filepath.Join(dir, prefix+"_mixed.mp3")
		if err := MixAndConvert(callerWAV, aiWAV, paths.MixedMP3); err != nil {
			logger.Error("Failed to mix and convert to MP3", zap.Error(err))
			if firstErr == nil {
				firstErr = err
			}
			paths.MixedMP3 = ""
		}
	}

	if paths.CallerMP3 == "" && paths.AIMP3 == "" && paths.MixedMP3 == "" {
		return nil, fmt.Errorf("all MP3 conversions failed: %w", firstErr)
	}

	logger.Info("MP3 conversion complete",
		zap.String("caller_mp3", paths.CallerMP3),
		zap.String("ai_mp3", paths.AIMP3),
		zap.String("mixed_mp3", paths.MixedMP3))

	return paths, nil
}

// FFmpegAvailable checks if ffmpeg is installed and accessible
func FFmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}
