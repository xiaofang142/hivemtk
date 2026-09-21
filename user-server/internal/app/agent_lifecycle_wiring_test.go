package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/aiagent/agent/lifecycle"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"

	"github.com/gin-gonic/gin"
)

// 本文件只验"按模式分派"这一件事，因此两个生命周期都用最省的替身：
// 记下自己被叫到、回一个可辨认的 Mode。请求内容怎么映射到会话引擎/SOP，
// 在 lifecycle 包自己的用例里验（active_test.go）。

type recordingLifecycle struct {
	name  string
	calls int
	req   *lifecycle.LifecycleRequest
	err   error
}

func (r *recordingLifecycle) Mode() string { return r.name }

func (r *recordingLifecycle) Run(_ context.Context, _ *dto.AgentContext, req *lifecycle.LifecycleRequest) (*lifecycle.LifecycleResult, error) {
	r.calls++
	r.req = req
	if r.err != nil {
		return nil, r.err
	}
	return &lifecycle.LifecycleResult{Mode: r.name, StopReason: "completed"}, nil
}

type fakeAgentLoader struct {
	ctx   *dto.AgentContext
	err   error
	calls int
	ids   []uint
}

func (f *fakeAgentLoader) LoadContext(_ context.Context, agentID uint) (*dto.AgentContext, error) {
	f.calls++
	f.ids = append(f.ids, agentID)
	return f.ctx, f.err
}

// newTestRuntime 把替身装进真实构造器，返回被动/主动两个记录器。
// 名字取 model 常量的字面值：一旦有人改这两个枚举或把两串常量换个位，本文件先红。
func newTestRuntime(loader AgentContextLoader) (*AgentLifecycleRuntime, *recordingLifecycle, *recordingLifecycle) {
	passive := &recordingLifecycle{name: string(model.AgentModePassive)}
	active := &recordingLifecycle{name: string(model.AgentModeActive)}
	return NewAgentLifecycleRuntime(loader, passive, active), passive, active
}

// TestAgentLifecycleDispatchByMode 兑现 AC①：agent_mode='active' 才走 Active，
// 其余一切（passive / 空串 / 历史脏值 / 拼错的模式）都回退 Passive。
//
// 回退方向刻意是"回退到被动"：被动只答一条消息，主动会对外发消息。
// 反过来（未知模式走 Active）等于一个字段填错就对外群发。
func TestAgentLifecycleDispatchByMode(t *testing.T) {
	cases := []struct {
		name string
		mode string
		want string
	}{
		{"active 走主动", "active", "active"},
		{"passive 走被动", "passive", "passive"},
		{"空串走被动", "", "passive"},
		{"未知模式走被动", "hybrid", "passive"},
		{"大小写与空白先归一", "  Active  ", "active"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt, passive, active := newTestRuntime(&fakeAgentLoader{})

			got := rt.LifecycleFor(tc.mode)
			if got == nil || got.Mode() != tc.want {
				t.Fatalf("LifecycleFor(%q) = %v, want %s", tc.mode, got, tc.want)
			}
			if _, err := got.Run(context.Background(), &dto.AgentContext{AgentMode: tc.mode}, &lifecycle.LifecycleRequest{}); err != nil {
				t.Fatalf("Run 失败: %v", err)
			}
			if tc.want == "active" && active.calls != 1 {
				t.Errorf("应叫到 Active: active=%d passive=%d", active.calls, passive.calls)
			}
			if tc.want == "passive" && passive.calls != 1 {
				t.Errorf("应叫到 Passive: active=%d passive=%d", active.calls, passive.calls)
			}
		})
	}
}

// Active 没装配（app 启动时拿不到 SOP 出口或客户源）时，active 模式的智能体必须回退到
// Passive，而不是 panic、也不是"返回一个空结果当成功了"。
func TestActiveModeFallsBackWhenActiveNotAssembled(t *testing.T) {
	loader := &fakeAgentLoader{ctx: &dto.AgentContext{AgentID: 1, AgentMode: "active"}}
	passive := &recordingLifecycle{name: string(model.AgentModePassive)}
	rt := NewAgentLifecycleRuntime(loader, passive, nil) // 主动依赖没配齐

	if got := rt.LifecycleFor("active"); got == nil || got.Mode() != "passive" {
		t.Fatalf("未装配 Active 时应回退 Passive, got %v", got)
	}
	if _, err := rt.Run(context.Background(), 1, &lifecycle.LifecycleRequest{CustomerID: "c-1"}); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if passive.calls != 1 {
		t.Errorf("Passive 调用数 = %d, want 1", passive.calls)
	}
}

