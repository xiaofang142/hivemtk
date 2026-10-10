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
	gotLatestID string

	genIn     service.QuoteGenerateInput
	genView   *service.QuoteView
	genErr    error
	genCalled int

	reviseID  string
	reviseIn  service.QuoteGenerateInput
	reviseErr error

	viewAtID  string
	viewAtVer int64
	viewAtErr error

	// emptyResult 让写视图的那两格（Generate/Revise）返回 (nil, nil)：
	// 这是"服务层实现漂了"的注码——本层的判据是不许把这种漂移渲染成一次成功。
	// 只作用于写腿：读腿的空结果有它自己的档位（404 not_found，见 MissingVersionReadsNotFound）。
	emptyResult bool
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
	if r.emptyResult {
		return nil, nil
	}
	if r.genView != nil {
		return r.genView, nil
	}
	return quoteTestView(), nil
}

// Revise 记的是"链号 + 那份入参"两格：判据要能看出模板与收件人有没有被递进服务层。
// 默认回一份 version=2 的视图（与 quoteTestView 同一行的下一版），
// 因为上一版视图的 version 是 1 —— 返回同一个 version 会让"版本号推进了"这件事读不出来。
func (r *quoteFakeReader) Revise(_ context.Context, quoteID string, in service.QuoteGenerateInput) (*service.QuoteView, error) {
	r.reviseID = quoteID
	r.reviseIn = in
	if r.viewErr != nil {
		return nil, r.viewErr
	}
	if r.emptyResult {
		return nil, nil
	}
	next := quoteTestView()
	next.ID = "q_100_2"
	next.Version = 2
	return next, nil
}

