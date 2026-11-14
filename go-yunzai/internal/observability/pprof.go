// 可选 pprof 端点：设置环境变量 YZGO_PPROF_ADDR（如 127.0.0.1:6060）后启用，
// 供阶段零剖析采集 CPU/block/mutex/heap 数据。未设置时不监听任何端口。
// 监听地址只允许 loopback（127.0.0.0/8 或 ::1）；0.0.0.0、空主机名、:: 以及
// 解析到非回环地址的主机名都会被拒绝，生产环境不应把剖析数据暴露到本机之外。
package observability

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime"
	"time"
)

// StartPprofFromEnv 读取 YZGO_PPROF_ADDR；未设置或为空时不启动并返回空操作。
func StartPprofFromEnv() (stop func(), err error) {
	addr := os.Getenv("YZGO_PPROF_ADDR")
	if addr == "" {
		return func() {}, nil
	}
	return StartPprof(addr)
}

// StartPprof 在给定地址上启动 pprof HTTP 服务，并开启 block/mutex 采样。
// 返回的 stop 会关闭监听并关闭采样；调用方负责在进程退出前调用。
func StartPprof(addr string) (stop func(), err error) {
	if addr == "" {
		return func() {}, fmt.Errorf("pprof address is empty")
	}
	// 先解析再校验：空主机名（":6060"）与 0.0.0.0/[::] 都会解析为通配地址，
	// 会被 IsLoopback 拒绝；localhost 这类主机名必须解析到回环地址才放行。
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return func() {}, fmt.Errorf("pprof address %s: %w", addr, err)
	}
	if tcpAddr.IP == nil || !tcpAddr.IP.IsLoopback() {
		return func() {}, fmt.Errorf("pprof address %s must resolve to loopback (127.0.0.0/8 or ::1), got %s", addr, tcpAddr)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	// block：每 10µs 阻塞时间采样一次；mutex：1/100 采样。监听成功后才开启，
	// 失败路径不留下全局采样开销。
	runtime.SetBlockProfileRate(10_000)
	runtime.SetMutexProfileFraction(100)

	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		runtime.SetBlockProfileRate(0)
		runtime.SetMutexProfileFraction(0)
		return func() {}, fmt.Errorf("pprof listen %s: %w", addr, err)
	}
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = server.Serve(listener) }()
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		_ = listener.Close()
		// 端点关闭后继续采样只会留下运行时开销；若调用方需要其他采样配置，
		// 应在 stop 之后自行重新设置。
		runtime.SetBlockProfileRate(0)
		runtime.SetMutexProfileFraction(0)
	}, nil
}
