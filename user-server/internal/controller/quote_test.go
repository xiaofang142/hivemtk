// quote_test.go T-P6-03：报价发送出口的入参口径、状态码口径与"停在 pending"的可读性。
//
// 服务层用例（service/quote_send_test.go）钉的是"谁够得着 reach、谁写不动状态"，
// 本层钉的是另一件事：**这四种结论在 HTTP 上分不分得开**。
// awaiting / sent / rejected / expired 在库里是同一行 draft 或 sent，
// 而在响应面上必须是四种不同的读法 —— 前端靠它决定"按钮变灰、等审批人"还是"这版发过了"。
//
// 三条看似多余的用例是这张网的承重墙，理由各自写在函数注释上：
// ① 202 那一档（awaiting 不许被压成 200）；
// ② 未装配时读口不许回空对象（503 与"查过了，没有"必须长得不一样）；
// ③ 响应里不许出现恢复凭证（审批表的 ResumeToken 靠 json:"-" 出不了门，这里再锁一层字段名）。
package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/service"
)

// —— 测试替身 ——————————————————————————————————————————

// quoteFakeReader 实现 QuoteViewReader：只记"被叫了哪一格、带了什么入参"。
type quoteFakeReader struct {
	available bool

	view    *service.QuoteView
	viewErr error

	gotRowID    string
	gotQuoteID  string
	gotLatestID string

	genIn     service.QuoteGenerateInput
	genView   *service.QuoteView
	genErr    error
	genCalled int
}

func (r *quoteFakeReader) Available() bool { return r.available }

func (r *quoteFakeReader) View(_ context.Context, id string) (*service.QuoteView, error) {
	r.gotRowID = id
	if r.viewErr != nil {
		return nil, r.viewErr
	}
	return r.view, nil
}

func (r *quoteFakeReader) LatestView(_ context.Context, quoteID string) (*service.QuoteView, error) {
	r.gotLatestID = quoteID
	if r.viewErr != nil {
		return nil, r.viewErr
	}
	return r.view, nil
}

func (r *quoteFakeReader) Generate(_ context.Context, in service.QuoteGenerateInput) (*service.QuoteView, error) {
	r.genCalled++
	r.genIn = in
	if r.genErr != nil {
		return nil, r.genErr
	}
	if r.genView != nil {
		return r.genView, nil
	}
	return quoteTestView(), nil
}

// quoteFakeSender 实现 QuoteSender。三个计数是分诊的根据：
// 本层的判据之一是"闸门在控制器这里就把没登录的挡掉"，
// 那只能靠"服务一次都没被调用"断出来 —— 状态码回 401 也可能是服务自己报的。
type quoteFakeSender struct {
	available bool

	result  *service.QuoteSendResult
	sendErr error
	in      service.QuoteSendInput
	calls   int

	open    *model.ApprovalRequest
	openErr error
	openArg string
}

func (s *quoteFakeSender) Available() bool { return s.available }

func (s *quoteFakeSender) Send(_ context.Context, in service.QuoteSendInput) (*service.QuoteSendResult, error) {
	s.calls++
	s.in = in
	if s.sendErr != nil {
		return nil, s.sendErr
	}
	return s.result, nil
}

func (s *quoteFakeSender) OpenApproval(_ context.Context, quoteRowID string) (*model.ApprovalRequest, error) {
	s.openArg = quoteRowID
	return s.open, s.openErr
}

// quoteTestNow 冻结的"现在"。写死一个时刻而不是 time.Now：本层的断言里有
// sent_at / expires_at 的序列化形状，夹具里的 now 一挪，判过期那条用例的
// 红因就不在 diff 里了（老账，见 opportunity_routes_test.go 的 oppTestClock）。
var quoteTestNow = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func quoteTestView() *service.QuoteView {
	until := quoteTestNow.Add(72 * time.Hour)
	return &service.QuoteView{
		ID:            "q_100_1",
		QuoteID:       "QT-A-B",
		OpportunityID: "opp_1",
		Version:       1,
		Status:        model.QuoteStatusDraft,
		Currency:      "CNY",
		ValidUntil:    &until,
		Lines: []service.QuoteLineView{
			{LineNo: 1, Title: "标准版席位", Quantity: 10, UnitPrice: 120, DiscountPercent: 0, Gross: 1200, Amount: 1200},
		},
		GrossTotal: 1200,
		Total:      1200,
	}
}

func quoteTestEngine(h *QuoteController, uid any) http.Handler {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api")
	auth.Use(func(c *gin.Context) {
		if uid != nil {
			c.Set("user_id", uid)
		}
		c.Next()
	})
	h.RegisterRoutes(auth)
	return engine
}

type quoteEnvelope struct {
	Code    any             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func doQuote(t *testing.T, h http.Handler, method, path, body string) (int, quoteEnvelope, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var env quoteEnvelope
	raw := w.Body.String()
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return w.Code, env, raw
	}
	return w.Code, env, raw
}

func quoteData(t *testing.T, env quoteEnvelope) map[string]any {
	t.Helper()
	if len(env.Data) == 0 {
		t.Fatalf("响应里没有 data 字段：%s", env.Message)
	}
	var out map[string]any
	if err := json.Unmarshal(env.Data, &out); err != nil {
		t.Fatalf("data 不是对象：%v —— %s", err, env.Data)
	}
	return out
}

