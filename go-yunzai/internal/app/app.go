package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/trss-yunzai/go-yunzai/internal/adapter/onebotv11"
	"github.com/trss-yunzai/go-yunzai/internal/config"
	"github.com/trss-yunzai/go-yunzai/internal/dispatch"
	"github.com/trss-yunzai/go-yunzai/internal/observability"
	pluginmanager "github.com/trss-yunzai/go-yunzai/internal/plugin"
	"github.com/trss-yunzai/go-yunzai/internal/pluginconfig"
	renderclient "github.com/trss-yunzai/go-yunzai/internal/render"
	"github.com/trss-yunzai/go-yunzai/internal/storage"
	"github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"
	"github.com/trss-yunzai/go-yunzai/plugins/builtin"
)

type App struct {
	onebot     *onebotv11.Client
	dispatcher *dispatch.Dispatcher
	plugins    *pluginmanager.Manager
	renderer   *renderclient.Client
	store      storage.Backend
	settings   *pluginconfig.Registry
}

// Options adjusts which plugins an application instance registers.
type Options struct {
	// DisableBuiltinDebug omits the built-in debug catch-all plugin.
	DisableBuiltinDebug bool
	// ExtraPlugins registers additional plugins after the built-in ones.
	ExtraPlugins []sdk.Plugin
}

func New(cfg config.Config) (*App, error) {
	return NewWith(cfg, Options{})
}

func NewWith(cfg config.Config, opts Options) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	logger, err := observability.NewLogger(cfg.Log.Level)
	if err != nil {
		return nil, fmt.Errorf("create logger: %w", err)
	}

	dispatcher := dispatch.NewWithLimit(logger, cfg.Dispatch.MaxConcurrent)
	onebotClient, err := onebotv11.NewClient(onebotv11.Options{
		URL:               cfg.OneBot.URL,
		AccessToken:       cfg.OneBot.AccessToken,
		Handler:           dispatcher,
		Logger:            logger,
		RequestTimeout:    cfg.OneBot.RequestTimeout,
		HeartbeatInterval: cfg.OneBot.HeartbeatInterval,
		ReconnectInitial:  cfg.OneBot.Reconnect.Initial,
		ReconnectMax:      cfg.OneBot.Reconnect.Max,
	})
	if err != nil {
		return nil, err
	}
	renderer, err := renderclient.NewClient(renderclient.Options{
		URL:           cfg.Render.URL,
		Timeout:       cfg.Render.Timeout,
		MaxRetries:    cfg.Render.MaxRetries,
		RetryBackoff:  cfg.Render.RetryBackoff,
		MaxConcurrent: cfg.Render.MaxConcurrent,
		FallbackText:  cfg.Render.FallbackText,
	})
	if err != nil {
		return nil, err
	}

	// 存储打开预算 30s：实测本机存在持续 ~30s 的机器级 IO 停滞（多进程同时被
	// 拖慢），停滞期间 SQLite 的阻塞 IO 无法被 context 中断，ctx 只能在 IO 返回
	// 后生效；放宽预算可避免"慢但能完成"的启动被过早判定失败。真正损坏的路径
	// （权限/文件锁）仍然立即报错。
	openCtx, cancelOpen := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelOpen()
	store, err := openStorage(openCtx, cfg.Storage)
	if err != nil {
		renderer.Close()
		return nil, fmt.Errorf("open plugin storage: %w", err)
	}
	if sqlite, ok := store.(*storage.SQLite); ok {
		logger.Info("plugin storage opened", "path", sqlite.Path())
	}
	settings, err := pluginconfig.New(cfg.Plugins.Settings)
	if err != nil {
		renderer.Close()
		_ = store.Close()
		return nil, err
	}
	plugins := pluginmanager.NewWithOptions(onebotClient, logger, pluginmanager.Options{
		Policy: cfg.Plugins.SDKPolicy(), Stores: store, Config: settings,
	})
	// 在 Run 接收协议流量之前完成插件注册。
	if !opts.DisableBuiltinDebug && cfg.Debug.Enabled {
		if err := plugins.Register(builtin.NewDebugPlugin(onebotClient, renderer, cfg.Debug.Reply)); err != nil {
			renderer.Close()
			_ = store.Close()
			return nil, err
		}
	}
	for _, plugin := range opts.ExtraPlugins {
		if err := plugins.Register(plugin); err != nil {
			renderer.Close()
			_ = store.Close()
			return nil, err
		}
	}
	dispatcher.Register(plugins)
	return &App{onebot: onebotClient, dispatcher: dispatcher, plugins: plugins, renderer: renderer, store: store, settings: settings}, nil
}

func openStorage(ctx context.Context, cfg config.Storage) (storage.Backend, error) {
	if strings.EqualFold(cfg.Backend, "redis") {
		return storage.OpenRedis(ctx, storage.RedisOptions{
			Addr: cfg.Redis.Addr, Username: cfg.Redis.Username, Password: cfg.Redis.Password,
			DB: cfg.Redis.DB, MaxRetries: cfg.Redis.MaxRetries,
			DialTimeout: cfg.Redis.DialTimeout, ReadTimeout: cfg.Redis.ReadTimeout, WriteTimeout: cfg.Redis.WriteTimeout,
		})
	}
	return storage.Open(ctx, cfg.Path)
}

func (a *App) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := a.plugins.Start(runCtx); err != nil {
		return err
	}
	runErr := a.onebot.Run(runCtx)
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), pluginmanager.DefaultShutdownTimeout)
	defer shutdownCancel()
	stopErr := a.plugins.Stop(shutdownCtx)
	drainErr := a.dispatcher.WaitContext(shutdownCtx)
	return errors.Join(runErr, stopErr, drainErr)
}

// Close releases shared dependencies after Run has returned.
func (a *App) Close() error {
	a.renderer.Close()
	return a.store.Close()
}

// ReloadPluginSettings validates a complete config file, then atomically
// replaces plugin settings only. Connection, policy and database settings
// continue to require an application restart. Failed loads keep the old view.
func (a *App) ReloadPluginSettings(path string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	return a.settings.Replace(cfg.Plugins.Settings)
}

// LoadPlugin starts a constructed plugin on the running application.
func (a *App) LoadPlugin(ctx context.Context, p sdk.Plugin) error {
	return a.plugins.Load(ctx, p)
}

// UnloadPlugin drains and removes one plugin without stopping the application.
func (a *App) UnloadPlugin(ctx context.Context, id string) error {
	return a.plugins.Unload(ctx, id)
}

func Run(ctx context.Context, cfg config.Config) (err error) {
	return RunWith(ctx, cfg, Options{})
}

func RunWith(ctx context.Context, cfg config.Config, opts Options) (err error) {
	application, err := NewWith(cfg, opts)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, application.Close()) }()
	return application.Run(ctx)
}
