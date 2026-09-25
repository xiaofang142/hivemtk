package ragcustomerservice

import (
	"context"
	"encoding/json"
	"fmt"
	"hivemtk-user/internal/aiagent/llm"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	sysconfig "hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/i18n"
	"hivemtk-user/internal/pkg/utils/logger"
	"strings"
)

// FewShotRenderer few-shot 示例渲染接口（由 service/translation.FewShotService 实现）。
//
// 通过依赖倒置避免 aiagent 层直接依赖 service 层，由调用方在装配期注入。
// 返回的 block 直接填充 MultilingualSystemPromptTemplate 的 {{.FewShotBlock}}。
type FewShotRenderer interface {
	Render(ctx context.Context, lang string) (string, error)
}

// FallbackBridge 低资源语言降级桥接口（由 service/translation.FallbackBridge 实现）。
//
// 通过依赖倒置避免 aiagent 层直接依赖 service 层。
// Enabled() 返回 false 时主流程不调用 Generate，走标准跨语言路径。
type FallbackBridge interface {
	Enabled() bool
	IsLowResource(lang string) bool
	Generate(ctx context.Context, query string, targetLang string, docs []any) (string, error)
}

// EvalHook 评估钩子接口（由 service/translation.EvalService 实现）。
//
// 在 LLM 生成回复后异步抽样评估质量（chrF++ + LLM-as-Judge）。
// 通过依赖倒置避免 aiagent 层直接依赖 service 层（五层架构方向：
// service → aiagent，不可反向），由调用方在装配期注入。
//
// reference 为空时 EvalService 自动跳过（无参考答案无法计算 chrF），
// 因此正常生成链路（无 reference）不会触发评估；仅在评测 / 测试场景
// 下由调用方提供 reference 才会触发。
type EvalHook interface {
	MaybeEvaluate(ctx context.Context, log *model.LLMRoutingLog, query, candidate, reference string)
}

// GlossaryRenderer 术语表渲染接口（由 service/translation.GlossaryService 实现）。
//
// 通过依赖倒置避免 aiagent 层直接依赖 service 层。返回的 block 填充
// MultilingualSystemPromptTemplate 的 {{.GlossaryBlock}}，把品牌术语在目标语言下的
// 正确写法注入 system prompt，约束 LLM 输出用词。
type GlossaryRenderer interface {
	Render(ctx context.Context, lang string) string
}

// OutputCalibrator 输出后置校准接口（由 service/translation.GlossaryService 适配实现）。
//
// 在 LLM 生成文本返回前做术语校准与敏感模式保护（SKU/金额/URL/邮箱等不被误翻译）。
// 通过依赖倒置避免 aiagent 层反向依赖 service 层（service → aiagent 合法）。
type OutputCalibrator interface {
	Calibrate(ctx context.Context, text string, targetLang string) (string, error)
}

// ResponseGeneratorImpl 回复生成器实现
//
// 翻译缓存（TranslationCache）：
//   - transCache 为可选注入的翻译缓存（Redis 后端），nil 时禁用缓存
//   - 仅跨语言路径（generateCrossLingualResponse）查/写缓存
//   - 同语种路径（generateSameLangResponse）零开销，不查缓存
//
// few-shot 与降级桥：
//   - fewShot 为可选注入的 few-shot 示例渲染器，跨语言路径注入 FewShotBlock
//   - fallbackBridge 为可选注入的低资源语言降级桥，命中低资源语言时走翻译路径
type ResponseGeneratorImpl struct {
	llmService     LLMServiceInterface
	config         *ResponseGenerationConfig
	transCache     *ragretrieval.TranslationCache
	fewShot        FewShotRenderer
	fallbackBridge FallbackBridge
	evalHook       EvalHook
	glossary       GlossaryRenderer
	calibrator     OutputCalibrator
}

// WithTranslationCache 注入翻译缓存（可选）
//
// 链式调用风格：返回 receiver 便于在构造时配置。
// 传入 nil 表示禁用缓存（向后兼容，不影响主流程）。
func (g *ResponseGeneratorImpl) WithTranslationCache(cache *ragretrieval.TranslationCache) *ResponseGeneratorImpl {
	g.transCache = cache
	return g
}

// WithFewShotRenderer 注入 few-shot 示例渲染器（可选）。
//
// 仅跨语言路径（generateCrossLingualResponse）调用 Render 填充 FewShotBlock。
// 传入 nil 表示禁用 few-shot 注入（向后兼容，不影响主流程）。
func (g *ResponseGeneratorImpl) WithFewShotRenderer(r FewShotRenderer) *ResponseGeneratorImpl {
	g.fewShot = r
	return g
}

