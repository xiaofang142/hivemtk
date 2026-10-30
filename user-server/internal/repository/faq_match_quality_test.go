package repository

import (
	"testing"

	"hivemtk-user/internal/model"
)

// 回归：官网客服 FAQ 匹配出现"答非所问"。
//
// 现象（2026-10-22 线上数据补全后实测）：
//   - 「不会写代码能用吗」→ 命中「商用需要买授权码吗？」（问的是会不会用，答的是授权）
//   - 「能做GEO优化吗」    → 命中「HiveMTK 的 AI 智能体能做什么？」
//   - 「多少钱」          → 命中「私有化部署需要什么配置？」
//
// 根因一：关键词命中**按"共用词元"判定**。中文二元组太容易巧合 ——
//   「不会写代码能用吗」与「商用需要买授权码吗？」共享"能"字元，
//   于是"商用""要钱吗""收费吗"这些**与问法毫无关系**的关键词被算成命中。
//   答非所问比匹配不上更糟。
//
// 根因二：关键词命中**每命中一个就独立加分**（0.7/次），而问法相似度整个才 0~1
//   ⇒ 关键词多、问法不匹配的那条靠堆砌压过问法更贴近的条目。
//
// 现在的口径：
//   - **只认精确命中**（关键词原样是用户句的连续子串）。语序变了、字面不连续的
//     同义问法匹配不上 —— 那种交给问法相似度与知识库 RAG 层兜底，不在这里硬凑；
//   - 命中**个数**是整数、天然有序，问法相似度只做同分时的决胜项，
//     相似度高不能翻盘命中数不同的两条。
//
// 判据用**生产真实形态**的 FAQ 集合（线上已有 10 条 + 本轮补的 19 条）。