func quoteReason(t *testing.T, env quoteEnvelope) string {
	t.Helper()
	reason, ok := quoteData(t, env)["reason"].(string)
	if !ok {
		t.Fatalf("错误响应缺机器可读的 data.reason：%s", env.Message)
	}
	return reason
}

// quoteOK 断"这是一个成功信封"：code 必须是数字 0，而不是 HTTP 2xx 那一带的任意值。
// 单独一条判据的理由：202 这一档是本仓第一次出现"非 200 的成功响应"，
// 而"状态码 202 + body code 202"与"状态码 202 + body code 0"在网关日志里一模一样，
// 只有后者是前端认的那一份。
func quoteOK(t *testing.T, env quoteEnvelope, status int, raw string) {
	t.Helper()
	if code, _ := env.Code.(float64); code != 0 {
		t.Errorf("成功响应的 code=%v，期望 0（前端按 body 里的 code 判成败，不是按 HTTP 状态码）：%s", env.Code, raw)
	}
}

// —— ① awaiting：202 那一档 ————————————————————————

// TestQuoteController_AwaitingAnswersTwoOhTwo AC② 在 HTTP 侧的形状。
//
// 为什么 200 不行：awaiting 说的是"这件事还没做完，稍后凭那个号再来问一次"。
// 回 200 时前端把这一格当成终态渲染（按钮消失、提示"已发送"），
// 而库里那一版仍是 draft 且客户什么都没收到 —— 于是销售以为报价出去了，
// 审批人那边的待办还开着。409 也不行：那是"你的请求被拒了"，
// 而这次请求既没错也没被拒，只是**还没轮到它**。
func TestQuoteController_AwaitingAnswersTwoOhTwo(t *testing.T) {
	reader := &quoteFakeReader{available: true, view: quoteTestView()}
	sender := &quoteFakeSender{available: true, result: &service.QuoteSendResult{
		Disposition: service.QuoteSendAwaiting, ApprovalID: "apr_77", Status: model.QuoteStatusDraft,
	}}
	engine := quoteTestEngine(NewQuoteController(reader, sender), uint(7))

	code, env, raw := doQuote(t, engine, http.MethodPost, "/api/quote/q_100_1/send", `{}`)
	if code != http.StatusAccepted {
		t.Fatalf("未批的发送回 %d，期望 202：%s —— %s", code, env.Message, raw)
	}
	quoteOK(t, env, code, raw)

	data := quoteData(t, env)
	if data["disposition"] != service.QuoteSendAwaiting {
		t.Errorf("disposition=%v，期望 %s", data["disposition"], service.QuoteSendAwaiting)
	}
	if data["approval_id"] != "apr_77" {
		t.Errorf("没把审批号带回去：%v —— 调用方丢了它就是只能重开一条待办", data["approval_id"])
	}
	if data["status"] != model.QuoteStatusDraft {
		t.Errorf("status=%v，期望 %s（这一版还停在草稿位是本次结论的一部分）", data["status"], model.QuoteStatusDraft)
	}
	if v, ok := data["event_recorded"]; ok && v == true {
		t.Errorf("未放行却记了已发生的事件：%v", data)
	}
	if sender.in.Operator != "7" {
		t.Errorf("Operator=%q，期望取自会话身份 \"7\"（sales_events.owner_id 靠它回答是谁点的发送）", sender.in.Operator)
	}
	if sender.in.QuoteRowID != "q_100_1" {
		t.Errorf("发送对象串位了：%q", sender.in.QuoteRowID)
	}
}

// TestQuoteController_DispositionsArePairwiseDistinct 四种处置各占一格，两两可分。
//
// 判据是"二元组 (状态码, reason) 互不相同"，而不是"每条各自等于期望值"：
// 前者会在有人把两档合并（比如把 expired 也回 409 approval_rejected）时立刻红，
// 后者只看自己那一格，合并的那一方仍然绿。
func TestQuoteController_DispositionsArePairwiseDistinct(t *testing.T) {
	cases := []struct {
		disposition string
		wantStatus  int
		wantReason  string
	}{
		{service.QuoteSendSent, http.StatusOK, ""},
		{service.QuoteSendAwaiting, http.StatusAccepted, ""},
		{service.QuoteSendRejected, http.StatusConflict, "approval_rejected"},
		{service.QuoteSendExpired, http.StatusConflict, "approval_expired"},
	}
	seen := map[string]string{}
	for _, tc := range cases {
		sender := &quoteFakeSender{available: true, result: &service.QuoteSendResult{
			Disposition: tc.disposition, ApprovalID: "apr_1", Status: model.QuoteStatusDraft,
		}}
		engine := quoteTestEngine(NewQuoteController(&quoteFakeReader{available: true, view: quoteTestView()}, sender), uint(7))

		code, env, raw := doQuote(t, engine, http.MethodPost, "/api/quote/q_100_1/send", `{}`)
		if code != tc.wantStatus {
			t.Errorf("%s 回 %d，期望 %d：%s", tc.disposition, code, tc.wantStatus, env.Message)
			continue
		}
		if tc.wantReason == "" {
			quoteOK(t, env, code, raw)
			if got := quoteData(t, env)["disposition"]; got != tc.disposition {
				t.Errorf("%s 的 data.disposition=%v", tc.disposition, got)
			}
			seen[fmt.Sprint(code)] = tc.disposition
			continue
		}
		if got := quoteReason(t, env); got != tc.wantReason {
			t.Errorf("%s 的 reason=%q，期望 %q", tc.disposition, got, tc.wantReason)
		}
		seen[fmt.Sprint(code)+":"+tc.wantReason] = tc.disposition
	}
	if len(seen) != len(cases) {
		t.Errorf("四种处置在 HTTP 面上塌成 %d 格（必须 4 格才可分诊）：%v", len(seen), seen)
	}
}

