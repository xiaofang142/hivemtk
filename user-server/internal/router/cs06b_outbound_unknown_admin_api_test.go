// cs06b_outbound_unknown_admin_api_test.go CH-37b 刀5：出站 unknown 对账管理面的
// HTTP 装配格（清单/计数/迁移三端点 + admin 门 + operator 落痕）。
//
// 刀4 交付了 repo+service 两级对账能力，但「管理侧 HTTP API 装配+鉴权」是登记的
// 剩余项：没有 HTTP 面时 unknown 的唯一出路只够单测腿调用，运维/管理端到不了。
// 本文件在真实引擎（Setup 全装配）+真 PG 上钉住：
//
//	① 路由真实注册且 admin 门真实工作——匿名 401 / 坐席 403 / 超管放行到 handler；
//	② 清单=待办读数：只回 unknown 行（delivered/failed 不混入），total 与行集一致，
//	  行 id 即对账定位键；
//	③ 迁移端到端：POST reconcile 带 unknown 行 id → delivered，行真翻转、push_error
//	  带 'reconciled_unknown:<jwt username>' 操作来源（对账必须留痕的规格由 API 层
//	  operator 从 JWT 取实现）；
//	④ ids 指到非 unknown 行=幂等 0 迁移（重复对账天然收敛，规格原文）。
//
// 预置走同库直写（本套件 ModeOff：直通，与刀4 套件的 off 模式腿同款）；
// 被测面走真实 HTTP + 真实 JWT/能力守卫链。
package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

const unk5Platform = "b37b_unk5"

