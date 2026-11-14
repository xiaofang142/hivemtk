package agent_runtime

import (
	"context"
	"errors"
	"strings"

	"hivemtk-user/internal/aiagent/agent/portcontract"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// AssetBundleLoader 资产包加载器
//
// 实现 AssetLoader 接口（dataflow_orchestrator.go）
type AssetBundleLoader struct {
	weave    portcontract.AssetBundleWeavePort
	history  portcontract.ChatHistoryPort
	search   portcontract.KnowledgeSearchPort
	defaults LoaderDefaults
}

// LoaderDefaults 加载器默认配置
type LoaderDefaults struct {
	AssetID             string
	HistoryLimit        int
	RAGTopK             int
	MerchantVars        map[string]string
	StripFewShotJSON    bool
	IncludeMerchantVars bool
	RAGPosition         string
}

// NewAssetBundleLoader 创建资产包加载器
func NewAssetBundleLoader(
	weave portcontract.AssetBundleWeavePort,
	history portcontract.ChatHistoryPort,
	search portcontract.KnowledgeSearchPort,
	defaults LoaderDefaults,
) *AssetBundleLoader {
	if defaults.HistoryLimit <= 0 {
		defaults.HistoryLimit = 10
	}
	if defaults.RAGTopK <= 0 {
		defaults.RAGTopK = 5
	}
	if defaults.RAGPosition == "" {
		defaults.RAGPosition = "after_fewshots"
	}
	defaults.IncludeMerchantVars = true
	defaults.StripFewShotJSON = true
	return &AssetBundleLoader{
		weave:    weave,
		history:  history,
		search:   search,
		defaults: defaults,
	}
}

// LoadContext 加载资产上下文
//
// 流程（文档 §二）：
//  1. 解析 AssetID（优先从 AgentContext 取；缺失用 defaults.AssetID）
//  2. 拉取活跃会话历史（ChatHistoryPort）
//  3. 检索商户本地 RAG（KnowledgeSearchPort）
//  4. 调用 WeaveForRequest 拼装 messages（AssetBundleWeavePort）
//  5. 输出 AssetContext.Messages（OpenAI 兼容，可直接喂 LLM）
func (l *AssetBundleLoader) LoadContext(ctx context.Context, payload CustomerMessagePayload, agentCtx *AgentContext) (*AssetContext, error) {
	if l == nil || l.weave == nil {
		return nil, errors.New("asset bundle loader not configured")
	}

	assetID := l.resolveAssetID(agentCtx)
	if assetID == "" {
		logger.Warnf("[asset_loader] no asset_id resolved, fallback to noop session=%s", payload.SessionID)
		return noopAssetLoader{}.LoadContext(ctx, payload, agentCtx)
	}

	history := []model.AssetBundleMessage{}
	if l.history != nil && payload.SessionID != "" {
		h, err := l.history.FetchHistory(ctx, payload.SessionID, l.defaults.HistoryLimit)
		if err != nil {
			logger.Warnf("[asset_loader] fetch history failed session=%s err=%v", payload.SessionID, err)
		} else {
			history = h
		}
	}

	ragDocs := []portcontract.RAGDocumentPort{}
	if l.search != nil && payload.Content != "" {
		docs, err := l.search.Search(ctx, payload.Content, l.defaults.RAGTopK)
		if err != nil {
			logger.Warnf("[asset_loader] rag search failed session=%s err=%v", payload.SessionID, err)
		} else {
			ragDocs = docs
		}
	}

	merchantVars := l.resolveMerchantVars(agentCtx)

	weaveReq := portcontract.WeaveRequestPort{
		AssetID:      assetID,
		UserQuery:    payload.Content,
		RAGDocs:      ragDocs,
		ChatHistory:  history,
		MerchantVars: merchantVars,
		Options: portcontract.WeaveOptionsPort{
			RAGPosition:         l.defaults.RAGPosition,
			MaxHistoryMessages:  l.defaults.HistoryLimit,
			StripFewShotJSON:    l.defaults.StripFewShotJSON,
			IncludeMerchantVars: l.defaults.IncludeMerchantVars,
		},
	}
	messages, err := l.weave.WeaveForRequest(ctx, weaveReq)
	if err != nil {
		if errors.Is(err, portcontract.ErrAssetBundleNotEnabled) || errors.Is(err, portcontract.ErrAssetBundleNotFound) {
			logger.Warnf("[asset_loader] bundle not available asset=%s err=%v, fallback noop", assetID, err)
			return noopAssetLoader{}.LoadContext(ctx, payload, agentCtx)
		}
		return nil, err
	}

	logger.Infof("[asset_loader] ok asset=%s msgs=%d rag=%d hist=%d session=%s",
		assetID, len(messages), len(ragDocs), len(history), payload.SessionID)

	promptText := ""
	for _, m := range messages {
		if m.Role == "system" {
			promptText = m.Content
			break
		}
	}
	_ = assetID
	_ = messages
	_ = ragDocs
	_ = merchantVars
	return &AssetContext{
		L1ShortTerm: map[string]string{},
		L2Profile:   map[string]string{},
		PromptText:  promptText,
		SystemTools: []string{},
	}, nil
}

func (l *AssetBundleLoader) resolveAssetID(agentCtx *AgentContext) string {
	// 审计二轮修复：此前把 agentCtx.Persona（自由文本人设）当 AssetID 用，
	// Persona 非空时必然查不到包。正确次序 = 显式绑定字段 → 智能体编码兜底 → 默认值。
	if agentCtx != nil {
		if agentCtx.AssetBundleID != "" {
			return agentCtx.AssetBundleID
		}
		if agentCtx.AgentCode != "" {
			return agentCtx.AgentCode
		}
	}
	if l.defaults.AssetID != "" {
		return l.defaults.AssetID
	}
	return ""
}

func (l *AssetBundleLoader) resolveMerchantVars(agentCtx *AgentContext) map[string]string {
	out := make(map[string]string)
	for k, v := range l.defaults.MerchantVars {
		out[k] = v
	}
	if agentCtx != nil && agentCtx.SystemPrompt != "" {
		for _, kv := range strings.Split(agentCtx.SystemPrompt, ";") {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) != 2 {
				continue
			}
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			if k != "" && v != "" {
				out[k] = v
			}
		}
	}
	return out
}

var _ AssetLoader = (*AssetBundleLoader)(nil)
