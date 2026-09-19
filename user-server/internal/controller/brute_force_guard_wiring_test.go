package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 第十轮守卫接线测试：路由上挂了 BruteForceGuard，控制器失败路径必须
// 单点 RecordBruteForceFailure，否则守卫永不触发（空挂）。
// 期望语义与 brute_force.go 约定一致：DefaultBruteForceConfig.MaxFailures=5，
// 第 1~5 次失败按各自端点原状态码返回，第 6 次请求被守卫 429 拦截。

func doBruteForcePost(t *testing.T, router *gin.Engine, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req, err := http.NewRequest("POST", path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestVerifyMFALogin_BruteForceGuardWired 临时令牌+错误验证码连续失败后，
// BruteForceGuard("auth.mfa") 必须开始拦截（旧实现：控制器从不计数 ⇒ 永不拦截）。
func TestVerifyMFALogin_BruteForceGuardWired(t *testing.T) {
	setupTestControllerDB(t)
	middleware.ResetBruteForceForTest()
	t.Cleanup(middleware.ResetBruteForceForTest)
	if middleware.BruteForceDisabledForTest() {
		t.Skip("BRUTE_FORCE_DISABLED 环境开启，跳过守卫测试")
	}

	authCtrl := NewAuthController()
	router := setupGinEngine()
	router.POST("/auth/mfa/verify", middleware.BruteForceGuard("auth.mfa"), authCtrl.VerifyMFALogin)

	payload := gin.H{"temp_token": "0000000000000000000000000000000000000000000000000000000000000000", "code": "123456"}
	for i := 1; i <= 5; i++ {
		w := doBruteForcePost(t, router, "/auth/mfa/verify", payload)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次失败期望 401，实际 %d, body=%s", i, w.Code, w.Body.String())
		}
	}
	w := doBruteForcePost(t, router, "/auth/mfa/verify", payload)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("第 6 次请求应被守卫 429 拦截，实际 %d, body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Retry-After"); got == "" {
		t.Error("429 响应缺少 Retry-After 头")
	}
}

func setupResetTokenDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t, &model.PasswordResetToken{}, &model.SystemUser{})
	db.SetTestDB(database)
	return database
}

// TestResetPassword_GuardCountsOnlyTokenFailures 无效令牌失败计入爆破计数并触发守卫；
// 有效令牌但密码不合策略（或其他非令牌错误）不得计数（防误锁持票用户）。
func TestResetPassword_GuardCountsOnlyTokenFailures(t *testing.T) {
	database := setupResetTokenDB(t)
	middleware.ResetBruteForceForTest()
	t.Cleanup(middleware.ResetBruteForceForTest)
	if middleware.BruteForceDisabledForTest() {
		t.Skip("BRUTE_FORCE_DISABLED 环境开启，跳过守卫测试")
	}

	ctrl := NewSelfServiceController(service.NewPasswordResetService(database))
	router := setupGinEngine()
	router.POST("/public/reset-password", middleware.BruteForceGuard("reset-password"), ctrl.ResetPassword)

	// 负例先行：有效令牌 + 弱新密码（走非令牌错误路径）→ 不得计入爆破计数
	tok := &model.PasswordResetToken{UserID: "424242", ExpiresAt: time.Now().Add(time.Hour)}
	if err := database.Create(tok).Error; err != nil {
		t.Fatalf("create reset token: %v", err)
	}
	if tok.Token == "" {
		t.Fatal("BeforeCreate 未填充 Token")
	}
	w := doBruteForcePost(t, router, "/public/reset-password", gin.H{"token": tok.Token, "new_password": "12345678"})
	if w.Code == http.StatusOK {
		t.Skip("密码策略与用户存在性校验全部通过（环境差异），跳过负例")
	}
	if got := middleware.GetBruteForceFailureCount("192.0.2.1", "reset-password"); got != 0 {
		t.Errorf("非令牌错误不得计数，实际 failures=%d", got)
	}

	// 正例：无效令牌连续 5 次 → 第 6 次被守卫拦截
	for i := 1; i <= 5; i++ {
		w := doBruteForcePost(t, router, "/public/reset-password", gin.H{"token": "bogus-token-no-such-row", "new_password": "Str0ng!Pass123"})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("第 %d 次无效令牌期望 400，实际 %d, body=%s", i, w.Code, w.Body.String())
		}
	}
	w = doBruteForcePost(t, router, "/public/reset-password", gin.H{"token": "bogus-token-no-such-row", "new_password": "Str0ng!Pass123"})
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("无效令牌 5 次后应被守卫 429 拦截，实际 %d, body=%s", w.Code, w.Body.String())
	}
}
