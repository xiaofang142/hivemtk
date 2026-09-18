package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	i18npkg "hivemtk-user/internal/pkg/i18n"
	"hivemtk-user/internal/pkg/utils/logger"
)

// LowResourceLangs 低资源语言列表（LLM 覆盖较弱，需翻译降级）。
//
// 该列表基于 v1.2 出海方案的默认配置，可通过 config.yaml 的
// i18n.fallback.low_resource_langs 覆盖。
var LowResourceLangs = map[string]bool{
	"ar": true,
	"th": true,
	"vi": true,
	"hi": true,
	"tr": true,
}

// IsLowResource 判断是否低资源语言。
func IsLowResource(lang string) bool {
	return LowResourceLangs[lang]
}

// SetLowResourceLangs 覆盖低资源语言列表（供 config 加载时调用）。
//
// 传入 nil 或空切片时恢复默认列表。该函数非并发安全，应在启动期
// 配置加载阶段调用，运行时不要修改。
func SetLowResourceLangs(langs []string) {
	LowResourceLangs = map[string]bool{}
	if len(langs) == 0 {
		LowResourceLangs["ar"] = true
		LowResourceLangs["th"] = true
		LowResourceLangs["vi"] = true
		LowResourceLangs["hi"] = true
		LowResourceLangs["tr"] = true
		return
	}
	for _, l := range langs {
		if l != "" {
			LowResourceLangs[l] = true
		}
	}
}

// Translator 翻译器接口（支持多种翻译引擎）。
//
// 实现方：
//   - DeepLTranslator（本文件）
//   - 未来：GoogleTranslator / NLLBTranslator
type Translator interface {
	Translate(ctx context.Context, text, fromLang, toLang string, opts TranslateOptions) (string, error)
	Name() string
}

// TranslateOptions 翻译选项。
type TranslateOptions struct {
	GlossaryID    string
	PreserveTerms map[string]string
}

const deeplDefaultBaseURL = "https://api.deepl.com/v2"

const deeplTimeout = 30 * time.Second

var deeplTimeoutProvider = func() time.Duration { return deeplTimeout }

// SetDeeplTimeoutProvider 上层注入函数（ConfigParam 初始化后调用）
func SetDeeplTimeoutProvider(fn func() time.Duration) {
	if fn != nil {
		deeplTimeoutProvider = fn
	}
}

// DeepLTranslator DeepL 翻译实现。
//
// API 文档：https://developers.deepl.com/docs/api-reference/translate/openapi-spec-for-translate
// 鉴权：Authorization: DeepL-Auth-Key {api_key}
type DeepLTranslator struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewDeepLTranslator 构造 DeepL 翻译器。
//
// apiKey 为空时构造仍成功，但 Translate 会返回错误（供 FallbackBridge
// 在启动期判断是否启用）。baseURL 为空时使用默认 Pro 版本地址。
func NewDeepLTranslator(apiKey, baseURL string) *DeepLTranslator {
	if baseURL == "" {
		baseURL = deeplDefaultBaseURL
	}
	return &DeepLTranslator{
		apiKey:     apiKey,
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: deeplTimeoutProvider()},
	}
}

// Name 返回翻译器名称。
func (t *DeepLTranslator) Name() string { return "deepl" }

// Available 检查翻译器是否可用（api key 已配置）。
func (t *DeepLTranslator) Available() bool {
	return t.apiKey != ""
}

// Translate 调用 DeepL API 翻译文本。
//
// 参数：
//   - text     ：待翻译文本
//   - fromLang ：源语言短码（如 "zh"），DeepL 要求大写（ZH）
//   - toLang   ：目标语言短码（如 "en"），DeepL 要求大写（EN）
//   - opts     ：翻译选项（glossary_id / preserve_terms）
//
// 错误处理：
//   - api key 未配置：返回 ErrTranslatorUnavailable
//   - HTTP / API 错误：返回带状态码的 error
//   - DeepL 不支持的目标语言：返回 API 错误（调用方兜底返回中文）
func (t *DeepLTranslator) Translate(ctx context.Context, text, fromLang, toLang string, opts TranslateOptions) (string, error) {
	if !t.Available() {
		return "", ErrTranslatorUnavailable
	}
	if text == "" {
		return "", nil
	}

	src := strings.ToUpper(i18npkg.NormalizeLang(fromLang))
	dst := strings.ToUpper(i18npkg.NormalizeLang(toLang))

	body := deeplTranslateRequest{
		Text:       []string{text},
		SourceLang: src,
		TargetLang: dst,
	}
	if opts.GlossaryID != "" {
		body.GlossaryID = opts.GlossaryID
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("deepl: marshal request failed: %w", err)
	}

	url := t.baseURL + "/translate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("deepl: new request failed: %w", err)
	}
	req.Header.Set("Authorization", "DeepL-Auth-Key "+t.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("deepl: http request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("deepl: read response failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("deepl: api error status=%d body=%s", resp.StatusCode, truncateForLog(string(raw), 256))
	}

	var result deeplTranslateResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("deepl: unmarshal response failed: %w", err)
	}
	if len(result.Translations) == 0 {
		return "", errors.New("deepl: empty translations in response")
	}
	translated := result.Translations[0].Text

	if len(opts.PreserveTerms) > 0 {
		translated = applyPreserveTerms(translated, opts.PreserveTerms)
	}
	return translated, nil
}

