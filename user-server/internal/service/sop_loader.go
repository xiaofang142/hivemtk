package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"hivemtk-user/internal/dto"

)

// IndustrySOP 行业 SOP
type IndustrySOP struct {
	ID       string                   `json:"id"`
	Name     string                   `json:"name"`
	Industry string                   `json:"industry"`
	Category string                   `json:"category"`
	Steps    []map[string]interface{} `json:"steps"`
	KPI      map[string]interface{}   `json:"kpi"`
	ToolRefs []string                 `json:"tool_refs"`
}

// SOPLoader SOP 加载器
type SOPLoader struct {
}

func NewSOPLoader() *SOPLoader {
	return &SOPLoader{}
}

func (l *SOPLoader) LoadSOP(ctx context.Context, sopID string) (*IndustrySOP, error) {
	if data, found := LoadAssetFromDB("industry_sop", sopID); found {
		var s IndustrySOP
		if err := json.Unmarshal(data, &s); err == nil {
			s.ID = sopID
			if len(s.Steps)+2 > maxNodesPerSOP {
				return nil, fmt.Errorf("sop exceeds max nodes (limit=%d)", maxNodesPerSOP)
			}
			return &s, nil
		}
		slog.Warn("SOPLoader invalid JSON, fallback", "asset_id", sopID)
	}
	return loadDefaultSOP(sopID)
}

func (l *SOPLoader) ListAllSOPs(ctx context.Context) ([]*IndustrySOP, error) {
	var result []*IndustrySOP
	rows, _ := ListAssetsFromDB("industry_sop")
	seen := map[string]bool{}
	for _, r := range rows {
		var s IndustrySOP
		if err := json.Unmarshal(r.Data, &s); err == nil {
			s.ID = r.AssetID
			s.Name = r.Name
			result = append(result, &s)
			seen[s.ID] = true
		}
	}
	for id, s := range defaultSOPs() {
		if !seen[id] {
			result = append(result, s)
		}
	}
	return result, nil
}

func loadDefaultSOP(id string) (*IndustrySOP, error) {
	if s, ok := defaultSOPs()[id]; ok {
		return s, nil
	}
	return nil, errors.New("sop not found: " + id)
}

func defaultSOPs() map[string]*IndustrySOP {
	return map[string]*IndustrySOP{
		"default-reception": {
			ID:       "default-reception",
			Name:     "默认客户接待 SOP",
			Category: "客户接待",
			Steps: []map[string]interface{}{
				{"order": 1, "name": "识别来源", "action": "tag_source"},
				{"order": 2, "name": "需求分析", "action": "ask_need"},
				{"order": 3, "name": "产品推荐", "action": "recommend"},
				{"order": 4, "name": "促成下单", "action": "close"},
			},
			KPI: map[string]interface{}{"conversion": ">15%"},
		},
	}
}

func mapActionToSOPNodeType(action string) string {
	switch action {
	case "tag_source":
		return "action"
	case "ask_need":
		return "inquire"
	case "recommend":
		return "introduce"
	case "close":
		return "close"
	default:
		return "message"
	}
}

func (s *IndustrySOP) ToCreateRequest(ctx context.Context, scenario string) *dto.CreateRequest {
	nodes := make([]dto.SOPNode, 0, len(s.Steps)+2)
	nodes = append(nodes, dto.SOPNode{ID: "start", Type: "start", Name: "开始"})
	prevID := "start"
	for i, step := range s.Steps {
		action, _ := step["action"].(string)
		name, _ := step["name"].(string)
		if name == "" {
			name = action
		}
		node := dto.SOPNode{
			ID:     fmt.Sprintf("n%d", i+1),
			Type:   mapActionToSOPNodeType(action),
			Name:   name,
			Prompt: name,
		}
		for idx := range nodes {
			if nodes[idx].ID == prevID {
				nodes[idx].Next = []string{node.ID}
				break
			}
		}
		nodes = append(nodes, node)
		prevID = node.ID
	}
	nodes = append(nodes, dto.SOPNode{ID: "end", Type: "end", Name: "结束"})
	for idx := range nodes {
		if nodes[idx].ID == prevID {
			nodes[idx].Next = []string{"end"}
			break
		}
	}
	desc := s.Industry
	if s.Category != "" {
		desc = desc + " / " + s.Category
	}
	return &dto.CreateRequest{
		Name:        s.Name,
		Scenario:    scenario,
		Description: desc,
		SOPGraph:    dto.SOPGraph{Entry: "start", Nodes: nodes},
	}
}