// —— ② 操作者：关闸在服务之前 ————————————————————————

// TestQuoteController_SendWithoutOperatorNeverReachesTheService 没身份就不该开审批。
//
// 反面是"把空 Operator 递给服务"：服务确实也会拒（ErrQuoteSendInputInvalid），
// 但那要走到"闸门已读、审批已入队"才拒得掉吗？不会 —— 服务的判据顺序是先 normalize。
// 所以这一格在**行为**上摘掉控制器这道关看不出差别，差别在**状态码**：
// 服务回的那一句会被本层翻成 400 input_invalid，而事实是"你没登录"，
// 该回 401 让前端去刷会话而不是改载荷。判据因此打两件事：401 + 服务一次没被调。
func TestQuoteController_SendWithoutOperatorNeverReachesTheService(t *testing.T) {
	for _, uid := range []any{nil, uint(0), -3, "  "} {
		sender := &quoteFakeSender{available: true, result: &service.QuoteSendResult{Disposition: service.QuoteSendSent}}
		engine := quoteTestEngine(NewQuoteController(&quoteFakeReader{available: true}, sender), uid)

		code, env, _ := doQuote(t, engine, http.MethodPost, "/api/quote/q_100_1/send", `{}`)
		if code != http.StatusUnauthorized {
			t.Errorf("user_id=%#v 时回 %d，期望 401：%s", uid, code, env.Message)
		}
		if got := quoteReason(t, env); got != "unauthenticated" {
			t.Errorf("user_id=%#v 的 reason=%q，期望 unauthenticated", uid, got)
		}
		if sender.calls != 0 {
			t.Errorf("user_id=%#v 时服务被调了 %d 次：待办已经开出去了，而这件事根本没人能做", uid, sender.calls)
		}
	}
}

// —— ③ 错误分诊：哨兵 → (状态码, reason) ——————————————

func TestQuoteController_SendErrorsAreTriagedBySentinel(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantReason string
	}{
		{"闸门关", fmt.Errorf("%w: 拦下原因 阶段未开", service.ErrQuoteGateClosed), http.StatusConflict, "gate_closed"},
		{"行不存在", fmt.Errorf("%w: 行键 q_x", service.ErrQuoteVersionMissing), http.StatusNotFound, "not_found"},
		{"非草稿", fmt.Errorf("%w: q_x 当前是 sent", service.ErrQuoteSendNotDraft), http.StatusConflict, "not_draft"},
		{"批准是别人的", service.ErrQuoteSendApprovalMismatch, http.StatusConflict, "approval_mismatch"},
		{"审批号查无", fmt.Errorf("%w: apr_9", service.ErrApprovalNotFound), http.StatusNotFound, "approval_not_found"},
		{"没有收件人", service.ErrQuoteSendRecipientMissing, http.StatusConflict, "recipient_missing"},
		{"没有行项目", service.ErrQuoteSendLinesMissing, http.StatusConflict, "lines_missing"},
		{"话术失效", fmt.Errorf("%w: 生效指针没落着", service.ErrQuoteScriptUnavailable), http.StatusConflict, "script_unavailable"},
		{"外发失败", fmt.Errorf("%w: 渠道 5xx", service.ErrQuoteSendOutboundFailed), http.StatusBadGateway, "outbound_failed"},
		{"状态写坏", service.ErrQuoteSendStatusStuck, http.StatusConflict, "status_stuck"},
		{"结论未知", service.ErrQuoteSendVerdictUnknown, http.StatusInternalServerError, "verdict_unknown"},
		{"入参不合法", service.ErrQuoteSendInputInvalid, http.StatusBadRequest, "input_invalid"},
		{"未装配", service.ErrQuoteServiceUnavailable, http.StatusServiceUnavailable, "unavailable"},
		{"底层故障", errors.New("connection reset by peer"), http.StatusInternalServerError, "internal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sender := &quoteFakeSender{available: true, sendErr: tc.err}
			engine := quoteTestEngine(NewQuoteController(&quoteFakeReader{available: true}, sender), uint(7))

			code, env, _ := doQuote(t, engine, http.MethodPost, "/api/quote/q_100_1/send", `{}`)
			if code != tc.wantStatus {
				t.Fatalf("回 %d，期望 %d：%s", code, tc.wantStatus, env.Message)
			}
			if got := quoteReason(t, env); got != tc.wantReason {
				t.Errorf("reason=%q，期望 %q", got, tc.wantReason)
			}
		})
	}
}