func (r *quoteFakeReader) ViewAt(_ context.Context, quoteID string, version int64) (*service.QuoteView, error) {
	r.viewAtID = quoteID
	r.viewAtVer = version
	if r.viewErr != nil {
		return nil, r.viewErr
	}
	return r.view, nil
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

// TestQuoteController_UnassembledAnswersFiveOhThree 未装配 ⇒ 六条端点全部 503，
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
		{"读指定版本/读腿缺", &quoteFakeReader{}, &quoteFakeSender{available: true}, http.MethodGet, "/api/quote/version/QT-1/2", ""},
		{"生成/读腿缺", &quoteFakeReader{}, &quoteFakeSender{available: true}, http.MethodPost, "/api/quote", `{"opportunity_id":"opp_1","template_code":"std"}`},
		{"追加一版/读腿缺", &quoteFakeReader{}, &quoteFakeSender{available: true}, http.MethodPost, "/api/quote/revise/QT-1", `{}`},
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

// —— ⑥ 还价（追加一版）与按版本号回读 ——————————————————

// TestQuoteController_ReviseRefusesTemplateAndRecipientFields 还价的入参面比生成窄。
//
// 这一条不是"少一个字段"的洁癖，而是两种坏法各挡一种：
//  1. 接受 `template_code` 时，服务层压根不会重跑模板（还价继承基准版的行项目）：这一格当场
//     变成"改了没人读"——调用方以为换了模板，库里那一版还是老行项目。这类缺陷在本仓最贵：
//     页面显示已保存而底层没动，看起来一切正常。
//  2. 接受 `one_id` 时，收件人成了请求体的一部分，而发送腿的收件人是商机带出来的：在还价这一格
//     先破一个口子，AC① 的"收件人由报价自己说"就只剩注释。
//
// 未知字段必须 400 且服务层一次都不被调用（静默丢弃等于把上面第 1 条变成默认行为）。
func TestQuoteController_ReviseRefusesTemplateAndRecipientFields(t *testing.T) {
	for _, body := range []string{
		`{"template_code":"premium"}`,
		`{"one_id":"one_zhang"}`,
		`{"content":"您好，这是还价后的正文"}`,
		`{"status":"accepted"}`,
		`{"version":9}`,
		`{"total":0.01}`,
	} {
		reader := &quoteFakeReader{available: true}
		engine := quoteTestEngine(NewQuoteController(reader, &quoteFakeSender{available: true}), uint(7))

		code, env, _ := doQuote(t, engine, http.MethodPost, "/api/quote/revise/QT-A", body)
		if code != http.StatusBadRequest {
			t.Errorf("%s 回 %d，期望 400：%s", body, code, env.Message)
			continue
		}
		if got := quoteReason(t, env); got != "input_invalid" {
			t.Errorf("reason=%q，期望 input_invalid", got)
		}
		if reader.reviseID != "" {
			t.Errorf("越界入参走到了服务层（链号 %q）：那就变成\"静默丢掉那一格\"", reader.reviseID)
		}
	}
}

func TestQuoteController_RevisePassesTheDeclaredInputThrough(t *testing.T) {
	reader := &quoteFakeReader{available: true}
	engine := quoteTestEngine(NewQuoteController(reader, &quoteFakeSender{available: true}), uint(7))

	body := `{"opportunity_id":"opp_9","currency":"usd","valid_until":"2026-11-01T00:00:00Z",` +
		`"lines":[{"product_id":"p1","quantity":2,"unit_price":88.5,"discount_percent":0}]}`
	code, env, raw := doQuote(t, engine, http.MethodPost, "/api/quote/revise/QT-A-B", body)
	if code != http.StatusOK {
		t.Fatalf("回 %d：%s —— %s", code, env.Message, raw)
	}
	quoteOK(t, env, code, raw)
	if reader.reviseID != "QT-A-B" {
		t.Errorf("链号递成了 %q，期望 QT-A-B（地址上是逻辑号，行键归服务层算）", reader.reviseID)
	}
	if reader.reviseIn.TemplateCode != "" {
		t.Errorf("TemplateCode=%q，期望留空：还价不重跑模板", reader.reviseIn.TemplateCode)
	}
	if reader.reviseIn.OneID != "" {
		t.Errorf("OneID=%q，期望留空：收件人不是入参", reader.reviseIn.OneID)
	}
	if reader.reviseIn.OpportunityID != "opp_9" || reader.reviseIn.Currency != "usd" {
		t.Errorf("归属/币种没递到：%+v", reader.reviseIn)
	}
	if reader.reviseIn.ValidUntil == nil || !reader.reviseIn.ValidUntil.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("valid_until=%v，期望 RFC3339 解出的那个时刻", reader.reviseIn.ValidUntil)
	}
	if len(reader.reviseIn.Lines) != 1 {
		t.Fatalf("行项目没递到：%+v", reader.reviseIn.Lines)
	}
	// 与生成侧同一条指针判据：还价到原价（显式 0 折扣）不能被读成"没给"，
	// 否则那一份折扣会悄悄留在下一版上，而报价单看起来已经"回到原价"了。
	line := reader.reviseIn.Lines[0]
	if line.DiscountPercent == nil || *line.DiscountPercent != 0 {
		t.Errorf("discount_percent=%v，期望显式的 0", line.DiscountPercent)
	}
	if line.Quantity == nil || *line.Quantity != 2 {
		t.Errorf("quantity=%v，期望 2", line.Quantity)
	}

	data := quoteData(t, env)
	if data["id"] != "q_100_2" {
		t.Errorf("data.id=%v，期望新版的行键：没有它就发不出去", data["id"])
	}
	if data["version"] != float64(2) {
		t.Errorf("data.version=%v，期望 2 —— 版本号由仓储递增，出口回显的就是它", data["version"])
	}
}

