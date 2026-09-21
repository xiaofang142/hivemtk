package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// ---- 测试替身 ---------------------------------------------------------------

// memKV 是 ltc.config 的内存存储替身，结构上满足 service 侧那个未导出的存储接口。
// 从本包只能"长得像"而不能点名那个接口 —— 这是 Go 里注入非导出依赖的常规形状。
type memKV struct {
	rows      map[string]string
	available bool
	upsertErr error
	writes    int
}

func newMemKV() *memKV { return &memKV{rows: map[string]string{}, available: true} }

func (m *memKV) Available() bool { return m.available }

func (m *memKV) Get(ctx context.Context, key string) (string, error) {
	return m.rows[key], nil
}

func (m *memKV) Upsert(ctx context.Context, key, value string) (string, error) {
	m.writes++
	if m.upsertErr != nil {
		return "", m.upsertErr
	}
	m.rows[key] = value
	return value, nil
}

func (m *memKV) EnsureTable(ctx context.Context) error { return nil }

// useLTCGlobal 把全局 LTC 服务换成接内存存储的实例，用例结束后换回。
// 不换回的话，本二进制里后面任何读到全局 LTC 的用例都会拿到这一条的夹具。
func useLTCGlobal(t *testing.T, kv *memKV) *service.LTCConfigService {
	t.Helper()
	prev := service.GlobalLTCConfig()
	svc := service.NewLTCConfigServiceWithStore(kv)
	service.SetGlobalLTCConfig(svc)
	t.Cleanup(func() { service.SetGlobalLTCConfig(prev) })
	return svc
}

// adminStub 只替代"上游 JWT 已经把 role 写进上下文"这一件事；
// AdminAuthMiddleware 走真实实现 —— 要证的正是这两个端点归它管。
func adminStub(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if role != "" {
			c.Set("role", role)
			c.Set("user_id", uint(7))
		}
		c.Next()
	}
}

func ltcAdminEngine(role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	setupLTCRoutes(r.Group("/api", adminStub(role)))
	return r
}

func serve(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeEnvelope(t *testing.T, w *httptest.ResponseRecorder) (map[string]any, map[string]any) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是 JSON：%v\n%s", err, w.Body.String())
	}
	data, _ := body["data"].(map[string]any)
	return body, data
}

// ---- 管理端点的鉴权装配 ----------------------------------------------------

// 两个端点都必须落在 AdminAuthMiddleware 之后：能改"整条 LTC 要不要对外发东西"的那个按钮，
// 不该是所有登录态都点得动的（/manage/config-params 那族今天正是这个漏法，已登记为遗留项）。
func TestSetup_LTCManageRoutesRejectNonAdmin(t *testing.T) {
	cases := []struct {
		name string
		role string
		want int
	}{
		{"没有身份（未登录）", "", http.StatusUnauthorized},
		{"登录了但不是管理员", "agent", http.StatusForbidden},
	}
	for _, tc := range cases {
		for _, probe := range []struct{ method, path, body string }{
			{"GET", "/api/manage/ltc/config", ""},
			{"PUT", "/api/manage/ltc/config", `{"enabled":true}`},
		} {
			t.Run(tc.name+"/"+probe.method, func(t *testing.T) {
				useLTCGlobal(t, newMemKV())
				w := serve(ltcAdminEngine(tc.role), probe.method, probe.path, probe.body)
				if w.Code != tc.want {
					t.Errorf("status=%d，期望 %d（body=%s）", w.Code, tc.want, w.Body.String())
				}
			})
		}
	}
}