// TestQuoteController_StuckPlusOutboundReportsBoth 崩溃窗口里那一条必须报满两件事。
//
// 服务层的判据是"错误里同时包着 OutboundFailed 与 StatusStuck"（外发失败且状态没退回去）。
// 本层的判据是**不许把这种复合错翻成 502**：502 的说法是"渠道坏了，重试即可"，
// 而重试会被自己那一行 sent 挡掉 —— 照着 502 的语义去重试的人，
// 会在库里已 sent、客户没收到的那一格上得出"已经发过了"。所以复合错一律升到 500，
// 且 reason 用 status_stuck（贵的那一半优先：它要人去核对数据）。
func TestQuoteController_StuckPlusOutboundReportsBoth(t *testing.T) {
	combined := fmt.Errorf("外发失败 ⇒ 且状态没退回草稿：%w ⇒ %w",
		service.ErrQuoteSendOutboundFailed, service.ErrQuoteSendStatusStuck)

	sender := &quoteFakeSender{available: true, sendErr: combined}
	engine := quoteTestEngine(NewQuoteController(&quoteFakeReader{available: true}, sender), uint(7))

	code, env, _ := doQuote(t, engine, http.MethodPost, "/api/quote/q_100_1/send", `{}`)
	if code == http.StatusBadGateway {
		t.Fatalf("复合错回 502：那等于告诉运维\"渠道坏了重试即可\"，而这一格重试会被自己的 sent 挡掉")
	}
	if code != http.StatusInternalServerError {
		t.Fatalf("回 %d，期望 500：%s", code, env.Message)
	}
	if got := quoteReason(t, env); got != "status_stuck" {
		t.Errorf("reason=%q，期望 status_stuck（两半里更贵的那一半）", got)
	}
	if strings.Contains(env.Message, "重试即可") {
		t.Errorf("复合错的文案里出现了\"重试即可\"：%q", env.Message)
	}
}

// TestQuoteController_InternalErrorTextIsNotLeaked 500 只说"失败了"，不说怎么失败的。
//
// 与商机那一路同口径：仓储报错带着 SQL 片段与列名，运维看的那一份在日志里，
// 两个通道各有各的受众。这条用例的注码是把底层错换成一句带列名的串。
func TestQuoteController_InternalErrorTextIsNotLeaked(t *testing.T) {
	sender := &quoteFakeSender{available: true,
		sendErr: errors.New(`ERROR: duplicate key value violates unique constraint "uq_quotes_quote_version" DETAIL: Key (quote_id, version)=(QT-A-B, 2) already exists.`)}
	engine := quoteTestEngine(NewQuoteController(&quoteFakeReader{available: true}, sender), uint(7))

	code, env, raw := doQuote(t, engine, http.MethodPost, "/api/quote/q_100_1/send", `{}`)
	if code != http.StatusInternalServerError {
		t.Fatalf("回 %d，期望 500：%s", code, env.Message)
	}
	for _, needle := range []string{"uq_quotes_quote_version", "duplicate key", "DETAIL"} {
		if strings.Contains(raw, needle) {
			t.Errorf("500 的响应体里透出底层错误串（含 %q）：%s", needle, raw)
		}
	}
}

// —— ④ 读端点：视图 + 开放审批 ————————————————————————

// TestQuoteController_ReadCarriesTheOpenApprovalWithoutTheToken AC① 的 HTTP 可读面。
//
// "停在 pending"要在 HTTP 上读得出来，否则调用方只能重发一次 Send 去撞那条待办。
// 反面有两层：① 响应里没带审批号（丢了号只能重开一条）；
// ② 带了审批却把整行 model.ApprovalRequest 顺手 marshal 出去 ——
// 那一行里有 ResumeToken。今天它靠 json:"-" 出不了门，本层再锁一层字段名：
// 只要本层是"自己挑字段"而不是"整个结构体交出去"，运营给审批表加一列凭证
// 也不会从这个口子漏出去。假件里那个凭证**刻意填了值**，
// 少了这一句正向对照，"响应里没有 resume_token"可能只是因为压根没有审批。
func TestQuoteController_ReadCarriesTheOpenApprovalWithoutTheToken(t *testing.T) {
	expires := quoteTestNow.Add(24 * time.Hour)
	open := &model.ApprovalRequest{
		ID: "apr_77", SubjectType: "quote", SubjectID: "q_100_1", PolicyKey: "quote.send",
		Status: model.ApprovalStatusPending, ResumeToken: "rt_deadbeefcafe", ExpiresAt: &expires,
	}
	reader := &quoteFakeReader{available: true, view: quoteTestView()}
	sender := &quoteFakeSender{available: true, open: open}
	engine := quoteTestEngine(NewQuoteController(reader, sender), uint(7))

	for _, probe := range []struct{ method, path string }{
		{http.MethodGet, "/api/quote/q_100_1"},
		{http.MethodGet, "/api/quote/latest/QT-A-B"},
	} {
		code, env, raw := doQuote(t, engine, probe.method, probe.path, "")
		if code != http.StatusOK {
			t.Fatalf("%s 回 %d：%s —— %s", probe.path, code, env.Message, raw)
		}
		quoteOK(t, env, code, raw)
		if open.ResumeToken == "" {
			t.Fatal("夹具把凭证填成空串了，下面的泄漏判据就成了空判")
		}
		for _, needle := range []string{"resume_token", "ResumeToken", "rt_deadbeefcafe"} {
			if strings.Contains(raw, needle) {
				t.Errorf("%s 的响应里出现了 %q：恢复凭证不是业务字段", probe.path, needle)
			}
		}
		for _, key := range []string{"id", "quote_id", "opportunity_id", "version", "status",
			"currency", "lines", "total", "gross_total"} {
			if _, ok := quoteData(t, env)[key]; !ok {
				t.Errorf("%s 的视图里没有 %q：%v", probe.path, key, quoteData(t, env))
			}
		}
		data := quoteData(t, env)
		if data["approval_lookup"] != "found" {
			t.Errorf("approval_lookup=%v，期望 found（这一格要说清\"问过了，有\"）", data["approval_lookup"])
		}
		oa, ok := data["open_approval"].(map[string]any)
		if !ok {
			t.Fatalf("open_approval 不是对象：%T", data["open_approval"])
		}
		if oa["approval_id"] != "apr_77" || oa["status"] != model.ApprovalStatusPending {
			t.Errorf("开放审批没带对：%v", oa)
		}
		if _, ok := oa["expires_at"].(string); !ok {
			t.Errorf("open_approval 里没有 expires_at：%v —— 没有它就答不了\"还要等多久\"", oa)
		}
	}
	if sender.openArg != "q_100_1" {
		t.Errorf("按链读最新那一版时，开放审批该问的是**查出来的那一行**，得到 %q", sender.openArg)
	}
}

