package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// newValidationRegistry 返回装配了标准执行器的注册表（与路由装配点同口径）。
func newValidationRegistry() *WorkflowNodeExecutorRegistry {
	r := NewWorkflowNodeExecutorRegistry()
	RegisterWorkflowNodeExecutors(r, nil)
	return r
}

func TestValidateWorkflowDefinition_AcceptsValidMinimal(t *testing.T) {
	if err := ValidateWorkflowDefinition(workflowTestDefinition(), newValidationRegistry()); err != nil {
		t.Fatalf("合法定义不应被拒绝: %v", err)
	}
}

func TestValidateWorkflowDefinition_RejectsUnknownNodeType(t *testing.T) {
	def := workflowTestDefinition()
	def["nodes"] = []any{
		map[string]any{"id": "n1", "type": "trigger", "config": map[string]any{}},
		map[string]any{"id": "n2", "type": "ghost_node", "config": map[string]any{}},
	}
	err := ValidateWorkflowDefinition(def, newValidationRegistry())
	if err == nil || !strings.Contains(err.Error(), "ghost_node") {
		t.Fatalf("未知节点类型必须被拒绝，得到 %v", err)
	}
}

func TestValidateWorkflowDefinition_RejectsUnknownActionType(t *testing.T) {
	def := workflowTestDefinition()
	def["nodes"] = []any{
		map[string]any{"id": "n1", "type": "trigger", "config": map[string]any{}},
		map[string]any{"id": "n2", "type": "action", "config": map[string]any{"action_type": "sms_blast"}},
	}
	if err := ValidateWorkflowDefinition(def, newValidationRegistry()); err == nil {
		t.Fatal("未知 action_type 必须被拒绝")
	}
}

func TestValidateWorkflowDefinition_RejectsMissingTriggerAndBadEdges(t *testing.T) {
	onlyAction := map[string]any{
		"nodes": []any{
			map[string]any{"id": "n1", "type": "action", "config": map[string]any{"action_type": "log"}},
		},
	}
	if err := ValidateWorkflowDefinition(onlyAction, nil); err == nil {
		t.Fatal("无 trigger 入口必须被拒绝")
	}

	badEdge := map[string]any{
		"nodes": []any{
			map[string]any{"id": "n1", "type": "trigger", "config": map[string]any{}},
		},
		"edges": []any{
			map[string]any{"source": "n1", "target": "missing", "label": ""},
		},
	}
	if err := ValidateWorkflowDefinition(badEdge, nil); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("边引用不存在节点必须被拒绝，得到 %v", err)
	}

	dupID := map[string]any{
		"nodes": []any{
			map[string]any{"id": "n1", "type": "trigger", "config": map[string]any{}},
			map[string]any{"id": "n1", "type": "action", "config": map[string]any{"action_type": "log"}},
		},
	}
	if err := ValidateWorkflowDefinition(dupID, nil); err == nil {
		t.Fatal("重复节点 ID 必须被拒绝")
	}

	subflowNoRef := map[string]any{
		"nodes": []any{
			map[string]any{"id": "n1", "type": "trigger", "config": map[string]any{}},
			map[string]any{"id": "n2", "type": "subflow", "config": map[string]any{}},
		},
	}
	if err := ValidateWorkflowDefinition(subflowNoRef, nil); err == nil {
		t.Fatal("subflow 缺 sub_workflow_id 必须被拒绝")
	}
}

func TestValidateWorkflowDefinition_EmptyNodesRejected(t *testing.T) {
	if err := ValidateWorkflowDefinition(map[string]any{"nodes": []any{}}, nil); err == nil {
		t.Fatal("空定义必须被拒绝")
	}
}

func TestActionNodeExecutor_UnknownActionZeroSideEffects(t *testing.T) {
	wctx, exec := httpActionCtx(9100, model.JSONMap{"action_type": "ghost"})
	e := &ActionNodeExecutor{}
	res, err := e.Execute(context.Background(), wctx)
	if err != nil {
		t.Fatalf("Execute 不应返回 err（失败走结果对象）：%v", err)
	}
	if res.Status != NodeStatusFailed || res.Retryable {
		t.Fatalf("未知动作期望 failed+不可重试，得到 %s", res.Status)
	}
	if len(extractWorkflowSideEffects(exec.Context)) != 0 {
		t.Fatalf("未知动作必须零副作用，得到 %v", extractWorkflowSideEffects(exec.Context))
	}
}

// —— DB 闸测试：三道闸 + 在途实例快照 ——