// TestQuoteController_ReviseEmptyBodyIsAPureAppend 空体与 `{}` 都放行：
// "客户没还，我们只是把这一版重新发一次"这件事在链上就是要占一个版本号。
// 让 curl 必须写出一个字段才能点"追加一版"，前端就会造一个假的空 lines 出来，
// 而那与"继承基准版"在库里是两种结果。
func TestQuoteController_ReviseEmptyBodyIsAPureAppend(t *testing.T) {
	for _, raw := range []string{"", "{}"} {
		reader := &quoteFakeReader{available: true}
		engine := quoteTestEngine(NewQuoteController(reader, &quoteFakeSender{available: true}), uint(7))

		code, env, body := doQuote(t, engine, http.MethodPost, "/api/quote/revise/QT-A", raw)
		if code != http.StatusOK {
			t.Errorf("体=%q 回 %d，期望 200：%s —— %s", raw, code, env.Message, body)
			continue
		}
		if len(reader.reviseIn.Lines) != 0 || reader.reviseIn.OpportunityID != "" || reader.reviseIn.Currency != "" {
			t.Errorf("体=%q 时入参被填出了东西：%+v", raw, reader.reviseIn)
		}
		if reader.reviseIn.ValidUntil != nil {
			t.Errorf("体=%q 时 valid_until 被凭空造出来：%v", raw, reader.reviseIn.ValidUntil)
		}
	}

	// 反向：非 JSON 的体与未知字段一样必须拒（"空体合法"不等于"任意体合法"）。
	reader := &quoteFakeReader{available: true}
	engine := quoteTestEngine(NewQuoteController(reader, &quoteFakeSender{available: true}), uint(7))
	for _, raw := range []string{"not json", `{"approval_id":"apr_1"}`} {
		code, env, _ := doQuote(t, engine, http.MethodPost, "/api/quote/revise/QT-A", raw)
		if code != http.StatusBadRequest {
			t.Errorf("体=%q 回 %d，期望 400：%s", raw, code, env.Message)
		}
	}
	if reader.reviseID != "" {
		t.Errorf("越界体走到了服务层（链号 %q）", reader.reviseID)
	}
}

func TestQuoteController_ReviseErrorsAreTriaged(t *testing.T) {
	// 分诊开关（replyError）由六条端点共用，所以这里从追加那一条打也照样量到全部映射；
	// `lines_missing` 由发送腿产出、经同一个 switch，钉的是"它落 409 而不是 500"这一格。
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantReason string
	}{
		{"闸门关", service.ErrQuoteGateClosed, http.StatusConflict, "gate_closed"},
		{"链上无版本（含基准版无可继承行项目）", service.ErrQuoteVersionMissing, http.StatusNotFound, "not_found"},
		{"商机不存在", service.ErrQuoteOpportunityMissing, http.StatusNotFound, "not_found"},
		{"版本号被占", service.ErrQuoteVersionConflict, http.StatusConflict, "version_conflict"},
		{"行项目缺失", service.ErrQuoteSendLinesMissing, http.StatusConflict, "lines_missing"},
		{"话术无生效版本", service.ErrQuoteScriptUnavailable, http.StatusConflict, "script_unavailable"},
		{"入参越界", service.ErrQuoteInputInvalid, http.StatusBadRequest, "input_invalid"},
		{"未装配", service.ErrQuoteServiceUnavailable, http.StatusServiceUnavailable, "unavailable"},
		{"底层故障", errors.New("duplicate key value violates unique constraint"), http.StatusInternalServerError, "internal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &quoteFakeReader{available: true, viewErr: tc.err}
			engine := quoteTestEngine(NewQuoteController(reader, &quoteFakeSender{available: true}), uint(7))

			code, env, raw := doQuote(t, engine, http.MethodPost, "/api/quote/revise/QT-A", `{}`)
			if code != tc.wantStatus {
				t.Fatalf("回 %d，期望 %d：%s —— %s", code, tc.wantStatus, env.Message, raw)
			}
			if got := quoteReason(t, env); got != tc.wantReason {
				t.Errorf("reason=%q，期望 %q", got, tc.wantReason)
			}
			// 409 这一档里 version_conflict 必须与 gate_closed / lines_missing 分得开：
			// 三者的修法分别是"重读最新版再追加""等运营开闸""先补行项目"。
			// 状态码相同而 reason 相同，前端就只能弹窗"冲突"了事。
		})
	}

	// 底层故障不许把 SQL 片段透出给调用方（与商机侧同一条判据）。
	reader := &quoteFakeReader{available: true, viewErr: errors.New("duplicate key value violates unique constraint \"idx_quote_version\"")}
	engine := quoteTestEngine(NewQuoteController(reader, &quoteFakeSender{available: true}), uint(7))
	_, env, _ := doQuote(t, engine, http.MethodPost, "/api/quote/revise/QT-A", `{}`)
	if strings.Contains(env.Message, "idx_quote_version") || strings.Contains(env.Message, "duplicate key") {
		t.Errorf("500 的文案透出了底层错误串：%q", env.Message)
	}
}

