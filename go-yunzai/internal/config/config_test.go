package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadExample(t *testing.T) {
	cfg, err := Load("../../config.example.yaml")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.OneBot.URL != "ws://127.0.0.1:3001/" {
		t.Fatalf("unexpected URL %q", cfg.OneBot.URL)
	}
	if cfg.OneBot.AccessToken != "replace-with-onebot-access-token" {
		t.Fatal("example no longer contains the documented placeholder")
	}
	if cfg.OneBot.RequestTimeout != 10*time.Second || cfg.OneBot.HeartbeatInterval != 20*time.Second {
		t.Fatalf("durations were not decoded: %+v", cfg.OneBot)
	}
	if cfg.Log.Level != "info" || cfg.Debug.Reply != DefaultDebugReply {
		t.Fatalf("unexpected log/debug config: %+v", cfg)
	}
	if cfg.Render.URL != "http://127.0.0.1:8080" || cfg.Render.Timeout != 15*time.Second || cfg.Render.MaxRetries != 1 || cfg.Render.RetryBackoff != 100*time.Millisecond || cfg.Render.MaxConcurrent != 4 || cfg.Render.FallbackText != "图片暂时不可用" {
		t.Fatalf("unexpected render config: %+v", cfg.Render)
	}
	if cfg.Storage.Backend != "sqlite" || cfg.Storage.Path != "data/plugin-state.sqlite" || cfg.Storage.Redis.MaxRetries != 2 {
		t.Fatalf("unexpected storage config: %+v", cfg.Storage)
	}
	if cfg.Dispatch.MaxConcurrent != 256 {
		t.Fatalf("unexpected dispatch config: %+v", cfg.Dispatch)
	}
}

func TestValidateRedisStorage(t *testing.T) {
	cfg := Default()
	cfg.OneBot.AccessToken = "unit-test-token"
	cfg.Storage = Storage{Backend: "redis", Redis: RedisStorage{Addr: "127.0.0.1:6379", MaxRetries: 2}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, modify := range []func(*Config){
		func(c *Config) { c.Storage.Backend = "unknown" },
		func(c *Config) { c.Storage.Redis.Addr = "" },
		func(c *Config) { c.Storage.Redis.DB = -1 },
		func(c *Config) { c.Storage.Redis.MaxRetries = -1 },
	} {
		invalid := cfg
		modify(&invalid)
		if err := invalid.Validate(); err == nil {
			t.Fatal("invalid Redis storage accepted")
		}
	}
}

func TestLoadPluginPolicyAndValidateSubjects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.yaml")
	data := []byte("onebot:\n  access_token: offline-test-token\nplugins:\n  masters:\n    10: [20, 21]\n  allowed_capabilities: [messages, render]\n  deny_users: [30]\n  allow_groups: [40]\n  cooldown: 2s\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	policy := cfg.Plugins.SDKPolicy()
	if len(policy.Masters[10]) != 2 || policy.Masters[10][0] != 20 || len(policy.AllowedCapabilities) != 2 || policy.DenyUsers[0] != 30 || policy.AllowGroups[0] != 40 || policy.Cooldown != 2*time.Second {
		t.Fatalf("policy = %+v", policy)
	}
	for _, modify := range []func(*Config){
		func(c *Config) { c.Plugins.Cooldown = -time.Second },
		func(c *Config) { c.Plugins.Masters = map[int64][]int64{0: {20}} },
		func(c *Config) { c.Plugins.Masters = map[int64][]int64{10: {-1}} },
		func(c *Config) { c.Plugins.AllowUsers = []int64{0} },
	} {
		invalid := cfg
		modify(&invalid)
		if err := invalid.Validate(); err == nil {
			t.Fatal("invalid plugin policy accepted")
		}
	}
}

func TestValidateRejectsMissingToken(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted an empty token")
	}
}

func TestValidateDispatchConcurrency(t *testing.T) {
	cfg := Default()
	cfg.OneBot.AccessToken = "unit-test-token"
	cfg.Dispatch.MaxConcurrent = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted zero dispatch concurrency")
	}
	cfg.Dispatch.MaxConcurrent = 4097
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted dispatch concurrency above 4096")
	}
	cfg.Dispatch.MaxConcurrent = 512
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid dispatch concurrency rejected: %v", err)
	}
}

func TestValidateRejectsCredentialURL(t *testing.T) {
	cfg := Default()
	cfg.OneBot.AccessToken = "unit-test-token"
	cfg.OneBot.URL = "ws://user:secret@127.0.0.1:3001/"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted credentials in the URL")
	}
}

func TestValidateRejectsQueryToken(t *testing.T) {
	for _, raw := range []string{
		"ws://127.0.0.1:3001/?access_token=secret",
		"ws://127.0.0.1:3001/?Token=secret",
		"ws://127.0.0.1:3001/?password=secret",
	} {
		cfg := Default()
		cfg.OneBot.AccessToken = "unit-test-token"
		cfg.OneBot.URL = raw
		if err := cfg.Validate(); err == nil {
			t.Fatalf("Validate() accepted query credentials in %q", raw)
		}
	}
}
