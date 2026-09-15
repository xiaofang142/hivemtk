package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

)

// MarketingWorkflow 自动化工作流
type MarketingWorkflow struct {
	ID       string                   `json:"id"`
	Name     string                   `json:"name"`
	Industry string                   `json:"industry"`
	Trigger  map[string]interface{}   `json:"trigger"`
	Steps    []map[string]interface{} `json:"steps"`
	KPI      map[string]interface{}   `json:"kpi"`
}

// WorkflowLoader 工作流加载器
type WorkflowLoader struct {
}

func NewWorkflowLoader() *WorkflowLoader {
	return &WorkflowLoader{}
}

func (l *WorkflowLoader) LoadWorkflow(ctx context.Context, workflowID string) (*MarketingWorkflow, error) {
	if data, found := LoadAssetFromDB("marketing_workflow", workflowID); found {
		var w MarketingWorkflow
		if err := json.Unmarshal(data, &w); err == nil {
			w.ID = workflowID
			return &w, nil
		}
		slog.Warn("WorkflowLoader invalid JSON, fallback", "asset_id", workflowID)
	}
	return loadDefaultWorkflow(workflowID)
}

func (l *WorkflowLoader) ListAllWorkflows(ctx context.Context) ([]*MarketingWorkflow, error) {
	var result []*MarketingWorkflow
	rows, _ := ListAssetsFromDB("marketing_workflow")
	seen := map[string]bool{}
	for _, r := range rows {
		var w MarketingWorkflow
		if err := json.Unmarshal(r.Data, &w); err == nil {
			w.ID = r.AssetID
			w.Name = r.Name
			result = append(result, &w)
			seen[w.ID] = true
		}
	}
	for id, w := range defaultWorkflows() {
		if !seen[id] {
			result = append(result, w)
		}
	}
	return result, nil
}

func loadDefaultWorkflow(id string) (*MarketingWorkflow, error) {
	if w, ok := defaultWorkflows()[id]; ok {
		return w, nil
	}
	return nil, errors.New("workflow not found: " + id)
}

func defaultWorkflows() map[string]*MarketingWorkflow {
	return map[string]*MarketingWorkflow{
		"default-welcome": {
			ID:   "default-welcome",
			Name: "默认新客欢迎流",
			Trigger: map[string]interface{}{
				"type": "new_customer", "channel": "all",
			},
			Steps: []map[string]interface{}{
				{"order": 1, "action": "send_message", "config": map[string]string{"template": "welcome"}},
				{"order": 2, "action": "wait", "config": map[string]string{"duration": "1d"}},
				{"order": 3, "action": "send_coupon", "config": map[string]string{"coupon_id": "new-user"}},
			},
			KPI: map[string]interface{}{"target_conversion": ">20%"},
		},
	}
}
