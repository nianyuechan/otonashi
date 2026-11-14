package render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ErrorKind string

const (
	ErrorTimeout         ErrorKind = "timeout"
	ErrorUnavailable     ErrorKind = "unavailable"
	ErrorInvalidArgument ErrorKind = "invalid_argument"
	ErrorInvalidImage    ErrorKind = "invalid_image"
)

type Request struct {
	HTML    string        `json:"html"`
	Options RenderOptions `json:"options"`
}

type RenderOptions struct {
	Width             int     `json:"width,omitempty"`
	Height            int     `json:"height,omitempty"`
	DeviceScaleFactor float64 `json:"device_scale_factor,omitempty"`
	TimeoutMS         int     `json:"timeout_ms,omitempty"`
	WaitUntil         string  `json:"wait_until,omitempty"`
	WaitSelector      string  `json:"wait_selector,omitempty"`
}

type Image struct {
	Data   []byte
	Width  int
	Height int
}

type Outcome struct {
	Image *Image
	Text  string
	Err   error
}

// Renderer 是业务插件依赖的最小渲染接口。
// Client 通过 RenderWithFallback 实现它，插件无需依赖 HTTP 细节。
type Renderer interface {
	RenderWithFallback(context.Context, Request) Outcome
}

type Error struct {
	Kind       ErrorKind
	Code       string
	Message    string
	StatusCode int
	Attempts   int
	Err        error
}

func (e *Error) Error() string {
	return fmt.Sprintf("render %s: code=%s status=%d attempts=%d message=%s", e.Kind, e.Code, e.StatusCode, e.Attempts, e.Message)
}

func (e *Error) Unwrap() error {
	return e.Err
}

type Options struct {
	URL              string
	Timeout          time.Duration
	MaxRetries       int
	RetryBackoff     time.Duration
	MaxConcurrent    int
	MaxResponseBytes int64
	FallbackText     string
	HTTPClient       *http.Client
}

type Client struct {
	baseURL          string
	httpClient       *http.Client
	timeout          time.Duration
	maxRetries       int
	retryBackoff     time.Duration
	maxResponseBytes int64
	fallbackText     string
	sem              chan struct{}
}

func NewClient(opts Options) (*Client, error) {
	parsed, err := url.Parse(opts.URL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("render URL must be an absolute http:// or https:// URL")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("render URL must not contain credentials")
	}
	if opts.Timeout == 0 {
		opts.Timeout = 15 * time.Second
	}
	if opts.RetryBackoff == 0 {
		opts.RetryBackoff = 100 * time.Millisecond
	}
	if opts.MaxConcurrent == 0 {
		opts.MaxConcurrent = 4
	}
	if opts.MaxResponseBytes == 0 {
		opts.MaxResponseBytes = 32 << 20
	}
	if opts.FallbackText == "" {
		opts.FallbackText = "图片暂时不可用"
	}
	if opts.Timeout < 0 || opts.RetryBackoff < 0 || opts.MaxRetries < 0 || opts.MaxRetries > 10 || opts.MaxConcurrent < 0 || opts.MaxResponseBytes < 0 {
		return nil, fmt.Errorf("invalid render client options")
	}
	if opts.HTTPClient == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		opts.HTTPClient = &http.Client{Transport: transport}
	}
	return &Client{
		baseURL:          strings.TrimRight(opts.URL, "/"),
		httpClient:       opts.HTTPClient,
		timeout:          opts.Timeout,
		maxRetries:       opts.MaxRetries,
		retryBackoff:     opts.RetryBackoff,
		maxResponseBytes: opts.MaxResponseBytes,
		fallbackText:     opts.FallbackText,
		sem:              make(chan struct{}, opts.MaxConcurrent),
	}, nil
}

// Render 调用独立渲染服务，并返回已解码校验的 PNG。
func (c *Client) Render(ctx context.Context, request Request) (Image, error) {
	if err := c.validateRequest(&request); err != nil {
		return Image{}, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return Image{}, &Error{Kind: ErrorInvalidArgument, Code: "encode_request", Message: "无法编码渲染请求", Err: err}
	}

	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return Image{}, contextError(ctx.Err(), 0)
	}

	for attempt := 1; attempt <= c.maxRetries+1; attempt++ {
		image, renderErr := c.doAttempt(ctx, payload, attempt)
		if renderErr == nil {
			return image, nil
		}
		if ctx.Err() != nil {
			return Image{}, renderErr
		}
		if !retryable(renderErr.Kind) || attempt > c.maxRetries {
			return Image{}, renderErr
		}
		if err := sleep(ctx, c.retryBackoff<<uint(attempt-1)); err != nil {
			return Image{}, contextError(err, attempt)
		}
	}
	return Image{}, &Error{Kind: ErrorUnavailable, Code: "retry_exhausted", Message: "渲染重试已结束"}
}

