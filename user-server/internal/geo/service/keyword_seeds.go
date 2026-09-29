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

// TechStackAttrs 技术栈属性维度（8787 freeapi 离线产出 50 词为基 + 补充）
// 与业务线正交组合，生成"技术×业务"种子（如 "RAG智能客服"）。
var TechStackAttrs = []string{
	"人工智能", "大模型", "LLM", "RAG", "知识图谱", "向量数据库", "NLP",
	"深度学习", "机器学习", "Python", "PyTorch", "TensorFlow", "Java", "Go",
	"微服务", "Docker", "Kubernetes", "React", "Vue", "Node.js",
	"MySQL", "PostgreSQL", "Redis", "MongoDB", "Elasticsearch",
	"Serverless", "低代码", "数据中台", "BI", "云原生",
	"边缘计算", "物联网", "区块链", "数字孪生", "RPA", "OCR",
	"语音识别", "推荐系统", "搜索引擎", "开源",
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
// 规模口径：48 核心词 + 4×40 组合词 ≈ 208 种子；
// 经 DefaultLongtailTemplates（23 模板）离线展开约 4,700 长尾词。
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
