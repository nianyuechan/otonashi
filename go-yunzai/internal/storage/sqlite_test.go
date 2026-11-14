package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"
)

func openTestStore(t *testing.T, path string) *SQLite {
	t.Helper()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func TestSQLitePersistenceNamespacesAndValueOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "子目录", "状态 #100%.sqlite")
	s := openTestStore(t, path)
	ctx := context.Background()
	a, b := s.Scope("plugin:a"), s.Scope("plugin")
	for key, value := range map[string][]byte{"key": []byte("durable"), "empty": nil, "v1:key": []byte("opaque")} {
		if err := a.Set(ctx, key, value, 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.Get(ctx, "a:v1:key"); !errors.Is(err, sdk.ErrKeyNotFound) {
		t.Fatalf("namespace escape = %v", err)
	}
	if err := b.Set(ctx, "key", []byte("other"), 0); err != nil {
		t.Fatal(err)
	}
	value, err := a.Get(ctx, "key")
	if err != nil || string(value) != "durable" {
		t.Fatalf("Get = %q, %v", value, err)
	}
	value[0] = 'X'
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTestStore(t, path)
	for plugin, expected := range map[string]string{"plugin:a": "durable", "plugin": "other"} {
		got, err := s.Scope(plugin).Get(ctx, "key")
		if err != nil || string(got) != expected {
			t.Fatalf("reopened %s = %q, %v", plugin, got, err)
		}
	}
	if got, err := s.Scope("plugin:a").Get(ctx, "empty"); err != nil || len(got) != 0 {
		t.Fatalf("empty value treated as missing: %q, %v", got, err)
	}
	if err := s.Scope("plugin").Delete(ctx, "key"); err != nil {
		t.Fatal(err)
	}
	if err := s.Scope("plugin").Delete(ctx, "key"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Scope("plugin:a").Get(ctx, "key"); err != nil {
		t.Fatal("deletion crossed namespaces", err)
	}
}

func TestSQLiteTTLReplacementAndExpiryAfterRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.sqlite")
	s := openTestStore(t, path)
	now := time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	kv := s.Scope("test")
	if err := kv.Set(ctx, "expires", []byte("short"), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := kv.Set(ctx, "retained", []byte("old"), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := kv.Set(ctx, "retained", []byte("new"), 0); err != nil {
		t.Fatal(err)
	}
	if err := kv.Set(ctx, "tiny", []byte("tiny"), time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	if _, err := kv.Get(ctx, "tiny"); err != nil {
		t.Fatal("positive TTL expired early", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTestStore(t, path)
	now = now.Add(time.Second)
	s.now = func() time.Time { return now }
	kv = s.Scope("test")
	for _, key := range []string{"expires", "tiny"} {
		if _, err := kv.Get(ctx, key); !errors.Is(err, sdk.ErrKeyNotFound) {
			t.Fatalf("expired %s = %v", key, err)
		}
	}
	var rows int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM yzgo_plugin_kv").Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("lazy TTL cleanup: rows=%d err=%v", rows, err)
	}
	if value, err := kv.Get(ctx, "retained"); err != nil || string(value) != "new" {
		t.Fatalf("zero TTL did not clear old expiry: %q %v", value, err)
	}
	if err := kv.Set(ctx, "retained", []byte("bad"), -time.Second); !errors.Is(err, sdk.ErrInvalidTTL) {
		t.Fatalf("negative TTL = %v", err)
	}
	if value, err := kv.Get(ctx, "retained"); err != nil || string(value) != "new" {
		t.Fatal("invalid Set altered existing state")
	}
}

func TestSQLiteFailuresDoNotMasqueradeAsMissing(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "state.sqlite"))
	kv := s.Scope("test")
	for _, key := range []string{"", strings.Repeat("x", 1025)} {
		if _, err := kv.Get(ctx, key); !errors.Is(err, sdk.ErrInvalidKey) {
			t.Fatalf("invalid key = %v", err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := kv.Set(canceled, "key", nil, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled write = %v", err)
	}
	if _, err := s.db.Exec("DROP TABLE yzgo_plugin_kv"); err != nil {
		t.Fatal(err)
	}
	if _, err := kv.Get(ctx, "sensitive-key"); !errors.Is(err, sdk.ErrStoreUnavailable) || strings.Contains(err.Error(), "sensitive-key") {
		t.Fatalf("backend failure = %v", err)
	}
	if err := kv.Set(ctx, "key", []byte("sensitive-value"), 0); !errors.Is(err, sdk.ErrStoreUnavailable) || strings.Contains(err.Error(), "sensitive-value") {
		t.Fatalf("backend write failure = %v", err)
	}
	if err := kv.Delete(ctx, "key"); !errors.Is(err, sdk.ErrStoreUnavailable) {
		t.Fatalf("backend delete failure = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := kv.Get(ctx, "key"); !errors.Is(err, sdk.ErrStoreClosed) {
		t.Fatalf("closed store = %v", err)
	}
	if _, err := Open(ctx, t.TempDir()); !errors.Is(err, sdk.ErrStoreUnavailable) {
		t.Fatalf("directory accepted as database: %v", err)
	}
	corrupt := filepath.Join(t.TempDir(), "corrupt.sqlite")
	if err := os.WriteFile(corrupt, []byte("not a sqlite database"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, corrupt); !errors.Is(err, sdk.ErrStoreUnavailable) {
		t.Fatalf("corrupt database accepted: %v", err)
	}
}

func TestSQLiteConcurrentWrites(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "state.sqlite"))
	kv := s.Scope("test")
	var wg sync.WaitGroup
	for i := range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", i)
			if err := kv.Set(context.Background(), key, []byte(key), 0); err != nil {
				t.Error(err)
				return
			}
			if got, err := kv.Get(context.Background(), key); err != nil || string(got) != key {
				t.Errorf("concurrent read = %q, %v", got, err)
			}
		}()
	}
	wg.Wait()
}
