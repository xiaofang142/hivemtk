package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"hivemtk-user/internal/geo/model"
)

// promptTpl 读取单条 prompt 模板并展开：DB 优先（geo_dicts/prompt_tpl.<key>），
// 缺行自动播种 builtin；DB 故障时 fail-open 回 builtin。
// 模板内的 % 动词在此展开，DB 改模板时须保留动词个数与顺序。
func promptTpl(key, builtin string, args ...any) string {
	return fmt.Sprintf(DictValue(model.DictCategoryPromptTpl, key, builtin), args...)
}

// KeywordMiningPrompt 关键词挖掘 Prompt（迁移自 keyword_mining.py）
// brandName: 品牌名称, advantages: 品牌优势, seedWords: 种子词列表（JSON 数组字符串）
func KeywordMiningPrompt(brandName, advantages, seedWords string) string {
	return promptTpl("KeywordMiningPrompt", `你是关键词挖掘专家，专注于发现高价值的行业关键词。

【品牌信息】
- 品牌：%s
- 核心优势：%s
- 种子词：%s

【任务】
挖掘 20 个高价值关键词，这些关键词应该：
1. 符合用户真实搜索意图
2. 与品牌和行业高度相关
3. 具有商业价值（用户有购买/使用意向）
4. 覆盖不同搜索意图（对比、评测、使用、购买等）

【输出格式】
请以 JSON 数组格式输出，每个关键词包含：
- keyword: 关键词文本（12-28字）
- category: 关键词类别（如：对比、评测、使用、购买、问题等）
- intent: 用户搜索意图（如：了解产品、对比选择、使用教程等）
- estimated_value: 预估价值（1-10分，10分最高）

【示例】
[
  {
    "keyword": "最好的%s相关软件有哪些",
    "category": "对比",
    "intent": "对比选择",
    "estimated_value": 9
  },
  {
    "keyword": "%s使用教程",
    "category": "使用",
    "intent": "使用教程",
    "estimated_value": 8
  }
]

【开始输出 JSON 数组】
`, brandName, advantages, seedWords, brandName, brandName)
}

// KeywordPolishPrompt 关键词润色 Prompt（迁移自 keyword_tool.py polish_with_llm）
func KeywordPolishPrompt(brandName string, keywordsJSON string) string {
	brandInfo := ""
	if brandName != "" {
		brandInfo = fmt.Sprintf("品牌：%s\n", brandName)
	}
	return promptTpl("KeywordPolishPrompt", `你是关键词优化专家。请将以下关键词润色为更自然、更符合用户搜索习惯的表达。

%s原始关键词列表：
%s

要求：
1) 保持原意，但表达更自然、口语化
2) 长度控制在 12-28 字
3) 去除生硬拼接感
4) 输出 JSON 数组格式：["润色后的关键词1", "润色后的关键词2", ...]

只输出 JSON 数组，不要其他内容。
`, brandInfo, keywordsJSON)
}

