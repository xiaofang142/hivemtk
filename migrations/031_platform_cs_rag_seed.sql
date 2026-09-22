-- ============================================================
-- 031_platform_cs_rag_seed.sql
-- 平台客服 RAG 知识库种子数据（网页客服自动回答商户咨询）
-- 背景:
--   * 官网网页客服（iframe 加载 user-server /#/chat/embed/default）
--     需基于本项目知识自动回答商户关于 开源/部署/运维/架构/资产市场 等咨询
--   * 本迁移创建专属 RAG 产品 + 13 篇知识文档 + 多条知识分段
--   * 知识分段 product_id = 'hivemtk-platform-cs'（与 rag_products.id 同为字符串产品 ID，
--     自 2026-08 产品ID统一为字符串后，不再使用 HashStringToInt64 数值哈希桥接）
--   * 检索侧：BM25 关键词召回立即可用；向量召回需 embedding 服务就绪后触发重建索引
-- 设计文档: docs/marketing-features/agent-rag-qa.md
-- 合规约束: 不含定价/版本下载/注册开户等已下线内容；含 GitHub/Gitee 地址
-- ============================================================

-- 0) 幂等清理（重复执行不报错）
DELETE FROM knowledge_chunks WHERE product_id = 'hivemtk-platform-cs';
DELETE FROM knowledge_documents WHERE product_id = 'hivemtk-platform-cs';
DELETE FROM rag_products WHERE id = 'hivemtk-platform-cs';

-- 1) 创建 RAG 产品（平台客服知识库）
INSERT INTO rag_products (
    id, name, description, category, vector_table,
    embedding_model, embedding_dim, llm_model,
    temperature, max_tokens, top_p, frequency_penalty, presence_penalty,
    response_format, system_prompt,
    top_k, chunk_size, chunk_overlap, similarity_threshold,
    is_active, status, doc_count, chunk_count,
    created_at, updated_at
) VALUES (
    'hivemtk-platform-cs',
    'HiveMTK 平台客服知识库',
    '用于官网网页客服自动回答商户关于 HiveMTK 项目的开源信息、部署、运维、架构、资产市场、AI 智能体等咨询。涵盖 13 大主题文档与可检索知识分段。',
    'platform_cs',
    'rag_platform_cs',
    'bge-m3', 1024, 'Qwen2.5-1.5B-Instruct',
    0.3, 1024, 0.9, 0.5, 0.5,
    'text',
    '你是 HiveMTK 官方客服助手，负责解答商户关于本项目的咨询。回答依据下方检索到的知识片段，要求：1) 准确，不编造；2) 简洁，单次回复不超过 200 字；3) 涉及部署/命令时给出具体步骤；4) 不涉及定价、版本下载、注册开户等已下线内容；5) 引导至 GitHub/Gitee 仓库或微信群获取更多帮助。',
    5, 800, 100, 0.55,
    TRUE, 1, 0, 0,
    NOW(), NOW()
);

-- ============================================================
-- 2) 知识文档（8 篇）+ 知识分段
-- 文档 ID 由 SERIAL 自增；分段 document_id 引用文档 ID
-- 为稳定可重入，使用 RETURNING 捕获 ID 写入分段
-- ============================================================

DO $$
DECLARE
    doc_id_bigint BIGINT;
    pid VARCHAR(64) := 'hivemtk-platform-cs';
