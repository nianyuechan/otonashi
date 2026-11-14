package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go-render/pkg/render/api"
)

type rendererFunc func(context.Context, api.Request) ([]byte, error)

func (f rendererFunc) Render(ctx context.Context, req api.Request) ([]byte, error) {
	return f(ctx, req)
}

func newTestHandler(t *testing.T, renderer Renderer) http.Handler {
	t.Helper()
	return NewHandler(renderer, Config{
		MaxTimeout: 30 * time.Second,
		BodyLimit:  4096,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Registry:   prometheus.NewRegistry(),
	})
}

func TestRenderReturnsPNGAndAppliesDefaults(t *testing.T) {
	var got api.Request
	handler := newTestHandler(t, rendererFunc(func(_ context.Context, req api.Request) ([]byte, error) {
		got = req
		return []byte("png-data"), nil
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/render", bytes.NewBufferString(`{"html":"<p>ok</p>"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", contentType)
	}
	if !bytes.Equal(recorder.Body.Bytes(), []byte("png-data")) {
		t.Fatalf("body = %q, want png-data", recorder.Body.Bytes())
	}
	if got.Options.Width != 800 || got.Options.Height != 600 || got.Options.TimeoutMS != 10000 {
		t.Fatalf("normalized options = %+v", got.Options)
	}
}

func TestRenderRejectsInvalidJSONAndOptions(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "malformed json", body: `{"html":`, wantStatus: http.StatusBadRequest},
		{name: "invalid viewport", body: `{"html":"ok","options":{"width":9000}}`, wantStatus: http.StatusBadRequest},
		{name: "body too large", body: `{"html":"` + string(bytes.Repeat([]byte("x"), 5000)) + `"}`, wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			handler := newTestHandler(t, rendererFunc(func(_ context.Context, _ api.Request) ([]byte, error) {
				called = true
				return nil, nil
			}))
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/v1/render", bytes.NewBufferString(tt.body))
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, body = %s, want %d", recorder.Code, recorder.Body.String(), tt.wantStatus)
			}
			if called {
				t.Fatal("renderer was called for an invalid request")
			}
			var response api.ErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("error response is not JSON: %v", err)
			}
			if response.Error == nil || response.Error.Code != api.CodeInvalidArgument {
				t.Fatalf("error response = %+v, want invalid_argument", response)
			}
		})
	}
}

func TestRenderMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "timeout", err: &api.Error{Code: api.CodeTimeout, Message: "deadline"}, wantStatus: http.StatusGatewayTimeout, wantCode: api.CodeTimeout},
		{name: "page crashed", err: &api.Error{Code: api.CodePageCrashed, Message: "crashed"}, wantStatus: http.StatusBadGateway, wantCode: api.CodePageCrashed},
		{name: "browser unavailable", err: &api.Error{Code: api.CodeBrowserUnavailable, Message: "down"}, wantStatus: http.StatusBadGateway, wantCode: api.CodeBrowserUnavailable},
		{name: "queue full", err: &api.Error{Code: api.CodeQueueFull, Message: "full"}, wantStatus: http.StatusTooManyRequests, wantCode: api.CodeQueueFull},
		{name: "shutting down", err: &api.Error{Code: api.CodeUnavailable, Message: "closed"}, wantStatus: http.StatusServiceUnavailable, wantCode: api.CodeUnavailable},
		{name: "unknown", err: errors.New("secret details"), wantStatus: http.StatusInternalServerError, wantCode: api.CodeInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newTestHandler(t, rendererFunc(func(_ context.Context, _ api.Request) ([]byte, error) {
				return nil, tt.err
			}))
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/v1/render", bytes.NewBufferString(`{"html":"<p>ok</p>"}`))
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, body = %s, want %d", recorder.Code, recorder.Body.String(), tt.wantStatus)
			}
			var response api.ErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("error response is not JSON: %v", err)
			}
			if response.Error == nil || response.Error.Code != tt.wantCode {
				t.Fatalf("error response = %+v, want %s", response, tt.wantCode)
			}
			if tt.name == "unknown" && bytes.Contains(recorder.Body.Bytes(), []byte("secret")) {
				t.Fatalf("internal error leaked details: %s", recorder.Body.String())
			}
		})
	}
}
