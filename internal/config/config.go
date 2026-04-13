package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the full application configuration
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Asterisk  AsteriskConfig  `yaml:"asterisk"`
	AI        AIConfig        `yaml:"ai"`
	Audio     AudioConfig     `yaml:"audio"`
	API       APIConfig       `yaml:"api"`
	Logging   LoggingConfig   `yaml:"logging"`
	Database  DatabaseConfig  `yaml:"database"`
	Recording RecordingConfig `yaml:"recording"`
	Admin      AdminConfig      `yaml:"admin"`
	Backoffice BackofficeConfig `yaml:"backoffice"`
	Webhook    WebhookConfig    `yaml:"webhook"`
	Lakimi     LakimiConfig     `yaml:"lakimi"`
	Tenants    []TenantConfig   `yaml:"tenants"`
}

// LakimiConfig holds configuration for the Lakimi/CTN multiplexed WSS connection
type LakimiConfig struct {
	Endpoint            string `yaml:"endpoint"`              // wss://lakimi.ctn.es/v1/realtime
	TLSCert             string `yaml:"tls_cert"`              // Path to client certificate
	TLSKey              string `yaml:"tls_key"`               // Path to client private key
	TLSCA               string `yaml:"tls_ca"`                // Path to Lakimi CA certificate
	FrameSizeMs         int    `yaml:"frame_size_ms"`         // Outbound frame size (default 30)
	PingInterval        int    `yaml:"ping_interval"`         // Keepalive interval in seconds (default 30)
	ReconnectMaxRetries int    `yaml:"reconnect_max_retries"` // Max reconnection attempts (default 5)
}

type ServerConfig struct {
	AudioSocketAddr string `yaml:"audiosocket_addr"`
	MaxConcurrent   int    `yaml:"max_concurrent"`
}

type AsteriskConfig struct {
	AMIHost     string `yaml:"ami_host"`
	AMIPort     int    `yaml:"ami_port"`
	AMIUser     string `yaml:"ami_user"`
	AMIPassword string `yaml:"ami_password"`
}

type AIConfig struct {
	Endpoint                string `yaml:"endpoint"`
	APIKey                  string `yaml:"api_key"`
	AuthType                string `yaml:"auth_type"`
	BearerToken             string `yaml:"bearer_token"`
	TimeoutSec              int    `yaml:"timeout_sec"`
	Type                    string `yaml:"type"`         // "openai" or "custom"
	Model                   string `yaml:"model"`
	Voice                   string `yaml:"voice"`
	Instructions            string `yaml:"instructions"`
	Language                string `yaml:"language"`
	OriginateRetries        int    `yaml:"originate_retries"`
	OriginateRetryIntervalSec int  `yaml:"originate_retry_interval_sec"`
}

type AudioConfig struct {
	SampleRate  int    `yaml:"sample_rate"`
	BitDepth    int    `yaml:"bit_depth"`
	Channels    int    `yaml:"channels"`
	Codec       string `yaml:"codec"`
	FrameSizeMs int    `yaml:"frame_size_ms"`
}

type APIConfig struct {
	Addr      string `yaml:"addr"`
	APIKey    string `yaml:"api_key"`
	PublicURL string `yaml:"public_url"` // Base URL for download links in webhooks (e.g. "https://bridge.smartgroup.es")
}

// WebhookConfig holds configuration for outgoing webhooks to CTN
type WebhookConfig struct {
	URL        string `yaml:"url"`         // CTN webhook endpoint
	APIKey     string `yaml:"api_key"`     // Authentication key sent as X-API-Key header
	Enabled    bool   `yaml:"enabled"`     // Enable/disable webhook sending
	TimeoutSec int    `yaml:"timeout_sec"` // HTTP request timeout (default 10)
	RetryCount int    `yaml:"retry_count"` // Max retry attempts (default 3)
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type RecordingConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
}

type AdminConfig struct {
	Password     string `yaml:"password"`      // plaintext (legacy, used if password_hash is empty)
	PasswordHash string `yaml:"password_hash"` // bcrypt hash (preferred)
}

