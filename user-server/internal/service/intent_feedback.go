package service

// intent_feedback.go TB-09：意图识别纠错反馈闭环。
//
// 链路：错误识别日志（intent_logs）→ 管理端纠错（改判意图）→ 原始话术作为
// 词面写入意图词面覆盖（intent_keywords_override KV，SaveIntentKeywordOverride
// 热更新）→ 下次同话术直接规则命中（intentsForRule 合并覆盖词表）→ 命中率可观测。
//
// 防有害循环（业界口径 review §四[7]）：纠错只进**人工确认面**（管理端操作），
// 不自动回写模型输出；纠正目标必须是词典内已知意图（默认词典或启用中的自定义词条）。

import (
	"context"
	"fmt"
	"strings"

	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// IntentFeedbackService 意图纠错反馈服务。
type IntentFeedbackService struct {
	logRepo       *repository.IntentLogRepository
	customIntents *CustomIntentService
}

func NewIntentFeedbackService() *IntentFeedbackService {
	return &IntentFeedbackService{
		logRepo: repository.NewIntentLogRepository(),
	}
}

// SetCustomIntentService 注入自定义词条服务（纠正目标包含自定义意图；nil-safe）。
func (s *IntentFeedbackService) SetCustomIntentService(svc *CustomIntentService) {
	s.customIntents = svc
}

// IntentFeedbackCorrectInput 纠错入参。
type IntentFeedbackCorrectInput struct {
	LogID         uint   `json:"log_id" binding:"required"`
	CorrectedType string `json:"corrected_type" binding:"required"`
	// Keyword 覆盖写入的词面（缺省取日志原文 raw_text）
	Keyword string `json:"keyword"`
	// Operator 操作者（管理面留痕）
	Operator string `json:"operator"`
}

// CorrectIntent 纠错一条识别日志：原文词面写进纠正意图的覆盖词表（立即热生效）。
func (s *IntentFeedbackService) CorrectIntent(ctx context.Context, in IntentFeedbackCorrectInput) (map[string]any, error) {
	if in.Operator == "" {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "操作者必填（留痕 fail-closed）")
	}
	rec, err := s.logRepo.GetByID(ctx, in.LogID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, bizerr.New(bizerr.CodeNotFound, "识别日志不存在")
	}
	if !s.isValidCorrectionTarget(ctx, in.CorrectedType) {
		return nil, bizerr.New(bizerr.CodeParamInvalid, fmt.Sprintf("纠正目标 %q 不是已知意图（默认词典或启用中的自定义词条）", in.CorrectedType))
	}
	if rec.IntentMajor == in.CorrectedType {
		return nil, bizerr.New(bizerr.CodeConflict, fmt.Sprintf("日志已是 %s，无需纠错", in.CorrectedType))
	}
	keyword := strings.TrimSpace(in.Keyword)
	if keyword == "" {
		keyword = strings.TrimSpace(rec.Message)
	}
	if keyword == "" {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "无可写入词面（日志原文为空且未提供 keyword）")
	}

	override, err := LoadIntentKeywordOverride(ctx)
	if err != nil {
		return nil, err
	}
	if override == nil {
		override = map[string][]string{}
	}
	seen := make(map[string]bool, len(override[in.CorrectedType]))
	for _, kw := range override[in.CorrectedType] {
		seen[kw] = true
	}
	if !seen[keyword] {
		override[in.CorrectedType] = append(override[in.CorrectedType], keyword)
	}
	if err := SaveIntentKeywordOverride(ctx, override); err != nil {
		return nil, err
	}
	logger.Infof("[intent_feedback] 纠错落库：log=%d %q→%s keyword=%q operator=%s", in.LogID, rec.IntentMajor, in.CorrectedType, keyword, in.Operator)
	return map[string]any{
		"log_id":         in.LogID,
		"from_intent":    rec.IntentMajor,
		"to_intent":      in.CorrectedType,
		"keyword":        keyword,
		"effective_mode": "override_kv_hot_reload",
	}, nil
}

// ListOverride 当前词面覆盖（管理端回显）。
func (s *IntentFeedbackService) ListOverride(ctx context.Context) (map[string][]string, error) {
	return LoadIntentKeywordOverride(ctx)
}

// isValidCorrectionTarget 纠正目标合法性：默认词典 ∪ 启用中的自定义词条。
func (s *IntentFeedbackService) isValidCorrectionTarget(ctx context.Context, intentType string) bool {
	for _, def := range DefaultIntents {
		if def.Type == intentType {
			return true
		}
	}
	if s.customIntents != nil {
		return s.customIntents.IsCustomIntentType(ctx, intentType)
	}
	return false
}