func websiteFAQSet() []model.FAQEntry {
	enabled := true
	kw := func(s ...string) []string { return s }
	return []model.FAQEntry{
		{ID: 1, Question: "HiveMTK 是什么？", Answer: "HiveMTK 是一款开源的智能营销系统（SaaS 化多租户），提供 AI 智能体客服、多渠道消息接入。",
			Keywords: kw("是什么", "介绍", "简介", "产品", "项目", "营销系统", "开源", "做什么", "平台", "HiveMTK"), Confidence: 0.95, Enabled: &enabled},
		{ID: 2, Question: "HiveMTK 支持哪些渠道？", Answer: "支持 13 个渠道：Telegram、QQ、企业微信、飞书、WhatsApp、钉钉、抖音、小红书、快手、闲鱼、TikTok，以及网页客服。",
			Keywords: kw("渠道", "接入", "Telegram", "QQ", "企业微信", "飞书", "WhatsApp", "钉钉", "抖音", "小红书", "快手", "闲鱼", "TikTok", "网页客服"), Confidence: 0.95, Enabled: &enabled},
		{ID: 3, Question: "HiveMTK 怎么收费？", Answer: "基础模块免费常开，17 个增值模块中 16 个 0 元开通、仅浏览器自动化 ¥1 买断，LLM 算力四档 ¥1/¥3/¥6/¥9，软件订阅 ¥9/月。",
			Keywords: kw("收费", "价格", "多少钱", "计费", "订阅", "套餐", "积分", "免费", "购买", "费用"), Confidence: 0.90, Enabled: &enabled},
		{ID: 4, Question: "HiveMTK 支持私有化部署吗？", Answer: "支持私有化部署（Docker Compose），也提供 SaaS 云端版。",
			Keywords: kw("私有化", "部署", "Docker", "安装", "本地", "服务器", "环境", "私有化部署"), Confidence: 0.90, Enabled: &enabled},
		{ID: 5, Question: "HiveMTK 的 AI 智能体能做什么？", Answer: "AI 智能体可 7×24 小时自动应答客户消息：绑定大模型、挂载知识库做 RAG 问答、配置 SOP 工作流。",
			Keywords: kw("智能体", "AI", "RAG", "自动回复", "功能", "能力", "销冠", "自动应答", "有什么功能", "产品功能", "功能有哪些"), Confidence: 0.90, Enabled: &enabled},
		{ID: 6, Question: "怎么联系 HiveMTK 官方？", Answer: "官网：xapptool.cn，在线体验：work.xapptool.cn。",
			Keywords: kw("联系", "微信", "人工", "客服", "联系方式", "官网", "客服呢", "找谁", "商务"), Confidence: 0.95, Enabled: &enabled},
		{ID: 7, Question: "HiveMTK 是什么技术架构？", Answer: "分层架构：接入层 → 编排层 → 能力层 → 数据层。",
			Keywords: kw("架构", "技术栈", "Go", "Vue", "pgvector", "Redis", "原理", "分层"), Confidence: 0.90, Enabled: &enabled},
		{ID: 8, Question: "HiveMTK 的数据安全吗？会出域吗？", Answer: "100% 私有化部署，数据零出域。",
			Keywords: kw("安全", "出域", "隐私", "数据", "合规", "私有", "零出域"), Confidence: 0.95, Enabled: &enabled},
		{ID: 9, Question: "什么是资产市场？", Answer: "共享能力中心，可发布与订阅智能体资产包、话术包、FAQ、SOP 等。",
			Keywords: kw("资产市场", "资产包", "资产", "共享", "市场"), Confidence: 0.95, Enabled: &enabled},
		{ID: 10, Question: "AI 智能体在哪里配置？后台怎么操作？", Answer: "管理后台 → AI 智能体，可创建/编辑智能体。",
			Keywords: kw("智能体", "配置", "后台", "绑定", "人设", "欢迎语", "渠道绑定"), Confidence: 0.95, Enabled: &enabled},
		{ID: 20, Question: "HiveMTK 和 Dify 有什么区别？", Answer: "Dify 是通用 LLM 应用平台；HiveMTK 是私域营销操作系统。",
			Keywords: kw("Dify", "扣子", "区别", "对比", "相比", "差异", "和Dify比"), Confidence: 0.90, Enabled: &enabled},
		{ID: 21, Question: "HiveMTK 支持哪些渠道接入？", Answer: "支持 13 个渠道统一接入。",
			Keywords: kw("渠道", "接入", "Telegram", "QQ", "企业微信", "飞书", "WhatsApp", "钉钉", "抖音", "小红书", "快手", "闲鱼", "TikTok", "网页客服", "端"), Confidence: 0.95, Enabled: &enabled},
		{ID: 22, Question: "私有化部署需要什么配置？", Answer: "最低 4 核 / 8 GB / 60 GB SSD，推荐 8 核 / 16 GB。",
			Keywords: kw("配置", "CPU", "内存", "显卡", "GPU", "多少核", "几个G", "服务器", "硬件", "多大", "机器"), Confidence: 0.90, Enabled: &enabled},
		{ID: 23, Question: "私有化部署怎么操作？", Answer: "配 .env 后 docker compose up -d。",
			Keywords: kw("部署", "安装", "搭建", "Docker", "compose", "步骤", "操作", "怎么部署"), Confidence: 0.90, Enabled: &enabled},
		{ID: 24, Question: "不会写代码能用吗？", Answer: "能。私有化部署只需配 .env 跑 Docker Compose。",
			Keywords: kw("不会写代码", "小白", "零代码", "门槛", "上手", "需要技术", "能懂", "二次开发"), Confidence: 0.90, Enabled: &enabled},
		{ID: 25, Question: "这个项目是开源的吗？", Answer: "开源。协议是 AGPL-3.0。",
			Keywords: kw("开源", "AGPL", "协议", "源码", "GitHub", "Gitee", "授权码", "自托管"), Confidence: 0.95, Enabled: &enabled},
		{ID: 26, Question: "商用需要买授权码吗？", Answer: "不需要。AGPL-3.0 完全开源。",
			Keywords: kw("授权码", "商用", "企业用", "采购", "买授权", "要钱吗", "收费吗"), Confidence: 0.90, Enabled: &enabled},
		{ID: 27, Question: "有 SaaS 版本吗？不用自己部署行不行？", Answer: "有。云端版在 work.xapptool.cn。",
			Keywords: kw("SaaS", "saas", "云端", "托管", "不用部署", "在线用", "版本"), Confidence: 0.90, Enabled: &enabled},
		{ID: 28, Question: "数据安全吗？会传到你们服务器吗？（隐私与出域）", Answer: "数据完全在你自己的环境。",
			Keywords: kw("安全", "出域", "隐私", "数据", "合规", "上传", "泄露", "零出域", "私有"), Confidence: 0.95, Enabled: &enabled},
		{ID: 29, Question: "HiveMTK 的 AI 智能体和普通客服机器人有什么区别？", Answer: "不是关键词匹配，是 ReAct 智能体。",
			Keywords: kw("智能体", "ReAct", "机器人", "区别", "AI", "工具", "和普通"), Confidence: 0.90, Enabled: &enabled},
		{ID: 30, Question: "怎么创建和配置智能体？", Answer: "管理后台新建智能体。",
			Keywords: kw("智能体", "创建", "新建", "配置", "后台", "绑定", "人设", "渠道绑定", "欢迎语"), Confidence: 0.90, Enabled: &enabled},
		{ID: 31, Question: "知识库怎么建？RAG 问答效果如何？", Answer: "上传文档自动切片向量化。",
			Keywords: kw("知识库", "RAG", "检索", "召回", "切片", "向量化", "rag", "文档", "建库"), Confidence: 0.90, Enabled: &enabled},
		{ID: 32, Question: "HiveMTK 能做 GEO 优化吗？", Answer: "能，内置 GEO 引擎。",
			Keywords: kw("GEO", "AI搜索", "品牌", "提及", "搜索引擎", "被AI推荐", "生成式引擎"), Confidence: 0.90, Enabled: &enabled},
		{ID: 33, Question: "支持哪些大模型？能用我自己的 API Key 吗？", Answer: "DeepSeek、通义、Qwen 等，也可用自己的 Key。",
			Keywords: kw("大模型", "模型", "DeepSeek", "通义", "Qwen", "API Key", "自己的", "本地模型", "GPT", "GLM", "Kimi"), Confidence: 0.90, Enabled: &enabled},
		{ID: 34, Question: "支持哪些语言？能做多语言营销吗？", Answer: "内置多语言管理模块。",
			Keywords: kw("语言", "多语言", "翻译", "英文", "出海", "国际化", "i18n", "日文", "阿拉伯文"), Confidence: 0.85, Enabled: &enabled},
		{ID: 35, Question: "能接入 Telegram 吗？", Answer: "能。",
			Keywords: kw("Telegram", "电报", "TG", "Bot", "机器人", "群", "FRP"), Confidence: 0.90, Enabled: &enabled},
		{ID: 36, Question: "部署报错怎么办？", Answer: "先看容器日志。",
			Keywords: kw("报错", "错误", "失败", "启动不了", "起不来", "异常", "排查", "error", "trouble", "日志"), Confidence: 0.85, Enabled: &enabled},
		{ID: 37, Question: "怎么联系你们？能加微信吗？（找人工客服）", Answer: "官网留言或 GitHub Issue。",
			Keywords: kw("联系", "微信", "人工", "联系方式", "客服呢", "找谁", "商务", "邮箱"), Confidence: 0.95, Enabled: &enabled},
		{ID: 38, Question: "能怎么帮我的业务？举个例子", Answer: "统一收件箱、SOP 自动推进、CDP 画像等。",
			Keywords: kw("能干什么", "帮我", "场景", "例子", "业务价值", "解决什么", "有什么用", "适合"), Confidence: 0.85, Enabled: &enabled},
	}
}

