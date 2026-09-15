package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
)

// AgentPersona 运行时人设
type AgentPersona struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	Industry          string                 `json:"industry"`
	SystemPrompt      string                 `json:"system_prompt"`
	Persona           map[string]interface{} `json:"persona"`
	GreetingTemplates []string               `json:"greeting_templates"`
	ObjectionHandlers []map[string]string    `json:"objection_handlers"`
	ToolWhitelist     []string               `json:"tool_whitelist"`
	KBRefs            []string               `json:"kb_refs"`
	DefaultTuning     map[string]interface{} `json:"default_tuning"`
	BasicInfo         map[string]interface{} `json:"basic_info"`
	QAPairs           []AssetQAPair          `json:"qa_pairs"`
}

// AssetQAPair 资产包 Q&A 样本
type AssetQAPair struct {
	ID       int    `json:"id"`
	Category string `json:"category"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// AgentLoader 智能体人设加载器
type AgentLoader struct {
}

func NewAgentLoader() *AgentLoader {
	return &AgentLoader{}
}

// LoadPersona 优先 DB，失败回退代码默认
func (l *AgentLoader) LoadPersona(ctx context.Context, personaID string) (*AgentPersona, error) {
	if data, found := LoadAssetFromDB("agent_persona", personaID); found {
		var p AgentPersona
		if err := json.Unmarshal(data, &p); err == nil {
			p.ID = personaID
			slog.Info("AgentLoader loaded from DB", "asset_id", personaID)
			return &p, nil
		}
		slog.Warn("AgentLoader invalid JSON, fallback", "asset_id", personaID)
	}
	slog.Info("AgentLoader fallback to default", "asset_id", personaID)
	return loadDefaultAgentPersona(personaID)
}

// ListAllPersonas DB ∪ 代码默认
func (l *AgentLoader) ListAllPersonas(ctx context.Context) ([]*AgentPersona, error) {
	var result []*AgentPersona
	rows, _ := ListAssetsFromDB("agent_persona")
	seen := map[string]bool{}
	for _, r := range rows {
		var p AgentPersona
		if err := json.Unmarshal(r.Data, &p); err == nil {
			p.ID = r.AssetID
			p.Name = r.Name
			result = append(result, &p)
			seen[p.ID] = true
		}
	}
	for id, p := range defaultAgentPersonas() {
		if !seen[id] {
			result = append(result, p)
		}
	}
	return result, nil
}

func loadDefaultAgentPersona(personaID string) (*AgentPersona, error) {
	defaults := defaultAgentPersonas()
	if p, ok := defaults[personaID]; ok {
		return p, nil
	}
	return nil, errors.New("agent persona not found: " + personaID)
}

func defaultAgentPersonas() map[string]*AgentPersona {
	return map[string]*AgentPersona{
		"default-sales": {
			ID:           "default-sales",
			Name:         "默认销售助手",
			SystemPrompt: "你是一位专业的销售助手,擅长倾听客户需求,提供准确的产品信息。",
			Persona: map[string]interface{}{
				"tone":      "专业、热情",
				"expertise": []string{"产品咨询", "价格答疑"},
			},
			ToolWhitelist: []string{"query_product", "recommend_sku", "check_stock"},
			DefaultTuning: map[string]interface{}{
				"temperature": 0.7, "max_tokens": 512, "react_max_rounds": 5,
			},
		},
		"default-service": {
			ID:           "default-service",
			Name:         "默认客服",
			SystemPrompt: "你是一位专业的客服,耐心解答用户问题。",
			Persona: map[string]interface{}{
				"tone":      "耐心、亲切",
				"expertise": []string{"售后问题", "使用指导"},
			},
			ToolWhitelist: []string{"query_order", "check_status"},
			DefaultTuning: map[string]interface{}{
				"temperature": 0.5, "max_tokens": 512, "react_max_rounds": 3,
			},
		},
	}
}