func setupWorkflowValidateSvc(t *testing.T) (*WorkflowOrchestratorService, *gorm.DB) {
	t.Helper()
	db := setupWorkflowServiceTestDB(t)
	if db == nil {
		return nil, nil
	}
	versionRepo := repository.NewWorkflowVersionRepository(db)
	execRepo := repository.NewWorkflowExecutionRepository(db)
	nodeExecRepo := repository.NewWorkflowNodeExecutionRepository(db)
	svc := NewWorkflowOrchestratorService(versionRepo, execRepo, nodeExecRepo)
	svc.SetNodeRegistry(newValidationRegistry())
	return svc, db
}

func TestWO_CreateVersion_RejectsInvalidDefinition(t *testing.T) {
	svc, _ := setupWorkflowValidateSvc(t)
	if svc == nil {
		return
	}
	_, err := svc.CreateVersion(context.Background(), &dto.WorkflowVersionCreateRequest{
		WorkflowID: "wf-fa02-create",
		Name:       "bad",
		Definition: map[string]any{"nodes": []any{}},
	})
	if err == nil {
		t.Fatal("空定义创建必须被创建闸拒绝")
	}
}

func TestWO_PublishVersion_RejectsInvalidDefinition(t *testing.T) {
	svc, db := setupWorkflowValidateSvc(t)
	if svc == nil {
		return
	}
	// 直接落库脏定义（模拟历史数据/绕过 API 的路径），发布闸必须拦截。
	v := &model.WorkflowVersion{WorkflowID: "wf-fa02-pub", Version: 1, Name: "dirty",
		Definition: model.JSONMap{"nodes": []any{}}, Status: model.WorkflowStatusDraft}
	if err := db.Create(v).Error; err != nil {
		t.Fatalf("seed 失败: %v", err)
	}
	err := svc.PublishVersion(context.Background(), v.ID)
	if err == nil {
		t.Fatal("脏定义必须被发布闸拒绝")
	}
	db.First(v, v.ID)
	if v.Status != model.WorkflowStatusDraft {
		t.Fatalf("发布被拒后状态必须仍是 draft，得到 %s", v.Status)
	}
}

func TestWO_UpdateVersion_PublishedDefinitionImmutable(t *testing.T) {
	svc, db := setupWorkflowValidateSvc(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	v, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{WorkflowID: "wf-fa02-frozen",
		Name: "v1", Definition: workflowTestDefinition()})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if err := svc.PublishVersion(ctx, v.ID); err != nil {
		t.Fatalf("发布失败: %v", err)
	}

	changed := workflowTestDefinition()
	changed["nodes"] = []any{
		map[string]any{"id": "n1", "type": "trigger", "config": map[string]any{}},
		map[string]any{"id": "n9", "type": "action", "name": "evil", "config": map[string]any{"action_type": "log"}},
	}
	err = svc.UpdateVersion(ctx, v.ID, &dto.WorkflowVersionUpdateRequest{Name: "v1-evil", Definition: changed})
	if !errors.Is(err, ErrPublishedVersionImmutable) {
		t.Fatalf("已发布版本改 definition 必须返回 ErrPublishedVersionImmutable，得到 %v", err)
	}

	stored := &model.WorkflowVersion{}
	db.First(stored, v.ID)
	if _, derr := ParseWorkflowDefinition(stored.Definition); derr != nil {
		t.Fatalf("快照应保持可解析: %v", derr)
	}
	if n, _ := stored.Definition["nodes"].([]any); len(n) != 2 {
		t.Fatalf("definition 快照被原位覆盖：%v", stored.Definition)
	}

	// 同内容 definition 的更新（幂等改元数据）允许。
	err = svc.UpdateVersion(ctx, v.ID, &dto.WorkflowVersionUpdateRequest{Name: "v1-meta",
		Description: "meta only", Definition: workflowTestDefinition()})
	if err != nil {
		t.Fatalf("同内容 definition+元数据更新不应被拒: %v", err)
	}
	db.First(stored, v.ID)
	if stored.Name != "v1-meta" {
		t.Fatalf("元数据应已更新，得到 %s", stored.Name)
	}
}

