package renderer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	// ErrProviderInvalid rejects a registration without a name or a factory.
	ErrProviderInvalid = errors.New("render provider is invalid")
	// ErrProviderRegistered rejects a duplicate provider name.
	ErrProviderRegistered = errors.New("render provider already registered")
	// ErrProviderUnknown reports a lookup for a name nobody registered.
	ErrProviderUnknown = errors.New("render provider is not registered")
)

// ProviderConfig carries deployment-level settings to a provider. Providers
// ignore fields they do not use; new fields are added here rather than as new
// parameters, so existing factories keep compiling.
type ProviderConfig struct {
	// ChromiumPath is an existing Chromium/Chrome/Edge executable.
	ChromiumPath string
}

// ProviderFactory builds a Browser from deployment configuration. It is the
// same shape as BrowserFactory, plus the configuration a real backend needs.
type ProviderFactory func(context.Context, ProviderConfig) (Browser, error)

// Provider is a named render backend. Registration happens in an init
// function next to the implementation, so adding a backend is adding a file.
type Provider struct {
	Name        string
	Description string
	Factory     ProviderFactory
}

// Registry of providers is process-wide: a renderer binary only ever needs one
// set of backends, and a registry owned by the service would force every caller
// to wire it. The lock is a plain RWMutex instead of sync.Map because lookups
// are rare (once per process start) and ordering is guaranteed by sort.
var providerRegistry = struct {
	mu     sync.RWMutex
	byName map[string]Provider
}{byName: make(map[string]Provider)}

// RegisterProvider adds a backend to the process-wide registry. It rejects an
// empty name, a nil factory, and a name that is already taken; returning an
// error instead of overwriting keeps a provider file from silently shadowing
// another one.
func RegisterProvider(provider Provider) error {
	if provider.Name == "" || provider.Factory == nil {
		return fmt.Errorf("%w: name and factory are required", ErrProviderInvalid)
	}
	providerRegistry.mu.Lock()
	defer providerRegistry.mu.Unlock()
	if _, exists := providerRegistry.byName[provider.Name]; exists {
		return fmt.Errorf("%w: %s", ErrProviderRegistered, provider.Name)
	}
	providerRegistry.byName[provider.Name] = provider
	return nil
}

// LookupProvider resolves a provider name. The returned Provider is a copy;
// the registry is never handed out directly.
func LookupProvider(name string) (Provider, error) {
	providerRegistry.mu.RLock()
	provider, ok := providerRegistry.byName[name]
	providerRegistry.mu.RUnlock()
	if !ok {
		available := ProviderNames()
		if len(available) == 0 {
			return Provider{}, fmt.Errorf("%w: %s (no providers registered)", ErrProviderUnknown, name)
		}
		return Provider{}, fmt.Errorf("%w: %s (available: %s)", ErrProviderUnknown, name, strings.Join(available, ", "))
	}
	return provider, nil
}

// ProviderNames returns a sorted copy of the registered names. The copy keeps
// callers from mutating registry state through the returned slice.
func ProviderNames() []string {
	providerRegistry.mu.RLock()
	names := make([]string, 0, len(providerRegistry.byName))
	for name := range providerRegistry.byName {
		names = append(names, name)
	}
	providerRegistry.mu.RUnlock()
	sort.Strings(names)
	return names
}