// ErrTranslatorUnavailable 翻译器不可用（如 api key 未配置）。
var ErrTranslatorUnavailable = errors.New("translator: unavailable (api key not configured)")

type deeplTranslateRequest struct {
	Text       []string `json:"text"`
	SourceLang string   `json:"source_lang,omitempty"`
	TargetLang string   `json:"target_lang"`
	GlossaryID string   `json:"glossary_id,omitempty"`
}

type deeplTranslateResponse struct {
	Translations []struct {
		DetectedSourceLanguage string `json:"detected_source_language"`
		Text                   string `json:"text"`
	} `json:"translations"`
}

func applyPreserveTerms(text string, terms map[string]string) string {
	for src, dst := range terms {
		if src == "" || dst == "" || src == dst {
			continue
		}
		text = strings.ReplaceAll(text, src, dst)
	}
	return text
}

func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// RAGGenerator 中文 RAG 生成器接口。
//
// 由 ragcustomerservice.ResponseGeneratorImpl 适配实现：调用方传入一个
// 适配器，将 Generate(ctx, query, docs) 转为内部中文生成路径
// （generateSameLangResponse）。
type RAGGenerator interface {
	Generate(ctx context.Context, query string, docs []any) (string, error)
}

// FallbackBridge 低资源语言降级桥。
//
// 流程：中文生成（RAG）→ DeepL 翻译 → Glossary 后处理校准。
//
// 默认 enabled=false（构造时 translator 为 nil 或 RAGGenerator 为 nil
// 时自动禁用），不影响主流程。
type FallbackBridge struct {
	translator      Translator
	ragGenerator    RAGGenerator
	glossaryService *GlossaryService
	postValidator   *PostValidator
	enabled         bool
}

// NewFallbackBridge 构造 FallbackBridge。
//
// 启用条件（全部满足才 enabled=true）：
//   - translator 非 nil 且可用（如 DeepL API key 已配置）
//   - ragGenerator 非 nil
//   - glossaryService 可为 nil（无术语后处理）
//
// 调用方通过 enabled 参数显式控制开关（对应 config i18n.fallback.enabled）。
func NewFallbackBridge(translator Translator, rag RAGGenerator, glossary *GlossaryService, enabled bool) *FallbackBridge {
	b := &FallbackBridge{
		translator:      translator,
		ragGenerator:    rag,
		glossaryService: glossary,
		postValidator:   NewPostValidator(),
		enabled:         enabled,
	}
	if enabled {
		if translator == nil {
			b.enabled = false
			logger.Warnf("fallback bridge: disabled because translator is nil")
		} else if dt, ok := translator.(*DeepLTranslator); ok && !dt.Available() {
			b.enabled = false
			logger.Warnf("fallback bridge: disabled because deepl api key is empty")
		}
		if rag == nil {
			b.enabled = false
			logger.Warnf("fallback bridge: disabled because rag generator is nil")
		}
	}
	return b
}

// Enabled 返回 FallbackBridge 是否启用。
func (b *FallbackBridge) Enabled() bool {
	return b != nil && b.enabled
}

// IsLowResource 判断是否需要降级（包方法，便于外部判断）。
func (b *FallbackBridge) IsLowResource(lang string) bool {
	return IsLowResource(lang)
}

// Generate 低资源语言降级生成。
//
// 流程：
//  1. 检查启用状态：未启用返回 ErrFallbackDisabled
//  2. 中文生成（复用现有 RAG 链路）
//  3. DeepL 翻译为目标语言
//  4. Glossary + PostValidator 后处理校准
//  5. 翻译失败兜底返回中文（保证用户能收到回复）
func (b *FallbackBridge) Generate(ctx context.Context, query string, targetLang string, docs []any) (string, error) {
	if !b.enabled || b.translator == nil {
		return "", ErrFallbackDisabled
	}

	targetLang = i18npkg.NormalizeLang(targetLang)

	zhResp, err := b.ragGenerator.Generate(ctx, query, docs)
	if err != nil {
		return "", fmt.Errorf("fallback: zh rag generate failed: %w", err)
	}
	if zhResp == "" {
		return "", errors.New("fallback: zh rag generate empty response")
	}

	translated, err := b.translator.Translate(ctx, zhResp, "zh", targetLang, TranslateOptions{})
	if err != nil {
		logger.Warnf("fallback: translate to %s failed, fallback to zh: %v", targetLang, err)
		return zhResp, nil
	}

	if b.glossaryService != nil {
		if view, err := b.glossaryService.LoadByLang(ctx, targetLang); err == nil && view != nil {
			calibrated, _ := b.postValidator.Validate(translated, targetLang, view)
			return calibrated, nil
		}
	}
	return translated, nil
}

// ErrFallbackDisabled FallbackBridge 未启用。
var ErrFallbackDisabled = errors.New("fallback bridge: disabled")