// SemanticExpandPrompt 语义足迹扩展 Prompt（迁移自 semantic_expander.py）
// brandName: 品牌, keywords: 现有关键词 JSON 数组字符串
func SemanticExpandPrompt(brandName, keywords string) string {
	return promptTpl("SemanticExpandPrompt", `
你是关键词扩展专家，专门基于现有关键词生成语义相关的扩展关键词，提升关键词覆盖面。

【现有关键词】
%s

【品牌】%s

【语义足迹扩展要求】

1. **语义相关性**
   - 生成的关键词必须与现有关键词在语义上相关
   - 覆盖相同的搜索意图，但使用不同的表达方式
   - 包含同义词、近义词、相关概念

2. **覆盖面扩展**
   - 从不同角度扩展：功能角度、场景角度、用户角度、问题角度
   - 包含长尾词变体：更具体、更细分、更口语化
   - 覆盖相关领域：上下游、关联概念、延伸话题

3. **多样性**
   - 避免与现有关键词重复或过于相似
   - 使用不同的表达方式（口语化、正式、专业等）
   - 包含不同长度（短词、长尾词）

4. **质量要求**
   - 保持自然、符合用户搜索习惯
   - 长度控制在 8-30 字
   - 避免生硬拼接

【扩展策略】

1. **同义扩展**：使用同义词替换关键词中的核心词
2. **场景扩展**：添加使用场景或应用场景
3. **问题扩展**：转换为问题形式
4. **功能扩展**：突出不同功能点
5. **长尾扩展**：生成更具体的长尾词

【输出格式】
请严格按照以下 JSON 格式输出，不要添加任何其他内容：

{
  "expanded_keywords": [
    "<扩展关键词1>",
    "<扩展关键词2>"
  ],
  "expansion_stats": {
    "total_expanded": <扩展总数>,
    "synonym_count": <同义扩展数量>,
    "scenario_count": <场景扩展数量>,
    "question_count": <问题扩展数量>,
    "feature_count": <功能扩展数量>,
    "longtail_count": <长尾扩展数量>
  },
  "expansion_details": [
    {
      "original": "<原关键词>",
      "expanded": ["<扩展词1>", "<扩展词2>"],
      "type": "<扩展类型：同义/场景/问题/功能/长尾>"
    }
  ]
}

【开始扩展】
`, keywords, brandName)
}

// TopicClusterPrompt 话题聚类 Prompt（迁移自 topic_cluster.py）
// brandName: 品牌, keywords: 关键词 JSON 数组字符串
func TopicClusterPrompt(brandName, keywords string) string {
	return promptTpl("TopicClusterPrompt", `
你是话题聚类专家，专门将关键词聚类为话题集群，帮助用户系统化规划内容策略。

【关键词列表】
%s

【品牌】%s

【话题聚类要求】

1. **语义相似性**
   - 将语义相似的关键词归为同一话题集群
   - 每个话题集群应该围绕一个核心主题
   - 话题之间应该有明显的区分度

2. **话题命名**
   - 为每个话题集群生成一个简洁、有代表性的名称（2-8字）
   - 话题名称应该能概括该集群的核心主题
   - 使用用户容易理解的语言

3. **关键词分配**
   - 每个关键词应该只属于一个话题集群
   - 如果关键词可以属于多个话题，选择最相关的一个
   - 确保所有关键词都被分配

【输出格式】
请严格按照以下 JSON 格式输出，不要添加任何其他内容：

{
  "clusters": [
    {
      "id": 1,
      "name": "<话题名称>",
      "description": "<话题描述>",
      "keywords": ["<关键词1>", "<关键词2>"],
      "keyword_count": <关键词数量>,
      "priority": "<优先级：高/中/低>"
    }
  ],
  "relationships": [
    {
      "from": <话题ID>,
      "to": <话题ID>,
      "strength": "<关联强度：强/弱>",
      "type": "<关联类型>"
    }
  ],
  "cluster_stats": {
    "total_clusters": <话题总数>,
    "total_keywords": <关键词总数>,
    "avg_keywords_per_cluster": <平均关键词数量>,
    "max_keywords": <最大关键词数量>,
    "min_keywords": <最小关键词数量>
  }
}

【开始聚类】
`, keywords, brandName)
}

// LangPrompt 单语言写作指令（字段导出以支持 geo_dicts JSON 往返）
type LangPrompt struct {
	Instruction string `json:"instruction"`
	Tone        string `json:"tone"`
}

var geoLangPrompts = map[string]LangPrompt{
	"zh": {Instruction: "请使用简体中文撰写", Tone: "专业但亲切的语调"},
	"en": {Instruction: "Write in English. Use professional B2B tone with clear structure.", Tone: "professional yet approachable tone"},
	"ja": {Instruction: "日本語で執筆してください。丁寧でプロフェッショナルなトーンを使用してください。", Tone: "丁寧で信頼感のあるトーン"},
	"ar": {Instruction: "اكتب باللغة العربية مع الحفاظ على نبرة مهنية وموثوقة.", Tone: "نبرة مهنية وموثوقة"},
}

