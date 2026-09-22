// bill_test.go T-P7-01 + T-P7-02：账单出口的入参口径与哨兵分诊（写：派生；读：对账）。
//
// 服务层那 18 条钉的是"谁够得着写、什么顺序写"，本层钉的是另一件事：
// **那八个哨兵在 HTTP 上分不分得开**。派生失败的处置动作各不相同（改载荷 / 换行号 /
// 去处置原账单 / 重读那一版再来），全压成 400 或全压成 500 时，调用方只能靠猜，
// 而猜错的方向在财务域是"再点一次"——那正是本域最不该重试的动作。
//
// 两条看着多余的用例是这张网的承重墙，理由各自写在函数注释上：
// ① 越界入参（体里带 amount / status）必须在**进服务层之前**被拒；
// ② 未装配与底座故障必须分得开（只测 503 的话，摘掉错误分支也看不出来）。
//
// T-P7-02 加的是读侧那六条，判据同源而对象不同：入参从请求体换成 URL 上那把键，
// 哨兵从八个换成四个（读不会撞并发、不该撞状态机），而**两腿必须各自关闸**——
// "派生能跑、对账读不到"是一种会真实发生的半装配，它不能表现为"两边一起 503"。
package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// billReadFake 实现 BillReader。
//
// 两格共用一个 calls 与 last：本层对读腿的判据是"哪把键透到了服务层"与"未装配时一次都不许读"，
// 而每一格用例只打一条端点，分开计数只是把断言写长。
type billReadFake struct {
	available bool
	calls     int
	last      string
	stmt      *service.BillStatementView
	list      []*service.BillStatementView
	err       error
	nilResult bool
	nilList   bool
}

func (f *billReadFake) Available() bool { return f.available }

func (f *billReadFake) Statement(_ context.Context, billID string) (*service.BillStatementView, error) {
	f.calls++
	f.last = billID
	if f.err != nil {
		return nil, f.err
	}
	if f.nilResult {
		return nil, nil
	}
	if f.stmt != nil {
		return f.stmt, nil
	}
	return billTestStatement(billID), nil
}

func (f *billReadFake) StatementsOfQuote(_ context.Context, quoteID string) ([]*service.BillStatementView, error) {
	f.calls++
	f.last = quoteID
	if f.err != nil {
		return nil, f.err
	}
	if f.nilList {
		return nil, nil
	}
	return f.list, nil
}

