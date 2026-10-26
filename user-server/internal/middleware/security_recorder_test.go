package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"github.com/gin-gonic/gin"
)

// C-5：401 遥测从记录到 kv 落账的整条腿。
// 三条断言各钉一处：①五个 401 abort 点挂了 recordAuthFailure（丢了遥测就瞎）；
// ②Flush 后 kv 里的载荷按天聚合、按形态计数、路径截两段前缀；
// ③再 Flush 一轮是增量合并不是覆盖（多实例/多波次各刷各的，谁也不能抹掉谁）。
func TestSecurityRecorderFlushMergesIntoKV(t *testing.T) {
	database := testutil.NewTestDB(t, &model.PlatformConfigKV{})
	InitSecurityRecorder(database)
	t.Cleanup(func() { InitSecurityRecorder(nil) })

	gin.SetMode(gin.TestMode)
	record := func(kind, path string) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		c, _ := gin.CreateTestContext(nil)
		c.Request = req
		recordAuthFailure(kind, c)
	}

	record(model.SecurityAccessKindTokenMissing, "/api/system/users")
	record(model.SecurityAccessKindTokenMissing, "/api/system/users")
	record(model.SecurityAccessKindTokenInvalid, "/api/security/audit/list")

	if err := FlushSecurityAccessFailures(); err != nil {
		t.Fatalf("首刷不该失败: %v", err)
	}

	repo := repository.NewPlatformConfigKVRepositoryWithDB(database)
	raw, err := repo.Get(context.Background(), model.SecurityAccessFailureKVKey)
	if err != nil || raw == "" {
		t.Fatalf("kv 里应有聚合行: err=%v raw=%q", err, raw)
	}
	payload := model.ParseSecurityAccessFailurePayload(raw)
	today := time.Now().Format("2006-01-02")
	day := payload.Days[today]
	if day == nil {
		t.Fatalf("应有今日聚合: days=%v", payload.Days)
	}
	if day.Kinds[model.SecurityAccessKindTokenMissing] != 2 {
		t.Fatalf("token_missing 应计 2 次，实际 %v", day.Kinds)
	}
	if day.Kinds[model.SecurityAccessKindTokenInvalid] != 1 {
		t.Fatalf("token_invalid 应计 1 次，实际 %v", day.Kinds)
	}
	top := payload.TopPaths(5)
	if len(top) == 0 || top[0] != "/api/system" {
		t.Fatalf("路径应截两段前缀 /api/system，实际 %v", top)
	}

	// 第二波：换个形态再刷，验证合并不是覆盖。
	record(model.SecurityAccessKindTokenExpired, "/api/knowledge-bases")
	if err := FlushSecurityAccessFailures(); err != nil {
		t.Fatalf("二刷不该失败: %v", err)
	}
	raw2, _ := repo.Get(context.Background(), model.SecurityAccessFailureKVKey)
	d2 := model.ParseSecurityAccessFailurePayload(raw2).Days[today]
	if d2 == nil || d2.Kinds[model.SecurityAccessKindTokenMissing] != 2 || d2.Kinds[model.SecurityAccessKindTokenExpired] != 1 {
		t.Fatalf("二刷应合并而非覆盖: %+v", d2)
	}
}

// TestJWTAbortPointsRecordFailures jwt.go 的五个 abort 点必须都挂了记录调用——
// 漏一个那类失败就永远不可见，总览上"没有数据"会被读成"没人攻击"。
func TestJWTAbortPointsRecordFailures(t *testing.T) {
	b, err := os.ReadFile("jwt.go")
	if err != nil {
		t.Fatalf("读取 jwt.go: %v", err)
	}
	src := string(b)
	for _, kind := range []string{
		"SecurityAccessKindTokenMissing", "SecurityAccessKindTokenFormat",
		"SecurityAccessKindTokenInvalid", "SecurityAccessKindTokenExpired",
		"SecurityAccessKindStatusChanged",
	} {
		if !strings.Contains(src, "recordAuthFailure(model."+kind+", c)") {
			t.Errorf("jwt.go 里没有 %s 的记录调用：该类 401 从此不进遥测", kind)
		}
	}
}

// TestPathPrefixOf 前缀截取：两段封顶，细粒度 ID 路径不进聚合键。
func TestPathPrefixOf(t *testing.T) {
	cases := map[string]string{
		"/api/system/users":            "/api/system",
		"/api/module-market/orders/12": "/api/module-market",
		"/api":                         "/api",
		"/":                            "/",
	}
	for in, want := range cases {
		if got := pathPrefixOf(in); got != want {
			t.Errorf("pathPrefixOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// 回归：异步刷盘腿与记录路径并发（-race 钉死）。旧实现把**活指针**递给 flush
// goroutine——goroutine 在锁外 Merge 遍历该 map，而请求路径锁内继续 Add，
// concurrent map iteration and map write 直接 fatal（2026-10-25 D-3 走查
// 18406 实测进程崩溃）。修复后刷盘走换出快照，本用例以 50 并发请求 × 强制
// 触发刷盘复现当时的并发形态，-race 下任何 map 并发都会红。
func TestSecurityRecorder_AsyncFlushRacesWithRecording(t *testing.T) {
	database := testutil.NewTestDB(t, &model.PlatformConfigKV{})
	InitSecurityRecorder(database)
	t.Cleanup(func() { InitSecurityRecorder(nil) })

	gin.SetMode(gin.TestMode)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	// 记录腿：8 个 goroutine 持续打 401 记录（合法 API 面，路径前缀聚在几段）
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			paths := []string{"/api/system/users", "/api/security/audit/list", "/api/quote/list"}
			kinds := []string{model.SecurityAccessKindTokenMissing, model.SecurityAccessKindTokenInvalid}
			for j := 0; ; j++ {
				select {
				case <-stop:
					return
				default:
				}
				req := httptest.NewRequest(http.MethodGet, paths[(n+j)%len(paths)], nil)
				c, _ := gin.CreateTestContext(nil)
				c.Request = req
				recordAuthFailure(kinds[j%len(kinds)], c)
			}
		}(i)
	}
	// 刷盘腿：反复走异步触发形态（flushing 标志 + 锁外 Merge 的同款并发窗口）
	for round := 0; round < 50; round++ {
		securityFailureMu.Lock()
		if !securityFailureFlushing && securityFailureDeltasCount() > 0 {
			securityFailureFlushing = true
			deltas := securityFailureDeltas
			securityFailureMu.Unlock()
			go func() {
				defer func() {
					// 与生产腿走同一扇门（endSecurityFlush），否则这条 -race 腿钉住的
					// 是"测试自己写全局"，而不是线上那条收尾路径。
					endSecurityFlush()
				}()
				_ = flushSecurityAccessFailures(deltas)
			}()
		} else {
			securityFailureMu.Unlock()
		}
		time.Sleep(2 * time.Millisecond)
	}
	close(stop)
	wg.Wait()
	if err := FlushSecurityAccessFailures(); err != nil {
		t.Fatalf("收口刷盘不该失败: %v", err)
	}
}