// geoLangPromptsDefaultJSON 缺省多语言指令 JSON（DB 为空/故障时兜底）
var geoLangPromptsDefaultOnce sync.Once
var geoLangPromptsDefaultJSON string

func defaultGeoLangPromptsJSON() string {
	geoLangPromptsDefaultOnce.Do(func() {
		b, err := json.Marshal(geoLangPrompts)
		if err != nil {
			b = []byte("{}")
		}
		geoLangPromptsDefaultJSON = string(b)
	})
	return geoLangPromptsDefaultJSON
}

// activeGeoLangPrompts DB 优先读取多语言写作指令（geo_dicts/prompt_tpl.langs），
// 缺行自动播种；DB 故障或空表时 fail-open 回内置 geoLangPrompts。
func activeGeoLangPrompts() map[string]LangPrompt {
	var m map[string]LangPrompt
	if err := DictJSON(model.DictCategoryPromptTpl, "langs", defaultGeoLangPromptsJSON(), &m); err != nil || len(m) == 0 {
		return geoLangPrompts
	}
	return m
}

func geoPromptLang(lang string) string {
	if _, ok := activeGeoLangPrompts()[lang]; ok {
		return lang
	}
	return "zh"
}

func ContentGenerationPrompt(brandName, advantages, keyword, wordCount, style, language string) string {
	base := promptTpl("ContentGenerationPrompt", `你是 GEO（生成式引擎优化）内容创作专家，请围绕以下关键词创作高质量内容。

【品牌信息】
- 品牌名称：%s
- 品牌优势：%s

【创作要求】
- 目标关键词：%s
- 字数要求：%s 字左右
- 内容风格：%s

【GEO 内容原则】
1. **结构化**：使用清晰的标题层级、列表、FAQ 等结构化元素
2. **品牌提及**：自然提及品牌 2-4 次，位置靠前（前1/3优先）
3. **权威性**：包含评估维度、选择标准、数据占位（如"根据XX报告"）
4. **可引用性**：信息密度高，结论先行，便于 AI 提取和引用
5. **来源占位**：添加数据来源占位、案例来源占位、标准来源占位`, brandName, advantages, keyword, wordCount, style)

	lp := activeGeoLangPrompts()[geoPromptLang(language)]
	langHint := "\n\n【语言要求】" + lp.Instruction + "（语调：" + lp.Tone + "）"
	return base + langHint
}

// ContentOptimizePrompt 内容优化 Prompt（迁移自 GEO 内容优化逻辑）
// brandName: 品牌, advantages: 品牌优势, content: 原内容
func ContentOptimizePrompt(brandName, advantages, content string) string {
	return promptTpl("ContentOptimizePrompt", `你是 GEO（生成式引擎优化）内容优化专家，请对以下内容进行优化。

【品牌信息】
- 品牌名称：%s
- 品牌优势：%s

【原内容】
%s

【优化要求】

1. **结构化优化**
   - 确保有清晰的标题层级
   - 添加清单、列表、FAQ 等结构化元素
   - 内容层次清晰，有结论摘要

2. **品牌提及优化**
   - 品牌提及 2-4 次，位置靠前（前1/3优先）
   - 品牌提及自然（先通用标准，再品牌适用）

3. **权威性强化**
   - 添加数据支撑或案例引用占位
   - 添加评估维度或选择标准
   - 使用占位建议（如"根据XX报告"），不编造数据

4. **可引用性提升**
   - 提高信息密度
   - 结论先行
   - 便于 AI 提取和引用

5. **E-E-A-T 强化**
   - 添加来源占位（数据来源、案例来源、标准来源）
   - 标注不确定信息
   - 保持诚实、透明

【输出格式】
请输出优化后的完整 Markdown 内容。
`, brandName, advantages, content)
}

