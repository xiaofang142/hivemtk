package service

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

// TechStackAttrs 技术栈/概念属性维度（覆盖 hivemtk 涉及的每个技术、每个概念）
// 与业务线正交组合，生成"技术×业务"种子（如 "RAG智能客服"）。
// 分组：AI大模型技术 / 营销概念 / 销售概念 / 客服概念 / GEO与AI搜索概念 / 工程与数据技术。
var TechStackAttrs = []string{
	// AI大模型技术
	"人工智能", "大模型", "LLM", "RAG", "知识图谱", "向量数据库", "NLP",
	"深度学习", "机器学习", "智能体", "Agent", "MCP", "Function Calling",
	"提示词工程", "模型微调", "推理模型", "Tokenizer", "Embedding模型",
	"重排序", "混合检索", "向量检索", "语义搜索", "多模态", "TTS",
	"ASR", "Agent工作流", "多智能体", "记忆机制", "ReAct", "思维链",
	"LoRA", "模型量化", "模型蒸馏", "RLHF", "安全护栏", "幻觉检测", "引用溯源",
	// 营销概念
	"营销自动化", "内容营销", "社交媒体营销", "SEO", "SEM", "信息流广告",
	"AARRR", "PLG", "ABM", "SCRM", "CDP", "DMP", "用户画像",
	"裂变营销", "分销", "KOC", "KOL", "直播带货", "私域流量",
	"归因模型", "UTM", "留存", "复购", "客户成功", "营销漏斗",
	// 销售概念
	"SDR", "BDR", "CRM", "SFA", "MEDDIC", "SPIN销售", "商机管理",
	"线索评分", "销售漏斗", "成单率", "客单价", "续费率",
	"交叉销售", "向上销售", "外呼", "电销", "客户跟进", "智能报价",
	// 客服概念
	"IVR", "语音识别", "语音合成", "SLA", "工单系统", "知识库",
	"FAQ", "人工坐席", "客服质检", "满意度", "首响时间", "解决率",
	"转人工率", "自助服务", "在线客服", "呼叫中心", "云客服", "全渠道",
	"多轮对话", "意图识别",
	// GEO与AI搜索概念
	"SOV声量份额", "E-E-A-T", "llms.txt", "robots.txt", "Schema标记",
	"JSON-LD", "知识面板", "AI Overviews", "引用率", "品牌提及",
	"零点击搜索", "答案引擎", "实体识别", "权威度", "AI搜索排名",
	// 工程与数据技术
	"Python", "Java", "Go", "Golang", "TypeScript", "React", "Vue", "Node.js",
	"Gin", "GORM", "MySQL", "PostgreSQL", "Redis", "Kafka", "Elasticsearch",
	"ClickHouse", "MongoDB", "Docker", "Kubernetes", "CI/CD",
	"微服务", "API网关", "服务网格", "Serverless", "云原生", "可观测性",
	"Prometheus", "Grafana", "链路追踪", "对象存储", "CDN", "负载均衡",
	"OAuth", "JWT", "WebSocket", "gRPC", "REST", "消息队列", "定时任务",
	"分布式锁", "缓存", "分库分表", "读写分离", "低代码", "RPA", "OCR",
	"BI", "数据仓库", "ETL", "数据湖", "Flink", "Spark", "数据中台",
	"数据治理", "H5", "小程序", "SaaS", "私有化部署", "混合云",
	"边缘计算", "物联网", "区块链", "数字孪生", "开源", "信创",
	"推荐系统", "搜索引擎", "PyTorch", "TensorFlow",
}

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
// 规模口径：48 核心词 + 4×184 技术/概念组合词 = 784 种子（4 线各 196）；
// 经 DefaultLongtailTemplates（23 模板）离线展开 18,032 长尾词（实测）；
// 再经 CrawlSuggest（5 引擎下拉）补充数千下拉词，全量经 SaveMiningResults 落库。
func BuildBusinessSeedMatrix() ([]string, map[string]string) {
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

	for _, bl := range BusinessLines {
		for _, core := range bl.CoreTerms {
			add(core, bl.Name)
		}
		for _, tech := range TechStackAttrs {
			add(tech+bl.ShortName, bl.Name)
		}
	}
	return seeds, seedLines
}

// BusinessLineOfSeed 查询种子词所属业务线（未知返回 ""）
func BusinessLineOfSeed(seedLines map[string]string, seed string) string {
	return seedLines[seed]
}