// TestQuoteController_ReadDistinguishesNoneFromNotAsked "没有开放审批"与"没问"要分开。
//
// 三档各来一次：审批服务在场且查无（none）、在场且读失败（read_failed）、
// 整个发送腿没装配（unavailable）。反面是全回 null：
// 拿 null 的前端会渲染"这一版没人审"，而事实可能是"底座起不来"或"这一次读失败了"，
// 三种情形的下一步动作完全不同（等审批人 / 稍后重试 / 去查装配）。
func TestQuoteController_ReadDistinguishesNoneFromNotAsked(t *testing.T) {
	cases := []struct {
		name       string
		sender     *quoteFakeSender
		wantLookup string
	}{
		{"查过了，没有", &quoteFakeSender{available: true}, "none"},
		{"问出了故障", &quoteFakeSender{available: true, openErr: errors.New("connection reset by peer")}, "read_failed"},
		{"没问（未装配）", &quoteFakeSender{available: false}, "unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := quoteTestEngine(NewQuoteController(&quoteFakeReader{available: true, view: quoteTestView()}, tc.sender), uint(7))
			code, env, raw := doQuote(t, engine, http.MethodGet, "/api/quote/q_100_1", "")
			if code != http.StatusOK {
				t.Fatalf("%s：回 %d —— %s", tc.name, code, raw)
			}
			data := quoteData(t, env)
			if got := data["approval_lookup"]; got != tc.wantLookup {
				t.Errorf("approval_lookup=%v，期望 %s", got, tc.wantLookup)
			}
			if tc.wantLookup == "none" || tc.wantLookup == "unavailable" {
				if v, ok := data["open_approval"]; ok && v != nil {
					t.Errorf("没有开放审批时 open_approval 被填成了 %v", v)
				}
			}
			// 视图那一半不能因为审批读失败就一起消失：报价单在库里，客户等着看。
			if data["id"] != "q_100_1" {
				t.Errorf("视图丢了：%v", data)
			}
		})
	}
}

// TestQuoteController_UnassembledAnswersFiveOhThree 未装配 ⇒ 四条端点全部 503，
// 且谁都不许回空对象（同商机那一路的判据：`{}` 与 `[]` 都是一句业务结论）。
func TestQuoteController_UnassembledAnswersFiveOhThree(t *testing.T) {
	for _, pair := range []struct {
		name       string
		reader     *quoteFakeReader
		sender     *quoteFakeSender
		method     string
		path, body string
	}{
		{"读一版/读腿缺", &quoteFakeReader{}, &quoteFakeSender{available: true}, http.MethodGet, "/api/quote/q_1", ""},
		{"读最新/读腿缺", &quoteFakeReader{}, &quoteFakeSender{available: true}, http.MethodGet, "/api/quote/latest/QT-1", ""},
		{"生成/读腿缺", &quoteFakeReader{}, &quoteFakeSender{available: true}, http.MethodPost, "/api/quote", `{"opportunity_id":"opp_1","template_code":"std"}`},
		{"发送/发腿缺", &quoteFakeReader{available: true}, &quoteFakeSender{}, http.MethodPost, "/api/quote/q_1/send", `{}`},
	} {
		t.Run(pair.name, func(t *testing.T) {
			engine := quoteTestEngine(NewQuoteController(pair.reader, pair.sender), uint(7))
			code, env, _ := doQuote(t, engine, pair.method, pair.path, pair.body)
			if code != http.StatusServiceUnavailable {
				t.Fatalf("回 %d，期望 503：%s", code, env.Message)
			}
			if got := quoteReason(t, env); got != "unavailable" {
				t.Errorf("reason=%q，期望 unavailable", got)
			}
			if !strings.Contains(env.Message, "未装配") && !strings.Contains(env.Message, "底座") {
				t.Errorf("503 的文案没说是装配缺失：%q", env.Message)
			}
		})
	}
}