func TestQuoteController_VersionReadsTheExactVersion(t *testing.T) {
	reader := &quoteFakeReader{available: true, view: quoteTestView()}
	engine := quoteTestEngine(NewQuoteController(reader, &quoteFakeSender{available: true}), uint(7))

	code, env, raw := doQuote(t, engine, http.MethodGet, "/api/quote/version/QT-A-B/3", "")
	if code != http.StatusOK {
		t.Fatalf("回 %d：%s —— %s", code, env.Message, raw)
	}
	quoteOK(t, env, code, raw)
	if reader.viewAtID != "QT-A-B" {
		t.Errorf("链号=%q，期望 QT-A-B", reader.viewAtID)
	}
	if reader.viewAtVer != 3 {
		t.Errorf("版本号=%d，期望 3（原样递过去，不能被读成\"取最新\"）", reader.viewAtVer)
	}
	if data := quoteData(t, env); data["id"] != "q_100_1" {
		t.Errorf("回的不是那一版的行键：%v", data)
	}
	// 按版本读也要能看见开放审批（与读行键那一条同一个理由：审批状态是"这一版现在能不能发"的一部分）。
	if data := quoteData(t, env); data["approval_lookup"] != "none" {
		t.Errorf("approval_lookup=%v，期望 none：%v", data["approval_lookup"], data)
	}
}

// TestQuoteController_VersionNumberMustBeAPositiveInteger 0 / 负数 / 非数字一律 400 且不落到仓储。
//
// 放 0 走到底层会回一个 404（那一版不存在），而 404 与"这么写根本没有对应事实"是两件事：
// 前者会让人去查数据，后者才是该修的地方。更实际的风险是日后有人把 0 实现成"取最新"，
// 那就凭空多出一条不在任何清单上、也没人审过的别名入口。
func TestQuoteController_VersionNumberMustBeAPositiveInteger(t *testing.T) {
	reader := &quoteFakeReader{available: true, viewErr: errors.New("never reached")}
	engine := quoteTestEngine(NewQuoteController(reader, &quoteFakeSender{available: true}), uint(7))

	// 负数走 `<= 0` 那一格，`1e3` 与 20 位数字走 ParseInt 报错那一格（后者是溢出，
	// 不是形状不对 —— 两者都归 400，但红因要能分得开，所以两条都留着）。
	// 不带 `%20` 这类空白：编号与版本号都先 trim 再判（与读行键那一条同一个口径），
	// `" 2"` 是合法的 2，不是非法形状。
	for _, probe := range []string{"/0", "/-1", "/abc", "/1.5", "/1e3", "/" + strings.Repeat("9", 20)} {
		code, env, _ := doQuote(t, engine, http.MethodGet, "/api/quote/version/QT-A"+probe, "")
		if code != http.StatusBadRequest {
			t.Errorf("%s 回 %d，期望 400：%s", probe, code, env.Message)
			continue
		}
		if got := quoteReason(t, env); got != "input_invalid" {
			t.Errorf("%s 的 reason=%q，期望 input_invalid", probe, got)
		}
	}
	if reader.viewAtID != "" {
		t.Errorf("非法版本号走到了服务层：链号 %q", reader.viewAtID)
	}

	// 对照组：合法版本号必须真去查（否则上面那些 400 可能只是路由没接上）。
	if code, _, _ := doQuote(t, engine, http.MethodGet, "/api/quote/version/QT-A/1", ""); code != http.StatusInternalServerError {
		t.Errorf("合法版本号回 %d，期望 500（对照组不成立）", code)
	}
	if reader.viewAtID != "QT-A" || reader.viewAtVer != 1 {
		t.Errorf("对照组没递到服务层：%q / %d", reader.viewAtID, reader.viewAtVer)
	}
}

