package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"github.com/smartgroup/audio-bridge/internal/bridge"
	"github.com/smartgroup/audio-bridge/internal/config"
	"github.com/smartgroup/audio-bridge/internal/db"
	"github.com/smartgroup/audio-bridge/internal/dialplan"
	"github.com/smartgroup/audio-bridge/internal/models"
	"github.com/smartgroup/audio-bridge/internal/webhook"
)

// rateLimiter implements a simple per-IP token bucket rate limiter
type rateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     int           // tokens per interval
	interval time.Duration // refill interval
	burst    int           // max tokens
}

type visitor struct {
	tokens   int
	lastSeen time.Time
}

func newRateLimiter(rate int, interval time.Duration, burst int) *rateLimiter {
	rl := &rateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate,
		interval: interval,
		burst:    burst,
	}
	// Cleanup stale visitors every minute
	go func() {
		for {
			time.Sleep(time.Minute)
			rl.mu.Lock()
			for ip, v := range rl.visitors {
				if time.Since(v.lastSeen) > 5*time.Minute {
					delete(rl.visitors, ip)
				}
			}
			rl.mu.Unlock()
		}
	}()
	return rl
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	if !exists {
		rl.visitors[ip] = &visitor{tokens: rl.burst - 1, lastSeen: time.Now()}
		return true
	}

	// Refill tokens based on elapsed time
	elapsed := time.Since(v.lastSeen)
	refill := int(elapsed / rl.interval) * rl.rate
	v.tokens += refill
	if v.tokens > rl.burst {
		v.tokens = rl.burst
	}
	v.lastSeen = time.Now()

	if v.tokens <= 0 {
		return false
	}
	v.tokens--
	return true
}

// Server provides the REST API for external systems and admin panel
type Server struct {
	bridge        *bridge.Bridge
	calls         *models.CallRegistry
	db            *db.DB
	tenants       *config.TenantRegistry
	sseHub        *models.SSEHub
	cfg           *config.Config
	apiKey        string
	logger        *zap.Logger
	router        *gin.Engine
	tokens        map[string]time.Time // simple token store
	limiter       *rateLimiter
	provisioner   *dialplan.Provisioner
	webhookClient *webhook.Client
}

func NewServer(b *bridge.Bridge, calls *models.CallRegistry, database *db.DB, tenants *config.TenantRegistry, sseHub *models.SSEHub, cfg *config.Config, webhookClient *webhook.Client, logger *zap.Logger) *Server {
	gin.SetMode(gin.ReleaseMode)

	s := &Server{
		bridge:        b,
		calls:         calls,
		db:            database,
		tenants:       tenants,
		sseHub:        sseHub,
		cfg:           cfg,
		apiKey:        cfg.API.APIKey,
		logger:        logger,
		router:        gin.New(),
		tokens:        make(map[string]time.Time),
		limiter:       newRateLimiter(1, time.Second, 30), // 1 token/sec, burst 30
		provisioner:   dialplan.NewProvisioner("", logger.Named("dialplan")),
		webhookClient: webhookClient,
	}

	// Restore sessions from DB so tokens survive restarts
	if saved, err := database.LoadTokens(); err == nil && len(saved) > 0 {
		s.tokens = saved
		logger.Info("Restored admin sessions from DB", zap.Int("count", len(saved)))
	}

	// Periodic cleanup of expired tokens (every 10 minutes)
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			if n, err := database.DeleteExpiredTokens(); err == nil && n > 0 {
				logger.Info("Cleaned expired sessions", zap.Int64("removed", n))
			}
		}
	}()

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.router.Use(gin.Recovery())
	s.router.Use(s.requestLogger())
	s.router.Use(s.corsMiddleware())

	// Health check (no auth)
	s.router.GET("/health", s.healthCheck)

	// --- API v1 (API key auth — existing external callers) ---
	v1 := s.router.Group("/api/v1")
	v1.Use(s.rateLimitMiddleware())
	v1.Use(s.apiKeyMiddleware())
	{
		v1.POST("/calls/precreate", s.precreateCall)
		v1.POST("/calls/outbound", s.originateCall)
		v1.GET("/calls/:call_id/status", s.getCallStatus)
		v1.GET("/calls/active", s.listActiveCalls)
		v1.GET("/stats", s.getStats)
		v1.POST("/calls/:call_id/transfer", s.transferCall)
		v1.POST("/calls/:call_id/hangup", s.hangupCall)
		v1.GET("/routing/check", s.checkRouting)
		v1.GET("/calls/:call_id/recording", s.downloadRecording)
	}

	// --- Admin Auth ---
	s.router.POST("/api/v1/auth/login", s.adminLogin)

	// --- Admin API (token auth) ---
	admin := s.router.Group("/api/v1/admin")
	admin.Use(s.rateLimitMiddleware())
	admin.Use(s.adminAuthMiddleware())
	{
		// Tenants CRUD
		admin.GET("/tenants", s.listTenants)
		admin.POST("/tenants", s.createTenant)
		admin.PUT("/tenants/:id", s.updateTenant)
		admin.DELETE("/tenants/:id", s.deleteTenant)

		// Call history (CDR)
		admin.GET("/calls", s.listCallHistory)
		admin.GET("/calls/:id", s.getCallDetail)

		// Recordings
		admin.GET("/recordings/:call_id/:channel", s.streamRecording)

		// Live sessions (SSE)
		admin.GET("/sessions/live", s.sseHandler)

		// Dashboard stats
		admin.GET("/stats", s.dashboardStats)

		// Logs
		admin.GET("/logs/interactions", s.listInteractionLogs)
		admin.GET("/logs/system", s.listSystemLogs)

		// Config
		admin.GET("/config", s.getConfig)
		admin.PUT("/config", s.updateConfig)
	}

	// Serve Vue admin panel static files
	distPath := filepath.Join("web", "dist")
	if _, err := os.Stat(distPath); err == nil {
		s.router.Static("/admin/assets", filepath.Join(distPath, "assets"))
		s.router.StaticFile("/admin/favicon.ico", filepath.Join(distPath, "favicon.ico"))
		// SPA fallback: serve index.html for all /admin/* routes
		s.router.NoRoute(func(c *gin.Context) {
			if strings.HasPrefix(c.Request.URL.Path, "/admin") {
				c.File(filepath.Join(distPath, "index.html"))
				return
			}
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		})
	}
}