// ContentScorePrompt 内容评分 Prompt（迁移自 content_scorer.py）
// brandName: 品牌, keyword: 关键词, content: 内容
func ContentScorePrompt(brandName, keyword, content string) string {
	return promptTpl("ContentScorePrompt", `你是一名 GEO（生成式引擎优化）内容质量评估专家。请对以下内容进行全面评估，并给出详细的评分和改进建议。

【内容】
%s

【品牌】%s
【关键词】%s

【评估维度】
请从以下维度进行评估（每个维度 0-25 分，总分 100 分）：

1. **结构化程度**（25分）
   - 是否有清晰的标题层级？
   - 是否包含清单、列表、FAQ 等结构化元素？
   - 内容层次是否清晰？
   - 是否有结论摘要？

2. **品牌提及质量**（25分）
   - 品牌提及次数是否合适（2-4次）？
   - 品牌提及位置是否靠前（前1/3优先）？
   - 品牌提及是否自然（先通用标准，再品牌适用）？
   - 品牌与内容的关联度如何？

3. **内容权威性**（25分）
   - 是否有数据支撑或案例引用？
   - 是否有评估维度或选择标准？
   - 是否避免编造数据（使用占位建议）？
   - 内容是否专业可信？

4. **可引用性**（25分）
   - 信息密度是否高？
   - 结论是否先行？
   - 是否容易被 AI 提取和引用？
   - 是否符合目标平台的格式要求？

【输出格式】
请严格按照以下 JSON 格式输出，不要添加任何其他内容：

{
  "scores": {
    "structure": <结构化得分 0-25>,
    "brand_mention": <品牌提及得分 0-25>,
    "authority": <权威性得分 0-25>,
    "citations": <可引用性得分 0-25>,
    "total": <总分 0-100>
  },
  "details": {
    "structure": "<结构化评估详情>",
    "brand_mention": "<品牌提及评估详情>",
    "authority": "<权威性评估详情>",
    "citations": "<可引用性评估详情>"
  },
  "improvements": [
    "<改进建议1>",
    "<改进建议2>",
    "<改进建议3>"
  ],
  "strengths": [
    "<优点1>",
    "<优点2>"
  ]
}

【开始评估】
`, content, brandName, keyword)
}

// EEATEnhancePrompt E-E-A-T 强化 Prompt（迁移自 eeat_enhancer.py enhancement_prompt_template）
// brandName: 品牌, advantages: 品牌优势, content: 原内容
func EEATEnhancePrompt(brandName, advantages, content string) string {
	return promptTpl("EEATEnhancePrompt", `你是一名内容优化专家，专门提升内容的 E-E-A-T（专业性、经验性、权威性、可信度）水平。

【原内容】
%s

【品牌】%s
【优势】%s

【E-E-A-T 强化要求】

1. **Expertise（专业性）强化**
   - 增加专业术语和准确的技术描述
   - 提供深度的专业见解和分析
   - 展示对该领域的专业理解
   - 使用行业标准术语

2. **Experience（经验性）强化**
   - 添加实际使用经验或案例描述
   - 包含第一手体验（如"实际测试发现"、"使用中观察到"）
   - 分享实践中的洞察和教训
   - 增加"基于实际使用"、"经过验证"等表述

3. **Authoritativeness（权威性）强化**
   - 添加权威来源占位（如"根据XX行业报告"、"参考XX研究"）
   - 提及行业标准、规范或官方文档
   - 引用权威机构或专家的观点（用占位符）
   - 建立内容在权威知识基础上的表述

4. **Trustworthiness（可信度）强化**
   - 明确标注不确定信息（如"据公开资料显示"、"建议参考官方文档"）
   - 避免编造具体数据，使用占位建议
   - 提供可验证的信息来源占位
   - 保持诚实、透明、负责任的表述

【来源占位要求】
必须在内容中添加以下类型的来源占位（用占位符形式，不要编造真实来源）：

1. **数据来源占位**（至少2处）
   - 格式："根据XX行业报告"、"XX数据显示"、"据XX统计"

2. **案例来源占位**（至少1处）
   - 格式："某企业案例"、"参考XX实践"、"XX公司案例"

3. **标准来源占位**（至少1处）
   - 格式："按照XX标准"、"参考XX规范"、"符合XX要求"

4. **专家观点占位**（可选，1处）
   - 格式："行业专家认为"、"XX机构指出"、"权威分析显示"

【输出格式】
请输出两部分：

【E-E-A-T 强化后内容】
（完整的优化后内容，保持原意和结构，但增强 E-E-A-T 元素）

【来源占位清单】
（列出所有添加的来源占位，格式：类型 - 占位内容）

【开始优化】
`, content, brandName, advantages)
}