// RenderWithFallback 保留分类错误，同时给消息处理器提供可读的文本降级结果。
func (c *Client) RenderWithFallback(ctx context.Context, request Request) Outcome {
	image, err := c.Render(ctx, request)
	if err == nil {
		return Outcome{Image: &image}
	}
	reason := "渲染服务暂不可用，请稍后重试。"
	var renderErr *Error
	if errors.As(err, &renderErr) {
		switch renderErr.Kind {
		case ErrorTimeout:
			reason = "渲染超时，请稍后重试。"
		case ErrorInvalidArgument:
			reason = "渲染请求无效，请检查输入。"
		case ErrorInvalidImage:
			reason = "渲染服务返回了无效图片，请稍后重试。"
		}
	}
	return Outcome{Text: c.fallbackText + "：" + reason, Err: err}
}

func (c *Client) Close() {
	c.httpClient.CloseIdleConnections()
}

func (c *Client) validateRequest(request *Request) *Error {
	invalid := func(message string) *Error {
		return &Error{Kind: ErrorInvalidArgument, Code: "invalid_argument", Message: message}
	}
	if strings.TrimSpace(request.HTML) == "" {
		return invalid("html 不能为空")
	}
	if request.Options.Width < 0 || request.Options.Width > 8192 || request.Options.Height < 0 || request.Options.Height > 8192 {
		return invalid("视口尺寸必须在 1 到 8192 之间")
	}
	if request.Options.DeviceScaleFactor < 0 || request.Options.DeviceScaleFactor > 4 || (request.Options.DeviceScaleFactor > 0 && request.Options.DeviceScaleFactor < 0.1) {
		return invalid("device_scale_factor 必须在 0.1 到 4 之间")
	}
	if request.Options.TimeoutMS < 0 {
		return invalid("timeout_ms 不能为负数")
	}
	if request.Options.TimeoutMS == 0 {
		request.Options.TimeoutMS = int(c.timeout / time.Millisecond)
		if request.Options.TimeoutMS == 0 {
			request.Options.TimeoutMS = 1
		}
	}
	switch request.Options.WaitUntil {
	case "", "load", "dom_content_loaded", "network_idle":
	default:
		return invalid("wait_until 必须是 load、dom_content_loaded 或 network_idle")
	}
	if len(request.Options.WaitSelector) > 2048 {
		return invalid("wait_selector 不能超过 2048 字节")
	}
	return nil
}

func (c *Client) doAttempt(ctx context.Context, payload []byte, attempt int) (Image, *Error) {
	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, c.baseURL+"/v1/render", bytes.NewReader(payload))
	if err != nil {
		return Image{}, &Error{Kind: ErrorInvalidArgument, Code: "build_request", Message: "无法创建渲染请求", Attempts: attempt, Err: err}
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return Image{}, contextError(ctx.Err(), attempt)
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
			return Image{}, &Error{Kind: ErrorTimeout, Code: "http_timeout", Message: "渲染服务请求超时", Attempts: attempt, Err: err}
		}
		return Image{}, &Error{Kind: ErrorUnavailable, Code: "http_unavailable", Message: "渲染服务连接失败", Attempts: attempt, Err: err}
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return Image{}, responseError(response, attempt)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "image/png" {
		return Image{}, &Error{Kind: ErrorInvalidImage, Code: "invalid_content_type", Message: "渲染服务未返回 image/png", StatusCode: response.StatusCode, Attempts: attempt, Err: err}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, c.maxResponseBytes+1))
	if err != nil {
		return Image{}, &Error{Kind: ErrorUnavailable, Code: "read_response", Message: "读取渲染响应失败", StatusCode: response.StatusCode, Attempts: attempt, Err: err}
	}
	if int64(len(data)) > c.maxResponseBytes {
		return Image{}, &Error{Kind: ErrorInvalidImage, Code: "image_too_large", Message: "渲染图片超过响应大小上限", StatusCode: response.StatusCode, Attempts: attempt}
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return Image{}, &Error{Kind: ErrorInvalidImage, Code: "invalid_png", Message: "渲染服务返回了无法解码的 PNG", StatusCode: response.StatusCode, Attempts: attempt, Err: err}
	}
	return Image{Data: data, Width: decoded.Bounds().Dx(), Height: decoded.Bounds().Dy()}, nil
}

func responseError(response *http.Response, attempt int) *Error {
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	var body struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(payload, &body)
	code := "http_status"
	message := http.StatusText(response.StatusCode)
	if body.Error != nil {
		code = body.Error.Code
		message = body.Error.Message
	}
	kind := ErrorUnavailable
	if response.StatusCode == http.StatusGatewayTimeout {
		kind = ErrorTimeout
	} else if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusRequestEntityTooLarge {
		kind = ErrorInvalidArgument
	}
	return &Error{Kind: kind, Code: code, Message: message, StatusCode: response.StatusCode, Attempts: attempt}
}

func retryable(kind ErrorKind) bool {
	return kind == ErrorTimeout || kind == ErrorUnavailable
}

func contextError(err error, attempts int) *Error {
	kind := ErrorUnavailable
	code := "canceled"
	message := "渲染请求已取消"
	if errors.Is(err, context.DeadlineExceeded) {
		kind = ErrorTimeout
		code = "context_timeout"
		message = "渲染请求超过调用方期限"
	}
	return &Error{Kind: kind, Code: code, Message: message, Attempts: attempts, Err: err}
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
