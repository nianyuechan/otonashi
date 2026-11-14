package api

import (
	"fmt"
	"strings"
	"time"
)

const (
	CodeInvalidArgument    = "invalid_argument"
	CodeTimeout            = "timeout"
	CodePageCrashed        = "page_crashed"
	CodeBrowserUnavailable = "browser_unavailable"
	CodeQueueFull          = "queue_full"
	CodeUnavailable        = "unavailable"
	CodeInternal           = "internal"
	WaitLoad               = "load"
	WaitDOMContentLoaded   = "dom_content_loaded"
	WaitNetworkIdle        = "network_idle"
	defaultTimeoutMS       = 10000
	defaultWidth           = 800
	defaultHeight          = 600
	maxViewportSide        = 8192
	maxDeviceScaleFactor   = 4.0
)

type Request struct {
	HTML    string  `json:"html"`
	Options Options `json:"options"`
}

type Options struct {
	Width             int     `json:"width,omitempty"`
	Height            int     `json:"height,omitempty"`
	DeviceScaleFactor float64 `json:"device_scale_factor,omitempty"`
	TimeoutMS         int     `json:"timeout_ms,omitempty"`
	WaitUntil         string  `json:"wait_until,omitempty"`
	WaitSelector      string  `json:"wait_selector,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

type ErrorResponse struct {
	Error *Error `json:"error"`
}

func (r *Request) Validate(maxTimeout time.Duration) *Error {
	if strings.TrimSpace(r.HTML) == "" {
		return invalid("html must not be empty")
	}
	if r.Options.Width < 0 || r.Options.Width > maxViewportSide {
		return invalid("width must be between 1 and 8192")
	}
	if r.Options.Height < 0 || r.Options.Height > maxViewportSide {
		return invalid("height must be between 1 and 8192")
	}
	if r.Options.Width == 0 {
		r.Options.Width = defaultWidth
	}
	if r.Options.Height == 0 {
		r.Options.Height = defaultHeight
	}
	if r.Options.DeviceScaleFactor < 0 || r.Options.DeviceScaleFactor > maxDeviceScaleFactor {
		return invalid("device_scale_factor must be between 0.1 and 4")
	}
	if r.Options.DeviceScaleFactor == 0 {
		r.Options.DeviceScaleFactor = 1
	} else if r.Options.DeviceScaleFactor < 0.1 {
		return invalid("device_scale_factor must be between 0.1 and 4")
	}
	if r.Options.TimeoutMS < 0 {
		return invalid("timeout_ms must not be negative")
	}
	if r.Options.TimeoutMS == 0 {
		r.Options.TimeoutMS = defaultTimeoutMS
	}
	if maxTimeout > 0 && time.Duration(r.Options.TimeoutMS)*time.Millisecond > maxTimeout {
		return invalid(fmt.Sprintf("timeout_ms must not exceed %d", maxTimeout.Milliseconds()))
	}
	if r.Options.WaitUntil == "" {
		r.Options.WaitUntil = WaitLoad
	}
	switch r.Options.WaitUntil {
	case WaitLoad, WaitDOMContentLoaded, WaitNetworkIdle:
	default:
		return invalid("wait_until must be load, dom_content_loaded, or network_idle")
	}
	if len(r.Options.WaitSelector) > 2048 {
		return invalid("wait_selector must not exceed 2048 bytes")
	}
	return nil
}

func invalid(message string) *Error {
	return &Error{Code: CodeInvalidArgument, Message: message}
}