func TestSetup_LTCManageRoutesOpenToAdmin(t *testing.T) {
	useLTCGlobal(t, newMemKV())
	w := serve(ltcAdminEngine("admin"), "GET", "/api/manage/ltc/config", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body, data := decodeEnvelope(t, w)
	if body["code"] != float64(0) {
		t.Errorf("code=%v，期望 0", body["code"])
	}
	for _, key := range []string{"enabled", "stages_enabled", "stage_status", "thresholds",
		"guarded_routes", "guarded_total", "reading_hints", "kv_key", "cache_ttl_seconds", "max_bytes"} {
		if _, ok := data[key]; !ok {
			t.Errorf("响应缺 %s 字段：%s", key, w.Body.String())
		}
	}
}

// ---- GET 视图：把"配成什么样"和"有几条路由归它管"一起给 ---------------------

func TestLTCConfigView_ShowsBothLocksAndMountCount(t *testing.T) {
	cfg := &service.LTCConfig{
		Enabled:       true,
		StagesEnabled: service.LTCStages{}.With(service.LTCStageQuote),
		Thresholds:    service.DefaultLTCConfig().Thresholds,
		Source:        service.SourceLTC,
	}
	view := ltcConfigView(cfg, map[service.LTCStage]int{service.LTCStageQuote: 2})

	status, _ := view["stage_status"].([]gin.H)
	if len(status) != len(service.LTCKnownStages) {
		t.Fatalf("stage_status 有 %d 项，期望 %d（六阶段每一项都要能单独看见生效与否）", len(status), len(service.LTCKnownStages))
	}
	var actives []string
	for _, row := range status {
		if on, _ := row["active"].(bool); on {
			actives = append(actives, row["stage"].(string))
		}
		if r, _ := row["reason"].(string); r == "" {
			t.Errorf("阶段 %v 没给出生效/未生效的原因", row["stage"])
		}
	}
	if len(actives) != 1 || actives[0] != string(service.LTCStageQuote) {
		t.Errorf("生效集合=%v，期望恰好 [quote]", actives)
	}
	if got := view["guarded_total"]; got != 2 {
		t.Errorf("guarded_total=%v，期望 2", got)
	}
	if hints := strings.Join(view["reading_hints"].([]string), "|"); strings.Contains(hints, "没有任何路由") {
		t.Errorf("已经挂了 2 条路由，reading_hint 还在说「没有任何路由」：%s", hints)
	}
}

// 放量档必须出现在这份"读回来的配置"里（T-P5-04）。
//
// PUT 收得下 reach_rollout、GET 却不回显它，等于让运营在两个方向上都会读错：
// 写过的档位看不见 ⇒ 以为没生效而重复写一遍；看不见 ⇒ 也没法改（PUT 是整份替换，
// 看不到名单就只能连名单一起重写，一次改档位顺手把灰度批次清空）。
func TestLTCConfigView_ShowsRolloutModeAndCohort(t *testing.T) {
	cfg := service.DefaultLTCConfig()
	cfg.ReachRollout = service.ReachRollout{
		Mode:      service.ReachRolloutWhitelist,
		Whitelist: []string{"sms:13800000000", "one:u-42"},
	}
	view := ltcConfigView(cfg, nil)

	section, _ := view["reach_rollout"].(gin.H)
	if section == nil {
		t.Fatalf("缺 reach_rollout 这一节（PUT 收得下、GET 却不回显）：%v", view["reach_rollout"])
	}
	if mode, _ := section["mode"].(service.ReachRolloutMode); mode != service.ReachRolloutWhitelist {
		// gin.H 里存的是原类型（命名类型 ReachRolloutMode），拿 string 去比永远不等。
		t.Errorf("mode=%v，期望 %s", section["mode"], service.ReachRolloutWhitelist)
	}
	list, _ := section["whitelist"].([]string)
	if len(list) != 2 {
		t.Fatalf("whitelist=%v，期望原样给出 2 条（整份替换的写入口必须读得回上一份）", section["whitelist"])
	}
	if n, _ := section["whitelist_entries"].(int); n != 2 {
		t.Errorf("whitelist_entries=%v, want 2", section["whitelist_entries"])
	}
	// 这一节只回答"配成了什么"。"现在到底拦不拦"要看闸门那个端点，措辞不能越界。
	blob, _ := json.Marshal(section)
	if s := string(blob); strings.Contains(s, "拦") || strings.Contains(s, "生效") {
		t.Errorf("配置视图里不该断言生效与否（那是 /agent/tools/reach-gate 的口径）：%s", s)
	}
}

// 交付态的真实形状：一条业务路由都没挂。这条断言是"本卡没让开关悄悄冒充已生效"的证据。
func TestLTCConfigView_ZeroMountedSaysItOutLoud(t *testing.T) {
	view := ltcConfigView(service.DefaultLTCConfig(), middleware.LTCGuardedRoutes())
	if view["source"] != service.SourceDefault {
		t.Errorf("source=%v，默认读应是 %s", view["source"], service.SourceDefault)
	}
	guarded, _ := view["guarded_routes"].(map[string]int)
	if len(guarded) != len(service.LTCKnownStages) {
		t.Fatalf("guarded_routes 有 %d 键，期望六阶段各一键（缺键会被前端渲染成 undefined 而不是 0）", len(guarded))
	}
	hints := strings.Join(view["reading_hints"].([]string), "|")
	if !strings.Contains(hints, "没有任何路由") {
		t.Errorf("一条路由都没挂却没说出口：%s", hints)
	}
}

func TestLTCConfigView_DegradedReadKeepsFaultVisible(t *testing.T) {
	cfg := service.DefaultLTCConfig()
	cfg.Degraded = true
	cfg.DegradeReason = "读取 ltc.config 失败：connection reset"
	view := ltcConfigView(cfg, nil)
	if r, _ := view["degrade_reason"].(string); !strings.Contains(r, "connection reset") {
		t.Errorf("degrade_reason=%v，只喊坏了不给原因，运维还得再来问一次", view["degrade_reason"])
	}
	if on, _ := view["enabled"].(bool); on {
		t.Error("降级读把 enabled 渲染成 true")
	}
	status, _ := view["stage_status"].([]gin.H)
	for _, row := range status {
		if row["reason"] != service.LTCReasonDegraded {
			t.Errorf("阶段 %v 的 reason=%v，degraded 时六档都该是 %s", row["stage"], row["reason"], service.LTCReasonDegraded)
		}
	}
}

// ---- PUT：写侧语义 --------------------------------------------------------

const ltcValidPUTBody = `{"enabled":true,"stages_enabled":{"quote":true,"bill":false},"thresholds":{"lead_score":75,"confidence":0.7,"discount_percent":20,"win_probability":0.4}}`

func TestLTCConfigPutPersistsAndEchoesEffective(t *testing.T) {
	kv := newMemKV()
	useLTCGlobal(t, kv)
	w := serve(ltcAdminEngine("admin"), "PUT", "/api/manage/ltc/config", ltcValidPUTBody)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	_, data := decodeEnvelope(t, w)
	if persisted, _ := data["persisted"].(bool); !persisted {
		t.Error("persisted=false 却回了 200")
	}
	if got, _ := data["stages_on"].([]any); len(got) != 1 {
		t.Errorf("stages_on=%v，期望恰好一项", data["stages_on"])
	}
	eff, _ := data["effective"].(map[string]any)
	if eff == nil {
		t.Fatalf("响应没有 effective：运营点完保存看不见「现在生效的是哪一份」")
	}
	if th, _ := eff["thresholds"].(map[string]any); th["lead_score"] != float64(75) {
		t.Errorf("effective.thresholds=%v，没反映刚写进去的那一份", eff["thresholds"])
	}

	stored, ok := kv.rows[service.LTCConfigKVKey]
	if !ok {
		t.Fatal("KV 里没有 ltc.config 这一行")
	}
	back, err := service.ParseLTCConfig([]byte(stored))
	if err != nil {
		t.Fatalf("存进去的形状读不回来：%v\n%s", err, stored)
	}
	if !back.StageEnabled(service.LTCStageQuote) || back.StageEnabled(service.LTCStageBill) {
		t.Errorf("存回来的开关不对：%s", stored)
	}
	if strings.Contains(stored, "degraded") || strings.Contains(stored, `"source"`) {
		t.Errorf("读路径元信息被写进库里：%s", stored)
	}
}

func TestLTCConfigPutRejectsIllegalBodyWithoutTouchingStore(t *testing.T) {
	cases := []struct{ name, body, wantMention string }{
		{"阶段名拼错", `{"enabled":true,"stages_enabled":{"colecction":true},"thresholds":{"lead_score":70,"confidence":0.8,"discount_percent":10,"win_probability":0.5}}`, "colecction"},
		{"合并阈值", `{"enabled":true,"stages_enabled":{"quote":true},"thresholds":{"score":90,"lead_score":70,"confidence":0.8,"discount_percent":10,"win_probability":0.5}}`, "score"},
		{"阈值越界", `{"enabled":true,"stages_enabled":{"quote":true},"thresholds":{"lead_score":700,"confidence":0.8,"discount_percent":10,"win_probability":0.5}}`, "lead_score"},
		{"阈值给 0 拆闸", `{"enabled":true,"stages_enabled":{"quote":true},"thresholds":{"lead_score":0,"confidence":0.8,"discount_percent":10,"win_probability":0.5}}`, "lead_score"},
		{"不是 JSON", `enabled=true`, "JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kv := newMemKV()
			useLTCGlobal(t, kv)
			w := serve(ltcAdminEngine("admin"), "PUT", "/api/manage/ltc/config", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status=%d，期望 400（body=%s）", w.Code, w.Body.String())
			}
			body, _ := decodeEnvelope(t, w)
			msg, _ := body["message"].(string)
			if !strings.Contains(msg, tc.wantMention) {
				t.Errorf("message=%q，没点名 %q", msg, tc.wantMention)
			}
			if !strings.Contains(msg, "未保存") {
				t.Errorf("message=%q，没先说清「这次没写进去」", msg)
			}
			if len(kv.rows) != 0 || kv.writes != 0 {
				t.Errorf("被拒的请求仍然触达了写侧：rows=%d writes=%d", len(kv.rows), kv.writes)
			}
		})
	}
}

