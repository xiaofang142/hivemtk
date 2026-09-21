package lifecycle

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

// ── 三个依赖的替身 ──────────────────────────────────────────────────────────
//
// 本包的三个依赖都是单方法接口，替身即真实现（不是 mock 框架造的行为）：
// 断言的是"传出去了什么"，不是"替身被调了几次"之外的东西。

type fakeConversation struct {
	calls    int
	lastReq  *dto.SalesRequest
	reply    string
	transfer bool
}

func (f *fakeConversation) HandleWithAgent(_ context.Context, req *dto.SalesRequest, _ *dto.AgentContext) (*dto.SalesResponse, error) {
	f.calls++
	f.lastReq = req
	return &dto.SalesResponse{Reply: f.reply, TransferredToHuman: f.transfer, TransferReason: "情绪升级"}, nil
}

type fakeSOPExecutor struct {
	calls int
	last  *dto.ExecuteRequest
	exec  *model.SOPExecution
	err   error
}

func (f *fakeSOPExecutor) Execute(_ context.Context, req *dto.ExecuteRequest) (*model.SOPExecution, error) {
	f.calls++
	f.last = req
	if f.err != nil {
		return nil, f.err
	}
	if f.exec != nil {
		return f.exec, nil
	}
	return &model.SOPExecution{ID: 99, Status: "running"}, nil
}

type fakeCustomers struct {
	row *model.Customer
	err error
}

func (f *fakeCustomers) GetByID(context.Context, string) (*model.Customer, error) {
	return f.row, f.err
}

func activeAgentCtx(sopIDs ...string) *dto.AgentContext {
	return &dto.AgentContext{AgentID: 7, AgentCode: "Act-A", AgentMode: "active", SOPIDs: sopIDs}
}

func TestActiveRunOrchestratesAgentSOP(t *testing.T) {
	exec := &fakeSOPExecutor{}
	sel := NewActiveAgentLifecycle(exec, &fakeCustomers{row: &model.Customer{ID: "c-1", UnifiedID: "one-1"}})

	res, err := sel.Run(context.Background(), activeAgentCtx("17"), &LifecycleRequest{CustomerID: "c-1"})
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if exec.calls != 1 {
		t.Fatalf("SOP 引擎调用数 = %d, want 1", exec.calls)
	}
	if exec.last.SOPID != 17 || exec.last.CustomerID != "c-1" {
		t.Errorf("传给 SOP 的对象错了: sop=%d customer=%s", exec.last.SOPID, exec.last.CustomerID)
	}
	if got := exec.last.Input["_trigger"]; got != "active_lifecycle" {
		t.Errorf(`Input["_trigger"] = %v, want "active_lifecycle"`, got)
	}
	if got := exec.last.Input[inputKeyOneID]; got != "one-1" {
		t.Errorf("归因 OneID 没进名单: %v", got)
	}
	if !strings.HasPrefix(exec.last.SessionID, "active-7-") {
		t.Errorf("SessionID = %q, 应以 \"active-<agent_id>-\" 开头", exec.last.SessionID)
	}
	if res.Mode != "active" || res.ExecutionID != 99 || res.OneID != "one-1" || res.StopReason != "running" {
		t.Errorf("结果不如预期: %+v", res)
	}
}

// TestActiveSessionKeyStaysWithinColumn 钉住合成会话键的**上界**：
// `sop_executions.session_id` 是 varchar(120)（model/ai_sales_champion.go:142），
// 而键里若混进 `agent_code`（varchar(64)）与 `customers.id`（varchar(36)），
// 最坏 7+64+1+36=108 —— 今天装得下，但那 12 字节余量押在**另外两张表的列宽**上，
// 谁把 agent_code 放宽到 128，这里就从"可读的键"变成一次 INSERT 报错。
// 改用 agent_id（数字，上界 20 位）后余量不再随那两列漂。
func TestActiveSessionKeyStaysWithinColumn(t *testing.T) {
	customerID := strings.Repeat("c", 36)
	exec := &fakeSOPExecutor{}
	sel := NewActiveAgentLifecycle(exec, &fakeCustomers{row: &model.Customer{ID: customerID, UnifiedID: "one-1"}})
	agentCtx := activeAgentCtx("17")
	agentCtx.AgentCode = strings.Repeat("A", 64) // 最宽的 agent_code 也不许把键顶出去

	if _, err := sel.Run(context.Background(), agentCtx, &LifecycleRequest{CustomerID: customerID}); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if n := len(exec.last.SessionID); n > 120 {
		t.Errorf("SessionID 长 %d, 超过 varchar(120)：PG 会在 INSERT 时直接拒绝", n)
	}
	if strings.Contains(exec.last.SessionID, agentCtx.AgentCode) {
		t.Errorf("SessionID 仍把 agent_code 拼了进去，宽度上界押在别的表上: %q", exec.last.SessionID)
	}
}

