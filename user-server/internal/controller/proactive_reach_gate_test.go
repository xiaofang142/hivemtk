package controller

// T-P3-07 AC① 的第一条非工具外发路径：直接 API。
//
// 断在 HTTP 入口这一侧，而不是只在 service 里断：`/reach/proactive/*` 有 5 个调用点
// （单发、快速发送、按客户、批量、validate），它们各自组装 `ProactiveReachRequest`，
// 其中 batch 还会把一个请求扇成 N 次外发。闸门装在 `ReachByCustomer` 内部正是为了
// 让"新加一个入口"不再等于"新漏一条路径"—— 这几条用例就是那句设计理由的证据。

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type reachGateStub struct {
	asks    []service.ReachSubject
	allowed bool
	reason  string
}

func (g *reachGateStub) CheckReachPreSend(_ context.Context, s service.ReachSubject) (bool, string) {
	g.asks = append(g.asks, s)
	return g.allowed, g.reason
}

func (g *reachGateStub) keys() []string {
	out := make([]string, 0, len(g.asks))
	for _, a := range g.asks {
		out = append(out, a.Key)
	}
	return out
}

// newGateAPIDB 每个用例建一次库。
//
// 只在用例开头调一次：NewTestDB 会 DROP 再重建传入的表（同进程共用一个测试库），
// 对照组若再建一次就把实验组刚种的数据抹了，测出来的是"环境把自己冲了"而不是行为。
func newGateAPIDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.Customer{}, &model.CustomerChannel{}, &model.CustomerDoNotContact{})
}

// newGateWiredReachAPI 在给定库上造一个"手机号出口指向计数器"的触达 API，闸门由调用方给定。
//
// 库必须是真的：手机/邮箱直发分支在出口前要先读全局退订标志位，传 nil 库会退到进程全局
// 句柄（测试进程里为 nil ⇒ panic）。
func newGateWiredReachAPI(t *testing.T, db *gorm.DB, gate service.ReachPreSendChecker) (*[]string, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	sent := &[]string{}
	svc := service.NewProactiveReachService(db, nil)
	svc.SetSMSRegistry(func() (func(ctx context.Context, phone, content, templateID string, params map[string]string) (string, error), error) {
		return func(_ context.Context, phone, _, _ string, _ map[string]string) (string, error) {
			*sent = append(*sent, phone)
			return "sms_api_spy", nil
		}, nil
	})
	if gate != nil {
		svc.SetPreSendApprovalChecker(gate)
	}
	ctrl := NewProactiveReachController(svc)
	router := gin.New()
	router.POST("/api/reach/proactive/send", ctrl.ProactiveSend)
	router.POST("/api/reach/proactive/quick", ctrl.QuickSend)
	router.POST("/api/reach/proactive/batch", ctrl.BatchProactiveSend)
	return sent, router
}