// TestFAQMatch_NoWrongAnswer 官网常见问法必须命中**正确**的 FAQ（不只是"命中了某条"）。
//
// 判据两层：
//  1. 命中的必须是**指定**那条（答非所问比匹配不上更糟）；
//  2. 命中分要过 0.6 门槛（过不了就走 RAG 弱召回 → 低置信 → 转人工）。
func TestFAQMatch_NoWrongAnswer(t *testing.T) {
	repo := &FAQRepository{}
	entries := websiteFAQSet()

	cases := []struct {
		query string
		want  string
	}{
		{"介绍下项目", "HiveMTK 是什么？"},
		{"多少钱", "HiveMTK 怎么收费？"},
		{"价格套餐怎么订阅", "HiveMTK 怎么收费？"},
		{"支持哪些渠道", "HiveMTK 支持哪些渠道？"},
		{"怎么安装部署", "私有化部署怎么操作？"},
		{"部署需要什么配置", "私有化部署需要什么配置？"},
		{"和Dify有什么区别", "HiveMTK 和 Dify 有什么区别？"},
		{"数据安全吗", "HiveMTK 的数据安全吗？会出域吗？"},
		{"会出域吗", "HiveMTK 的数据安全吗？会出域吗？"},
		{"有SaaS吗", "有 SaaS 版本吗？不用自己部署行不行？"},
		{"商用要买授权码吗", "商用需要买授权码吗？"},
		{"不会写代码能用吗", "不会写代码能用吗？"},
		{"AI智能体和普通客服机器人有啥区别", "HiveMTK 的 AI 智能体和普通客服机器人有什么区别？"},
		{"怎么创建智能体", "怎么创建和配置智能体？"},
		{"知识库怎么建", "知识库怎么建？RAG 问答效果如何？"},
		{"能做GEO优化吗", "HiveMTK 能做 GEO 优化吗？"},
		{"支持哪些大模型", "支持哪些大模型？能用我自己的 API Key 吗？"},
		{"能用我自己的API Key吗", "支持哪些大模型？能用我自己的 API Key 吗？"},
		{"支持哪些语言", "支持哪些语言？能做多语言营销吗？"},
		{"能接入Telegram吗", "HiveMTK 支持哪些渠道接入？"},
		{"部署报错怎么办", "部署报错怎么办？"},
		{"怎么联系你们", "怎么联系 HiveMTK 官方？"},
		{"能怎么帮我的业务", "能怎么帮我的业务？举个例子"},
		{"你们产品有什么功能", "HiveMTK 的 AI 智能体能做什么？"},
		// 上一轮修复留下的问法（.specify/bugs/website-chat-false-handoff）—— 保留判据不丢
		{"HiveMTK 是什么？", "HiveMTK 是什么？"},
		{"这个平台是做什么的", "HiveMTK 是什么？"},
		{"平台能接入哪些渠道", "HiveMTK 支持哪些渠道接入？"},
		{"我想知道价格套餐，怎么订阅？", "HiveMTK 怎么收费？"},
		{"怎么收费", "HiveMTK 怎么收费？"},
		{"支持私有化部署吗", "HiveMTK 支持私有化部署吗？"},
		{"部署需要什么环境", "HiveMTK 支持私有化部署吗？"},
		{"AI 智能体能做什么", "HiveMTK 的 AI 智能体能做什么？"},
		{"什么是资产市场", "什么是资产市场？"},
		{"怎么配置智能体", "AI 智能体在哪里配置？后台怎么操作？"},
		{"怎么用", "能怎么帮我的业务？举个例子"},
		{"我要找人工客服", "怎么联系 HiveMTK 官方？"},
	}

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			got, err := repo.scoreAndRank(entries, tc.query, 3)
			if err != nil {
				t.Fatalf("scoreAndRank 失败: %v", err)
			}
			if len(got) == 0 {
				t.Fatalf("「%s」一条 FAQ 都没匹配上", tc.query)
			}
			top := got[0]
			if top.Question != tc.want {
				t.Errorf("「%s」应命中 %q，实际命中 %q（分 %.4f）—— 答非所问比匹配不上更糟",
					tc.query, tc.want, top.Question, top.Confidence)
			}
			if top.Confidence < 0.6 {
				t.Errorf("「%s」命中分 %.4f < 0.6，达不到 Layer1 命中门槛（会走 RAG 弱召回 → 转人工）",
					tc.query, top.Confidence)
			}
		})
	}
}

// TestTokenize_EnglishWordInChineseSentenceKeepsWholeWord 夹在中文里的英文要整体成词。
//
// 早先只把 CJK 二元组与单字入词，英文被按字符切成 2 字碎片
// （"Telegram" → le/te/gr/am…），于是英文关键词的匹配全是碎片乱撞。
func TestTokenize_EnglishWordInChineseSentenceKeepsWholeWord(t *testing.T) {
	tokens := tokenize("能接入 Telegram 吗")
	found := false
	for _, tk := range tokens {
		if tk == "telegram" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("英文单词应整体成词，实际 token 集合 %v", tokens)
	}
	// 不应出现 2 字碎片
	for _, tk := range tokens {
		if tk == "le" || tk == "te" || tk == "gr" {
			t.Errorf("英文单词不应被切成 2 字碎片，出现了 %q（token 集合 %v）", tk, tokens)
		}
	}
}