// billTestStatement 一份"收过一笔、还欠尾款"的对账视图。
//
// 状态与币种写成字面量而不是引 model 里的常量：这一层往外送的是 JSON 上的词，
// 常量改名是本包之外的事，而线上那一格漂了要在这条用例上红。
func billTestStatement(billID string) *service.BillStatementView {
	return &service.BillStatementView{
		BillID: billID, QuoteID: "QT-1-2", QuoteRowID: "q_100_1", OpportunityID: "opp_1",
		Status: "partial", Amount: 369.99, Currency: "CNY",
		Settled: 200, Outstanding: 169.99,
		Payments: []service.PaymentView{{
			ID: "p_1", BillID: billID, Amount: 200, Currency: "CNY",
			ChannelRef: "ref-1", Status: "settled", Platform: "taobao", OrderID: "ord-1",
			PaidAt: time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC),
		}},
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

func newBillCtrl(derive BillDeriver) *BillController { return NewBillController(derive, nil) }

// newBillReadCtrl 只装读腿的那一半（派生侧的用例不需要它，读侧的用例不需要那一半）。
// 两腿分开构造是本层的真实形状：两个装配点各装一条，可以一有一无。
func newBillReadCtrl(read BillReader) *BillController { return NewBillController(nil, read) }

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

// TestBillController_ReadSeamIsExactlyTheDocumentedTriple 读接缝只有 Available 与两格 Statement*。
//
// 这一格真正判的是**没有**什么：没有 GetByID / List / 任何仓储级行读，也没有分页与按客户捞。
// 一旦这层能拿到 bills 与 payments 的行，它就会顺手自己加一遍总，而"已收"这个数
// 只允许有一个算法（service/payment.go 的 statementOfBill）—— 多一处算就是多一份事实，
// 而 AC② 要对的正是这个数。方法集合用反射钉，因为"加一个方法"在编译面上毫无痕迹：
// 接缝变宽不需要改任何调用点。
func TestBillController_ReadSeamIsExactlyTheDocumentedTriple(t *testing.T) {
	st := reflect.TypeOf((*BillReader)(nil)).Elem()
	got := make([]string, st.NumMethod())
	for i := 0; i < st.NumMethod(); i++ {
		got[i] = st.Method(i).Name
	}
	want := []string{"Available", "Statement", "StatementsOfQuote"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("BillReader 方法集=%v，期望恰好 %v", got, want)
	}
}

// —— ⑥ 读侧：结论的形状 ——————————————————————————————

// TestBillController_ViewHappyPath 读到的必须**就是服务层给的那份结论**，逐格原样出。
//
// 断言里最要紧的一格是"本层没有把 settled/outstanding 改写过"：期望值 200/169.99 来自
// 夹具（即服务层），而不是来自控制器的计算。第二要紧的是 due_at 在没账期时**整格缺席**
// （BillStatementView 上是 *time.Time + omitempty）—— 这与派生侧的 BillView 不同：
// 那边是"账期未定"要写 null 给前端看，这边的对账视图今天没有任何一处填它，
// 硬造一个 null 反而是把"这一格存在"当成契约承诺出去。
func TestBillController_ViewHappyPath(t *testing.T) {
	fake := &billReadFake{available: true}
	code, env, raw := doBill(t, billTestEngine(newBillReadCtrl(fake), billSession),
		http.MethodGet, "/api/bill/b_1762335000000000000_1", "")
	if code != http.StatusOK {
		t.Fatalf("读单回 %d：%s —— %s", code, env.Message, raw)
	}
	if fake.calls != 1 {
		t.Fatalf("服务层被调用 %d 次，期望 1", fake.calls)
	}
	if fake.last != "b_1762335000000000000_1" {
		t.Errorf("传给服务层的账单号是 %q，期望原样透传", fake.last)
	}
	data := billDataFrom(t, env)
	for key, want := range map[string]any{
		"bill_id": "b_1762335000000000000_1", "quote_id": "QT-1-2", "quote_row_id": "q_100_1",
		"opportunity_id": "opp_1", "status": "partial", "currency": "CNY",
	} {
		if data[key] != want {
			t.Errorf("data.%s=%v，期望 %v", key, data[key], want)
		}
	}
	// 三个数一起看才叫对账：只给"还欠多少"的话，两边各自少一个可比的数。
	for key, want := range map[string]float64{"amount": 369.99, "settled": 200, "outstanding": 169.99} {
		if got, ok := data[key].(float64); !ok || got != want {
			t.Errorf("data.%s=%v，期望 %v（这一层不加第二遍总）", key, data[key], want)
		}
	}
	rows, ok := data["payments"].([]any)
	if !ok {
		t.Fatalf("data.payments=%T，期望数组：%v", data["payments"], data["payments"])
	}
	if len(rows) != 1 {
		t.Fatalf("payments %d 行，期望 1", len(rows))
	}
	if first, _ := rows[0].(map[string]any); first["channel_ref"] != "ref-1" || first["amount"] != 200.0 {
		t.Errorf("回款行透得不完整：%v", rows[0])
	}
	if v, present := data["due_at"]; present {
		t.Errorf("due_at=%v，期望整格缺席（夹具没给账期，这一层不替它编一个）", v)
	}
}

// TestBillController_DueAtPassesThroughWhenTheServiceSetsIt 账期那一格由 T-P7-03 填；
// 今天先把"服务给了就出得来"钉住，免得那一卡交付时才发现读侧把它吞了。
func TestBillController_DueAtPassesThroughWhenTheServiceSetsIt(t *testing.T) {
	st := billTestStatement("b_1")
	due := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	st.DueAt = &due
	fake := &billReadFake{available: true, stmt: st}
	code, env, raw := doBill(t, billTestEngine(newBillReadCtrl(fake), billSession), http.MethodGet, "/api/bill/b_1", "")
	if code != http.StatusOK {
		t.Fatalf("回 %d：%s —— %s", code, env.Message, raw)
	}
	got, present := billDataFrom(t, env)["due_at"].(string)
	if !present || !strings.HasPrefix(got, "2026-12-01") {
		t.Errorf("data.due_at=%v（存在=%v），期望以 2026-12-01 开头", billDataFrom(t, env)["due_at"], present)
	}
}

// TestBillController_OfQuoteShape 第二个入口：两个数（list 与 count）加上"没有也是数组"。
//
// 三格具体各有归宿：
//   - 两张：一条链上多个版本各自成交过一次，对账要读的是那一串，count 必须同长；
//   - 零张而服务给空切片：list 出 []；
//   - 零张而服务**直接 return nil**：今天 statementOf 那条路给的是 make 出来的空切片，
//     哪天改成 return nil 就是线上从 [] 变 null 而全绿 —— null 在前端是"没读到"，
//     而这里的事实是"这张单子确实一张应收都没开"。
func TestBillController_OfQuoteShape(t *testing.T) {
	one := billTestStatement("b_1")
	two := billTestStatement("b_2")
	two.Status = "paid"
	for _, probe := range []struct {
		name      string
		list      []*service.BillStatementView
		nilList   bool
		wantItems int
	}{
		{"两张", []*service.BillStatementView{one, two}, false, 2},
		{"空切片", []*service.BillStatementView{}, false, 0},
		{"服务给 nil", nil, true, 0},
	} {
		fake := &billReadFake{available: true, list: probe.list, nilList: probe.nilList}
		code, env, raw := doBill(t, billTestEngine(newBillReadCtrl(fake), billSession),
			http.MethodGet, "/api/bill/of-quote/QT-1-2", "")
		if code != http.StatusOK {
			t.Fatalf("%s 回 %d：%s —— %s", probe.name, code, env.Message, raw)
		}
		if fake.last != "QT-1-2" {
			t.Errorf("%s：传给服务层的报价号是 %q，期望原样透传", probe.name, fake.last)
		}
		data := billDataFrom(t, env)
		items, ok := data["list"].([]any)
		if !ok {
			t.Fatalf("%s：data.list=%T，期望数组（null 与 [] 是两件事）", probe.name, data["list"])
		}
		if len(items) != probe.wantItems {
			t.Errorf("%s：list %d 项，期望 %d", probe.name, len(items), probe.wantItems)
		}
		if n, ok := data["count"].(float64); !ok || int(n) != probe.wantItems {
			t.Errorf("%s：count=%v，期望 %d（与 list 同长，否则前端按 count 渲染会截断）", probe.name, data["count"], probe.wantItems)
		}
	}
}

// —— ⑦ 读侧的入参：URL 上那把键 ——————————————————————

// TestBillController_BlankPathKeyIsRejectedLocally 空键在这一层挡下，且服务层一次都不许被调用。
//
// 空串在这一族路由上是**能匹配**的（`/api/bill/ ` 那一种"看着有其实没有"，%20 解出来是一个空格），
// 所以判据不能只靠"路由挂在那儿"。透到服务层会撞上它自己的入参哨兵，那句错误文本的主语
// 是 bill_id，调用方在 URL 上找不到这个词；更坏的是服务层哪天放松那一格，
// 空键就退化成"读全表第一行"——那在账单域是别人的应收。
func TestBillController_BlankPathKeyIsRejectedLocally(t *testing.T) {
	for _, path := range []string{"/api/bill/%20", "/api/bill/of-quote/%20"} {
		fake := &billReadFake{available: true}
		code, env, _ := doBill(t, billTestEngine(newBillReadCtrl(fake), billSession), http.MethodGet, path, "")
		if code != http.StatusBadRequest {
			t.Errorf("%s 回 %d，期望 400：%s", path, code, env.Message)
		}
		if got := billReasonFrom(t, env); got != "input_invalid" {
			t.Errorf("%s 的 reason=%q，期望 input_invalid", path, got)
		}
		if fake.calls != 0 {
			t.Errorf("%s：空键仍然走到了服务层（calls=%d）", path, fake.calls)
		}
	}
}

// TestBillController_OverlongPathKeyIsRejectedAndNotEchoed 键宽与回显有界（同派生侧的行号那一格）。
//
// 上限取 64 是因为 bills.id / bills.quote_id / quotes.id 三把键列同为 varchar(64)：
// 超长必是拿错了东西（把整段 URL 抄进来了），而提示语里会带上它。
func TestBillController_OverlongPathKeyIsRejectedAndNotEchoed(t *testing.T) {
	long := "b_" + strings.Repeat("x", 200)
	for _, path := range []string{"/api/bill/" + long, "/api/bill/of-quote/" + long} {
		fake := &billReadFake{available: true}
		code, env, _ := doBill(t, billTestEngine(newBillReadCtrl(fake), billSession), http.MethodGet, path, "")
		if code != http.StatusBadRequest {
			t.Errorf("超长键回 %d，期望 400：%s", code, env.Message)
		}
		if strings.Contains(env.Message, strings.Repeat("x", 64)) {
			t.Errorf("超长键被原样回显进提示里：%q", env.Message)
		}
		if fake.calls != 0 {
			t.Errorf("%s：超长键仍然走到了服务层", path)
		}
	}
}

// TestBillController_PathKeyPassesThroughUnchanged 除首尾空白之外**不做任何清洗**。
//
// 这一条判的是"没写的那一半"：控制器把账单号转小写、补前缀、去掉下划线中的任何一种，
// 都会把一次本来能命中的读变成 404，而 404 说的是"没有这张单"——在财务域这是一句谎。
// 大小写混排与下划线同时出现在探针里，因为那正是账单号（b_<unixnano>_<seq>）与报价号
// （QT-XXX）真实的形状。
func TestBillController_PathKeyPassesThroughUnchanged(t *testing.T) {
	for _, probe := range []struct{ path, key string }{
		{"/api/bill/QT_MixedCase_9", "QT_MixedCase_9"},
		{"/api/bill/of-quote/QT-MIXED-9", "QT-MIXED-9"},
	} {
		fake := &billReadFake{available: true}
		code, _, raw := doBill(t, billTestEngine(newBillReadCtrl(fake), billSession), http.MethodGet, probe.path, "")
		if code != http.StatusOK {
			t.Fatalf("%s 回 %d：%s", probe.path, code, raw)
		}
		if fake.last != probe.key {
			t.Errorf("透到服务层的键是 %q，期望原样 %q（这一层不清洗账单号）", fake.last, probe.key)
		}
	}
}

// —— ⑧ 读侧的分诊与关闸 ——————————————————————————————

// TestBillController_ReadSentinelsHaveDistinctOutcomes 读侧四档各有各的 reason，两条端点同判据。
//
// 只有四档且**没有 409**：读不会撞并发、不会撞状态机，任何"你先去处置一下再来"在这里
// 都是把一次查询写成了一个流程。两条端点各跑一遍是因为分诊出口虽共用，
// handler 却是两个（给其中一个单独加一档 switch 是零成本的事）。
// 503 那一格在 available=true 的具体下打：那是"腿装着而仓储说没库"，
// 与下面那条关闸用例判的不是同一件事。
func TestBillController_ReadSentinelsHaveDistinctOutcomes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		reason string
	}{
		{service.ErrPaymentInputInvalid, http.StatusBadRequest, "input_invalid"},
		{fmt.Errorf("%w: b_nope 没有对应应收", service.ErrPaymentBillNotFound), http.StatusNotFound, "not_found"},
		{fmt.Errorf("%w: 仓储未接库", service.ErrPaymentServiceUnavailable), http.StatusServiceUnavailable, "unavailable"},
		{errors.New("connection reset by peer"), http.StatusInternalServerError, "internal"},
	}
	seen := map[string]int{}
	for _, c := range cases {
		seen[c.reason]++
		for _, path := range []string{"/api/bill/b_1", "/api/bill/of-quote/QT-1-2"} {
			fake := &billReadFake{available: true, err: c.err}
			code, env, _ := doBill(t, billTestEngine(newBillReadCtrl(fake), billSession), http.MethodGet, path, "")
			if code != c.status {
				t.Errorf("%v @ %s 回 %d，期望 %d：%s", c.err, path, code, c.status, env.Message)
			}
			if got := billReasonFrom(t, env); got != c.reason {
				t.Errorf("%v @ %s 的 reason=%q，期望 %q", c.err, path, got, c.reason)
			}
		}
	}
	for reason, n := range seen {
		if n != 1 {
			t.Errorf("reason %q 出现了 %d 次：两种修法共用了同一个判据词", reason, n)
		}
	}
	// 404 的文案要带着调用方递进来的那把键：不透出的话，"我查的哪个号"这一格只能去翻日志。
	fake := &billReadFake{available: true, err: fmt.Errorf("%w: b_check_me", service.ErrPaymentBillNotFound)}
	_, env, _ := doBill(t, billTestEngine(newBillReadCtrl(fake), billSession), http.MethodGet, "/api/bill/b_check_me", "")
	if !strings.Contains(env.Message, "b_check_me") {
		t.Errorf("「没这张单」那一档没有回显键名：%q", env.Message)
	}
}

