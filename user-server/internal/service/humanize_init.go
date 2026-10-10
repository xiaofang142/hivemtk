package service

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"

	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/config"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	humanizesvc "hivemtk-user/internal/service/humanize"
)

var (
	humanizeEvalServiceOnce sync.Once
	humanizeEvalService     *humanizesvc.HumanizeEvalService
)

type humanizeLLMAdapter struct {
	dispatcher *llm.Dispatcher
}

func (a *humanizeLLMAdapter) ChatSend(ctx context.Context, prompt string) (string, string, error) {
	if a.dispatcher == nil {
		return "", "", nil
	}
	result, err := a.dispatcher.Dispatch(ctx, llm.DispatchRequest{
		Scenario:  llm.ScenarioObjection,
		Prompt:    prompt,
		MaxTokens: 256,
	})
	if err != nil {
		return "", "", err
	}
	return result.Content, result.Model, nil
}

// HumanizeEvaluatorSwitch 拟人度评估器开关（私域本地 LLM 部署下应禁用）
//
// 背景（私域部署 修复）：
// 拟人度评估器设计用于商业 SaaS 场景，用于拦截 AI 痕迹过重的回复
//   - 但 1.5B q4 本地 LLM 推理在 CPU 上的回复拟人度普遍 < 0.85
//     （受模型能力、生成 token 长度限制），硬走 0.85 阈值会导致
//     "3 次重生成仍失败 → 转人工"，AI 实际无自动回复
//   - 私域场景客户期望：LLM 推理成功即应自动回复，由真实人工按需接管
//
// 用法：
// 私域本地 LLM 部署：设 MTK_HUMANIZE_EVAL_DISABLED=true 强制禁用（或自动检测本地 base_url 后禁用）
//   - 商业云端 SaaS：默认启用（保持商业场景的回复质量拦截）
//   - 自定义阈值: 设 MTK_HUMANIZE_EVAL_THRESHOLD=0.50
//
// 5 层架构：本开关在 Service 层，Router/Factory 层读取 env 后通过
// SetHumanizeEvaluatorEnabled 注入。
var HumanizeEvaluatorEnabled = true

// InitHumanizeEvalService 初始化拟人度评估器（启动时调用一次）
//
// 依赖：
//   - db:         GORM DB（销冠基线 / 评分持久化）
//   - dispatcher: LLM 调度器
func InitHumanizeEvalService(db *gorm.DB, dispatcher *llm.Dispatcher) *humanizesvc.HumanizeEvalService {
	humanizeEvalServiceOnce.Do(func() {
		if v := os.Getenv("MTK_HUMANIZE_EVAL_DISABLED"); v != "" {
			if disabled, _ := strconv.ParseBool(v); disabled {
				HumanizeEvaluatorEnabled = false
				logger.Info("[humanize] MTK_HUMANIZE_EVAL_DISABLED=true，拟人度评估器已禁用（私域本地 LLM 模式）")
			}
		}
		if HumanizeEvaluatorEnabled {
			llmBaseURL := config.GetAppConfig().Inference.LLM.BaseURL
			if isLocalLLMBaseURL(llmBaseURL) {
				HumanizeEvaluatorEnabled = false
				logger.Infof("[humanize] 检测到本地 LLM（base_url=%s），拟人度评估器自动禁用（本地小模型拟人度 <0.85，避免无效重生成）", llmBaseURL)
			}
		}
		if HumanizeEvaluatorEnabled {
			logger.Info("[humanize] 拟人度评估器已启用（线上模式）")
		}
		ruleScorer := humanizesvc.NewRuleScorer()
		llmScorer := humanizesvc.NewLLMScorer(&humanizeLLMAdapter{dispatcher: dispatcher})

		baselineRepo := repository.NewChampionBaselineRepository()
		scoreRepo := repository.NewHumanizeScoreRepository()
		sampleCollector := repository.NewHumanizeLowQualitySampleCollector()

		svc := humanizesvc.NewHumanizeEvalService(
			ruleScorer,
			llmScorer,
			baselineRepo,
			scoreRepo,
			sampleCollector,
		)
		if v := os.Getenv("MTK_HUMANIZE_EVAL_THRESHOLD"); v != "" {
			if thr, perr := strconv.ParseFloat(strings.TrimSpace(v), 64); perr == nil && thr > 0 && thr <= 1 {
				svc = svc.WithThreshold(context.Background(), thr)
				logger.Infof("[humanize] MTK_HUMANIZE_EVAL_THRESHOLD 生效 threshold=%.2f", thr)
			} else {
				logger.Warnf("[humanize] MTK_HUMANIZE_EVAL_THRESHOLD=%q 非法（须 0<thr<=1），使用 %.2f", v, humanizesvc.EffectiveThreshold())
			}
		}
		humanizeEvalService = svc
	})
	return humanizeEvalService
}