// 选材：主动入口必须带着一个明确的客户。没有客户就等于"对全体跑一轮"，
// 那是把误伤半径从一个人放大到全库，本卡刻意不做。
func TestActiveRequiresExplicitCustomer(t *testing.T) {
	exec := &fakeSOPExecutor{}
	sel := NewActiveAgentLifecycle(exec, &fakeCustomers{row: &model.Customer{UnifiedID: "one-1"}})

	_, err := sel.Run(context.Background(), activeAgentCtx("17"), &LifecycleRequest{})
	if err == nil || !strings.Contains(err.Error(), "客户") {
		t.Fatalf("无客户应报错并点名客户, got %v", err)
	}
	if exec.calls != 0 {
		t.Errorf("被拒的入口仍去跑了 SOP：%d 次", exec.calls)
	}
}

// 决策：智能体没挂任何可解析的 SOP 时，只能报错。
// 这一条红的是"悄悄当成没事发生"：返回成功 + 零执行，调用方读起来像"跑了，没结果"。
func TestActiveWithoutSOPRefuses(t *testing.T) {
	exec := &fakeSOPExecutor{}
	sel := NewActiveAgentLifecycle(exec, &fakeCustomers{row: &model.Customer{UnifiedID: "one-1"}})

	for name, ctx := range map[string]*dto.AgentContext{
		"空 SOP 列表":     activeAgentCtx(),
		"SOP ID 全不可解析": activeAgentCtx("abc", ""),
		"nil 上下文":      nil,
	} {
		_, err := sel.Run(context.Background(), ctx, &LifecycleRequest{CustomerID: "c-1"})
		if err == nil {
			t.Errorf("%s: 应报错, got nil", name)
			continue
		}
		if !strings.Contains(err.Error(), "SOP") {
			t.Errorf("%s: 红因没点名 SOP: %v", name, err)
		}
	}
	if exec.calls != 0 {
		t.Errorf("三种坏输入合计跑了 %d 次 SOP", exec.calls)
	}
}

// SOPIDs 是 text[]，运营手填就可能混进非数字项；取第一个可解析的，而不是整批作废。
func TestActivePicksFirstParsableSOP(t *testing.T) {
	exec := &fakeSOPExecutor{}
	sel := NewActiveAgentLifecycle(exec, &fakeCustomers{row: &model.Customer{UnifiedID: "one-1"}})

	if _, err := sel.Run(context.Background(), activeAgentCtx("abc", "17", "23"), &LifecycleRequest{CustomerID: "c-1"}); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if exec.last.SOPID != 17 {
		t.Errorf("SOPID = %d, want 17（第一个可解析项，不是最后一个 23）", exec.last.SOPID)
	}
}

// 归因前置：客户行查不到（repo 的 not-found 契约是 (nil, nil)）⇒ 不开工。
// 放行就等于建一条没有归因对象的执行，事后既查不到发给了谁、也补不回来。
func TestActiveMissingCustomerRowRefuses(t *testing.T) {
	exec := &fakeSOPExecutor{}
	sel := NewActiveAgentLifecycle(exec, &fakeCustomers{row: nil, err: nil})

	_, err := sel.Run(context.Background(), activeAgentCtx("17"), &LifecycleRequest{CustomerID: "c-404"})
	if err == nil {
		t.Fatal("客户行不存在时应报错, got nil")
	}
	if exec.calls != 0 {
		t.Errorf("查不到客户仍建了执行：%d 次", exec.calls)
	}
}

// 查库报错必须上抛，不能塌成"这个客户没有 OneID"。
func TestActiveCustomerQueryFailurePropagates(t *testing.T) {
	exec := &fakeSOPExecutor{}
	sel := NewActiveAgentLifecycle(exec, &fakeCustomers{err: errors.New("boom")})

	if _, err := sel.Run(context.Background(), activeAgentCtx("17"), &LifecycleRequest{CustomerID: "c-1"}); err == nil {
		t.Fatal("查库失败应上抛, got nil")
	}
	if exec.calls != 0 {
		t.Errorf("查库失败还去建执行了：%d 次", exec.calls)
	}
}

// OneID 允许为空（历史客户行没跑过归一），此时照常开工，但**不往 Input 里塞空串**——
// 空串会被下游当成一个合法的归因键，比缺键更难查。
func TestActiveEmptyOneIDOmitsKey(t *testing.T) {
	exec := &fakeSOPExecutor{}
	sel := NewActiveAgentLifecycle(exec, &fakeCustomers{row: &model.Customer{ID: "c-1"}})

	res, err := sel.Run(context.Background(), activeAgentCtx("17"), &LifecycleRequest{CustomerID: "c-1"})
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if exec.calls != 1 {
		t.Fatalf("无 OneID 也应照常开工, calls=%d", exec.calls)
	}
	if _, ok := exec.last.Input[inputKeyOneID]; ok {
		t.Errorf("空 OneID 不该写进 Input: %v", exec.last.Input[inputKeyOneID])
	}
	if res.OneID != "" {
		t.Errorf("res.OneID = %q, want 空", res.OneID)
	}
}

