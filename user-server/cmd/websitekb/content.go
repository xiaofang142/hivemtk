package main

// 官网客服知识库内容（2026-02 口径改写版）
//
// 来源：本地 user_db 的 rag_products.id='hivemtk-platform-cs'（5 篇 / 75 chunk），
// 按官网访客视角改写：去占位符（{port} {limit} {seconds} …）、去过时口径
// （Go 1.22→1.25+、embedding 8191→平台服务、2核4G→4核8G）、去内部实现细节
// （FeatureFlag 名 / Phase 阶段 / Handler 层命名 / 占位域名 hiveapi.xxx.com）。

type kbDoc struct {
	Title    string
	Category string
	Chunks   []string
}

var kbDocs = []kbDoc{
	{
		Title:    "HiveMTK 技术架构与分层",
		Category: "技术",
		Chunks: []string{
			"HiveMTK 的整体分层：接入层（多端渠道 + 桥接）→ 编排层（ReAct 智能体 + 对话编排）→ 能力层（RAG 知识库、内容生成、内置工具、资产市场）→ 数据层（PostgreSQL + pgvector 向量库 + Redis 缓存）。每一层可独立替换与水平扩展。",
			"后端技术栈：Go 1.25+ / Gin / GORM / PostgreSQL 15 / Redis 7；向量检索用 pgvector（HNSW 索引）；前端 Vue 3 + Vite + Element Plus；本地推理栈 llama.cpp（LLM）+ TEI（Embedding / Rerank）。支持 x86_64 与 Apple Silicon 纯 Docker 部署。",
			"智能体引擎的 ReAct 循环：Thought（思考）→ Action（调用工具）→ Observation（观察结果）→ 继续思考，直到给出答案。工具调用链带五重防护：权限校验、失败重试、超时控制、速率限制、审计留痕。",
			"内置工具 45 个，分五类：customer.*（客户）、knowledge.*（知识库）、reach.*（触达 13 类通道）、business.*（订单/售后）、pm.*（私信与卡片）。工具通过统一注册表调度，智能体自主编排调用顺序。",
			"三级 RAG 检索：Level1 关键词/BM25 粗排 → Level2 向量语义召回（pgvector）→ Level3 bge-reranker 交叉编码器重排，命中片段作为上下文交给 LLM 生成自然语言回答。",
			"记忆系统分四层：L1 短期（当次会话上下文）、L2 长期（PostgreSQL + pgvector 语义检索）、L3 SOP（对话流程状态）、L4 业务（订单/客户等业务事实）。",
			"智能体决策流水线：感知（情绪 + 意图）→ 对齐（拟人度 6 维）→ 门禁（危机场景转人工）→ 规划（选工具与 SOP）→ 行动（执行）→ 复盘（写回记忆）。",
			"降级链保证永不掉线：LLM 不可用 → 退回 RAG 单独作答 → 退回 SOP 模板 → 退回 FAQ 标准答案 → 退回固定话术。任一层故障都不会让访客看到无响应。",
			"多租户隔离：所有业务表带 tenant_id，跨租户查询在数据访问层统一拦截；渠道、智能体、知识库、资产包都按租户隔离，一个租户看不到另一个租户的数据。",
			"多渠道统一消息格式：{channel, user_id, text, timestamp, metadata} 进，各渠道适配器再转成各自协议。同一套智能体可同时绑多个渠道，消息分渠道入库。",
			"可观测性：每条消息与工具调用携带唯一 TraceID，从入站到回复全链路可追溯；结构化 JSON 日志可直接接 Loki / ELK。健康检查端点 /healthz 返回服务、数据库、Redis、Embedding、推理的逐项状态。",
			"为什么选开源 AGPL-3.0：代码与全部功能模块在 GitHub / Gitee 公开，可自托管、可二次开发、不收授权费；代价是对外提供基于本项目的网络服务时需公开你的修改。",
		},
	},
	{
		Title:    "HiveMTK 部署与运维手册",
		Category: "部署",
		Chunks: []string{
			"私有化部署最低配置：4 核 CPU / 8 GB 内存 / 60 GB SSD；推荐 8 核 / 16 GB / 200 GB SSD。GPU 不是必需，llama.cpp 纯 CPU 也能跑，只是生成速度慢一些。磁盘要给模型目录单独预留 10 GB 以上。",
			"三种部署方式：① Docker Compose 一条命令拉起后端、前端、PostgreSQL、Redis 与本地推理栈，约 1 分钟启动，最适合快速试用；② 源码部署，适合二次开发；③ Helm Chart 部署到 Kubernetes 集群，适合多实例生产。",
			"快速开始三步：① clone 仓库后 cp .env.example .env，填数据库、Redis 与各 API Key；② docker compose up -d 拉起全部服务；③ 打开控制台，进入「AI 智能体」配人设与知识库，再到「聊天渠道」把智能体绑到接待渠道。",
			"模型接入两种方式：① 本地推理（llama.cpp + TEI），把 GGUF 模型放到 /opt/hivemtk/models/ 下，在 .env 里配 LOCAL_LLM_PATH，数据完全不出内网；② 云端大模型（DeepSeek / 通义千问 / GPT / 智谱 / Kimi），填自己的 API Key 即可，私有化部署下模型调用不额外计费。",
			"Embedding 与 Rerank 服务：TEI 承载 bge-m3 向量模型与 bge-reranker 重排模型，提供 OpenAI 兼容接口；也可以直接填云端 embedding 服务的地址与密钥，配置在管理后台「Embedding 配置」里，改完即时生效。",
			"数据库迁移：migrations/ 目录下的 SQL 脚本按序号执行，启动时也会自动检查未应用的迁移并提示。新建库第一次启动会自动建表，无需手工干预。",
			"端口规划：user-server 对外 8204（API 与 WebSocket）、PostgreSQL 8202、Redis 8203、平台端 8205。内部端口（数据库、缓存、推理）只监听内网，不要对公网开放；对外只开 8204，由 nginx / Caddy 反代并自动签发 HTTPS 证书。",
			"安全基线：API Key 不落配置文件，统一放 .env 或密钥管理；Webhook 必须走 HTTPS 且校验签名；对外只暴露 API 端口，数据库与推理端口仅内网可达；管理员账号启用强密码并定期轮换密钥。",
			"备份与恢复：定期 pg_dump 导出核心表（会话、客户、知识库、资产包）与模型配置目录，备份文件要异地留存；恢复时先建库再执行 migrations/ 下的 SQL，然后还原数据目录。",
			"升级流程：git pull 后 docker compose up -d --build。注意数据库兼容性：升级前先备份，若新版本带迁移，启动时会自动执行；建议先在测试环境验证再上生产。",
		},
	},
	{
		Title:    "HiveMTK 渠道接入指南",
		Category: "渠道",
		Chunks: []string{
			"支持 13 个渠道：Telegram、QQ、企业微信、飞书、WhatsApp、钉钉、抖音、小红书、快手、闲鱼、TikTok，以及网页客服（web / web_embed，就是官网右下角这个浮标）。所有渠道消息汇入同一个统一收件箱，由绑定的智能体自动接待与分流。",
			"网页客服接入有两种方式：① iframe 嵌入，适合已有网站；② JS SDK 动态加载（就是官网用的这种），一行 script 标签即可拉起浮标，配置 appKey 与 API 地址，能设浮标位置、颜色、标题与欢迎语。",
			"Telegram 接入：在 BotFather 创建 Bot 拿 Token，配置 Webhook（公网 HTTPS 地址）或用长轮询模式（适合没有公网 IP 的场景，服务器每几秒拉一次更新）；群内需要 @机器人 才回复，这是 Telegram 隐私模式的默认行为，可在 BotFather 用 /setprivacy 关掉。",
			"Telegram Bot 还支持主动 DM（突破 Telegram 不能主动私信的限制）、群线索挖掘、群消息分析与入群管控。Bot API 建议用 7.0 以上版本。",
			"企业微信接入需要公众号 / 小程序 / 企业号资质，做完 OAuth 授权后绑定；企微支持小程序跳转等特定能力。",
			"WhatsApp 通过 Twilio / MessageBird 等第三方 API 接入，需要商业账号；国内个人号请走企业微信。",
			"渠道特定能力：Telegram 支持按钮 / 卡片 / 文件；网页客服支持图片与语音；企业微信支持小程序跳转。智能体回复时按渠道能力降级，不会给渠道发它不支持的消息类型。",
			"频率限制：各渠道都有发送频率上限（Telegram 约 30 条/秒量级、企微与 WhatsApp 更严），智能体会自动做速率控制与失败重试，不会因为超限丢消息。",
			"Webhook 安全：必须 HTTPS；Telegram 用 X-Telegram-Bot-Api-Secret-Token 校验请求来源；Token 定期轮换。",
			"离线消息：渠道适配器会把发送失败或对端未就绪的消息缓存下来，服务重启后补发，不丢会话。",
		},
	},
	{
		Title:    "HiveMTK FAQ、SOP 与 RAG 使用指南",
		Category: "功能",
		Chunks: []string{
			"FAQ 怎么加：管理后台 → 知识库 → FAQ 管理 → 新增，填 question（标准问法）、answer（标准答案）、keywords（命中关键词，逗号分隔）、category（分类）、intent（意图）。keywords 写用户真会用的词，命中后直接返回标准答案，不消耗 Token。",
			"FAQ 匹配逻辑：用户消息先做关键词匹配（关键词原样出现在问句里即精确命中；语序变了但核心词在则算词元命中），再按问法与 FAQ 的整体相似度做同分决胜。精确命中优先于词元命中 —— 词元命中只在没有任何精确命中时兜底，避免「能」「怎么」这类字面巧合造成答非所问。",
			"FAQ 权重：confidence 是这条答案的可信度（0~1），命中多条时按可信度综合排序；hit_count 记录被命中过的次数，长期没被命中的会自然沉底。关键词不要写「支持」「怎么」这类泛化词，写区分度高的主题词（如「多少钱」「私有化」「Telegram」）。",
			"SOP 模板：字段是 name（名称）、intent（意图）、stage（阶段）、template（话术模板）、variables（变量）。SOP 按对话阶段匹配，阶段划分是 initial（开场）→ middle（引导）→ objection（异议处理）→ closing（逼单）→ late（售后跟进）。",
			"SOP 变量：模板里用 {product}、{feature} 这样的占位符，发送前用实际值填充，例如「推荐您{product}，它{feature}」。变量取不到值时有兜底话术，不会发出带花括号的内容。",
			"RAG 产品（知识库）：每个 RAG 产品是独立的知识库，有自己的文档、切片与向量；一个智能体可以绑定多个 RAG 产品，按绑定关系召回，避免不同业务的知识互相污染。",
			"RAG 文档导入：支持 PDF / Markdown / TXT 等格式，导入后自动切片（按语义切块并保留重叠）→ 向量化 → 入库。文档更新后需要重新向量化才会生效（管理后台点「重建索引」，或用定时任务批量处理）。",
			"RAG 检索：用户问题先向量化 → 向量相似度搜索 top_k → rerank 重排 → 返回片段。混合检索同时走向量（语义相似）与关键词（BM25）两路，结果合并去重后一起给 LLM。",
			"相似度阈值：低于阈值的结果不召回，避免召回不相关的知识导致错答。阈值调高更保守（宁可让 LLM 自由回答），调低更激进（更容易命中但可能答偏）。",
			"缓存优化：相同问题在缓存有效期内直接返回缓存结果，不重复调 embedding 与 LLM，既省钱又快。FAQ 命中是零 Token 的 —— 命中标准答案时完全不需要调用大模型。",
		},
	},
}

