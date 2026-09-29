package service

import "strings"

// BusinessLine GEO 关键词一级类目：hivemtk 4 大核心业务线
//
// 所有批量挖掘的种子词都归属其中一条业务线（写入 GeoKeyword.Cluster），
// 技术栈只做属性维度（与业务线正交组合），保证关键词体系先按业务分桶。
type BusinessLine struct {
	// Name 一级类目名，同时用作 GeoKeyword.Cluster 值
	Name string
	// ShortName 与技术栈组合时的简称（如 "GEO"）
	ShortName string
	// CoreTerms 业务核心词（直接作为 Layer='seed' 种子）
	CoreTerms []string
}

// TechStackAttrs 技术栈/概念属性维度：只收录本项目真实的功能性、技术性名词。
//
// 来源（均可在本仓库验证）：
//   - GEO 功能：internal/geo/service/*.go 文件名即功能（keyword/content/verification/
//     visibility/probe/push/workflow/kb/entity/decision/index_tracker/scheduler…）
//   - 前端模块：user-web/src/views/geo/*.vue（25 个功能页）
//   - 后端依赖：go.mod（Gin/GORM/PostgreSQL/Redis/JWT/cron/WebSocket/OpenTelemetry…）
//   - AI 网关：internal/aiagent/llm（DeepSeek/通义/豆包/文心/Kimi/GLM/投票/熔断…）
//   - 渠道集成：WhatsApp、Telegram Bot、SMTP 邮件、二维码、PDF 解析
//
// 与业务线正交组合，生成"技术×业务"种子（如 "RAG智能客服"）。
// 分组：AI大模型与网关 / GEO与AI搜索功能 / 客服营销销售功能 / 平台工程技术。
var TechStackAttrs = []string{
	// AI大模型与网关（项目真实接入的模型与网关能力）
	"人工智能", "大模型", "LLM", "NLP", "RAG", "RAG问答",
	"知识图谱", "向量数据库", "Embedding模型", "重排序", "混合检索",
	"向量检索", "语义搜索", "知识库", "提示词工程", "Function Calling",
	"MCP", "Agent", "智能体", "多智能体", "Agent工作流", "ReAct",
	"思维链", "记忆机制", "多模态", "多模型投票", "自一致性",
	"熔断降级", "金丝雀发布", "Token计费", "上下文窗口", "JSON模式",
	"流式输出", "内容审核", "幻觉检测", "引用溯源", "私域部署",
	"本地部署", "DeepSeek", "通义千问", "豆包", "文心一言",
	"月之暗面", "Kimi", "智谱GLM", "GPT", "Claude", "Gemini",
	// GEO与AI搜索功能（geo 模块真实功能，见 internal/geo/service）
	"GEO优化", "生成式引擎优化", "AI搜索优化", "品牌可见性监测",
	"SOV声量份额", "AI引用监测", "AI搜索排名", "E-E-A-T", "EEAT增强",
	"llms.txt", "robots.txt", "Schema标记", "JSON-LD", "知识面板",
	"AI Overviews", "引用率", "品牌提及", "零点击搜索", "答案引擎",
	"实体识别", "实体图谱", "GEO审计", "GEO评分", "内容评分",
	"内容生成", "内容优化", "原创度检测", "事实密度增强", "关键词挖掘",
	"长尾词", "下拉词", "语义扩展", "话题聚类", "意图识别",
	"Prompt扇出", "负面监控", "收录追踪", "来源归因", "信源目录",
	"爬虫监控", "决策报告", "缺口补位", "工作流引擎", "定时任务",
	"推送配额", "站点部署", "健康检查",
	// 客服营销销售功能（4 大业务线真实功能概念）
	"智能客服", "客服机器人", "工单系统", "知识库问答", "FAQ",
	"多轮对话", "人工转接", "在线客服", "呼叫中心", "云客服",
	"全渠道", "客服质检", "满意度", "SLA", "首响时间", "解决率",
	"转人工率", "自助服务", "营销自动化", "内容营销", "邮件营销",
	"社交媒体营销", "短视频营销", "直播营销", "私域流量", "KOC",
	"SEO", "SEM", "UTM", "营销漏斗", "客户旅程", "营销归因",
	"AARRR", "ABM", "客户成功", "WhatsApp营销", "Telegram机器人",
	"二维码营销", "销售智能体", "AI销售", "线索挖掘", "商机识别",
	"商机管理", "线索评分", "客户画像", "客户跟进", "销售漏斗",
	"销售预测", "销售话术", "智能报价", "外呼机器人", "SDR",
	"BDR", "CRM", "SCRM", "MEDDIC", "成单率", "客单价",
	"续费率", "交叉销售", "电销",
	// 平台工程技术（go.mod / user-web 真实依赖与部署链）
	"Golang", "Gin", "GORM", "PostgreSQL", "Redis",
	"Vue", "Element Plus", "ECharts", "Pinia", "TypeScript",
	"JWT", "SSO单点登录", "WebSocket", "链路追踪", "分布式锁",
	"缓存", "Docker", "Docker Compose", "GitHub Actions", "Hugo",
	"Cloudflare", "二维码", "PDF解析", "邮件服务", "cron",
}

