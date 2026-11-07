// pipeline_router_auth_test.go CS-02 鉴权/安全负例——**真实 router 链**端到端
// （外部测试包：gin 路由 + 真实 BridgeIngressGuard 中间件 + 真实 bridge HandleHTTPIngest
// → ToEvent 固定键防线 → IngressPipeline → 真实 PG 8217）。
//
// 覆盖三格（失败一律无绑定副作用：0 行、0 次 AI 触发、无账号/租户写入）：
//
//	缺 token → 401；错 token → 401；未配置 token 且未显式 off → 503 fail-closed。
//	合法 token + 恶意 wire Extra.account_id（CH-37b 刀1 固定键防覆盖的端到端半边）：
//	线路伪造的 account_id 被丢弃并计数，落库行/触发元组的归属判定全部用服务端顶层
//	account_id——扩展侧没有任何键位能改写消息归属。
//
// 「账号不属当前 tenant」自证负例由 internal/bridge/tenant_binding_test.go
// （RowSelfCertification：行自证击败 tenant_code 自报）与 channelgw/ws_tenant_test.go
// （TenantBinder 拒绝握手）钉住，本文件不重复。
package channelgw_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"hivemtk-user/internal/bridge"
	"hivemtk-user/internal/channelgw"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// cs02AuthTrigger 可计数 AI 触发 fake：断言「失败 0 触发 / 成功 1 触发且归属=服务端账号」。
type cs02AuthTrigger struct {
	mu    sync.Mutex
	calls int
	last  map[string]string
}

func (f *cs02AuthTrigger) TriggerInboundAI(_ context.Context, channel, accountID, conversationID, customerID, content, eventID string, _ ...service.TriggerInboundOption) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.last = map[string]string{
		"channel": channel, "account": accountID, "conv": conversationID,
		"cust": customerID, "content": content, "event": eventID,
	}
}

func (f *cs02AuthTrigger) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *cs02AuthTrigger) snapshot() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

// cs02AuthSetup 真实 router 链夹具：gin + BridgeIngressGuard + 真实 bridge handler +
// 真实 pipeline/服务 + 真库。返回 router、触发 fake、库句柄。
func cs02AuthSetup(t *testing.T) (*gin.Engine, *cs02AuthTrigger) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.MessageHub{})
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })

	ingress := service.NewInboxIngressService()
	tr := &cs02AuthTrigger{}
	ingress.SetAITrigger(tr)
	service.SetBridgeChannels([]string{"douyin"})
	t.Cleanup(func() { service.SetBridgeChannels(nil) })
	service.SetGlobalInboxIngressService(ingress)
	t.Cleanup(func() { service.SetGlobalInboxIngressService(nil) })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	// 与 router.go 装配同源：bridge 组只挂 InitGuard + BridgeIngressGuard（无 JWT）。
	bridgeWS := r.Group("/api")
	bridgeWS.Use(middleware.BridgeIngressGuard())
	bridgeWS.POST("/bridge/ingest", bridge.NewBridgeIngestHandler(ingress).HandleHTTPIngest)
	return r, tr
}

func cs02AuthPost(r *gin.Engine, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/bridge/ingest", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Bridge-Token", token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// cs02MaliciousBody 恶意 wire Extra 帧：顶层 account_id 是服务端权威归属，
// 消息级 extra 伪造 account_id/sender_type 试图改写归属。
func cs02MaliciousBody(eventID, conv, cust, serverAccount, evilAccount string) string {
	return `{"v":1,"channel":"douyin","account_id":"` + serverAccount + `","conversation_id":"` + conv + `","messages":[
		{"event_id":"` + eventID + `","sender_id":"` + cust + `","sender_type":"customer","msg_type":"text",
		 "content":"CS02鉴权贯通样本：这个能用吗",
		 "extra":{"account_id":"` + evilAccount + `","sender_type":"self","injected":"x"}}
	],"expect_reply":false}`
}

func cs02HubCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := db.GetDB().Model(&model.MessageHub{}).Count(&n).Error; err != nil {
		t.Fatalf("数 message_hub 行失败: %v", err)
	}
	return n
}

// TestCS02_BridgeRouterAuth_MissingTokenRejectedWithoutSideEffects 缺 token → 401，无副作用。
func TestCS02_BridgeRouterAuth_MissingTokenRejectedWithoutSideEffects(t *testing.T) {
	t.Setenv("BRIDGE_INGEST_TOKEN", "cs02-token-e2e")
	r, tr := cs02AuthSetup(t)

	w := cs02AuthPost(r, cs02MaliciousBody("cs02-evt-auth-1", "cs02-conv-auth-1", "cs02-openid-auth", "cs02-acc-a1", "evil-account"), "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("缺 token 应 401，实际 %d body=%s", w.Code, w.Body.String())
	}
	if n := cs02HubCount(t); n != 0 {
		t.Errorf("鉴权失败不得落任何行，实际 %d", n)
	}
	if tr.count() != 0 {
		t.Errorf("鉴权失败不得触发 AI，实际 %d", tr.count())
	}
}

