package loadgen

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/coder/websocket"
)

// Run 执行一次完整基准：建立连接 → 预热 → 推送负载 → 等待回复 → 输出结果。
func Run(ctx context.Context, cfg Config, log *slog.Logger, monitor *pidMonitor) (Result, error) {
	startedAt := time.Now()
	conn, err := connect(ctx, cfg, log)
	if err != nil {
		return Result{}, err
	}
	defer conn.Close(websocket.StatusNormalClosure, "bench finished")

	session := newSession(cfg, conn, log)
	sessionCtx, cancelSession := context.WithCancel(ctx)
	defer cancelSession()
	readErr := make(chan error, 1)
	go func() { readErr <- session.readLoop(sessionCtx) }()

	if cfg.Mode == ModeClient {
		// 旧版 TRSS-Yunzai 是反向 WS 服务端：连入后必须先发 lifecycle connect，
		// 随后回答它的初始化动作序列（get_login_info/get_cookies 等）。
		if err := session.write(lifecycleEvent(cfg)); err != nil {
			return Result{}, fmt.Errorf("发送 lifecycle 失败: %w", err)
		}
		log.Info("已发送 lifecycle connect，等待旧版初始化")
	}

	log.Info("预热等待", "duration", cfg.Warmup)
	select {
	case err := <-readErr:
		return Result{}, fmt.Errorf("连接在预热期断开: %w", err)
	case <-time.After(cfg.Warmup):
	}

	monitorStart := time.Now()
	loadErr := runLoad(ctx, cfg, session, log)
	if loadErr != nil {
		cancelSession()
		return Result{}, fmt.Errorf("负载推送失败: %w", loadErr)
	}
	log.Info("负载推送完成，等待尾部回复", "grace", cfg.ReplyGrace)
	select {
	case err := <-readErr:
		// 旧版在负载完成后不会主动断开；读循环出错仍要先收结果再返回。
		log.Warn("读循环提前结束", "error", err)
	case <-time.After(cfg.ReplyGrace):
	}
	cancelSession()

	events := session.eventRecords()
	actionCount, actionErrs, _ := session.counters()
	samples := monitor.samples()
	wallMS := float64(time.Since(monitorStart)) / 1e6
	summary := BuildSummary(events, samples, float64(actionCount), float64(actionErrs), wallMS, len(events))
	result := Result{
		ProtocolVersion: ProtocolVersion,
		Mode:            string(cfg.Mode),
		Workload:        string(cfg.Workload),
		Kind:            string(cfg.Kind),
		Rate:            cfg.Rate,
		DurationMS:      float64(cfg.Duration.Milliseconds()),
		Count:           cfg.Count,
		SelfID:          cfg.SelfID,
		UserID:          cfg.UserID,
		GroupID:         cfg.GroupID,
		MessageType:     cfg.MessageType,
		GroupCount:      cfg.GroupCount,
		UserCount:       cfg.UserCount,
		StartedAt:       startedAt.Format(time.RFC3339),
		FinishedAt:      time.Now().Format(time.RFC3339),
		Summary:         summary,
		Events:          events,
		Samples:         samples,
	}
	return result, nil
}

// connect 建立 WebSocket 连接并返回连接对象。
func connect(ctx context.Context, cfg Config, log *slog.Logger) (*websocket.Conn, error) {
	switch cfg.Mode {
	case ModeServer:
		return listenAccept(ctx, cfg, log)
	case ModeClient:
		dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		conn, _, err := websocket.Dial(dialCtx, cfg.URL, nil)
		if err != nil {
			return nil, fmt.Errorf("连接 %s 失败: %w", cfg.URL, err)
		}
		log.Info("已连入被测机器人", "url", cfg.URL)
		return conn, nil
	default:
		return nil, fmt.Errorf("未知模式 %q", cfg.Mode)
	}
}

func runLoad(ctx context.Context, cfg Config, session *session, log *slog.Logger) error {
	switch cfg.Kind {
	case KindSteady:
		return runSteady(ctx, cfg, session, log)
	case KindBurst:
		return runBurst(ctx, cfg, session, log)
	default:
		return fmt.Errorf("未知场景 %q", cfg.Kind)
	}
}

func runSteady(ctx context.Context, cfg Config, session *session, log *slog.Logger) error {
	interval := time.Duration(float64(time.Second) / cfg.Rate)
	deadline := time.Now().Add(cfg.Duration)
	log.Info("开始稳定负载", "rate", cfg.Rate, "duration", cfg.Duration, "workload", cfg.Workload)
	index := 0
	for {
		at := time.Now()
		if at.After(deadline) {
			return nil
		}
		spec := session.gen.next(index)
		if err := session.push(ctx, spec, index); err != nil {
			return fmt.Errorf("推送第 %d 条失败: %w", index, err)
		}
		index++
		next := at.Add(interval)
		if sleep := time.Until(next); sleep > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(sleep):
			}
		}
	}
}

func runBurst(ctx context.Context, cfg Config, session *session, log *slog.Logger) error {
	log.Info("开始突发负载", "count", cfg.Count, "workload", cfg.Workload)
	start := time.Now()
	for index := 0; index < cfg.Count; index++ {
		spec := session.gen.next(index)
		if err := session.push(ctx, spec, index); err != nil {
			return fmt.Errorf("推送第 %d 条失败: %w", index, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	log.Info("突发负载推送完成", "took", time.Since(start))
	return nil
}