// —— ⑤ 生成：入参面就是 AC① 的那道门 ————————————————

// TestQuoteController_GenerateRefusesScriptAndRecipientFields 正文与收件人不是入参。
//
// 服务层的判据是"入参结构体里没有话术字段"。HTTP 侧默认的行为是**丢弃未知字段**，
// 于是 `{"content":"..."}` 会静默成功而那份正文被扔掉 —— 调用方以为自己传了话术，
// 库里那一条看起来与走正路的一模一样。所以这里必须拒收未知字段，
// 而且拒得要说清是哪一格（回 400 + input_invalid，文案里点名未知字段）。
func TestQuoteController_GenerateRefusesScriptAndRecipientFields(t *testing.T) {
	for _, body := range []string{
		`{"opportunity_id":"opp_1","template_code":"std","content":"您好，这是您的报价"}`,
		`{"opportunity_id":"opp_1","template_code":"std","one_id":"one_zhang"}`,
		`{"opportunity_id":"opp_1","template_code":"std","total":9999}`,
		`{"opportunity_id":"opp_1","template_code":"std","win_probability":0.99}`,
	} {
		reader := &quoteFakeReader{available: true}
		engine := quoteTestEngine(NewQuoteController(reader, &quoteFakeSender{available: true}), uint(7))

		code, env, _ := doQuote(t, engine, http.MethodPost, "/api/quote", body)
		if code != http.StatusBadRequest {
			t.Errorf("%s 回 %d，期望 400：%s", body, code, env.Message)
			continue
		}
		if got := quoteReason(t, env); got != "input_invalid" {
			t.Errorf("reason=%q，期望 input_invalid", got)
		}
		if reader.genCalled != 0 {
			t.Errorf("越界入参走到了服务层（第 %d 次）：那就变成\"静默丢掉那一格\"", reader.genCalled)
		}
	}
}

func TestQuoteController_GeneratePassesTheDeclaredInputThrough(t *testing.T) {
	reader := &quoteFakeReader{available: true}
	engine := quoteTestEngine(NewQuoteController(reader, &quoteFakeSender{available: true}), uint(7))

	body := `{"opportunity_id":" opp_9 ","template_code":"std","currency":"usd",` +
		`"valid_until":"2026-10-01T00:00:00Z","lines":[{"product_id":"p1","quantity":3,"unit_price":120.5,"discount_percent":10}]}`
	code, env, raw := doQuote(t, engine, http.MethodPost, "/api/quote", body)
	if code != http.StatusOK {
		t.Fatalf("回 %d：%s —— %s", code, env.Message, raw)
	}
	quoteOK(t, env, code, raw)
	if reader.genIn.OpportunityID != " opp_9 " {
		t.Errorf("OpportunityID=%q，期望**原样递过去**（本层不改写载荷：归一只有服务层一处，\n\t这里 trim 一遍、服务层再 trim 一遍，两份口径日后会分家，而分家时看不出谁改了）",
			reader.genIn.OpportunityID)
	}
	if reader.genIn.TemplateCode != "std" || reader.genIn.Currency != "usd" {
		t.Errorf("模板/币种没递到：%+v", reader.genIn)
	}
	if reader.genIn.ValidUntil == nil || !reader.genIn.ValidUntil.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("valid_until=%v，期望 RFC3339 解出的那个时刻", reader.genIn.ValidUntil)
	}
	if len(reader.genIn.Lines) != 1 {
		t.Fatalf("行项目没递到：%+v", reader.genIn.Lines)
	}
	line := reader.genIn.Lines[0]
	if line.ProductID != "p1" {
		t.Errorf("product_id=%q", line.ProductID)
	}
	// 全指针的判据在 HTTP 侧的兑现：**显式 0** 必须递成 0，而不是"没给"。
	// 摘掉指针改成非零才覆盖，这一格会静默变回"沿用模板那份折扣"。
	if line.DiscountPercent == nil || *line.DiscountPercent != 10 {
		t.Errorf("discount_percent=%v，期望 10", line.DiscountPercent)
	}
	if line.Quantity == nil || *line.Quantity != 3 {
		t.Errorf("quantity=%v，期望 3", line.Quantity)
	}
	if data := quoteData(t, env); data["id"] != "q_100_1" {
		t.Errorf("生成结果没带回版本行键：%v —— 没有它就发不出去", data)
	}
}

// TestQuoteController_GenerateZeroDiscountSurvivesRoundTrip 上一格的正面：
// 显式 0 折扣（客户还价、折扣回到原价）不能被读成"没给"。
func TestQuoteController_GenerateZeroDiscountSurvivesRoundTrip(t *testing.T) {
	reader := &quoteFakeReader{available: true}
	engine := quoteTestEngine(NewQuoteController(reader, &quoteFakeSender{available: true}), uint(7))

	code, env, raw := doQuote(t, engine, http.MethodPost, "/api/quote",
		`{"opportunity_id":"opp_1","template_code":"std","lines":[{"product_id":"p1","quantity":1,"unit_price":100,"discount_percent":0}]}`)
	if code != http.StatusOK {
		t.Fatalf("回 %d：%s —— %s", code, env.Message, raw)
	}
	got := reader.genIn.Lines[0].DiscountPercent
	if got == nil {
		t.Fatal("discount_percent 被读成\"没给\"：那一份折扣会悄悄留在报价上")
	}
	if *got != 0 {
		t.Errorf("discount_percent=%v，期望 0", *got)
	}
}

