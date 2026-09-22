// bill_test.go T-P7-01：账单出口的入参口径与哨兵分诊。
//
// 服务层那 18 条钉的是"谁够得着写、什么顺序写"，本层钉的是另一件事：
// **那八个哨兵在 HTTP 上分不分得开**。派生失败的处置动作各不相同（改载荷 / 换行号 /
// 去处置原账单 / 重读那一版再来），全压成 400 或全压成 500 时，调用方只能靠猜，
// 而猜错的方向在财务域是"再点一次"——那正是本域最不该重试的动作。
//
// 两条看着多余的用例是这张网的承重墙，理由各自写在函数注释上：
// ① 越界入参（体里带 amount / status）必须在**进服务层之前**被拒；
// ② 未装配与底座故障必须分得开（只测 503 的话，摘掉错误分支也看不出来）。
package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/service"
)

// —— 测试替身 ——————————————————————————————————————————

// billFake 实现 BillDeriver：只记"被叫了几次、带了什么入参"，并按开关回一种结论。
//
// calls 这一格不是装饰：本层有两条判据是"服务层一次都不许被调用"（越界入参、未装配），
// 光看状态码分不出"控制器挡下的"与"服务层挡下之后控制器翻成同一个码的"。
type billFake struct {
	available bool
	calls     int
	in        service.BillDeriveInput
	view      *service.BillView
	err       error
	nilResult bool
}

func (f *billFake) Available() bool { return f.available }

func (f *billFake) DeriveFromQuote(_ context.Context, in service.BillDeriveInput) (*service.BillView, error) {
	f.calls++
	f.in = in
	if f.err != nil {
		return nil, f.err
	}
	if f.nilResult {
		return nil, nil
	}
	if f.view != nil {
		return f.view, nil
	}
	return billTestView(), nil
}

func billTestView() *service.BillView {
	at := time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC)
	return &service.BillView{
		ID:            "b_1762335000000000000_1",
		QuoteID:       "QT-1-2",
		QuoteRowID:    "q_100_1",
		OpportunityID: "opp_1",
		Amount:        369.99,
		Currency:      "CNY",
		Status:        "open",
		CreatedAt:     at,
	}
}

func billTestEngine(h *BillController, uid any) http.Handler {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api")
	// 复现鉴权中间件那一格：它成功放行后往上下文里写的就是 user_id。
	// 传 nil 就是"过了中间件而身份那格是空的"，控制器那一条 401 腿只有在这种形状下才咬得动。
	auth.Use(func(c *gin.Context) {
		if uid != nil {
			c.Set("user_id", uid)
		}
		c.Next()
	})
	h.RegisterRoutes(auth)
	return engine
}

// billSession 有身份的会话。这一族用例默认带着它：
// 401 那条判据单独成一格（TestBillController_RequiresSession），不在每格里重复中间件的行为。
const billSession uint = 7

func doBill(t *testing.T, h http.Handler, method, path, body string) (int, billEnvelopeShape, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	raw := w.Body.String()
	var env billEnvelopeShape
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("响应不是合法 JSON（%d）：%s", w.Code, raw)
	}
	return w.Code, env, raw
}