func postReachJSON(t *testing.T, router *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestProactiveReachAPI_SingleSendDeniedYieldsZeroOutbound(t *testing.T) {
	// 固定号段即可：手机直发分支不写冷却键（冷却按 one_id 记），跨运行不会互相污染。
	const phone = "12900007777"
	db := newGateAPIDB(t)

	gate := &reachGateStub{allowed: false, reason: "denied_default"}
	sent, router := newGateWiredReachAPI(t, db, gate)

	w := postReachJSON(t, router, "/api/reach/proactive/send", `{"phone":"`+phone+`","content":"好久不见"}`)
	if len(*sent) != 0 {
		t.Fatalf("闸门拒绝时不得外发: %v", *sent)
	}
	if w.Code == http.StatusOK {
		t.Fatalf("闸门拒绝时不得返回成功: %s", w.Body.String())
	}
	// 状态码刻意不断言：本控制器把 DNC/冷却/闸门三类拒绝统一映射成 500（既有行为），
	// 把它改成分层语义会同时改动两条与本次无关的路径 —— 归"待拍板"，不在本卡顺手改。
	if !strings.Contains(w.Body.String(), "denied_default") {
		t.Errorf("响应里要能读到拒绝理由，否则调用方只知道失败不知道为什么: %s", w.Body.String())
	}
	if len(gate.asks) != 1 {
		t.Fatalf("一次外发问一次闸门，实际 %d 次", len(gate.asks))
	}

	// 对照组：同一套装配、闸门放行 ⇒ 必须真的发出去（否则上面的"零外发"是空断言）。
	okGate := &reachGateStub{allowed: true, reason: "whitelisted"}
	sent2, router2 := newGateWiredReachAPI(t, db, okGate)
	w2 := postReachJSON(t, router2, "/api/reach/proactive/send", `{"phone":"`+phone+`","content":"好久不见"}`)
	if w2.Code != http.StatusOK || len(*sent2) != 1 {
		t.Fatalf("放行时应正常外发: code=%d sent=%v body=%s", w2.Code, *sent2, w2.Body.String())
	}
}

// 快速发送入口带着用户可填的 account_id：判定键必须是 user_id 带来的 one_id，
// 且不能是那个 account_id（AC② 的同一件事，在另一个入口上再钉一遍）。
func TestProactiveReachAPI_QuickSendKeysOnOneIDNotAccountID(t *testing.T) {
	db := newGateAPIDB(t)
	gate := &reachGateStub{allowed: false, reason: "denied_default"}
	sent, router := newGateWiredReachAPI(t, db, gate)

	w := postReachJSON(t, router, "/api/reach/proactive/quick",
		`{"channel":"sms","phone":"12900008888","account_id":"acc-777","user_id":"uid-api-quick","content":"hi"}`)
	if len(*sent) != 0 {
		t.Fatalf("闸门拒绝时不得外发: %v", *sent)
	}
	if w.Code == http.StatusOK {
		t.Fatalf("闸门拒绝时不得返回成功: %s", w.Body.String())
	}
	if len(gate.asks) != 1 {
		t.Fatalf("应问一次闸门，实际 %d 次", len(gate.asks))
	}
	if got := gate.asks[0].Key; got != "uid-api-quick" {
		t.Errorf("判定键应为 one_id，实际 %q（account_id=%q 不得参与判定）", got, gate.asks[0].AccountID)
	}
}

// 批量入口是这三条路径里最需要一个好闸门的一条：一个请求扇成 N 次外发。
// 断的是"每个目标各问一次、一次都没发出"，不是"整个请求失败"。
func TestProactiveReachAPI_BatchDeniedYieldsZeroOutboundForEveryTarget(t *testing.T) {
	db := newGateAPIDB(t)
	gate := &reachGateStub{allowed: false, reason: "denied_default"}
	sent, router := newGateWiredReachAPI(t, db, gate)

	body := `{"targets":[` +
		`{"phone":"12900000001","content":"a"},` +
		`{"phone":"12900000002","content":"b"},` +
		`{"phone":"12900000003","content":"c"}]}`
	w := postReachJSON(t, router, "/api/reach/proactive/batch", body)
	if len(*sent) != 0 {
		t.Fatalf("闸门拒绝时批量不得有任何外发: %v", *sent)
	}
	if len(gate.asks) != 3 {
		t.Fatalf("批量应逐目标各问一次闸门，实际 %d 次（%v）", len(gate.asks), gate.keys())
	}
	// 批量入口整体返回 200，逐目标结果在 data 里（既有语义），所以这里断的是"三个目标全失败"，
	// 而不是"整个请求失败"—— 后者在零外发时同样成立，却证明不了每条都被判过。
	var out struct {
		Data struct {
			Total        int `json:"total"`
			SuccessCount int `json:"success_count"`
			FailCount    int `json:"fail_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应解析失败: %v body=%s", err, w.Body.String())
	}
	if out.Data.Total != 3 || out.Data.SuccessCount != 0 || out.Data.FailCount != 3 {
		t.Errorf("批量应 3 个目标全部失败: %+v", out)
	}

	// 对照组：放行时三条都应发出，否则上面的 fail_count=3 可能只是接口压根不通。
	okGate := &reachGateStub{allowed: true, reason: "whitelisted"}
	sent2, router2 := newGateWiredReachAPI(t, db, okGate)
	w2 := postReachJSON(t, router2, "/api/reach/proactive/batch", body)
	if len(*sent2) != 3 {
		t.Fatalf("放行时三条都应外发: %v body=%s", *sent2, w2.Body.String())
	}
}