// SOP 引擎自己的错误原样上抛：本层不解释、不重试、不降级成"会话式回复"。
func TestActivePropagatesExecutionError(t *testing.T) {
	sel := NewActiveAgentLifecycle(&fakeSOPExecutor{err: errors.New("sop is not active")},
		&fakeCustomers{row: &model.Customer{UnifiedID: "one-1"}})

	_, err := sel.Run(context.Background(), activeAgentCtx("17"), &LifecycleRequest{CustomerID: "c-1"})
	if err == nil || !strings.Contains(err.Error(), "sop is not active") {
		t.Fatalf("应原样上抛 SOP 引擎的错误, got %v", err)
	}
}

func TestPassiveRunDelegatesConversation(t *testing.T) {
	runner := &fakeConversation{reply: "在的"}
	lc := NewPassiveAgentLifecycle(runner)

	res, err := lc.Run(context.Background(), &dto.AgentContext{AgentID: 3, AgentMode: "passive"},
		&LifecycleRequest{CustomerID: "c-9", Content: "多少钱", Channel: "web"})
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("会话引擎调用数 = %d, want 1", runner.calls)
	}
	if runner.lastReq.CustomerID != "c-9" || runner.lastReq.UserMessage != "多少钱" {
		t.Errorf("传给会话引擎的请求错了: %+v", runner.lastReq)
	}
	if res.Mode != "passive" || res.ReplyContent != "在的" {
		t.Errorf("结果不如预期: %+v", res)
	}
}

// 被动模式由入站消息触发：没有消息就没有事做。报错而不是拿空串去跑一轮 LLM。
func TestPassiveWithoutContentRefuses(t *testing.T) {
	runner := &fakeConversation{}
	lc := NewPassiveAgentLifecycle(runner)

	if _, err := lc.Run(context.Background(), &dto.AgentContext{AgentID: 3}, &LifecycleRequest{CustomerID: "c-9"}); err == nil {
		t.Fatal("无消息时应报错, got nil")
	}
	if runner.calls != 0 {
		t.Errorf("被拒的入口仍跑了会话引擎：%d 次", runner.calls)
	}
}

// 依赖未装配（app 层拿不到 DB / 拿不到引擎时构造出的 nil）必须在 Run 时红，
// 不能在构造时 panic，也不能静默返回空结果。
func TestUnassembledDepsFailAtRun(t *testing.T) {
	if _, err := NewActiveAgentLifecycle(nil, nil).Run(context.Background(), activeAgentCtx("17"), &LifecycleRequest{CustomerID: "c-1"}); err == nil {
		t.Error("Active 依赖未装配时应报错")
	}
	if _, err := NewPassiveAgentLifecycle(nil).Run(context.Background(), &dto.AgentContext{AgentID: 3}, &LifecycleRequest{CustomerID: "c-1", Content: "hi"}); err == nil {
		t.Error("Passive 依赖未装配时应报错")
	}
}

func TestModesAreStable(t *testing.T) {
	if NewActiveAgentLifecycle(nil, nil).Mode() != string(model.AgentModeActive) {
		t.Error("Active.Mode() 应为 model.AgentModeActive 的字面值")
	}
	if NewPassiveAgentLifecycle(nil).Mode() != string(model.AgentModePassive) {
		t.Error("Passive.Mode() 应为 model.AgentModePassive 的字面值")
	}
}

// TestActiveNeverImportsConversationEngine 是 AC②（"Active 不新建自由 Agent 循环，编排走 SOP"）
// 的**静态**锁：本包一旦 import 会话引擎所在的包（`internal/service` 或 `agent/runtime`），
// 下一个人在 Active.Run 里直接调 LLM 循环就不需要跨任何边界了 —— 那正是 C7 否掉的路。
//
// 为什么用读文件而不是编译期断言：编译期断言只能证明"当前这版没调"，证明不了"加 import
// 会被发现"。这条用例是跑在磁盘上的真实 import 集合上的，红因指到文件名与那一行。
func TestActiveNeverImportsConversationEngine(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("解析本包失败: %v", err)
	}
	banned := []string{"/internal/service", "/agent/runtime", "/agent/bridge"}
	for name, pkg := range pkgs {
		for path, file := range pkg.Files {
			for _, imp := range file.Imports {
				got, _ := strconv.Unquote(imp.Path.Value)
				for _, b := range banned {
					if strings.HasSuffix(got, b) {
						t.Errorf("包 %s（%s）import 了会话引擎所在的包 %q —— 主动模式只许编排 SOP（C7）",
							name, filepath.Base(path), got)
					}
				}
			}
		}
	}
}