// Run 是"读模式 → 分派"的缝合处：模式来自上下文（W-4 修的就是这一路读不出模式），
// 结果里的 Mode 必须回显实际走了哪条 —— 否则运营分不清"真跑了主动"与"悄悄回退了"。
func TestRunByIDDispatchesOnLoadedMode(t *testing.T) {
	for _, mode := range []string{"active", "passive"} {
		passive := &recordingLifecycle{name: "passive"}
		active := &recordingLifecycle{name: "active"}
		loader := &fakeAgentLoader{ctx: &dto.AgentContext{AgentID: 7, AgentMode: mode}}
		rt := NewAgentLifecycleRuntime(loader, passive, active)

		res, err := rt.Run(context.Background(), 7, &lifecycle.LifecycleRequest{CustomerID: "c-1", Content: "在吗"})
		if err != nil {
			t.Fatalf("mode=%s Run 失败: %v", mode, err)
		}
		if res.Mode != mode {
			t.Errorf("mode=%s 回显 = %q（没回显实际走的哪条，读的人分不清真跑了还是回退了）", mode, res.Mode)
		}
		if loader.calls != 1 || loader.ids[0] != 7 {
			t.Errorf("上下文加载 %d 次 %v, want 1 次 [7]", loader.calls, loader.ids)
		}
		want := passive
		if mode == "active" {
			want = active
		}
		if want.calls != 1 || want.req.CustomerID != "c-1" {
			t.Errorf("mode=%s 分派错了: passive=%d active=%d", mode, passive.calls, active.calls)
		}
	}
}

// 上下文读不出来（DB 报错）必须上抛，且一个生命周期都不许叫。
func TestRunByIDPropagatesLoadFailure(t *testing.T) {
	rt, passive, active := newTestRuntime(&fakeAgentLoader{err: errors.New("db down")})

	if _, err := rt.Run(context.Background(), 9, &lifecycle.LifecycleRequest{CustomerID: "c-1"}); err == nil {
		t.Fatal("加载失败应上抛, got nil")
	}
	if passive.calls+active.calls != 0 {
		t.Errorf("读不到模式仍跑了生命周期: passive=%d active=%d", passive.calls, active.calls)
	}
}

// LoadContext 对"不存在/已禁用"的智能体回的是 (nil, nil)（仓层契约），
// 此时既没有模式可分派、也没有上下文可传给会话引擎 —— 必须明确拒绝，
// 不能拿 nil 上下文去跑 Passive（那会跑出一轮没有智能体人设的回复）。
func TestRunByIDRefusesNilContext(t *testing.T) {
	rt, passive, active := newTestRuntime(&fakeAgentLoader{ctx: nil})

	_, err := rt.Run(context.Background(), 3, &lifecycle.LifecycleRequest{CustomerID: "c-1", Content: "hi"})
	if err == nil {
		t.Fatal("上下文为 nil 时应拒绝, got nil")
	}
	if passive.calls+active.calls != 0 {
		t.Errorf("nil 上下文仍跑了生命周期: passive=%d active=%d", passive.calls, active.calls)
	}
}

// 分派出来的生命周期自己报错时，错误要原样传出去（主动入口的每条拒绝都得让运营读得懂）。
func TestRunByIDPropagatesLifecycleError(t *testing.T) {
	bang := errors.New("active: 主动运行必须指定一个客户")
	rt, _, active := newTestRuntime(&fakeAgentLoader{ctx: &dto.AgentContext{AgentMode: "active"}})
	active.err = bang

	_, err := rt.Run(context.Background(), 7, &lifecycle.LifecycleRequest{})
	if err == nil || !errors.Is(err, bang) {
		t.Fatalf("应原样上抛生命周期错误, got %v", err)
	}
}

