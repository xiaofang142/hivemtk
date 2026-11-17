package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBruteForceGuard_NoLockOnRequestAlone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ResetBruteForceForTest()
	defer ResetBruteForceForTest()

	calls := 0
	r := gin.New()
	r.Use(func(c *gin.Context) { calls++; c.Next() })
	r.Use(BruteForceGuard("test.endpoint"))
	r.GET("/x", func(c *gin.Context) { c.Status(200) })

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/x", nil)
		req.RemoteAddr = "192.0.2.1:1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d should pass, got %d", i, w.Code)
		}
	}
	if calls != 10 {
		t.Fatalf("expected 10 successful passes, got %d", calls)
	}
}

func TestBruteForceGuard_LocksAfterRecordFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ResetBruteForceForTest()
	defer ResetBruteForceForTest()

	r := gin.New()
	r.Use(BruteForceGuard("test.endpoint"))
	r.GET("/x", func(c *gin.Context) {
		RecordBruteForceFailure(c, "test.endpoint")
		c.Status(http.StatusUnauthorized)
	})

	ip := "192.0.2.2:1234"

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/x", nil)
		req.RemoteAddr = ip
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d should reach handler, got %d", i, w.Code)
		}
	}

	req := httptest.NewRequest("GET", "/x", nil)
	req.RemoteAddr = ip
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("attempt 6 should be locked (429), got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After header should be set")
	}
}

func TestBruteForceGuard_ClearResetsLock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ResetBruteForceForTest()
	defer ResetBruteForceForTest()

	r := gin.New()
	r.Use(BruteForceGuard("test.endpoint"))
	hitHandler := false
	r.GET("/x", func(c *gin.Context) {
		hitHandler = true
		ClearBruteForceFailure(c, "test.endpoint")
		c.Status(http.StatusOK)
	})

	ip := "192.0.2.3:1234"

	for i := 0; i < 6; i++ {
		req := httptest.NewRequest("GET", "/x", nil)
		req.RemoteAddr = ip
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("clear-loop attempt %d should be 200, got %d", i, w.Code)
		}
	}
	if !hitHandler {
		t.Fatal("handler should be reached")
	}
}

// TestAccountDimensionLockout 账号维度防爆破（2026-11-15 审计 M-4 回归钉）：
// IP 维度挡不住代理池/IPv6 轮换源对同一账号的分布式爆破；账号桶独立计数、
// username 归一（trim+lower）防变体绕开同一桶，任一维度命中即拒。
func TestAccountDimensionLockout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ResetBruteForceForTest()
	defer ResetBruteForceForTest()

	if BruteForceDisabledForTest() {
		t.Skip("BRUTE_FORCE_DISABLED=1，防爆破关闭")
	}

	// 5 次失败（含大小写/空白变体，归一后同桶）触发账号锁定
	for i := 0; i < 5; i++ {
		RecordBruteForceAccountFailure("auth.login", " Admin ")
	}
	locked, retryAfter := CheckBruteForceAccountLocked("auth.login", "admin")
	if !locked {
		t.Fatalf("5 次失败后账号应锁定（变体归一后同桶）")
	}
	if retryAfter < 1 {
		t.Fatalf("Retry-After 应 >= 1 秒，实际 %d", retryAfter)
	}
	if locked2, _ := CheckBruteForceAccountLocked("auth.login", "ADMIN"); !locked2 {
		t.Fatalf("大小写变体应命中同一账号桶")
	}

	// 其他账号不受影响（无横向误伤）
	if locked3, _ := CheckBruteForceAccountLocked("auth.login", "cs01"); locked3 {
		t.Fatalf("未失败的账号不应被锁定")
	}

	// 成功登录清零
	ClearBruteForceAccountFailure("auth.login", "admin")
	if locked4, _ := CheckBruteForceAccountLocked("auth.login", "admin"); locked4 {
		t.Fatalf("Clear 后账号不应仍锁定")
	}
}
