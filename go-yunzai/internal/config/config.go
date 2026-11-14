package config

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/trss-yunzai/go-yunzai/internal/pluginconfig"
	"github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"
	"gopkg.in/yaml.v3"
)

const DefaultDebugReply = "[go-yunzai] M1 调试回复"

type Config struct {
	OneBot   OneBot   `yaml:"onebot"`
	Render   Render   `yaml:"render"`
	Log      Log      `yaml:"log"`
	Debug    Debug    `yaml:"debug"`
	Dispatch Dispatch `yaml:"dispatch"`
	Plugins  Plugins  `yaml:"plugins"`
	Storage  Storage  `yaml:"storage"`
}

// Dispatch 控制入站事件的并发处理上限。
type Dispatch struct {
	MaxConcurrent int `yaml:"max_concurrent"`
}

type Storage struct {
	Backend string       `yaml:"backend"`
	Path    string       `yaml:"path"`
	Redis   RedisStorage `yaml:"redis"`
}

type RedisStorage struct {
	Addr         string        `yaml:"addr"`
	Username     string        `yaml:"username"`
	Password     string        `yaml:"password"`
	DB           int           `yaml:"db"`
	MaxRetries   int           `yaml:"max_retries"`
	DialTimeout  time.Duration `yaml:"dial_timeout"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
}

// Plugins configures trusted account owners and plugin admission policies.
type Plugins struct {
	Masters             map[int64][]int64              `yaml:"masters"`
	AllowedCapabilities []sdk.Capability               `yaml:"allowed_capabilities"`
	AllowUsers          []int64                        `yaml:"allow_users"`
	DenyUsers           []int64                        `yaml:"deny_users"`
	AllowGroups         []int64                        `yaml:"allow_groups"`
	DenyGroups          []int64                        `yaml:"deny_groups"`
	Cooldown            time.Duration                  `yaml:"cooldown"`
	Settings            map[string]pluginconfig.Layers `yaml:"settings"`
}

func (p Plugins) SDKPolicy() sdk.Policy {
	return sdk.Policy{
		Masters: p.Masters, AllowedCapabilities: p.AllowedCapabilities,
		AllowUsers: p.AllowUsers, DenyUsers: p.DenyUsers,
		AllowGroups: p.AllowGroups, DenyGroups: p.DenyGroups, Cooldown: p.Cooldown,
	}
}

type OneBot struct {
	URL               string        `yaml:"url"`
	AccessToken       string        `yaml:"access_token"`
	RequestTimeout    time.Duration `yaml:"request_timeout"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
	Reconnect         Reconnect     `yaml:"reconnect"`
}

type Reconnect struct {
	Initial time.Duration `yaml:"initial"`
	Max     time.Duration `yaml:"max"`
}

type Log struct {
	Level string `yaml:"level"`
}

type Debug struct {
	Enabled bool   `yaml:"enabled"`
	Reply   string `yaml:"reply"`
}

type Render struct {
	URL           string        `yaml:"url"`
	Timeout       time.Duration `yaml:"timeout"`
	MaxRetries    int           `yaml:"max_retries"`
	RetryBackoff  time.Duration `yaml:"retry_backoff"`
	MaxConcurrent int           `yaml:"max_concurrent"`
	FallbackText  string        `yaml:"fallback_text"`
}