// BackofficeConfig holds configuration for the CTN backoffice API
type BackofficeConfig struct {
	URL      string `yaml:"url"`       // CTN backoffice endpoint
	APIKey   string `yaml:"api_key"`   // Authentication key
	CacheTTL int    `yaml:"cache_ttl"` // Cache TTL in seconds (default 300)
	Enabled  bool   `yaml:"enabled"`   // Enable backoffice tenant lookup
}

// TenantConfig represents a single notary's configuration
type TenantConfig struct {
	NotariaID    string         `yaml:"notaria_id" json:"notaria_id"`
	CompanyID    string         `yaml:"company_id" json:"company_id"`     // PekePBX company ID (e.g. "20242") — triggers dialplan auto-provisioning
	Name         string         `yaml:"name" json:"name"`
	DDIs         []string       `yaml:"ddis" json:"ddis"`
	Schedule     ScheduleConfig `yaml:"schedule" json:"schedule"`
	Transfers    TransferConfig `yaml:"transfers" json:"transfers"`
	SIPTrunk     string         `yaml:"sip_trunk" json:"sip_trunk"`
	Enabled      bool           `yaml:"enabled" json:"enabled"`
	Instructions string         `yaml:"instructions" json:"instructions"` // Custom AI prompt (empty = global default)
	Voice        string         `yaml:"voice" json:"voice"`               // OpenAI voice override (empty = global)
	Language     string         `yaml:"language" json:"language"`           // Language override (empty = global)
	VIPWhitelist []string       `yaml:"vip_whitelist" json:"vip_whitelist"` // Caller IDs that bypass AI (direct transfer)
}

type ScheduleConfig struct {
	Timezone      string      `yaml:"timezone" json:"timezone"`
	BusinessHours []HourRange `yaml:"business_hours" json:"business_hours"`
}

type HourRange struct {
	Days  string `yaml:"days" json:"days"`
	Start string `yaml:"start" json:"start"`
	End   string `yaml:"end" json:"end"`
}

type TransferConfig struct {
	Default    string            `yaml:"default" json:"default"`
	Extensions map[string]string `yaml:"extensions" json:"extensions"`
	GroupHunt  string            `yaml:"group_hunt" json:"group_hunt"`
}

// TenantConfigJSON is used for DB storage (DDIs and config as JSON)
type TenantConfigJSON struct {
	NotariaID string `json:"notaria_id"`
	CompanyID string `json:"company_id"`
	Name      string `json:"name"`
	DDIs      string `json:"ddis"`     // JSON array
	Enabled   bool   `json:"enabled"`
	SIPTrunk  string `json:"sip_trunk"`
	Config    string `json:"config"`   // JSON blob (schedule, transfers)
}

// ToJSON converts TenantConfig to JSON storage format
func (t *TenantConfig) ToJSON() TenantConfigJSON {
	ddisJSON, _ := json.Marshal(t.DDIs)
	configMap := map[string]interface{}{
		"schedule":      t.Schedule,
		"transfers":     t.Transfers,
		"instructions":  t.Instructions,
		"voice":         t.Voice,
		"language":      t.Language,
		"vip_whitelist": t.VIPWhitelist,
	}
	configJSON, _ := json.Marshal(configMap)
	return TenantConfigJSON{
		NotariaID: t.NotariaID,
		CompanyID: t.CompanyID,
		Name:      t.Name,
		DDIs:      string(ddisJSON),
		Enabled:   t.Enabled,
		SIPTrunk:  t.SIPTrunk,
		Config:    string(configJSON),
	}
}