// WithFallbackBridge 注入低资源语言降级桥（可选）。
//
// 跨语言路径前会先判断是否低资源语言：命中且 bridge 启用时走翻译降级路径，
// 否则走标准跨语言 LLM 生成路径。传入 nil 表示禁用降级（向后兼容）。
func (g *ResponseGeneratorImpl) WithFallbackBridge(b FallbackBridge) *ResponseGeneratorImpl {
	g.fallbackBridge = b
	return g
}

// WithEvalHook 注入质量评估钩子（可选）。
//
// 在 LLM 生成回复成功后异步触发 chrF++ + LLM-as-Judge 评估。
// 传入 nil 表示禁用评估（向后兼容，不影响主流程）。
// 钩子内部使用 goroutine 异步执行，不阻塞 GenerateResponse 主流程。
func (g *ResponseGeneratorImpl) WithEvalHook(h EvalHook) *ResponseGeneratorImpl {
	g.evalHook = h
	return g
}

// WithGlossaryRenderer 注入术语表渲染器（可选）。
//
// 仅跨语言路径调用 Render 填充 GlossaryBlock。传入 nil 表示禁用（向后兼容）。
func (g *ResponseGeneratorImpl) WithGlossaryRenderer(r GlossaryRenderer) *ResponseGeneratorImpl {
	g.glossary = r
	return g
}

// WithOutputCalibrator 注入输出后置校准器（可选）。
//
// 在 LLM 生成回复后做术语校准与敏感模式保护。传入 nil 表示禁用（向后兼容）。
func (g *ResponseGeneratorImpl) WithOutputCalibrator(c OutputCalibrator) *ResponseGeneratorImpl {
	g.calibrator = c
	return g
}

// LLMServiceInterface LLM服务接口
type LLMServiceInterface interface {
	Generate(ctx context.Context, config any, prompt string) (string, error)
	GenerateStructured(ctx context.Context, config any, prompt string, schema any) (any, error)
	ValidateConfig(config any) error
	GetDefaultConfig() any
}

// ResponseGenerationConfig 回复生成配置
type ResponseGenerationConfig struct {
	LLMModel           string  `json:"llm_model"`
	DefaultTemperature float64 `json:"default_temperature"`
	DefaultMaxTokens   int     `json:"default_max_tokens"`
	TopP               float64 `json:"top_p"`
	FrequencyPenalty   float64 `json:"frequency_penalty"`
	PresencePenalty    float64 `json:"presence_penalty"`
	SystemPrompt       string  `json:"system_prompt"`
}

// NewResponseGeneratorImpl 创建新的回复生成器
func NewResponseGeneratorImpl(llmService LLMServiceInterface, config *ResponseGenerationConfig) *ResponseGeneratorImpl {
	if config == nil {
		config = &ResponseGenerationConfig{
			LLMModel:           sysconfig.DefaultLLMModel(),
			DefaultTemperature: 0.7,
			DefaultMaxTokens:   1000,
			TopP:               0.9,
			FrequencyPenalty:   0.5,
			PresencePenalty:    0.5,
			SystemPrompt:       defaultSystemPrompt,
		}
	}

	return &ResponseGeneratorImpl{
		llmService: llmService,
		config:     config,
	}
}

