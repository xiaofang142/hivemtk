package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

)

// SalesScript 销冠话术
type SalesScript struct {
	ID        string                   `json:"id"`
	Name      string                   `json:"name"`
	Industry  string                   `json:"industry"`
	Scenario  string                   `json:"scenario"`
	Scripts   []map[string]interface{} `json:"scripts"`
	Variables []string                 `json:"variables"`
}

// ScriptLoader 话术加载器
type ScriptLoader struct {
}

func NewScriptLoader() *ScriptLoader {
	return &ScriptLoader{}
}

func (l *ScriptLoader) LoadScript(ctx context.Context, scriptID string) (*SalesScript, error) {
	if data, found := LoadAssetFromDB("sales_script", scriptID); found {
		var s SalesScript
		if err := json.Unmarshal(data, &s); err == nil {
			s.ID = scriptID
			return &s, nil
		}
		slog.Warn("ScriptLoader invalid JSON, fallback", "asset_id", scriptID)
	}
	return loadDefaultScript(scriptID)
}

func (l *ScriptLoader) ListAllScripts(ctx context.Context) ([]*SalesScript, error) {
	var result []*SalesScript
	rows, _ := ListAssetsFromDB("sales_script")
	seen := map[string]bool{}
	for _, r := range rows {
		var s SalesScript
		if err := json.Unmarshal(r.Data, &s); err == nil {
			s.ID = r.AssetID
			s.Name = r.Name
			result = append(result, &s)
			seen[s.ID] = true
		}
	}
	for id, s := range defaultScripts() {
		if !seen[id] {
			result = append(result, s)
		}
	}
	return result, nil
}

func loadDefaultScript(id string) (*SalesScript, error) {
	if s, ok := defaultScripts()[id]; ok {
		return s, nil
	}
	return nil, errors.New("script not found: " + id)
}

func defaultScripts() map[string]*SalesScript {
	return map[string]*SalesScript{
		"default-first-order": {
			ID:       "default-first-order",
			Name:     "默认首单转化话术",
			Scenario: "首单转化",
			Scripts: []map[string]interface{}{
				{"step": 1, "name": "破冰", "content": "您好，请问有什么可以帮到您？"},
				{"step": 2, "name": "需求探询", "content": "方便了解一下您的具体需求吗？"},
				{"step": 3, "name": "促成", "content": "目前有优惠活动，要不要现在下单？"},
			},
		},
	}
}
