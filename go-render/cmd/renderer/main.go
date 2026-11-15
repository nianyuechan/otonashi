package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/nianyuechan/otonashi/go-render/internal/httpapi"
	"github.com/nianyuechan/otonashi/go-render/internal/renderer"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	chromium := flag.String("chromium", os.Getenv("CHROMIUM_PATH"), "path to an existing Chromium/Chrome/Edge executable (or CHROMIUM_PATH)")
	concurrency := flag.Int("concurrency", 2, "number of pooled pages and maximum concurrent renders")
	queueSize := flag.Int("queue-size", 16, "maximum number of queued requests")
	maxTimeout := flag.Duration("max-timeout", 30*time.Second, "maximum per-request queue plus render timeout")
	browserStartTimeout := flag.Duration("browser-start-timeout", 20*time.Second, "browser and page startup timeout")
	bodyLimit := flag.Int64("body-limit", 4<<20, "maximum JSON request body size in bytes")
	shutdownTimeout := flag.Duration("shutdown-timeout", 15*time.Second, "graceful shutdown timeout")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if *chromium == "" || *concurrency <= 0 || *queueSize < 0 || *maxTimeout <= 0 || *browserStartTimeout <= 0 || *bodyLimit <= 0 || *shutdownTimeout <= 0 {
		logger.Error("invalid configuration",
			"chromium_required", *chromium == "",
			"concurrency", *concurrency,
			"queue_size", *queueSize,
			"max_timeout", *maxTimeout,
			"browser_start_timeout", *browserStartTimeout,
			"body_limit", *bodyLimit,
			"shutdown_timeout", *shutdownTimeout,
		)
		os.Exit(2)
	}

	registry := prometheus.NewRegistry()
	metrics := renderer.NewMetrics(registry)
	service := renderer.NewService(func(ctx context.Context) (renderer.Browser, error) {
		return renderer.NewChromedpBrowser(ctx, *chromium)
	}, renderer.Config{
		Concurrency:         *concurrency,
		QueueSize:           *queueSize,
		BrowserStartTimeout: *browserStartTimeout,
	}, logger, metrics)

	startCtx, cancelStart := context.WithTimeout(context.Background(), *browserStartTimeout+time.Duration(*concurrency)*5*time.Second)
	if err := service.Start(startCtx); err != nil {
		cancelStart()
		logger.Error("renderer startup failed", "error", err)
		os.Exit(1)
	}
	cancelStart()

	server := &http.Server{
		Addr:              *addr,
		Handler:           httpapi.NewHandler(service, httpapi.Config{MaxTimeout: *maxTimeout, BodyLimit: *bodyLimit, Logger: logger, Registry: registry}),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	logger.Info("HTTP server listening", "addr", *addr)

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	var serveErr error
	select {
	case <-signalCtx.Done():
		logger.Info("shutdown signal received")
	case serveErr = <-serverErrors:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("HTTP server stopped", "error", serveErr)
		}
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), *shutdownTimeout)
	defer cancelShutdown()
	shutdownErr := server.Shutdown(shutdownCtx)
	closeErr := service.Close(shutdownCtx)
	if shutdownErr != nil || closeErr != nil || (serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed)) {
		logger.Error("renderer stopped with errors", "http_shutdown_error", shutdownErr, "service_close_error", closeErr, "serve_error", serveErr)
		os.Exit(1)
	}
	logger.Info("renderer stopped")
}