// GenerateResponse 生成回复
//
// 多语言方案：
//   - CrossLingual=false（同语种）：走原逻辑，零开销，保持向后兼容
//   - CrossLingual=true（跨语言）：
//   - 若 fallbackBridge 启用且 targetLang 为低资源语言 → 走翻译降级路径
//     （中文生成 + DeepL 翻译 + Glossary 后处理）
//   - 否则 → 走标准跨语言 LLM 生成路径（多语言 system prompt + few-shot）
//
// 生成成功后触发 EvalHook 异步抽样评估（reference 为空时
// EvalService 自动跳过，因此正常生成链路无额外开销）。
func (g *ResponseGeneratorImpl) GenerateResponse(ctx context.Context, request ResponseGenerationRequest) (string, error) {
	internalLang := i18n.GetInternalLang(ctx)
	configuredTarget := i18n.GetTargetLang(ctx)

	targetLang := g.resolveTargetLang(ctx, internalLang, configuredTarget, request.Query)
	crossLingual := internalLang != targetLang

	var reply string
	var err error

	if !crossLingual {
		reply, err = g.generateSameLangResponse(ctx, request, internalLang)
	} else {
		if g.fallbackBridge != nil && g.fallbackBridge.Enabled() && g.fallbackBridge.IsLowResource(targetLang) {
			r, e := g.fallbackBridge.Generate(ctx, request.Query, targetLang, request.SearchResults)
			if e == nil {
				reply = r
			} else {
				logger.Warnf("[ResponseGenerator] fallback bridge failed, fallthrough to cross-lingual: %v", e)
				reply, err = g.generateCrossLingualResponse(ctx, request, internalLang, targetLang)
			}
		} else {
			reply, err = g.generateCrossLingualResponse(ctx, request, internalLang, targetLang)
		}
	}

	if err != nil {
		return "", err
	}

	if g.calibrator != nil {
		if calibrated, cErr := g.calibrator.Calibrate(ctx, reply, targetLang); cErr == nil {
			reply = calibrated
		} else {
			logger.Warnf("[ResponseGenerator] output calibration failed, keep original: %v", cErr)
		}
	}

	if g.evalHook != nil {
		g.evalHook.MaybeEvaluate(ctx, &model.LLMRoutingLog{
			InternalLang: internalLang,
			TargetLang:   targetLang,
			CrossLingual: crossLingual,
		}, request.Query, reply, "")
	}

	return reply, nil
}

func (g *ResponseGeneratorImpl) resolveTargetLang(ctx context.Context, internalLang, configuredTarget, query string) string {
	if configuredTarget != "" && configuredTarget != internalLang {
		return configuredTarget
	}
	if detected := i18n.DetectLangCode(query); detected != "" && detected != internalLang {
		return detected
	}
	return internalLang
}

func (g *ResponseGeneratorImpl) generateSameLangResponse(ctx context.Context, request ResponseGenerationRequest, lang string) (string, error) {
	contextStr := g.buildContextString(request.SearchResults, request.Context)

	prompt := g.buildResponsePrompt(request.Query, contextStr, request.Session, request.Context)

	llmConfig := prepareLLMConfig(g.config, request.Config)

	err := g.llmService.ValidateConfig(llmConfig)
	if err != nil {
		return "", fmt.Errorf("invalid LLM config: %w", err)
	}

	response, err := g.llmService.Generate(ctx, llmConfig, prompt)
	if err != nil {
		return "", fmt.Errorf("failed to generate response: %w", err)
	}

	return response, nil
}

func (g *ResponseGeneratorImpl) generateCrossLingualResponse(ctx context.Context, request ResponseGenerationRequest, internalLang, targetLang string) (string, error) {
	kbVersion := request.Session.KBID
	if g.transCache != nil {
		if cached, _ := g.transCache.Get(ctx, internalLang, targetLang, request.Query, kbVersion); cached != "" {
			logger.Infof("[ResponseGenerator] TranslationCache hit: internal=%s target=%s", internalLang, targetLang)
			return cached, nil
		}
	}

	reply, err := g.doCrossLingualLLM(ctx, request, internalLang, targetLang)
	if err != nil {
		return "", err
	}

	if g.transCache != nil {
		if err := g.transCache.Set(ctx, internalLang, targetLang, request.Query, kbVersion, reply); err != nil {
			logger.Warnf("[ResponseGenerator] TranslationCache Set failed: %v", err)
		}
	}
	return reply, nil
}

func (g *ResponseGeneratorImpl) doCrossLingualLLM(ctx context.Context, request ResponseGenerationRequest, internalLang, targetLang string) (string, error) {
	contextStr := g.buildContextString(request.SearchResults, request.Context)

	fewShotBlock := ""
	if g.fewShot != nil {
		if block, err := g.fewShot.Render(ctx, targetLang); err == nil {
			fewShotBlock = block
		} else {
			logger.Warnf("[ResponseGenerator] few-shot render failed, skip: %v", err)
		}
	}

	glossaryBlock := ""
	if g.glossary != nil {
		glossaryBlock = g.glossary.Render(ctx, targetLang)
	}
	systemPrompt := renderMultilingualSystemPrompt(internalLang, targetLang, glossaryBlock, fewShotBlock)

	prompt := fmt.Sprintf(`%s

Knowledge base context:
%s

Customer question: %s

Customer service reply:`, systemPrompt, contextStr, request.Query)

	llmConfig := prepareLLMConfig(g.config, request.Config)

	if err := g.llmService.ValidateConfig(llmConfig); err != nil {
		return "", fmt.Errorf("invalid LLM config: %w", err)
	}

	return g.llmService.Generate(ctx, llmConfig, prompt)
}