// TestQuoteController_MissingVersionReadsNotFound 服务层说"那一版不存在"时，HTTP 面必须回 404。
// 回 200 + 空对象会让前端长成"这一版金额为 0"那样的一句业务结论。
func TestQuoteController_MissingVersionReadsNotFound(t *testing.T) {
	engine := quoteTestEngine(NewQuoteController(&quoteFakeReader{available: true}, &quoteFakeSender{available: true}), uint(7))
	code, env, _ := doQuote(t, engine, http.MethodGet, "/api/quote/version/QT-A/9", "")
	if code != http.StatusNotFound {
		t.Fatalf("回 %d，期望 404：%s", code, env.Message)
	}
	if got := quoteReason(t, env); got != "not_found" {
		t.Errorf("reason=%q，期望 not_found", got)
	}
	// data 里只许有分诊码：带出 id/status/lines 中的任何一格，调用方就会拿它当"读到的那一版"。
	data := quoteData(t, env)
	for _, business := range []string{"id", "quote_id", "status", "lines", "total", "version"} {
		if v, ok := data[business]; ok {
			t.Errorf("404 的 data 里出现了业务字段 %s=%v", business, v)
		}
	}
}

// —— ⑦ 服务层给不出结果、也不给错 ——————————————————————

// TestQuoteController_ServiceNilResultWithoutError 服务层"要么给视图/结果，要么给错"这条
// 不变量在 HTTP 侧的兜底：返回 nil 又不给错时按失败报，绝不回一个 data 为 null 的成功信封。
//
// 为什么不放过这一格：200 + 「data:null」在调用方读起来是"成功了，只是没内容"，
// 而在报价这条链路上"没内容"最容易被接着读成"这一版没有行项目"——
// 于是人去补行项目，真正的故障（服务层实现漂了）一次都没出声。
// reason 必须是 internal 而不是 not_found：not_found 说的是"查无此号"，
// 那是客户端能自助修的事实，而这里没有任何客户端能修的东西。
func TestQuoteController_ServiceNilResultWithoutError(t *testing.T) {
	for _, probe := range []struct{ name, method, path, body string }{
		{"生成", http.MethodPost, "/api/quote", `{"opportunity_id":"opp_1","template_code":"std"}`},
		{"追加一版", http.MethodPost, "/api/quote/revise/QT-A-B", `{}`},
	} {
		t.Run(probe.name, func(t *testing.T) {
			reader := &quoteFakeReader{available: true, emptyResult: true}
			engine := quoteTestEngine(NewQuoteController(reader, &quoteFakeSender{available: true}), uint(7))
			code, env, raw := doQuote(t, engine, probe.method, probe.path, probe.body)
			if code >= 200 && code < 300 {
				t.Fatalf("%s：空结果被当成成功回了 %d（body=%s）—— 调用方会把它读成「没内容」而不是「失败了」", probe.name, code, raw)
			}
			if code != http.StatusInternalServerError {
				t.Errorf("回 %d，期望 500：%s", code, env.Message)
			}
			if got := quoteReason(t, env); got != "internal" {
				t.Errorf("reason=%q，期望 internal", got)
			}
		})
	}

	// 发送腿：假件把 result 留空、err 也留空，就是同一个漂移形状。
	sender := &quoteFakeSender{available: true}
	engine := quoteTestEngine(NewQuoteController(&quoteFakeReader{available: true}, sender), uint(7))
	code, env, raw := doQuote(t, engine, http.MethodPost, "/api/quote/q_100_1/send", `{}`)
	if code >= 200 && code < 300 {
		t.Fatalf("发送腿的空结果被当成成功回了 %d（body=%s）", code, raw)
	}
	if code != http.StatusInternalServerError {
		t.Errorf("发送腿回 %d，期望 500：%s", code, env.Message)
	}
	if got := quoteReason(t, env); got != "internal" {
		t.Errorf("发送腿 reason=%q，期望 internal", got)
	}
	if sender.calls != 1 {
		t.Errorf("发送腿被调了 %d 次，期望 1 次（没走到服务就回 500，那测的不是这一格）", sender.calls)
	}

	// 正向对照：同一个假件把空结果关掉就必须回 2xx。
	// 少了这一句，上面那些 500 可能只是因为这几条路由压根没接上服务层。
	control := quoteTestEngine(NewQuoteController(&quoteFakeReader{available: true}, &quoteFakeSender{
		available: true, result: &service.QuoteSendResult{Disposition: service.QuoteSendSent},
	}), uint(7))
	for _, probe := range []struct{ name, method, path, body string }{
		{"生成", http.MethodPost, "/api/quote", `{"opportunity_id":"opp_1","template_code":"std"}`},
		{"追加一版", http.MethodPost, "/api/quote/revise/QT-A-B", `{}`},
		{"发送", http.MethodPost, "/api/quote/q_100_1/send", `{}`},
	} {
		code, env, raw := doQuote(t, control, probe.method, probe.path, probe.body)
		if code < 200 || code >= 300 {
			t.Errorf("正向对照（%s）回 %d，期望 2xx：%s —— %s", probe.name, code, env.Message, raw)
		}
	}
}