// TestBillController_ReadErrorDoesNotLeak 读侧 500 同样不透出底层错误串。
//
// 这一条腿的底层报错来自 statementOfBill 里两处求和失败，带的是表名（payments）与 SQL 片段。
func TestBillController_ReadErrorDoesNotLeak(t *testing.T) {
	boom := errors.New(`ERROR: relation "payments" does not exist (SQLSTATE 42P01)`)
	for _, path := range []string{"/api/bill/b_1", "/api/bill/of-quote/QT-1-2"} {
		fake := &billReadFake{available: true, err: boom}
		code, env, raw := doBill(t, billTestEngine(newBillReadCtrl(fake), billSession), http.MethodGet, path, "")
		if code != http.StatusInternalServerError {
			t.Fatalf("%s 回 %d，期望 500：%s", path, code, env.Message)
		}
		for _, needle := range []string{"42P01", "SQLSTATE", "relation", "payments"} {
			if strings.Contains(raw, needle) {
				t.Errorf("%s 的响应里出现了 %q：底层错误串被透出", path, needle)
			}
		}
	}
}

// TestBillController_ReadNilResultWithoutErrorIsFailure 读侧的 (nil, nil) 按失败报。
//
// 这一格比派生侧更贵：空对象配 200 会被前端渲染成"这张应收金额 0、已收 0、没有回款行"，
// 而对账的人据此得出的结论是"这笔钱结了"或"这单没开账"——两种都是错。
func TestBillController_ReadNilResultWithoutErrorIsFailure(t *testing.T) {
	fake := &billReadFake{available: true, nilResult: true}
	code, env, _ := doBill(t, billTestEngine(newBillReadCtrl(fake), billSession), http.MethodGet, "/api/bill/b_1", "")
	if code != http.StatusInternalServerError {
		t.Errorf("回 %d，期望 500：%s", code, env.Message)
	}
	if got := billReasonFrom(t, env); got != "internal" {
		t.Errorf("reason=%q，期望 internal（实现漂了不是'没这张单'）", got)
	}
	if strings.TrimSpace(env.Message) == "" {
		t.Error("空对象配空提示：调用方读到的是一句没有内容的成功")
	}
}

