package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// stubLTCReader 是一台"照着脚本给配置"的读侧替身。
type stubLTCReader struct {
	cfg   *service.LTCConfig
	reads int
}

func (s *stubLTCReader) Config(ctx context.Context) *service.LTCConfig {
	s.reads++
	return s.cfg
}

var _ LTCConfigReader = (*stubLTCReader)(nil)

// sideEffects 记录"业务 handler 有没有真的跑起来"。闸门拦不住副作用就等于没拦：
// 这条计数器是 AC①（不产生外发/写库副作用）在这张卡里唯一的证据形态。
type sideEffects struct {
	handlerCalls int
}

func (s *sideEffects) handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		s.handlerCalls++
		c.String(http.StatusOK, "biz")
	}
}

// doGate 造一条挂了闸门的真实路由，业务 handler 只负责把"我跑过了"记下来。
func doGate(t *testing.T, stage service.LTCStage, reader LTCConfigReader) (*httptest.ResponseRecorder, *sideEffects) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	se := &sideEffects{}
	r := gin.New()
	r.GET("/ltc/thing", ltcStageGate(stage, reader), se.handler())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/ltc/thing", nil))
	return w, se
}

func decodeData(t *testing.T, w *httptest.ResponseRecorder) (map[string]any, gin.H) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是 JSON：%v\n%s", err, w.Body.String())
	}
	data, _ := body["data"].(map[string]any)
	return body, gin.H(data)
}

// ---- AC①：没打开 ⇒ 409，且业务 handler 一次都没执行 ------------------------

// 三条码对应三种修法（与浏览器域批10 同一理由）：
//   - 没开：去 LTC 配置页点两下
//   - 读坏了：点开关没用，得去修存储/告警
//   - 阶段名不认识：路由挂错了，是装配 bug，运营和运维都修不了
//
// 挤成同一个码，任何"按码分流"的改动都会把上面三件事办成同一件。
func TestLTCStageGate_BlockedRequestsNeverReachHandler(t *testing.T) {
	cases := []struct {
		name   string
		cfg    *service.LTCConfig
		reason string
		code   utils.ErrorCode
		phrase string
	}{
		{
			name:   "总开关关着",
			cfg:    &service.LTCConfig{Enabled: false, StagesEnabled: service.LTCStages{Quote: true}, Source: service.SourceDefault},
			reason: service.LTCReasonMasterOff,
			code:   utils.ErrorCodeLTCStageDisabled,
			phrase: "未启用",
		},
		{
			name:   "总开关开着但这一档没开",
			cfg:    &service.LTCConfig{Enabled: true, StagesEnabled: service.LTCStages{Bill: true}, Source: service.SourceLTC},
			reason: service.LTCReasonStageOff,
			code:   utils.ErrorCodeLTCStageDisabled,
			phrase: "未启用",
		},
		{
			name:   "配置读不动（degraded）",
			cfg:    &service.LTCConfig{Enabled: true, StagesEnabled: service.LTCStages{Quote: true}, Degraded: true, DegradeReason: "存储不可用"},
			reason: service.LTCReasonDegraded,
			code:   utils.ErrorCodeLTCConfigDegraded,
			phrase: "配置读不动",
		},
		{
			name:   "阶段名不认识",
			cfg:    &service.LTCConfig{Enabled: true, StagesEnabled: service.LTCStages{Quote: true}},
			reason: service.LTCReasonUnknownStage,
			code:   utils.ErrorCodeLTCStageUnknown,
			phrase: "装配错误",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stage := service.LTCStageQuote
			if tc.reason == service.LTCReasonUnknownStage {
				stage = service.LTCStage("qoute")
			}
			reader := &stubLTCReader{cfg: tc.cfg}
			w, se := doGate(t, stage, reader)

			if w.Code != http.StatusConflict {
				t.Errorf("status=%d，期望 409（body=%s）", w.Code, w.Body.String())
			}
			if se.handlerCalls != 0 {
				t.Errorf("业务 handler 执行了 %d 次：闸门形同虚设，外发/写库副作用已经发生", se.handlerCalls)
			}
			body, data := decodeData(t, w)
			if code, ok := body["code"].(string); !ok || code != string(tc.code) {
				t.Errorf("信封 code=%v，期望 %s", body["code"], tc.code)
			}
			msg, _ := body["message"].(string)
			if !strings.Contains(msg, tc.phrase) {
				t.Errorf("message=%q，没说出「%s」这件事", msg, tc.phrase)
			}
			if !strings.Contains(msg, "未产生外发或写库副作用") {
				t.Errorf("message=%q，没把「拦在哪、有没有副作用」说出口", msg)
			}
			if data["reason"] != tc.reason {
				t.Errorf("reason=%v，期望 %s（调用方按这个字段分流，不能靠读消息文本）", data["reason"], tc.reason)
			}
			if data["stage"] != string(stage) {
				t.Errorf("stage=%v，期望 %s", data["stage"], stage)
			}
		})
	}
}

