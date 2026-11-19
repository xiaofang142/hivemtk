package service

import (
	"context"
	"fmt"

	"hivemtk-user/internal/model"
)

// 服务端工作流定义校验（FA-02）：未知节点/动作在创建、更新、发布三道闸统一拒绝，
// 不再依赖运行时 noop 兜底；前端校验不得替代这里的约束。
//
// 结构约束恒校验（入口、边引用、已知 action、subflow 引用、节点 ID 唯一）；
// 节点类型以注册表为准（registry 注入后校验，装配点恒注入）。

// KnownWorkflowActionTypes action 节点允许的动作类型集合。
var KnownWorkflowActionTypes = map[string]bool{
	"log":             true,
	"http":            true,
	"delay":           true,
	"browser_session": true,
	"browser_task":    true,
}

// ValidateWorkflowDefinition 校验定义 JSONMap。registry 非 nil 时同时校验节点类型已注册。
func ValidateWorkflowDefinition(m model.JSONMap, registry *WorkflowNodeExecutorRegistry) error {
	def, err := ParseWorkflowDefinition(m)
	if err != nil {
		return fmt.Errorf("workflow definition invalid: %w", err)
	}
	if len(def.Nodes) == 0 {
		return fmt.Errorf("workflow definition invalid: at least one node required")
	}

	knownTypes := map[string]bool{}
	if registry != nil {
		for _, t := range registry.AllRegistered(context.Background()) {
			knownTypes[t] = true
		}
	}

	hasTrigger := false
	ids := map[string]bool{}
	for i := range def.Nodes {
		n := &def.Nodes[i]
		if n.ID == "" {
			return fmt.Errorf("workflow definition invalid: node #%d missing id", i+1)
		}
		if ids[n.ID] {
			return fmt.Errorf("workflow definition invalid: duplicate node id %q", n.ID)
		}
		ids[n.ID] = true
		if n.Type == WorkflowNodeTypeTrigger {
			hasTrigger = true
		}
		if registry != nil && !knownTypes[n.Type] {
			return fmt.Errorf("workflow definition invalid: node %q has unregistered type %q", n.ID, n.Type)
		}
		switch n.Type {
		case WorkflowNodeTypeAction:
			at, _ := n.Config["action_type"].(string)
			if !KnownWorkflowActionTypes[at] {
				return fmt.Errorf("workflow definition invalid: node %q has unknown action_type %q", n.ID, at)
			}
		case WorkflowNodeTypeSubflow:
			sid, _ := n.Config["sub_workflow_id"].(string)
			if sid == "" {
				return fmt.Errorf("workflow definition invalid: subflow node %q requires sub_workflow_id", n.ID)
			}
		}
	}
	if !hasTrigger {
		return fmt.Errorf("workflow definition invalid: no trigger entry node")
	}

	for _, e := range def.Edges {
		if !ids[e.Source] {
			return fmt.Errorf("workflow definition invalid: edge source %q not in nodes", e.Source)
		}
		if !ids[e.Target] {
			return fmt.Errorf("workflow definition invalid: edge target %q not in nodes", e.Target)
		}
	}
	return nil
}