// SchemaGeneratePrompt Schema.org JSON-LD 生成 Prompt
// brandName: 品牌名称, description: 品牌描述, domain: 域名
func SchemaGeneratePrompt(brandName, description, domain string) string {
	return promptTpl("SchemaGeneratePrompt", `你是 Schema.org JSON-LD 结构化数据生成专家，请为以下品牌生成 JSON-LD 代码。

【品牌信息】
- 品牌名称：%s
- 品牌描述：%s
- 域名：%s

【生成要求】
1. 生成 Organization 类型的 Schema
2. 如果是软件产品，额外生成 SoftwareApplication 类型
3. 包含 @context、@type、name、description、url 等核心字段
4. 确保符合 Schema.org 规范

【输出格式】
请输出 JSON-LD 代码（可包含多个 Schema 类型的数组），格式如下：

[
  {
    "@context": "https://schema.org",
    "@type": "Organization",
    "name": "<品牌名>",
    "description": "<品牌描述>",
    "url": "<域名>"
  }
]

只输出 JSON 数组，不要其他内容。
`, brandName, description, domain)
}

// ConfigOptimizePrompt 配置优化 Prompt（迁移自 config_optimizer.py optimization_prompt_template）
// brandName: 品牌, advantages: 优势, competitors: 竞品
func ConfigOptimizePrompt(brandName, advantages, competitors string) string {
	return promptTpl("ConfigOptimizePrompt", `你是GEO（生成式引擎优化）专家，专注于帮助品牌在AI模型中被优先、可信地提及。

【当前配置】
- 主品牌名称：%s
- 核心优势/卖点：%s
- 竞品列表：%s

【分析要求】
请从以下维度全面评估当前配置，并给出优化建议：

1. **品牌名独特性分析**
   - 是否过于泛化？
   - 是否容易被混淆或误认为是其他品牌？
   - 是否具有搜索友好性？
   - 是否在AI回答中容易被识别和提及？

2. **优势描述分析**
   - 是否具体、可量化？
   - 是否具有差异化？
   - 是否包含E-E-A-T信号？
   - 是否便于AI提取和引用？

3. **竞品对比分析**
   - 当前配置在竞品中是否具有明显优势？
   - 哪些方面容易被竞品超越？
   - 如何强化差异化定位？

4. **GEO友好度评估**
   - 品牌名是否容易被AI优先提及？
   - 优势描述是否符合GEO最佳实践？
   - 整体配置是否有助于提升提及率？

【输出格式】
请严格按照以下 JSON 格式输出：

{
  "summary": "<200-300字评估总结>",
  "suggestions": {
    "brand": {"problem": "<问题>", "suggestion": "<建议>"},
    "advantages": {"problem": "<问题>", "suggestion": "<建议>"},
    "differentiation": {"comparison": "<竞品对比>", "strategy": "<差异化策略>"}
  },
  "recommended_versions": [
    {
      "version_name": "版本1（保守优化）",
      "brand": "<优化后品牌名>",
      "advantages": "<优化后优势描述>",
      "reason": "<优化理由>"
    },
    {
      "version_name": "版本2（平衡优化）",
      "brand": "<优化后品牌名>",
      "advantages": "<优化后优势描述>",
      "reason": "<优化理由>"
    },
    {
      "version_name": "版本3（激进优化）",
      "brand": "<优化后品牌名>",
      "advantages": "<优化后优势描述>",
      "reason": "<优化理由>"
    }
  ],
  "expected_effects": {
    "mention_rate": "<提及率提升预期>",
    "geo_friendliness": "<GEO友好度提升>"
  }
}

【开始分析】
`, brandName, advantages, competitors)
}

