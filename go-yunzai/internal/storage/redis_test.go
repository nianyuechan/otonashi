package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"
)

func openTestRedis(t *testing.T) (*Redis, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	backend, err := OpenRedis(context.Background(), RedisOptions{
		Addr: server.Addr(), MaxRetries: 0,
		DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	return backend, server
}

func TestRedisPersistenceNamespaceAndTTL(t *testing.T) {
	backend, server := openTestRedis(t)
	ctx := context.Background()
	a := backend.Scope("plugin:a")
	b := backend.Scope("plugin")
	if err := a.Set(ctx, "key", []byte("durable"), 0); err != nil {
		t.Fatal(err)
	}
	if err := b.Set(ctx, "key", []byte("other"), 0); err != nil {
		t.Fatal(err)
	}
	value, err := a.Get(ctx, "key")
	if err != nil || string(value) != "durable" {
		t.Fatalf("namespace value = %q, %v", value, err)
	}
	value[0] = 'X'
	if got, err := a.Get(ctx, "key"); err != nil || string(got) != "durable" {
		t.Fatalf("returned bytes alias backend: %q %v", got, err)
	}
	if _, err := b.Get(ctx, "a:key"); !errors.Is(err, sdk.ErrKeyNotFound) {
		t.Fatalf("namespace escape = %v", err)
	}
	if err := a.Set(ctx, "expires", []byte("short"), time.Second); err != nil {
		t.Fatal(err)
	}
	server.FastForward(2 * time.Second)
	if _, err := a.Get(ctx, "expires"); !errors.Is(err, sdk.ErrKeyNotFound) {
		t.Fatalf("expired value = %v", err)
	}
	if err := a.Delete(ctx, "expires"); err != nil {
		t.Fatal(err)
	}
	if err := a.Delete(ctx, "expires"); err != nil {
		t.Fatal(err)
	}
	if err := a.Set(ctx, "key", []byte("new"), -time.Second); !errors.Is(err, sdk.ErrInvalidTTL) {
		t.Fatalf("negative TTL = %v", err)
	}
	if got, err := a.Get(ctx, "key"); err != nil || string(got) != "durable" {
		t.Fatalf("invalid Set changed state: %q %v", got, err)
	}
}

func TestRedisUnavailableCancellationAndCloseAreTyped(t *testing.T) {
	backend, server := openTestRedis(t)
	key := "secret-key"
	server.Close()
	if _, err := backend.Scope("test").Get(context.Background(), key); !errors.Is(err, sdk.ErrStoreUnavailable) || strings.Contains(err.Error(), key) {
		t.Fatalf("unavailable read = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := backend.Scope("test").Set(canceled, "key", []byte("secret-value"), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled write = %v", err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Scope("test").Get(context.Background(), "key"); !errors.Is(err, sdk.ErrStoreClosed) {
		t.Fatalf("closed read = %v", err)
	}
}

func TestOpenRedisFailureDoesNotExposeConnectionDetails(t *testing.T) {
	server := miniredis.RunT(t)
	addr := server.Addr()
	server.Close()
	_, err := OpenRedis(context.Background(), RedisOptions{Addr: addr, MaxRetries: 0, DialTimeout: 20 * time.Millisecond})
	if !errors.Is(err, sdk.ErrStoreUnavailable) || strings.Contains(err.Error(), addr) {
		t.Fatalf("open failure = %v", err)
	}
	if _, err := OpenRedis(context.Background(), RedisOptions{Addr: "", MaxRetries: 0}); !errors.Is(err, sdk.ErrStoreUnavailable) {
		t.Fatalf("empty address = %v", err)
	}
	if _, err := OpenRedis(context.Background(), RedisOptions{Addr: "127.0.0.1:1", MaxRetries: -1}); !errors.Is(err, sdk.ErrStoreUnavailable) {
		t.Fatalf("invalid retries = %v", err)
	}
}
