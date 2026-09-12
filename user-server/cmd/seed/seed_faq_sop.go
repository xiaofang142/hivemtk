// seed_faq_sop.go 模块 D2：FAQ 知识库 + SOP 模板（HiveMTK 开源项目支持方向）
//
// 覆盖表：
// faq_entries (50+) Layer1 FAQ 命中 (零 LLM <100ms) — HiveMTK 8 大类高频咨询
// sop_templates (25+) Layer1 SOP 模板拼接 (Go text/template) — HiveMTK 客服 5 阶段
//
// 设计依据： AI 智能体性能优化
// Layer1 双层架构：FAQ 命中 / SOP 模板拼接直接出答案，命中后 SkipLLM
// intent 维度：overview / deploy / ops / architecture / features / asset / community / general
// stage 维度：initial（开场）→ middle（需求澄清）→ late（方案介绍）→ objection（疑虑处理）→ closing（收尾引导）
//
// 内容要求：全部基于本项目真实事实（README / 迁移 031 知识库 / docs/ 设计文档），
// 不涉及定价、版本下载、注册开户等已下线内容；不含任何虚构用户数/满意度/品牌背书。
package main

import (
	"fmt"
	"log"

	"hivemtk-user/internal/model"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

type faqSopSeeder struct{}

func (s *faqSopSeeder) Name() string { return "faq_sop" }
func (s *faqSopSeeder) Description() string {
	return "FAQ知识库(50+ HiveMTK 8大类) + SOP模板(25+ 5阶段开源支持流)"
}

func (s *faqSopSeeder) Clean(database *gorm.DB) error {
	if _, err := cleanByCondition(database, &model.SOPTemplate{}, "name LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 sop_templates 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.FAQEntry{}, "question LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 faq_entries 失败: %w", err)
	}
	return nil
}

func (s *faqSopSeeder) Seed(database *gorm.DB, ctx *SeedContext) error {
	faqs := s.buildFAQs()
	if err := batchInsert(database, faqs, 50); err != nil {
		return fmt.Errorf("写入 faq_entries 失败: %w", err)
	}

	sops := s.buildSOPTemplates()
	if err := batchInsert(database, sops, 50); err != nil {
		return fmt.Errorf("写入 sop_templates 失败: %w", err)
	}

	log.Printf("  ✓ 已写入 FAQ %d 条 + SOP 模板 %d 条", len(faqs), len(sops))
	return nil
}

func (s *faqSopSeeder) buildFAQs() []model.FAQEntry {
	entries := make([]model.FAQEntry, 0, 80)
	entries = append(entries, s.faqOverview()...)
	entries = append(entries, s.faqDeploy()...)
	entries = append(entries, s.faqOps()...)
	entries = append(entries, s.faqArchitecture()...)
	entries = append(entries, s.faqFeatures()...)
	entries = append(entries, s.faqAsset()...)
	entries = append(entries, s.faqCommunity()...)
	entries = append(entries, s.faqGeneral()...)
	entries = append(entries, s.faqUrgency()...)
	entries = append(entries, s.faqSupport()...)
	return entries
}

// faqOverview 项目概述与定位 (7 条)
func (s *faqSopSeeder) faqOverview() []model.FAQEntry {
	trueVal := true
	specs := []struct {
		Q, A     string
		Keywords []string
		Conf     float64
	}{
		{
			Q:        "HiveMTK 是什么 " + seedTag,
			A:        "HiveMTK 是私域部署的 AI 营销操作系统，把三件事同时做透：七端社媒接入、ReAct 自主 AI 智能体、零出域数据安全。它内置能感知→规划→调工具→反思的自主智能体（ReAct 循环 + 41 个内置工具），采用 AGPL-3.0 开源协议。",
			Keywords: []string{"是什么", "介绍", "定位", "项目"},
			Conf:     0.97,
		},
		{
			Q:        "和 Dify、Coze 有什么区别 " + seedTag,
			A:        "HiveMTK 不是给大模型套壳，也不是写死的自动化脚本。核心差异：①七端渠道原生接入（抖音/快手/小红书/闲鱼/TikTok/微信/短信/邮件）②私有化零出域 ③自主智能体编排（非纯工作流）。Dify/Coze 偏工作流编排与云端 SaaS，HiveMTK 偏私域多渠道营销自动化与数据不出域。",
			Keywords: []string{"区别", "Dify", "Coze", "对比"},
			Conf:     0.93,
		},
		{
			Q:        "开源协议是什么 " + seedTag,
			A:        "采用 GNU AGPL-3.0。要点：修改后若通过网络（SaaS/云端/API/托管实例）对外提供服务，必须按 AGPL-3.0 向所有用户免费提供修改后的完整对应源代码；仅内部私有部署、不对外提供网络服务时无需公开修改。",
			Keywords: []string{"协议", "AGPL", "开源", "许可证"},
			Conf:     0.96,
		},
		{
			Q:        "仓库地址在哪 " + seedTag,
			A:        "Gitee 主仓库 https://gitee.com/xhpmayun/hivemtk ；GitHub 镜像 https://github.com/xiaofang142/hivemtk ；平台端仓库 hivemtk-platform 同样已开源。",
			Keywords: []string{"仓库", "github", "gitee", "源码"},
			Conf:     0.95,
		},
		{
			Q:        "是免费的吗 " + seedTag,
			A:        "项目本身完全开源免费，可自由使用与私有部署。企业级技术支持、定制集成等商务合作可通过 jideilvluoqun@gmail.com 联系。",
			Keywords: []string{"免费", "收费", "价格", "商用"},
			Conf:     0.94,
		},
		{
			Q:        "支持 SaaS 或多租户吗 " + seedTag,
			A:        "不支持。HiveMTK 是私域独立部署：每个商户一套完整系统（user-server + PostgreSQL + Redis + 推理栈），无 merchant_id 字段，所有数据归属当前部署实例。禁止 SaaS/多租户模式。",
			Keywords: []string{"SaaS", "多租户", "部署模式"},
			Conf:     0.9,
		},
		{
			Q:        "核心特色是什么 " + seedTag,
			A:        "五大核心：①七端渠道打通 ②ReAct 自主智能体 + 41 个内置工具 ③三级 RAG 检索（向量召回 + bge-reranker 精排 + LLM 改写）④100% 私域零出域数据安全 ⑤资产市场 ISV 生态（agent_persona / sales_script / ab_test_plan / marketing_workflow / industry_sop）。",
			Keywords: []string{"特色", "核心", "卖点"},
			Conf:     0.92,
		},
	}
	out := make([]model.FAQEntry, 0, len(specs))
	for _, sp := range specs {
		out = append(out, model.FAQEntry{
			Question:   sp.Q,
			Answer:     sp.A,
			Keywords:   pq.StringArray(sp.Keywords),
			Category:   "overview",
			Intent:     "overview",
			Confidence: sp.Conf,
			HitCount:   int64(randInt(30, 1200)),
			Enabled:    &trueVal,
		})
	}
	return out
}

// faqDeploy 部署与初始化 (8 条)
func (s *faqSopSeeder) faqDeploy() []model.FAQEntry {
	trueVal := true
	specs := []struct {
		Q, A     string
		Keywords []string
		Conf     float64
	}{
		{
			Q:        "怎么安装 " + seedTag,
			A:        "三步：1) git clone https://gitee.com/xhpmayun/hivemtk.git && cd hivemtk；2) make install（自动生成 .env + docker-compose.yml + 构建前端）；3) vim .env 修改 4 个密钥（POSTGRES_PASSWORD / REDIS_PASSWORD / JWT_SECRET / PLATFORM_ADMIN_PASSWORD，可用 openssl rand -hex 24 生成），然后 make up 启动，访问 http://localhost:8204，默认账号 admin + 你设置的密码。",
			Keywords: []string{"安装", "部署", "make install", "上手"},
			Conf:     0.96,
		},
		{
			Q:        "硬件要求 " + seedTag,
			A:        "最低 2 核 CPU / 4GB 内存 / 50GB 磁盘；推荐生产 8 核+ / 16GB+ / 200GB+（含模型文件约 10GB）。dev 轻量档（Qwen2.5-1.5B-Instruct + Qwen3-Embedding-0.6B）8GB 即可；prod 重量档（Qwen2.5-14B-Instruct + bge-m3）需 16GB+。GPU 可选。前置要求 Docker 24+ & Docker Compose v2。",
			Keywords: []string{"硬件", "配置", "内存", "要求"},
			Conf:     0.94,
		},
		{
			Q:        "有哪些端口 " + seedTag,
			A:        "8204 user-server API（RESTful + WebSocket）；8202 PostgreSQL（宿主机映射 8232）；8203 Redis；8207 mtk-llm（llama.cpp，Qwen2.5-1.5B-Instruct）；8208 mtk-embedding（bge-m3，1024 维）；8209 mtk-rerank（bge-reranker-v2-m3）。健康检查：curl http://localhost:8204/health。",
			Keywords: []string{"端口", "port", "8204", "健康检查"},
			Conf:     0.93,
		},
		{
			Q:        "模型档位怎么切换 " + seedTag,
			A:        "编辑 .env 替换 LLM_*/EMBEDDING_* 三行。dev 轻量档（当前默认）：Qwen2.5-1.5B-Instruct(Q4) + Qwen3-Embedding-0.6B，内存 8GB；prod 重量档：Qwen2.5-14B-Instruct(Q4+) + BAAI/bge-m3(1024 维)，内存 16GB+。",
			Keywords: []string{"模型", "档位", "dev", "prod", "切换"},
			Conf:     0.9,
		},
		{
			Q:        "本地推理栈怎么启动 " + seedTag,
			A:        "make inference-host-install 安装 llama.cpp 二进制（首次）；make inference-host-models 下载 dev 档模型（首次）；make inference-host-up 启动 LLM+Embedding+Rerank 三个 llama-server；make inference-host-warmup 预热；make inference-host-test 端到端 smoke test；make inference-host-status 统一查看状态。",
			Keywords: []string{"推理栈", "llama", "inference", "模型服务"},
			Conf:     0.91,
		},
		{
			Q:        "公网访问怎么做 " + seedTag,
			A:        "用 FRP 私域穿透：配置 frpc.toml 指向平台端 frps，把本地 user-server 的 8204 端口暴露为公网子域名。访客从公网进，数据经隧道回本地，云端不落一条对话。详见 docs/architecture/FRP私域部署指南.md。纯内网部署不强制公网 IP。",
			Keywords: []string{"公网", "FRP", "穿透", "外网"},
			Conf:     0.88,
		},
		{
			Q:        "初始化流程是什么 " + seedTag,
			A:        "浏览器访问 http://your-server-ip:8204/setup：1) 设置超管账号（首次登录强制改密）；2) 完成系统初始化。私域部署无 LicenseKey 强制要求，初始化后默认管理员账号为 admin。",
			Keywords: []string{"初始化", "setup", "超管", "首登"},
			Conf:     0.89,
		},
		{
			Q:        "必须先启动本地推理栈吗 " + seedTag,
			A:        "如果 LLM 走本地 llama-server（默认 dev 档），需要先 make inference-host-up 把 8207/8208/8209 跑起来，否则对话/向量化无法工作。也可把 LLM_BASE_URL 改为 DeepSeek/OpenAI 等云端 API，但 Embedding/Rerank 仍强制本地。",
			Keywords: []string{"推理栈", "必须先", "本地模型", "云端"},
			Conf:     0.85,
		},
	}
	out := make([]model.FAQEntry, 0, len(specs))
	for _, sp := range specs {
		out = append(out, model.FAQEntry{
			Question:   sp.Q,
			Answer:     sp.A,
			Keywords:   pq.StringArray(sp.Keywords),
			Category:   "deploy",
			Intent:     "deploy",
			Confidence: sp.Conf,
			HitCount:   int64(randInt(30, 1200)),
			Enabled:    &trueVal,
		})
	}
	return out
}

// faqOps 运维与排障 (8 条)
func (s *faqSopSeeder) faqOps() []model.FAQEntry {
	trueVal := true
	specs := []struct {
		Q, A     string
		Keywords []string
		Conf     float64
	}{
		{
			Q:        "常用运维命令 " + seedTag,
			A:        "make install 一键安装；make up 启动所有服务；make down 停止；make restart 重启；make logs 查看 user-server 日志；make ps 查看服务状态；make inference-up/down 单独管理本地推理栈；make web-build / sdk-build 重新构建前端与 embed-sdk；make backup / restore FILE=... 备份恢复。",
			Keywords: []string{"运维", "命令", "make", "常用"},
			Conf:     0.95,
		},
		{
			Q:        "怎么备份数据库 " + seedTag,
			A:        "make db-backup 备份 PostgreSQL（输出 backup_YYYYMMDD_HHMMSS.sql）；make db-restore FILE=backup_xxx.sql 恢复。备份文件为纯 SQL，可直接 psql 导入。建议生产环境每日自动备份。",
			Keywords: []string{"备份", "restore", "数据库", "pg"},
			Conf:     0.93,
		},
		{
			Q:        "本地开发热更新 " + seedTag,
			A:        "make dev-install 安装 air 热更新工具；make dev 启动 user-server 热更新（监听 .go/.yaml/.html 自动重编+重启）；前端开发 cd user-web && npm run dev 启动 Vite 开发服务器。",
			Keywords: []string{"开发", "热更新", "dev", "air"},
			Conf:     0.9,
		},
		{
			Q:        "日志怎么配置 " + seedTag,
			A:        "user-server/config.yaml 的 logging 段：level（debug/info/warn/error）、format（json 生产便于采集 / console 本地带颜色）、output（stdout/file/both）、file 路径（超过 max_size(MB) 自动滚动保留 1 份备份）、component 写入每条日志的 service 标识。",
			Keywords: []string{"日志", "log", "logging", "配置"},
			Conf:     0.88,
		},
		{
			Q:        "user-server 构建报随机错误 " + seedTag,
			A:        "user-server 构建缓存易损坏，若遇随机 undefined/EOF 报错，先 go clean -cache 再编译：cd hivemtk/user-server && go clean -cache && go build ./...。若仍失败检查 Go 版本需 1.25+。",
			Keywords: []string{"构建", "报错", "build", "缓存"},
			Conf:     0.91,
		},
		{
			Q:        "怎么看推理栈状态 " + seedTag,
			A:        "make inference-host-status 显示数据层容器状态、llama-server 进程、端点连通性（8207 LLM / 8208 Embedding / 8209 Rerank / 8204 user-server，各端点 /health 返回 200 即正常）。make inference-host-logs tail 三个 llama-server 日志。",
			Keywords: []string{"状态", "推理栈", "status", "health"},
			Conf:     0.87,
		},
		{
			Q:        "端口被占用怎么办 " + seedTag,
			A:        "确认 8202-8209 空闲，停掉占用进程或改 docker-compose 的端口映射。PostgreSQL 容器内 8202，宿主机默认映射 8232；Redis 8203。被占用会导致服务起不来。",
			Keywords: []string{"端口占用", "冲突", "起不来"},
			Conf:     0.84,
		},
		{
			Q:        "怎么升级到新版 " + seedTag,
			A:        "git pull 最新代码后：make web-build 与 make sdk-build 重新构建前端与客服 SDK，再 make up --force-recreate 重建服务。数据库结构由迁移脚本演进，按序执行迁移即可。",
			Keywords: []string{"升级", "更新", "版本", "pull"},
			Conf:     0.86,
		},
	}
	out := make([]model.FAQEntry, 0, len(specs))
	for _, sp := range specs {
		out = append(out, model.FAQEntry{
			Question:   sp.Q,
			Answer:     sp.A,
			Keywords:   pq.StringArray(sp.Keywords),
			Category:   "ops",
			Intent:     "ops",
			Confidence: sp.Conf,
			HitCount:   int64(randInt(20, 900)),
			Enabled:    &trueVal,
		})
	}
	return out
}

// faqArchitecture 架构与技术 (7 条)
func (s *faqSopSeeder) faqArchitecture() []model.FAQEntry {
	trueVal := true
	specs := []struct {
		Q, A     string
		Keywords []string
		Conf     float64
	}{
		{
			Q:        "整体架构是怎样的 " + seedTag,
			A:        "访客浏览器（公网）经 HTTPS/WSS（FRP/公网 IP/反代）→ 客户本地用户端（user-server Go+Gin :8204，含 PostgreSQL :8202、Redis :8203、mtk-llm :8207、mtk-embedding :8208、mtk-rerank :8209）→ 平台端（独立仓库 hivemtk-platform，仅做版本检查/商户标识校验/官方支持，不碰业务数据）。",
			Keywords: []string{"架构", "整体", "用户端", "平台端"},
			Conf:     0.93,
		},
		{
			Q:        "Go 代码规范 " + seedTag,
			A:        "严格遵守分层架构：Controller（接口层）→ Service（业务层）→ Repository（数据访问层）→ Model（数据模型层）→ Infra（基础设施层）。禁止 controller 直访 db/repository、service 直访 db、model 含业务方法、dto 反向引用 service。检查脚本 scripts/check-architecture.sh 已集成 CI。",
			Keywords: []string{"分层", "架构规范", "go", "CI"},
			Conf:     0.9,
		},
		{
			Q:        "RAG 问答怎么工作 " + seedTag,
			A:        "消息 → 本地 Embedding（TEI + bge-m3，1024 维）→ pgvector 向量检索 → Top-K 知识片段 → 拼装 Prompt → LLM 调用（外部 API：Qwen/Claude/GPT）→ 敏感词过滤 → 返回 reply+sources。Embedding/Rerank 强制本地，LLM 可走外部 API。",
			Keywords: []string{"RAG", "向量", "检索", "流程"},
			Conf:     0.92,
		},
		{
			Q:        "三级 RAG 检索是什么 " + seedTag,
			A:        "①粗排——向量召回（pgvector + bge-m3 embedding，1024 维）②精排——bge-reranker-v2-m3 重排（多语言跨编码器）③LLM 改写——HyDE / Query Rewriter 优化查询。置信度阈值默认 0.7，低于阈值降级到通用 LLM。多轮对话保留 3-5 轮上下文。",
			Keywords: []string{"三级", "rerank", "改写", "精排"},
			Conf:     0.9,
		},
		{
			Q:        "数据安全怎么保证 " + seedTag,
			A:        "100% 私域零出域：本地 AI 推理栈（llama.cpp + TEI）跑在客户内网；对话/知识库/向量化/检索增强全程在客户内网完成；FRP 穿透时访客公网进、数据经隧道回本地，云端不落一条对话；满足等保、数据出境管控、私有化部署基线。",
			Keywords: []string{"数据安全", "零出域", "隐私", "私域"},
			Conf:     0.94,
		},
		{
			Q:        "多智能体怎么协作 " + seedTag,
			A:        "三个核心实体：AIAgent（智能体主表）、ChannelAgentBinding（渠道账号↔智能体，同渠道可绑多个但仅一个 is_primary=true）、CustomerServiceAgent（座席↔智能体）。智能体类型 sales/customer_service/hybrid；工作模式 passive（消息进入系统调用）/active（智能体主动触达）。",
			Keywords: []string{"智能体", "多智能体", "绑定", "协作"},
			Conf:     0.88,
		},
		{
			Q:        "转人工策略是什么 " + seedTag,
			A:        "chat.transfer_keywords 命中（人工/真人/转人工/human/operator/客服/agent/找人）自动转人工；连续 5 次 AI 回复（MaxAIConsecutive=5）后建议转人工；命中关键词后 30 秒内无人工接入提示「客服正在接入中」（chat.fallback_seconds=30）。",
			Keywords: []string{"转人工", "transfer", "人工", "策略"},
			Conf:     0.89,
		},
	}
	out := make([]model.FAQEntry, 0, len(specs))
	for _, sp := range specs {
		out = append(out, model.FAQEntry{
			Question:   sp.Q,
			Answer:     sp.A,
			Keywords:   pq.StringArray(sp.Keywords),
			Category:   "architecture",
			Intent:     "architecture",
			Confidence: sp.Conf,
			HitCount:   int64(randInt(20, 800)),
			Enabled:    &trueVal,
		})
	}
	return out
}

// faqFeatures 功能模块 (8 条)
func (s *faqSopSeeder) faqFeatures() []model.FAQEntry {
	trueVal := true
	specs := []struct {
		Q, A     string
		Keywords []string
		Conf     float64
	}{
		{
			Q:        "支持哪些渠道 " + seedTag,
			A:        "七端打通：抖音（触达/智能卡片/自动回复/RAG 客服，含直播私信）、快手（同抖音）、小红书（含私信评论）、闲鱼（二手商品场景）、TikTok（海外矩阵）、微信/企业微信（含社群朋友圈）、短信（多通道营销）、邮件（SMTP/163/QQ）。统一 CDP 客户视图，统一消息中心。",
			Keywords: []string{"渠道", "七端", "抖音", "小红书"},
			Conf:     0.95,
		},
		{
			Q:        "抖音/小红书怎么接入 " + seedTag,
			A:        "在各渠道后台配置账号与凭证即可接入。抖音支持触达/智能卡片/自动回复/RAG 客服（含直播私信）；小红书含私信与评论。使用主动触达能力必须遵守各平台规范，仅向已授权联系人发送内容。",
			Keywords: []string{"接入", "抖音", "小红书", "配置"},
			Conf:     0.9,
		},
		{
			Q:        "什么是 ReAct 自主智能体 " + seedTag,
			A:        "ReAct 循环——感知→规划→调工具→反思（最多 5 轮），智能体自主决策而非套死脚本；内置 41 个工具（查库存、查物流、查客户画像、改地址、加白名单等）；配合三级 RAG 与多智能体协作（被动应答 + 主动触达）。",
			Keywords: []string{"ReAct", "智能体", "工具", "自主"},
			Conf:     0.92,
		},
		{
			Q:        "有哪些功能模块 " + seedTag,
			A:        "62 个核心业务模块按域分类：认证与用户管理 4 个、多平台卡片 5 个、自动回复+RAG 6 个、邮件营销 5 个、短信营销 4 个、社群管理 4 个、短链与活码 3 个、线索与客户 9 个、营销自动化 6 个、内容创作 4 个、系统管理 6 个、第三方对接 2 个、统一消息 2 个。",
			Keywords: []string{"模块", "功能", "62", "业务域"},
			Conf:     0.9,
		},
		{
			Q:        "嵌入式客服怎么接 " + seedTag,
			A:        "embed-sdk（原生 JS IIFE + iframe + postMessage）可嵌入任意第三方网站。私域部署通过 frp 暴露的 user-server 前台聊天窗 SPA；进入后自动调用 /api/chat/public/* 与 /api/ws/visitor 完成双向会话。渠道 ID 软解析顺序：ctx.chat_channel_id > body.channel_id > X-Chat-Channel-Id > 默认 default。",
			Keywords: []string{"嵌入式", "客服", "widget", "embed", "iframe"},
			Conf:     0.88,
		},
		{
			Q:        "AI 销冠是什么 " + seedTag,
			A:        "话术模板 + RAG + 自动跟进的全流程坐席辅助能力：内置销冠话术库、异议处理模板与可视化 SOP，辅助人工坐席提升转化，而非替代人工决策。",
			Keywords: []string{"AI销冠", "话术", "坐席", "辅助"},
			Conf:     0.87,
		},
		{
			Q:        "可视化工作流 " + seedTag,
			A:        "营销自动化编辑器支持零代码搭建 SOP/用户旅程自动化（营销_workflow 资产类型），可编排触发条件、分支、触达动作并织入 RAG 与智能体。",
			Keywords: []string{"工作流", "自动化", "SOP", "零代码"},
			Conf:     0.85,
		},
		{
			Q:        "客服转人工怎么配 " + seedTag,
			A:        "在 chat 配置中设置 transfer_keywords（如「人工」「转人工」）即可命中转人工；前端对用户永远显示「在线客服」，企业内部分为 AI 客服 + 人工客服。30 秒无人工接入提示「客服正在接入中」。",
			Keywords: []string{"转人工", "配置", "关键词", "客服"},
			Conf:     0.86,
		},
	}
	out := make([]model.FAQEntry, 0, len(specs))
	for _, sp := range specs {
		out = append(out, model.FAQEntry{
			Question:   sp.Q,
			Answer:     sp.A,
			Keywords:   pq.StringArray(sp.Keywords),
			Category:   "features",
			Intent:     "features",
			Confidence: sp.Conf,
			HitCount:   int64(randInt(50, 1500)),
			Enabled:    &trueVal,
		})
	}
	return out
}

// faqAsset 资产市场与 ISV (6 条)
func (s *faqSopSeeder) faqAsset() []model.FAQEntry {
	trueVal := true
	specs := []struct {
		Q, A     string
		Keywords []string
		Conf     float64
	}{
		{
			Q:        "资产市场是什么 " + seedTag,
			A:        "三端闭环：平台端 platform-server（资产包商城/中台，唯一数据源 + 分发中心，负责存储/审核上架/购买分发/贡献者账户/使用统计）；开发者端（ISV/商户内 Playground，生产者，调教 messages 后提交平台审核上架）；商户端 user-server+user-web（消费者/运行者，浏览/购买试用/拉取到本地/运行织入自身 RAG）。数据流铁律：user-web→user-server→platform-server，禁止 user-web 直连平台。",
			Keywords: []string{"资产市场", "ISV", "三端", "平台"},
			Conf:     0.92,
		},
		{
			Q:        "资产包有哪些类型 " + seedTag,
			A:        "5 大类：①agent_persona 智能体角色（人设+开场白+语气）②sales_script 销冠话术 ③ab_test_plan AB 测试方案 ④marketing_workflow 自动化工作流 ⑤industry_sop 行业 SOP 模板。数据格式 JSONB，存于 local_asset_data 表。",
			Keywords: []string{"资产包", "类型", "persona", "sop"},
			Conf:     0.9,
		},
		{
			Q:        "怎么从市场获取资产 " + seedTag,
			A:        "业务链 8 步：开发者 Playground 调教→提交平台审核→运营审核通过→商户浏览市场试用/购买→SyncPull 拉取到本地 local_assets + local_asset_data→客服系统 LoadByType 加载织入 RAG（use_count 累加）→使用次数回传平台（best-effort 不阻塞主流程）。",
			Keywords: []string{"获取", "拉取", "SyncPull", "使用"},
			Conf:     0.89,
		},
		{
			Q:        "怎么成为贡献者/ISV " + seedTag,
			A:        "在 Playground 调教 messages→提交平台审核→通过后上架到资产市场。请先阅读 CONTRIBUTING.md 与贡献者公约（群规：禁止广告/政治/人肉，违者秒踢）。",
			Keywords: []string{"贡献者", "ISV", "上架", "Playground"},
			Conf:     0.87,
		},
		{
			Q:        "资产数据格式 " + seedTag,
			A:        "资产包以 JSONB 存储于 local_asset_data 表，包含人设（Persona）、话术库（ScriptLibrary）、SOP（industry_sop）、工作流（marketing_workflow）等字段，由客服系统 LoadByType 按类型加载。",
			Keywords: []string{"格式", "JSONB", "local_asset_data", "存储"},
			Conf:     0.84,
		},
		{
			Q:        "使用资产要付费吗 " + seedTag,
			A:        "开源资产免费；平台市场部分资产可能由 ISV 自行定价，本仓库仅做种子演示（记录成本不实际接入支付）。企业级集成与定制联系平台运营商务邮箱。",
			Keywords: []string{"付费", "免费", "定价", "ISV"},
			Conf:     0.83,
		},
	}
	out := make([]model.FAQEntry, 0, len(specs))
	for _, sp := range specs {
		out = append(out, model.FAQEntry{
			Question:   sp.Q,
			Answer:     sp.A,
			Keywords:   pq.StringArray(sp.Keywords),
			Category:   "asset",
			Intent:     "asset",
			Confidence: sp.Conf,
			HitCount:   int64(randInt(20, 600)),
			Enabled:    &trueVal,
		})
	}
	return out
}

// faqCommunity 社区与支持 (6 条)
func (s *faqSopSeeder) faqCommunity() []model.FAQEntry {
	trueVal := true
	specs := []struct {
		Q, A     string
		Keywords []string
		Conf     float64
	}{
		{
			Q:        "怎么报 Bug 或提需求 " + seedTag,
			A:        "Bug / Feature Request 走 Gitee Issues（12 小时内首响）：https://gitee.com/xhpmayun/hivemtk/issues 。提交前请先检索是否已有重复项。",
			Keywords: []string{"bug", "issue", "需求", "反馈"},
			Conf:     0.93,
		},
		{
			Q:        "有交流群吗 " + seedTag,
			A:        "微信交流群管理员 wxid: xiao142000，提供 7x24 答疑（产品/技术/运营）。群规：禁止广告、禁止政治、禁止人肉，违者秒踢。",
			Keywords: []string{"群", "微信", "交流", "社区"},
			Conf:     0.9,
		},
		{
			Q:        "商务合作怎么联系 " + seedTag,
			A:        "企业级技术支持、定制集成等商务合作请发邮件至 jideilvluoqun@gmail.com。",
			Keywords: []string{"商务", "合作", "邮箱", "企业"},
			Conf:     0.88,
		},
		{
			Q:        "怎么贡献代码 " + seedTag,
			A:        "Fork → 分支 → PR。先读 CONTRIBUTING.md 与 Go 分层架构规范；CI 含 scripts/check-architecture.sh 架构检查。PR 描述请说明动机与测试方式。",
			Keywords: []string{"贡献", "PR", "fork", "代码"},
			Conf:     0.87,
		},
		{
			Q:        "有哪些文档 " + seedTag,
			A:        "仓库 docs/ 目录含架构（GO_FIVE_LAYER_ARCHITECTURE、FRP 私域部署指南）、营销特性（agent-rag-qa 等）、部署指南；README 含快速开始与常见问题。平台端 hivemtk-platform 也有独立文档。",
			Keywords: []string{"文档", "docs", "README", "指南"},
			Conf:     0.86,
		},
		{
			Q:        "版本怎么更新 " + seedTag,
			A:        "用户端 git pull 最新后按升级流程重建；平台端会通过低频 HTTPS 心跳做版本检查与商户标识校验，不接触用户业务数据。",
			Keywords: []string{"版本", "更新", "心跳", "检查"},
			Conf:     0.83,
		},
	}
	out := make([]model.FAQEntry, 0, len(specs))
	for _, sp := range specs {
		out = append(out, model.FAQEntry{
			Question:   sp.Q,
			Answer:     sp.A,
			Keywords:   pq.StringArray(sp.Keywords),
			Category:   "community",
			Intent:     "community",
			Confidence: sp.Conf,
			HitCount:   int64(randInt(10, 500)),
			Enabled:    &trueVal,
		})
	}
	return out
}

// faqGeneral 通用 (5 条)
func (s *faqSopSeeder) faqGeneral() []model.FAQEntry {
	trueVal := true
	specs := []struct {
		Q, A     string
		Keywords []string
		Conf     float64
	}{
		{
			Q:        "你好 " + seedTag,
			A:        "您好，我是 HiveMTK 官方客服助手，可为您解答关于项目开源信息、部署、运维、架构、资产市场、AI 智能体等问题。请问有什么可以帮您？",
			Keywords: []string{"你好", "hi", "hello"},
			Conf:     0.9,
		},
		{
			Q:        "在吗 " + seedTag,
			A:        "在的呢，请问有什么可以帮您？部署、运维、功能、资产市场相关问题都可以问我。",
			Keywords: []string{"在吗", "在不在", "客服"},
			Conf:     0.88,
		},
		{
			Q:        "能帮我把项目跑起来吗 " + seedTag,
			A:        "可以。最简三步：git clone https://gitee.com/xhpmayun/hivemtk.git && cd hivemtk → make install → vim .env 改 4 个密钥后 make up，访问 http://localhost:8204（账号 admin + 你设的密码）。需要哪一步的详细说明我都可以展开。",
			Keywords: []string{"跑起来", "启动", "上手", "帮助"},
			Conf:     0.85,
		},
		{
			Q:        "这个能商用吗 " + seedTag,
			A:        "AGPL-3.0 允许商用与修改。关键义务：若修改后通过网络对外提供服务，必须开源修改后的完整源码；仅内部私有部署不对外服务时无需公开。请遵守各渠道平台规范与所在地法律法规。",
			Keywords: []string{"商用", "AGPL", "合规", "授权"},
			Conf:     0.86,
		},
		{
			Q:        "感谢 " + seedTag,
			A:        "不客气，祝部署顺利！更多帮助见 GitHub/Gitee 仓库或微信交流群（管理员 wxid: xiao142000）。",
			Keywords: []string{"感谢", "谢谢", "辛苦"},
			Conf:     0.9,
		},
	}
	out := make([]model.FAQEntry, 0, len(specs))
	for _, sp := range specs {
		out = append(out, model.FAQEntry{
			Question:   sp.Q,
			Answer:     sp.A,
			Keywords:   pq.StringArray(sp.Keywords),
			Category:   "general",
			Intent:     "general",
			Confidence: sp.Conf,
			HitCount:   int64(randInt(10, 500)),
			Enabled:    &trueVal,
		})
	}
	return out
}

func (s *faqSopSeeder) buildSOPTemplates() []model.SOPTemplate {
	trueVal := true
	tpls := make([]model.SOPTemplate, 0, 30)
	tpls = append(tpls, s.sopInitialStage()...)
	tpls = append(tpls, s.sopMiddleStage()...)
	tpls = append(tpls, s.sopLateStage()...)
	tpls = append(tpls, s.sopObjectionStage()...)
	tpls = append(tpls, s.sopClosingStage()...)
	tpls = append(tpls, s.sopUrgencyStage()...)
	tpls = append(tpls, s.sopSupportStage()...)
	_ = trueVal
	for i := range tpls {
		tpls[i].HitCount = int64(randInt(20, 800))
	}
	return tpls
}

// sopInitialStage 开场阶段 (6 条)
func (s *faqSopSeeder) sopInitialStage() []model.SOPTemplate {
	trueVal := true
	specs := []struct {
		Name     string
		Intent   string
		Stage    string
		Template string
		Vars     string
		Priority int
		Conf     float64
	}{
		{
			Name:     "HiveMTK 通用欢迎语 " + seedTag,
			Intent:   "general",
			Stage:    "initial",
			Template: "您好，我是 HiveMTK 官方客服助手，可为您解答关于项目开源信息、部署、运维、架构、资产市场、AI 智能体等问题。请问有什么可以帮您？",
			Vars:     `{}`,
			Priority: 100,
			Conf:     0.95,
		},
		{
			Name:     "部署咨询开场 " + seedTag,
			Intent:   "deploy",
			Stage:    "initial",
			Template: "您好，关于部署我可以帮您梳理：硬件要求、make install 三步、.env 四个密钥、模型档位（dev/prod）、本地推理栈启动，以及 FRP 公网穿透。您现在是准备首次安装，还是已经部署好想排查问题？",
			Vars:     `{}`,
			Priority: 92,
			Conf:     0.93,
		},
		{
			Name:     "功能咨询开场 " + seedTag,
			Intent:   "features",
			Stage:    "initial",
			Template: "您好，HiveMTK 的核心能力包括七端渠道接入、ReAct 自主 AI 智能体（41 个内置工具）、三级 RAG 检索、零出域数据安全，以及资产市场 ISV 生态。您最想了解哪一块？",
			Vars:     `{}`,
			Priority: 90,
			Conf:     0.92,
		},
		{
			Name:     "资产市场开场 " + seedTag,
			Intent:   "asset",
			Stage:    "initial",
			Template: "您好，资产市场是三端闭环：平台端分发审核、开发者端 Playground 调教、商户端拉取运行织入 RAG。您是想了解怎么使用现成资产，还是怎么作为 ISV 贡献资产？",
			Vars:     `{}`,
			Priority: 88,
			Conf:     0.9,
		},
		{
			Name:     "社区与贡献开场 " + seedTag,
			Intent:   "community",
			Stage:    "initial",
			Template: "您好，社区支持渠道：Gitee Issues（12h 首响）、微信交流群（管理员 wxid: xiao142000，7x24 答疑）、商务合作邮箱 jideilvluoqun@gmail.com。您是想报 Bug、提需求，还是想参与贡献？",
			Vars:     `{}`,
			Priority: 86,
			Conf:     0.89,
		},
		{
			Name:     "架构咨询开场 " + seedTag,
			Intent:   "architecture",
			Stage:    "initial",
			Template: "您好，关于架构我可以讲解：整体部署拓扑（用户端 + 平台端职责划分）、Go 分层架构规范、RAG 问答数据流、三级检索，以及 100% 私域零出域设计。您关心哪方面？",
			Vars:     `{}`,
			Priority: 84,
			Conf:     0.88,
		},
	}
	out := make([]model.SOPTemplate, 0, len(specs))
	for _, sp := range specs {
		out = append(out, model.SOPTemplate{
			Name:       sp.Name,
			Intent:     sp.Intent,
			Stage:      sp.Stage,
			Template:   sp.Template,
			Vars:       sp.Vars,
			Priority:   sp.Priority,
			Confidence: sp.Conf,
			Enabled:    &trueVal,
		})
	}
	return out
}

// sopMiddleStage 需求澄清阶段 (5 条)
func (s *faqSopSeeder) sopMiddleStage() []model.SOPTemplate {
	trueVal := true
	specs := []struct {
		Name     string
		Intent   string
		Stage    string
		Template string
		Vars     string
		Priority int
		Conf     float64
	}{
		{
			Name:     "澄清部署环境 " + seedTag,
			Intent:   "deploy",
			Stage:    "middle",
			Template: "为了给您准确的部署建议，方便说下运行环境吗？例如：纯本地/内网服务器、云主机（哪一家）、是否需要公网访问（用 FRP 还是反向代理）、是否为生产环境（决定 dev/prod 模型档位）。",
			Vars:     `{}`,
			Priority: 82,
			Conf:     0.9,
		},
		{
			Name:     "澄清使用场景 " + seedTag,
			Intent:   "features",
			Stage:    "middle",
			Template: "您主要打算用 HiveMTK 做哪类业务？例如：品牌私域客服、社媒营销自动化、ISV 资产开发，还是内部工具集成？不同场景我推荐不同的智能体与渠道组合。",
			Vars:     `{}`,
			Priority: 80,
			Conf:     0.88,
		},
		{
			Name:     "澄清技术熟悉度 " + seedTag,
			Intent:   "architecture",
			Stage:    "middle",
			Template: "您对 Go / Docker / PostgreSQL / llama.cpp 这套技术栈熟悉吗？如果偏运维侧，我重点讲 make 命令与排障；如果偏研发侧，我重点讲分层架构与智能体编排。",
			Vars:     `{}`,
			Priority: 78,
			Conf:     0.85,
		},
		{
			Name:     "澄清渠道需求 " + seedTag,
			Intent:   "features",
			Stage:    "middle",
			Template: "您目前最想打通哪个渠道？抖音/快手/小红书/闲鱼/TikTok/微信企业微信/短信/邮件？七端都可接，但各渠道凭证与合规要求不同，我可以针对性说明接入步骤。",
			Vars:     `{}`,
			Priority: 76,
			Conf:     0.86,
		},
		{
			Name:     "澄清硬件条件 " + seedTag,
			Intent:   "deploy",
			Stage:    "middle",
			Template: "您这边可用的服务器配置大概多少？CPU 核数、内存、是否有 GPU。最低 2 核/4GB 可跑，但生产推荐 8 核+/16GB+；有 NVIDIA 16GB+ 可上 prod 档 14B 模型。",
			Vars:     `{}`,
			Priority: 75,
			Conf:     0.84,
		},
	}
	out := make([]model.SOPTemplate, 0, len(specs))
	for _, sp := range specs {
		out = append(out, model.SOPTemplate{
			Name:       sp.Name,
			Intent:     sp.Intent,
			Stage:      sp.Stage,
			Template:   sp.Template,
			Vars:       sp.Vars,
			Priority:   sp.Priority,
			Confidence: sp.Conf,
			Enabled:    &trueVal,
		})
	}
	return out
}

// sopLateStage 方案介绍阶段 (6 条)
func (s *faqSopSeeder) sopLateStage() []model.SOPTemplate {
	trueVal := true
	specs := []struct {
		Name     string
		Intent   string
		Stage    string
		Template string
		Vars     string
		Priority int
		Conf     float64
	}{
		{
			Name:     "功能概览介绍 " + seedTag,
			Intent:   "features",
			Stage:    "late",
			Template: "HiveMTK 共 62 个核心模块：认证与用户管理、多平台卡片、自动回复+RAG、邮件/短信营销、社群管理、短链活码、线索与客户、营销自动化、内容创作、系统管理、第三方对接、统一消息。统一 CDP 客户视图 + 统一消息中心一处看完会话/工单/留言。",
			Vars:     `{}`,
			Priority: 90,
			Conf:     0.9,
		},
		{
			Name:     "ReAct 智能体介绍 " + seedTag,
			Intent:   "features",
			Stage:    "late",
			Template: "ReAct 循环：感知→规划→调工具→反思（最多 5 轮），智能体自主决策。内置 41 个工具（查库存、查物流、查客户画像、改地址、加白名单等），配合三级 RAG 与多智能体协作（被动应答 + 主动触达）。一个 AIAgent = 人设 + 知识库 + LLM + SOP + 话术库 + 决策策略的完整配置。",
			Vars:     `{}`,
			Priority: 88,
			Conf:     0.9,
		},
		{
			Name:     "七端渠道介绍 " + seedTag,
			Intent:   "features",
			Stage:    "late",
			Template: "七端打通：抖音（触达/智能卡片/自动回复/RAG 客服，含直播私信）、快手、小红书（私信+评论）、闲鱼、TikTok（海外）、微信/企业微信（社群+朋友圈）、短信、邮件。每个渠道支持触达、智能卡片、自动回复与 RAG 客服。",
			Vars:     `{}`,
			Priority: 86,
			Conf:     0.88,
		},
		{
			Name:     "零出域数据安全介绍 " + seedTag,
			Intent:   "architecture",
			Stage:    "late",
			Template: "100% 私域零出域：本地推理栈（llama.cpp + TEI）跑在客户内网；对话/知识库/向量化/检索增强全程在内网；FRP 穿透时数据经隧道回本地，云端不落一条对话。满足等保、数据出境管控、私有化基线。可选云端 LLM（改 LLM_BASE_URL），但 Embedding/Rerank 强制本地。",
			Vars:     `{}`,
			Priority: 87,
			Conf:     0.9,
		},
		{
			Name:     "资产市场介绍 " + seedTag,
			Intent:   "asset",
			Stage:    "late",
			Template: "资产市场提供 5 类资产包：agent_persona（人设）、sales_script（话术）、ab_test_plan（AB 测试）、marketing_workflow（工作流）、industry_sop（行业 SOP），均为 JSONB。商户从市场浏览/购买→SyncPull 拉取到本地→客服系统加载织入 RAG。开发者可在 Playground 调教后提交上架。",
			Vars:     `{}`,
			Priority: 84,
			Conf:     0.87,
		},
		{
			Name:     "三级 RAG 介绍 " + seedTag,
			Intent:   "architecture",
			Stage:    "late",
			Template: "三级 RAG：①向量召回（pgvector + bge-m3，1024 维）②bge-reranker-v2-m3 精排 ③LLM 改写（HyDE / Query Rewriter）。置信度阈值默认 0.7，低于则降级通用 LLM。多轮对话保留 3-5 轮上下文，命中转人工关键词立即转人工。",
			Vars:     `{}`,
			Priority: 83,
			Conf:     0.87,
		},
	}
	out := make([]model.SOPTemplate, 0, len(specs))
	for _, sp := range specs {
		out = append(out, model.SOPTemplate{
			Name:       sp.Name,
			Intent:     sp.Intent,
			Stage:      sp.Stage,
			Template:   sp.Template,
			Vars:       sp.Vars,
			Priority:   sp.Priority,
			Confidence: sp.Conf,
			Enabled:    &trueVal,
		})
	}
	return out
}

// sopObjectionStage 疑虑处理阶段 (6 条)
func (s *faqSopSeeder) sopObjectionStage() []model.SOPTemplate {
	trueVal := true
	specs := []struct {
		Name     string
		Intent   string
		Stage    string
		Template string
		Vars     string
		Priority int
		Conf     float64
	}{
		{
			Name:     "部署复杂度疑虑 " + seedTag,
			Intent:   "deploy",
			Stage:    "objection",
			Template: "部署确实有一定门槛，但已经尽量简化：make install 一键生成 .env 与 docker-compose，改 4 个密钥后 make up 即可。官方文档含安装、运维、FRP 穿透指南；微信交流群 7x24 答疑。建议先用 dev 轻量档在本地熟悉流程。",
			Vars:     `{}`,
			Priority: 90,
			Conf:     0.9,
		},
		{
			Name:     "硬件成本疑虑 " + seedTag,
			Intent:   "deploy",
			Stage:    "objection",
			Template: "可以按需起步：dev 轻量档（Qwen2.5-1.5B + Qwen3-Embedding-0.6B）8GB 内存的普通机器即可跑；确认效果后再升级 prod 档（16GB+，可选 GPU）。不必一开始投入高配。",
			Vars:     `{}`,
			Priority: 88,
			Conf:     0.88,
		},
		{
			Name:     "数据安全疑虑 " + seedTag,
			Intent:   "architecture",
			Stage:    "objection",
			Template: "这正是 HiveMTK 的设计重点：100% 私域零出域，所有对话与知识库都在您自己的内网，平台端不接触任何业务数据。FRP 穿透也只是隧道中转，云端不落对话。满足等保与数据出境管控要求。",
			Vars:     `{}`,
			Priority: 89,
			Conf:     0.91,
		},
		{
			Name:     "开源合规疑虑 " + seedTag,
			Intent:   "overview",
			Stage:    "objection",
			Template: "AGPL-3.0 的要点很明确：您可以自由使用与修改；只有「修改后通过网络对外提供服务」时才须开源修改后的完整源码，内部私有部署无需公开。合规上比很多商业 SCRM 更透明。使用主动触达请遵守各渠道平台规范与所在地法律。",
			Vars:     `{}`,
			Priority: 87,
			Conf:     0.9,
		},
		{
			Name:     "与 Dify 对比疑虑 " + seedTag,
			Intent:   "overview",
			Stage:    "objection",
			Template: "两者定位不同：Dify/Coze 偏通用工作流编排与云端 SaaS；HiveMTK 偏私域多渠道营销自动化，强调七端原生接入、零出域与自主智能体编排。如果您的核心诉求是私域社媒 + 数据不出域，HiveMTK 更贴合。",
			Vars:     `{}`,
			Priority: 85,
			Conf:     0.88,
		},
		{
			Name:     "模型效果疑虑 " + seedTag,
			Intent:   "deploy",
			Stage:    "objection",
			Template: "本地 dev 档 1.5B 模型适合轻量客服；若追求更强效果，prod 档可上 14B 模型，或把 LLM_BASE_URL 指向 DeepSeek/OpenAI 等云端大模型（Embedding/Rerank 仍本地）。三级 RAG 也会显著提升召回准确率。",
			Vars:     `{}`,
			Priority: 84,
			Conf:     0.87,
		},
	}
	out := make([]model.SOPTemplate, 0, len(specs))
	for _, sp := range specs {
		out = append(out, model.SOPTemplate{
			Name:       sp.Name,
			Intent:     sp.Intent,
			Stage:      sp.Stage,
			Template:   sp.Template,
			Vars:       sp.Vars,
			Priority:   sp.Priority,
			Confidence: sp.Conf,
			Enabled:    &trueVal,
		})
	}
	return out
}

// sopClosingStage 收尾引导阶段 (6 条)
func (s *faqSopSeeder) sopClosingStage() []model.SOPTemplate {
	trueVal := true
	specs := []struct {
		Name     string
		Intent   string
		Stage    string
		Template string
		Vars     string
		Priority int
		Conf     float64
	}{
		{
			Name:     "引导仓库与文档 " + seedTag,
			Intent:   "general",
			Stage:    "closing",
			Template: "您可以先从仓库入手：Gitee 主仓库 https://gitee.com/xhpmayun/hivemtk ，GitHub 镜像 https://github.com/xiaofang142/hivemtk ；README 含快速开始，docs/ 含架构与部署指南。建议先 make install 跑通最小闭环。",
			Vars:     `{}`,
			Priority: 95,
			Conf:     0.9,
		},
		{
			Name:     "引导微信交流群 " + seedTag,
			Intent:   "community",
			Stage:    "closing",
			Template: "部署或二次开发遇到问题，欢迎加微信交流群，管理员 wxid: xiao142000，7x24 答疑（产品/技术/运营）。群里也会同步版本与最佳实践。",
			Vars:     `{}`,
			Priority: 93,
			Conf:     0.89,
		},
		{
			Name:     "引导初始化步骤 " + seedTag,
			Intent:   "deploy",
			Stage:    "closing",
			Template: "下一步建议：1) make install 生成配置 2) vim .env 设置 4 个密钥（openssl rand -hex 24 生成）3) make up 启动 4) 浏览器访问 :8204/setup 完成初始化。完成后健康检查 curl http://localhost:8204/health 应返回 200。",
			Vars:     `{}`,
			Priority: 92,
			Conf:     0.9,
		},
		{
			Name:     "引导资产市场 " + seedTag,
			Intent:   "asset",
			Stage:    "closing",
			Template: "部署好之后，您可以从资产市场获取现成的智能体人设、话术库、行业 SOP 等资产包，SyncPull 拉取到本地即可织入 RAG，快速具备业务能力，不必从零调教。",
			Vars:     `{}`,
			Priority: 88,
			Conf:     0.87,
		},
		{
			Name:     "引导转人工/商务 " + seedTag,
			Intent:   "community",
			Stage:    "closing",
			Template: "如果以上仍无法解决您的问题，可转人工支持，或发邮件 jideilvluoqun@gmail.com 谈企业级技术支持与定制集成。我们会按您的场景给出具体方案。",
			Vars:     `{}`,
			Priority: 86,
			Conf:     0.86,
		},
		{
			Name:     "收尾致谢 " + seedTag,
			Intent:   "general",
			Stage:    "closing",
			Template: "感谢您关注 HiveMTK！这是个持续迭代的开源项目，欢迎在 Gitee Issues 提反馈、在微信群交流、按 CONTRIBUTING.md 贡献代码。祝部署顺利。",
			Vars:     `{}`,
			Priority: 84,
			Conf:     0.88,
		},
	}
	out := make([]model.SOPTemplate, 0, len(specs))
	for _, sp := range specs {
		out = append(out, model.SOPTemplate{
			Name:       sp.Name,
			Intent:     sp.Intent,
			Stage:      sp.Stage,
			Template:   sp.Template,
			Vars:       sp.Vars,
			Priority:   sp.Priority,
			Confidence: sp.Conf,
			Enabled:    &trueVal,
		})
	}
	return out
}

// ================================================================
// faqUrgency — 逼单/催进展 FAQ (8 条) — Layer1 命中直接引流微信
// ================================================================
func (s *faqSopSeeder) faqUrgency() []model.FAQEntry {
	trueVal := true
	specs := []struct {
		Q, A     string
		Keywords []string
		Conf     float64
	}{
		{
			Q:        "什么时候好 __URGENCY_SUPPORT_SEED_20260908__",
			A:        `加作者微信 xiao142000 直接沟通进展，备注逼单会优先处理。作者在线时间 9:00-23:00，通常 1 小时内回复。`,
			Keywords: []string{"什么时候好", "多久好", "什么时候完成", "预计什么时候"},
			Conf:     0.94,
		},
		{
			Q:        "帮我催一下 __URGENCY_SUPPORT_SEED_20260908__",
			A:        `好的！请加作者微信 xiao142000，备注逼单-您的需求简述，作者会优先看到。也可以拉进专属群同步进度。`,
			Keywords: []string{"催", "快一点", "加急", "快点", "进展"},
			Conf:     0.93,
		},
		{
			Q:        "能快一点吗 __URGENCY_SUPPORT_SEED_20260908__",
			A:        `理解您的急迫。加微信 xiao142000 沟通后，可以讨论走加急通道、先出 MVP 版本或优先排期。不要犹豫，直接加微信！`,
			Keywords: []string{"快一点", "能快", "有没有更快", "太慢"},
			Conf:     0.92,
		},
		{
			Q:        "我比较急 __URGENCY_SUPPORT_SEED_20260908__",
			A:        `急单请直接微信 xiao142000，备注逼单-紧急，作者会置顶处理。微信群也有 SOP 维护者在线，可以同步进度。`,
			Keywords: []string{"急", "紧急", "很急", "等不及"},
			Conf:     0.91,
		},
		{
			Q:        "谁负责这个项目 __URGENCY_SUPPORT_SEED_20260908__",
			A:        `作者兼维护者 wxid: xiao142000。直接加微信沟通最快，微信群也有产品/技术/运营 7x24 答疑。`,
			Keywords: []string{"谁负责", "负责人", "作者", "维护者"},
			Conf:     0.95,
		},
		{
			Q:        "能联系作者吗 __URGENCY_SUPPORT_SEED_20260908__",
			A:        `当然！作者微信 xiao142000。加好友时备注来意（部署/开发/逼单/合作），秒通过。`,
			Keywords: []string{"联系作者", "联系开发", "找作者", "找开发"},
			Conf:     0.96,
		},
		{
			Q:        "你们几点在线 __URGENCY_SUPPORT_SEED_20260908__",
			A:        `作者在线时间 9:00-23:00（北京时区）。紧急情况加微信 xiao142000 随时留言，看到就回。`,
			Keywords: []string{"几点在线", "营业时间", "什么时候有人", "在线时间"},
			Conf:     0.88,
		},
		{
			Q:        "有微信吗 __URGENCY_SUPPORT_SEED_20260908__",
			A:        `有！作者 wxid: xiao142000，加好友时备注来意更快通过。也可以说拉群进 SOP 专属微信群。`,
			Keywords: []string{"微信", "有微信吗", "联系方式", "怎么联系"},
			Conf:     0.97,
		},
	}
	out := make([]model.FAQEntry, 0, len(specs))
	for _, sp := range specs {
		out = append(out, model.FAQEntry{
			Question:   sp.Q,
			Answer:     sp.A,
			Keywords:   pq.StringArray(sp.Keywords),
			Category:   "urgency",
			Intent:     "urgency",
			Confidence: sp.Conf,
			HitCount:   int64(randInt(200, 2000)),
			Enabled:    &trueVal,
		})
	}
	return out
}

// ================================================================
// faqSupport — SOP 有问题/技术卡住 FAQ (7 条) — Layer1 命中引导拉群
// ================================================================
func (s *faqSopSeeder) faqSupport() []model.FAQEntry {
	trueVal := true
	specs := []struct {
		Q, A     string
		Keywords []string
		Conf     float64
	}{
		{
			Q:        "SOP 有问题 __URGENCY_SUPPORT_SEED_20260908__",
			A:        `SOP 遇到问题别慌！加微信 xiao142000，说明 SOP问题后可以拉您进 SOP 专属微信群，群里有维护者和同行业商户一起排查。`,
			Keywords: []string{"SOP有问题", "SOP不对", "SOP bug", "SOP 出错"},
			Conf:     0.94,
		},
		{
			Q:        "SOP 不触发 __URGENCY_SUPPORT_SEED_20260908__",
			A:        `SOP 不触发通常是意图识别没命中或 confidence 不够。加微信 xiao142000 进群，贴出您的意图配置和触发条件，维护者会帮您定位。`,
			Keywords: []string{"SOP不触发", "不触发SOP", "触发不了", "触发失败"},
			Conf:     0.93,
		},
		{
			Q:        "跑不起来怎么办 __URGENCY_SUPPORT_SEED_20260908__",
			A:        `跑不起来原因很多（端口冲突/密钥没改/Docker 没启动/模型缺失）。最快方式：加微信 xiao142000，拉进交流群，贴出报错日志，大家一起定位。`,
			Keywords: []string{"跑不起来", "启动失败", "报错", "错误", "panic"},
			Conf:     0.92,
		},
		{
			Q:        "部署卡住了 __URGENCY_SUPPORT_SEED_20260908__",
			A:        `部署卡住常见原因：端口被占、.env 密钥没生成、Docker daemon 没启动、模型文件没下载。加微信 xiao142000，拉交流群，贴日志秒排查。`,
			Keywords: []string{"卡住", "卡死", "不动了", "一直转圈"},
			Conf:     0.89,
		},
		{
			Q:        "有群吗 __URGENCY_SUPPORT_SEED_20260908__",
			A:        `有三个专属群：①部署/技术交流群 ②SOP 问题排查群 ③二次开发群。加微信 xiao142000 说拉群，按需拉入。`,
			Keywords: []string{"有群吗", "微信群", "交流群", "加群", "拉群"},
			Conf:     0.95,
		},
		{
			Q:        "求助 __URGENCY_SUPPORT_SEED_20260908__",
			A:        `加微信 xiao142000，说明遇到的问题（部署/SOP/开发/架构），拉进对应群聊，维护者 7x12 小时在线。贴日志+截图定位更快。`,
			Keywords: []string{"求助", "救命", "help", "需要帮助", "帮我"},
			Conf:     0.90,
		},
		{
			Q:        "有没有人管 __URGENCY_SUPPORT_SEED_20260908__",
			A:        `有的！作者兼维护者微信 xiao142000，加好友秒通过。也可以拉进交流群，产品/技术/运营都在。`,
			Keywords: []string{"有没有人管", "没人", "不理", "没人回复", "客服呢"},
			Conf:     0.88,
		},
	}
	out := make([]model.FAQEntry, 0, len(specs))
	for _, sp := range specs {
		out = append(out, model.FAQEntry{
			Question:   sp.Q,
			Answer:     sp.A,
			Keywords:   pq.StringArray(sp.Keywords),
			Category:   "support",
			Intent:     "support",
			Confidence: sp.Conf,
			HitCount:   int64(randInt(200, 2000)),
			Enabled:    &trueVal,
		})
	}
	return out
}

// ================================================================
// sopUrgencyStage — 逼单意图 5 阶段 SOP (10 条)
// ================================================================
func (s *faqSopSeeder) sopUrgencyStage() []model.SOPTemplate {
	trueVal := true
	specs := []struct {
		Name     string
		Intent   string
		Stage    string
		Template string
		Vars     string
		Priority int
		Conf     float64
	}{
		{Name: `逼单开场 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "urgency", Stage: "initial", Template: `您好，看来您对项目进展比较关注。请加作者微信 xiao142000 直接沟通，他会给您最及时的进展同步和优先级反馈。备注逼单会优先处理。`, Vars: `{}`, Priority: 99, Conf: 0.96},
		{Name: `逼单澄清需求 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "urgency", Stage: "middle", Template: `理解您的急迫心情。加微信 xiao142000 后直接说清需求和期望时间点，作者会评估排期并给您明确答复。微信群也有同步进度，加微信后可拉入专属群。`, Vars: `{}`, Priority: 97, Conf: 0.95},
		{Name: `逼单方案介绍 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "urgency", Stage: "late", Template: `项目持续迭代中，功能验证需要时间。加微信 xiao142000 后可以讨论：①是否先出 MVP 版本跑通核心链路 ②您的场景是否可以先手动绕过 ③排期预估。微信沟通效率最高。`, Vars: `{}`, Priority: 95, Conf: 0.93},
		{Name: `逼单疑虑-等待太久 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "urgency", Stage: "objection", Template: `非常抱歉让您久等了。加微信 xiao142000 后可以走加急通道：①优先排期 ②指定模块先出 ③MVP 方案讨论。微信群也有 SOP 维护者同步进度。`, Vars: `{}`, Priority: 98, Conf: 0.96},
		{Name: `逼单疑虑-不信任 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "urgency", Stage: "objection", Template: `理解您的顾虑。加微信 xiao142000 后可以：①实时看代码仓库 commit 历史 ②了解下一个版本计划 ③微信群里看同行业用户的实际部署反馈。透明化沟通最快。`, Vars: `{}`, Priority: 94, Conf: 0.92},
		{Name: `逼单收尾-加急通道 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "urgency", Stage: "closing", Template: `马上加微信 xiao142000，备注逼单-您的需求简述。作者在线时间 9:00-23:00，通常 1 小时内回复。不要等，现在就加！`, Vars: `{}`, Priority: 100, Conf: 0.97},
		{Name: `逼单收尾-后续跟进 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "urgency", Stage: "closing", Template: `加完微信后请说拉群，可以进专属进度同步群。群里会实时更新排期和变更，比单独聊天更有保障。`, Vars: `{}`, Priority: 96, Conf: 0.94},
		{Name: `催进展-重复询问 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "urgency", Stage: "middle", Template: `加微信 xiao142000 后进群，群里有进度看板实时更新。不要反复问，直接看群公告。`, Vars: `{}`, Priority: 90, Conf: 0.90},
		{Name: `紧急排期 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "urgency", Stage: "initial", Template: `紧急需求请直接微信 xiao142000，备注紧急-排期，作者会立即评估能否插队。群里也可以 @维护者。`, Vars: `{}`, Priority: 98, Conf: 0.95},
		{Name: `加不到微信怎么办 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "urgency", Stage: "objection", Template: `如果加微信没通过：①检查微信号 xiao142000 有没有拼错 ②等 10 分钟作者会自动通过 ③发邮件 jideilvluoqun@gmail.com 邮件标题带逼单。`, Vars: `{}`, Priority: 88, Conf: 0.91},
	}
	out := make([]model.SOPTemplate, 0, len(specs))
	for _, sp := range specs {
		out = append(out, model.SOPTemplate{
			Name:       sp.Name,
			Intent:     sp.Intent,
			Stage:      sp.Stage,
			Template:   sp.Template,
			Vars:       sp.Vars,
			Priority:   sp.Priority,
			Confidence: sp.Conf,
			Enabled:    &trueVal,
		})
	}
	return out
}

// ================================================================
// sopSupportStage — SOP 有问题 5 阶段 SOP (10 条)
// ================================================================
func (s *faqSopSeeder) sopSupportStage() []model.SOPTemplate {
	trueVal := true
	specs := []struct {
		Name     string
		Intent   string
		Stage    string
		Template string
		Vars     string
		Priority int
		Conf     float64
	}{
		{Name: `SOP问题开场 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "support", Stage: "initial", Template: `您好，SOP 遇到问题了别担心。请加微信 xiao142000，说明是 SOP 问题后可以拉您进 SOP 专属微信群，群里有维护者和同行业商户一起排查。`, Vars: `{}`, Priority: 99, Conf: 0.96},
		{Name: `SOP问题-澄清类型 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "support", Stage: "middle", Template: `能描述下具体是什么问题吗？比如 SOP 不触发、触发了但执行步骤不对、还是某个节点报错？加微信 xiao142000 后进群，带着报错截图或日志，大家一起定位会更快。`, Vars: `{}`, Priority: 97, Conf: 0.95},
		{Name: `SOP问题-排查思路 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "support", Stage: "late", Template: `SOP 不触发常见原因：①意图识别 confidence 低于阈值 ②entry_policy 拦截重复进入 ③SOP 版本没更新。加微信 xiao142000 进群，维护者会帮您逐一排查，调参建议直接给出。`, Vars: `{}`, Priority: 95, Conf: 0.93},
		{Name: `SOP疑虑-我加过群了 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "support", Stage: "objection", Template: `如果之前加过没解决，可能是群消息刷过去了或没 @ 维护者。重新加微信 xiao142000，说 SOP 问题-二次求助，会直接推给资深维护者。`, Vars: `{}`, Priority: 94, Conf: 0.92},
		{Name: `SOP疑虑-线上排查慢 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "support", Stage: "objection", Template: `SOP 逻辑跟具体业务场景强相关，线上排查确实不如群里实时沟通高效。加微信 xiao142000 后进群，维护者会根据您的场景给出调参建议或临时绕过方案。`, Vars: `{}`, Priority: 96, Conf: 0.94},
		{Name: `SOP收尾-快速通道四步 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "support", Stage: "closing", Template: `快速通道：①微信搜 xiao142000 ②备注 SOP问题-您的行业 ③通过后说拉SOP群 ④进群后 @维护者 + 贴问题截图。群里 7x12 小时有人响应。`, Vars: `{}`, Priority: 100, Conf: 0.97},
		{Name: `SOP收尾-紧急情况 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "support", Stage: "closing", Template: `紧急情况可直接私聊作者微信 xiao142000，发问题截图，通常 1 小时内回复。也可以拉群后 @维护者。`, Vars: `{}`, Priority: 98, Conf: 0.95},
		{Name: `技术卡住开场 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "support", Stage: "initial", Template: `技术问题卡住了？加微信 xiao142000 进交流群，贴出报错日志，大家一起定位比自己摸索快 10 倍。`, Vars: `{}`, Priority: 97, Conf: 0.94},
		{Name: `技术卡住-部署失败 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "support", Stage: "middle", Template: `部署失败常见 5 个原因：端口冲突、.env 密钥没生成、Docker daemon 没启动、模型文件缺失、PostgreSQL 连接不上。加微信 xiao142000，拉群后逐步排查。`, Vars: `{}`, Priority: 92, Conf: 0.91},
		{Name: `技术卡住-二次开发 __URGENCY_SUPPORT_SEED_20260908__`, Intent: "support", Stage: "late", Template: `二次开发问题更适合拉进开发群讨论，里面有 Go 分层架构专家和 GORM 坑位经验。加微信 xiao142000，说拉开发群即可。`, Vars: `{}`, Priority: 90, Conf: 0.90},
	}
	out := make([]model.SOPTemplate, 0, len(specs))
	for _, sp := range specs {
		out = append(out, model.SOPTemplate{
			Name:       sp.Name,
			Intent:     sp.Intent,
			Stage:      sp.Stage,
			Template:   sp.Template,
			Vars:       sp.Vars,
			Priority:   sp.Priority,
			Confidence: sp.Conf,
			Enabled:    &trueVal,
		})
	}
	return out
}