// TestBillController_ReadLegUnassembledAnswersFiveOhThree 读腿三种缺件形状，两条端点各回 503。
//
// 与派生侧同一条判据（503 而不是 404），但这里多一重理由：账单在库里**一直是有的**，
// 只是这次没读到。404 会被读成"这张单子没开账"，那是业务结论，而此刻的事实是"一次都没查"。
// 文案里必须出现"对账"而不能出现"派生"：读到 503 的人要立刻知道该催哪个装配点
// （app.InitPaymentRuntime），而不是去查为什么开不出应收。
func TestBillController_ReadLegUnassembledAnswersFiveOhThree(t *testing.T) {
	for _, probe := range []struct {
		name string
		read BillReader
	}{
		{"腿为 nil", nil},
		{"腿在但缺件", &billReadFake{available: false}},
	} {
		fake, _ := probe.read.(*billReadFake)
		for _, path := range []string{"/api/bill/b_1", "/api/bill/of-quote/QT-1-2"} {
			code, env, _ := doBill(t, billTestEngine(newBillReadCtrl(probe.read), billSession), http.MethodGet, path, "")
			if code != http.StatusServiceUnavailable {
				t.Errorf("%s @ %s 回 %d，期望 503：%s", probe.name, path, code, env.Message)
			}
			if got := billReasonFrom(t, env); got != "unavailable" {
				t.Errorf("%s 的 reason=%q，期望 unavailable", probe.name, got)
			}
			if !strings.Contains(env.Message, "对账") || strings.Contains(env.Message, "派生") {
				t.Errorf("%s 的文案没点名读腿：%q", probe.name, env.Message)
			}
			if fake != nil && fake.calls != 0 {
				t.Errorf("%s：Available 为假仍然读了（%d 次）", probe.name, fake.calls)
			}
		}
	}
	// nil 控制器：两条 GET 都不许 panic，且回 503 而不是 500。
	var nilCtrl *BillController
	if nilCtrl.Available() {
		t.Error("nil 控制器报告可用")
	}
	if nilCtrl.canRead() {
		t.Error("nil 控制器报告可读")
	}
	code, env, _ := doBill(t, billTestEngine(nilCtrl, billSession), http.MethodGet, "/api/bill/b_1", "")
	if code != http.StatusServiceUnavailable {
		t.Errorf("nil 控制器读单回 %d，期望 503：%s", code, env.Message)
	}
}

