package db

import (
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	"go.uber.org/zap/zapcore"
)

// LogCore is a zapcore.Core that buffers log entries and batch-inserts them into SQLite.
type LogCore struct {
	conn      *sql.DB
	minLevel  zapcore.Level
	mu        sync.Mutex
	buffer    []logEntry
	fields    []zapcore.Field
	done      chan struct{}
}

type logEntry struct {
	Timestamp string
	Level     string
	Logger    string
	Message   string
	Caller    string
	Metadata  string
}

// NewLogCore creates a core that writes logs to the system_logs table.
// Logs are buffered and flushed every 2 seconds.
func NewLogCore(conn *sql.DB, minLevel zapcore.Level) *LogCore {
	c := &LogCore{
		conn:     conn,
		minLevel: minLevel,
		buffer:   make([]logEntry, 0, 64),
		done:     make(chan struct{}),
	}
	go c.flushLoop()
	return c
}

func (c *LogCore) Enabled(level zapcore.Level) bool {
	return level >= c.minLevel
}

func (c *LogCore) With(fields []zapcore.Field) zapcore.Core {
	clone := &LogCore{
		conn:     c.conn,
		minLevel: c.minLevel,
		buffer:   c.buffer,
		fields:   append(append([]zapcore.Field{}, c.fields...), fields...),
		done:     c.done,
	}
	return clone
}

func (c *LogCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return ce.AddCore(entry, c)
	}
	return ce
}

func (c *LogCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	// Build metadata from fields
	allFields := append(c.fields, fields...)
	meta := make(map[string]interface{}, len(allFields))
	enc := zapcore.NewMapObjectEncoder()
	for _, f := range allFields {
		f.AddTo(enc)
	}
	for k, v := range enc.Fields {
		meta[k] = v
	}
	metaJSON, _ := json.Marshal(meta)

	caller := ""
	if entry.Caller.Defined {
		caller = entry.Caller.TrimmedPath()
	}

	e := logEntry{
		Timestamp: entry.Time.UTC().Format(time.RFC3339Nano),
		Level:     entry.Level.String(),
		Logger:    entry.LoggerName,
		Message:   entry.Message,
		Caller:    caller,
		Metadata:  string(metaJSON),
	}

	c.mu.Lock()
	c.buffer = append(c.buffer, e)
	c.mu.Unlock()
	return nil
}

func (c *LogCore) Sync() error {
	c.flush()
	return nil
}

// Stop signals the flush loop to exit and does a final flush.
func (c *LogCore) Stop() {
	close(c.done)
	c.flush()
}

func (c *LogCore) flushLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.flush()
		case <-c.done:
			return
		}
	}
}

func (c *LogCore) flush() {
	c.mu.Lock()
	if len(c.buffer) == 0 {
		c.mu.Unlock()
		return
	}
	batch := c.buffer
	c.buffer = make([]logEntry, 0, 64)
	c.mu.Unlock()

	tx, err := c.conn.Begin()
	if err != nil {
		return
	}
	stmt, err := tx.Prepare("INSERT INTO system_logs (timestamp, level, logger, message, caller, metadata) VALUES (?,?,?,?,?,?)")
	if err != nil {
		tx.Rollback()
		return
	}
	defer stmt.Close()

	for _, e := range batch {
		stmt.Exec(e.Timestamp, e.Level, e.Logger, e.Message, e.Caller, e.Metadata)
	}
	tx.Commit()
}