func (s *Server) Start(addr string) error {
	s.logger.Info("API server starting", zap.String("addr", addr))
	return s.router.Run(addr)
}

// =============================================================================
// Existing API handlers (API key auth)
// =============================================================================

func (s *Server) healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":       "ok",
		"active_calls": s.calls.Count(),
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) precreateCall(c *gin.Context) {
	var req struct {
		UUID      string `json:"uuid" form:"uuid" binding:"required"`
		NotariaID string `json:"notaria_id" form:"notaria_id" binding:"required"`
		CallerID  string `json:"caller_id" form:"caller_id"`
		DDI       string `json:"ddi" form:"ddi"`
		Channel   string `json:"channel" form:"channel"`
		Direction string `json:"direction" form:"direction"`
	}
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dir := models.CallInbound
	if req.Direction == "outbound" {
		dir = models.CallOutbound
	}

	call := &models.Call{
		ID:              req.UUID,
		CallerID:        req.CallerID,
		DDI:             req.DDI,
		NotariaID:       req.NotariaID,
		Direction:       dir,
		State:           models.CallStateRinging,
		CallType:        "inbound",
		AsteriskChannel: req.Channel,
		StartTime:       time.Now(),
	}
	s.calls.Add(call)

	s.logger.Info("Call precreated",
		zap.String("uuid", req.UUID),
		zap.String("notaria_id", req.NotariaID),
		zap.String("caller_id", req.CallerID),
		zap.String("ddi", req.DDI))

	c.JSON(http.StatusOK, gin.H{"status": "precreated", "uuid": req.UUID})
}

func (s *Server) originateCall(c *gin.Context) {
	var req models.OutboundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	call, err := s.bridge.OriginateOutbound(req)
	if err != nil {
		s.logger.Error("Failed to originate call", zap.Error(err))
		c.JSON(http.StatusInternalServerError, models.OutboundResponse{
			CallID:  call.ID,
			Status:  "failed",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusAccepted, models.OutboundResponse{
		CallID:  call.ID,
		Status:  "queued",
		Message: "Call originated successfully",
	})
}

func (s *Server) getCallStatus(c *gin.Context) {
	callID := c.Param("call_id")
	call, ok := s.calls.Get(callID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "call not found"})
		return
	}

	c.JSON(http.StatusOK, models.CallStatusResponse{
		CallID:          call.ID,
		State:           call.GetState(),
		Direction:       string(call.Direction),
		CallerID:        call.CallerID,
		NotariaID:       call.NotariaID,
		StartTime:       call.StartTime.Format(time.RFC3339),
		DurationSeconds: call.Duration,
		EndReason:       call.EndReason,
	})
}

