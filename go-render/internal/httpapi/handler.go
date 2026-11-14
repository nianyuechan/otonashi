package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go-render/pkg/render/api"
)

type Renderer interface {
	Render(context.Context, api.Request) ([]byte, error)
}

type Config struct {
	MaxTimeout time.Duration
	BodyLimit  int64
	Logger     *slog.Logger
	Registry   *prometheus.Registry
}

type handler struct {
	renderer  Renderer
	maxTime   time.Duration
	bodyLimit int64
	logger    *slog.Logger
	requestID atomic.Uint64
}

func NewHandler(renderer Renderer, cfg Config) http.Handler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	h := &handler{
		renderer:  renderer,
		maxTime:   cfg.MaxTimeout,
		bodyLimit: cfg.BodyLimit,
		logger:    cfg.Logger,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/render", h.render)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(cfg.Registry, promhttp.HandlerOpts{}))
	return h.logRequests(mux)
}

func (h *handler) render(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.bodyLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var req api.Request
	if err := decoder.Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, &api.Error{Code: api.CodeInvalidArgument, Message: "request body too large"})
			return
		}
		writeError(w, &api.Error{Code: api.CodeInvalidArgument, Message: "invalid JSON request"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, &api.Error{Code: api.CodeInvalidArgument, Message: "request must contain one JSON object"})
		return
	}
	if err := req.Validate(h.maxTime); err != nil {
		writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(req.Options.TimeoutMS)*time.Millisecond)
	defer cancel()
	image, err := h.renderer.Render(ctx, req)
	if err != nil {
		var apiErr *api.Error
		if !errors.As(err, &apiErr) {
			h.logger.Error("render failed", "error", err)
			apiErr = &api.Error{Code: api.CodeInternal, Message: "render failed"}
		}
		writeError(w, apiErr)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(image)
}

func writeError(w http.ResponseWriter, err *api.Error) {
	status := http.StatusInternalServerError
	switch err.Code {
	case api.CodeInvalidArgument:
		status = http.StatusBadRequest
		if err.Message == "request body too large" {
			status = http.StatusRequestEntityTooLarge
		}
	case api.CodeTimeout:
		status = http.StatusGatewayTimeout
	case api.CodePageCrashed, api.CodeBrowserUnavailable:
		status = http.StatusBadGateway
	case api.CodeQueueFull:
		status = http.StatusTooManyRequests
	case api.CodeUnavailable:
		status = http.StatusServiceUnavailable
	case api.CodeInternal:
		err = &api.Error{Code: api.CodeInternal, Message: "render failed"}
	}
	writeJSON(w, status, api.ErrorResponse{Error: err})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (h *handler) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		h.logger.Info("http request",
			"request_id", fmt.Sprintf("r-%d", h.requestID.Add(1)),
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}