// GetHumanizeEvalService 获取全局评估器
func GetHumanizeEvalService() *humanizesvc.HumanizeEvalService {
	return humanizeEvalService
}

func humanizeEffectiveThreshold() float64 {
	if svc := GetHumanizeEvalService(); svc != nil {
		return svc.Threshold()
	}
	// 评估器还没初始化（单测里常见）时不能退回编译期常量：
	// 那会让落库的 threshold 字段与参数中心里的值对不上。
	return humanizesvc.EffectiveThreshold()
}

type humanizeRegenerateAdapter struct {
	dispatcher *llm.Dispatcher
}

func (a *humanizeRegenerateAdapter) Regenerate(ctx context.Context, input *dto.HumanizeEvalInput, last *dto.HumanizeEvalResult) (string, error) {
	if a.dispatcher == nil {
		return "", nil
	}
	prompt := buildHumanizeRegeneratePrompt(input, last)
	result, err := a.dispatcher.Dispatch(ctx, llm.DispatchRequest{
		Scenario:  llm.ScenarioObjection,
		Prompt:    prompt,
		MaxTokens: 256,
	})
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

func buildHumanizeRegeneratePrompt(input *dto.HumanizeEvalInput, last *dto.HumanizeEvalResult) string {
	var sb strings.Builder
	sb.WriteString("请基于以下反馈重新生成回复，让它更像真人说话、更自然。\n\n")
	sb.WriteString("【客户消息】")
	sb.WriteString(input.CustomerMessage)
	sb.WriteString("\n\n")
	sb.WriteString("【上一次回复】")
	sb.WriteString(input.AIReply)
	sb.WriteString("\n\n")
	sb.WriteString("【上一次评估反馈】\n")
	if last != nil {
		sb.WriteString("- 总分：")
		sb.WriteString(strconv.FormatFloat(last.TotalScore, 'f', 2, 64))
		sb.WriteString("（阈值 ")
		sb.WriteString(strconv.FormatFloat(humanizeEffectiveThreshold(), 'f', 2, 64))
		sb.WriteString("）\n")
		for _, sc := range last.Scores {
			sb.WriteString("- ")
			sb.WriteString(string(sc.Dimension))
			sb.WriteString("：")
			sb.WriteString(strconv.FormatFloat(sc.Score, 'f', 2, 64))
			if sc.Reason != "" {
				sb.WriteString("（")
				sb.WriteString(sc.Reason)
				sb.WriteString("）")
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n【要求】\n")
	sb.WriteString("1. 严格按上述低分维度改进（自然度低 → 口语化；共情低 → 加情绪词；专业度低 → 加行业词）\n")
	sb.WriteString("2. 不超过 100 字，像微信聊天一样自然\n")
	sb.WriteString("3. 不重复上次回复的开头\n")
	return sb.String()
}

// SetHumanizeRegenerateDispatcher 注入重生成 dispatcher（让评估器能重新调用 LLM）
//
// 必须在 InitHumanizeEvalService 之后调用，否则会 panic
func SetHumanizeRegenerateDispatcher(dispatcher *llm.Dispatcher) {
	svc := GetHumanizeEvalService()
	if svc == nil {
		return
	}
	svc.WithRegenerateFn(context.Background(), func(ctx context.Context, input *dto.HumanizeEvalInput, last *dto.HumanizeEvalResult) (string, error) {
		adapter := &humanizeRegenerateAdapter{dispatcher: dispatcher}
		return adapter.Regenerate(ctx, input, last)
	})
}

func isLocalLLMBaseURL(baseURL string) bool {
	if baseURL == "" {
		return true
	}
	lower := strings.ToLower(baseURL)
	return strings.Contains(lower, "127.0.0.1") ||
		strings.Contains(lower, "localhost") ||
		strings.Contains(lower, "mtk-llm")
}
