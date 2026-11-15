package renderer

import (
	"context"
	"errors"
)

// The chromedp backend is the only production provider today. It registers
// itself here so that cmd/renderer can select it by name without importing any
// particular implementation: swapping backends becomes adding a file with a
// sibling init, not editing main.
func init() {
	err := RegisterProvider(Provider{
		Name:        "chromedp",
		Description: "Chromium/Chrome/Edge driven through chromedp/CDP",
		Factory: func(ctx context.Context, cfg ProviderConfig) (Browser, error) {
			if cfg.ChromiumPath == "" {
				return nil, errors.New("chromedp provider requires a browser executable path")
			}
			return NewChromedpBrowser(ctx, cfg.ChromiumPath)
		},
	})
	if err != nil {
		// Registration only fails on a duplicate name or an invalid provider,
		// both of which are programming errors found at startup, not at runtime.
		panic(err)
	}
}
