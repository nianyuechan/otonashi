package loadgen

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/coder/websocket"
)

// maxLoadReconnects 限制一次基准运行内允许的断线重连次数，防止被测机器人
// 反复崩溃时负载器无限等待。
const maxLoadReconnects = 8

// Run 执行一次完整基准：建立连接 → 预热 → 推送负载 → 等待回复 → 输出结果。
func Run(ctx context.Context, cfg Config, log *slog.Logger, monitor *pidMonitor) (Result, error) {
	startedAt := time.Now()
	conn, activeListener, err := connect(ctx, cfg, log)
	if err != nil {
		return Result{}, err
	}
	session := newSession(cfg, conn, log)
	session.listener = activeListener
	defer func() {
		if c := session.currentConn(); c != nil {
			_ = c.Close(websocket.StatusNormalClosure, "bench finished")
		}
		if activeListener != nil {
			activeListener.close()
		}
	}()

	sessionCtx, cancelSession := context.WithCancel(ctx)
	defer cancelSession()
	session.startRead(sessionCtx, conn)

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
	case err := <-session.readErrors():
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
	graceTimer := time.After(cfg.ReplyGrace)
graceDrain:
	for {
		select {
		case err := <-session.readErrors():
			// 旧版在负载完成后不会主动断开；读循环出错仍要先收结果再返回。
			log.Warn("读循环提前结束", "error", err)
		case <-graceTimer:
			break graceDrain
		}
	}
	cancelSession()

	events := session.eventRecords()
	actionCount, actionErrs, _ := session.counters()
	textReplies, textSamples := session.textReplyStats()
	samples := monitor.samples()
	wallMS := float64(time.Since(monitorStart)) / 1e6
	summary := BuildSummary(events, samples, float64(actionCount), float64(actionErrs), wallMS, len(events), textReplies, textSamples)
	result := Result{
		SchemaVersion:   ResultSchemaVersion,
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

// connect 建立 WebSocket 连接；server 模式同时返回保留的监听器，供断线后
// 等待被测机器人重新接入续跑。
func connect(ctx context.Context, cfg Config, log *slog.Logger) (*websocket.Conn, *listener, error) {
	switch cfg.Mode {
	case ModeServer:
		return listenKeep(ctx, cfg, log)
	case ModeClient:
		dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		conn, _, err := websocket.Dial(dialCtx, cfg.URL, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("连接 %s 失败: %w", cfg.URL, err)
		}
		log.Info("已连入被测机器人", "url", cfg.URL)
		return conn, nil, nil
	default:
		return nil, nil, fmt.Errorf("未知模式 %q", cfg.Mode)
	}
}

// pushWithRecovery 推送一条事件；失败且可重连时等待被测机器人重新接入后重试
// 同一条（测量记录在写入成功后才登记，重试不会产生孤儿配对）。
func pushWithRecovery(ctx context.Context, session *session, spec messageSpec, index int, log *slog.Logger, used *int) error {
	err := session.push(ctx, spec, index)
	for err != nil && session.reconnectable() && *used < maxLoadReconnects && ctx.Err() == nil {
		*used++
		log.Warn("推送失败，尝试等待重连后继续", "index", index, "error", err, "attempt", *used)
		conn, rerr := session.reconnect(ctx, log)
		if rerr != nil {
			return fmt.Errorf("推送第 %d 条失败且重连失败: %w", index, rerr)
		}
		session.startRead(ctx, conn)
		err = session.push(ctx, spec, index)
	}
	return err
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
	reconnects := 0
	for {
		at := time.Now()
		if at.After(deadline) {
			return nil
		}
		spec := session.gen.next(index)
		if err := pushWithRecovery(ctx, session, spec, index, log, &reconnects); err != nil {
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
	reconnects := 0
	for index := 0; index < cfg.Count; index++ {
		spec := session.gen.next(index)
		if err := pushWithRecovery(ctx, session, spec, index, log, &reconnects); err != nil {
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