func TestWO_UpdateVersion_DraftValidatedButMutable(t *testing.T) {
	svc, _ := setupWorkflowValidateSvc(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	v, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{WorkflowID: "wf-fa02-draft",
		Name: "v1", Definition: workflowTestDefinition()})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	bad := map[string]any{"nodes": []any{}}
	if err := svc.UpdateVersion(ctx, v.ID, &dto.WorkflowVersionUpdateRequest{Name: "x", Definition: bad}); err == nil {
		t.Fatal("draft 改稿也必须过校验闸")
	}

	changed := workflowTestDefinition()
	changed["nodes"] = []any{
		map[string]any{"id": "n1", "type": "trigger", "config": map[string]any{}},
		map[string]any{"id": "n2", "type": "action", "name": "log step", "config": map[string]any{"action_type": "delay", "duration_ms": 10}},
		map[string]any{"id": "n3", "type": "action", "name": "log2", "config": map[string]any{"action_type": "log"}},
	}
	changed["edges"] = []any{
		map[string]any{"source": "n1", "target": "n2", "label": ""},
		map[string]any{"source": "n2", "target": "n3", "label": ""},
	}
	if err := svc.UpdateVersion(ctx, v.ID, &dto.WorkflowVersionUpdateRequest{Name: "v2", Definition: changed}); err != nil {
		t.Fatalf("draft 合法改稿应通过: %v", err)
	}
}

func TestWorkflowDispatcher_UnknownNodeTypeFailsExecution(t *testing.T) {
	db := setupWorkflowServiceTestDB(t)
	if db == nil {
		return
	}
	// 直落库含未知节点类型的版本（等价历史脏数据绕过校验闸），运行时必须失败而非跳过。
	v := &model.WorkflowVersion{WorkflowID: "wf-fa02-exec", Version: 1, Name: "ghost",
		Definition: model.JSONMap{"nodes": []any{
			map[string]any{"id": "n1", "type": "trigger", "config": map[string]any{}},
			map[string]any{"id": "n2", "type": "ghost_node", "config": map[string]any{}},
		}, "edges": []any{
			map[string]any{"source": "n1", "target": "n2", "label": ""},
		}}, Status: model.WorkflowStatusPublished}
	if err := db.Create(v).Error; err != nil {
		t.Fatalf("seed 失败: %v", err)
	}
	exec := &model.WorkflowExecution{WorkflowID: v.WorkflowID, Version: 1,
		Status: model.WorkflowExecRunning, Context: model.JSONMap{}}
	if err := db.Create(exec).Error; err != nil {
		t.Fatalf("seed 失败: %v", err)
	}

	versionRepo := repository.NewWorkflowVersionRepository(db)
	execRepo := repository.NewWorkflowExecutionRepository(db)
	nodeExecRepo := repository.NewWorkflowNodeExecutionRepository(db)
	d := NewWorkflowDispatcher(versionRepo, execRepo, nodeExecRepo, newValidationRegistry(), nil)
	d.runExecution(context.Background(), exec.ID, "tr-fa02")

	got := &model.WorkflowExecution{}
	db.First(got, exec.ID)
	if got.Status != model.WorkflowExecFailed {
		t.Fatalf("未知节点类型必须令执行失败（零副作用零跳过），得到 %s", got.Status)
	}
	if !strings.Contains(got.Error, "unknown node type") {
		t.Fatalf("失败原因应写明未知节点类型，得到 %q", got.Error)
	}
}

func TestTriggerAndActionAdvanceAlongDefaultEdge(t *testing.T) {
	def := workflowTestDefinition()
	parsed, err := ParseWorkflowDefinition(def)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	wctx, _ := httpActionCtx(9200, model.JSONMap{})
	wctx.Graph = parsed
	wctx.NodeID = "n1"
	wctx.NodeType = WorkflowNodeTypeTrigger

	tr := &TriggerNodeExecutor{}
	res, err := tr.Execute(context.Background(), wctx)
	if err != nil || res.Status != NodeStatusCompleted {
		t.Fatalf("trigger 执行失败: %v %s", err, res.Status)
	}
	if res.NextNodeID != "n2" {
		t.Fatalf("trigger 必须沿默认边推进到 n2，得到 %q（线性链截断回归）", res.NextNodeID)
	}

	wctx.NodeID = "n2"
	wctx.NodeType = WorkflowNodeTypeAction
	wctx.NodeConfig = model.JSONMap{"action_type": "log", "message": "hi"}
	act := &ActionNodeExecutor{}
	res2, err := act.Execute(context.Background(), wctx)
	if err != nil || res2.Status != NodeStatusCompleted {
		t.Fatalf("action 执行失败: %v %s", err, res2.Status)
	}
	_ = res2 // n2 无出边，推进到空串收口
}
