package tooluse

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

type revocationRoundTripper struct{ calls atomic.Int64 }

type revocationRetryPolicy struct{}

func (revocationRetryPolicy) MaxAttempts() int                             { return 3 }
func (revocationRetryPolicy) NextBackoff(int, error) (time.Duration, bool) { return 0, true }

func TestManagedExecutor_RevocationBeforeRetry(t *testing.T) {
	database := testutil.NewTestDB(t, &model.AIToolConfig{})
	ctx := tenant.WithTenantID(context.Background(), 1)
	const name = "runtime.retry"
	if err := database.WithContext(ctx).Create(&model.AIToolConfig{TenantID: 1, ToolName: name, IsEnabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	tool := newMockTool(name, CategoryBusiness, func(ctx context.Context, _ map[string]any) (ToolResult, error) {
		calls.Add(1)
		if err := repository.NewAIToolConfigRepository(database).UpdateStatus(ctx, name, false); err != nil {
			return ErrorResult(name, err), err
		}
		err := errors.New("simulated transient failure")
		return ErrorResult(name, err), err
	})
	reg := NewToolRegistry()
	if err := reg.Register(tool); err != nil {
		t.Fatal(err)
	}
	executor := NewToolExecutor(reg, ToolExecutorConfig{AdmissionChecker: repository.NewToolRuntimeRepositoryWithDB(database), RetryPolicy: revocationRetryPolicy{}})
	result := executor.Execute(ctx, ExecuteRequest{ToolName: name})
	if result.Success || !errors.Is(result.Err, repository.ErrToolRuntimeDisabled) || calls.Load() != 1 {
		t.Fatalf("首次调用后撤销必须拦住后续重试，calls=%d result=%+v", calls.Load(), result)
	}
}

func TestManagedExecutor_RevocationInflightBoundary(t *testing.T) {
	database := testutil.NewTestDB(t, &model.AIToolConfig{})
	ctx := tenant.WithTenantID(context.Background(), 1)
	const name = "runtime.inflight"
	if err := database.WithContext(ctx).Create(&model.AIToolConfig{TenantID: 1, ToolName: name, IsEnabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var calls atomic.Int64
	tool := newMockTool(name, CategoryBusiness, func(context.Context, map[string]any) (ToolResult, error) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return SuccessResult(name, "simulated side effect"), nil
	})
	reg := NewToolRegistry()
	if err := reg.Register(tool); err != nil {
		t.Fatal(err)
	}
	executor := NewToolExecutor(reg, ToolExecutorConfig{AdmissionChecker: repository.NewToolRuntimeRepositoryWithDB(database)})
	done := make(chan ExecuteResult, 1)
	utils.SafeGo(ctx, "test-managed-tool-inflight", func(ctx context.Context) { done <- executor.Execute(ctx, ExecuteRequest{ToolName: name}) })
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("在途工具没有进入已准入执行体")
	}
	if err := repository.NewAIToolConfigRepository(database).UpdateStatus(ctx, name, false); err != nil {
		t.Fatal(err)
	}
	denied := executor.Execute(ctx, ExecuteRequest{ToolName: name})
	if denied.Success || !errors.Is(denied.Err, repository.ErrToolRuntimeDisabled) || calls.Load() != 1 {
		t.Fatalf("撤销提交后的新调用必须零副作用: calls=%d result=%+v", calls.Load(), denied)
	}
	once.Do(func() { close(release) })
	select {
	case result := <-done:
		if !result.Success || result.Err != nil || calls.Load() != 1 {
			t.Fatalf("已准入的在途调用应允许完成一次: %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("在途调用未正常完成")
	}
}

func (r *revocationRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls.Add(1)
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"stock":42}`))}, nil
}

// 两个独立启动副本持有旧对象，管理提交后每次真实 Execute 都必须服从权威行。
func TestManagedOpenAPI_RevocationAcrossReplicas(t *testing.T) {
	database := testutil.NewTestDB(t, &model.CustomTool{}, &model.AIToolConfig{})
	ctx := tenant.WithTenantID(context.Background(), 1)
	row := model.CustomTool{TenantID: 1, Name: "revocation", BaseURL: "https://erp.example.com", SchemaJSON: bootTestSchema, IsActive: true}
	if err := database.WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	transport := &revocationRoundTripper{}
	load := func() Tool {
		t.Helper()
		providers := LoadCustomToolProviders(database, repository.NewToolRuntimeRepositoryWithDB(database))
		if len(providers) != 1 {
			t.Fatalf("应重建一个持久化工具集，实际 %d", len(providers))
		}
		tools, err := providers[0].Provide(ProviderContext{})
		if err != nil || len(tools) != 1 {
			t.Fatalf("重建工具失败: %v", err)
		}
		tools[0].(*openAPITool).client = &http.Client{Transport: transport}
		return tools[0]
	}
	first, second := load(), load()
	check := func(label string, tools []Tool, allow bool) {
		t.Helper()
		for _, tool := range tools {
			before := transport.calls.Load()
			result, err := tool.Execute(ctx, map[string]any{"sku": "simulated"})
			if allow && (err != nil || !result.Success || transport.calls.Load() != before+1) {
				t.Errorf("%s 应实际调用一次，结果 %+v，错误 %v", label, result, err)
			}
			if !allow && (err == nil || result.Success || transport.calls.Load() != before) {
				t.Errorf("%s 应拒绝且零新副作用，结果 %+v，错误 %v，调用增量 %d", label, result, err, transport.calls.Load()-before)
			}
		}
	}
	write := func(value any, fields map[string]any) {
		t.Helper()
		if err := database.WithContext(ctx).Model(value).Updates(fields).Error; err != nil {
			t.Fatal(err)
		}
	}
	unwired, err := CustomToolRowToProvider(&row, nil)
	if err != nil {
		t.Fatal(err)
	}
	unwiredTools, err := unwired.Provide(ProviderContext{})
	if err != nil || len(unwiredTools) != 1 {
		t.Fatalf("未装配权威端口的工具构建失败: %v", err)
	}
	unwiredTools[0].(*openAPITool).client = &http.Client{Transport: transport}
	check("缺失权威端口", unwiredTools, false)
	old := []Tool{first, second}
	check("初始", old, true)
	config := model.AIToolConfig{TenantID: 1, ToolName: first.Name(), IsEnabled: true}
	if err := database.WithContext(ctx).Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	write(&config, map[string]any{"is_enabled": false})
	check("配置停用", old, false)
	write(&config, map[string]any{"is_enabled": true})
	check("配置恢复", old, true)
	write(&row, map[string]any{"is_active": false})
	check("来源停用", old, false)
	write(&row, map[string]any{"is_active": true, "auth_header": "X-Revision"})
	check("执行配置已变更", old, false)
	fresh := load()
	check("重建使用当前执行配置", []Tool{fresh}, true)
	if err := database.WithContext(ctx).Delete(&row).Error; err != nil {
		t.Fatal(err)
	}
	check("来源删除", append(old, fresh), false)
	recreated := row
	recreated.ID = 0
	if err := database.WithContext(ctx).Create(&recreated).Error; err != nil {
		t.Fatal(err)
	}
	check("同名新 ID 不能复活旧对象", append(old, fresh), false)
	current := load()
	check("同名重建后的启动恢复", []Tool{current}, true)
	before := transport.calls.Load()
	if _, err := current.Execute(tenant.WithTenantID(context.Background(), 2), map[string]any{"sku": "simulated"}); err == nil || transport.calls.Load() != before {
		t.Error("另一租户不能直接使用持久化实例")
	}
	if err := database.Callback().Query().Before("gorm:query").Register("test:tool-runtime-query-fault", func(tx *gorm.DB) { tx.AddError(context.DeadlineExceeded) }); err != nil {
		t.Fatal(err)
	}
	check("权威读取故障", []Tool{current}, false)
	if err := database.Callback().Query().Remove("test:tool-runtime-query-fault"); err != nil {
		t.Fatal(err)
	}
	check("权威读取恢复", []Tool{current}, true)
}