// GenerateStructuredResponse 生成结构化回复
func (g *ResponseGeneratorImpl) GenerateStructuredResponse(ctx context.Context, request ResponseGenerationRequest, schema any) (any, error) {
	contextStr := g.buildContextString(request.SearchResults, request.Context)

	prompt := g.buildStructuredResponsePrompt(request.Query, contextStr, request.Session, request.Context, schema)

	llmConfig := prepareLLMConfig(g.config, request.Config)

	if llmConfigMap, ok := llmConfig.(map[string]any); ok {
		llmConfigMap["response_format"] = "json_object"
	}

	err := g.llmService.ValidateConfig(llmConfig)
	if err != nil {
		return nil, fmt.Errorf("invalid LLM config: %w", err)
	}

	response, err := g.llmService.GenerateStructured(ctx, llmConfig, prompt, schema)
	if err != nil {
		return nil, fmt.Errorf("failed to generate structured response: %w", err)
	}

	return response, nil
}

// BuildContextString 构建上下文字符串
func (g *ResponseGeneratorImpl) BuildContextString(results []any, context Context) string {
	return g.buildContextString(results, context)
}

// BuildResponsePrompt 构建回复提示词
func (g *ResponseGeneratorImpl) BuildResponsePrompt(query, contextStr string, session Session, context Context) string {
	return g.buildResponsePrompt(query, contextStr, session, context)
}

func (g *ResponseGeneratorImpl) buildContextString(results []any, context Context) string {
	var contextStr string

	if len(results) > 0 {
		contextStr += "参考知识库信息:\n"
		for i, result := range results {
			if resultStr, ok := result.(string); ok {
				contextStr += fmt.Sprintf("[%d] 参考信息: %s\n\n", i+1, resultStr)
			} else if resultMap, ok := result.(map[string]any); ok {
				content, _ := resultMap["content"].(string)
				docID, _ := resultMap["document_id"].(string)
				score, _ := resultMap["score"].(float64)
				contextStr += fmt.Sprintf("[%d] 来源: %s (相关性: %.2f)\n%s\n\n",
					i+1, docID, score, content)
			}
		}
	} else {
		contextStr += "未找到相关知识库信息。\n"
	}

	if context.Topic != "" {
		contextStr += fmt.Sprintf("当前话题: %s\n", context.Topic)
	}

	if context.Intent != "" {
		contextStr += fmt.Sprintf("用户意图: %s\n", context.Intent)
	}

	if len(context.Entities) > 0 {
		contextStr += "识别的实体:\n"
		for entity, values := range context.Entities {
			contextStr += fmt.Sprintf("- %s: %s\n", entity, strings.Join(values, ", "))
		}
	}

	if context.Sentiment.Score != 0 {
		contextStr += fmt.Sprintf("用户情感: %s (%.2f)\n", context.Sentiment.Label, context.Sentiment.Score)
	}

	return contextStr
}

func (g *ResponseGeneratorImpl) buildResponsePrompt(query, contextStr string, session Session, context Context) string {
	systemPrompt := g.config.SystemPrompt
	if session.Config.SystemPrompt != "" {
		systemPrompt = session.Config.SystemPrompt
	}

	prompt := fmt.Sprintf(`%s

当前对话上下文:
%s

用户问题: %s

注意事项:
1. 回复要专业、友好、简洁
2. 如果知识库信息不足以回答，请诚实告知用户
3. 保持与当前话题的相关性
4. 根据用户情感调整回复语气
5. 如有具体商品或服务询问，基于知识库信息给出详细回复
6. 如遇售后或投诉类问题，表达积极解决问题的态度

客服回复:`, systemPrompt, contextStr, query)

	return prompt
}

func (g *ResponseGeneratorImpl) buildStructuredResponsePrompt(query, contextStr string, session Session, context Context, schema any) string {
	schemaJSON, _ := json.Marshal(schema)

	systemPrompt := g.config.SystemPrompt
	if session.Config.SystemPrompt != "" {
		systemPrompt = session.Config.SystemPrompt
	}

	prompt := fmt.Sprintf(`%s

当前对话上下文:
%s

用户问题: %s

请严格按照以下JSON Schema返回结果:
%s

注意事项:
1. 回复要专业、友好、简洁
2. 如果知识库信息不足以回答，请在回复中说明
3. 保持与当前话题的相关性
4. 根据用户情感调整回复语气
5. 遵循JSON Schema格式要求

回复:`, systemPrompt, contextStr, query, string(schemaJSON))

	return prompt
}