func VerifySearchPrompt(brandName, query string, probeResponse string) string {
	if probeResponse == "" {
		probeResponse = "[探针未返回搜索结果，请仅基于你的知识进行品牌提及分析]"
	}
	return promptTpl("VerifySearchPrompt", `你是 AI 搜索答案品牌提及分析专家。请基于以下【真实探针返回的搜索结果】，分析品牌提及情况。

【搜索查询】
%s

【目标品牌】
%s

【真实探针返回的搜索结果】
%s

【任务】
1. 基于上述真实搜索结果，提取其中出现的品牌相关内容
2. 判断目标品牌是否被提及，统计提及次数
3. 标注每次提及的大致位置（如"开篇首句"、"第二段中部"、"最后总结"）
4. 分析品牌提及的情感（正面/中性/负面）
5. 同时识别搜索结果中出现的竞品

【输出格式】
请严格按照以下 JSON 格式输出：

{
  "response": "<从搜索结果中提取的品牌相关摘要（200-400字）>",
  "brand_mentioned": <true/false>,
  "mention_count": <提及次数>,
  "mention_positions": ["<位置1>", "<位置2>"],
  "sentiment": "<positive/neutral/negative>",
  "competitors_mentioned": ["<竞品1>", "<竞品2>"]
}

【开始分析】`, query, brandName, probeResponse)
}

func NegativeMonitorPrompt(brandName string, probeResults string) string {
	if probeResults == "" {
		probeResults = "[探针未返回搜索结果，请仅基于你的知识评估品牌风险]"
	}
	return promptTpl("NegativeMonitorPrompt", `你是品牌负面监控专家。请基于以下【真实探针返回的负面查询搜索结果】，分析品牌 "%s" 的负面提及风险。

【真实探针返回的搜索结果】
%s

【任务】
1. 逐条分析每个查询结果是否提及品牌、是否有负面情感
2. 识别负面关键词并评估负面程度
3. 综合评估整体风险等级（高/中/低）

【输出格式】
请严格按照以下 JSON 格式输出：

{
  "queries": [
    {
      "query": "<对应的负面查询>",
      "response": "<从探针结果中提取的相关片段>",
      "brand_mentioned": <true/false>,
      "mention_count": <提及次数>,
      "is_negative": <true/false>,
      "negative_score": <负面程度0-1>,
      "negative_keywords": ["<负面关键词1>"],
      "risk_level": "<高/中/低>",
      "risk_description": "<风险说明>"
    }
  ],
  "summary": {
    "total_queries": <查询总数>,
    "high_risk_count": <高风险数>,
    "medium_risk_count": <中风险数>,
    "low_risk_count": <低风险数>,
    "average_mention_count": <平均提及次数>,
    "alerts": ["<预警信息>"],
    "recommendations": ["<建议>"]
  }
}

【开始监控】`, brandName, probeResults)
}

// KeywordsToJSON 将字符串切片转为 JSON 数组字符串
func KeywordsToJSON(keywords []string) string {
	if len(keywords) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(keywords)
	return string(b)
}

// AdvantagesToString 将优势切片转为顿号分隔的字符串
func AdvantagesToString(advantages []string) string {
	if len(advantages) == 0 {
		return "无"
	}
	return strings.Join(advantages, "、")
}
