package tooluse

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
)

// identity 只是本地凭据版本的模拟标识，执行体不发起网络请求。
type scopedCountingTool struct {
	BaseTool
	identity string
	calls    atomic.Int64
	execute  func(context.Context)
}

func (s *scopedCountingTool) Execute(ctx context.Context, _ map[string]any) (ToolResult, error) {
	s.calls.Add(1)
	if s.execute != nil {
		s.execute(ctx)
	}
	return SuccessResult(s.Name(), s.identity), nil
}

func newScopedCountingTool(name, identity string) *scopedCountingTool {
	return &scopedCountingTool{BaseTool: BaseTool{
		NameVal: name, CategoryVal: CategoryBusiness, RiskVal: RiskReadonly,
		ParamsVal: ToolParameters{Type: "object"},
	}, identity: identity}
}

func setupScopedExecutorTest(t *testing.T) (context.Context, context.Context) {
	t.Helper()
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	for _, id := range []uint{17001, 17002} {
		tenantRegsMu.Lock()
		previous := tenantRegs[id]
		delete(tenantRegs, id)
		tenantRegsMu.Unlock()
		t.Cleanup(func() {
			tenantRegsMu.Lock()
			defer tenantRegsMu.Unlock()
			if previous == nil {
				delete(tenantRegs, id)
			} else {
				tenantRegs[id] = previous
			}
		})
	}
	return tenant.WithTenantID(context.Background(), 17001), tenant.WithTenantID(context.Background(), 17002)
}

func registerScopedTool(t *testing.T, id uint, tool Tool) {
	t.Helper()
	if err := RegisterTenantTool(id, tool); err != nil {
		t.Fatalf("登记租户 %d 工具失败: %v", id, err)
	}
}

type scopedPermissionRecorder struct {
	mu     sync.Mutex
	name   string
	scopes []uint
}