// 存储写不进去 ≠ 运营填错了：前者 503、后者 400，两句话不能共用一个红字。
func TestLTCConfigPutStoreFailureIs503Not400(t *testing.T) {
	kv := newMemKV()
	kv.upsertErr = errors.New("dial tcp: connection refused")
	useLTCGlobal(t, kv)

	w := serve(ltcAdminEngine("admin"), "PUT", "/api/manage/ltc/config", ltcValidPUTBody)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d，期望 503（body=%s）", w.Code, w.Body.String())
	}
	body, _ := decodeEnvelope(t, w)
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, "当前生效的仍是改动前那一份") {
		t.Errorf("message=%q，没说清「改动到底生没生效」——运营据此会再点一次", msg)
	}
}

func TestLTCConfigPutOversizedBodyIs413(t *testing.T) {
	kv := newMemKV()
	useLTCGlobal(t, kv)

	pad := strings.Repeat("x", service.LTCConfigMaxBytes)
	body := `{"enabled":true,"stages_enabled":{"quote":true},"note":"` + pad +
		`","thresholds":{"lead_score":70,"confidence":0.8,"discount_percent":10,"win_probability":0.5}}`
	w := serve(ltcAdminEngine("admin"), "PUT", "/api/manage/ltc/config", body)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status=%d，期望 413（body=%s）", w.Code, w.Body.String())
	}
	if kv.writes != 0 {
		t.Error("超限请求仍然触达了写侧")
	}
}

