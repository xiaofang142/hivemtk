package service

import (
	"context"

	"fmt"

	"hivemtk-user/internal/dto"

	"hivemtk-user/internal/model"
)

type RAGSearcher interface {
	Search(ctx context.Context, query string, topK int) ([]RAGChunk, error)
}

// RAGProductSearcher 分产品检索（可选能力）。
// RagSearcher 已实现；fake/第三方实现未实现时自动回退全局 Search。
type RAGProductSearcher interface {
	SearchProducts(ctx context.Context, productIDs []string, query string, topK int) ([]RAGChunk, error)
}

type RAGChunk = dto.RAGChunk

type ScriptLookup interface {
	MatchScript(ctx context.Context, intent string, scenario string) (*ScriptTemplate, error)
}

type ScriptTemplate = dto.ScriptTemplate

type CustomerLookup interface {
	GetByOneID(ctx context.Context, oneID string) (*model.Customer, error)
	GetByID(ctx context.Context, id string) (*model.Customer, error)
}

type IntentRecognizerInterface interface {
	Recognize(ctx context.Context, sessionID, customerID, text string) (*dto.RecognizeResult, error)
}

type DialogueMemoryInterface interface {
	AppendMessage(ctx context.Context, sessionID, customerID string, msg dto.Message) error
	GetOrCreateMemory(ctx context.Context, sessionID, customerID string) (*model.DialogueMemory, error)
}

type SOPMatcherInterface interface {
	MatchByIntent(ctx context.Context, intentType string) ([]model.SOPAgent, error)
}

func (e *SalesEngine) resolveCustomer(ctx context.Context, req *SalesRequest) (*model.Customer, error) {
	if e.customerLookup == nil {
		return nil, fmt.Errorf("customer_lookup is nil")
	}
	if req.OneID != "" {
		c, err := e.customerLookup.GetByOneID(ctx, req.OneID)
		if err == nil && c != nil {
			return c, nil
		}
	}
	if req.CustomerID != "" {
		return e.customerLookup.GetByID(ctx, req.CustomerID)
	}
	return nil, fmt.Errorf("no customer identifier provided")
}

func (e *SalesEngine) recallMemory(ctx context.Context, req *SalesRequest) (*model.DialogueMemory, error) {
	if e.memory == nil {
		return &model.DialogueMemory{}, nil
	}
	return e.memory.GetOrCreateMemory(ctx, req.SessionID, req.CustomerID)
}

func (e *SalesEngine) matchSOP(ctx context.Context, intent *dto.RecognizeResult, customer *model.Customer) (*model.SOPAgent, string, error) {
	if e.sop == nil || intent == nil {
		return nil, "", nil
	}
	if intent.IntentType == IntentUnknown {
		return nil, "", nil
	}
	stage := "default"
	if customer != nil && customer.ChurnRisk != "" {
		switch customer.ChurnRisk {
		case "high":
			stage = "churn_risk"
		case "low":
			stage = "active"
		}
	}
	sops, err := e.sop.MatchByIntent(ctx, intent.IntentType)
	if err != nil || len(sops) == 0 {
		return nil, stage, nil
	}

	return &sops[0], stage, nil
}

func (e *SalesEngine) recallRAG(ctx context.Context, req *SalesRequest, intent *dto.RecognizeResult) ([]RAGChunk, error) {
	if !req.Config.EnableRAG || e.ragSearcher == nil {
		return nil, nil
	}
	// 优先按智能体绑定的 rag_product_ids 分产品召回（隔离），无命中再回退全局。
	if pids := dto.SalesRequestRagProductIDs(req); len(pids) > 0 {
		if ps, ok := e.ragSearcher.(RAGProductSearcher); ok {
			if chunks, err := ps.SearchProducts(ctx, pids, req.UserMessage, req.Config.RAGTopK); err == nil && len(chunks) > 0 {
				return chunks, nil
			}
		}
	}
	return e.ragSearcher.Search(ctx, req.UserMessage, req.Config.RAGTopK)
}

func (e *SalesEngine) matchScript(ctx context.Context, intent *dto.RecognizeResult) (*ScriptTemplate, error) {
	if e.scriptLookup == nil || intent == nil {
		return nil, nil
	}
	script, err := e.scriptLookup.MatchScript(ctx, intent.IntentType, intent.IntentName)
	if err != nil {
		return nil, err
	}
	if script != nil {
		return script, nil
	}

	if r := GetAssetResolver(); r != nil {
		if s, ok := r.GetActiveScript(ctx); ok && s != nil {
			return &ScriptTemplate{
				ID:       s.ID,
				Title:    s.Name,
				Scenario: s.Scenario,
				Content:  renderSalesScriptSteps(s.Scripts),
				Tags:     []string{"asset-market", s.ID},
			}, nil
		}
	}
	return nil, nil
}