func TestOutboundUnknownAdminAPI_GateAndFlow(t *testing.T) {
	// F-4-6：tenant 模式是进程级全局态，cleanup 必须恢复**进入前**的模式。
	// 原实现硬编码恢复 ModeOn，在默认 off 的测试进程里会把后续用例（按文件名序
	// 排在 cs06b 之后的 data_scope_gate 等）推进多租户模式，其无租户 ctx 的
	// 直写被隔离 callback 拒绝而红。
	prevMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(prevMode) })
	engine, database := gateEngineAndDB(t)
	_, admin := gateTokens(t)
	staff := gateRoleToken(t, "customer_service")

	// 预置四行：unknown 2 行 + delivered/failed 对照各 1 行（清单只该回 unknown）。
	// gateEngineAndDB 的固定模型表没有 message_hub，这里按 inbox_agents 同款补迁移。
	if err := database.AutoMigrate(&model.MessageHub{}); err != nil {
		t.Fatalf("迁移 message_hub 失败: %v", err)
	}
	rows := []*model.MessageHub{
		{Platform: unk5Platform, AccountID: "acc5", ConversationID: "c1", MsgID: "unk5-a", MsgType: "text", Direction: "outbound", Status: model.BridgeAckStatusUnknown, PushError: "ack_unknown"},
		{Platform: unk5Platform, AccountID: "acc5", ConversationID: "c1", MsgID: "unk5-b", MsgType: "text", Direction: "outbound", Status: model.BridgeAckStatusUnknown, PushError: "ack_unknown"},
		{Platform: unk5Platform, AccountID: "acc5", ConversationID: "c1", MsgID: "unk5-ok", MsgType: "text", Direction: "outbound", Status: model.BridgeAckStatusDelivered},
		{Platform: unk5Platform, AccountID: "acc5", ConversationID: "c1", MsgID: "unk5-bad", MsgType: "text", Direction: "outbound", Status: model.BridgeAckStatusFailed},
	}
	for _, r := range rows {
		if err := database.Create(r).Error; err != nil {
			t.Fatalf("seed %s 失败: %v", r.MsgID, err)
		}
	}
	unknownIDs := []uint{rows[0].ID, rows[1].ID}

	// —— ① 门：匿名 401 ——
	anon := httptest.NewRequest(http.MethodGet, "/api/inbox/outbound-unknown", nil)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, anon)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("匿名读清单应 401，实得 %d / %s", w.Code, w.Body.String())
	}
	// 坐席被 admin 门拦（响应体断言沿 route_policy_gate 惯例：看文案不看裸状态码，
	// 但这里状态码 403 本身就是门语义，两个都看）。
	code, body := callUnk5(t, engine, http.MethodGet, "/api/inbox/outbound-unknown", staff, "")
	if code != http.StatusForbidden {
		t.Fatalf("坐席读全局治理口应被 admin 门拦成 403，实得 %d / %s", code, body)
	}

	// —— ② 清单：admin 放行，只回 unknown 行，total=2 ——
	code, body = callUnk5(t, engine, http.MethodGet, "/api/inbox/outbound-unknown?channel="+unk5Platform+"&account_id=acc5", admin, "")
	if code != http.StatusOK {
		t.Fatalf("admin 清单应 200，实得 %d / %s", code, body)
	}
	var listRes struct {
		Data struct {
			Total int64 `json:"total"`
			Rows  []struct {
				ID    uint   `json:"id"`
				MsgID string `json:"msg_id"`
			} `json:"rows"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &listRes); err != nil {
		t.Fatalf("清单响应解析失败: %v / %s", err, body)
	}
	if listRes.Data.Total != 2 {
		t.Fatalf("unknown 待办应 2 行（delivered/failed 不混入），实得 %d / %s", listRes.Data.Total, body)
	}
	gotIDs := map[uint]bool{}
	for _, r := range listRes.Data.Rows {
		gotIDs[r.ID] = true
	}
	for _, want := range unknownIDs {
		if !gotIDs[want] {
			t.Fatalf("unknown 行 id=%d 不在清单里: %s", want, body)
		}
	}

	// —— ③ 计数端点 ——
	code, body = callUnk5(t, engine, http.MethodGet, "/api/inbox/outbound-unknown/count?channel="+unk5Platform, admin, "")
	if code != http.StatusOK || !strings.Contains(body, `"total":2`) {
		t.Fatalf("计数端点应 200+total=2，实得 %d / %s", code, body)
	}

	// —— ④ 迁移端到端：unknown→delivered，operator=JWT username 落 push_error ——
	payload, _ := json.Marshal(map[string]any{
		"channel": unk5Platform, "account_id": "acc5",
		"ids": unknownIDs, "target": model.BridgeAckStatusDelivered,
	})
	code, body = callUnk5(t, engine, http.MethodPost, "/api/inbox/outbound-unknown/reconcile", admin, string(payload))
	if code != http.StatusOK {
		t.Fatalf("对账迁移应 200，实得 %d / %s", code, body)
	}
	for _, id := range unknownIDs {
		var got struct {
			Status    string
			PushError string
		}
		if err := database.Model(&model.MessageHub{}).Select("status, COALESCE(push_error,'') AS push_error").
			Where("id = ?", id).Scan(&got).Error; err != nil || got.Status == "" {
			t.Fatalf("回读行 %d 失败: %v", id, err)
		}
		if got.Status != model.BridgeAckStatusDelivered {
			t.Fatalf("行 %d 应翻 delivered，实得 %s", id, got.Status)
		}
		// gateTokens 的 admin 令牌 username=gate-admin。
		if !strings.HasPrefix(got.PushError, "reconciled_unknown:gate-admin") {
			t.Fatalf("push_error 应留操作来源 reconciled_unknown:gate-admin*，实得 %q", got.PushError)
		}
	}

	// —— ⑤ 幂等：同一批 id 重复对账 = 0 迁移不报错 ——
	code, body = callUnk5(t, engine, http.MethodPost, "/api/inbox/outbound-unknown/reconcile", admin, string(payload))
	if code != http.StatusOK || !strings.Contains(body, `"affected":0`) {
		t.Fatalf("重复对账应 200+affected=0，实得 %d / %s", code, body)
	}

	// —— ⑥ 对照行不被触碰：delivered/failed 各自原地 ——
	var ok, bad string
	database.Model(&model.MessageHub{}).Select("status").Where("id = ?", rows[2].ID).Scan(&ok)
	database.Model(&model.MessageHub{}).Select("status").Where("id = ?", rows[3].ID).Scan(&bad)
	if ok != model.BridgeAckStatusDelivered || bad != model.BridgeAckStatusFailed {
		t.Fatalf("对照行应原地不动：delivered=%s failed=%s", ok, bad)
	}
}

// callUnk5 极简请求腿：带 Bearer（可空）+ 可选 JSON body，回 code+body。
func callUnk5(t *testing.T, engine interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}, method, path, token, body string) (int, string) {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}