type billEnvelopeShape struct {
	Code    any             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func billDataFrom(t *testing.T, env billEnvelopeShape) map[string]any {
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

func billReasonFrom(t *testing.T, env billEnvelopeShape) string {
	t.Helper()
	reason, ok := billDataFrom(t, env)["reason"].(string)
	if !ok {
		t.Fatalf("错误响应缺机器可读的 data.reason：%s", env.Message)
	}
	return reason
}

func newBillCtrl(derive BillDeriver) *BillController { return NewBillController(derive) }

// —— ① 正路 ——————————————————————————————————————————————

func TestBillController_DeriveHappyPath(t *testing.T) {
	fake := &billFake{available: true}
	code, env, raw := doBill(t, billTestEngine(newBillCtrl(fake), billSession), http.MethodPost, "/api/bill",
		`{"quote_row_id":"q_100_1"}`)
	if code != http.StatusOK {
		t.Fatalf("派生回 %d：%s —— %s", code, env.Message, raw)
	}
	if fake.calls != 1 {
		t.Fatalf("服务层被调用 %d 次，期望 1", fake.calls)
	}
	if fake.in.QuoteRowID != "q_100_1" {
		t.Errorf("传给服务层的行号是 %q，期望原样透传 q_100_1（控制器不清洗、不补前缀）", fake.in.QuoteRowID)
	}
	data := billDataFrom(t, env)
	for key, want := range map[string]any{
		"id": "b_1762335000000000000_1", "quote_row_id": "q_100_1",
		"opportunity_id": "opp_1", "currency": "CNY", "status": "open",
	} {
		if data[key] != want {
			t.Errorf("data.%s=%v，期望 %v", key, data[key], want)
		}
	}
	// 金额按数字比：JSON 里 369.99 会解成 float64，字符串判据会在格式变了之后假红。
	if amt, ok := data["amount"].(float64); !ok || amt != 369.99 {
		t.Errorf("data.amount=%v，期望 369.99", data["amount"])
	}
	// 账期这一格今天必须是 null 而不是零值日期：本卡没有任何一处定义过付款条件，
	// 而 0001-01-01 会被运营读成"公元 1 年就到期了"。
	if v, present := data["due_at"]; present && v != nil {
		t.Errorf("data.due_at=%v，期望缺席或 null（账期未定不是账期已定）", v)
	}
}

// TestBillController_ReusedIsEchoed 复用那一格必须原样出到响应面上。
//
// "我刚开了一张应收"与"这张应收早就在了"对调用方是两句话：前者该去通知客户付款，
// 后者该去核对为什么有人重复点了确认。压成同一个 200 之后，幂等这条本来是好事的
// 机制会**藏掉重复操作**，而重复确认在财务上意味着两次主张。
func TestBillController_ReusedIsEchoed(t *testing.T) {
	reused := billTestView()
	reused.Reused = true
	fake := &billFake{available: true, view: reused}
	code, env, _ := doBill(t, billTestEngine(newBillCtrl(fake), billSession), http.MethodPost, "/api/bill",
		`{"quote_row_id":"q_100_1"}`)
	if code != http.StatusOK {
		t.Fatalf("回 %d：%s", code, env.Message)
	}
	if got := billDataFrom(t, env)["reused"]; got != true {
		t.Errorf("data.reused=%v，期望 true", got)
	}

	fresh := &billFake{available: true}
	_, env2, _ := doBill(t, billTestEngine(newBillCtrl(fresh), billSession), http.MethodPost, "/api/bill", `{"quote_row_id":"q_100_1"}`)
	if got := billDataFrom(t, env2)["reused"]; got != false {
		t.Errorf("首次派生的 data.reused=%v，期望 false（缺省值会被 JSON 吃成 null，那一格前端读不出来）", got)
	}
}

// —— ② 入参面 ——————————————————————————————————————————————

// TestBillController_RequiresSession 没有身份 ⇒ 401，且服务层一次都不许被调用。
//
// 本卡没有"谁确认的"这一格（bills 的列清单里没有，补它属 T-P8-01 的埋点范围），
// 所以这一道闸门能守的不是数据库、是**日志里那一行**：派生成功时控制器打的审计行
// 带的就是操作者身份。没有身份就没有那行 —— 而这一条腿写的是"该收多少钱"，
// 事后有人问"这张应收是谁开出来的"，至少要有一处答得上来。
// 401 而不是 400：该做的是刷会话，不是改载荷（与发送腿同一判据，理由抄在那边）。
func TestBillController_RequiresSession(t *testing.T) {
	fake := &billFake{available: true}
	code, env, _ := doBill(t, billTestEngine(newBillCtrl(fake), nil), http.MethodPost, "/api/bill",
		`{"quote_row_id":"q_100_1"}`)
	if code != http.StatusUnauthorized {
		t.Errorf("回 %d，期望 401：%s", code, env.Message)
	}
	if got := billReasonFrom(t, env); got != "unauthenticated" {
		t.Errorf("reason=%q，期望 unauthenticated", got)
	}
	if fake.calls != 0 {
		t.Errorf("无身份时服务层仍被调用 %d 次：账单已经开出去了，而没人在场", fake.calls)
	}
}

// TestBillController_BodyCarriesOnlyTheRowKey 体里带 amount / status / currency 一律 400，
// 且服务层一次都不许被调用。
//
// 这条判据在账单域比在报价域更硬：报价那边"未知字段被容下"的坏法是话术与收件人从体里进来，
// 而这边进来的是**应收金额本身**。DisallowUnknownFields 是 AC② 在 HTTP 边界上的另一半 ——
// 服务层的入参结构里没有那一格（由反射用例钉着），但绑定若是宽容的，
// 调用方递进来的 amount 会被静默丢掉，于是"我传了 5000 而系统按 369.99 记账"
// 在两边看起来都是对的。
func TestBillController_BodyCarriesOnlyTheRowKey(t *testing.T) {
	for _, body := range []string{
		`{"quote_row_id":"q_1","amount":5000}`,
		`{"quote_row_id":"q_1","status":"paid"}`,
		`{"quote_row_id":"q_1","currency":"USD"}`,
		`{"quote_row_id":"q_1","due_at":"2026-12-31"}`,
		`{"quote_row_id":"q_1","opportunity_id":"opp_x"}`,
		`{"quote_id":"QT-1-2"}`,
	} {
		fake := &billFake{available: true}
		code, env, _ := doBill(t, billTestEngine(newBillCtrl(fake), billSession), http.MethodPost, "/api/bill", body)
		if code != http.StatusBadRequest {
			t.Errorf("体 %s 回 %d，期望 400：%s", body, code, env.Message)
		}
		if got := billReasonFrom(t, env); got != "input_invalid" {
			t.Errorf("体 %s 的 reason=%q，期望 input_invalid", body, got)
		}
		if fake.calls != 0 {
			t.Errorf("体 %s 服务层仍被调用 %d 次：越界入参走到了业务判据之后", body, fake.calls)
		}
	}
}

func TestBillController_MissingOrMalformedBody(t *testing.T) {
	for _, probe := range []struct{ name, body string }{
		{"空体", ""},
		{"空对象", `{}`},
		{"行号为空白", `{"quote_row_id":"   "}`},
		{"形状不对", `["q_1"]`},
		{"类型不对", `{"quote_row_id":12}`},
	} {
		fake := &billFake{available: true}
		code, env, _ := doBill(t, billTestEngine(newBillCtrl(fake), billSession), http.MethodPost, "/api/bill", probe.body)
		if code != http.StatusBadRequest {
			t.Errorf("%s 回 %d，期望 400：%s", probe.name, code, env.Message)
		}
		if got := billReasonFrom(t, env); got != "input_invalid" {
			t.Errorf("%s 的 reason=%q，期望 input_invalid", probe.name, got)
		}
		if fake.calls != 0 {
			t.Errorf("%s 服务层被调用 %d 次：空行号本该在门口挡下", probe.name, fake.calls)
		}
	}
}

// TestBillController_BodyIsCapped 请求体封顶：判据必须**只有封顶才兑现得了**。
//
// 第一具体是"语义合法而字节数超限"：前导空白在 TrimSpace 后消失，行号本身合规，
// 唯一过界的是体积 —— 摘掉封顶它就会一路走进派生腿并真的开出一张应收。
// 只测"20KB 的 q"抓不到这件事：那一具体同时被行号长度上限挡下，封顶在不在都回 400，
// 等于没测（本卡变异电池 K61 就是这么暴露的）。第二具体管回显有界。
//
// reason 三肢同为 input_invalid、状态码同为 400，所以断言还要点出"红自体积那一支"。
func TestBillController_BodyIsCapped(t *testing.T) {
	fake := &billFake{available: true}
	body := `{"quote_row_id":"` + strings.Repeat(" ", 4<<10) + `q_100_1"}`
	if len(body) <= billBodyMaxBytes {
		t.Fatalf("探针本身没过界（len=%d，上限=%d），这一格没有牙", len(body), billBodyMaxBytes)
	}
	code, env, _ := doBill(t, billTestEngine(newBillCtrl(fake), billSession), http.MethodPost, "/api/bill", body)
	if code != http.StatusBadRequest {
		t.Errorf("语义合法而字节超限的体回 %d，期望 400：%s", code, env.Message)
	}
	if !strings.Contains(env.Message, "请求体超过") {
		t.Errorf("红因不是体积那一支：%q", env.Message)
	}
	if fake.calls != 0 {
		t.Errorf("超大请求体仍然走到了服务层（calls=%d）：封顶没生效", fake.calls)
	}

	noise := &billFake{available: true}
	noiseBody := `{"quote_row_id":"` + strings.Repeat("q", 20<<10) + `"}`
	code, env, _ = doBill(t, billTestEngine(newBillCtrl(noise), billSession), http.MethodPost, "/api/bill", noiseBody)
	if code != http.StatusBadRequest {
		t.Errorf("超大请求体回 %d，期望 400：%s", code, env.Message)
	}
	if strings.Contains(env.Message, strings.Repeat("q", 64)) {
		t.Errorf("超限请求体的内容被回显进提示里：%q", env.Message)
	}
	if noise.calls != 0 {
		t.Error("超大请求体仍然走到了服务层")
	}
}

// TestBillController_RowIDLengthIsBounded 行号长度有界：它会被回显进提示语里。
//
// 上限取 64 而不是"够用就行"：bills.id 会被抄进 payments.bill_id（varchar(64)，T-P7-02），
// 而 quotes.id 那一侧同样是 text。无上限的回显等于一个免费的响应体放大器。
func TestBillController_RowIDLengthIsBounded(t *testing.T) {
	fake := &billFake{available: true}
	long := `{"quote_row_id":"q_` + strings.Repeat("x", 200) + `"}`
	code, env, _ := doBill(t, billTestEngine(newBillCtrl(fake), billSession), http.MethodPost, "/api/bill", long)
	if code != http.StatusBadRequest {
		t.Errorf("超长行号回 %d，期望 400：%s", code, env.Message)
	}
	if strings.Contains(env.Message, strings.Repeat("x", 64)) {
		t.Error("超长入参被原样回显")
	}
	if fake.calls != 0 {
		t.Error("超长行号仍然走到了服务层")
	}
}

// —— ③ 哨兵分诊 ——————————————————————————————————————————————

// TestBillController_SentinelsHaveDistinctOutcomes 八个哨兵各有档位，且 reason 两两不同。
//
// 判据打在"两两不同"而不是"逐格期望值"上（下面那张表仍逐格写死），因为这一族最常见的
// 退化是"新加一个哨兵时顺手复用了一个现成的 reason"——那样编译过、用例也可能照样绿，
// 而调用方从此分不清两种修法。
func TestBillController_SentinelsHaveDistinctOutcomes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		reason string
	}{
		{service.ErrBillInputInvalid, http.StatusBadRequest, "input_invalid"},
		{service.ErrBillQuoteNotFound, http.StatusNotFound, "not_found"},
		{service.ErrBillQuoteNotSent, http.StatusConflict, "not_sent"},
		{service.ErrBillNotLatestVersion, http.StatusConflict, "not_latest_version"},
		{service.ErrBillChainAlreadyAccepted, http.StatusConflict, "chain_already_accepted"},
		{service.ErrBillLinesMissing, http.StatusConflict, "lines_missing"},
		{service.ErrBillStatusStuck, http.StatusConflict, "status_stuck"},
		{service.ErrBillServiceUnavailable, http.StatusServiceUnavailable, "unavailable"},
		{errors.New("connection reset by peer"), http.StatusInternalServerError, "internal"},
	}
	seen := map[string]int{}
	for _, c := range cases {
		seen[c.reason]++
		fake := &billFake{available: true, err: c.err}
		code, env, _ := doBill(t, billTestEngine(newBillCtrl(fake), billSession), http.MethodPost, "/api/bill",
			`{"quote_row_id":"q_1"}`)
		if code != c.status {
			t.Errorf("%v 回 %d，期望 %d：%s", c.err, code, c.status, env.Message)
		}
		if got := billReasonFrom(t, env); got != c.reason {
			t.Errorf("%v 的 reason=%q，期望 %q", c.err, got, c.reason)
		}
		// 包装过的哨兵同样要分得开：服务层每一格都带 %w 与上下文文本。
		fake2 := &billFake{available: true, err: &billWrapped{inner: c.err}}
		code2, env2, _ := doBill(t, billTestEngine(newBillCtrl(fake2), billSession), http.MethodPost, "/api/bill", `{"quote_row_id":"q_1"}`)
		if code2 != c.status || billReasonFrom(t, env2) != c.reason {
			t.Errorf("包装后的 %v 回 %d/%s，期望 %d/%q", c.err, code2,
				billDataFrom(t, env2)["reason"], c.status, c.reason)
		}
	}
	for reason, n := range seen {
		if n != 1 {
			t.Errorf("reason %q 出现了 %d 次：两种修法共用了同一个判据词", reason, n)
		}
	}
}

