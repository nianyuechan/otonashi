package observability

import (
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestStartPprofRejectsInvalidAddress(t *testing.T) {
	if _, err := StartPprof("bad-address-without-port"); err == nil {
		t.Fatal("expected error for invalid address")
	}
	_, err := StartPprof("")
	if err == nil {
		t.Fatal("expected error for empty address")
	}
}

// 非 loopback 地址必须全部拒绝：通配地址（0.0.0.0/::/空主机名）与公网、
// 链路本地地址都不允许作为 pprof 监听目标。
func TestStartPprofRejectsNonLoopbackAddresses(t *testing.T) {
	for _, addr := range []string{
		"0.0.0.0:60614",
		":60614",
		"[::]:60614",
		"192.0.2.1:60614",
		"[fe80::1]:60614",
	} {
		stop, err := StartPprof(addr)
		if err == nil {
			stop()
			t.Fatalf("StartPprof(%q) accepted non-loopback address", addr)
		}
	}
}

// IPv6 回环同样允许；环境没有 ::1 时跳过而不是失败。
func TestStartPprofServesOnIPv6Loopback(t *testing.T) {
	const addr = "[::1]:60615"
	stop, err := StartPprof(addr)
	if err != nil {
		t.Skipf("环境不可用，跳过: %v", err)
	}
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + addr + "/debug/pprof/heap?debug=1")
	if err != nil {
		t.Fatalf("pprof heap request over ::1 failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pprof heap status = %d", resp.StatusCode)
	}
}

func TestStartPprofHandlerResponds(t *testing.T) {
	// 用固定高位端口；占用时跳过而不是失败。
	const addr = "127.0.0.1:60613"
	stop, err := StartPprof(addr)
	if err != nil {
		t.Skipf("端口被占用，跳过: %v", err)
	}
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + addr + "/debug/pprof/heap?debug=1")
	if err != nil {
		t.Fatalf("pprof heap request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pprof heap status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || len(body) == 0 {
		t.Fatalf("pprof heap body empty or read failed: %v", err)
	}

	stop()
	stop() // stop 必须可以重复调用。
}

func TestStartPprofFromEnvDisabledByDefault(t *testing.T) {
	t.Setenv("YZGO_PPROF_ADDR", "")
	stop, err := StartPprofFromEnv()
	if err != nil {
		t.Fatalf("StartPprofFromEnv with empty env should not fail: %v", err)
	}
	defer stop() // 未启用时返回空操作。
	_ = os.Unsetenv("YZGO_PPROF_ADDR")
}
