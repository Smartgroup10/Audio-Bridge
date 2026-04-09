package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"go.uber.org/zap"

	"github.com/smartgroup/audio-bridge/internal/config"
)

// DB wraps the SQLite connection
type DB struct {
	conn   *sql.DB
	logger *zap.Logger
}

// CallRecord represents a call stored in the database
type CallRecord struct {
	ID              string   `json:"id"`
	CallerID        string   `json:"caller_id"`
	DDI             string   `json:"ddi"`
	NotariaID       string   `json:"notaria_id"`
	Direction       string   `json:"direction"`
	State           string   `json:"state"`
	CallType        string   `json:"call_type"`
	Schedule        string   `json:"schedule"`
	StartTime       string   `json:"start_time"`
	AnswerTime      *string  `json:"answer_time,omitempty"`
	EndTime         *string  `json:"end_time,omitempty"`
	DurationSeconds float64  `json:"duration_seconds"`
	EndReason       string   `json:"end_reason"`
	TransferDest    string   `json:"transfer_dest"`
	AsteriskChannel string   `json:"asterisk_channel"`
	RecordingCaller    string   `json:"recording_caller"`
	RecordingAI        string   `json:"recording_ai"`
	RecordingCallerMP3 string   `json:"recording_caller_mp3"`
	RecordingAIMP3     string   `json:"recording_ai_mp3"`
	RecordingMixedMP3  string   `json:"recording_mixed_mp3"`
	TranscriptUser     string   `json:"transcript_user"`
	TranscriptAI       string   `json:"transcript_ai"`
	CreatedAt          string   `json:"created_at"`
}

// InteractionLog represents a log entry for a call
type InteractionLog struct {
	ID        int64  `json:"id"`
	CallID    string `json:"call_id"`
	Timestamp string `json:"timestamp"`
	Direction string `json:"direction"` // "user" or "ai"
	Content   string `json:"content"`
	EventType string `json:"event_type"` // "speech", "transfer", "hangup", "function_call"
	Metadata  string `json:"metadata"`
}

