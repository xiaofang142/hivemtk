package controller

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

// T-P2-02 控制器侧的入口核对：公开验签入口必须带着 guard 写下的"已验平台"才允许落库。
// 这里刻意在测试里**只注册控制器、不挂 guard**（生产路由是挂了的），
// 用来证"忘了挂 guard 的新路由不会静默收下推送"。

func orderWebhookSign(secret, platform, ts, nonce string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(platform + "\n" + ts + "\n" + nonce + "\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func setupOrderWebhookControllerDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.SystemConfigKV{},
		&model.ExternalOrder{},
		&model.WebhookEvent{},
	)
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })
	return database
}

func postJSON(r *gin.Engine, path string, headers map[string]string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestReceiveOrderWebhook_PublicEntryRejectsUnverified 新注册一条"看起来是公开入口"的路由
// 但没挂 guard ⇒ 必须 403，且不许写镜像。
func TestReceiveOrderWebhook_PublicEntryRejectsUnverified(t *testing.T) {
	database := setupOrderWebhookControllerDB(t)
	platform := "tp202c" + strconv.FormatInt(time.Now().UnixNano()%1_000_000, 10)
	t.Cleanup(func() {
		_ = database.Where("platform = ?", platform).Delete(&model.ExternalOrder{}).Error
		_ = database.Where("platform = ?", platform).Delete(&model.WebhookEvent{}).Error
	})

	ctrl := NewIntegrationController()
	r := setupGinEngine()
	r.POST(middleware.OrderWebhookVerifiedPathPrefix+":"+middleware.OrderWebhookPlatformParam,
		ctrl.ReceiveOrderWebhook) // 故意不挂 guard

	body := []byte(`{"order_id":"UNVERIFIED-1","status":"paid"}`)
	w := postJSON(r, middleware.OrderWebhookVerifiedPathPrefix+platform, nil, body)
	if w.Code != http.StatusForbidden {
		t.Fatalf("未挂 guard 的公开入口应 403，实际 %d：%s", w.Code, w.Body.String())
	}
	var n int64
	if err := database.Model(&model.ExternalOrder{}).Where("platform = ?", platform).Count(&n).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if n != 0 {
		t.Errorf("被拒的推送不得写镜像，实际 %d 条", n)
	}
}

// TestReceiveOrderWebhook_LegacyEntryStillServesWithoutGuard 旧契约入口不挂 guard 也要照常服务：
// 现网内部调用方没有签名，把核对扩到旧路径等于当场打断它们（AC③"保留一个大版本"）。
func TestReceiveOrderWebhook_LegacyEntryStillServesWithoutGuard(t *testing.T) {
	database := setupOrderWebhookControllerDB(t)
	platform := "tp202l" + strconv.FormatInt(time.Now().UnixNano()%1_000_000, 10)
	t.Cleanup(func() {
		_ = database.Where("platform = ?", platform).Delete(&model.ExternalOrder{}).Error
		_ = database.Where("platform = ?", platform).Delete(&model.WebhookEvent{}).Error
	})

	ctrl := NewIntegrationController()
	r := setupGinEngine()
	r.POST("/api/integration/order-webhook/:"+middleware.OrderWebhookPlatformParam, ctrl.ReceiveOrderWebhook)

	body := []byte(`{"order_id":"LEGACY-OK-1","status":"paid"}`)
	w := postJSON(r, "/api/integration/order-webhook/"+platform, nil, body)
	if w.Code != http.StatusOK {
		t.Fatalf("旧入口带凭证语义（此处由路由组保证）应 200，实际 %d：%s", w.Code, w.Body.String())
	}
	var n int64
	if err := database.Model(&model.ExternalOrder{}).Where("platform = ?", platform).Count(&n).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if n != 1 {
		t.Errorf("旧入口应写入 1 条镜像，实际 %d", n)
	}
}

// TestReceiveOrderWebhook_VerifiedPlatformMismatch guard 写下的平台与路径参数不一致时必须拒。
// 单看中间件不会不一致（同一个 Param 读出来的），所以这条专门证"核对不是走过场"：
// 若将来有人让 guard 从别的来源（比如头里）取平台，跨平台搬运会被这里拦下。
func TestReceiveOrderWebhook_VerifiedPlatformMismatch(t *testing.T) {
	database := setupOrderWebhookControllerDB(t)
	platform := "tp202m" + strconv.FormatInt(time.Now().UnixNano()%1_000_000, 10)
	t.Cleanup(func() {
		_ = database.Where("platform = ?", platform).Delete(&model.ExternalOrder{}).Error
	})

	ctrl := NewIntegrationController()
	r := setupGinEngine()
	r.POST(middleware.OrderWebhookVerifiedPathPrefix+":"+middleware.OrderWebhookPlatformParam,
		func(c *gin.Context) { c.Set(middleware.VerifiedWebhookKey, "some-other-platform") },
		ctrl.ReceiveOrderWebhook)

	body := []byte(`{"order_id":"MISMATCH-1","status":"paid"}`)
	w := postJSON(r, middleware.OrderWebhookVerifiedPathPrefix+platform, nil, body)
	if w.Code != http.StatusForbidden {
		t.Fatalf("已验平台与参数平台不一致应 403，实际 %d：%s", w.Code, w.Body.String())
	}
}