func (s *Server) listActiveCalls(c *gin.Context) {
	calls := s.calls.List()
	active := make([]models.CallStatusResponse, 0)
	for _, call := range calls {
		if call.GetState() != models.CallStateCompleted {
			active = append(active, models.CallStatusResponse{
				CallID:    call.ID,
				State:     call.GetState(),
				Direction: string(call.Direction),
				CallerID:  call.CallerID,
				NotariaID: call.NotariaID,
				StartTime: call.StartTime.Format(time.RFC3339),
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"count": len(active),
		"calls": active,
	})
}

func (s *Server) getStats(c *gin.Context) {
	allCalls := s.calls.List()
	active := 0
	completed := 0
	inbound := 0
	outbound := 0
	for _, call := range allCalls {
		if call.GetState() != models.CallStateCompleted {
			active++
		} else {
			completed++
		}
		if call.Direction == models.CallInbound {
			inbound++
		} else {
			outbound++
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"active_calls":    active,
		"completed_calls": completed,
		"inbound_total":   inbound,
		"outbound_total":  outbound,
		"timestamp":       time.Now().UTC().Format(time.RFC3339),
	})
}

// =============================================================================
// Routing Check (API key auth — used by Asterisk dialplan before AudioSocket)
// =============================================================================

// checkRouting determines routing action for an incoming call.
// Returns pipe-separated text for easy Asterisk dialplan parsing:
// action|notaria_id|transfer_dest|schedule
// Actions: "ai" (send to AI), "vip" (VIP direct), "closed" (after hours), "direct" (bypass)
func (s *Server) checkRouting(c *gin.Context) {
	ddi := c.Query("ddi")
	callerID := c.Query("caller_id")

	if ddi == "" {
		c.String(http.StatusBadRequest, "ai|||unknown")
		return
	}

	// Lookup tenant by DDI
	tenant, ok := s.tenants.LookupByDDI(ddi)
	if !ok {
		// No tenant found — fallback to AI
		s.logger.Debug("Routing: no tenant for DDI, fallback to AI", zap.String("ddi", ddi))
		c.String(http.StatusOK, "ai|||unknown")
		return
	}

	notariaID := tenant.NotariaID
	transferDest := tenant.Transfers.Default

	// Check VIP whitelist
	if callerID != "" && tenant.IsVIP(callerID) {
		s.logger.Info("Routing: VIP caller detected",
			zap.String("ddi", ddi),
			zap.String("caller_id", callerID),
			zap.String("notaria_id", notariaID))

		// Webhook: call.routed (VIP)
		if s.webhookClient != nil {
			s.webhookClient.Send(webhook.Payload{
				Event:         webhook.EventCallRouted,
				InteractionID: fmt.Sprintf("routed-%s-%d", callerID, time.Now().Unix()),
				NotariaID:     notariaID,
				CallerID:      callerID,
				DDI:           ddi,
				Direction:     "inbound",
				Result:        "vip",
				Reason:        "vip",
				Schedule:      "vip",
				TransferDest:  transferDest,
				Timestamp:     time.Now().UTC().Format(time.RFC3339),
			})
		}

		c.String(http.StatusOK, "vip|%s|%s|vip", notariaID, transferDest)
		return
	}

	// Check business hours
	inHours, schedule := tenant.IsBusinessHours()
	if !inHours {
		s.logger.Info("Routing: outside business hours",
			zap.String("ddi", ddi),
			zap.String("notaria_id", notariaID),
			zap.String("schedule", schedule))

		// Webhook: call.routed (after hours)
		if s.webhookClient != nil {
			s.webhookClient.Send(webhook.Payload{
				Event:         webhook.EventCallRouted,
				InteractionID: fmt.Sprintf("routed-%s-%d", callerID, time.Now().Unix()),
				NotariaID:     notariaID,
				CallerID:      callerID,
				DDI:           ddi,
				Direction:     "inbound",
				Result:        "closed",
				Reason:        "after_hours",
				Schedule:      schedule,
				TransferDest:  transferDest,
				Timestamp:     time.Now().UTC().Format(time.RFC3339),
			})
		}

		c.String(http.StatusOK, "closed|%s|%s|%s", notariaID, transferDest, schedule)
		return
	}

	// Normal: send to AI
	c.String(http.StatusOK, "ai|%s||%s", notariaID, schedule)
}

// =============================================================================
// Recording Download (API key auth — used by CTN)
// =============================================================================

// downloadRecording serves a call recording file.
// Query params: format (mp3/wav, default mp3), channel (mixed/caller/ai, default mixed)
func (s *Server) downloadRecording(c *gin.Context) {
	callID := c.Param("call_id")
	format := c.DefaultQuery("format", "mp3")
	channel := c.DefaultQuery("channel", "mixed")

	call, err := s.db.GetCall(callID)
	if err != nil || call == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "call not found"})
		return
	}

	var filePath string
	var contentType string

	switch format {
	case "mp3":
		contentType = "audio/mpeg"
		switch channel {
		case "mixed":
			filePath = call.RecordingMixedMP3
		case "caller":
			filePath = call.RecordingCallerMP3
		case "ai":
			filePath = call.RecordingAIMP3
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "channel must be 'mixed', 'caller', or 'ai'"})
			return
		}

		// If MP3 not ready yet but WAV exists, return 202
		if filePath == "" {
			wavPath := ""
			switch channel {
			case "caller":
				wavPath = call.RecordingCaller
			case "ai":
				wavPath = call.RecordingAI
			}
			if wavPath != "" {
				if _, err := os.Stat(wavPath); err == nil {
					c.JSON(http.StatusAccepted, gin.H{"status": "processing", "message": "MP3 conversion in progress, try again shortly"})
					return
				}
			}
		}

	case "wav":
		contentType = "audio/wav"
		switch channel {
		case "caller":
			filePath = call.RecordingCaller
		case "ai":
			filePath = call.RecordingAI
		case "mixed":
			// For mixed WAV, we'd need to mix on-the-fly or check if a mixed WAV exists
			// For now, return error suggesting MP3 format for mixed
			c.JSON(http.StatusBadRequest, gin.H{"error": "mixed channel only available in MP3 format, use format=mp3"})
			return
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "channel must be 'caller' or 'ai' for WAV format"})
			return
		}

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "format must be 'mp3' or 'wav'"})
		return
	}

	if filePath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no recording available for this channel/format"})
		return
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "recording file not found on disk"})
		return
	}

	ext := "mp3"
	if format == "wav" {
		ext = "wav"
	}
	filename := fmt.Sprintf("%s_%s.%s", callID[:8], channel, ext)

	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.File(filePath)
}