func (s *scopedPermissionRecorder) Check(ctx context.Context, name string, tc *ToolContext) error {
	id, ok := tenant.FromContext(ctx)
	if !ok || (id != 17001 && id != 17002) || name != s.name || tc == nil || tc.AgentID != "cache-test" {
		return fmt.Errorf("权限检查未收到原请求身份: tenant=%d tool=%s", id, name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scopes = append(s.scopes, id)
	return nil
}

func expectScopedIdentity(t *testing.T, executor *ToolExecutor, ctx context.Context, name, identity string) {
	t.Helper()
	result := executor.Execute(ctx, ExecuteRequest{
		ToolName: name, ToolCtx: &ToolContext{AgentID: "cache-test"}, Args: map[string]any{"tenant_id": 99999},
	})
	if result.Err != nil || !result.Success || result.Data != identity {
		t.Fatalf("应实际执行 %s，实际 data=%v success=%v err=%v", identity, result.Data, result.Success, result.Err)
	}
}

func TestToolExecutor_ScopedHandlerIdentityAndAudit(t *testing.T) {
	ctxA, ctxB := setupScopedExecutorTest(t)
	const name = "scope-test.same-name"
	a, b := newScopedCountingTool(name, "tenant-a.v1"), newScopedCountingTool(name, "tenant-b.v1")
	registerScopedTool(t, 17001, a)
	registerScopedTool(t, 17002, b)
	audit := NewMemoryAuditLogger(20)
	permission := &scopedPermissionRecorder{name: name}
	executor := NewToolExecutor(NewToolRegistry(), ToolExecutorConfig{AuditLogger: audit, PermissionChecker: permission})
	for range 2 {
		expectScopedIdentity(t, executor, ctxA, name, a.identity)
		expectScopedIdentity(t, executor, ctxB, name, b.identity)
	}
	if a.calls.Load() != 2 || b.calls.Load() != 2 {
		t.Fatalf("两租户应各调用两次，A=%d B=%d", a.calls.Load(), b.calls.Load())
	}
	permission.mu.Lock()
	scopes := append([]uint(nil), permission.scopes...)
	permission.mu.Unlock()
	if fmt.Sprint(scopes) != "[17001 17002 17001 17002]" {
		t.Fatalf("权限检查必须按每次请求的可信租户判定，实际 %v", scopes)
	}
	entries := audit.Entries()
	if len(entries) != 4 {
		t.Fatalf("应保留四次真实执行的审计，实际 %d", len(entries))
	}
	for i, entry := range entries {
		wantID, wantIdentity := uint(17001), a.identity
		if i%2 == 1 {
			wantID, wantIdentity = 17002, b.identity
		}
		if entry.TenantID != wantID || !strings.Contains(entry.ResultSummary, wantIdentity) {
			t.Errorf("审计归属与实际实例不一致: %+v", entry)
		}
	}
	missing := executor.Execute(context.Background(), ExecuteRequest{ToolName: name})
	if !errors.Is(missing.Err, ErrToolNotFound) || a.calls.Load() != 2 || b.calls.Load() != 2 {
		t.Fatalf("无可信租户上下文不得命中已暖缓存: %+v", missing)
	}
	updated := newScopedCountingTool(name, "tenant-a.credential-v2")
	registerScopedTool(t, 17001, updated)
	expectScopedIdentity(t, executor, ctxA, name, updated.identity)
	expectScopedIdentity(t, executor, ctxB, name, b.identity)
	if a.calls.Load() != 2 || updated.calls.Load() != 1 || b.calls.Load() != 3 {
		t.Fatal("更新凭据标识后仍执行旧实例或干扰了另一租户")
	}
	if err := TenantRegistry(17001).Unregister(name); err != nil {
		t.Fatal(err)
	}
	if result := executor.Execute(ctxA, ExecuteRequest{ToolName: name}); !errors.Is(result.Err, ErrToolNotFound) {
		t.Fatalf("注销后不得从缓存继续执行: %+v", result)
	}
	recreated := newScopedCountingTool(name, "tenant-a.recreated-v3")
	registerScopedTool(t, 17001, recreated)
	expectScopedIdentity(t, executor, ctxA, name, recreated.identity)
	executor.SetOverride(ToolOverride{ToolName: name, Disabled: true})
	for _, ctx := range []context.Context{ctxA, ctxB} {
		if result := executor.Execute(ctx, ExecuteRequest{ToolName: name}); result.Err == nil {
			t.Fatal("工具禁用应作用于该名称的所有租户缓存")
		}
	}
	executor.ClearOverride(name)
	expectScopedIdentity(t, executor, ctxB, name, b.identity)
	executor.ClearCache()
	expectScopedIdentity(t, executor, ctxA, name, recreated.identity)
}

// 名称访问允许让出调度，但工具名本身不变；替换不应因此产生注销空档。
type yieldingNameTool struct{ Tool }

func (s yieldingNameTool) Name() string {
	runtime.Gosched()
	return s.Tool.Name()
}

func TestToolExecutor_ScopedHandlerAtomicReplacement(t *testing.T) {
	ctx, _ := setupScopedExecutorTest(t)
	const name = "scope-test.atomic-replacement"
	a, b := newScopedCountingTool(name, "atomic.a"), newScopedCountingTool(name, "atomic.b")
	registerScopedTool(t, 17001, yieldingNameTool{a})
	executor := NewToolExecutor(NewToolRegistry(), ToolExecutorConfig{})
	var wg sync.WaitGroup
	failures := make(chan error, 400)
	start := make(chan struct{})
	wg.Add(1)
	utils.SafeGo(ctx, "test.tool-cache.replace", func(context.Context) {
		defer wg.Done()
		<-start
		for i := range 200 {
			tool := a
			if i%2 == 1 {
				tool = b
			}
			if err := RegisterTenantTool(17001, yieldingNameTool{tool}); err != nil {
				failures <- err
			}
		}
	})
	close(start)
	for range 200 {
		result := executor.Execute(ctx, ExecuteRequest{ToolName: name})
		if result.Err != nil || (result.Data != a.identity && result.Data != b.identity) {
			failures <- fmt.Errorf("同名替换期间只能执行完整旧/新实例，实际 %v/%v", result.Data, result.Err)
		}
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
}

func TestToolExecutor_ScopedHandlerSourceAndGeneration(t *testing.T) {
	ctx, _ := setupScopedExecutorTest(t)
	const name = "scope-test.source-change"
	reg := NewToolRegistry()
	builtin, custom := newScopedCountingTool(name, "builtin.v1"), newScopedCountingTool(name, "custom.v1")
	reg.MustRegister(builtin)
	registerScopedTool(t, 17001, custom)
	checker := &recordingApprovalChecker{approved: false}
	executor := NewToolExecutor(reg, ToolExecutorConfig{ApprovalChecker: checker})
	expectScopedIdentity(t, executor, ctx, name, builtin.identity)
	if err := reg.Unregister(name); err != nil {
		t.Fatal(err)
	}
	expectScopedIdentity(t, executor, ctx, name, custom.identity)
	// 同一指针重新登记也必须产生新代际，不能只用指针相等来判断配置未变。
	outbound := newScopedCountingTool("scope-test.outbound", "outbound.v1")
	registerScopedTool(t, 17001, outbound)
	expectScopedIdentity(t, executor, ctx, outbound.Name(), outbound.identity)
	outbound.CategoryVal = CategoryReach
	registerScopedTool(t, 17001, outbound)
	result := executor.Execute(ctx, ExecuteRequest{ToolName: outbound.Name()})
	if !errors.Is(result.Err, ErrApprovalDenied) || outbound.calls.Load() != 1 || checker.askedCount() != 1 {
		t.Fatalf("重新登记后审批须使用新类别且零外发，calls=%d asked=%d err=%v", outbound.calls.Load(), checker.askedCount(), result.Err)
	}
}

func TestToolExecutor_ScopedHandlerRiskGeneration(t *testing.T) {
	ctx, _ := setupScopedExecutorTest(t)
	const name = "scope-test.risk-change"
	low, high := newScopedCountingTool(name, "readonly.v1"), newScopedCountingTool(name, "high-write.v2")
	high.RiskVal = RiskHighWrite
	registerScopedTool(t, 17001, low)
	obs := NewMemoryRiskObserver(20)
	executor := NewToolExecutor(NewToolRegistry(), ToolExecutorConfig{
		RiskGrants: &stubGrants{byAgent: map[string][]string{}}, RiskObserver: obs, RiskEnforce: true,
	})
	expectScopedIdentity(t, executor, ctx, name, low.identity)
	registerScopedTool(t, 17001, high)
	result := executor.Execute(ctx, ExecuteRequest{ToolName: name, ToolCtx: &ToolContext{AgentID: "cache-test"}})
	if result.Err == nil || result.Success || high.calls.Load() != 0 || low.calls.Load() != 1 {
		t.Fatalf("新代际高危工具须被真实风险闸拒绝，low=%d high=%d err=%v", low.calls.Load(), high.calls.Load(), result.Err)
	}
}

func TestToolExecutor_ScopedHandlerConcurrentFirstCall(t *testing.T) {
	ctxA, ctxB := setupScopedExecutorTest(t)
	const name = "scope-test.concurrent"
	a, b := newScopedCountingTool(name, "concurrent.a"), newScopedCountingTool(name, "concurrent.b")
	registerScopedTool(t, 17001, a)
	registerScopedTool(t, 17002, b)
	executor := NewToolExecutor(NewToolRegistry(), ToolExecutorConfig{})
	var wg sync.WaitGroup
	failures := make(chan string, 64)
	start := make(chan struct{})
	for i := range 64 {
		ctx, identity := ctxA, a.identity
		if i%2 == 1 {
			ctx, identity = ctxB, b.identity
		}
		wg.Add(1)
		utils.SafeGo(ctx, "test.tool-cache.concurrent", func(ctx context.Context) {
			defer wg.Done()
			<-start
			result := executor.Execute(ctx, ExecuteRequest{ToolName: name})
			if result.Err != nil || result.Data != identity {
				failures <- fmt.Sprintf("期望 %s，实际 %v/%v", identity, result.Data, result.Err)
			}
		})
	}
	close(start)
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	if a.calls.Load() != 32 || b.calls.Load() != 32 {
		t.Errorf("并发首次调用应各 32 次，实际 A=%d B=%d", a.calls.Load(), b.calls.Load())
	}
}

func TestToolExecutor_ScopedHandlerInflightBoundary(t *testing.T) {
	ctx, _ := setupScopedExecutorTest(t)
	const name = "scope-test.inflight"
	old, updated := newScopedCountingTool(name, "inflight.old"), newScopedCountingTool(name, "inflight.new")
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	old.execute = func(context.Context) { close(started); <-release }
	registerScopedTool(t, 17001, old)
	executor := NewToolExecutor(NewToolRegistry(), ToolExecutorConfig{DefaultTimeout: 2 * time.Second})
	done := make(chan ExecuteResult, 1)
	utils.SafeGo(ctx, "test.tool-cache.inflight", func(ctx context.Context) {
		done <- executor.Execute(ctx, ExecuteRequest{ToolName: name})
	})
	defer unblock()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("旧调用未按时开始")
	}
	registerScopedTool(t, 17001, updated)
	expectScopedIdentity(t, executor, ctx, name, updated.identity)
	if err := TenantRegistry(17001).Unregister(name); err != nil {
		t.Fatal(err)
	}
	if result := executor.Execute(ctx, ExecuteRequest{ToolName: name}); !errors.Is(result.Err, ErrToolNotFound) {
		t.Fatalf("撤销返回后新调用必须拒绝: %+v", result)
	}
	unblock()
	select {
	case result := <-done:
		if result.Err != nil || result.Data != old.identity || old.calls.Load() != 1 || updated.calls.Load() != 1 {
			t.Fatalf("在途旧快照应允许结束且不污染更新后调用: %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("在途旧调用未按时结束")
	}
}