// TenantRecord represents a tenant stored in the database
type TenantRecord struct {
	NotariaID string `json:"notaria_id"`
	Name      string `json:"name"`
	DDIs      string `json:"ddis"`
	Enabled   int    `json:"enabled"`
	SIPTrunk  string `json:"sip_trunk"`
	Config    string `json:"config"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// SystemLogRecord represents a system log entry
type SystemLogRecord struct {
	ID        int64  `json:"id"`
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Logger    string `json:"logger"`
	Message   string `json:"message"`
	Caller    string `json:"caller"`
	Metadata  string `json:"metadata"`
}

// DashboardStats holds aggregate stats
type DashboardStats struct {
	CallsToday       int     `json:"calls_today"`
	CallsActive      int     `json:"calls_active"`
	AvgDuration      float64 `json:"avg_duration_seconds"`
	TotalCalls       int     `json:"total_calls"`
	ByNotaria        map[string]int `json:"by_notaria"`
	InboundToday     int     `json:"inbound_today"`
	OutboundToday    int     `json:"outbound_today"`
}

// New creates a new DB instance
func New(dbPath string, logger *zap.Logger) (*DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating db directory %s: %w", dir, err)
	}

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite %s: %w", dbPath, err)
	}

	// Enable WAL mode for better concurrency (readers don't block writers)
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}
	// Wait up to 5s if DB is locked instead of failing immediately
	if _, err := conn.Exec("PRAGMA busy_timeout=5000"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("setting busy timeout: %w", err)
	}
	// Synchronous NORMAL is safe with WAL and much faster than FULL
	if _, err := conn.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("setting synchronous mode: %w", err)
	}

	// Connection pool: allow many readers but only 1 writer (SQLite limitation)
	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(30 * time.Minute)

	db := &DB{conn: conn, logger: logger}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	logger.Info("Database initialized", zap.String("path", dbPath))
	return db, nil
}

// Close closes the database connection
func (d *DB) Close() error {
	return d.conn.Close()
}

func (d *DB) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS calls (
			id TEXT PRIMARY KEY,
			caller_id TEXT DEFAULT '',
			ddi TEXT DEFAULT '',
			notaria_id TEXT DEFAULT '',
			direction TEXT DEFAULT '',
			state TEXT DEFAULT '',
			call_type TEXT DEFAULT '',
			schedule TEXT DEFAULT '',
			start_time DATETIME,
			answer_time DATETIME,
			end_time DATETIME,
			duration_seconds REAL DEFAULT 0,
			end_reason TEXT DEFAULT '',
			transfer_dest TEXT DEFAULT '',
			asterisk_channel TEXT DEFAULT '',
			recording_caller TEXT DEFAULT '',
			recording_ai TEXT DEFAULT '',
			transcript_user TEXT DEFAULT '',
			transcript_ai TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS tenants (
			notaria_id TEXT PRIMARY KEY,
			name TEXT DEFAULT '',
			ddis TEXT DEFAULT '[]',
			enabled INTEGER DEFAULT 1,
			sip_trunk TEXT DEFAULT '',
			config TEXT DEFAULT '{}',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS interaction_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			call_id TEXT NOT NULL,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			direction TEXT DEFAULT '',
			content TEXT DEFAULT '',
			event_type TEXT DEFAULT '',
			metadata TEXT DEFAULT '{}',
			FOREIGN KEY (call_id) REFERENCES calls(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_calls_notaria ON calls(notaria_id)`,
		`CREATE INDEX IF NOT EXISTS idx_calls_start ON calls(start_time)`,
		`CREATE INDEX IF NOT EXISTS idx_calls_state ON calls(state)`,
		`CREATE INDEX IF NOT EXISTS idx_logs_call ON interaction_logs(call_id)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			expires_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS system_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			level TEXT DEFAULT 'info',
			logger TEXT DEFAULT '',
			message TEXT DEFAULT '',
			caller TEXT DEFAULT '',
			metadata TEXT DEFAULT '{}'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_syslogs_ts ON system_logs(timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_syslogs_level ON system_logs(level)`,
	}

	for _, m := range migrations {
		if _, err := d.conn.Exec(m); err != nil {
			return fmt.Errorf("migration error: %w\nSQL: %s", err, m)
		}
	}

	// Additive migrations (ALTER TABLE — ignore "duplicate column" errors)
	alterMigrations := []string{
		`ALTER TABLE tenants ADD COLUMN company_id TEXT DEFAULT ''`,
		`ALTER TABLE calls ADD COLUMN recording_caller_mp3 TEXT DEFAULT ''`,
		`ALTER TABLE calls ADD COLUMN recording_ai_mp3 TEXT DEFAULT ''`,
		`ALTER TABLE calls ADD COLUMN recording_mixed_mp3 TEXT DEFAULT ''`,
	}
	for _, m := range alterMigrations {
		d.conn.Exec(m) // ignore error if column already exists
	}

	d.logger.Info("Database migrations complete")
	return nil
}

// --- Calls CRUD ---

