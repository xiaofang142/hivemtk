package service

// PromptManager GEO 模块国际化 Prompt 管理器（G13.1）
//
// 按 language (zh/en/ja) 返回不同 prompt template。
// 支持的语言：
//   - zh：简体中文（主语言）
//   - en：英文
//   - ja：日文（当前走英文 fallback，预留扩展位）
//
// 设计原则：与现有 prompts.go（keyword/content/config/verify 等纯函数 Prompt）并存。
// PromptManager 只负责实体抽取、信源分类等新增能力的多语言 Prompt。
// 这样不会影响既有调用链，降低改动半径。
type PromptManager struct{}

// NewPromptManager 创建 PromptManager
func NewPromptManager() *PromptManager {
	return &PromptManager{}
}

func normalizeLanguage(lang string) string {
	switch lang {
	case "zh", "zh-CN", "zh-cn", "chinese":
		return "zh"
	case "en", "en-US", "en-us", "english":
		return "en"
	case "ja", "ja-JP", "ja-jp", "japanese":
		return "ja"
	default:
		return "zh"
	}
}

// EntityExtractPrompt 根据文档标题和正文生成实体抽取 Prompt
func (pm *PromptManager) EntityExtractPrompt(title, content string) string {
	return pm.EntityExtractPromptWithLang("zh", title, content)
}

// EntityExtractPromptWithLang 带语言参数的版本（ja 走 en fallback）
func (pm *PromptManager) EntityExtractPromptWithLang(lang, title, content string) string {
	norm := normalizeLanguage(lang)

	switch norm {
	case "en", "ja":
		return promptTpl("EntityExtractPrompt.en", `You are a named entity extraction expert. Extract structured entities and relations from the given document.

【Document Title】
%s

【Document Content】
%s

【Task】
1. Extract ALL entities of types: Product, Person, Organization, Location, Concept
2. For each entity: provide name, type, aliases (array), description (1-2 sentences), confidence (0-1)
3. Extract relations between entities: is_a, used_for, competitor_of, part_of

【Output Format】
Respond ONLY with valid JSON, no markdown fences:
{
  "entities": [
    {"name": "...", "type": "Product|Person|Organization|Location|Concept", "aliases": ["..."], "description": "...", "confidence": 0.9}
  ],
  "relations": [
    {"entity_a": "EntityNameA", "entity_b": "EntityNameB", "relation": "is_a|used_for|competitor_of|part_of"}
  ]
}

【Start】`, title, content)

	default:
		return promptTpl("EntityExtractPrompt.zh", `你是实体抽取专家。请从以下文档中抽取结构化实体和实体关系。

【文档标题】
%s

【文档内容】
%s

【任务要求】
1. 抽取所有实体，实体类型包括：Product（产品）/Person（人物）/Organization（组织）/Location（地点）/Concept（概念）
2. 每个实体需提供：name（实体名）、type（类型）、aliases（别名数组）、description（1-2 句描述）、confidence（置信度 0-1）
3. 抽取实体间关系：is_a（是一种）、used_for（用于）、competitor_of（竞品）、part_of（属于）

【输出格式】
只输出合法 JSON，不要 markdown 代码块标记：
{
  "entities": [
    {"name": "...", "type": "Product|Person|Organization|Location|Concept", "aliases": ["..."], "description": "...", "confidence": 0.9}
  ],
  "relations": [
    {"entity_a": "实体名A", "entity_b": "实体名B", "relation": "is_a|used_for|competitor_of|part_of"}
  ]
}

【开始输出】`, title, content)
	}
}

// SourceClassifyPrompt 按语言返回信源分类 Prompt（A/B/C/D + 央媒/省市/行业/无效）
func (pm *PromptManager) SourceClassifyPrompt(url, content string) string {
	return pm.SourceClassifyPromptWithLang("zh", url, content)
}

// SourceClassifyPromptWithLang 带语言参数
func (pm *PromptManager) SourceClassifyPromptWithLang(lang, url, content string) string {
	norm := normalizeLanguage(lang)

	switch norm {
	case "en", "ja":
		return promptTpl("SourceClassifyPrompt.en", `Classify the credibility and category of the following web source.

【Source URL】
%s

【Page Content (truncated)】
%s

【Labels】
Level: A (top-tier authoritative) / B (trustworthy industry) / C (generic / low-authority) / D (unreliable / no-value)
Category: 央媒 / 省市 / 行业 / 无效

【Output Format】
Respond ONLY with valid JSON:
{"level": "A|B|C|D", "category": "央媒|省市|行业|无效", "reason": "brief explanation"}

【Start】`, url, truncate(content, 2000))

	default:
		return promptTpl("SourceClassifyPrompt.zh", `请对以下信源做可信度和分类判定。

【信源 URL】
%s

【页面内容（截断）】
%s

【判定标签】
等级：A（顶级权威）/ B（可信行业）/ C（通用低权）/ D（无效）
分类：央媒 / 省市 / 行业 / 无效

【输出格式】
只输出合法 JSON：
{"level": "A|B|C|D", "category": "央媒|省市|行业|无效", "reason": "简短判定理由"}

【开始输出】`, url, truncate(content, 2000))
	}
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return s
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