func TestQuoteController_GenerateErrorsAreTriaged(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantReason string
	}{
		{"闸门关", service.ErrQuoteGateClosed, http.StatusConflict, "gate_closed"},
		{"商机不存在", service.ErrQuoteOpportunityMissing, http.StatusNotFound, "not_found"},
		{"模板没配", service.ErrQuoteTemplateMissing, http.StatusConflict, "template_missing"},
		{"模板读不出", service.ErrQuoteTemplateInvalid, http.StatusConflict, "template_invalid"},
		{"话术无生效版本", service.ErrQuoteScriptUnavailable, http.StatusConflict, "script_unavailable"},
		{"入参越界", service.ErrQuoteInputInvalid, http.StatusBadRequest, "input_invalid"},
		{"未装配", service.ErrQuoteServiceUnavailable, http.StatusServiceUnavailable, "unavailable"},
		{"底层故障", errors.New("connection reset by peer"), http.StatusInternalServerError, "internal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &quoteFakeReader{available: true, genErr: tc.err}
			engine := quoteTestEngine(NewQuoteController(reader, &quoteFakeSender{available: true}), uint(7))

			code, env, _ := doQuote(t, engine, http.MethodPost, "/api/quote",
				`{"opportunity_id":"opp_1","template_code":"std"}`)
			if code != tc.wantStatus {
				t.Fatalf("回 %d，期望 %d：%s", code, tc.wantStatus, env.Message)
			}
			if got := quoteReason(t, env); got != tc.wantReason {
				t.Errorf("reason=%q，期望 %q", got, tc.wantReason)
			}
		})
	}
}

// —— ⑥ 入参关闸：id 形状 ——————————————————————————————

func TestQuoteController_BlankAndOversizedIDsAreRefusedBeforeTheService(t *testing.T) {
	reader := &quoteFakeReader{available: true, viewErr: errors.New("never reached")}
	sender := &quoteFakeSender{available: true, sendErr: errors.New("never reached")}
	engine := quoteTestEngine(NewQuoteController(reader, sender), uint(7))

	for _, probe := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/quote/%20", ""},
		{http.MethodGet, "/api/quote/latest/%20", ""},
		{http.MethodPost, "/api/quote/%20/send", `{}`},
		{http.MethodGet, "/api/quote/opp_" + strings.Repeat("x", 200), ""},
	} {
		code, env, _ := doQuote(t, engine, probe.method, probe.path, probe.body)
		if code != http.StatusBadRequest {
			t.Errorf("%s %s 回 %d，期望 400：%s", probe.method, probe.path, code, env.Message)
			continue
		}
		if got := quoteReason(t, env); got != "input_invalid" {
			t.Errorf("%s %s 的 reason=%q，期望 input_invalid（空白 id 不能说成查无此行）", probe.method, probe.path, got)
		}
		if strings.Contains(env.Message, strings.Repeat("x", 200)) {
			t.Errorf("%s 的超长 id 被整段回显：%q", probe.path, env.Message)
		}
	}
	if reader.gotRowID != "" || reader.gotLatestID != "" || sender.calls != 0 {
		t.Errorf("越界 id 走到了服务层：row=%q latest=%q send=%d", reader.gotRowID, reader.gotLatestID, sender.calls)
	}

	// 正向对照：同样的句柄收到合规 id 就必须真去查，否则上面那些 400
	// 可能只是因为路由压根没接上服务。
	if code, _, _ := doQuote(t, engine, http.MethodGet, "/api/quote/q_ok", ""); code != http.StatusInternalServerError {
		t.Errorf("合规 id 回 %d，期望 500（对照组不成立，上面的 400 判不出关闸）", code)
	}
}

