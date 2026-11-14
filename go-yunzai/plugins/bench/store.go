// StorePlugin 是阶段零真实写入基准插件（Z2）：每条消息执行一次按用户/会话
// 隔离的 Store.Set + Store.Get 回读校验，衡量"插件 handler + SQLite 逐消息
// 写入"的成本。成功路径不回复，避免干扰负载器的回复配对；失败由框架错误
// 隔离并记录日志。它只服务于 bench 场景，不参与新旧对照的规则集约定。
package bench

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"
)

// storeSummaryEvery 每处理多少条消息输出一次写入延迟汇总。
const storeSummaryEvery = 2000

// storeSlowWriteThreshold 单次 Set+Get 超过该阈值时立即记录慢写入证据。
// busy_timeout(2500) 下文件锁等待上限为 2.5s；超过 1s 的写入在无外部锁持有
// 者时通常意味着机器级 IO 停滞或存储过载，是本基线要捕捉的信号。
const storeSlowWriteThreshold = time.Second

// StorePlugin 在 bench.workload 之后对每条消息做一次真实存储写入。
type StorePlugin struct {
	writes      atomic.Int64
	errors      atomic.Int64
	windowCount atomic.Int64
	windowNS    atomic.Int64
	windowMaxNS atomic.Int64
}

// NewStore 创建真实写入基准插件实例。
func NewStore() sdk.Plugin { return &StorePlugin{} }

func (p *StorePlugin) Descriptor() sdk.Descriptor {
	return sdk.Descriptor{
		ID:           "bench.store",
		Version:      Version,
		Priority:     Priority + 10, // 60：在 bench.workload(50) 之后处理每条消息。
		Events:       []string{"message"},
		Capabilities: []sdk.Capability{sdk.CapabilityMessages, sdk.CapabilityStorage},
	}
}

func (p *StorePlugin) Register(router sdk.Router) error {
	return router.Rule(sdk.Rule{ID: "bench.store.write", Event: "message"}, p.handleWrite)
}

// handleWrite 对每条消息执行 Set + 回读校验。键与内置 #note 相同的隔离维度
// （账号、消息类型、群、用户），值由下一次写入覆盖，数据库大小保持有界。
func (p *StorePlugin) handleWrite(ctx context.Context, pluginCtx *sdk.Context) (sdk.Result, error) {
	if pluginCtx == nil || pluginCtx.Event == nil {
		return sdk.Stop, nil
	}
	if pluginCtx.Store == nil {
		p.errors.Add(1)
		return sdk.Stop, fmt.Errorf("bench.store: store not injected; declare CapabilityStorage")
	}
	var text bytes.Buffer
	for _, segment := range pluginCtx.Event.Message {
		if segment.Type == "text" {
			if value, ok := segment.Data["text"].(string); ok {
				text.WriteString(value)
			}
		}
	}
	key := fmt.Sprintf("bench:a:%d:t:%s:g:%d:u:%d",
		pluginCtx.Runtime.AccountID, pluginCtx.Runtime.MessageType,
		pluginCtx.Runtime.GroupID, pluginCtx.Runtime.UserID)
	value := text.Bytes()

	start := time.Now()
	if err := pluginCtx.Store.Set(ctx, key, value, 0); err != nil {
		p.errors.Add(1)
		return sdk.Stop, fmt.Errorf("bench.store set failed: %w", err)
	}
	readback, err := pluginCtx.Store.Get(ctx, key)
	if err != nil {
		p.errors.Add(1)
		return sdk.Stop, fmt.Errorf("bench.store get failed: %w", err)
	}
	if !bytes.Equal(readback, value) {
		p.errors.Add(1)
		return sdk.Stop, fmt.Errorf("bench.store readback mismatch: got %d bytes, want %d", len(readback), len(value))
	}
	elapsed := time.Since(start)
	total := p.writes.Add(1)
	if elapsed > storeSlowWriteThreshold {
		slog.Warn("bench.store slow write", "elapsed_ms", elapsed.Milliseconds(), "total_writes", total)
	}
	count := p.windowCount.Add(1)
	p.windowNS.Add(elapsed.Nanoseconds())
	for {
		current := p.windowMaxNS.Load()
		if elapsed.Nanoseconds() <= current || p.windowMaxNS.CompareAndSwap(current, elapsed.Nanoseconds()) {
			break
		}
	}
	if count >= storeSummaryEvery {
		// 窗口重置与统计之间存在并发事件造成的轻微竞态，对基准汇总可接受。
		p.windowCount.Store(0)
		avgNS := p.windowNS.Swap(0) / max(count, 1)
		maxNS := p.windowMaxNS.Swap(0)
		slog.Info("bench.store write summary",
			"window_writes", count,
			"avg_ms", float64(avgNS)/1e6,
			"max_ms", float64(maxNS)/1e6,
			"total_writes", total,
			"errors", p.errors.Load(),
		)
	}
	return sdk.Stop, nil
}

func (p *StorePlugin) Start(context.Context) error { return nil }

func (p *StorePlugin) Stop(context.Context) error { return nil }
