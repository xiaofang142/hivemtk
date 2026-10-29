package service

// workflow_egolite_node_test.go —— S-10 egolite_run 节点守卫用例。
//
// 钉的是四条纪律：未装配/缺归属人必须 fail-closed、参数非法不许下发、
// 同一节点重跑不重复起执行、只有"等一会儿能好"的失败才标 retryable。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
)

// fakeEgoLiteRunner 记录下发次数与归属人，用来验证"到底有没有真跑"。
type fakeEgoLiteRunner struct {
	calls  []egoLiteCall
	result *WorkflowEgoLiteExecution
	err    error
}

type egoLiteCall struct {
	userID uint
	req    WorkflowEgoLiteStartRequest
}

func (f *fakeEgoLiteRunner) Start(_ context.Context, userID uint, req WorkflowEgoLiteStartRequest) (*WorkflowEgoLiteExecution, error) {
	f.calls = append(f.calls, egoLiteCall{userID: userID, req: req})
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func newEgoLiteWctx(cfg model.JSONMap, input, ctxMap model.JSONMap) *WorkflowExecContext {
	if input == nil {
		input = model.JSONMap{}
	}
	if ctxMap == nil {
		ctxMap = model.JSONMap{}
	}
	return &WorkflowExecContext{
		Execution: &model.WorkflowExecution{
			WorkflowID:     "wf-egolite",
			Version:        1,
			TriggerPayload: input,
			Context:        ctxMap,
		},
		NodeID:     "n-ego",
		NodeType:   "egolite_run",
		NodeConfig: cfg,
		Input:      input,
		Context:    ctxMap,
	}
}

func withEgoLiteRunner(t *testing.T, r WorkflowEgoLiteRunner) {
	t.Helper()
	SetWorkflowEgoLiteRunner(r)
	t.Cleanup(func() { SetWorkflowEgoLiteRunner(nil) })
}

// 1. 未装配 ⇒ fail-closed（不能"看起来跑过了"）
func TestEgoLiteRunNode_UnassembledFailsClosed(t *testing.T) {
	SetWorkflowEgoLiteRunner(nil)
	res, err := (&EgoLiteRunNodeExecutor{}).Execute(context.Background(), newEgoLiteWctx(model.JSONMap{
		"origin": "draft", "flow_id": float64(3), "user_id": float64(9),
	}, nil, nil))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if res.Status != NodeStatusFailed {
		t.Fatalf("未装配时 Status=%s，必须是 failed", res.Status)
	}
	if !strings.Contains(res.ErrorMessage, "未装配") {
		t.Errorf("错误文案要能指认装配缺失，got=%q", res.ErrorMessage)
	}
}

// 2. 参数非法一律不下发（runner 调用次数=0 才是"没跑"的证据）
func TestEgoLiteRunNode_InvalidConfigNeverDispatches(t *testing.T) {
	cases := []struct {
		name string
		cfg  model.JSONMap
		msg  string
	}{
		{
			name: "origin 非 draft/market",
			cfg:  model.JSONMap{"origin": "production", "flow_id": float64(3), "user_id": float64(9)},
			msg:  "origin",
		},
		{
			name: "draft 缺 flow_id",
			cfg:  model.JSONMap{"origin": "draft", "user_id": float64(9)},
			msg:  "flow_id",
		},
		{
			name: "market 缺 asset_id/entry",
			cfg:  model.JSONMap{"origin": "market", "asset_id": "", "user_id": float64(9)},
			msg:  "asset_id",
		},
		{
			name: "confirm_policy 非法",
			cfg: model.JSONMap{"origin": "draft", "flow_id": float64(3), "user_id": float64(9),
				"confirm_policy": "always"},
			msg: "confirm_policy",
		},
		{
			name: "缺 user_id（节点/载荷/ctx 三处都没有）",
			cfg:  model.JSONMap{"origin": "draft", "flow_id": float64(3)},
			msg:  "user_id",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := &fakeEgoLiteRunner{result: &WorkflowEgoLiteExecution{ID: 1, Status: "created", ExecKey: "ego-1"}}
			withEgoLiteRunner(t, fake)
			res, err := (&EgoLiteRunNodeExecutor{}).Execute(context.Background(), newEgoLiteWctx(c.cfg, nil, nil))
			if err != nil {
				t.Fatalf("Execute err: %v", err)
			}
			if res.Status != NodeStatusFailed {
				t.Fatalf("Status=%s want=failed", res.Status)
			}
			if !strings.Contains(res.ErrorMessage, c.msg) {
				t.Errorf("错误文案要包含 %q，got=%q", c.msg, res.ErrorMessage)
			}
			if res.Retryable {
				t.Errorf("配置类失败不该重试")
			}
			if len(fake.calls) != 0 {
				t.Errorf("非法配置下发了 %d 次执行，必须 0 次", len(fake.calls))
			}
		})
	}
}

