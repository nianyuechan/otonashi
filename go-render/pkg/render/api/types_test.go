package api

import (
	"testing"
	"time"
)

func TestRequestValidateAppliesDefaults(t *testing.T) {
	req := Request{HTML: "<html><body>ok</body></html>"}
	if err := req.Validate(30 * time.Second); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if req.Options.Width != 800 || req.Options.Height != 600 {
		t.Fatalf("viewport = %dx%d, want 800x600", req.Options.Width, req.Options.Height)
	}
	if req.Options.DeviceScaleFactor != 1 {
		t.Fatalf("device scale factor = %v, want 1", req.Options.DeviceScaleFactor)
	}
	if req.Options.TimeoutMS != 10000 {
		t.Fatalf("timeout = %dms, want 10000ms", req.Options.TimeoutMS)
	}
	if req.Options.WaitUntil != WaitLoad {
		t.Fatalf("wait until = %q, want %q", req.Options.WaitUntil, WaitLoad)
	}
}

func TestRequestValidateRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name string
		req  Request
	}{
		{name: "empty html", req: Request{HTML: " \n"}},
		{name: "negative width", req: Request{HTML: "ok", Options: Options{Width: -1}}},
		{name: "zero explicit height is default", req: Request{HTML: "ok", Options: Options{Height: 9000}}},
		{name: "scale factor too large", req: Request{HTML: "ok", Options: Options{DeviceScaleFactor: 5}}},
		{name: "negative timeout", req: Request{HTML: "ok", Options: Options{TimeoutMS: -1}}},
		{name: "timeout above service maximum", req: Request{HTML: "ok", Options: Options{TimeoutMS: 30001}}},
		{name: "unknown wait condition", req: Request{HTML: "ok", Options: Options{WaitUntil: "eventually"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate(30 * time.Second)
			if err == nil || err.Code != CodeInvalidArgument {
				t.Fatalf("Validate() error = %#v, want invalid_argument", err)
			}
		})
	}
}

func TestRequestValidateAcceptsWaitConditions(t *testing.T) {
	for _, waitUntil := range []string{WaitDOMContentLoaded, WaitNetworkIdle} {
		req := Request{HTML: "<p>ok</p>", Options: Options{WaitUntil: waitUntil, WaitSelector: "#ready"}}
		if err := req.Validate(30 * time.Second); err != nil {
			t.Fatalf("Validate(%q) error = %v", waitUntil, err)
		}
	}
}