// 三条码在**真实响应体**上必须互不相同，且不落回 HTTP 派生的 3003。
// 只在常量表上比是自我证明：把三个常量改成同一个值，常量比对仍然绿，
// 而前端已经分流不出来了 —— 这里断的是打了这趟 HTTP 之后 body 里的字。
func TestLTCStageGate_CodesDistinguishTheThreeFailures(t *testing.T) {
	codeOf := func(cfg *service.LTCConfig, stage service.LTCStage) string {
		w, _ := doGate(t, stage, &stubLTCReader{cfg: cfg})
		body, _ := decodeData(t, w)
		code, _ := body["code"].(string)
		return code
	}
	on := service.LTCStages{Quote: true}
	codes := map[string]string{
		"没开":   codeOf(&service.LTCConfig{Enabled: true, StagesEnabled: service.LTCStages{}}, service.LTCStageQuote),
		"读坏了":  codeOf(&service.LTCConfig{Enabled: true, StagesEnabled: on, Degraded: true}, service.LTCStageQuote),
		"挂错阶段": codeOf(&service.LTCConfig{Enabled: true, StagesEnabled: on}, service.LTCStage("qoute")),
	}
	seen := map[string]string{}
	for name, code := range codes {
		if code == "" {
			t.Errorf("%s 的响应体没有字符串 code：%v", name, code)
			continue
		}
		if code == string(utils.ErrorCodeDuplicateEntry) || code == string(utils.ErrorCodeAlreadyExists) {
			t.Errorf("%s 折回了「重复记录」这一 HTTP 派生码（%s）：闸门一个键都没写，码却在说键冲突", name, code)
		}
		if prev, dup := seen[code]; dup {
			t.Errorf("%s 与 %s 撞在同一个码 %s 上，前端无法分流", name, prev, code)
		}
		seen[code] = name
	}
}

// degraded 与"运营关着"必须分得开：前者要去修存储，后者要去点开关。
func TestLTCStageGate_DegradedIsNotRenderedAsOperatorShutdown(t *testing.T) {
	cfg := &service.LTCConfig{
		Enabled: true, StagesEnabled: service.LTCStages{Quote: true},
		Degraded: true, DegradeReason: "读取 ltc.config 失败：connection reset", Source: service.SourceDefault,
	}
	w, se := doGate(t, service.LTCStageQuote, &stubLTCReader{cfg: cfg})
	if se.handlerCalls != 0 {
		t.Error("degraded 放行了：读不到配置等于默认全开，这条承诺是反的")
	}
	_, data := decodeData(t, w)
	if degraded, _ := data["degraded"].(bool); !degraded {
		t.Errorf("data.degraded=%v，必须是 true，否则运维会以为运营把开关关了", data["degraded"])
	}
	if src, _ := data["config_source"].(string); src != service.SourceDefault {
		t.Errorf("config_source=%v，期望 %s（这份是回落默认，不是库里那一份）", src, service.SourceDefault)
	}
	if r, _ := data["degrade_reason"].(string); !strings.Contains(r, "connection reset") {
		t.Errorf("degrade_reason=%q，没带上底层故障原因", r)
	}
	if master, _ := data["master_enabled"].(bool); !master {
		t.Error("master_enabled=false：库里明明写着开着，读数不能把两件事混成一件")
	}
}

// ---- 放行侧 ---------------------------------------------------------------

func TestLTCStageGate_PassesWhenBothLocksAreOn(t *testing.T) {
	cfg := &service.LTCConfig{
		Enabled:       true,
		StagesEnabled: service.LTCStages{}.With(service.LTCStageQuote),
		Source:        service.SourceLTC,
	}
	reader := &stubLTCReader{cfg: cfg}
	w, se := doGate(t, service.LTCStageQuote, reader)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s，两道锁都开着应当放行", w.Code, w.Body.String())
	}
	if se.handlerCalls != 1 {
		t.Errorf("handler 执行 %d 次，期望 1 次", se.handlerCalls)
	}
	if reader.reads != 1 {
		t.Errorf("每个请求读配置 %d 次，期望 1 次（服务侧自带 TTL 缓存，闸门不该重复拉）", reader.reads)
	}
}

// 阶段独立性在闸门这一层的形状：开了 bill 不能顺手把 quote 放进来。
func TestLTCStageGate_OtherStageOnDoesNotAdmitThisStage(t *testing.T) {
	cfg := &service.LTCConfig{Enabled: true, StagesEnabled: service.LTCStages{}.With(service.LTCStageBill)}
	w, se := doGate(t, service.LTCStageQuote, &stubLTCReader{cfg: cfg})
	if w.Code != http.StatusConflict || se.handlerCalls != 0 {
		t.Errorf("只开 bill 时 quote 被放行了：status=%d handlerCalls=%d", w.Code, se.handlerCalls)
	}
	if w2, se2 := doGate(t, service.LTCStageBill, &stubLTCReader{cfg: cfg}); w2.Code != http.StatusOK || se2.handlerCalls != 1 {
		t.Errorf("开了 bill 却没放行：status=%d handlerCalls=%d", w2.Code, se2.handlerCalls)
	}
}

// ---- 挂载登记：把"挂了几条"做成可读的事实 ---------------------------------

func TestLTCStageGate_RegistrationIsCountedAndDefensive(t *testing.T) {
	before := LTCGuardedRoutes()[service.LTCStagePayment]

	_ = LTCStageGate(service.LTCStagePayment)
	_ = LTCStageGate(service.LTCStagePayment)

	after := LTCGuardedRoutes()[service.LTCStagePayment]
	if after != before+2 {
		t.Errorf("挂载登记从 %d 涨到 %d，期望 +2（每条挂闸门的路由都要留痕）", before, after)
	}

	// 返回的必须是副本：调用方改一笔就让全进程的读数说谎。
	snap := LTCGuardedRoutes()
	snap[service.LTCStagePayment] = 99999
	if got := LTCGuardedRoutes()[service.LTCStagePayment]; got == 99999 {
		t.Error("LTCGuardedRoutes 返回了内部 map，外部可写")
	}
}