// FromJSON converts JSON storage format to TenantConfig
func TenantFromJSON(j TenantConfigJSON) TenantConfig {
	t := TenantConfig{
		NotariaID: j.NotariaID,
		CompanyID: j.CompanyID,
		Name:      j.Name,
		Enabled:   j.Enabled,
		SIPTrunk:  j.SIPTrunk,
	}
	json.Unmarshal([]byte(j.DDIs), &t.DDIs)
	var configMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(j.Config), &configMap); err == nil {
		if raw, ok := configMap["schedule"]; ok {
			json.Unmarshal(raw, &t.Schedule)
		}
		if raw, ok := configMap["transfers"]; ok {
			json.Unmarshal(raw, &t.Transfers)
		}
		if raw, ok := configMap["instructions"]; ok {
			json.Unmarshal(raw, &t.Instructions)
		}
		if raw, ok := configMap["voice"]; ok {
			json.Unmarshal(raw, &t.Voice)
		}
		if raw, ok := configMap["language"]; ok {
			json.Unmarshal(raw, &t.Language)
		}
		if raw, ok := configMap["vip_whitelist"]; ok {
			json.Unmarshal(raw, &t.VIPWhitelist)
		}
	}
	return t
}

// TenantRegistry provides thread-safe tenant lookup by DDI
type TenantRegistry struct {
	mu    sync.RWMutex
	byDDI map[string]*TenantConfig
	byID  map[string]*TenantConfig
	all   []*TenantConfig
}

func NewTenantRegistry(tenants []TenantConfig) *TenantRegistry {
	r := &TenantRegistry{
		byDDI: make(map[string]*TenantConfig),
		byID:  make(map[string]*TenantConfig),
	}
	for i := range tenants {
		t := &tenants[i]
		r.all = append(r.all, t)
		if !t.Enabled {
			continue
		}
		r.byID[t.NotariaID] = t
		for _, ddi := range t.DDIs {
			r.byDDI[ddi] = t
		}
	}
	return r
}

func (r *TenantRegistry) LookupByDDI(ddi string) (*TenantConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.byDDI[ddi]
	return t, ok
}

func (r *TenantRegistry) LookupByID(id string) (*TenantConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.byID[id]
	return t, ok
}

func (r *TenantRegistry) ListAll() []*TenantConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*TenantConfig, len(r.all))
	copy(out, r.all)
	return out
}

func (r *TenantRegistry) Add(t TenantConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tc := &t
	r.all = append(r.all, tc)
	if tc.Enabled {
		r.byID[tc.NotariaID] = tc
		for _, ddi := range tc.DDIs {
			r.byDDI[ddi] = tc
		}
	}
}

func (r *TenantRegistry) Update(t TenantConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Remove old DDI mappings
	if old, ok := r.byID[t.NotariaID]; ok {
		for _, ddi := range old.DDIs {
			delete(r.byDDI, ddi)
		}
	}
	// Update in-place
	tc := &t
	r.byID[t.NotariaID] = tc
	for i, existing := range r.all {
		if existing.NotariaID == t.NotariaID {
			r.all[i] = tc
			break
		}
	}
	if tc.Enabled {
		for _, ddi := range tc.DDIs {
			r.byDDI[ddi] = tc
		}
	} else {
		delete(r.byID, t.NotariaID)
	}
}

func (r *TenantRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.byID[id]; ok {
		for _, ddi := range old.DDIs {
			delete(r.byDDI, ddi)
		}
		delete(r.byID, id)
	}
	for i, t := range r.all {
		if t.NotariaID == id {
			r.all = append(r.all[:i], r.all[i+1:]...)
			break
		}
	}
}

// IsVIP checks if a caller ID is in the tenant's VIP whitelist
func (t *TenantConfig) IsVIP(callerID string) bool {
	for _, vip := range t.VIPWhitelist {
		if vip == callerID {
			return true
		}
	}
	return false
}

