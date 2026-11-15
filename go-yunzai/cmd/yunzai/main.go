package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nianyuechan/otonashi/go-yunzai/internal/app"
	"github.com/nianyuechan/otonashi/go-yunzai/internal/config"
	"github.com/nianyuechan/otonashi/go-yunzai/internal/observability"
)

func main() {
	if err := run(); err != nil {
		slog.Error("yunzai stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "config.yaml", "path to YAML configuration")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	pprofStop, err := observability.StartPprofFromEnv()
	if err != nil {
		return fmt.Errorf("start pprof: %w", err)
	}
	defer pprofStop()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return app.Run(ctx, cfg)
}
