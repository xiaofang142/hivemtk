package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils"
)

// FA-WF-08：空草稿能存为 draft，但发布时严格校验拒绝；结构性错误在草稿创建时仍被拒。
func TestFAWF08_EmptyDraftAllowedStrictPublishRejects(t *testing.T) {
	svc, database := setupWorkflowDeepService(t)
	if svc == nil || database == nil {
		t.Skip("测试库不可用")
	}
	ctx := context.Background()

	empty := map[string]any{"nodes": []any{}, "edges": []any{}}
	created, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
		WorkflowID: "wf08-empty", Name: "空草稿", Definition: empty,
	})
	if err != nil || created == nil {
		t.Fatalf("空 definition 应能存为 draft: err=%v created=%+v", err, created)
	}
	if err := svc.PublishVersion(ctx, created.ID); !errors.Is(err, utils.ErrInvalidInput) {
		t.Fatalf("空定义发布应被严格校验拒绝 ErrInvalidInput, 实际 %v", err)
	}
}

// FA-WF-08：未知节点类型/未知 action_type 的畸形定义，草稿创建时仍被拒（FA-02 结构性约束不废）。
func TestFAWF08_MalformedDraftStillRejected(t *testing.T) {
	svc, database := setupWorkflowDeepService(t)
	if svc == nil || database == nil {
		t.Skip("测试库不可用")
	}
	// 节点类型注册校验只在 registry 非 nil 时执行；注入标准 registry 以验证未知类型被拒。
	svc.nodeRegistry = newValidationRegistry()
	ctx := context.Background()

	// 未知节点类型
	badType := workflowTestDefinition()
	nodes := badType["nodes"].([]any)
	n0 := nodes[0].(map[string]any)
	n0["type"] = "bogus"
	if _, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
		WorkflowID: "wf08-badtype", Name: "未知类型", Definition: badType,
	}); !errors.Is(err, utils.ErrInvalidInput) {
		t.Fatalf("未知节点类型应在草稿创建时被拒 ErrInvalidInput, 实际 %v", err)
	}

	// 未知 action_type
	badAction := workflowTestDefinition()
	nodes = badAction["nodes"].([]any)
	n1 := nodes[1].(map[string]any)
	n1["config"] = map[string]any{"action_type": "bogus_action"}
	if _, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
		WorkflowID: "wf08-badaction", Name: "未知动作", Definition: badAction,
	}); !errors.Is(err, utils.ErrInvalidInput) {
		t.Fatalf("未知 action_type 应在草稿创建时被拒 ErrInvalidInput, 实际 %v", err)
	}
}

// FA-WF-08：正常定义路径仍绿。
func TestFAWF08_NormalDefinitionStillWorks(t *testing.T) {
	svc, database := setupWorkflowDeepService(t)
	if svc == nil || database == nil {
		t.Skip("测试库不可用")
	}
	ctx := context.Background()
	created, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
		WorkflowID: "wf08-normal", Name: "正常", Definition: workflowTestDefinition(),
	})
	if err != nil || created == nil {
		t.Fatalf("正常定义应能创建: err=%v created=%+v", err, created)
	}
	if err := svc.PublishVersion(ctx, created.ID); err != nil {
		t.Fatalf("正常定义应能发布: %v", err)
	}
}