// =============================================================================
// Call Actions (API key auth — used by external AI like Lakimi/CTN)
// =============================================================================

func (s *Server) transferCall(c *gin.Context) {
	callID := c.Param("call_id")
	var req struct {
		Destination     string `json:"destination" binding:"required"`
		DestinationType string `json:"destination_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := s.bridge.TransferCall(callID, req.Destination, req.DestinationType); err != nil {
		// Distinguish "not found" from "AMI failed"
		if _, ok := s.calls.Get(callID); !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "call not found"})
			return
		}
		s.logger.Error("Transfer via API failed", zap.String("call_id", callID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "transferred",
		"call_id":     callID,
		"destination": req.Destination,
	})
}

func (s *Server) hangupCall(c *gin.Context) {
	callID := c.Param("call_id")
	var req struct {
		Reason string `json:"reason"`
	}
	c.ShouldBindJSON(&req)
	if req.Reason == "" {
		req.Reason = "api_hangup"
	}

	if err := s.bridge.HangupCall(callID, req.Reason); err != nil {
		if _, ok := s.calls.Get(callID); !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "call not found"})
			return
		}
		s.logger.Error("Hangup via API failed", zap.String("call_id", callID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "hung_up",
		"call_id": callID,
		"reason":  req.Reason,
	})
}

// =============================================================================
// Admin Auth
// =============================================================================

func (s *Server) adminLogin(c *gin.Context) {
	var req struct {
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password required"})
		return
	}

	// Prefer bcrypt hash, fall back to plaintext comparison
	if s.cfg.Admin.PasswordHash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(s.cfg.Admin.PasswordHash), []byte(req.Password)); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid password"})
			return
		}
	} else if req.Password != s.cfg.Admin.Password {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid password"})
		return
	}

	token := generateToken()
	expires := time.Now().Add(24 * time.Hour)
	s.tokens[token] = expires

	// Persist to DB so token survives server restarts
	if err := s.db.SaveToken(token, expires); err != nil {
		s.logger.Warn("Failed to persist session token", zap.Error(err))
	}

	c.JSON(http.StatusOK, gin.H{"token": token})
}

// =============================================================================
// Admin Tenants CRUD
// =============================================================================

func (s *Server) listTenants(c *gin.Context) {
	tenants := s.tenants.ListAll()
	c.JSON(http.StatusOK, gin.H{"tenants": tenants})
}

func (s *Server) createTenant(c *gin.Context) {
	var t config.TenantConfig
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if t.NotariaID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "notaria_id required"})
		return
	}

	// Save to DB
	if s.db != nil {
		if err := s.db.InsertTenant(t); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// Update in-memory registry
	s.tenants.Add(t)

	// Auto-provision dialplan if company_id is set
	dialplanOK := false
	if t.CompanyID != "" {
		if err := s.provisioner.Provision(t, s.apiKey, s.bridge.AMI()); err != nil {
			s.logger.Warn("Dialplan provisioning failed (tenant created, provision manually)",
				zap.String("notaria_id", t.NotariaID),
				zap.String("company_id", t.CompanyID),
				zap.Error(err))
		} else {
			dialplanOK = true
		}
	}

	s.logger.Info("Tenant created", zap.String("notaria_id", t.NotariaID), zap.String("company_id", t.CompanyID))
	c.JSON(http.StatusCreated, gin.H{"status": "created", "notaria_id": t.NotariaID, "dialplan_provisioned": dialplanOK})
}

func (s *Server) updateTenant(c *gin.Context) {
	notariaID := c.Param("id")

	var t config.TenantConfig
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t.NotariaID = notariaID

	if s.db != nil {
		if err := s.db.UpdateTenant(t); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	s.tenants.Update(t)

	// Re-provision dialplan if company_id is set
	dialplanOK := false
	if t.CompanyID != "" {
		if err := s.provisioner.Provision(t, s.apiKey, s.bridge.AMI()); err != nil {
			s.logger.Warn("Dialplan re-provisioning failed",
				zap.String("notaria_id", notariaID),
				zap.String("company_id", t.CompanyID),
				zap.Error(err))
		} else {
			dialplanOK = true
		}
	}

	s.logger.Info("Tenant updated", zap.String("notaria_id", notariaID), zap.String("company_id", t.CompanyID))
	c.JSON(http.StatusOK, gin.H{"status": "updated", "notaria_id": notariaID, "dialplan_provisioned": dialplanOK})
}

func (s *Server) deleteTenant(c *gin.Context) {
	notariaID := c.Param("id")

	// Read tenant before deleting to get company_id for dialplan cleanup
	var companyID string
	if s.db != nil {
		if existing, err := s.db.GetTenant(notariaID); err == nil && existing != nil {
			companyID = existing.CompanyID
		}
	}

	if s.db != nil {
		if err := s.db.DeleteTenant(notariaID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	s.tenants.Remove(notariaID)

	// Remove dialplan file if it was provisioned
	if companyID != "" {
		if err := s.provisioner.Deprovision(companyID, s.bridge.AMI()); err != nil {
			s.logger.Warn("Dialplan deprovision failed",
				zap.String("notaria_id", notariaID),
				zap.String("company_id", companyID),
				zap.Error(err))
		}
	}

	s.logger.Info("Tenant deleted", zap.String("notaria_id", notariaID), zap.String("company_id", companyID))
	c.JSON(http.StatusOK, gin.H{"status": "deleted", "notaria_id": notariaID})
}

// =============================================================================
// Admin Call History
// =============================================================================

func (s *Server) listCallHistory(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	notariaID := c.Query("notaria_id")
	from := c.Query("from")
	to := c.Query("to")

	calls, total, err := s.db.ListCalls(page, limit, notariaID, from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"calls": calls,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (s *Server) getCallDetail(c *gin.Context) {
	callID := c.Param("id")

	call, err := s.db.GetCall(callID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if call == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "call not found"})
		return
	}

	logs, _ := s.db.GetCallLogs(callID)

	c.JSON(http.StatusOK, gin.H{
		"call": call,
		"logs": logs,
	})
}

// =============================================================================
// Admin Recordings
// =============================================================================

func (s *Server) streamRecording(c *gin.Context) {
	callID := c.Param("call_id")
	channel := c.Param("channel") // "caller" or "ai"

	call, err := s.db.GetCall(callID)
	if err != nil || call == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "call not found"})
		return
	}

	var filePath string
	switch channel {
	case "caller":
		filePath = call.RecordingCaller
	case "ai":
		filePath = call.RecordingAI
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel must be 'caller' or 'ai'"})
		return
	}

	if filePath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no recording available"})
		return
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "recording file not found"})
		return
	}

	c.Header("Content-Type", "audio/wav")
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=%s_%s.wav", callID[:8], channel))
	c.File(filePath)
}

// =============================================================================
// Admin Logs
// =============================================================================

func (s *Server) listInteractionLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	callID := c.Query("call_id")
	direction := c.Query("direction")
	eventType := c.Query("event_type")
	from := c.Query("from")
	to := c.Query("to")

	logs, total, err := s.db.ListInteractionLogs(page, limit, callID, direction, eventType, from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if logs == nil {
		logs = []db.InteractionLog{}
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs, "total": total, "page": page})
}

func (s *Server) listSystemLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	level := c.Query("level")
	from := c.Query("from")
	to := c.Query("to")

	logs, total, err := s.db.ListSystemLogs(page, limit, level, from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if logs == nil {
		logs = []db.SystemLogRecord{}
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs, "total": total, "page": page})
}

// =============================================================================
// Admin SSE (Server-Sent Events)
// =============================================================================

func (s *Server) sseHandler(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	ch := s.sseHub.Subscribe()
	defer s.sseHub.Unsubscribe(ch)

	// Flush headers immediately so the client knows we're connected
	c.Writer.WriteString(": connected\n\n")
	c.Writer.Flush()

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	c.Stream(func(w io.Writer) bool {
		select {
		case event, ok := <-ch:
			if !ok {
				return false
			}
			data, _ := json.Marshal(event)
			c.SSEvent(event.Type, string(data))
			return true
		case <-heartbeat.C:
			c.Writer.WriteString(": heartbeat\n\n")
			c.Writer.Flush()
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}

// =============================================================================
// Admin Dashboard Stats
// =============================================================================

func (s *Server) dashboardStats(c *gin.Context) {
	stats, err := s.db.GetDashboardStats(s.calls.ActiveCount())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// =============================================================================
// Admin Config
// =============================================================================

func (s *Server) getConfig(c *gin.Context) {
	// Return safe config (no secrets)
	c.JSON(http.StatusOK, gin.H{
		"server":    s.cfg.Server,
		"recording": s.cfg.Recording,
		"ai": gin.H{
			"type":     s.cfg.AI.Type,
			"model":    s.cfg.AI.Model,
			"voice":    s.cfg.AI.Voice,
			"language": s.cfg.AI.Language,
			"originate_retries": s.cfg.AI.OriginateRetries,
		},
		"audio":   s.cfg.Audio,
		"logging": s.cfg.Logging,
	})
}

func (s *Server) updateConfig(c *gin.Context) {
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Only allow safe config updates
	if rec, ok := updates["recording"].(map[string]interface{}); ok {
		if enabled, ok := rec["enabled"].(bool); ok {
			s.cfg.Recording.Enabled = enabled
		}
	}
	if logging, ok := updates["logging"].(map[string]interface{}); ok {
		if level, ok := logging["level"].(string); ok {
			s.cfg.Logging.Level = level
		}
	}

	s.logger.Info("Config updated via admin panel")
	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

// =============================================================================
// Middleware
// =============================================================================

func (s *Server) apiKeyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			apiKey = c.GetHeader("Authorization")
			if len(apiKey) > 7 && apiKey[:7] == "Bearer " {
				apiKey = apiKey[7:]
			}
		}
		if apiKey != s.apiKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing API key"})
			return
		}
		c.Next()
	}
}

func (s *Server) adminAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if len(token) > 7 && strings.HasPrefix(token, "Bearer ") {
			token = token[7:]
		}

		expires, ok := s.tokens[token]
		if !ok || time.Now().After(expires) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

func (s *Server) corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func (s *Server) rateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !s.limiter.allow(ip) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}

func (s *Server) requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		// Don't log SSE connections or static files
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/admin/assets") || path == "/api/v1/admin/sessions/live" {
			return
		}
		s.logger.Info("API request",
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", time.Since(start)),
		)
	}
}

// =============================================================================
// Helpers
// =============================================================================

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