// ---- 端到端：KV → 服务 → 闸门 → HTTP --------------------------------------

// AC① 在这一条里才是完整的：不是"闸门单元测试绿"，而是
// "库里那份没打开 ⇒ 业务路由 409 且 handler 一次没跑；把库里那份打开 ⇒ 同一条路由 200；
// 关回去 ⇒ 不重启也立即停住"。
func TestLTCGate_EndToEndFollowsStoredConfig(t *testing.T) {
	useLTCGlobal(t, newMemKV())

	sendRan := 0
	engine := gin.New()
	engine.POST("/api/ltc/outreach/send", middleware.LTCStageGate(service.LTCStageOutreach),
		func(c *gin.Context) {
			sendRan++
			c.String(http.StatusOK, "外发已发生")
		})
	collectRan := 0
	engine.POST("/api/ltc/collection/run", middleware.LTCStageGate(service.LTCStageCollection),
		func(c *gin.Context) {
			collectRan++
			c.String(http.StatusOK, "ok")
		})

	w := serve(engine, "POST", "/api/ltc/outreach/send", "")
	if w.Code != http.StatusConflict || sendRan != 0 {
		t.Fatalf("开箱状态放行了业务路由：status=%d ran=%d body=%s", w.Code, sendRan, w.Body.String())
	}
	body, data := decodeEnvelope(t, w)
	if data["reason"] != service.LTCReasonMasterOff {
		t.Errorf("reason=%v，期望 %s", data["reason"], service.LTCReasonMasterOff)
	}
	// 域内码要一路穿过真实 HTTP 栈还在：只在闸门单元测试里断过，
	// 不等于调用方收到的是它——中间任何一层换成按状态码折算法都会把它抹掉。
	if code, _ := body["code"].(string); code != string(utils.ErrorCodeLTCStageDisabled) {
		t.Errorf("code=%v，期望 %s", body["code"], utils.ErrorCodeLTCStageDisabled)
	}

	putBody := func(stages string) {
		t.Helper()
		admin := ltcAdminEngine("admin")
		res := serve(admin, "PUT", "/api/manage/ltc/config",
			fmt.Sprintf(`{"enabled":true,"stages_enabled":%s,"thresholds":{"lead_score":70,"confidence":0.8,"discount_percent":10,"win_probability":0.5}}`, stages))
		if res.Code != http.StatusOK {
			t.Fatalf("PUT 失败：status=%d body=%s", res.Code, res.Body.String())
		}
	}

	putBody(`{"outreach":true}`)
	if w2 := serve(engine, "POST", "/api/ltc/outreach/send", ""); w2.Code != http.StatusOK || sendRan != 1 {
		t.Fatalf("开关已打开却没放行：status=%d ran=%d", w2.Code, sendRan)
	}
	// 只开 outreach：collection 仍拦着，分阶段开关在 HTTP 层也是各管各的。
	if w3 := serve(engine, "POST", "/api/ltc/collection/run", ""); w3.Code != http.StatusConflict || collectRan != 0 {
		t.Errorf("只开 outreach 时 collection 被放行：status=%d ran=%d", w3.Code, collectRan)
	}

	// 关回总开关：同一个进程不重启也要立即停住（写侧失效了自己的缓存）。
	masterOff := serve(ltcAdminEngine("admin"), "PUT", "/api/manage/ltc/config",
		`{"enabled":false,"stages_enabled":{"outreach":true},"thresholds":{"lead_score":70,"confidence":0.8,"discount_percent":10,"win_probability":0.5}}`)
	if masterOff.Code != http.StatusOK {
		t.Fatalf("关回去这一步就失败：status=%d body=%s", masterOff.Code, masterOff.Body.String())
	}
	if w4 := serve(engine, "POST", "/api/ltc/outreach/send", ""); w4.Code != http.StatusConflict || sendRan != 1 {
		t.Errorf("关回开关后仍在放行：status=%d ran=%d", w4.Code, sendRan)
	}
}

