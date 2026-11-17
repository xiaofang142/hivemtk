package service

import (
	"context"
	"strings"
	"unicode"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// faqFastQuestionSafe 是快速路径资格，不是代替意图/投诉判定的分类器。
// 不确定问题让完整链处理，不能通过关键词分把额外要求当成已覆盖。
func faqFastQuestionSafe(question string) bool {
	return !MatchTransferKeywords(question) && !MatchExplicitKeywords(question) &&
		!MatchUrgentKeywords(question) && ClassifyEmotion(question) == EmotionNeutral
}

func normalizedFAQQuestion(question string) string {
	value := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, question)
	return strings.TrimRight(value, "?？。.!！")
}

func faqQuestionCovered(question, standard string) bool {
	q, s := normalizedFAQQuestion(question), normalizedFAQQuestion(standard)
	if q == "" || s == "" {
		return false
	}
	if q == s {
		return true
	}
	// 只有完整基础介绍短句与“是什么”标准问等价，不能接受任意介绍前缀。
	switch q {
	case "介绍下项目", "介绍一下项目", "介绍下产品", "介绍一下产品", "项目介绍", "产品介绍":
		// 保留本项目既有基础介绍契约；无产品主体映射时，不猜其他定义等价。
		return s == "hivemtk是什么" || s == "项目是什么" || s == "产品是什么"
	default:
		return false
	}
}

// faqHasPriorAnswer 只读取权威会话范围；缺身份/读取失败不走免检快答。
// 生成过不代表交付成功，此处仅决定是否需要上下文，不吞客户消息。
func (o *SmartCSOrchestrator) faqHasPriorAnswer(ctx context.Context, in *IncomingContext, session *model.CustomerSession) bool {
	if o.messageRepo == nil || in == nil || session == nil || session.SessionID == "" ||
		session.Platform != in.Platform || session.AccountID != in.AccountID {
		return true
	}
	tenantID, _ := tenant.FromContext(ctx)
	if tenantID == 0 {
		if tenant.IsMultiTenant() {
			return true
		}
		tenantID = tenant.DefaultTenantID
	}
	if session.TenantID != 0 && session.TenantID != tenantID {
		return true
	}
	rows, err := o.messageRepo.ListRecentDescBySessionID(ctx, session.SessionID, 20)
	if err != nil {
		return true
	}
	for _, row := range rows {
		if row.TenantID != tenantID {
			return true
		}
		if !row.IsInternal && strings.TrimSpace(row.Content) != "" &&
			(row.SenderType == "ai" || row.SenderType == "agent" || row.SenderType == "human") {
			return true
		}
	}
	return false
}
