package service

import (
	"context"
	"errors"
	"fmt"
	"hivemtk-user/internal/aiagent/llm"
	rag_core "hivemtk-user/internal/aiagent/rag/core"
	rag_service "hivemtk-user/internal/aiagent/rag/service"
	"hivemtk-user/internal/model"
	"strings"
	"time"
)

// GetAccountConfig 获取账号配置
func (s *RagConfigService) GetAccountConfig(ctx context.Context, accountID, platform string) (*model.PlatformAccountConfig, error) {
	config, err := s.repo.GetAccountConfig(ctx, accountID, platform)
	if err != nil {
		return nil, err
	}

	return config, nil
}

// UpdateAccountConfig 更新账号配置
func (s *RagConfigService) UpdateAccountConfig(ctx context.Context, req *model.PlatformAccountConfig) error {
	// 平台白名单与 channelgw/registry.go 桥接渠道注册表对齐（2026-11-12 收口）：
	// weibo/bilibili 等桥接渠道此前不在清单内，账号级 RAG/自动回复配置无法开启。
	// tiktok/jd/pdd/personal_wx 同批补齐，消除「注册表已注册、配置白名单缺失」的镜像缺口。
	validPlatforms := []string{
		"douyin", "kuaishou", "xiaohongshu", "xianyu", "tiktok",
		"jd", "bilibili", "weibo", "personal_wx", "pdd",
	}
	isValidPlatform := false
	for _, p := range validPlatforms {
		if req.Platform == p {
			isValidPlatform = true
			break
		}
	}
	if !isValidPlatform {
		return errors.New("invalid platform, must be one of: douyin, kuaishou, xiaohongshu, xianyu, tiktok, jd, bilibili, weibo, personal_wx, pdd")
	}

	if req.IsRagEnabled && req.RagProductID != nil {
		_, err := s.repo.GetRagProductByID(ctx, *req.RagProductID)
		if err != nil {
			return fmt.Errorf("rag product not found: %w", err)
		}
	}

	req.UpdatedAt = time.Now()

	return s.repo.UpsertAccountConfig(ctx, req)
}

// ProcessMessage 处理消息
func (s *RagConfigService) ProcessMessage(ctx context.Context, platform, accountID, message string) (string, error) {
	config, err := s.GetAccountConfig(ctx, accountID, platform)
	if err != nil {
		return "", fmt.Errorf("failed to get account config: %w", err)
	}

	if !config.IsAutoReplyEnabled {
		return "", errors.New("auto reply is not enabled for this account")
	}

	if config.IsRagEnabled && config.RagProductID != nil {
		ragReply, err := s.processWithRag(ctx, *config.RagProductID, message, platform, accountID)
		if err != nil {
			return "", fmt.Errorf("failed to process with rag: %w", err)
		}
		return ragReply, nil
	}

	ruleReply := s.generateRuleBasedReply(config.ReplyRules, message)
	if ruleReply != "" {
		return ruleReply, nil
	}

	return "感谢您的消息，我们会尽快回复您！", nil
}

func (s *RagConfigService) processWithRag(ctx context.Context, productID, message, platform, accountID string) (string, error) {
	product, err := s.repo.GetRagProductByID(ctx, productID)
	if err != nil {
		return "", fmt.Errorf("failed to get rag product: %w", err)
	}

	if product == nil {
		return "", errors.New("rag product not found")
	}

	llmConfig := &llm.LLMConfig{
		APIKey:           product.LLMProviderConfig.APIKey,
		BaseURL:          product.LLMProviderConfig.BaseURL,
		APIType:          product.LLMProviderConfig.APIType,
		Model:            product.LLMProviderConfig.Model,
		MaxRetries:       product.LLMProviderConfig.MaxRetries,
		RequestTimeout:   product.LLMProviderConfig.RequestTimeout,
		Temperature:      product.Temperature,
		MaxTokens:        product.MaxTokens,
		TopP:             product.TopP,
		FrequencyPenalty: product.FrequencyPenalty,
		PresencePenalty:  product.PresencePenalty,
		ResponseFormat:   product.ResponseFormat,
		SystemPrompt:     product.SystemPrompt,
	}

	ragConfig := &rag_core.RAGConfig{
		ChunkSize:           512,
		ChunkOverlap:        50,
		MaxChunksToRetrieve: DefaultTopK(),
		SimilarityThreshold: DefaultSimilarityThreshold(),
		VectorDimension:     EmbeddingDim(),
	}

	queryReq := &rag_service.QueryRequest{
		Query:     message,
		RAGConfig: ragConfig,
		LLMConfig: llmConfig,
		Context: map[string]any{
			"platform":  platform,
			"accountID": accountID,
			"productID": productID,
		},
	}

	response, err := s.ragService.Query(ctx, queryReq)
	if err != nil {
		return "", fmt.Errorf("failed to execute rag query: %w", err)
	}

	return response.Answer, nil
}

func (s *RagConfigService) generateRuleBasedReply(rules []model.ReplyRule, message string) string {
	lowerMsg := strings.ToLower(message)

	for _, rule := range rules {
		if !rule.IsActive {
			continue
		}

		for _, keyword := range rule.Keywords {
			if strings.Contains(lowerMsg, strings.ToLower(keyword)) {
				return rule.ReplyTemplate
			}
		}
	}

	return ""
}

func (s *RagConfigService) llmServiceConfigured(product *model.RagProduct) bool {
	if product == nil {
		return false
	}
	cfg := product.LLMProviderConfig
	if cfg.APIKey == "" && cfg.BaseURL == "" {
		return false
	}
	return true
}