BEGIN
    -- =========================================================
    -- 文档 1：项目概述与定位
    -- =========================================================
    INSERT INTO knowledge_documents (
        product_id, source_type, title, file_name, filename, file_type,
        chunk_count, embed_status, tags, category, priority,
        metadata, imported_by, status, created_at, updated_at
    ) VALUES (
        pid, 'text', 'HiveMTK 项目概述与定位', 'platform_overview.md', 'platform_overview.md', 'md',
        5, 'indexed', '["项目概述","定位","开源"]', 'overview', 100,
        '{"source":"seed","industry":"platform"}', 'system_seed', 1, NOW(), NOW()
    ) RETURNING id INTO doc_id_bigint;

    INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, char_count, metadata, created_at) VALUES
    (doc_id_bigint, pid, 0, 'HiveMTK 是一个私域部署的 AI 营销操作系统，核心定位是「把七端社媒、AI 智能体、零出域数据安全三件事同时做透」。它不是给大模型套壳，也不是写死的自动化脚本，而是内置一套能感知→规划→调工具→反思的自主 AI 智能体（ReAct 循环 + 41 个内置工具）。开源协议 AGPL-3.0，任何公司或个人可自由使用与私有部署。', 184, '{"doc":"overview","section":"定位"}', NOW()),
    (doc_id_bigint, pid, 1, 'HiveMTK 三大核心卖点：1) 渠道覆盖——七端打通（抖音/快手/小红书/闲鱼/TikTok/微信企业微信/短信/邮件），一个工作台全管；2) AI 范式——ReAct 自主智能体（非写死工作流），三级 RAG 检索（向量召回 + bge-reranker 精排 + LLM 改写）；3) 数据安全——100% 私域零出域，本地 AI 推理栈，对话不出客户内网。', 193, '{"doc":"overview","section":"卖点"}', NOW()),
    (doc_id_bigint, pid, 2, 'HiveMTK 仓库地址：Gitee 主仓库 https://gitee.com/xhpmayun/hivemtk ；GitHub 镜像 https://github.com/xiaofang142/hivemtk 。平台端仓库 hivemtk-platform 同样已开源。开源协议为 GNU AGPL-3.0：修改后若通过网络对外提供服务，必须按 AGPL-3.0 向所有用户免费提供修改后的完整源代码；仅内部私有部署不对外提供服务时无需公开修改。', 208, '{"doc":"overview","section":"仓库地址"}', NOW()),
    (doc_id_bigint, pid, 3, 'HiveMTK 技术栈：后端 Go 1.25 + Gin + GORM + pgvector；前端 Vue 3 + Vite + Element Plus + Pinia；数据库 PostgreSQL 15 + pgvector（1024 维）；缓存 Redis 7；LLM 走 llama.cpp + Qwen2.5-1.5B-Instruct（OpenAI 兼容 API）；Embedding 走本地 TEI/Qwen3-Embedding-0.6B（1024 维）；Rerank 走 bge-reranker-base；嵌入式客服为原生 JS IIFE + iframe + postMessage；部署为 Docker Compose。', 218, '{"doc":"overview","section":"技术栈"}', NOW()),
    (doc_id_bigint, pid, 4, 'HiveMTK 与平台端的关系：用户端（hivemtk，本仓库）归属企业客户，运行在客户本地内网，存储全部业务数据（对话/知识库/客户）；平台端（hivemtk-platform）归属平台运营方，运行在平台云端，仅存储元数据（商户/版本/统计），不接触、不存储、不访问任何用户业务数据。用户端通过低频 HTTPS 心跳访问平台端。', 178, '{"doc":"overview","section":"用户端与平台端"}', NOW());

    -- =========================================================
    -- 文档 2：开源信息与协议
    -- =========================================================
    INSERT INTO knowledge_documents (
        product_id, source_type, title, file_name, filename, file_type,
        chunk_count, embed_status, tags, category, priority,
        metadata, imported_by, status, created_at, updated_at
    ) VALUES (
        pid, 'text', 'HiveMTK 开源信息与协议', 'opensource_license.md', 'opensource_license.md', 'md',
        4, 'indexed', '["开源","协议","AGPL","免责"]', 'license', 95,
        '{"source":"seed"}', 'system_seed', 1, NOW(), NOW()
    ) RETURNING id INTO doc_id_bigint;

    INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, char_count, metadata, created_at) VALUES
    (doc_id_bigint, pid, 0, 'HiveMTK 采用 GNU Affero General Public License v3.0（AGPL-3.0）开源协议。核心诉求（AGPL-3.0 第 13 条·远程网络交互）：任何公司或个人只要修改了本项目代码，并将其通过网络（SaaS/云端/API/托管实例等）对外提供服务，就必须按 AGPL-3.0 向使用该服务的所有用户免费提供其修改后的完整对应源代码，且同样以 AGPL-3.0 开源。仅自己内部私有部署、不对外提供网络服务时无需公开修改。', 196, '{"doc":"license","section":"AGPL核心"}', NOW()),
    (doc_id_bigint, pid, 1, 'HiveMTK 主动触达模块（短信、邮件、微信公众号/企业微信、抖音/快手/小红书/闲鱼私信、Telegram、WhatsApp、网页客服等主动推送能力）属于核心敏感功能。使用者必须自行遵守各渠道平台规范，仅可向已授权联系人发送内容，禁止发送垃圾营销/欺诈/骚扰/钓鱼/色情/赌博/侵权内容。因违规使用导致的一切后果由使用者自行承担，与项目及作者无关。每次主动触达发送时服务端日志会打印 [COMPLIANCE] 合规提示，不可关闭。', 210, '{"doc":"license","section":"合规免责"}', NOW()),
    (doc_id_bigint, pid, 2, 'HiveMTK 是完全开源的本地私有化客服底座工具。用户利用本系统本地部署大语言模型、构建知识库及对话时，必须自行遵守所在国家、地区以及相关社交平台的法律法规。作者不参与任何用户的实际部署与运营，不对用户因本地模型产生的任何言论、内容合规性及后果承担法律责任。本项目按「原样（AS IS）」提供，不保证触达能力在任何平台长期可用。', 176, '{"doc":"license","section":"法律免责"}', NOW()),
    (doc_id_bigint, pid, 3, 'HiveMTK 联系与社区：Bug/Feature Request 走 Gitee Issues（12 小时内首响）；微信交流群管理员 wxid 为 xiao142000（7x24 答疑，产品/技术/运营）；商务合作邮箱 jideilvluoqun@gmail.com（企业级技术支持、定制集成）；贡献者公约见 CONTRIBUTING.md。群规：禁止广告/禁止政治/禁止人肉，违者秒踢。', 168, '{"doc":"license","section":"社区联系"}', NOW());

    -- =========================================================
    -- 文档 3：部署指南
    -- =========================================================
    INSERT INTO knowledge_documents (
        product_id, source_type, title, file_name, filename, file_type,
        chunk_count, embed_status, tags, category, priority,
        metadata, imported_by, status, created_at, updated_at
    ) VALUES (
        pid, 'text', 'HiveMTK 部署指南', 'deployment_guide.md', 'deployment_guide.md', 'md',
        8, 'indexed', '["部署","Docker","安装","硬件"]', 'deployment', 100,
        '{"source":"seed"}', 'system_seed', 1, NOW(), NOW()
    ) RETURNING id INTO doc_id_bigint;

    INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, char_count, metadata, created_at) VALUES
    (doc_id_bigint, pid, 0, 'HiveMTK 部署模式为私域独立部署：部署在商户自己的服务器（或私有云、混合云），数据库、推理栈、用户数据全部本地化。平台端以公网 API 形式调用，数据不落地平台端。每个商户独立一套完整系统（user-server + PostgreSQL + Redis + 推理栈）。禁止 SaaS/多租户模式，无 merchant_id 字段，所有数据归属当前部署实例。', 169, '{"doc":"deployment","section":"部署模式"}', NOW()),
    (doc_id_bigint, pid, 1, 'HiveMTK 硬件最低要求：CPU 2 核（推荐 8 核+，LLM 推理需 4 核+）；内存 4GB（推荐 16GB+，14B 模型需 12GB+）；磁盘 50GB（推荐 200GB+，模型文件约 10GB）；网络内网（公网对话需 HTTPS 或 FRP 穿透）。GPU 加速可选：NVIDIA 8GB+（dev 档 3B 模型），NVIDIA 16GB+（prod 档 14B 模型）。前置要求：Docker 24+ & Docker Compose v2。', 191, '{"doc":"deployment","section":"硬件要求"}', NOW()),
    (doc_id_bigint, pid, 2, 'HiveMTK 5 分钟上手三步：1) git clone https://gitee.com/xhpmayun/hivemtk.git && cd hivemtk；2) make install（自动生成 .env + docker-compose.yml + 构建前端）；3) vim .env 修改 3 个密钥（POSTGRES_PASSWORD / REDIS_PASSWORD / JWT_SECRET，可用 openssl rand -hex 32 生成），然后 make up 启动所有服务，访问 http://localhost:8204，默认账号 admin + 你设置的密码。只有把平台端也跑起来（PLATFORM_ENABLED=true）才需要再加 PLATFORM_ADMIN_PASSWORD 与 MERCHANT_API_SECRET。', 209, '{"doc":"deployment","section":"快速上手"}', NOW()),
    (doc_id_bigint, pid, 3, 'HiveMTK 关键端口：8204 user-server API（RESTful + WebSocket）；8202 PostgreSQL user_db（容器内端口，宿主机映射 8232）；8203 Redis；8207 mtk-llm（llama.cpp 推理，Qwen2.5-1.5B-Instruct）；8208 mtk-embedding（bge-m3，1024 维）；8209 mtk-rerank（bge-reranker-v2-m3）。健康检查：curl http://localhost:8204/health。', 196, '{"doc":"deployment","section":"端口"}', NOW()),
    (doc_id_bigint, pid, 4, 'HiveMTK 数据持久化通过命名卷：mtk_user_pg_data（PostgreSQL 数据）、mtk_user_redis_data（Redis 数据）、mtk_user_logs（应用日志）、mtk_user_uploads（用户上传文件）、mtk_user_data（install.lock 等运行时凭证）。不要用 bind mount 替换这些卷，否则数据可能丢失。', 161, '{"doc":"deployment","section":"持久化"}', NOW()),
    (doc_id_bigint, pid, 5, 'HiveMTK 模型档位切换：编辑 .env 替换 LLM_*/EMBEDDING_* 三行。dev 轻量档（当前默认）：LLM 为 Qwen2.5-1.5B-Instruct (Q4)，Embedding 为 Qwen3-Embedding-0.6B，内存需求 8GB，适合个人电脑/小内存部署。prod 重量档：LLM 为 Qwen2.5-14B-Instruct (Q4+)，Embedding 为 BAAI/bge-m3 (1024 维)，内存需求 16GB+，适合生产环境。', 195, '{"doc":"deployment","section":"模型档位"}', NOW()),
    (doc_id_bigint, pid, 6, 'HiveMTK 本地推理栈启动：make inference-host-install 安装 llama.cpp 二进制（首次）；make inference-host-models 下载 dev 档模型（首次）；make inference-host-up 启动 LLM + Embedding + Rerank 三个 llama-server；make inference-host-warmup 预热三端点（避免首请求慢）；make inference-host-test 端到端 smoke test；make inference-host-status 统一查看数据层+推理栈+user-server 状态。', 213, '{"doc":"deployment","section":"推理栈"}', NOW()),
    (doc_id_bigint, pid, 7, 'HiveMTK FRP 私域穿透：访客从公网进，数据经隧道回本地，云端不落一条对话。适合本地部署但需公网访问的场景。配置 frpc.toml 指向平台端 frps，将本地 user-server 的 8204 端口暴露为公网子域名。详细配置见 docs/architecture/FRP私域部署指南.md。', 159, '{"doc":"deployment","section":"FRP穿透"}', NOW());

    -- =========================================================
    -- 文档 4：运维手册
    -- =========================================================
    INSERT INTO knowledge_documents (
        product_id, source_type, title, file_name, filename, file_type,
        chunk_count, embed_status, tags, category, priority,
        metadata, imported_by, status, created_at, updated_at
    ) VALUES (
        pid, 'text', 'HiveMTK 运维手册', 'operations_manual.md', 'operations_manual.md', 'md',
        6, 'indexed', '["运维","备份","日志","监控"]', 'operations', 90,
        '{"source":"seed"}', 'system_seed', 1, NOW(), NOW()
    ) RETURNING id INTO doc_id_bigint;

    INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, char_count, metadata, created_at) VALUES
    (doc_id_bigint, pid, 0, 'HiveMTK 常用运维命令：make install 一键安装；make up 启动所有服务；make down 停止；make restart 重启；make logs 查看 user-server 日志；make ps 查看服务状态；make inference-up 单独拉起本地推理栈；make inference-down 停止推理栈（保留模型）；make web-build 重新构建前端；make sdk-build 重新构建 embed-sdk；make backup 备份 PostgreSQL；make restore FILE=... 恢复备份。', 198, '{"doc":"operations","section":"常用命令"}', NOW()),
    (doc_id_bigint, pid, 1, 'HiveMTK 数据层运维：make db-up 启动 PG + Redis 容器；make db-down 停止；make db-ps 查看容器状态；make db-logs 查看容器日志；make db-backup 备份 PG（输出 backup_YYYYMMDD_HHMMSS.sql）；make db-restore FILE=backup_xxx.sql 恢复 PG。备份文件为纯 SQL，可直接用 psql 导入。建议生产环境每日自动备份。', 182, '{"doc":"operations","section":"数据层"}', NOW()),
    (doc_id_bigint, pid, 2, 'HiveMTK 本地开发热更新：make dev-install 安装 air 热更新工具（如未安装）；make dev 启动 user-server 热更新（air 监听 .go/.yaml/.html 自动重编+重启）；make dev-stop 停止 air 进程；make dev-all 一键全栈（数据层 + 推理栈 + air 提示）；make dev-down 停止数据层 + 推理栈 + air。前端开发：cd user-web && npm run dev 启动 Vite 开发服务器。', 196, '{"doc":"operations","section":"开发模式"}', NOW()),
    (doc_id_bigint, pid, 3, 'HiveMTK 统一日志系统配置（zerolog 驱动）：level 可选 debug/info/warn/error；format 可选 json（生产，便于采集）或 console（本地，带颜色）；output 可选 stdout/file/both；file 为日志文件路径（output 为 file/both 时生效），超过 max_size(MB) 自动滚动保留 1 份备份；component 写入每条日志的 service 标识。配置位于 user-server/config.yaml 的 logging 段。', 198, '{"doc":"operations","section":"日志配置"}', NOW()),
    (doc_id_bigint, pid, 4, 'HiveMTK 推理栈状态检查：make inference-host-status 会显示数据层容器状态、llama-server 进程、端点连通性（8207 LLM / 8208 Embedding / 8209 Rerank / 8204 user-server，每个端点 curl /health 返回 200 即正常）。make inference-host-logs tail 三个 llama-server 日志。make inference-host-ps 显示 ps aux | grep llama-server。', 189, '{"doc":"operations","section":"推理栈检查"}', NOW()),
    (doc_id_bigint, pid, 5, 'HiveMTK 系统初始化流程：浏览器访问 http://your-server-ip:8204/setup，1) 设置超管账号（首次登录会强制改密，system_users.must_change_password 字段标记）；2) 完成系统初始化。私域部署无 LicenseKey 强制要求。初始化后默认管理员账号为 admin。健康检查 curl http://localhost:8204/health 返回 200 即服务正常。', 188, '{"doc":"operations","section":"初始化"}', NOW());

    -- =========================================================
    -- 文档 5：架构说明
    -- =========================================================
    INSERT INTO knowledge_documents (
        product_id, source_type, title, file_name, filename, file_type,
        chunk_count, embed_status, tags, category, priority,
        metadata, imported_by, status, created_at, updated_at
    ) VALUES (
        pid, 'text', 'HiveMTK 架构说明', 'architecture.md', 'architecture.md', 'md',
        5, 'indexed', '["架构","分层","私域","RAG"]', 'architecture', 95,
        '{"source":"seed"}', 'system_seed', 1, NOW(), NOW()
    ) RETURNING id INTO doc_id_bigint;

    INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, char_count, metadata, created_at) VALUES
    (doc_id_bigint, pid, 0, 'HiveMTK 整体架构：访客浏览器（公网）经 HTTPS/WSS（FRP/公网 IP/反代）→ 客户本地用户端（user-server Go+Gin :8204，含 PostgreSQL user_db :8202、Redis 7 :8203、mtk-llm :8207 Qwen2.5-1.5B-Instruct、mtk-embedding :8208 Qwen3-Embedding-0.6B、mtk-rerank :8209 bge-reranker-base）→ 平台端（独立仓库 hivemtk-platform，提供版本检查/商户标识校验/官方支持，不碰业务数据）。', 208, '{"doc":"architecture","section":"整体架构"}', NOW()),
    (doc_id_bigint, pid, 1, 'HiveMTK Go 代码严格遵守分层架构规范：Controller（接口层）→ Service（业务层）→ Repository（数据访问层）→ Model（数据模型层）→ Infra（基础设施层）。禁止：controller 直访 db/repository、service 直访 db、model 含业务方法、dto 反向引用 service。检查脚本 hivemtk/scripts/check-architecture.sh 已集成 CI。主文档 hivemtk/docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md。', 207, '{"doc":"architecture","section":"分层架构"}', NOW()),
    (doc_id_bigint, pid, 2, 'HiveMTK RAG 智能问答架构（2026-07-16 私域基线）：LLM 走外部 API（按业务需求出域），Embedding 走本地 docker 容器（私域数据不出域）。数据流：客户消息 → Embedding（本地 TEI+BAAI/bge-m3，dim=1024）→ pgvector 向量检索 → Top-K 知识片段 → 拼装 Prompt → LLM 调用（外部 API：Qwen/Claude/GPT）→ 后处理（敏感词过滤）→ 返回 reply+sources。', 196, '{"doc":"architecture","section":"RAG架构"}', NOW()),
    (doc_id_bigint, pid, 3, 'HiveMTK 三级 RAG 检索：1) 粗排——向量召回（pgvector + bge-m3 embedding，1024 维）；2) 精排——bge-reranker-v2-m3 重排（多语言跨编码器）；3) LLM 改写——HyDE/Query Rewriter 优化查询。置信度阈值默认 0.7，低于阈值降级到通用 LLM。多轮对话保留 3-5 轮上下文。转人工策略：MaxAIConsecutive=5（连续 5 次 AI 回复后建议转人工），ConfidenceThreshold=0.7。', 199, '{"doc":"architecture","section":"三级检索"}', NOW()),
    (doc_id_bigint, pid, 4, 'HiveMTK 数据安全：100% 私域零出域。本地 AI 推理栈（llama.cpp + TEI）三个 OpenAI 兼容服务跑在客户内网；所有对话、知识库、向量化、检索增强全程在客户内网完成，零外网可跑；FRP 私域穿透时访客从公网进、数据经隧道回本地，云端不落一条对话；满足等保、数据出境管控、私有化部署基线。可选云端 LLM：把 LLM_BASE_URL 改成 DeepSeek/OpenAI 即可，但 Embedding/Rerank 仍强制本地。', 208, '{"doc":"architecture","section":"数据安全"}', NOW());

    -- =========================================================
    -- 文档 6：功能模块
    -- =========================================================
    INSERT INTO knowledge_documents (
        product_id, source_type, title, file_name, filename, file_type,
        chunk_count, embed_status, tags, category, priority,
        metadata, imported_by, status, created_at, updated_at
    ) VALUES (
        pid, 'text', 'HiveMTK 功能模块', 'features.md', 'features.md', 'md',
        5, 'indexed', '["功能","模块","渠道","AI"]', 'features', 90,
        '{"source":"seed"}', 'system_seed', 1, NOW(), NOW()
    ) RETURNING id INTO doc_id_bigint;

    INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, char_count, metadata, created_at) VALUES
    (doc_id_bigint, pid, 0, 'HiveMTK 渠道覆盖（七端打通）：抖音（触达/智能卡片/自动回复/RAG 客服，含直播私信）、快手（同抖音）、小红书（含私信评论）、闲鱼（二手商品场景）、TikTok（海外矩阵）、微信/企业微信（含社群朋友圈）、短信（多通道营销）、邮件（SMTP/163/QQ）。统一 CDP 客户视图，一份资料全渠道触达；统一消息中心，会话/工单/留言一处看完。', 184, '{"doc":"features","section":"渠道覆盖"}', NOW()),
    (doc_id_bigint, pid, 1, 'HiveMTK AI 范式（ReAct 自主智能体）：ReAct 循环——感知→规划→调工具→反思（最多 5 轮），智能体自主决策；41 个内置工具——查库存、查物流、查客户画像、改地址、加白名单等；三级 RAG 检索；多智能体协作——被动应答智能体 + 主动触达智能体（ADR-013）；AI 销冠——话术模板 + RAG + 自动跟进全流程辅助坐席；可视化工作流——营销自动化编辑器零代码搭建 SOP。', 198, '{"doc":"features","section":"AI范式"}', NOW()),
    (doc_id_bigint, pid, 2, 'HiveMTK 功能模块（62 个核心业务模块）按业务域分类：认证与用户管理 4 个（JWT 鉴权/团队角色/商户初始化）；多平台卡片 5 个（抖音/快手/小红书/闲鱼/TikTok 自动生卡）；自动回复+RAG 6 个；邮件营销 5 个；短信营销 4 个；社群管理 4 个；短链与活码 3 个；线索与客户 9 个；营销自动化 6 个；内容创作 4 个；系统管理 6 个；第三方对接 2 个；统一消息 2 个。', 196, '{"doc":"features","section":"模块概览"}', NOW()),
    (doc_id_bigint, pid, 3, 'HiveMTK 嵌入式客服 Web Widget（embed-sdk）：原生 JS IIFE + iframe + postMessage 实现，可嵌入任意第三方网站。私域部署通过 frp 暴露的 user-server 前台聊天窗 SPA。进入后自动调用 /api/chat/public/* 与 /api/ws/visitor 完成双向会话。渠道 ID 软解析顺序：ctx.chat_channel_id > body.channel_id > X-Chat-Channel-Id > 默认 default。私域部署不强制要求 X-Chat-App-Key Header。', 198, '{"doc":"features","section":"嵌入式客服"}', NOW()),
    (doc_id_bigint, pid, 4, 'HiveMTK 客服转人工机制：chat.transfer_keywords 配置转人工关键词（人工/真人/转人工/human/operator/客服/agent/找人），命中后自动转人工；命中关键词后 30 秒内无人工接入则提示「客服正在接入中」（chat.fallback_seconds=30）。前端转人工按钮已移除，对用户永远显示「在线客服」，企业内部分为 AI 客服 + 人工客服。', 181, '{"doc":"features","section":"转人工"}', NOW());

    -- =========================================================
    -- 文档 7：资产市场与智能体
    -- =========================================================
    INSERT INTO knowledge_documents (
        product_id, source_type, title, file_name, filename, file_type,
        chunk_count, embed_status, tags, category, priority,
        metadata, imported_by, status, created_at, updated_at
    ) VALUES (
        pid, 'text', 'HiveMTK 资产市场与 AI 智能体', 'asset_market_agents.md', 'asset_market_agents.md', 'md',
        6, 'indexed', '["资产市场","智能体","资产包","RAG"]', 'asset', 85,
        '{"source":"seed"}', 'system_seed', 1, NOW(), NOW()
    ) RETURNING id INTO doc_id_bigint;

    INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, char_count, metadata, created_at) VALUES
    (doc_id_bigint, pid, 0, 'HiveMTK 资产市场三端闭环：平台端 platform-server（资产包商城/中台，唯一数据源+分发中心，存储/审核上架/购买分发/贡献者账户/使用统计）；开发者端（ISV/商户内 Playground，生产者，在 Playground 调教 messages→提交平台审核上架）；商户端 user-server+user-web（消费者/运行者，从平台浏览/购买试用/拉取到本地/运行织入自身 RAG，不生产只消费）。数据流铁律：user-web→user-server→platform-server，禁止 user-web 直连平台。', 217, '{"doc":"asset","section":"三端闭环"}', NOW()),
    (doc_id_bigint, pid, 1, 'HiveMTK 资产包 5 大类型：1) agent_persona 智能体角色（人设+开场白+语气）；2) sales_script 销冠话术（售前到成交话术库）；3) ab_test_plan AB 测试方案（话术/流程实验配置）；4) marketing_workflow 自动化工作流（用户旅程自动化）；5) industry_sop 行业 SOP 模板（标准化服务流程）。资产包数据格式为 JSONB，存储于 local_asset_data 表。', 193, '{"doc":"asset","section":"资产类型"}', NOW()),
    (doc_id_bigint, pid, 2, 'HiveMTK 资产市场业务链 8 步闭环：①开发者 Playground 调教→发布本地 asset_bundles；②提交平台审核 user-server SubmitToPlatform；③平台存资产+进入待审核；④平台运营审核通过；⑤商户浏览市场→试用/购买（记录成本不实际接入支付）；⑥商户同步拉取到本地库存 local_assets+local_asset_data；⑦商户客服系统实际使用资产 LoadByType 加载（use_count 累加）；⑧使用次数回传平台（best-effort 不阻塞主流程）。', 208, '{"doc":"asset","section":"业务链"}', NOW()),
    (doc_id_bigint, pid, 3, 'HiveMTK 多 AI 智能体架构：三个核心实体——1) AIAgent 智能体主表（人设/知识库/LLM/SOP/话术，可被多渠道账号绑定也可被多客服座席挂载）；2) ChannelAgentBinding 渠道账号↔智能体绑定（同一渠道账号可绑定多个智能体但只有一个 is_primary=true）；3) CustomerServiceAgent 客服座席↔智能体挂载。智能体类型：sales 销售型/customer_service 客服型/hybrid 混合。工作模式：passive 被动（消息进入系统调用智能体）/active 主动（智能体调用工具链主动触达）。', 223, '{"doc":"asset","section":"智能体架构"}', NOW()),
    (doc_id_bigint, pid, 4, 'HiveMTK AI 智能体配置：一个 AIAgent = 一套完整的智能体配置（人设 Persona + 知识库 RagProductIDs + LLM 配置 + SOP IDs + 话术库 ScriptLibraryIDs + 决策策略 DecisionStrategyIDs + A/B 实验 ABExperimentIDs）。LLM 参数：Temperature 0.7、MaxTokens 800、TopP 0.9、FrequencyPenalty 0.5、PresencePenalty 0.5。销售引擎开关：EnableRAG/EnableScriptMatch/EnableHumanizePolish/EnableContentAudit/EnablePlaybook 默认全开，RAGTopK 默认 3。', 224, '{"doc":"asset","section":"智能体配置"}', NOW()),
    (doc_id_bigint, pid, 5, 'HiveMTK 网页客服 AI 自动回复流程：访客发消息→VisitorChatService.SendMessage→校验会话归属→落库访客消息→同步到统一收件箱→WebSocket 推送给坐席→agentBindingSvc.LoadAgentForChannel 按 (web_embed, channel_id) 加载绑定智能体上下文→orchestrator.HandleIncomingWithAgent 走 RAG+AI 决策→AI 回复落库（sender_id=ai_assistant, AISource=rag）→HTTP 响应带 ai_response。多 AI 智能体路由与 webhook 保持一致。', 213, '{"doc":"asset","section":"网页客服流程"}', NOW());

    -- =========================================================
    -- 文档 8：常见问题 FAQ
    -- =========================================================
    INSERT INTO knowledge_documents (
        product_id, source_type, title, file_name, filename, file_type,
        chunk_count, embed_status, tags, category, priority,
        metadata, imported_by, status, created_at, updated_at
    ) VALUES (
        pid, 'text', 'HiveMTK 常见问题 FAQ', 'faq.md', 'faq.md', 'md',
        8, 'indexed', '["FAQ","常见问题"," troubleshooting"]', 'faq', 100,
        '{"source":"seed"}', 'system_seed', 1, NOW(), NOW()
    ) RETURNING id INTO doc_id_bigint;

    INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, char_count, metadata, created_at) VALUES
    (doc_id_bigint, pid, 0, 'Q: HiveMTK 是开源的吗？A: 是，采用 AGPL-3.0 协议完全开源。Gitee 主仓库 https://gitee.com/xhpmayun/hivemtk ，GitHub 镜像 https://github.com/xiaofang142/hivemtk 。修改后若通过网络对外提供服务须按 AGPL-3.0 开源修改后的完整源代码；仅内部私有部署不对外提供服务时无需公开。', 184, '{"doc":"faq","q":"开源协议"}', NOW()),
    (doc_id_bigint, pid, 1, 'Q: HiveMTK 收费吗？A: 本项目本身完全开源免费，可自由使用与私有部署。商务合作/企业级技术支持/定制集成可通过 jideilvluoqun@gmail.com 联系。微信交流群管理员 wxid: xiao142000 提供 7x24 答疑。', 158, '{"doc":"faq","q":"收费"}', NOW()),
    (doc_id_bigint, pid, 2, 'Q: 部署需要什么硬件？A: 最低 2 核 CPU/4GB 内存/50GB 磁盘；推荐生产 8 核+/16GB+/200GB+（含 LLM）。dev 轻量档（Qwen2.5-1.5B-Instruct + Qwen3-Embedding-0.6B）8GB 内存即可；prod 重量档（Qwen2.5-14B-Instruct + bge-m3）需 16GB+。GPU 加速可选（NVIDIA 8GB+ dev / 16GB+ prod）。前置要求 Docker 24+ & Docker Compose v2。', 206, '{"doc":"faq","q":"硬件要求"}', NOW()),
    (doc_id_bigint, pid, 3, 'Q: 数据安全吗？A: 100% 私域零出域。所有对话、知识库、向量化、检索增强全程在客户内网完成。本地 AI 推理栈（llama.cpp + TEI）跑在客户内网。FRP 私域穿透时访客从公网进、数据经隧道回本地，云端不落一条对话。满足等保、数据出境管控、私有化部署基线。可选云端 LLM（改 LLM_BASE_URL），但 Embedding/Rerank 仍强制本地。', 193, '{"doc":"faq","q":"数据安全"}', NOW()),
    (doc_id_bigint, pid, 4, 'Q: 支持哪些渠道？A: 七端打通——抖音/快手/小红书/闲鱼/TikTok/微信企业微信/短信/邮件。每个渠道支持触达、智能卡片（前 5 端）、自动回复、RAG 客服。统一 CDP 客户视图一份资料全渠道触达，统一消息中心会话/工单/留言一处看完。', 158, '{"doc":"faq","q":"渠道支持"}', NOW()),
    (doc_id_bigint, pid, 5, 'Q: 怎么启动？A: 三步——1) git clone https://gitee.com/xhpmayun/hivemtk.git && cd hivemtk；2) make install；3) vim .env 改 3 个密钥（POSTGRES_PASSWORD/REDIS_PASSWORD/JWT_SECRET，openssl rand -hex 32 生成），make up 启动。访问 http://localhost:8204，账号 admin + 你设置的密码。健康检查 curl http://localhost:8204/health。', 211, '{"doc":"faq","q":"启动"}', NOW()),
    (doc_id_bigint, pid, 6, 'Q: user-server 构建报错怎么办？A: user-server 构建缓存易损坏，若遇随机 undefined/EOF 报错，先 go clean -cache 再编译。命令：cd hivemtk/user-server && go clean -cache && go build ./...。若仍失败检查 Go 版本需 1.25+。', 167, '{"doc":"faq","q":"构建报错"}', NOW()),
    (doc_id_bigint, pid, 7, 'Q: 怎么联系作者？A: Bug/Feature Request 走 Gitee Issues（12 小时内首响）https://gitee.com/xhpmayun/hivemtk/issues ；微信交流群管理员 wxid: xiao142000（7x24 答疑）；商务合作 jideilvluoqun@gmail.com。贡献者公约见 CONTRIBUTING.md。', 187, '{"doc":"faq","q":"联系作者"}', NOW());

    -- =========================================================
    -- 文档 9：详细安装与初始化
    -- =========================================================
    INSERT INTO knowledge_documents (
        product_id, source_type, title, file_name, filename, file_type,
        chunk_count, embed_status, tags, category, priority,
        metadata, imported_by, status, created_at, updated_at
    ) VALUES (
        pid, 'text', 'HiveMTK 详细安装与初始化', 'install_guide.md', 'install_guide.md', 'md',
        6, 'indexed', '["安装","初始化","env","setup"]', 'install', 95,
        '{"source":"seed"}', 'system_seed', 1, NOW(), NOW()
    ) RETURNING id INTO doc_id_bigint;

    INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, char_count, metadata, created_at) VALUES
    (doc_id_bigint, pid, 0, 'HiveMTK 安装三步：1) git clone https://gitee.com/xhpmayun/hivemtk.git && cd hivemtk；2) make install 自动生成 .env + docker-compose.yml 并构建前端；3) vim .env 修改 3 个密钥（POSTGRES_PASSWORD / REDIS_PASSWORD / JWT_SECRET，用 openssl rand -hex 32 生成），然后 make up 启动所有服务，访问 http://localhost:8204，默认账号 admin + 你设置的密码。', 218, '{"doc":"install","section":"安装步骤"}', NOW()),
    (doc_id_bigint, pid, 1, 'HiveMTK 的 .env 三把必改密钥：POSTGRES_PASSWORD（PostgreSQL 密码）、REDIS_PASSWORD（Redis 密码）、JWT_SECRET（JWT token 签名密钥，务必随机）。均可用 openssl rand -hex 32 生成高强度值。改完无需手动建库，make install 已生成 docker-compose 与配置。超管初始口令属平台端（PLATFORM_ENABLED=true）范畴，离线部署默认不装配。', 205, '{"doc":"install","section":"env密钥"}', NOW()),
    (doc_id_bigint, pid, 2, 'HiveMTK 初始化：浏览器访问 http://your-server-ip:8204/setup，1) 设置超管账号（首次登录强制改密，system_users.must_change_password 标记）；2) 完成系统初始化。私域部署无 LicenseKey 强制要求，初始化后默认管理员账号为 admin。健康检查 curl http://localhost:8204/health 返回 200 即服务正常。', 198, '{"doc":"install","section":"初始化"}', NOW()),
    (doc_id_bigint, pid, 3, 'HiveMTK 数据持久化命名卷：mtk_user_pg_data（PostgreSQL 数据）、mtk_user_redis_data（Redis 数据）、mtk_user_logs（应用日志）、mtk_user_uploads（用户上传文件）、mtk_user_data（install.lock 等运行时凭证）。不要用 bind mount 替换这些卷，否则数据可能丢失。', 184, '{"doc":"install","section":"持久化"}', NOW()),
    (doc_id_bigint, pid, 4, 'HiveMTK 仓库目录：user-server（Go+Gin 后端，含 cmd/seed、migrations）、user-web（Vue3+Vite 前端）、hivemtk-platform（平台端，独立仓库）、scripts（构建/检查脚本）、docs（架构与营销特性文档）、migrations（SQL 迁移与种子）。构建产物由 make web-build / sdk-build 生成。', 198, '{"doc":"install","section":"目录结构"}', NOW()),
    (doc_id_bigint, pid, 5, 'HiveMTK 常用 make 目标：install（一键安装）、up/down/restart（启停）、logs/ps（日志/状态）、web-build/sdk-build（重建前端与 embed-sdk）、backup/restore（数据库备份恢复）、db-up/down/backup/restore（数据层）、inference-host-install/models/up/warmup/test/status（本地推理栈）、dev（开发热更新）。', 210, '{"doc":"install","section":"make目标"}', NOW());

    -- =========================================================
    -- 文档 10：推理栈与模型档位
    -- =========================================================
    INSERT INTO knowledge_documents (
        product_id, source_type, title, file_name, filename, file_type,
        chunk_count, embed_status, tags, category, priority,
        metadata, imported_by, status, created_at, updated_at
    ) VALUES (
        pid, 'text', 'HiveMTK 推理栈与模型档位', 'inference_models.md', 'inference_models.md', 'md',
        6, 'indexed', '["推理栈","模型","llama","档位"]', 'inference', 95,
        '{"source":"seed"}', 'system_seed', 1, NOW(), NOW()
    ) RETURNING id INTO doc_id_bigint;

    INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, char_count, metadata, created_at) VALUES
    (doc_id_bigint, pid, 0, 'HiveMTK 本地推理栈由三个 OpenAI 兼容服务组成：mtk-llm（:8207，llama.cpp 运行 Qwen2.5-1.5B-Instruct）、mtk-embedding（:8208，运行 bge-m3，1024 维向量）、mtk-rerank（:8209，运行 bge-reranker-v2-m3）。三者均跑在客户内网，由 llama.cpp 提供 HTTP 接口。', 196, '{"doc":"inference","section":"组成"}', NOW()),
    (doc_id_bigint, pid, 1, 'HiveMTK 推理栈命令：make inference-host-install 安装 llama.cpp 二进制（首次）；make inference-host-models 下载 dev 档模型（首次）；make inference-host-up 启动 LLM+Embedding+Rerank 三个 llama-server；make inference-host-warmup 预热避免首请求慢；make inference-host-test 端到端 smoke test；make inference-host-status 查看状态。', 216, '{"doc":"inference","section":"命令"}', NOW()),
    (doc_id_bigint, pid, 2, 'HiveMTK dev 轻量档（默认）：LLM 为 Qwen2.5-1.5B-Instruct (Q4)，Embedding 为 Qwen3-Embedding-0.6B，内存需求约 8GB，适合个人电脑/小内存部署，适合先跑通最小闭环。', 156, '{"doc":"inference","section":"dev档"}', NOW()),
    (doc_id_bigint, pid, 3, 'HiveMTK prod 重量档：LLM 为 Qwen2.5-14B-Instruct (Q4+)，Embedding 为 BAAI/bge-m3 (1024 维)，内存需求 16GB+，适合生产环境；可选 NVIDIA 16GB+ GPU 加速。', 158, '{"doc":"inference","section":"prod档"}', NOW()),
    (doc_id_bigint, pid, 4, 'HiveMTK 模型档位切换：编辑 .env 替换 LLM_MODEL/LLM_BASE_URL/EMBEDDING_MODEL/EMBEDDING_BASE_URL 等几行，保存后 make inference-host-up 重建推理栈生效。dev/prod 两套参数已内置，改 BASE_URL 也可指向已有 llama-server。', 178, '{"doc":"inference","section":"切换"}', NOW()),
    (doc_id_bigint, pid, 5, 'HiveMTK 可选云端 LLM：把 LLM_BASE_URL 改成 DeepSeek/OpenAI 等云端 API 即可走更强模型，但 Embedding/Rerank 仍强制本地（数据不出域）。注意 reasoning 类模型会消耗 max_tokens，必要时调大或摘掉 tools 重试。', 188, '{"doc":"inference","section":"云端兜底"}', NOW());

    -- =========================================================
    -- 文档 11：运维与排障
    -- =========================================================
    INSERT INTO knowledge_documents (
        product_id, source_type, title, file_name, filename, file_type,
        chunk_count, embed_status, tags, category, priority,
        metadata, imported_by, status, created_at, updated_at
    ) VALUES (
        pid, 'text', 'HiveMTK 运维与排障', 'troubleshooting.md', 'troubleshooting.md', 'md',
        6, 'indexed', '["运维","排障","备份","端口"]', 'troubleshooting', 90,
        '{"source":"seed"}', 'system_seed', 1, NOW(), NOW()
    ) RETURNING id INTO doc_id_bigint;

    INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, char_count, metadata, created_at) VALUES
    (doc_id_bigint, pid, 0, 'HiveMTK 运维命令：make up/down/restart/logs/ps 管理服务；make db-up/down/ps/logs 管理数据层；make db-backup/db-restore 备份恢复 PG；make inference-up/down 单独管理推理栈；make web-build/sdk-build 重建前端与客服 SDK。', 186, '{"doc":"troubleshooting","section":"命令"}', NOW()),
    (doc_id_bigint, pid, 1, 'HiveMTK 备份恢复：make db-backup 输出 backup_YYYYMMDD_HHMMSS.sql（纯 SQL，可直接 psql 导入）；make db-restore FILE=backup_xxx.sql 恢复。建议生产环境每日自动备份，并与应用日志卷(mtk_user_logs)一起归档。', 192, '{"doc":"troubleshooting","section":"备份"}', NOW()),
    (doc_id_bigint, pid, 2, 'HiveMTK 构建报随机 undefined/EOF 错误：user-server 构建缓存易损坏，先 go clean -cache 再编译：cd hivemtk/user-server && go clean -cache && go build ./...。仍需失败请确认 Go 版本 >= 1.25。', 178, '{"doc":"troubleshooting","section":"构建缓存"}', NOW()),
    (doc_id_bigint, pid, 3, 'HiveMTK 端口规划：8204 user-server；PostgreSQL 容器内 8202（宿主机映射 8232）；Redis 8203；8207 LLM；8208 Embedding；8209 Rerank。服务起不来常因 8202-8209 被占用，停占用进程或改 docker-compose 映射即可。', 190, '{"doc":"troubleshooting","section":"端口"}', NOW()),
    (doc_id_bigint, pid, 4, 'HiveMTK 推理栈健康检查：make inference-host-status 显示数据层容器、llama-server 进程、端点连通性（8207/8208/8209/8204 各 /health 返回 200 即正常）；make inference-host-logs tail 查看三个 llama-server 日志；make inference-host-ps 看进程。', 192, '{"doc":"troubleshooting","section":"推理栈状态"}', NOW()),
    (doc_id_bigint, pid, 5, 'HiveMTK 常见症状速查：①对话无回复→本地推理栈未起或 LLM_BASE_URL 错误，查 8207 /health；②RAG 召回为空→embedding 服务未跑或知识库为空，查 8208 与 rag_products；③构建随机报错→go clean -cache；④端口冲突→释放 8202-8209。', 206, '{"doc":"troubleshooting","section":"症状速查"}', NOW());

    -- =========================================================
    -- 文档 12：七端渠道接入与主动触达
    -- =========================================================
    INSERT INTO knowledge_documents (
        product_id, source_type, title, file_name, filename, file_type,
        chunk_count, embed_status, tags, category, priority,
        metadata, imported_by, status, created_at, updated_at
    ) VALUES (
        pid, 'text', 'HiveMTK 七端渠道接入与主动触达', 'channels_reach.md', 'channels_reach.md', 'md',
        6, 'indexed', '["渠道","接入","触达","合规"]', 'channels', 90,
        '{"source":"seed"}', 'system_seed', 1, NOW(), NOW()
    ) RETURNING id INTO doc_id_bigint;

    INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, char_count, metadata, created_at) VALUES
    (doc_id_bigint, pid, 0, 'HiveMTK 渠道覆盖（七端打通）：抖音（触达/智能卡片/自动回复/RAG 客服，含直播私信）、快手（同抖音）、小红书（含私信评论）、闲鱼（二手商品场景）、TikTok（海外矩阵）、微信/企业微信（含社群朋友圈）、短信（多通道营销）、邮件（SMTP/163/QQ）。统一 CDP 客户视图，统一消息中心。', 198, '{"doc":"channels","section":"七端概览"}', NOW()),
    (doc_id_bigint, pid, 1, 'HiveMTK 各渠道接入：在后台配置对应账号与凭证即可。抖音支持触达、智能卡片、自动回复、RAG 客服（含直播私信）；小红书含私信与评论；微信/企业微信含社群与朋友圈。各渠道主动触达须遵守平台规范，仅向已授权联系人发送。', 176, '{"doc":"channels","section":"接入"}', NOW()),
    (doc_id_bigint, pid, 2, 'HiveMTK 嵌入式客服 Web Widget（embed-sdk）：原生 JS IIFE + iframe + postMessage 实现，可嵌入任意第三方网站。私域部署通过 frp 暴露的 user-server 前台聊天窗 SPA；进入后自动调用 /api/chat/public/* 与 /api/ws/visitor 完成双向会话。渠道 ID 软解析顺序：ctx.chat_channel_id > body.channel_id > X-Chat-Channel-Id > 默认 default。', 220, '{"doc":"channels","section":"嵌入式客服"}', NOW()),
    (doc_id_bigint, pid, 3, 'HiveMTK 主动触达与合规：支持短信、邮件、微信公众号/企业微信、抖音/快手/小红书/闲鱼私信、Telegram、WhatsApp、网页客服等主动推送。使用者必须自行遵守各渠道平台规范，仅可向已授权联系人发送，禁止垃圾营销/欺诈/骚扰/钓鱼/色情/赌博/侵权内容。每次主动触达服务端日志打印 [COMPLIANCE] 提示，不可关闭。', 216, '{"doc":"channels","section":"合规"}', NOW()),
    (doc_id_bigint, pid, 4, 'HiveMTK 客服转人工：chat.transfer_keywords 配置转人工关键词（人工/真人/转人工/human/operator/客服/agent/找人），命中自动转人工；连续 5 次 AI 回复（MaxAIConsecutive=5）后建议转人工；命中关键词 30 秒内无人工接入提示「客服正在接入中」（chat.fallback_seconds=30）。', 210, '{"doc":"channels","section":"转人工"}', NOW()),
    (doc_id_bigint, pid, 5, 'HiveMTK 多平台卡片：自动生卡模块支持抖音/快手/小红书/闲鱼/TikTok 五端智能卡片（rich card），可下发结构化的商品卡、订单卡等；配合 RAG 客服与自动回复形成完整私域运营闭环。', 158, '{"doc":"channels","section":"智能卡片"}', NOW());

    -- =========================================================
    -- 文档 13：资产市场使用与贡献指南
    -- =========================================================
    INSERT INTO knowledge_documents (
        product_id, source_type, title, file_name, filename, file_type,
        chunk_count, embed_status, tags, category, priority,
        metadata, imported_by, status, created_at, updated_at
    ) VALUES (
        pid, 'text', 'HiveMTK 资产市场使用与贡献指南', 'asset_guide.md', 'asset_guide.md', 'md',
        6, 'indexed', '["资产市场","ISV","贡献","使用"]', 'asset_guide', 85,
        '{"source":"seed"}', 'system_seed', 1, NOW(), NOW()
    ) RETURNING id INTO doc_id_bigint;

    INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, char_count, metadata, created_at) VALUES
    (doc_id_bigint, pid, 0, 'HiveMTK 资产市场三端闭环：平台端 platform-server（资产包商城/中台，唯一数据源+分发中心，存储/审核上架/购买分发/贡献者账户/使用统计）；开发者端（ISV/商户内 Playground，生产者）；商户端 user-server+user-web（消费者/运行者）。数据流铁律：user-web→user-server→platform-server，禁止 user-web 直连平台。', 218, '{"doc":"asset_guide","section":"三端闭环"}', NOW()),
    (doc_id_bigint, pid, 1, 'HiveMTK 资产包 5 大类型：①agent_persona 智能体角色（人设+开场白+语气）②sales_script 销冠话术 ③ab_test_plan AB 测试方案 ④marketing_workflow 自动化工作流 ⑤industry_sop 行业 SOP 模板。数据格式 JSONB，存于 local_asset_data 表。', 196, '{"doc":"asset_guide","section":"资产类型"}', NOW()),
    (doc_id_bigint, pid, 2, 'HiveMTK 资产获取与使用：商户浏览市场→试用/购买→SyncPull 拉取到本地 local_assets + local_asset_data→客服系统 LoadByType 按类型加载织入自身 RAG（use_count 累加）→使用次数 best-effort 回传平台。整个过程商户端不生产资产，只消费运行。', 198, '{"doc":"asset_guide","section":"获取使用"}', NOW()),
    (doc_id_bigint, pid, 3, 'HiveMTK 贡献者/ISV 流程：在 Playground 调教 messages→通过 user-server SubmitToPlatform 提交平台审核→运营审核通过→上架市场。请先阅读 CONTRIBUTING.md 与贡献者公约（群规：禁广告/政治/人肉，违者秒踢）。', 178, '{"doc":"asset_guide","section":"贡献流程"}', NOW()),
    (doc_id_bigint, pid, 4, 'HiveMTK 资产业务链 8 步：①开发者 Playground 调教→发布本地 asset_bundles ②提交平台审核 ③平台存资产+待审核 ④运营审核通过 ⑤商户浏览试用/购买 ⑥SyncPull 拉取本地库存 ⑦客服系统实际使用 LoadByType ⑧使用次数回传平台。', 196, '{"doc":"asset_guide","section":"业务链"}', NOW()),
    (doc_id_bigint, pid, 5, 'HiveMTK 资产使用须知：开源资产免费；平台市场部分资产可能由 ISV 自行定价，本仓库种子仅做演示（记录成本不实际接入支付）。企业级集成与定制联系平台运营商务邮箱 jideilvluoqun@gmail.com。', 176, '{"doc":"asset_guide","section":"使用须知"}', NOW());

    -- 更新 RAG 产品统计
    UPDATE rag_products
       SET doc_count = 13,
           chunk_count = (SELECT COUNT(*) FROM knowledge_chunks WHERE product_id = pid),
           last_import_at = NOW(),
           updated_at = NOW()
     WHERE id = 'hivemtk-platform-cs';
END $$;

-- ============================================================
-- 3) 绑定 default 渠道：补充欢迎语，引导商户咨询
--    default_rag_product_id 暂不在此设置（字符串产品ID绑定，由 AI 智能体绑定覆盖）
--    AI 智能体绑定见 033_industry_ai_agents_seed.sql
-- ============================================================
UPDATE chat_channels
   SET welcome_message = '您好，我是 HiveMTK 官方客服助手，可为您解答关于项目开源信息、部署、运维、架构、资产市场、AI 智能体等问题。请问有什么可以帮您？',
       updated_at = NOW()
 WHERE channel_id = 'default';