func prepareLLMConfig(config *ResponseGenerationConfig, sessionConfig SessionConfig) any {
	llmConfig := make(map[string]any)

	if sessionConfig.Temperature != 0 {
		llmConfig["temperature"] = sessionConfig.Temperature
	} else {
		llmConfig["temperature"] = config.DefaultTemperature
	}

	if sessionConfig.MaxTokens != 0 {
		llmConfig["max_tokens"] = sessionConfig.MaxTokens
	} else {
		llmConfig["max_tokens"] = config.DefaultMaxTokens
	}

	llmConfig["model"] = config.LLMModel
	llmConfig["top_p"] = config.TopP
	llmConfig["frequency_penalty"] = config.FrequencyPenalty
	llmConfig["presence_penalty"] = config.PresencePenalty

	return llmConfig
}

const defaultSystemPrompt = `你是专业的电商客服助手，正在为客户提供咨询服务。请基于提供的信息回答用户问题，遵循以下原则：
1. 专业礼貌：使用专业、友好的语气
2. 准确性：基于提供的信息进行回复
3. 清晰简洁：提供清晰、简洁的答案
4. 积极态度：展现积极解决问题的态度
5. 个性化：根据用户的具体情况进行个性化回复`

// RemoteLLMService 真实 LLM 服务适配器(对接 OpenAI 兼容 /v1/chat/completions)
// Session 级别配置会覆盖全局默认。
type RemoteLLMService struct {
	svc *llm.LLMService
}

// NewRemoteLLMService 创建真实 LLM 服务
func NewRemoteLLMService() *RemoteLLMService {
	return &RemoteLLMService{svc: llm.NewLLMService()}
}

// Generate 真实调用 LLM
func (r *RemoteLLMService) Generate(ctx context.Context, config any, prompt string) (string, error) {
	llmCfg, err := toLLMConfig(r.svc, config)
	if err != nil {
		return "", err
	}
	return r.svc.Generate(ctx, llmCfg, prompt)
}

// GenerateStructured 真实结构化调用
func (r *RemoteLLMService) GenerateStructured(ctx context.Context, config any, prompt string, schema any) (any, error) {
	llmCfg, err := toLLMConfig(r.svc, config)
	if err != nil {
		return nil, err
	}
	llmCfg.ResponseFormat = "json_object"
	return r.svc.GenerateStructured(ctx, llmCfg, prompt, schema)
}

// ValidateConfig 校验配置
func (r *RemoteLLMService) ValidateConfig(config any) error {
	llmCfg, err := toLLMConfig(r.svc, config)
	if err != nil {
		return err
	}
	return r.svc.ValidateConfig(llmCfg)
}

// GetDefaultConfig 返回默认 LLMConfig
func (r *RemoteLLMService) GetDefaultConfig() any {
	return r.svc.GetDefaultConfig()
}

func toLLMConfig(svc *llm.LLMService, raw any) (*llm.LLMConfig, error) {
	if raw == nil {
		return svc.GetDefaultConfig(), nil
	}
	switch v := raw.(type) {
	case *llm.LLMConfig:
		if v.APIKey == "" {
			v.APIKey = svc.GetDefaultConfig().APIKey
		}
		if v.BaseURL == "" {
			v.BaseURL = svc.GetDefaultConfig().BaseURL
		}
		return v, nil
	case llm.LLMConfig:
		return toLLMConfig(svc, &v)
	case map[string]any:
		cfg := svc.GetDefaultConfig()
		if s, ok := v["model"].(string); ok && s != "" {
			cfg.Model = s
		}
		if s, ok := v["base_url"].(string); ok && s != "" {
			cfg.BaseURL = s
		}
		if s, ok := v["api_key"].(string); ok && s != "" {
			cfg.APIKey = s
		}
		if s, ok := v["api_type"].(string); ok && s != "" {
			cfg.APIType = s
		}
		if f, ok := v["temperature"].(float64); ok {
			cfg.Temperature = f
		}
		if i, ok := v["max_tokens"].(int); ok && i > 0 {
			cfg.MaxTokens = i
		} else if f, ok := v["max_tokens"].(float64); ok && f > 0 {
			cfg.MaxTokens = int(f)
		}
		if s, ok := v["response_format"].(string); ok && s != "" {
			cfg.ResponseFormat = s
		}
		return cfg, nil
	case SessionConfig:
		return toLLMConfig(svc, map[string]any{
			"temperature": v.Temperature,
			"max_tokens":  v.MaxTokens,
		})
	default:
		return svc.GetDefaultConfig(), nil
	}
}