// pricingCorrection 线上「HiveMTK 价格与计费」文档的旧口径修正。
//
// 旧文写的是"基础订阅 14900 积分/月、LLM 四档 990/9900/19900/49900 积分"，
// 与线上真实定价（platform_db 实测 2026-10-22）不符：
//
//	module_catalog：7 个基础模块 0 分（免费常开）、17 个增值模块中 16 个 0 分、
//	                仅 browser-automation 100 分（¥1 买断）
//	llm_plans：体验包 ¥1 / 标准包 ¥3 / 专业包 ¥6 / 旗舰包 ¥9
//	subscription_config：¥9/月，15 天试用
//
// 2026-10-30 用户裁决：功能模块只有浏览器自动化收费，其余全部免费。
// 官网访客问「多少钱」时召回的就是这条 —— 口径错了就是对外报价错。
var pricingCorrection = "HiveMTK 的收费口径：① 基础模块免费常开（系统基座、客户中心、会话工作台、知识库、资产与话术、自有模型接入、平台对接 共 7 个）；② 增值模块共 17 个，其中 16 个 0 元开通（AI 智能体、AI 销冠、营销自动化、社媒引流卡片等），仅浏览器自动化 ¥1 一次性买断；③ 平台模型算力（LLM）四档预付费：体验包 ¥1、标准包 ¥3、专业包 ¥6、旗舰包 ¥9；④ 云端版软件订阅 ¥9/月，含 15 天免费体验期；⑤ 私有化部署免费，用自己的大模型 API Key 运行 AI 能力、不消耗平台额度。代码与全部功能模块 AGPL-3.0 开源，不收授权费。"