// TestCS02_BridgeRouterAuth_WrongTokenRejectedWithoutSideEffects 错 token → 401，无副作用。
func TestCS02_BridgeRouterAuth_WrongTokenRejectedWithoutSideEffects(t *testing.T) {
	t.Setenv("BRIDGE_INGEST_TOKEN", "cs02-token-e2e")
	r, tr := cs02AuthSetup(t)

	w := cs02AuthPost(r, cs02MaliciousBody("cs02-evt-auth-2", "cs02-conv-auth-2", "cs02-openid-auth", "cs02-acc-a1", "evil-account"), "totally-wrong-token")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("错 token 应 401，实际 %d body=%s", w.Code, w.Body.String())
	}
	if n := cs02HubCount(t); n != 0 {
		t.Errorf("鉴权失败不得落任何行，实际 %d", n)
	}
	if tr.count() != 0 {
		t.Errorf("鉴权失败不得触发 AI，实际 %d", tr.count())
	}
}

// TestCS02_BridgeRouterAuth_FailClosedWhenTokenNotConfigured 未配置 token 且未显式
// BRIDGE_INGEST_AUTH=off → 503 fail-closed（不允许静默退回无鉴权模式）。
func TestCS02_BridgeRouterAuth_FailClosedWhenTokenNotConfigured(t *testing.T) {
	t.Setenv("BRIDGE_INGEST_TOKEN", "")
	r, tr := cs02AuthSetup(t)

	w := cs02AuthPost(r, cs02MaliciousBody("cs02-evt-auth-3", "cs02-conv-auth-3", "cs02-openid-auth", "cs02-acc-a1", "evil-account"), "any-token")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("未配置 token 应 503 fail-closed，实际 %d body=%s", w.Code, w.Body.String())
	}
	if n := cs02HubCount(t); n != 0 {
		t.Errorf("fail-closed 不得落任何行，实际 %d", n)
	}
	if tr.count() != 0 {
		t.Errorf("fail-closed 不得触发 AI，实际 %d", tr.count())
	}
}

// TestCS02_BridgeRouterAuth_ValidToken_MaliciousExtraAccountIDDropped 合法 token +
// 恶意 wire Extra.account_id：CH-37b 刀1 固定键防覆盖的**端到端**断言——
// 伪造 account_id/sender_type 被丢弃并计数，落库行与 AI 触发元组的归属全部用服务端值。
func TestCS02_BridgeRouterAuth_ValidToken_MaliciousExtraAccountIDDropped(t *testing.T) {
	t.Setenv("BRIDGE_INGEST_TOKEN", "cs02-token-e2e")
	r, tr := cs02AuthSetup(t)

	const (
		serverAccount = "cs02-acc-a1"
		evilAccount   = "cs02-evil-account"
		evt           = "cs02-evt-auth-4"
		conv          = "cs02-conv-auth-4"
		cust          = "cs02-openid-auth"
	)
	droppedBefore := channelgw.IngestExtraDroppedKeys.Load()

	w := cs02AuthPost(r, cs02MaliciousBody(evt, conv, cust, serverAccount, evilAccount), "cs02-token-e2e")
	if w.Code != http.StatusOK {
		t.Fatalf("合法 token 应 200，实际 %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		OK       bool `json:"ok"`
		Ingested []struct {
			EventID  string `json:"event_id"`
			Accepted bool   `json:"accepted"`
		} `json:"ingested"`
		Reason string `json:"reason,omitempty"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败: %v body=%s", err, w.Body.String())
	}
	if !resp.OK {
		t.Fatalf("ingest ok=false: %s", w.Body.String())
	}
	if len(resp.Ingested) != 1 || !resp.Ingested[0].Accepted {
		t.Fatalf("恶意 extra 帧本身应被受理（伪造键丢弃≠拒帧）: %+v", resp)
	}

	// 落库行：归属 = 服务端顶层 account_id；extra 里的 account_id 也是服务端值，
	// 线路伪造值不得出现在任何键位。
	var row model.MessageHub
	if err := db.GetDB().Where("msg_id = ?", evt).First(&row).Error; err != nil {
		t.Fatalf("消息未落库: %v", err)
	}
	if row.AccountID != serverAccount {
		t.Errorf("行 AccountID = %q，期望服务端值 %q（恶意 Extra.account_id 不得改写归属）", row.AccountID, serverAccount)
	}
	if row.Direction != "inbound" {
		t.Errorf("行方向 = %q，期望 inbound（线路伪造 sender_type=self 也不得翻转方向）", row.Direction)
	}
	if v, _ := row.Extra["account_id"].(string); v != serverAccount {
		t.Errorf("extra.account_id = %q，期望服务端值 %q", v, serverAccount)
	}
	if v, _ := row.Extra["sender_type"].(string); v == "self" {
		t.Errorf("extra.sender_type = self（线路伪造值未被丢弃）")
	}
	if extraJSON, err := json.Marshal(row.Extra); err == nil && bytes.Contains(extraJSON, []byte(evilAccount)) {
		t.Errorf("伪造账号值 %q 泄漏进行 extra: %s", evilAccount, extraJSON)
	}

	// 固定键伪造被丢弃并计数（account_id + sender_type 至少 2 键）。
	if delta := channelgw.IngestExtraDroppedKeys.Load() - droppedBefore; delta < 2 {
		t.Errorf("IngestExtraDroppedKeys 增量 = %d，期望 ≥2（account_id/sender_type 伪造键丢弃计数）", delta)
	}

	// AI 触发元组：归属判定用服务端值。
	if tr.count() != 1 {
		t.Fatalf("AI 触发应恰 1 次，实际 %d", tr.count())
	}
	if snap := tr.snapshot(); snap["account"] != serverAccount {
		t.Errorf("触发元组 account = %q，期望服务端值 %q", snap["account"], serverAccount)
	}
}
