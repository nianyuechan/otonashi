// yunzai-bench 是阶段零基准专用入口：与 cmd/yunzai 相同的应用接线，但注册
// bench.workload 插件并停用内置调试 catch-all，保证与旧版基准负载一致。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/trss-yunzai/go-yunzai/internal/app"
	"github.com/trss-yunzai/go-yunzai/internal/config"
	"github.com/trss-yunzai/go-yunzai/internal/observability"
	"github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"
	"github.com/trss-yunzai/go-yunzai/plugins/bench"
)

func main() {
	if err := run(); err != nil {
		slog.Error("yunzai-bench stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "config.yaml", "path to YAML configuration")
	storeWrites := flag.Bool("store", false, "register bench.store to write every message to plugin storage (phase-zero Z2 baseline)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	pprofStop, err := observability.StartPprofFromEnv()
	if err != nil {
		return fmt.Errorf("start pprof: %w", err)
	}
	defer pprofStop()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	extra := []sdk.Plugin{bench.New()}
	if *storeWrites {
		extra = append(extra, bench.NewStore())
	}
	return app.RunWith(ctx, cfg, app.Options{
		DisableBuiltinDebug: true,
		ExtraPlugins:        extra,
	})
}
