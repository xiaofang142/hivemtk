package service

import (
	"context"

	"hivemtk-user/internal/aiagent/knowledge/kbsearch"
	"runtime/debug"

	"hivemtk-user/internal/aiagent/llm"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	"hivemtk-user/internal/pkg/utils/logger"
)

// contextualEnhanceProvider 是「上下文增强（R-3）总开关」的取值口。
// SaaS 配置面改造（B16，2026-11-17）：RAG_CONTEXTUAL_ENHANCE 环境变量退役，
// 改由参数中心 knowledge.contextual_enhance_enabled 经 provider 注入
// （装配点 internal/app；knowledge 包不反向依赖 service，走 seam 范式）。
// 默认 true（与退役前 env 未设时的语义一致）。
var contextualEnhanceProvider = func() bool { return true }

// SetContextualEnhanceProvider 注入取值口。传 nil 是空操作而不是复位。
func SetContextualEnhanceProvider(fn func() bool) {
	if fn != nil {
		contextualEnhanceProvider = fn
	}
}

func contextualEnhanceEnabled() bool { return contextualEnhanceProvider() }

func (s *KnowledgeService) enhanceDocumentContextual(ctx context.Context, documentID uint64) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[R-3] contextual enhance panic doc=%d: %v\n%s", documentID, r, debug.Stack())
		}
	}()
	if s.chunkRepo == nil || !s.chunkRepo.Connected() || s.llmSvc == nil || s.embeddingSvc == nil {
		return
	}
	chatClient := ragretrieval.NewDispatcherChatAdapter(llm.NewDispatcher(s.llmSvc)).
		WithScenario(llm.ScenarioLongSummary)
	enhancer := kbsearch.NewContextualEnhancerFromRepo(s.chunkRepo, chatClient, s.embeddingSvc, nil)
	if err := enhancer.EnhanceDocument(ctx, documentID); err != nil {
		logger.Warnf("[R-3] contextual enhance failed doc=%d (non-blocking): %v", documentID, err)
		return
	}
	logger.Infof("[R-3] contextual enhance done doc=%d", documentID)
}