// DefaultBrandName 推广品牌（本项目 hivemtk 的对外品牌名，需与 GeoConfig.BrandName 一致）
const DefaultBrandName = "HiveMTK"

// BusinessLines hivemtk 4 大核心业务线
var BusinessLines = []BusinessLine{
	{
		Name:      "营销工具",
		ShortName: "营销工具",
		CoreTerms: []string{
			"营销自动化", "内容营销", "邮件营销", "社交媒体营销",
			"SEO优化工具", "广告投放", "客户旅程", "营销归因",
			"营销漏斗", "私域营销", "短视频营销", "直播营销",
		},
	},
	{
		Name:      "GEO工具",
		ShortName: "GEO",
		CoreTerms: []string{
			"GEO优化", "生成式引擎优化", "AI搜索优化", "品牌可见性监测",
			"SOV监测", "llms.txt", "AI引用监测", "知识库问答优化",
			"大模型品牌提及", "GEO审计", "GEO评分", "AI搜索排名",
		},
	},
	{
		Name:      "智能客服",
		ShortName: "智能客服",
		CoreTerms: []string{
			"智能客服", "客服机器人", "工单系统", "知识库问答",
			"多轮对话", "意图识别", "人工转接", "语音客服",
			"在线客服", "客服质检", "常见问题库", "客服自动化",
		},
	},
	{
		Name:      "销售智能体",
		ShortName: "销售智能体",
		CoreTerms: []string{
			"销售智能体", "AI销售", "线索挖掘", "商机识别",
			"客户跟进", "销售话术", "智能报价", "销售预测",
			"客户画像", "外呼机器人", "销售漏斗", "成单助手",
		},
	},
}

// BuildBusinessSeedMatrix 构造"业务×技术"种子矩阵（离线、确定性、零 LLM 成本）
//
// 返回：
//   - seeds：去重保序的种子词列表（业务核心词 + 技术×业务组合词）
//   - seedLines：种子词 → 一级类目（业务线名）映射，用于回填 GeoKeyword.Cluster
//
// 规模口径：48 核心词 + 约 4×181 技术/概念组合词（含已含简称词的跳过）+ 16 品牌推广词
// （品牌×4业务线 + 品牌×12 GEO核心词）≈ 700+ 种子；
// 经 DefaultLongtailTemplates（23 模板）离线展开约 16,000+ 长尾词；
// 再经 CrawlSuggest（5 引擎下拉）补充数千下拉词，全量经 SaveMiningResults 落库。
func BuildBusinessSeedMatrix() ([]string, map[string]string) {
	return buildSeedMatrix(TechStackAttrs, BusinessLines, nil)
}

// buildSeedMatrix 种子矩阵通用构建（供 DB 驱动的 SeedTermService 复用）。
// brandCombos 非 nil 时直接采用 DB 中的品牌组合词（term→业务线），
// 为 nil 时按业务线派生（品牌×业务线 + 品牌×GEO核心词）。
func buildSeedMatrix(techAttrs []string, lines []BusinessLine, brandCombos map[string]string) ([]string, map[string]string) {
	seeds := make([]string, 0, 256)
	seedLines := make(map[string]string, 256)
	seen := make(map[string]bool, 256)
	add := func(term, line string) {
		if term == "" || seen[term] {
			return
		}
		seen[term] = true
		seeds = append(seeds, term)
		seedLines[term] = line
	}

	for _, bl := range lines {
		for _, core := range bl.CoreTerms {
			add(core, bl.Name)
		}
		for _, tech := range techAttrs {
			// 技术词已含业务线简称时跳过组合，避免 "GEO审计GEO" 类非真实搜索词
			if strings.Contains(tech, bl.ShortName) {
				continue
			}
			add(tech+bl.ShortName, bl.Name)
		}
	}

	if brandCombos != nil {
		// DB 驱动：品牌组合词以表为准
		for term, line := range brandCombos {
			add(term, line)
		}
		return seeds, seedLines
	}

	// 品牌推广词：推广本项目 hivemtk，所有品牌组合词必须真实有效（品牌+业务线名/GEO核心词均为真实搜索模式）
	for _, bl := range lines {
		brandTerm := DefaultBrandName + bl.ShortName
		if bl.ShortName == "GEO" {
			brandTerm = DefaultBrandName + " GEO"
		}
		add(brandTerm, bl.Name)
	}
	if len(lines) > 1 {
		for _, core := range lines[1].CoreTerms {
			add(DefaultBrandName+" "+core, lines[1].Name)
		}
	}
	return seeds, seedLines
}

// BusinessLineOfSeed 查询种子词所属业务线（未知返回 ""）
func BusinessLineOfSeed(seedLines map[string]string, seed string) string {
	return seedLines[seed]
}