// 3. 归属人回落：节点配置 → 触发载荷 → 运行上下文
func TestEgoLiteRunNode_UserIDResolution(t *testing.T) {
	cases := []struct {
		name   string
		cfg    model.JSONMap
		input  model.JSONMap
		ctxMap model.JSONMap
		want   uint
	}{
		{
			name:  "节点配置优先",
			cfg:   model.JSONMap{"origin": "draft", "flow_id": float64(3), "user_id": float64(11)},
			input: model.JSONMap{"user_id": float64(22)}, want: 11,
		},
		{
			name:  "载荷回落（定时/Webhook 触发没有登录用户）",
			cfg:   model.JSONMap{"origin": "draft", "flow_id": float64(3)},
			input: model.JSONMap{"user_id": float64(22)}, want: 22,
		},
		{
			name:  "载荷里的字符串 user_id 也能认（JSON 反序列化常见形态）",
			cfg:   model.JSONMap{"origin": "draft", "flow_id": float64(3)},
			input: model.JSONMap{"user_id": "33"}, want: 33,
		},
		{
			name:   "上下文回落",
			cfg:    model.JSONMap{"origin": "draft", "flow_id": float64(3)},
			ctxMap: model.JSONMap{"user_id": float64(44)}, want: 44,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := &fakeEgoLiteRunner{result: &WorkflowEgoLiteExecution{ID: 7, Status: "dispatching", ExecKey: "ego-7"}}
			withEgoLiteRunner(t, fake)
			res, err := (&EgoLiteRunNodeExecutor{}).Execute(context.Background(), newEgoLiteWctx(c.cfg, c.input, c.ctxMap))
			if err != nil {
				t.Fatalf("Execute err: %v", err)
			}
			if res.Status != NodeStatusCompleted {
				t.Fatalf("Status=%s want=completed（err=%s）", res.Status, res.ErrorMessage)
			}
			if len(fake.calls) != 1 {
				t.Fatalf("下发次数=%d want=1", len(fake.calls))
			}
			if fake.calls[0].userID != c.want {
				t.Errorf("归属人=%d want=%d", fake.calls[0].userID, c.want)
			}
		})
	}
}

// 4. 成功路径：产物 + 副作用键 + 默认 confirm_policy=require + params 透传
func TestEgoLiteRunNode_CompletedCarriesExecutionIdentity(t *testing.T) {
	fake := &fakeEgoLiteRunner{result: &WorkflowEgoLiteExecution{ID: 42, Status: "dispatching", ExecKey: "ego-42"}}
	withEgoLiteRunner(t, fake)
	res, err := (&EgoLiteRunNodeExecutor{}).Execute(context.Background(), newEgoLiteWctx(model.JSONMap{
		"origin": "draft", "flow_id": float64(3), "user_id": float64(9),
		"params": map[string]any{"keyword": "蜂巢"}, "ctx": "ignored",
	}, nil, nil))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if res.Status != NodeStatusCompleted {
		t.Fatalf("Status=%s want=completed（err=%s）", res.Status, res.ErrorMessage)
	}
	if res.Output["egolite_execution_id"] != "42" {
		t.Errorf("执行号=%v want=42（下游 condition 要靠它判分支）", res.Output["egolite_execution_id"])
	}
	if res.Output["egolite_exec_key"] != "ego-42" {
		t.Errorf("exec_key=%v want=ego-42", res.Output["egolite_exec_key"])
	}
	if res.Output["egolite_status"] != "dispatching" {
		t.Errorf("status=%v want=dispatching", res.Output["egolite_status"])
	}
	if len(res.SideEffects) != 1 || res.SideEffects[0] != "wf_0_n-ego_egolite_run:42" {
		t.Errorf("副作用键=%v want=[wf_0_n-ego_egolite_run:42]（节点级：带 execID+nodeID）", res.SideEffects)
	}
	got := fake.calls[0].req
	if got.ConfirmPolicy != "require" {
		t.Errorf("confirm_policy 缺省必须是 require（外发动作不默认放行），got=%q", got.ConfirmPolicy)
	}
	if string(got.Params) != `{"keyword":"蜂巢"}` {
		t.Errorf("params 透传失真，got=%s", string(got.Params))
	}
}

// 5. 幂等：本节点已起过执行 ⇒ 复用，不再起第二次（判据是节点级副作用账）
func TestEgoLiteRunNode_ReusesExistingExecution(t *testing.T) {
	exec := &model.WorkflowExecution{ID: 77, WorkflowID: "wf-egolite", Context: model.JSONMap{}}
	exec.Context["_side_effects"] = []any{"wf_77_n-ego_egolite_run:41"}
	wctx := &WorkflowExecContext{
		Execution:  exec,
		NodeID:     "n-ego",
		NodeType:   "egolite_run",
		NodeConfig: model.JSONMap{"origin": "draft", "flow_id": float64(3), "user_id": float64(9)},
		Input:      model.JSONMap{},
		Context:    exec.Context,
	}
	fake := &fakeEgoLiteRunner{result: &WorkflowEgoLiteExecution{ID: 99, Status: "created", ExecKey: "ego-99"}}
	withEgoLiteRunner(t, fake)

	res, err := (&EgoLiteRunNodeExecutor{}).Execute(context.Background(), wctx)
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if res.Status != NodeStatusCompleted {
		t.Fatalf("Status=%s want=completed（err=%s）", res.Status, res.ErrorMessage)
	}
	if res.Output["egolite_execution_id"] != "41" {
		t.Errorf("复用执行号=%v want=41", res.Output["egolite_execution_id"])
	}
	if res.Output["egolite_reused"] != true {
		t.Errorf("复用要留痕（egolite_reused=true），got=%v", res.Output["egolite_reused"])
	}
	if len(fake.calls) != 0 {
		t.Errorf("复用路径又下发了 %d 次执行——重试会把一次真跑变成两次", len(fake.calls))
	}
}