// TestBillController_LegsFailIndependently 两腿各自关闸：一半装配时另一半照常工作。
//
// 这一条是"两把接缝而不是合成一把"的全部回报。两个装配点是两次真事（InitBillRuntime
// 与 InitPaymentRuntime），于是半装配是一种会真实发生的故障形状；
// 而半装配在 HTTP 面上必须表现成"一半 503、一半 200"，不是"两边一起 503"——
// 后者会把运维送去查一个本来就好的底座。
func TestBillController_LegsFailIndependently(t *testing.T) {
	deriveOnly := NewBillController(&billFake{available: true}, nil)
	if !deriveOnly.Available() {
		t.Fatal("只装派生腿时 Available() 报告不可用")
	}
	if deriveOnly.canRead() {
		t.Error("只装派生腿时 canRead() 报告可读：两半混成了一半")
	}
	code, env, _ := doBill(t, billTestEngine(deriveOnly, billSession), http.MethodPost, "/api/bill", `{"quote_row_id":"q_1"}`)
	if code != http.StatusOK {
		t.Errorf("只装派生腿时 POST 回 %d，期望 200：%s", code, env.Message)
	}
	if code, env, _ = doBill(t, billTestEngine(deriveOnly, billSession), http.MethodGet, "/api/bill/b_1", ""); code != http.StatusServiceUnavailable {
		t.Errorf("只装派生腿时 GET 回 %d，期望 503：%s", code, env.Message)
	}

	readOnly := NewBillController(nil, &billReadFake{available: true})
	if readOnly.Available() {
		t.Error("只装读腿时 Available() 报告可派生")
	}
	if !readOnly.canRead() {
		t.Error("只装读腿时 canRead() 报告不可读")
	}
	if code, env, _ = doBill(t, billTestEngine(readOnly, billSession), http.MethodGet, "/api/bill/b_1", ""); code != http.StatusOK {
		t.Errorf("只装读腿时 GET 回 %d，期望 200：%s", code, env.Message)
	}
	if code, env, _ = doBill(t, billTestEngine(readOnly, billSession), http.MethodPost, "/api/bill", `{"quote_row_id":"q_1"}`); code != http.StatusServiceUnavailable {
		t.Errorf("只装读腿时 POST 回 %d，期望 503：%s", code, env.Message)
	}
}