// billWrapped 只带错误链、不带文本的包装器（判 errors.Is 而不是判字符串）。
type billWrapped struct{ inner error }

func (w *billWrapped) Error() string { return "派生账单时出错" }
func (w *billWrapped) Unwrap() error { return w.inner }

// TestBillController_UnknownErrorDoesNotLeak 500 的文案里不许出现底层错误串。
//
// 派生这条腿的底层报错会带着 SQL 片段、索引名（uq_bills_quote_row）与列名，
// 那些是攻击者要的、也是运维要的 —— 后者在日志里，不在响应里。
func TestBillController_UnknownErrorDoesNotLeak(t *testing.T) {
	boom := errors.New(`ERROR: duplicate key value violates unique constraint "uq_bills_quote_row" (SQLSTATE 23505)`)
	fake := &billFake{available: true, err: boom}
	code, env, raw := doBill(t, billTestEngine(newBillCtrl(fake), billSession), http.MethodPost, "/api/bill", `{"quote_row_id":"q_1"}`)
	if code != http.StatusInternalServerError {
		t.Fatalf("回 %d，期望 500：%s", code, env.Message)
	}
	for _, needle := range []string{"23505", "uq_bills_quote_row", "SQLSTATE", "duplicate key"} {
		if strings.Contains(raw, needle) {
			t.Errorf("响应里出现了 %q：底层错误串被透出", needle)
		}
	}
}

