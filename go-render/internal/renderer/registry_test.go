package renderer

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
)

func TestRegistryRegistersAndResolvesProviders(t *testing.T) {
	factory := func(context.Context, ProviderConfig) (Browser, error) {
		return &fakeBrowser{done: make(chan struct{})}, nil
	}
	if err := RegisterProvider(Provider{Name: "registry-test-resolve", Description: "test backend", Factory: factory}); err != nil {
		t.Fatalf("RegisterProvider() error = %v", err)
	}

	provider, err := LookupProvider("registry-test-resolve")
	if err != nil {
		t.Fatalf("LookupProvider() error = %v", err)
	}
	if provider.Name != "registry-test-resolve" || provider.Description != "test backend" || provider.Factory == nil {
		t.Fatalf("LookupProvider() = %+v", provider)
	}
	browser, err := provider.Factory(context.Background(), ProviderConfig{})
	if err != nil || browser == nil {
		t.Fatalf("provider factory = %v, %v", browser, err)
	}
}

func TestRegistryRejectsInvalidAndDuplicateProviders(t *testing.T) {
	for name, provider := range map[string]Provider{
		"empty name":  {Factory: func(context.Context, ProviderConfig) (Browser, error) { return nil, nil }},
		"nil factory": {Name: "registry-test-nil-factory"},
	} {
		if err := RegisterProvider(provider); !errors.Is(err, ErrProviderInvalid) {
			t.Errorf("%s: RegisterProvider() error = %v; want ErrProviderInvalid", name, err)
		}
	}

	factory := func(context.Context, ProviderConfig) (Browser, error) { return nil, nil }
	if err := RegisterProvider(Provider{Name: "registry-test-duplicate", Factory: factory}); err != nil {
		t.Fatalf("first registration error = %v", err)
	}
	err := RegisterProvider(Provider{Name: "registry-test-duplicate", Description: "shadow", Factory: factory})
	if !errors.Is(err, ErrProviderRegistered) {
		t.Fatalf("duplicate registration error = %v; want ErrProviderRegistered", err)
	}
	provider, _ := LookupProvider("registry-test-duplicate")
	if provider.Description == "shadow" {
		t.Fatal("duplicate registration overwrote the original provider")
	}
}

func TestRegistryUnknownProviderListsAvailableNames(t *testing.T) {
	_, err := LookupProvider("registry-test-absent")
	if !errors.Is(err, ErrProviderUnknown) {
		t.Fatalf("LookupProvider(unknown) error = %v; want ErrProviderUnknown", err)
	}
	if !strings.Contains(err.Error(), "registry-test-absent") || !strings.Contains(err.Error(), "chromedp") {
		t.Fatalf("unknown-provider error should name the request and the alternatives: %v", err)
	}
}

func TestProviderNamesAreSortedAndCopied(t *testing.T) {
	names := ProviderNames()
	if !sort.StringsAreSorted(names) {
		t.Fatalf("ProviderNames() is not sorted: %v", names)
	}
	if len(names) == 0 {
		t.Fatal("ProviderNames() is empty; the chromedp provider should be registered")
	}
	names[0] = "registry-test-mutated"
	if ProviderNames()[0] == "registry-test-mutated" {
		t.Fatal("ProviderNames() returned a slice that aliases registry state")
	}
}

func TestChromedpProviderIsRegisteredAndRequiresAPath(t *testing.T) {
	provider, err := LookupProvider("chromedp")
	if err != nil {
		t.Fatalf("chromedp provider is not registered: %v", err)
	}
	if _, err := provider.Factory(context.Background(), ProviderConfig{}); err == nil {
		t.Fatal("chromedp provider accepted an empty executable path")
	}
}
