package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"superbot/internal/agent"
	"superbot/internal/auth"
	"superbot/internal/chat"
	"superbot/internal/config"
	"superbot/internal/connectors"
	"superbot/internal/emailtrigger"
	"superbot/internal/events"
	"superbot/internal/httpapi"
	"superbot/internal/jobs"
	"superbot/internal/llm"
	"superbot/internal/memory"
	"superbot/internal/push"
	routinesvc "superbot/internal/routines"
	"superbot/internal/sandbox"
	"superbot/internal/secrets"
	"superbot/internal/seed"
	"superbot/internal/skills"
	"superbot/internal/store"
	"superbot/internal/tunnel"
)

func main() {
	if err := run(); err != nil {
		slog.Error("superbot exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]
	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}
	if command == "genkey" {
		return generateKey()
	}
	if command == "device" {
		return runDevice(args[1:])
	}
	if command == "mcp-echo" {
		return runMCPEcho()
	}
	if command == "docker-proxy" {
		return runDockerProxy()
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := newLogger(cfg)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch command {
	case "migrate":
		if err := store.Migrate(ctx, cfg.DatabaseURL); err != nil {
			return err
		}
		logger.Info("migrations applied")
		return nil
	case "serve":
		return runServe(ctx, cfg, logger)
	case "worker":
		return runWorker(ctx, cfg, logger)
	case "reembed-memory":
		return runReembedMemory(ctx, cfg, logger)
	default:
		return fmt.Errorf("unknown command %q (want serve, worker, migrate, docker-proxy or genkey)", command)
	}
}

// runReembedMemory moves stored memory vectors into the configured embedding
// provider's vector space. Rows keep their old embedding_model until they are
// re-embedded, and recall only compares vectors from one space.
func runReembedMemory(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if strings.TrimSpace(cfg.EmbeddingAPIKey) == "" {
		return errors.New("SUPERBOT_EMBEDDING_API_KEY is required to re-embed memories")
	}
	if err := store.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return err
	}
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	service := memory.NewService(db, cfg.EmbeddingAPIKey, cfg.EmbeddingBaseURL, cfg.EmbeddingModel)
	target := service.ModelName()
	total := 0
	for {
		rows, err := db.ListMemoriesNeedingReembed(ctx, store.ListMemoriesNeedingReembedParams{
			EmbeddingModel: target, MaxRows: 100,
		})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			vector, err := service.EmbedText(ctx, row.Content)
			if err != nil {
				return fmt.Errorf("re-embed memory %s: %w", row.ID, err)
			}
			if err := db.SetMemoryEmbedding(ctx, store.SetMemoryEmbeddingParams{
				ID: row.ID, OwnerID: row.OwnerID, Embedding: vector, EmbeddingModel: target,
			}); err != nil {
				return err
			}
			total++
		}
		logger.Info("re-embedded memories", "count", total, "model", target)
	}
	logger.Info("memory re-embed complete", "count", total, "model", target)
	return nil
}

func runServe(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if err := store.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return err
	}
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	box, err := secrets.New(cfg.SecretKey)
	if err != nil {
		return err
	}
	authService := auth.NewService(db.Queries, cfg.SessionLifetime)
	if err := authService.EnsureAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword, "Admin"); err != nil {
		return err
	}
	if admin, err := db.GetUserByEmail(ctx, cfg.AdminEmail); err != nil {
		logger.Warn("admin user not found; skipping provider seed", "email", cfg.AdminEmail)
	} else {
		if cfg.DeepSeekAPIKey != "" {
			if err := seed.EnsureDeepSeek(ctx, db, box, admin.ID, cfg.DeepSeekAPIKey); err != nil {
				logger.Warn("deepseek bootstrap failed", "error", err)
			} else {
				logger.Info("deepseek provider is available")
			}
		}
		if err := skills.NewService(db, cfg.DataDir, logger).EnsureBuiltins(ctx, admin.ID); err != nil {
			logger.Warn("builtin skills bootstrap failed", "error", err)
		} else {
			logger.Info("builtin skills are available")
		}
	}

	bus := events.NewBus(db.Pool, logger)
	go bus.Run(ctx)
	chatService := chat.NewService(db, bus, logger)
	factory := llm.NewFactory(box)
	api := httpapi.NewServer(cfg, db, authService, box, chatService, bus, factory, logger)

	go cleanupSessions(ctx, authService, logger)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("api listening", "addr", cfg.HTTPAddr)
		errCh <- server.ListenAndServe()
	}()
	var proxyServer *http.Server
	if cfg.TunnelToken != "" {
		socksServer := tunnel.NewSocksServer(api.TunnelHub(), cfg.TunnelToken)
		go func() {
			logger.Info("socks5 proxy listening", "addr", cfg.SocksAddr)
			if err := socksServer.ListenAndServe(ctx, cfg.SocksAddr); err != nil {
				logger.Error("socks5 proxy stopped", "error", err)
			}
		}()
		proxyServer = &http.Server{
			Addr:              cfg.EgressAddr,
			Handler:           api.ProxyHandler(),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			logger.Info("egress proxy listening", "addr", cfg.EgressAddr)
			if err := proxyServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("egress proxy stopped", "error", err)
			}
		}()
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		logger.Info("shutting down api")
		if proxyServer != nil {
			_ = proxyServer.Shutdown(shutdownCtx)
		}
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func runWorker(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if err := store.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return err
	}
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	box, err := secrets.New(cfg.SecretKey)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	factory := llm.NewFactory(box)
	bus := events.NewBus(db.Pool, logger)

	var sandboxManager *sandbox.Manager
	sm, err := sandbox.New(cfg, db, logger)
	if err != nil {
		logger.Warn("docker sandbox unavailable; shell/python tools are disabled", "error", err)
	} else {
		pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
		pingErr := sm.Ping(pingCtx)
		pingCancel()
		if pingErr != nil {
			logger.Warn("docker sandbox unavailable; shell/python tools are disabled", "error", pingErr)
			_ = sm.Close()
		} else {
			sandboxManager = sm
			defer sm.Close()
		}
	}

	engine := agent.NewEngine(db, factory, bus, cfg, sandboxManager, box, logger)
	worker := jobs.NewWorker(db, engine, logger)

	if pushService, err := push.NewService(db, cfg.DataDir, logger); err != nil {
		logger.Warn("web push disabled", "error", err)
	} else {
		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					pushService.NotifyUnpushed(ctx)
				}
			}
		}()
	}

	routineService := routinesvc.NewService(db, bus, logger)
	connectorService := connectors.NewService(db, box, logger)
	emailTriggers := emailtrigger.NewService(db, connectorService, routineService, logger)
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				emailTriggers.CheckOnce(ctx)
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		routineService.ReconcileDue(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				routineService.ReconcileDue(ctx)
			}
		}
	}()

	if sandboxManager != nil {
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					sandboxManager.Reconcile(ctx)
				}
			}
		}()
	}
	return worker.Run(ctx)
}

func cleanupSessions(ctx context.Context, authService *auth.Service, logger *slog.Logger) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := authService.DeleteExpiredSessions(ctx); err != nil {
				logger.Warn("delete expired sessions failed", "error", err)
			}
		}
	}
}

func newLogger(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if cfg.IsDevelopment() {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}

func generateKey() error {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(buf))
	return nil
}
