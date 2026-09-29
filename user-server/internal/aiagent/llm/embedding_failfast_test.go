package llm

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// refusedAddr 返回一个刚刚释放的 127.0.0.1 端口：再连必然 connection refused，
// 且不会像固定端口那样可能被别的进程占着（那样测的就不是"不可达"了）。
func refusedAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("取空闲端口失败: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("释放端口失败: %v", err)
	}
	return addr
}

func embeddingErrConfig(svc *EmbeddingService, baseURL string) *EmbeddingConfig {
	cfg := svc.DefaultConfig()
	cfg.BaseURL = baseURL
	cfg.Model = "bge-m3"
	cfg.Dimension = 1024
	cfg.AllowFallback = false
	cfg.APIKey = "x"
	return cfg
}

// TestEmbeddingUnreachableFailsFast 守护"端口拒绝连接 ⇒ 立即返回"这一条在线时延铁律。
// 退避只对瞬时故障有意义：127.0.0.1:8208 被拒说明推理栈根本没起来，同一请求内重试 5 次
// （1+2+4+8+16s 退避）只会把一次检索拖成 30s，实测把 bridge AI 回复从秒级推到 60–260s。
func TestEmbeddingUnreachableFailsFast(t *testing.T) {
	clearEmbeddingEnv(t)
	svc := NewEmbeddingService()
	cfg := embeddingErrConfig(svc, "http://"+refusedAddr(t)+"/v1")
	if cfg.MaxRetries < 2 {
		t.Fatalf("夹具失效：本用例要测生产默认的多重试档，实际 MaxRetries=%d", cfg.MaxRetries)
	}

	start := time.Now()
	_, err := svc.Embed(context.Background(), cfg, []string{"hello"})
	elapsed := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "本地 embedding 服务不可达") {
		t.Fatalf("不可达时应返回明确错误, 实际 %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("端口拒绝连接却仍在退避重试：耗时 %.1fs（应 <3s 快速失败）", elapsed.Seconds())
	}
}

// TestEmbeddingServerErrorStillRetries 是上一例的反向保险：快速失败只针对"连不上"，
// 已连上但对端 5xx 属瞬时故障，必须保留重试，否则改完把真实抖动一并杀掉了。
func TestEmbeddingServerErrorStillRetries(t *testing.T) {
	clearEmbeddingEnv(t)
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	t.Cleanup(srv.Close)

	svc := NewEmbeddingService()
	cfg := embeddingErrConfig(svc, srv.URL+"/v1")
	cfg.MaxRetries = 3
	cfg.RequestTimeout = 5

	start := time.Now()
	_, err := svc.Embed(context.Background(), cfg, []string{"hello"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("对端持续 5xx 时应返回错误")
	}
	mu.Lock()
	got := hits
	mu.Unlock()
	if got != 3 {
		t.Fatalf("5xx 属可重试故障，应打满 %d 次，实际 %d 次", cfg.MaxRetries, got)
	}
	if elapsed < time.Second {
		t.Fatalf("未观察到退避痕迹（耗时 %.1fs），重试链路可能被误伤", elapsed.Seconds())
	}
}
