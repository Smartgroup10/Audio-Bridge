package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/smartgroup/audio-bridge/internal/ami"
	"github.com/smartgroup/audio-bridge/internal/api"
	"github.com/smartgroup/audio-bridge/internal/audiosocket"
	"github.com/smartgroup/audio-bridge/internal/bridge"
	"github.com/smartgroup/audio-bridge/internal/config"
	"github.com/smartgroup/audio-bridge/internal/db"
	"github.com/smartgroup/audio-bridge/internal/models"
	"github.com/smartgroup/audio-bridge/internal/recording"
	"github.com/smartgroup/audio-bridge/internal/webhook"
	"github.com/smartgroup/audio-bridge/internal/wssclient"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		panic("Failed to load config: " + err.Error())
	}

	// Setup base logger (stdout only, needed before DB is ready)
	baseLogger := setupLogger(cfg.Logging)

	// Initialize database (using base logger for now)
	database, err := db.New(cfg.Database.Path, baseLogger.Named("db"))
	if err != nil {
		baseLogger.Fatal("Failed to initialize database", zap.Error(err))
	}
	defer database.Close()

	// Now attach DB log core so system logs are persisted to SQLite
	dbCore := db.NewLogCore(database.Conn(), zapcore.InfoLevel)
	defer dbCore.Stop()
	logger := baseLogger.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		return zapcore.NewTee(core, dbCore)
	}))
	defer logger.Sync()

	// Daily cleanup of old system logs (>30 days)
	go func() {
		for {
			time.Sleep(24 * time.Hour)
			if n, err := database.PruneSystemLogs(30); err == nil && n > 0 {
				baseLogger.Info("Pruned old system logs", zap.Int64("removed", n))
			}
		}
	}()

	logger.Info("Audio Bridge starting",
		zap.String("audiosocket_addr", cfg.Server.AudioSocketAddr),
		zap.String("api_addr", cfg.API.Addr),
		zap.Int("max_concurrent", cfg.Server.MaxConcurrent))

	// Initialize tenant registry from YAML config
	tenants := config.NewTenantRegistry(cfg.Tenants)
	logger.Info("Tenant registry loaded", zap.Int("tenants", len(cfg.Tenants)))

	// Seed tenants to DB (won't overwrite existing ones)
	if err := database.SyncTenantsFromConfig(cfg.Tenants); err != nil {
		logger.Error("Failed to sync tenants to DB", zap.Error(err))
	}

	// Initialize call registry and SSE hub
	calls := models.NewCallRegistry()
	sseHub := models.NewSSEHub()

	// Create recordings directory if enabled
	if cfg.Recording.Enabled {
		if err := os.MkdirAll(cfg.Recording.Path, 0755); err != nil {
			logger.Fatal("Failed to create recordings directory", zap.Error(err))
		}
		logger.Info("Recording enabled", zap.String("path", cfg.Recording.Path))
	}

	// Connect to Asterisk AMI
	amiClient := ami.NewClient(
		cfg.Asterisk.AMIHost,
		cfg.Asterisk.AMIPort,
		cfg.Asterisk.AMIUser,
		cfg.Asterisk.AMIPassword,
		logger.Named("ami"),
	)
	if err := amiClient.Connect(); err != nil {
		logger.Fatal("Failed to connect to Asterisk AMI", zap.Error(err))
	}
	defer amiClient.Close()
	logger.Info("Connected to Asterisk AMI")

	// Create context with graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start AMI ping loop for keep-alive
	amiDone := make(chan struct{})
	amiClient.StartPingLoop(amiDone, 30*time.Second)
	defer close(amiDone)

	// Check ffmpeg availability (needed for MP3 conversion)
	if !recording.FFmpegAvailable() {
		logger.Warn("ffmpeg not found in PATH — MP3 conversion will not work. Install ffmpeg for recording features.")
	} else {
		logger.Info("ffmpeg available for MP3 conversion")
	}

	// Create webhook client (nil if disabled)
	webhookClient := webhook.NewClient(cfg.Webhook, logger.Named("webhook"))

	// Initialize Lakimi hub if AI type is "lakimi"
	var lakimiHub *wssclient.LakimiHub
	if cfg.AI.Type == "lakimi" {
		if cfg.Lakimi.Endpoint == "" {
			logger.Fatal("Lakimi mode requires lakimi.endpoint in config")
		}
		lakimiHub = wssclient.NewLakimiHub(cfg.Lakimi, logger)
		if err := lakimiHub.Connect(ctx); err != nil {
			logger.Fatal("Failed to connect to Lakimi", zap.Error(err))
		}
		defer lakimiHub.Close()
		logger.Info("Lakimi hub connected",
			zap.String("endpoint", cfg.Lakimi.Endpoint),
			zap.Int("frame_size_ms", cfg.Lakimi.FrameSizeMs))
	}

	// Create the bridge
	b := bridge.New(cfg, tenants, calls, amiClient, database, sseHub, webhookClient, lakimiHub, logger.Named("bridge"))

	// Handle OS signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("Received shutdown signal", zap.String("signal", sig.String()))
		cancel()
	}()

	// Start AudioSocket server
	asServer := audiosocket.NewServer(
		cfg.Server.AudioSocketAddr,
		b.HandleAudioSocket,
		logger.Named("audiosocket"),
	)
	asDone := make(chan struct{})
	go func() {
		if err := asServer.Start(ctx); err != nil {
			logger.Error("AudioSocket server error", zap.Error(err))
		}
		close(asDone)
	}()

	// Start REST API server (with admin panel)
	apiServer := api.NewServer(b, calls, database, tenants, sseHub, cfg, webhookClient, logger.Named("api"))
	go func() {
		if err := apiServer.Start(cfg.API.Addr); err != nil {
			logger.Error("API server error", zap.Error(err))
		}
	}()

	logger.Info("Audio Bridge is running. Press Ctrl+C to stop.")

	// Wait for shutdown signal
	<-ctx.Done()
	logger.Info("Shutting down — draining active calls (max 60s)...")

	// Wait for AudioSocket server to drain active calls, with timeout
	drainTimeout := time.After(60 * time.Second)
	select {
	case <-asDone:
		logger.Info("All active calls drained successfully")
	case <-drainTimeout:
		logger.Warn("Drain timeout reached, forcing shutdown",
			zap.Int("active_calls", calls.ActiveCount()))
	}
}

func setupLogger(cfg config.LoggingConfig) *zap.Logger {
	var level zapcore.Level
	switch cfg.Level {
	case "debug":
		level = zapcore.DebugLevel
	case "info":
		level = zapcore.InfoLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	default:
		level = zapcore.InfoLevel
	}

	var zapCfg zap.Config
	if cfg.Format == "json" {
		zapCfg = zap.NewProductionConfig()
	} else {
		zapCfg = zap.NewDevelopmentConfig()
	}
	zapCfg.Level = zap.NewAtomicLevelAt(level)

	logger, err := zapCfg.Build()
	if err != nil {
		panic("Failed to create logger: " + err.Error())
	}
	return logger
}