// TestQuoteController_SendBodyIsOptionalButStrict 不带体 = 只开一条待办；
// 带了体就必须是 approval_id 那一格。
//
// 空体（Content-Length 0）与 `{}` 都得放行：AC② 的第一次调用就是"手里没有结论"，
// 让 curl 必须写一个 `{}` 才能点发送，前端就会改成"顺手塞个 {\"approval_id\":\"\"}"，
// 而那是同一个动作的两种写法 —— 两种写法迟早有一种绕过判空。
// 反过来，`{"note":"催一下"}` 这种未知字段必须 400（与生成侧同一条锁）。
func TestQuoteController_SendBodyIsOptionalButStrict(t *testing.T) {
	for _, probe := range []struct{ raw, wantApproval string }{
		{"", ""},
		{"{}", ""},
		{`{"approval_id":""}`, ""},
		{`{"approval_id":"apr_77"}`, "apr_77"},
	} {
		sender := &quoteFakeSender{available: true, result: &service.QuoteSendResult{
			Disposition: service.QuoteSendAwaiting, ApprovalID: "apr_1", Status: model.QuoteStatusDraft}}
		engine := quoteTestEngine(NewQuoteController(&quoteFakeReader{available: true}, sender), uint(7))

		code, env, raw := doQuote(t, engine, http.MethodPost, "/api/quote/q_100_1/send", probe.raw)
		if code != http.StatusAccepted {
			t.Errorf("体=%q 回 %d，期望 202：%s —— %s", probe.raw, code, env.Message, raw)
			continue
		}
		if sender.in.ApprovalID != probe.wantApproval {
			t.Errorf("体=%q 时 approval_id 递成了 %q，期望 %q", probe.raw, sender.in.ApprovalID, probe.wantApproval)
		}
	}

	sender := &quoteFakeSender{available: true}
	engine := quoteTestEngine(NewQuoteController(&quoteFakeReader{available: true}, sender), uint(7))
	for _, body := range []string{
		`{"note":"催一下"}`,
		// 路径与体各说一个对象：批准是**按对象**给的，两次读其中任一份就能给别的报价开门。
		`{"quote_row_id":"q_other"}`,
		// "谁点的发送"由调用方自报，等于审计列当场作废。
		`{"operator":"mallory"}`,
		`not json`,
	} {
		code, env, _ := doQuote(t, engine, http.MethodPost, "/api/quote/q_100_1/send", body)
		if code != http.StatusBadRequest {
			t.Errorf("体=%q 回 %d，期望 400：%s", body, code, env.Message)
		}
	}
	if sender.calls != 0 {
		t.Errorf("越界请求体走到了服务层 %d 次", sender.calls)
	}
}

// TestQuoteController_LargeSendBodyIsRefused 请求体封顶：与商机侧同一条判据。
// 这里给一个**形状合法**的胖体（approval_id 塞 1MB），
// 否则 400 只是因为 JSON 解析器先报错，体积那一刀有没有落下读不出来。
func TestQuoteController_LargeSendBodyIsRefused(t *testing.T) {
	fat := `{"approval_id":"apr_` + strings.Repeat("x", 1<<20) + `"}`
	sender := &quoteFakeSender{available: true, result: &service.QuoteSendResult{Disposition: service.QuoteSendSent}}
	engine := quoteTestEngine(NewQuoteController(&quoteFakeReader{available: true}, sender), uint(7))

	code, env, _ := doQuote(t, engine, http.MethodPost, "/api/quote/q_100_1/send", fat)
	if code != http.StatusBadRequest {
		t.Fatalf("回 %d，期望 400：%s", code, env.Message)
	}
	if !strings.Contains(env.Message, "字节") {
		t.Errorf("体积超限被报成了形状错误：%q —— 运维会去查调用方的字段而不是查这一格的封顶", env.Message)
	}
	if sender.calls != 0 {
		t.Errorf("胖体走到了服务层 %d 次", sender.calls)
	}

	// 对照组：同一形状的瘦体必须放行，否则上面的 400 与"路由没接上"分不开。
	if code, _, raw := doQuote(t, engine, http.MethodPost, "/api/quote/q_100_1/send",
		`{"approval_id":"apr_77"}`); code != http.StatusOK {
		t.Fatalf("瘦体回 %d：%s", code, raw)
	}
}

// —— ⑦ 类型层：控制器只够得着那几格 ————————————————

// TestQuoteController_InterfacesAreTheNarrowSurfaces 两道接缝的方法集合钉成白名单。
//
// 反面不是"多一个方法"，而是"多一个方法之后有人用它"：
// 读接缝里出现 UpdateStatus 或 Send，控制器就能在一次 GET 里把状态推进一格；
// 发接缝里出现 Decide/Submit，"点发送的人自己批"就成了可能。
// 服务层那条同族判据（quoteSendStore 只有三个方法）钉的是腿里的依赖，
// 这一条钉的是**出口能看见的依赖**，两者不是同一格。
func TestQuoteController_InterfacesAreTheNarrowSurfaces(t *testing.T) {
	readerMethods := ifaceMethods(reflect.TypeOf((*QuoteViewReader)(nil)).Elem())
	senderMethods := ifaceMethods(reflect.TypeOf((*QuoteSender)(nil)).Elem())

	if want := []string{"Available", "Generate", "LatestView", "View"}; !equalStrings(readerMethods, want) {
		t.Errorf("读接缝方法集=%v，期望 %v", readerMethods, want)
	}
	if want := []string{"Available", "OpenApproval", "Send"}; !equalStrings(senderMethods, want) {
		t.Errorf("发接缝方法集=%v，期望 %v", senderMethods, want)
	}
	for _, m := range append(append([]string{}, readerMethods...), senderMethods...) {
		switch m {
		case "Decide", "ExpireOverdue", "Submit", "UpdateStatus", "Append", "Create":
			t.Errorf("出口接缝里出现了 %q：批的人与发的人必须不是同一个动作的主体", m)
		}
	}
}

// ifaceMethods 按字典序列出一个接口类型的方法名（判据是集合，不是声明顺序）。
func ifaceMethods(t reflect.Type) []string {
	out := make([]string, 0, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		out = append(out, t.Method(i).Name)
	}
	sort.Strings(out)
	return out
}

func equalStrings(got, want []string) bool {
	return reflect.DeepEqual(got, want)
}