// InsertCall stores a completed call
func (d *DB) InsertCall(c CallRecord) error {
	_, err := d.conn.Exec(`
		INSERT OR REPLACE INTO calls
		(id, caller_id, ddi, notaria_id, direction, state, call_type, schedule,
		 start_time, answer_time, end_time, duration_seconds, end_reason,
		 transfer_dest, asterisk_channel, recording_caller, recording_ai,
		 transcript_user, transcript_ai)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.CallerID, c.DDI, c.NotariaID, c.Direction, c.State,
		c.CallType, c.Schedule, c.StartTime, c.AnswerTime, c.EndTime,
		c.DurationSeconds, c.EndReason, c.TransferDest, c.AsteriskChannel,
		c.RecordingCaller, c.RecordingAI, c.TranscriptUser, c.TranscriptAI,
	)
	return err
}

// UpdateCallMP3 updates the MP3 recording paths for a call
func (d *DB) UpdateCallMP3(callID, callerMP3, aiMP3, mixedMP3 string) error {
	_, err := d.conn.Exec(
		`UPDATE calls SET recording_caller_mp3=?, recording_ai_mp3=?, recording_mixed_mp3=? WHERE id=?`,
		callerMP3, aiMP3, mixedMP3, callID)
	return err
}

// GetCall retrieves a call by ID
func (d *DB) GetCall(id string) (*CallRecord, error) {
	var c CallRecord
	err := d.conn.QueryRow(`SELECT id, caller_id, ddi, notaria_id, direction, state,
		call_type, schedule, start_time, answer_time, end_time, duration_seconds,
		end_reason, transfer_dest, asterisk_channel, recording_caller, recording_ai,
		recording_caller_mp3, recording_ai_mp3, recording_mixed_mp3,
		transcript_user, transcript_ai, created_at
		FROM calls WHERE id = ?`, id).Scan(
		&c.ID, &c.CallerID, &c.DDI, &c.NotariaID, &c.Direction, &c.State,
		&c.CallType, &c.Schedule, &c.StartTime, &c.AnswerTime, &c.EndTime,
		&c.DurationSeconds, &c.EndReason, &c.TransferDest, &c.AsteriskChannel,
		&c.RecordingCaller, &c.RecordingAI,
		&c.RecordingCallerMP3, &c.RecordingAIMP3, &c.RecordingMixedMP3,
		&c.TranscriptUser, &c.TranscriptAI,
		&c.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &c, err
}

// ListCalls returns paginated calls with optional filters
func (d *DB) ListCalls(page, limit int, notariaID, from, to string) ([]CallRecord, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	where := "1=1"
	args := []interface{}{}

	if notariaID != "" {
		where += " AND notaria_id = ?"
		args = append(args, notariaID)
	}
	if from != "" {
		where += " AND start_time >= ?"
		args = append(args, from)
	}
	if to != "" {
		where += " AND start_time <= ?"
		args = append(args, to)
	}

	// Count total
	var total int
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	err := d.conn.QueryRow("SELECT COUNT(*) FROM calls WHERE "+where, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Fetch page
	query := fmt.Sprintf("SELECT id, caller_id, ddi, notaria_id, direction, state, call_type, schedule, start_time, answer_time, end_time, duration_seconds, end_reason, transfer_dest, asterisk_channel, recording_caller, recording_ai, recording_caller_mp3, recording_ai_mp3, recording_mixed_mp3, transcript_user, transcript_ai, created_at FROM calls WHERE %s ORDER BY start_time DESC LIMIT ? OFFSET ?", where)
	args = append(args, limit, offset)

	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var calls []CallRecord
	for rows.Next() {
		var c CallRecord
		if err := rows.Scan(&c.ID, &c.CallerID, &c.DDI, &c.NotariaID, &c.Direction,
			&c.State, &c.CallType, &c.Schedule, &c.StartTime, &c.AnswerTime,
			&c.EndTime, &c.DurationSeconds, &c.EndReason, &c.TransferDest,
			&c.AsteriskChannel, &c.RecordingCaller, &c.RecordingAI,
			&c.RecordingCallerMP3, &c.RecordingAIMP3, &c.RecordingMixedMP3,
			&c.TranscriptUser, &c.TranscriptAI, &c.CreatedAt); err != nil {
			return nil, 0, err
		}
		calls = append(calls, c)
	}
	return calls, total, nil
}

// GetDashboardStats returns aggregate statistics
func (d *DB) GetDashboardStats(activeCalls int) (*DashboardStats, error) {
	stats := &DashboardStats{
		CallsActive: activeCalls,
		ByNotaria:   make(map[string]int),
	}

	today := time.Now().Format("2006-01-02")

	// Calls today
	d.conn.QueryRow("SELECT COUNT(*) FROM calls WHERE date(start_time) = ?", today).Scan(&stats.CallsToday)

	// Avg duration today
	d.conn.QueryRow("SELECT COALESCE(AVG(duration_seconds), 0) FROM calls WHERE date(start_time) = ? AND duration_seconds > 0", today).Scan(&stats.AvgDuration)

	// Total calls
	d.conn.QueryRow("SELECT COUNT(*) FROM calls").Scan(&stats.TotalCalls)

	// Inbound/outbound today
	d.conn.QueryRow("SELECT COUNT(*) FROM calls WHERE date(start_time) = ? AND direction = 'inbound'", today).Scan(&stats.InboundToday)
	d.conn.QueryRow("SELECT COUNT(*) FROM calls WHERE date(start_time) = ? AND direction = 'outbound'", today).Scan(&stats.OutboundToday)

	// By notaria today
	rows, err := d.conn.Query("SELECT notaria_id, COUNT(*) FROM calls WHERE date(start_time) = ? GROUP BY notaria_id", today)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var nid string
			var cnt int
			if rows.Scan(&nid, &cnt) == nil {
				stats.ByNotaria[nid] = cnt
			}
		}
	}

	return stats, nil
}

// --- Interaction Logs ---

// InsertLog stores a call interaction log
func (d *DB) InsertLog(log InteractionLog) error {
	_, err := d.conn.Exec(`INSERT INTO interaction_logs (call_id, timestamp, direction, content, event_type, metadata) VALUES (?,?,?,?,?,?)`,
		log.CallID, log.Timestamp, log.Direction, log.Content, log.EventType, log.Metadata)
	return err
}

// GetCallLogs retrieves all logs for a call
func (d *DB) GetCallLogs(callID string) ([]InteractionLog, error) {
	rows, err := d.conn.Query("SELECT id, call_id, timestamp, direction, content, event_type, metadata FROM interaction_logs WHERE call_id = ? ORDER BY timestamp", callID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []InteractionLog
	for rows.Next() {
		var l InteractionLog
		if err := rows.Scan(&l.ID, &l.CallID, &l.Timestamp, &l.Direction, &l.Content, &l.EventType, &l.Metadata); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}

// --- Tenants CRUD ---

// InsertTenant creates a new tenant
func (d *DB) InsertTenant(t config.TenantConfig) error {
	j := t.ToJSON()
	now := time.Now().Format(time.RFC3339)
	_, err := d.conn.Exec(`INSERT INTO tenants (notaria_id, company_id, name, ddis, enabled, sip_trunk, config, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		j.NotariaID, j.CompanyID, j.Name, j.DDIs, boolToInt(t.Enabled), j.SIPTrunk, j.Config, now, now)
	return err
}

// UpdateTenant updates an existing tenant
func (d *DB) UpdateTenant(t config.TenantConfig) error {
	j := t.ToJSON()
	now := time.Now().Format(time.RFC3339)
	_, err := d.conn.Exec(`UPDATE tenants SET company_id=?, name=?, ddis=?, enabled=?, sip_trunk=?, config=?, updated_at=? WHERE notaria_id=?`,
		j.CompanyID, j.Name, j.DDIs, boolToInt(t.Enabled), j.SIPTrunk, j.Config, now, j.NotariaID)
	return err
}

// DeleteTenant removes a tenant
func (d *DB) DeleteTenant(notariaID string) error {
	_, err := d.conn.Exec("DELETE FROM tenants WHERE notaria_id = ?", notariaID)
	return err
}

// ListTenants returns all tenants
func (d *DB) ListTenants() ([]config.TenantConfig, error) {
	rows, err := d.conn.Query("SELECT notaria_id, company_id, name, ddis, enabled, sip_trunk, config FROM tenants ORDER BY notaria_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tenants []config.TenantConfig
	for rows.Next() {
		var j config.TenantConfigJSON
		var enabled int
		if err := rows.Scan(&j.NotariaID, &j.CompanyID, &j.Name, &j.DDIs, &enabled, &j.SIPTrunk, &j.Config); err != nil {
			return nil, err
		}
		j.Enabled = enabled == 1
		tenants = append(tenants, config.TenantFromJSON(j))
	}
	return tenants, nil
}

// GetTenant returns a single tenant
func (d *DB) GetTenant(notariaID string) (*config.TenantConfig, error) {
	var j config.TenantConfigJSON
	var enabled int
	err := d.conn.QueryRow("SELECT notaria_id, company_id, name, ddis, enabled, sip_trunk, config FROM tenants WHERE notaria_id = ?", notariaID).
		Scan(&j.NotariaID, &j.CompanyID, &j.Name, &j.DDIs, &enabled, &j.SIPTrunk, &j.Config)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j.Enabled = enabled == 1
	t := config.TenantFromJSON(j)
	return &t, nil
}

// SyncTenantsFromConfig loads tenants from config YAML into DB (initial seed)
func (d *DB) SyncTenantsFromConfig(tenants []config.TenantConfig) error {
	for _, t := range tenants {
		existing, err := d.GetTenant(t.NotariaID)
		if err != nil {
			return err
		}
		if existing == nil {
			if err := d.InsertTenant(t); err != nil {
				return fmt.Errorf("inserting tenant %s: %w", t.NotariaID, err)
			}
			d.logger.Info("Seeded tenant from config", zap.String("notaria_id", t.NotariaID))
		}
	}
	return nil
}

// --- Recordings helpers ---

// ListRecordings returns calls that have recordings
func (d *DB) ListRecordings(page, limit int) ([]CallRecord, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	var total int
	d.conn.QueryRow("SELECT COUNT(*) FROM calls WHERE recording_caller != '' OR recording_ai != ''").Scan(&total)

	rows, err := d.conn.Query(`SELECT id, caller_id, ddi, notaria_id, direction, state, call_type, schedule, start_time, answer_time, end_time, duration_seconds, end_reason, transfer_dest, asterisk_channel, recording_caller, recording_ai, recording_caller_mp3, recording_ai_mp3, recording_mixed_mp3, transcript_user, transcript_ai, created_at
		FROM calls WHERE recording_caller != '' OR recording_ai != ''
		ORDER BY start_time DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var calls []CallRecord
	for rows.Next() {
		var c CallRecord
		if err := rows.Scan(&c.ID, &c.CallerID, &c.DDI, &c.NotariaID, &c.Direction,
			&c.State, &c.CallType, &c.Schedule, &c.StartTime, &c.AnswerTime,
			&c.EndTime, &c.DurationSeconds, &c.EndReason, &c.TransferDest,
			&c.AsteriskChannel, &c.RecordingCaller, &c.RecordingAI,
			&c.RecordingCallerMP3, &c.RecordingAIMP3, &c.RecordingMixedMP3,
			&c.TranscriptUser, &c.TranscriptAI, &c.CreatedAt); err != nil {
			return nil, 0, err
		}
		calls = append(calls, c)
	}
	return calls, total, nil
}

// --- System Logs ---

// InsertSystemLog stores a system log entry
func (d *DB) InsertSystemLog(level, logger, message, caller, metadata string) error {
	_, err := d.conn.Exec(
		`INSERT INTO system_logs (timestamp, level, logger, message, caller, metadata) VALUES (?,?,?,?,?,?)`,
		time.Now().UTC().Format(time.RFC3339Nano), level, logger, message, caller, metadata)
	return err
}

// ListSystemLogs returns paginated system logs with optional filters
func (d *DB) ListSystemLogs(page, limit int, level, from, to string) ([]SystemLogRecord, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	offset := (page - 1) * limit

	where := "1=1"
	args := []interface{}{}
	if level != "" {
		where += " AND level = ?"
		args = append(args, level)
	}
	if from != "" {
		where += " AND timestamp >= ?"
		args = append(args, from)
	}
	if to != "" {
		where += " AND timestamp <= ?"
		args = append(args, to)
	}

	var total int
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	d.conn.QueryRow("SELECT COUNT(*) FROM system_logs WHERE "+where, countArgs...).Scan(&total)

	query := fmt.Sprintf("SELECT id, timestamp, level, logger, message, caller, metadata FROM system_logs WHERE %s ORDER BY id DESC LIMIT ? OFFSET ?", where)
	args = append(args, limit, offset)

	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var logs []SystemLogRecord
	for rows.Next() {
		var l SystemLogRecord
		if err := rows.Scan(&l.ID, &l.Timestamp, &l.Level, &l.Logger, &l.Message, &l.Caller, &l.Metadata); err != nil {
			return nil, 0, err
		}
		logs = append(logs, l)
	}
	return logs, total, nil
}

// ListInteractionLogs returns paginated interaction logs with global filters
func (d *DB) ListInteractionLogs(page, limit int, callID, direction, eventType, from, to string) ([]InteractionLog, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	offset := (page - 1) * limit

	where := "1=1"
	args := []interface{}{}
	if callID != "" {
		where += " AND call_id = ?"
		args = append(args, callID)
	}
	if direction != "" {
		where += " AND direction = ?"
		args = append(args, direction)
	}
	if eventType != "" {
		where += " AND event_type = ?"
		args = append(args, eventType)
	}
	if from != "" {
		where += " AND timestamp >= ?"
		args = append(args, from)
	}
	if to != "" {
		where += " AND timestamp <= ?"
		args = append(args, to)
	}

	var total int
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	d.conn.QueryRow("SELECT COUNT(*) FROM interaction_logs WHERE "+where, countArgs...).Scan(&total)

	query := fmt.Sprintf("SELECT id, call_id, timestamp, direction, content, event_type, metadata FROM interaction_logs WHERE %s ORDER BY id DESC LIMIT ? OFFSET ?", where)
	args = append(args, limit, offset)

	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var logs []InteractionLog
	for rows.Next() {
		var l InteractionLog
		if err := rows.Scan(&l.ID, &l.CallID, &l.Timestamp, &l.Direction, &l.Content, &l.EventType, &l.Metadata); err != nil {
			return nil, 0, err
		}
		logs = append(logs, l)
	}
	return logs, total, nil
}

// PruneSystemLogs removes system logs older than maxAgeDays
func (d *DB) PruneSystemLogs(maxAgeDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -maxAgeDays).UTC().Format(time.RFC3339)
	res, err := d.conn.Exec("DELETE FROM system_logs WHERE timestamp < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Conn returns the raw database connection (for custom cores)
func (d *DB) Conn() *sql.DB {
	return d.conn
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- Sessions (persistent tokens) ---

// SaveToken persists a session token so it survives server restarts
func (d *DB) SaveToken(token string, expiresAt time.Time) error {
	_, err := d.conn.Exec(
		`INSERT OR REPLACE INTO sessions (token, expires_at) VALUES (?, ?)`,
		token, expiresAt.UTC().Format(time.RFC3339))
	return err
}

// LoadTokens returns all non-expired tokens from the database
func (d *DB) LoadTokens() (map[string]time.Time, error) {
	rows, err := d.conn.Query(
		`SELECT token, expires_at FROM sessions WHERE expires_at > ?`,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tokens := make(map[string]time.Time)
	for rows.Next() {
		var tok, exp string
		if err := rows.Scan(&tok, &exp); err != nil {
			continue
		}
		t, err := time.Parse(time.RFC3339, exp)
		if err != nil {
			continue
		}
		tokens[tok] = t
	}
	return tokens, nil
}

// DeleteExpiredTokens removes tokens that have passed their expiry
func (d *DB) DeleteExpiredTokens() (int64, error) {
	res, err := d.conn.Exec(
		`DELETE FROM sessions WHERE expires_at <= ?`,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Helper to marshal metadata to JSON string
func MetadataJSON(m map[string]string) string {
	if m == nil {
		return "{}"
	}
	data, _ := json.Marshal(m)
	return string(data)
}