// 5b. 两条 egolite_run 节点不串味：A 的执行号在 ctx 里，不能拦下 B 的下发
func TestEgoLiteRunNode_SiblingNodesDoNotShareExecution(t *testing.T) {
	exec := &model.WorkflowExecution{ID: 78, WorkflowID: "wf-egolite", Context: model.JSONMap{
		// A 节点的输出（全局合并键）与副作用账
		"egolite_execution_id": "41",
		"_side_effects":        []any{"wf_78_n-ego-a_egolite_run:41"},
	}}
	wctx := &WorkflowExecContext{
		Execution:  exec,
		NodeID:     "n-ego-b",
		NodeType:   "egolite_run",
		NodeConfig: model.JSONMap{"origin": "draft", "flow_id": float64(4), "user_id": float64(9)},
		Input:      model.JSONMap{},
		Context:    exec.Context,
	}
	fake := &fakeEgoLiteRunner{result: &WorkflowEgoLiteExecution{ID: 42, Status: "dispatching", ExecKey: "ego-42"}}
	withEgoLiteRunner(t, fake)

	res, err := (&EgoLiteRunNodeExecutor{}).Execute(context.Background(), wctx)
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if res.Status != NodeStatusCompleted {
		t.Fatalf("Status=%s want=completed（err=%s）", res.Status, res.ErrorMessage)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("B 节点下发次数=%d want=1（被 A 节点的执行号误判成已跑）", len(fake.calls))
	}
	if res.Output["egolite_execution_id"] != "42" {
		t.Errorf("B 的执行号=%v want=42", res.Output["egolite_execution_id"])
	}
	if len(res.SideEffects) != 1 || res.SideEffects[0] != "wf_78_n-ego-b_egolite_run:42" {
		t.Errorf("B 的副作用键=%v want=[wf_78_n-ego-b_egolite_run:42]", res.SideEffects)
	}
}

// 6. 失败分类：在途上限/超时/runner 未连接 ⇒ retryable；资产不可用 ⇒ 不重试
func TestEgoLiteRunNode_FailureClassification(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"在途上限", errors.New("在途执行已达上限，请等待当前执行结束或停止它"), true},
		{"硬超时", errors.New("egolite: 执行超时"), true},
		{"runner 未连接", errors.New("ego-runner 未连接：请确认本机已安装 ego lite 并运行 ego-runner"), true},
		{"资产不可用", errors.New("资产不在本实例目录内"), false},
		{"参数非法", errors.New("origin 必须为 draft 或 market"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := &fakeEgoLiteRunner{err: c.err}
			withEgoLiteRunner(t, fake)
			res, err := (&EgoLiteRunNodeExecutor{}).Execute(context.Background(), newEgoLiteWctx(model.JSONMap{
				"origin": "draft", "flow_id": float64(3), "user_id": float64(9),
			}, nil, nil))
			if err != nil {
				t.Fatalf("Execute err: %v", err)
			}
			if res.Status != NodeStatusFailed {
				t.Fatalf("Status=%s want=failed", res.Status)
			}
			if res.Retryable != c.retryable {
				t.Errorf("Retryable=%v want=%v（err=%v）", res.Retryable, c.retryable, c.err)
			}
			if !strings.Contains(res.ErrorMessage, "egolite_run") {
				t.Errorf("错误文案要带节点前缀便于定位，got=%q", res.ErrorMessage)
			}
		})
	}
}

// 7. 执行服务返回空执行 ⇒ 不能当作已跑
func TestEgoLiteRunNode_NilExecutionIsFailure(t *testing.T) {
	withEgoLiteRunner(t, &fakeEgoLiteRunner{result: nil})
	res, err := (&EgoLiteRunNodeExecutor{}).Execute(context.Background(), newEgoLiteWctx(model.JSONMap{
		"origin": "draft", "flow_id": float64(3), "user_id": float64(9),
	}, nil, nil))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if res.Status != NodeStatusFailed {
		t.Fatalf("Status=%s want=failed", res.Status)
	}
}

// 8. 执行器自述：节点类型与异步语义（runner 侧异步完成，节点下发即返回）
func TestEgoLiteRunNode_NodeTypeContract(t *testing.T) {
	e := &EgoLiteRunNodeExecutor{}
	if e.NodeType() != "egolite_run" {
		t.Errorf("NodeType=%s want=egolite_run（前端节点词表与之同源）", e.NodeType())
	}
	if e.IsAsync() {
		t.Errorf("IsAsync=true 会让 dispatcher 把节点挂在 waiting，但执行号已经拿到了")
	}
}
