// onebot-bench 是阶段零基准负载器。server 模式等待 go-yunzai 连入，client
// 模式连入旧版 TRSS-Yunzai；两种模式使用同一负载定义与指标口径。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/trss-yunzai/bench/internal/loadgen"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "onebot-bench:", err)
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := loadgen.ParseFlags(os.Args[1:])
	if err != nil {
		flag.Usage()
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	monitor := loadgen.NewPidMonitor(cfg.SamplePID, cfg.SampleInterval, log)
	monitor.Start()

	result, err := loadgen.Run(ctx, cfg, log, monitor)
	if err != nil {
		return err
	}
	if err := loadgen.WriteResult(cfg.OutPath, result); err != nil {
		return fmt.Errorf("写出结果失败: %w", err)
	}

	summary := result.Summary
	log.Info("基准完成",
		"sent", summary.Sent,
		"paired", summary.PairedReplies,
		"missing", summary.MissingReplies,
		"eps", fmt.Sprintf("%.1f", summary.ThroughputEPS),
		"p50_ms", fmt.Sprintf("%.2f", summary.LatencyP50MS),
		"p95_ms", fmt.Sprintf("%.2f", summary.LatencyP95MS),
		"p99_ms", fmt.Sprintf("%.2f", summary.LatencyP99MS),
		"max_ms", fmt.Sprintf("%.2f", summary.LatencyMaxMS),
		"cpu_avg", fmt.Sprintf("%.1f", summary.CPUAvgPercent),
		"rss_max_mb", fmt.Sprintf("%.1f", float64(summary.RSSMaxBytes)/1024/1024),
		"out", cfg.OutPath,
	)
	return nil
}