// TestAgentLifecycleRoutesRegistered 钉住"装配了但没人登记路由"这一格：
// 本卡要修的正是"双模式零调用方"，路由没挂上就等于把未接线从包层搬到了 app 层。
func TestAgentLifecycleRoutesRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rt, _, _ := newTestRuntime(&fakeAgentLoader{ctx: &dto.AgentContext{AgentID: 7, AgentMode: "passive"}})
	useAgentLifecycleRuntime(t, rt)

	engine := gin.New()
	SetupAgentLifecycleRoutes(engine.Group("/api"))

	var found bool
	for _, r := range engine.Routes() {
		if r.Method == http.MethodPost && r.Path == "/api/agent/lifecycle/run" {
			found = true
		}
	}
	if !found {
		t.Fatalf("POST /api/agent/lifecycle/run 未登记，实际路由：%v", engine.Routes())
	}
}

// useAgentLifecycleRuntime 装上全局运行时并在用例结束时还原 ——
// app 包的其余用例（以及同包后续新增的）都读到同一个全局，不还原就是跨用例污染。
func useAgentLifecycleRuntime(t *testing.T, rt *AgentLifecycleRuntime) {
	t.Helper()
	SetAgentLifecycleRuntime(rt)
	t.Cleanup(func() { SetAgentLifecycleRuntime(nil) })
}

func postAgentLifecycleRun(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	engine := gin.New()
	SetupAgentLifecycleRoutes(engine.Group("/api"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agent/lifecycle/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)
	return w
}

// 端到端一枪：HTTP 进来 → 读上下文拿模式 → 分派 → 响应里回显 mode。
// 这条用例是"主动模式第一次真有一个入口"的证据；没有它，本包仍然零调用方。
func TestAgentLifecycleRunEndpointDispatches(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, mode := range []string{"passive", "active"} {
		rt, passive, active := newTestRuntime(&fakeAgentLoader{ctx: &dto.AgentContext{AgentID: 7, AgentMode: mode}})
		useAgentLifecycleRuntime(t, rt)

		w := postAgentLifecycleRun(t, `{"agent_id":7,"customer_id":"c-1","content":"在吗"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("mode=%s HTTP %d: %s", mode, w.Code, w.Body.String())
		}
		var payload struct {
			Code int `json:"code"`
			Data struct {
				Mode string `json:"mode"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("响应不是 JSON: %v / %s", err, w.Body.String())
		}
		if payload.Data.Mode != mode {
			t.Errorf("响应 mode = %q, want %q（不回显就分不清真跑了主动还是悄悄回退）", payload.Data.Mode, mode)
		}
		want, other := passive, active
		if mode == "active" {
			want, other = active, passive
		}
		if want.calls != 1 || other.calls != 0 {
			t.Errorf("mode=%s 分派错了: passive=%d active=%d", mode, passive.calls, active.calls)
		}
	}
}

// 缺 agent_id / 缺 customer_id 的请求在进门时就被挡下，不出现"跑了但跑错对象"。
func TestAgentLifecycleRunEndpointRejectsBadBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rt, passive, active := newTestRuntime(&fakeAgentLoader{ctx: &dto.AgentContext{AgentID: 7, AgentMode: "active"}})
	useAgentLifecycleRuntime(t, rt)

	for _, body := range []string{`{"customer_id":"c-1"}`, `{"agent_id":7}`, `{}`, `not-json`} {
		w := postAgentLifecycleRun(t, body)
		if w.Code == http.StatusOK {
			t.Errorf("body=%q 不该回 200: %s", body, w.Body.String())
		}
	}
	if passive.calls+active.calls != 0 {
		t.Errorf("坏请求仍跑了生命周期: passive=%d active=%d", passive.calls, active.calls)
	}
}

// 运行时没装配（启动时拿不到会话引擎/SOP 出口）时，路由必须明说，
// 而不是回一个看起来成功的空结果 —— 与 human-task / opportunity 底座同一口径。
func TestAgentLifecycleRunEndpointReportsNotAssembled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	SetAgentLifecycleRuntime(nil)
	t.Cleanup(func() { SetAgentLifecycleRuntime(nil) })

	w := postAgentLifecycleRun(t, `{"agent_id":7,"customer_id":"c-1","content":"hi"}`)
	if w.Code == http.StatusOK {
		t.Fatalf("未装配时不该回 200: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "lifecycle") {
		t.Errorf("错误响应该点名是生命周期运行时没装配: %s", w.Body.String())
	}
}