// TestBillController_NilResultWithoutErrorIsFailure 服务层"要么给视图要么给错"，
// 走到 (nil, nil) 就是实现漂了：按失败报，不回一个空对象。
func TestBillController_NilResultWithoutErrorIsFailure(t *testing.T) {
	fake := &billFake{available: true, nilResult: true}
	code, env, _ := doBill(t, billTestEngine(newBillCtrl(fake), billSession), http.MethodPost, "/api/bill", `{"quote_row_id":"q_1"}`)
	if code != http.StatusInternalServerError {
		t.Errorf("回 %d，期望 500：%s", code, env.Message)
	}
	if strings.TrimSpace(env.Message) == "" {
		t.Error("空对象配空提示：调用方读到的是一句没有内容的成功")
	}
}

// —— ④ 装配回显 ——————————————————————————————————————————————

// TestBillController_UnassembledAnswersFiveOhThree 两条缺件路：腿为 nil、腿在但 Available 为假。
//
// data 必须非空：503 配空对象会被前端渲染成"这个商机还没有账单"，
// 而此刻的事实是"一次都没查"。业务结论与装配结论混成同一格，是这一层最贵的错。
func TestBillController_UnassembledAnswersFiveOhThree(t *testing.T) {
	for _, probe := range []struct {
		name   string
		derive BillDeriver
	}{
		{"腿为 nil", nil},
		{"腿在但缺件", &billFake{available: false}},
	} {
		fake, _ := probe.derive.(*billFake)
		code, env, _ := doBill(t, billTestEngine(newBillCtrl(probe.derive), billSession), http.MethodPost, "/api/bill",
			`{"quote_row_id":"q_1"}`)
		if code != http.StatusServiceUnavailable {
			t.Errorf("%s 回 %d，期望 503：%s", probe.name, code, env.Message)
		}
		if got := billReasonFrom(t, env); got != "unavailable" {
			t.Errorf("%s 的 reason=%q，期望 unavailable", probe.name, got)
		}
		if fake != nil && fake.calls != 0 {
			t.Errorf("%s：Available 为假仍然调用了派生（%d 次）", probe.name, fake.calls)
		}
	}
	// nil 控制器（连实例都没有）不许 panic：这是"路由挂在了装配之前"的真实形状。
	var nilCtrl *BillController
	if nilCtrl.Available() {
		t.Error("nil 控制器报告可用")
	}
	code, env, _ := doBill(t, billTestEngine(nilCtrl, billSession), http.MethodPost, "/api/bill", `{"quote_row_id":"q_1"}`)
	if code != http.StatusServiceUnavailable {
		t.Errorf("nil 控制器回 %d，期望 503：%s", code, env.Message)
	}
}

// —— ⑤ 接缝形状 ——————————————————————————————————————————————

// TestBillController_SeamIsExactlyTheDocumentedPair 发接缝里只有 Available 与 DeriveFromQuote。
//
// 判据与报价侧同一条，但这里更具体：**没有** Cancel / Void / MarkPaid / UpdateStatus。
// 账单的作废与结清要读回款（T-P7-02），而回款入账走的是外部 webhook ——
// 今天在这层开任何一格"把账单改成已收"，就是给财务台账开一条不走回款的捷径。
func TestBillController_SeamIsExactlyTheDocumentedPair(t *testing.T) {
	st := reflect.TypeOf((*BillDeriver)(nil)).Elem()
	got := make([]string, st.NumMethod())
	for i := 0; i < st.NumMethod(); i++ {
		got[i] = st.Method(i).Name
	}
	want := []string{"Available", "DeriveFromQuote"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("BillDeriver 方法集=%v，期望恰好 %v", got, want)
	}
}
