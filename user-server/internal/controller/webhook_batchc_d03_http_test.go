package controller

// 审计 D-03 的 HTTP 层收口。service 包内的用例只能证明 Receive 会拒，
// 这里补上真正对外的那一段：路由是否存在、状态码是不是 400、有没有把"下一步该用哪个入口"
// 带给渠道方；顺带坐实一件读码定不了的事——/api/webhook/dingtalk/:account_id 与
// 通用通配 /:channel/:account_id 在同一棵路由树上共存，谁拿到请求由静态段优先级决定。

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// gateRejectMarker 通用路由能力闸的拒绝文案指纹。
const gateRejectMarker = "通用 webhook"

func newWebhookRouteEnv(t *testing.T) (*gin.Engine, *service.WebhookService, *gorm.DB) {
	t.Helper()
	// 关掉不安全豁免：让"被能力闸拒"和"被验签拒"在响应里可区分。
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "")
	database := testutil.NewTestDB(t,
		&model.WebhookEvent{},
		&model.UnifiedMessage{},
		&model.MessageHub{},
		&model.IntegrationAccount{},
		&model.DingTalkAppAccount{},
	)
	dbutil.SetTestDB(database)
	t.Cleanup(func() { dbutil.SetTestDB(nil) })

	webhookSvc := service.NewWebhookService(database)
	t.Cleanup(func() { webhookSvc.Stop(context.Background()) })

	gin.SetMode(gin.TestMode)
	g := gin.New()
	ctrl := NewWebhookController(webhookSvc)
	ctrl.SetDingTalkAppService(service.NewDingTalkAppService(database, webhookSvc))
	ctrl.RegisterRoutes(g)
	return g, webhookSvc, database
}

func postWebhook(t *testing.T, g *gin.Engine, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	return rec
}

// TestWebhookRoute_NoAdapterChannelsGet400WithGuidance 无适配器渠道必须 400 且给出正确入口。
// 状态码是这里唯一的对外契约：回 200 就等于告诉渠道方"推送成功、不必重投"，
// 而消息既没进收件箱也不会被回复（这正是 D-03 的现场）。
func TestWebhookRoute_NoAdapterChannelsGet400WithGuidance(t *testing.T) {
	g, _, db := newWebhookRouteEnv(t)

	cases := []struct {
		path     string
		wantHint string
	}{
		{"/api/webhook/kuaishou/1", "/api/bridge/ingest"},
		{"/api/webhook/xiaohongshu/1", "/api/bridge/ingest"},
		{"/api/webhook/xianyu/1", "/api/bridge/ingest"},
		{"/api/webhook/custom/1", "无入站适配器"},
	}
	for _, tc := range cases {
		rec := postWebhook(t, g, tc.path, []byte(`{"event_id":"d03-http","content":"客户消息"}`))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s 应 400，got %d body=%s", tc.path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), tc.wantHint) {
			t.Errorf("%s 拒绝理由要指向 %q，got %s", tc.path, tc.wantHint, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"accepted":false`) {
			t.Errorf("%s 响应要显式标出未受理，got %s", tc.path, rec.Body.String())
		}
		var n int64
		if err := db.Model(&model.WebhookEvent{}).Where("raw_data LIKE ?", "%d03-http%").Count(&n).Error; err != nil {
			t.Fatalf("count events %s: %v", tc.path, err)
		}
		if n != 0 {
			t.Errorf("%s 被拒后不应留下事件行，实际 %d 条", tc.path, n)
		}
	}
}

// TestWebhookRoute_AdaptedChannelsNotCaughtByGate 反向闸：有适配器的渠道不能被能力闸一起拒了。
// 未配密钥会让它们因验签被拒，但那句话必须是验签报错，不是"渠道不支持"。
func TestWebhookRoute_AdaptedChannelsNotCaughtByGate(t *testing.T) {
	g, _, _ := newWebhookRouteEnv(t)

	for _, ch := range []string{"wecom", "whatsapp", "telegram", "qq", "feishu", "douyin", "tiktok"} {
		rec := postWebhook(t, g, "/api/webhook/"+ch+"/1", []byte(`{"event_id":"d03-ok"}`))
		if strings.Contains(rec.Body.String(), gateRejectMarker) {
			t.Errorf("渠道 %s 有入站适配器，却被能力闸当成不支持：%d %s", ch, rec.Code, rec.Body.String())
		}
	}
}

// TestWebhookRoute_DingTalkStaticRouteBeatsGenericWildcard 证明钉钉专用回调没被通用通配截走：
// RegisterRoutes 里 `/:channel/:account_id` 先注册，`/dingtalk/:account_id` 后注册，
// 谁赢取决于 gin 的静态段优先级。若通用路由赢了，钉钉入站会整渠道被能力闸拒（400 + 指路文案）。
func TestWebhookRoute_DingTalkStaticRouteBeatsGenericWildcard(t *testing.T) {
	g, _, _ := newWebhookRouteEnv(t)

	rec := postWebhook(t, g, "/api/webhook/dingtalk/1", []byte(`{"msgtype":"text"}`))
	if strings.Contains(rec.Body.String(), gateRejectMarker) {
		t.Fatalf("钉钉专用路由被通用通配截走了：响应来自能力闸而不是 DingTalkReceive → %d %s", rec.Code, rec.Body.String())
	}
	// 没有签名头时专用入口自己会报错，报错文案来自 DingTalkAppService。
	if rec.Code == http.StatusOK {
		t.Errorf("无签名的钉钉回调不得被当作受理，got %d %s", rec.Code, rec.Body.String())
	}
}