// —— ⑧ 入参关闸：id 形状 ——————————————————————————————

func TestQuoteController_BlankAndOversizedIDsAreRefusedBeforeTheService(t *testing.T) {
	reader := &quoteFakeReader{available: true, viewErr: errors.New("never reached")}
	sender := &quoteFakeSender{available: true, sendErr: errors.New("never reached")}
	engine := quoteTestEngine(NewQuoteController(reader, sender), uint(7))

	for _, probe := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/quote/%20", ""},
		{http.MethodGet, "/api/quote/latest/%20", ""},
		{http.MethodGet, "/api/quote/version/%20/1", ""},
		{http.MethodPost, "/api/quote/%20/send", `{}`},
		{http.MethodPost, "/api/quote/revise/%20", `{}`},
		{http.MethodGet, "/api/quote/opp_" + strings.Repeat("x", 200), ""},
		{http.MethodPost, "/api/quote/revise/QT" + strings.Repeat("x", 200), `{}`},
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
	if reader.gotRowID != "" || reader.gotLatestID != "" || reader.reviseID != "" || reader.viewAtID != "" || sender.calls != 0 {
		t.Errorf("越界 id 走到了服务层：row=%q latest=%q revise=%q view_at=%q send=%d",
			reader.gotRowID, reader.gotLatestID, reader.reviseID, reader.viewAtID, sender.calls)
	}

	// 正向对照：同样的句柄收到合规 id 就必须真去查，否则上面那些 400
	// 可能只是因为路由压根没接上服务。
	for _, path := range []string{"/api/quote/q_ok", "/api/quote/latest/QT_OK", "/api/quote/version/QT_OK/1"} {
		if code, _, _ := doQuote(t, engine, http.MethodGet, path, ""); code != http.StatusInternalServerError {
			t.Errorf("%s 回 %d，期望 500（对照组不成立，上面的 400 判不出关闸）", path, code)
		}
	}
	{
		// 追加那一条的正向对照：链号合规就必须真的走到服务层。
		reader.reviseID = ""
		if code, _, _ := doQuote(t, engine, http.MethodPost, "/api/quote/revise/QT_OK", `{}`); code != http.StatusInternalServerError {
			t.Errorf("合规链号的追加回 %d，期望 500（对照组不成立）", code)
		}
		if reader.reviseID != "QT_OK" {
			t.Errorf("服务层收到的链号是 %q，期望 QT_OK", reader.reviseID)
		}
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

// —— ⑨ 类型层：控制器只够得着那几格 ————————————————

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

	if want := []string{"Available", "Generate", "LatestView", "Revise", "View", "ViewAt"}; !equalStrings(readerMethods, want) {
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