// 管理端响应里的两个数必须与服务侧同源：前端拿它们做约束，写歪一次就是两头各说各话。
func TestLTCConfigView_ExposesCacheAndSizeContract(t *testing.T) {
	useLTCGlobal(t, newMemKV())
	w := serve(ltcAdminEngine("admin"), "GET", "/api/manage/ltc/config", "")
	_, data := decodeEnvelope(t, w)
	if got, _ := data["cache_ttl_seconds"].(float64); got != service.LTCConfigCacheTTL.Seconds() {
		t.Errorf("cache_ttl_seconds=%v，期望 %v", data["cache_ttl_seconds"], service.LTCConfigCacheTTL.Seconds())
	}
	if got, _ := data["max_bytes"].(float64); got != service.LTCConfigMaxBytes {
		t.Errorf("max_bytes=%v，期望 %d", data["max_bytes"], service.LTCConfigMaxBytes)
	}
	if ranges, _ := data["threshold_ranges"].(map[string]any); len(ranges) != 4 {
		t.Errorf("threshold_ranges=%v，期望四项量程都给出（前端按它渲染输入边界）", data["threshold_ranges"])
	}
	// 同一批数字的机器可读那份：视图里那些 el-input-number 的 min/max/zero_legal 只能来自这里。
	bounds, _ := data["threshold_bounds"].(map[string]any)
	if len(bounds) != 4 {
		t.Fatalf("threshold_bounds=%v，期望四项", data["threshold_bounds"])
	}
	for _, key := range service.LTCKnownThresholdKeys {
		b, ok := bounds[key].(map[string]any)
		if !ok {
			t.Errorf("threshold_bounds 缺 %s 这一项", key)
			continue
		}
		if _, hasMin := b["min"]; !hasMin {
			t.Errorf("%s 的边界没有 min：%v", key, b)
		}
		if _, hasMax := b["max"]; !hasMax {
			t.Errorf("%s 的边界没有 max：%v", key, b)
		}
		zero, hasZero := b["zero_legal"].(bool)
		if !hasZero {
			t.Errorf("%s 的边界没有 bool zero_legal（前端无法判断 0 要不要标红）：%v", key, b)
			continue
		}
		// 这一条把 HTTP 侧与服务侧绑成同一份事实：只有 discount_percent 的 0 是合法的。
		wantZero := key == "discount_percent"
		if zero != wantZero {
			t.Errorf("%s zero_legal=%v，期望 %v", key, zero, wantZero)
		}
	}
}
