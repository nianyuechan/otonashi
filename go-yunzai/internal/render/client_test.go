package render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRenderSuccess(t *testing.T) {
	pngData := makePNG(t, 12, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/render" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		var request Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.HTML != "<html><body>ok</body></html>" || request.Options.Width != 320 || request.Options.Height != 200 || request.Options.DeviceScaleFactor != 2 || request.Options.TimeoutMS != 15000 || request.Options.WaitUntil != "network_idle" || request.Options.WaitSelector != "#ready" {
			t.Errorf("unexpected render request: %+v", request)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngData)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, 0)
	result, err := client.Render(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if result.Width != 12 || result.Height != 8 || !bytes.Equal(result.Data, pngData) {
		t.Fatalf("unexpected image result: %+v", result)
	}
}

func TestRenderTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, 0)
	client.timeout = 30 * time.Millisecond
	_, err := client.Render(context.Background(), validRequest())
	renderErr := requireErrorKind(t, err, ErrorTimeout)
	if renderErr.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", renderErr.Attempts)
	}
}

func TestRenderContextCancellationStopsRetries(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
	}))
	defer server.Close()
	defer close(release)

	client := newTestClient(t, server.URL, 3)
	client.timeout = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.Render(ctx, validRequest())
		done <- err
	}()

	<-started
	cancel()
	err := <-done
	renderErr := requireErrorKind(t, err, ErrorUnavailable)
	if renderErr.Code != "canceled" || renderErr.Attempts != 1 {
		t.Fatalf("unexpected cancellation error: %+v", renderErr)
	}
}

func TestRenderRetriesServerError(t *testing.T) {
	pngData := makePNG(t, 4, 4)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "starting")
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngData)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, 2)
	result, err := client.Render(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if result.Width != 4 || calls.Load() != 3 {
		t.Fatalf("result = %+v, calls = %d", result, calls.Load())
	}
}

func TestRenderPersistent5xxIsUnavailable(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeError(w, http.StatusInternalServerError, "internal", "render failed")
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, 1)
	_, err := client.Render(context.Background(), validRequest())
	renderErr := requireErrorKind(t, err, ErrorUnavailable)
	if renderErr.StatusCode != http.StatusInternalServerError || renderErr.Code != "internal" || renderErr.Attempts != 2 || calls.Load() != 2 {
		t.Fatalf("unexpected error %+v, calls = %d", renderErr, calls.Load())
	}
}

func TestRenderGatewayTimeoutIsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusGatewayTimeout, "timeout", "render exceeded its deadline")
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, 0)
	_, err := client.Render(context.Background(), validRequest())
	requireErrorKind(t, err, ErrorTimeout)
}

func TestRenderInvalidArgumentIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeError(w, http.StatusBadRequest, "invalid_argument", "html must not be empty")
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, 3)
	_, err := client.Render(context.Background(), validRequest())
	renderErr := requireErrorKind(t, err, ErrorInvalidArgument)
	if renderErr.Attempts != 1 || calls.Load() != 1 {
		t.Fatalf("unexpected error %+v, calls = %d", renderErr, calls.Load())
	}
}

func TestRenderRejectsInvalidPNG(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("not-a-png"))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, 3)
	_, err := client.Render(context.Background(), validRequest())
	renderErr := requireErrorKind(t, err, ErrorInvalidImage)
	if renderErr.Attempts != 1 || calls.Load() != 1 {
		t.Fatalf("unexpected error %+v, calls = %d", renderErr, calls.Load())
	}
}

func TestRenderServiceNotStartedIsUnavailable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	url := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	client := newTestClient(t, url, 0)
	client.timeout = 50 * time.Millisecond
	_, err = client.Render(context.Background(), validRequest())
	renderErr := requireErrorKind(t, err, ErrorUnavailable)
	if renderErr.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", renderErr.Attempts)
	}
}

func TestRenderLimitsConcurrency(t *testing.T) {
	pngData := makePNG(t, 2, 2)
	var active atomic.Int32
	var maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngData)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, 0)
	client.timeout = time.Second
	const requests = 8
	start := make(chan struct{})
	errorsCh := make(chan error, requests)
	var wg sync.WaitGroup
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := client.Render(context.Background(), validRequest())
			errorsCh <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("Render() error = %v", err)
		}
	}
	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum concurrent HTTP requests = %d, want 2", got)
	}
}

func TestRenderWithFallbackReturnsReadableText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "internal details")
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, 0)
	outcome := client.RenderWithFallback(context.Background(), validRequest())
	if outcome.Image != nil || outcome.Text == "" || outcome.Err == nil {
		t.Fatalf("unexpected fallback outcome: %+v", outcome)
	}
	requireErrorKind(t, outcome.Err, ErrorUnavailable)
	if !strings.Contains(outcome.Text, "图片暂时不可用") || !strings.Contains(outcome.Text, "稍后重试") || strings.Contains(outcome.Text, "internal details") {
		t.Fatalf("fallback text = %q", outcome.Text)
	}
}

func TestRenderRejectsBlankHTMLBeforeHTTP(t *testing.T) {
	client := newTestClient(t, "http://127.0.0.1:1", 3)
	request := validRequest()
	request.HTML = " \n\t"
	_, err := client.Render(context.Background(), request)
	renderErr := requireErrorKind(t, err, ErrorInvalidArgument)
	if renderErr.Attempts != 0 {
		t.Fatalf("attempts = %d, want 0", renderErr.Attempts)
	}
}

func validRequest() Request {
	return Request{
		HTML: "<html><body>ok</body></html>",
		Options: RenderOptions{
			Width:             320,
			Height:            200,
			DeviceScaleFactor: 2,
			TimeoutMS:         15000,
			WaitUntil:         "network_idle",
			WaitSelector:      "#ready",
		},
	}
}

func newTestClient(t *testing.T, url string, retries int) *Client {
	t.Helper()
	client, err := NewClient(Options{
		URL: url,
		// Functional tests should tolerate a busy host; timeout tests override this.
		Timeout:       2 * time.Second,
		MaxRetries:    retries,
		RetryBackoff:  time.Millisecond,
		MaxConcurrent: 2,
		FallbackText:  "图片暂时不可用",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(client.Close)
	return client
}

func makePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func requireErrorKind(t *testing.T, err error, kind ErrorKind) *Error {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want kind %q", kind)
	}
	var renderErr *Error
	if !errors.As(err, &renderErr) {
		t.Fatalf("error type = %T, want *render.Error: %v", err, err)
	}
	if renderErr.Kind != kind {
		t.Fatalf("error kind = %q, want %q: %v", renderErr.Kind, kind, err)
	}
	return renderErr
}

func TestErrorIncludesSafeSummary(t *testing.T) {
	err := &Error{Kind: ErrorUnavailable, Code: "internal", StatusCode: 500, Attempts: 2, Message: "render failed"}
	for _, part := range []string{"unavailable", "internal", "500", "2"} {
		if !strings.Contains(fmt.Sprint(err), part) {
			t.Fatalf("error summary %q does not contain %q", err, part)
		}
	}
}
