package service

import (
	"context"

	"fmt"

	"hivemtk-user/internal/aiagent/llm"

	"hivemtk-user/internal/dto"

	"hivemtk-user/internal/model"

	"strings"
)

type PlaybookRecommenderInterface interface {
	RecommendForResponse(ctx context.Context, industry Industry, productID string, stage JourneyStage, intent string) []*PlaybookEntry
}

func renderSalesScriptSteps(scripts []map[string]interface{}) string {
	if len(scripts) == 0 {
		return ""
	}
	var b strings.Builder
	for i, st := range scripts {
		if i > 0 {
			b.WriteString("\n")
		}
		if name, ok := st["name"].(string); ok && name != "" {
			b.WriteString("【" + name + "】\n")
		}
		if content, ok := st["content"].(string); ok {
			b.WriteString(content)
		}
	}
	return b.String()
}

// SetPlaybook 注入销冠话术库（可选）
// 重构：参数改为 PlaybookRecommenderInterface，可注入测试替身实现进行单元测试
func (e *SalesEngine) SetPlaybook(ctx context.Context, p PlaybookRecommenderInterface) {
	e.playbook = p
}

// RecommendPlaybook 推荐话术（销售辅助模式专用入口）
// 商业产品级场景：销售收到客户消息后，点"获取建议"按钮调用
//   - industry: 推断或由前端传入（从客户档案/产品配置获取）
//   - productID: 产品ID
//   - stage: 客户当前旅程阶段
//   - intent: 当前意图（用于推断异议类型）
//
// 返回 3-5 条按成功率排序的话术建议
func (e *SalesEngine) RecommendPlaybook(ctx context.Context, industry Industry, productID string, stage JourneyStage, intent string) []*PlaybookEntry {
	if e.playbook == nil {
		return nil
	}
	return e.playbook.RecommendForResponse(ctx, industry, productID, stage, intent)
}

func (e *SalesEngine) fetchPlaybookSuggestions(ctx context.Context, industry Industry, productID string, stage JourneyStage, intent string) []*PlaybookEntry {
	if e.playbook == nil {
		return nil
	}
	return e.playbook.RecommendForResponse(ctx, industry, productID, stage, intent)
}

func (e *SalesEngine) generateCandidate(
	ctx context.Context,
	req *SalesRequest,
	intent *dto.RecognizeResult,
	mem *model.DialogueMemory,
	sop *model.SOPAgent,
	stage string,
	ragChunks []RAGChunk,
	script *ScriptTemplate,
	customer *model.Customer,
) (string, *llm.DispatchResult, []model.RichCard, error) {

	targetLang := e.resolveTargetLang(ctx, req.UserMessage)

	if e.layerRouter != nil {
		decision := e.layerRouter.Route(ctx, &RouteRequest{
			SessionID:   req.SessionID,
			CustomerID:  req.CustomerID,
			UserMessage: req.UserMessage,
			Intent:      intent,
			RAGChunks:   ragChunks,
			Stage:       stage,

			AgentID: agentIDFromCtx(req),
		})
		if decision != nil && decision.SkipLLM && decision.Reply != "" {

			return e.calibrate(ctx, decision.Reply, targetLang), nil, nil, nil
		}
	}

	if e.dispatcher == nil {
		return "", nil, nil, fmt.Errorf("dispatcher is nil")
	}

	agentPrompt := req.UserMessage
	prompt := e.buildPrompt(req, intent, mem, sop, stage, ragChunks, script, customer)
	scenario := llm.ScenarioSOPReply
	if intent != nil && intent.IntentType == IntentObjectionPrice {
		scenario = llm.ScenarioObjection
	} else if intent != nil && (intent.IntentType == IntentSocial || intent.IntentType == IntentGreeting) {
		scenario = llm.ScenarioFriendlyChat
	}

	if e.toolExecutor != nil {
		availableTools := e.toolExecutor.ListTools()
		if len(availableTools) > 0 {
			return e.runAgentLoop(ctx, scenario, agentPrompt, req, intent, mem, customer, availableTools, ragChunks)
		}
	}

	result, err := e.dispatcher.Dispatch(ctx, llm.DispatchRequest{
		Scenario:     scenario,
		Prompt:       prompt,
		SystemPrompt: e.personaWithLang(ctx, req.Config.Persona, targetLang),
		MaxTokens:    req.Config.MaxTokens,
		Temperature:  req.Config.Temperature,
		CacheKey:     llm.CacheKey(scenario, prompt),
		CacheTTL:     3600,
	})
	if err != nil {
		return "", nil, nil, err
	}
	reply := strings.TrimSpace(result.Content)
	// 空 LLM 回复（finish_reason=length 截断/模型异常）不降级的话用户会收到空消息，
	// 且空串会被 dispatcher 按 CacheKey 缓存 1 小时，同 prompt 持续命中空回复
	if reply == "" {
		reply = e.emptyReplyFallback()
	}
	return e.calibrate(ctx, reply, targetLang), result, nil, nil
}