func Default() Config {
	return Config{
		OneBot: OneBot{
			URL:               "ws://127.0.0.1:3001/",
			RequestTimeout:    10 * time.Second,
			HeartbeatInterval: 20 * time.Second,
			Reconnect: Reconnect{
				Initial: 500 * time.Millisecond,
				Max:     30 * time.Second,
			},
		},
		Render: Render{
			URL:           "http://127.0.0.1:8080",
			Timeout:       15 * time.Second,
			MaxRetries:    1,
			RetryBackoff:  100 * time.Millisecond,
			MaxConcurrent: 4,
			FallbackText:  "图片暂时不可用",
		},
		Log:      Log{Level: "info"},
		Debug:    Debug{Reply: DefaultDebugReply},
		Dispatch: Dispatch{MaxConcurrent: 256},
		Storage:  Storage{Backend: "sqlite", Path: "data/plugin-state.sqlite"},
	}
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	cfg := Default()
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	backend := strings.ToLower(strings.TrimSpace(c.Storage.Backend))
	if backend == "" {
		backend = "sqlite"
	}
	switch backend {
	case "sqlite":
		if strings.TrimSpace(c.Storage.Path) == "" || c.Storage.Path == ":memory:" {
			return fmt.Errorf("storage.path must name a persistent database file")
		}
	case "redis":
		if strings.TrimSpace(c.Storage.Redis.Addr) == "" {
			return fmt.Errorf("storage.redis.addr is required when storage.backend is redis")
		}
		if c.Storage.Redis.DB < 0 || c.Storage.Redis.MaxRetries < 0 {
			return fmt.Errorf("storage.redis.db and max_retries must be non-negative")
		}
	default:
		return fmt.Errorf("storage.backend must be sqlite or redis")
	}
	if _, err := pluginconfig.New(c.Plugins.Settings); err != nil {
		return fmt.Errorf("plugins.settings: %w", err)
	}
	u, err := url.Parse(c.OneBot.URL)
	if err != nil || u.Host == "" || (u.Scheme != "ws" && u.Scheme != "wss") {
		return fmt.Errorf("onebot.url must be an absolute ws:// or wss:// URL")
	}
	if u.User != nil {
		return fmt.Errorf("onebot.url must not contain credentials")
	}
	if sensitiveQueryValue(u.Query()) {
		return fmt.Errorf("onebot.url must not contain credentials in query parameters")
	}
	if strings.TrimSpace(c.OneBot.AccessToken) == "" {
		return fmt.Errorf("onebot.access_token is required")
	}
	if c.OneBot.RequestTimeout <= 0 || c.OneBot.HeartbeatInterval <= 0 {
		return fmt.Errorf("onebot timeouts must be positive")
	}
	if c.OneBot.Reconnect.Initial <= 0 || c.OneBot.Reconnect.Max < c.OneBot.Reconnect.Initial {
		return fmt.Errorf("onebot.reconnect requires 0 < initial <= max")
	}
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level must be debug, info, warn, or error")
	}
	if c.Debug.Reply == "" {
		return fmt.Errorf("debug.reply must not be empty")
	}
	renderURL, err := url.Parse(c.Render.URL)
	if err != nil || renderURL.Host == "" || (renderURL.Scheme != "http" && renderURL.Scheme != "https") {
		return fmt.Errorf("render.url must be an absolute http:// or https:// URL")
	}
	if renderURL.User != nil {
		return fmt.Errorf("render.url must not contain credentials")
	}
	if c.Render.Timeout <= 0 || c.Render.RetryBackoff < 0 {
		return fmt.Errorf("render.timeout must be positive and retry_backoff non-negative")
	}
	if c.Render.MaxRetries < 0 || c.Render.MaxRetries > 10 || c.Render.MaxConcurrent <= 0 {
		return fmt.Errorf("render requires 0 <= max_retries <= 10 and max_concurrent > 0")
	}
	if strings.TrimSpace(c.Render.FallbackText) == "" {
		return fmt.Errorf("render.fallback_text must not be empty")
	}
	if c.Plugins.Cooldown < 0 {
		return fmt.Errorf("plugins.cooldown must be non-negative")
	}
	if c.Dispatch.MaxConcurrent <= 0 || c.Dispatch.MaxConcurrent > 4096 {
		return fmt.Errorf("dispatch.max_concurrent must be between 1 and 4096")
	}
	for account, users := range c.Plugins.Masters {
		if account <= 0 {
			return fmt.Errorf("plugins.masters requires positive account ids")
		}
		for _, user := range users {
			if user <= 0 {
				return fmt.Errorf("plugins.masters requires positive user ids")
			}
		}
	}
	for _, ids := range [][]int64{c.Plugins.AllowUsers, c.Plugins.DenyUsers, c.Plugins.AllowGroups, c.Plugins.DenyGroups} {
		for _, id := range ids {
			if id <= 0 {
				return fmt.Errorf("plugin subject lists require positive ids")
			}
		}
	}
	for _, capability := range c.Plugins.AllowedCapabilities {
		if strings.TrimSpace(string(capability)) == "" {
			return fmt.Errorf("plugins.allowed_capabilities must not contain blank entries")
		}
	}
	return nil
}

func sensitiveQueryValue(values url.Values) bool {
	for key := range values {
		switch strings.ToLower(key) {
		case "access_token", "token", "secret", "password", "authorization":
			return true
		}
	}
	return false
}
