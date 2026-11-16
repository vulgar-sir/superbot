package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"superbot/internal/dockerproxy"
)

// runDockerProxy serves the allowlisting Docker Engine proxy the worker uses
// instead of the raw socket. It needs none of the application config, so it
// runs without the database URL or secret key.
func runDockerProxy() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	cfg, err := dockerproxy.ConfigFromEnv()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return dockerproxy.ListenAndServe(ctx, cfg, logger)
}