// IsBusinessHours checks if the current time is within business hours for the tenant
func (t *TenantConfig) IsBusinessHours() (bool, string) {
	if len(t.Schedule.BusinessHours) == 0 {
		return true, "no_schedule" // No schedule = always open
	}

	loc := time.UTC
	if t.Schedule.Timezone != "" {
		var err error
		loc, err = time.LoadLocation(t.Schedule.Timezone)
		if err != nil {
			return true, "invalid_timezone" // Fail open
		}
	}

	now := time.Now().In(loc)
	weekday := strings.ToLower(now.Weekday().String()[:3]) // "mon", "tue", etc.
	currentMinutes := now.Hour()*60 + now.Minute()

	for _, hr := range t.Schedule.BusinessHours {
		if !dayInRange(weekday, hr.Days) {
			continue
		}
		startMin := parseTime(hr.Start)
		endMin := parseTime(hr.End)
		if currentMinutes >= startMin && currentMinutes < endMin {
			return true, "business_hours"
		}
	}
	return false, "after_hours"
}

// dayInRange checks if a weekday is in a range like "mon-fri" or "mon,wed,fri"
func dayInRange(day, rangeStr string) bool {
	days := map[string]int{"mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6, "sun": 7}
	dayNum, ok := days[day]
	if !ok {
		return false
	}

	// Handle comma-separated list: "mon,wed,fri"
	if strings.Contains(rangeStr, ",") {
		for _, d := range strings.Split(rangeStr, ",") {
			if strings.TrimSpace(d) == day {
				return true
			}
		}
		return false
	}

	// Handle range: "mon-fri"
	parts := strings.SplitN(rangeStr, "-", 2)
	if len(parts) == 2 {
		startNum, ok1 := days[strings.TrimSpace(parts[0])]
		endNum, ok2 := days[strings.TrimSpace(parts[1])]
		if ok1 && ok2 {
			return dayNum >= startNum && dayNum <= endNum
		}
	}

	// Single day: "mon"
	return strings.TrimSpace(rangeStr) == day
}

// parseTime parses "HH:MM" to minutes since midnight
func parseTime(t string) int {
	parts := strings.SplitN(t, ":", 2)
	if len(parts) != 2 {
		return 0
	}
	h := 0
	m := 0
	fmt.Sscanf(parts[0], "%d", &h)
	fmt.Sscanf(parts[1], "%d", &m)
	return h*60 + m
}

// Load reads and parses the YAML configuration file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	// Defaults
	if cfg.Server.MaxConcurrent == 0 {
		cfg.Server.MaxConcurrent = 50
	}
	if cfg.Audio.SampleRate == 0 {
		cfg.Audio.SampleRate = 16000
	}
	if cfg.Audio.BitDepth == 0 {
		cfg.Audio.BitDepth = 16
	}
	if cfg.Audio.Channels == 0 {
		cfg.Audio.Channels = 1
	}
	if cfg.Audio.FrameSizeMs == 0 {
		cfg.Audio.FrameSizeMs = 20
	}
	if cfg.AI.TimeoutSec == 0 {
		cfg.AI.TimeoutSec = 3
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "/opt/audio-bridge/data/bridge.db"
	}
	if cfg.Recording.Path == "" {
		cfg.Recording.Path = "/opt/audio-bridge/recordings"
	}
	if cfg.Backoffice.CacheTTL == 0 {
		cfg.Backoffice.CacheTTL = 300
	}
	if cfg.AI.OriginateRetries == 0 {
		cfg.AI.OriginateRetries = 3
	}
	if cfg.AI.OriginateRetryIntervalSec == 0 {
		cfg.AI.OriginateRetryIntervalSec = 30
	}
	if cfg.Webhook.TimeoutSec == 0 {
		cfg.Webhook.TimeoutSec = 10
	}
	if cfg.Webhook.RetryCount == 0 {
		cfg.Webhook.RetryCount = 3
	}
	if cfg.Lakimi.FrameSizeMs == 0 {
		cfg.Lakimi.FrameSizeMs = 30
	}
	if cfg.Lakimi.PingInterval == 0 {
		cfg.Lakimi.PingInterval = 30
	}
	if cfg.Lakimi.ReconnectMaxRetries == 0 {
		cfg.Lakimi.ReconnectMaxRetries = 5
	}
	return &cfg, nil
}
