package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nianyuechan/otonashi/go-yunzai/pkg/plugin/sdk"
)

func TestLegacyPreservesRawKeyJSONAndTTL(t *testing.T) {
	backend, server := openTestRedis(t)
	legacy := backend.Legacy()
	ctx := context.Background()
	key := "Yz:loginMsg:2081344177"
	payload := []byte(`{"isStop":false,"time":123}`)
	if err := legacy.Set(ctx, key, payload, 90*time.Second); err != nil {
		t.Fatal(err)
	}
	if !server.Exists(key) {
		t.Fatal("legacy adapter changed the key name")
	}
	got, err := legacy.Get(ctx, key)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("legacy JSON = %q, %v", got, err)
	}
	ttl, err := legacy.TTL(ctx, key)
	if err != nil || ttl <= 0 || ttl > 90*time.Second {
		t.Fatalf("legacy TTL = %v, %v", ttl, err)
	}
	if value, err := legacy.Incr(ctx, "Yz:count:send:total:2026"); err != nil || value != 1 {
		t.Fatalf("legacy INCR = %d, %v", value, err)
	}
	if value, err := legacy.Incr(ctx, "Yz:count:send:total:2026"); err != nil || value != 2 {
		t.Fatalf("legacy INCR second = %d, %v", value, err)
	}
}

func TestLegacyInspectIsReadOnlyAndRedactsValues(t *testing.T) {
	backend, server := openTestRedis(t)
	legacy := backend.Legacy()
	ctx := context.Background()
	key := "Yz:restart:2081344177"
	payload := []byte(`{"isExit":true,"time":456}`)
	if err := legacy.Set(ctx, key, payload, 0); err != nil {
		t.Fatal(err)
	}
	before := append([]string(nil), server.Keys()...)
	records, err := legacy.Inspect(ctx, key, "Yz:missing")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || !records[0].Exists || records[1].Exists || records[0].Bytes != len(payload) || !records[0].JSONValid || records[0].TTL != 0 {
		t.Fatalf("inspect records = %+v", records)
	}
	hash := sha256.Sum256(payload)
	if records[0].SHA256 != hex.EncodeToString(hash[:]) {
		t.Fatalf("inspect hash = %q", records[0].SHA256)
	}
	if strings.Contains(records[0].SHA256, string(payload)) {
		t.Fatal("inspect leaked the JSON value")
	}
	after := server.Keys()
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("inspect wrote keys: before=%v after=%v", before, after)
	}
}

func TestLegacyPipelineExposesPartialCommandFailure(t *testing.T) {
	backend, _ := openTestRedis(t)
	legacy := backend.Legacy()
	ctx := context.Background()
	if err := legacy.Set(ctx, "Yz:wrongtype", []byte("text"), 0); err != nil {
		t.Fatal(err)
	}
	results, err := legacy.Batch(ctx,
		LegacyOp{Kind: LegacyIncr, Key: "Yz:wrongtype"},
		LegacyOp{Kind: LegacySet, Key: "Yz:ok", Value: []byte(`{"ok":true}`), TTL: 0},
		LegacyOp{Kind: LegacyGet, Key: "Yz:missing"},
	)
	if err != nil || len(results) != 3 {
		t.Fatalf("batch = %+v, %v", results, err)
	}
	if !errors.Is(results[0].Err, sdk.ErrStoreCommand) {
		t.Fatalf("wrong type result = %v", results[0].Err)
	}
	if results[1].Err != nil {
		t.Fatalf("successful sibling result = %v", results[1].Err)
	}
	if !errors.Is(results[2].Err, sdk.ErrKeyNotFound) {
		t.Fatalf("missing result = %v", results[2].Err)
	}
	if value, err := legacy.Get(ctx, "Yz:ok"); err != nil || string(value) != `{"ok":true}` {
		t.Fatalf("partial batch hid successful write: %q %v", value, err)
	}
}

// 该用例只验证 Go 侧 JSON 编解码往返，不代表旧版真实写入字节的兼容性；
// 真实旧数据迁移仍需单独 fixture。
func TestLegacyJSONGoRoundTrip(t *testing.T) {
	backend, _ := openTestRedis(t)
	legacy := backend.Legacy()
	payload := map[string]any{"isStop": true, "time": 123.0}
	encoded, _ := json.Marshal(payload)
	if err := legacy.Set(context.Background(), "Yz:restart:fixture", encoded, 0); err != nil {
		t.Fatal(err)
	}
	got, err := legacy.Get(context.Background(), "Yz:restart:fixture")
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil || decoded["isStop"] != true || decoded["time"] != 123.0 {
		t.Fatalf("legacy fixture readback = %s, %v", got, err)
	}
}
