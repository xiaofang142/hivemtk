-- ============================================================
-- 034_rich_product_data.sql
-- 产品级数据丰富：新增 3 个通用智能体 + 绑定 FAQ/SOP 到智能体
-- 内容:
--   1) 新增 3 个核心通用智能体（通用电商客服 / 电商销售混合 / 通用社群运营）
--   2) 为 HiveMTK 官方客服智能体绑定 FAQ + SOP（设置 agent_id）
--   3) 为 HiveMTK 官方智能体补充更多产品级 FAQ（竞品对比、选型建议等）
--   4) 为通用电商客服智能体创建 40+ 条产品级 FAQ + 25 条 SOP 模板
--   5) 为电商销售混合智能体创建 30+ 条 FAQ + 25 条销售话术 SOP
--   6) 为通用社群运营智能体创建 25+ 条 FAQ + 20 条社群 SOP
--   7) 创建通用电商 RAG 知识库（3 文档 20 分段）
--   8) 替换 7 个行业假资产包中的 2 个（HiveMTK 官方 + 通用电商）为真实产品级内容
-- 背景:
--   迁移 033 创建了 8 个智能体但 FAQ/SOP 未绑定 agent_id，Layer1 匹配不到
--   迁移 032 的 500 条行业 Q&A 全是模板占位符（#1~#500 相同回答）
-- 设计依据:
--   - Layer1 FAQ 匹配严格按 agent_id 过滤（见 faq_entry.go MatchByAgent）
--   - Layer1 SOP 匹配严格按 agent_id 过滤（见 sop_template.go MatchByAgent）
-- ============================================================

-- ============================================================
-- PART 0: 幂等清理
-- ============================================================

-- 清理新智能体绑定的渠道
DELETE FROM channel_agent_bindings WHERE agent_id IN (
    SELECT id FROM ai_agents WHERE agent_code IN (
        'hivemtk-agent-general-cs',
        'hivemtk-agent-ecom-sales',
        'hivemtk-agent-community'
    )
);

-- 清理新智能体
DELETE FROM ai_agents WHERE agent_code IN (
    'hivemtk-agent-general-cs',
    'hivemtk-agent-ecom-sales',
    'hivemtk-agent-community'
);

-- 清理 FAQ/SOP 中带本迁移 tag 的数据（幂等重跑）
DELETE FROM faq_entries WHERE question LIKE '%[seed-034]%';
DELETE FROM sop_templates WHERE name LIKE '%[seed-034]%';

-- ============================================================
-- PART 1: 新增 3 个核心通用智能体
-- ============================================================

-- 1.1 通用电商客服智能体（覆盖售前咨询/售后退换/物流查询/活动说明）
INSERT INTO ai_agents (
    agent_code, name, description, avatar,
    agent_type, agent_mode,
    persona, system_prompt, greeting,
    rag_product_ids, sop_ids, script_library_ids,
    decision_strategy_ids, ab_experiment_ids,
    llm_model, llm_api_type, llm_base_url, llm_api_key,
    llm_model_detail, llm_max_retries, llm_request_timeout,
    temperature, max_tokens, top_p, frequency_penalty, presence_penalty,
    enable_rag, enable_script_match, enable_humanize_polish,
    enable_content_audit, enable_playbook, rag_top_k,
    confidence_threshold, max_ai_consecutive,
    status, version, created_at, updated_at
) VALUES (
    'hivemtk-agent-general-cs',
    '通用电商客服',
    '覆盖电商全场景的智能客服：售前商品咨询、规格材质推荐、价格活动说明、优惠券使用；售后退换货政策、维修保养、使用指导；物流查询、快递追踪、发货说明；订单修改、地址变更、发票申请等。绑定通用电商 RAG 知识库（ecommerce-general-cs）+ Layer1 FAQ（40+）+ SOP（25+）。',
    '',
    'customer_service',
    'passive',
    '你是一位专业、耐心、高效的电商客服，熟悉各类商品规格、退换货政策、物流流程、活动规则。回答原则：1) 商品参数准确无误；2) 价格与活动按现行规则；3) 退换货按政策不越权承诺；4) 物流查询给明确追踪方式；5) 语气温和亲切，避免生硬模板。',
    E'你是通用电商客服智能体。\n\n回答原则：\n1. 优先从 Layer1 FAQ 命中，命中直接返回标准答案\n2. FAQ 未命中时，从 RAG 知识库（ecommerce-general-cs）召回商品/物流/政策片段\n3. 涉及具体价格/库存/订单状态 → 引导用户提供订单号后查询，AI 不猜测\n4. 退换货 → 严格按 7 天无理由 / 15 天质量问题政策，不承诺超政策服务\n5. 物流 → 给快递公司 + 运单号，引导官网查询；超 3 天未更新建议联系人工\n6. 活动/优惠券 → 按当前活动规则说明，不同时使用的要明确告知\n7. 转人工关键词：人工/真人/转人工/客服/找人/投诉/差评',
    '您好，我是电商客服小助手😊 可以帮您：商品咨询、订单查询、物流追踪、退换货说明、活动参与等。请问有什么需要帮忙的？',
    ARRAY['ecommerce-general-cs']::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    'Qwen2.5-1.5B-Instruct',
    'openai',
    '',
    '',
    '',
    3,
    60,
    0.6,
    1000,
    0.9,
    0.5,
    0.5,
    TRUE,
    TRUE,
    TRUE,
    TRUE,
    FALSE,
    3,
    0.7,
    8,
    1,
    1,
    NOW(),
    NOW()
);

-- 1.2 电商销售混合智能体（种草推荐/逼单话术/复购提醒）
INSERT INTO ai_agents (
    agent_code, name, description, avatar,
    agent_type, agent_mode,
    persona, system_prompt, greeting,
    rag_product_ids, sop_ids, script_library_ids,
    decision_strategy_ids, ab_experiment_ids,
    llm_model, llm_api_type, llm_base_url, llm_api_key,
    llm_model_detail, llm_max_retries, llm_request_timeout,
    temperature, max_tokens, top_p, frequency_penalty, presence_penalty,
    enable_rag, enable_script_match, enable_humanize_polish,
    enable_content_audit, enable_playbook, rag_top_k,
    confidence_threshold, max_ai_consecutive,
    status, version, created_at, updated_at
) VALUES (
    'hivemtk-agent-ecom-sales',
    '电商销售混合',
    '兼具客服与销售能力的混合型智能体：售前种草推荐、痛点挖掘、场景化推荐；异议处理（价格/品质/物流/尺寸）；逼单话术（限时/库存/专属）；复购提醒、会员权益引导；活动预热、节日营销话术。适合微信/企微/社群私域触达场景。',
    '',
    'hybrid',
    'passive',
    '你是一位资深电商销冠，擅长种草式推荐、场景化话术、异议柔化处理、临门一脚逼单。不是生硬推销，而是基于客户需求的贴心顾问。语气活泼、共情力强、有温度。',
    E'你是电商销售智能体。\n\n核心能力：\n1. 种草推荐：问清使用场景 → 匹配对应系列 → 讲 1-2 个真实使用场景 → 引导选型号\n2. 异议处理：价格高 → 对比价值/分三期/有优惠券；怕不好 → 讲品质保证/7 天无理由/真实评价；物流慢 → 讲预计时效/可升级顺丰\n3. 逼单话术：限时优惠剩 X 小时；库存剩 X 件；今天下单送 X\n4. 复购引导：提醒上次买了 X，这次搭配 Y 更划算；会员积分可抵\n5. 转人工：大客户/定制需求引导转人工谈\n\n禁止：夸大功效、虚假承诺、贬低竞品',
    '您好呀～我是小助手，帮您找到最适合的商品 🎯 想买什么品类？送礼还是自用呀？',
    ARRAY['ecommerce-general-cs']::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    'Qwen2.5-1.5B-Instruct',
    'openai',
    '',
    '',
    '',
    3,
    60,
    0.7,
    1200,
    0.9,
    0.5,
    0.5,
    TRUE,
    TRUE,
    TRUE,
    TRUE,
    FALSE,
    3,
    0.65,
    6,
    1,
    1,
    NOW(),
    NOW()
);

-- 1.3 通用社群运营智能体
INSERT INTO ai_agents (
    agent_code, name, description, avatar,
    agent_type, agent_mode,
    persona, system_prompt, greeting,
    rag_product_ids, sop_ids, script_library_ids,
    decision_strategy_ids, ab_experiment_ids,
    llm_model, llm_api_type, llm_base_url, llm_api_key,
    llm_model_detail, llm_max_retries, llm_request_timeout,
    temperature, max_tokens, top_p, frequency_penalty, presence_penalty,
    enable_rag, enable_script_match, enable_humanize_polish,
    enable_content_audit, enable_playbook, rag_top_k,
    confidence_threshold, max_ai_consecutive,
    status, version, created_at, updated_at
) VALUES (
    'hivemtk-agent-community',
    '通用社群运营',
    '面向品牌私域社群的运营助手：新人入群欢迎、群规说明、活动通知、话题引导、Q&A 答疑、投诉安抚、活跃度提醒、积分查询、会员权益。适合品牌粉丝群、用户交流群、VIP 会员群等场景。',
    '',
    'customer_service',
    'passive',
    '你是一位热情、负责、有同理心的社群运营小姐姐，熟悉群规、活动、会员体系。既能温暖关怀，又能维护秩序。遇到敏感/投诉话题先共情再引导，不激化矛盾。',
    E'你是社群运营智能体。\n\n场景处理：\n1. 新人入群 → 发欢迎语 + 群规 + 本周活动预告\n2. 活动通知 → 按活动日程准时推送，附参与方式\n3. 群友提问 → 优先 FAQ 命中回答，复杂问题引导 @管理员\n4. 投诉/负面 → 先共情致歉 + 承诺转给管理员 + 跟进反馈\n5. 广告/违规 → 温和提醒"请遵守群规哦～违规会被移出群"\n6. 活跃度 → 定时抛出话题："大家周末都在用啥呀？"\n7. 转人工 → @管理员 或 "我帮您 @群主来处理哈"\n\n语气：亲切、有温度、像邻居小姐姐，不要官方套话',
    '欢迎来到咱们的社群呀～新人记得先看群规哦，有问题随时问我或者 @管理员 😊',
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    'Qwen2.5-1.5B-Instruct',
    'openai',
    '',
    '',
    '',
    3,
    60,
    0.65,
    800,
    0.9,
    0.5,
    0.5,
    TRUE,
    TRUE,
    TRUE,
    TRUE,
    FALSE,
    3,
    0.7,
    5,
    1,
    1,
    NOW(),
    NOW()
);

-- ============================================================
-- PART 2: 为 HiveMTK 官方客服智能体绑定已有的 FAQ + SOP
-- seed_faq_sop.go 创建的 FAQ/SOP 没有设置 AgentID，Layer1 匹配不到
-- 这里将它们全部绑定到 HiveMTK 官方智能体
-- ============================================================

DO $$
DECLARE
    hivemtk_agent_id BIGINT;
    cs_agent_id BIGINT;
    sales_agent_id BIGINT;
    community_agent_id BIGINT;
    pid VARCHAR(64) := 'ecommerce-general-cs';
BEGIN
    -- 获取智能体 ID
    SELECT id INTO hivemtk_agent_id FROM ai_agents WHERE agent_code = 'seed-hivemtk-product-service';
    SELECT id INTO cs_agent_id FROM ai_agents WHERE agent_code = 'hivemtk-agent-general-cs';
    SELECT id INTO sales_agent_id FROM ai_agents WHERE agent_code = 'hivemtk-agent-ecom-sales';
    SELECT id INTO community_agent_id FROM ai_agents WHERE agent_code = 'hivemtk-agent-community';

    RAISE NOTICE '智能体ID: hivemtk=%, cs=%, sales=%, community=%', hivemtk_agent_id, cs_agent_id, sales_agent_id, community_agent_id;

    -- ================================================================
    -- PART 2.1: 绑定 HiveMTK 官方 FAQ + SOP（带 seedTag 的全局数据）
    -- ================================================================
    IF hivemtk_agent_id IS NOT NULL THEN
        UPDATE faq_entries SET agent_id = hivemtk_agent_id WHERE question LIKE '%HiveMTK%' AND agent_id IS NULL;
        UPDATE sop_templates SET agent_id = hivemtk_agent_id WHERE name LIKE '%HiveMTK%' AND agent_id IS NULL;
        RAISE NOTICE '✓ 已将 HiveMTK 主题 FAQ/SOP 绑定到官方智能体 %', hivemtk_agent_id;
    END IF;

    -- ================================================================
    -- PART 3: 为 HiveMTK 官方智能体补充更多产品级 FAQ（竞品/选型/深入）
    -- ================================================================
    IF hivemtk_agent_id IS NOT NULL THEN
        INSERT INTO faq_entries (question, answer, keywords, category, intent, confidence, hit_count, agent_id, enabled, created_at, updated_at) VALUES
        (
            'HiveMTK 和 Dify Coze 详细对比 [seed-034]',
            E'HiveMTK vs Dify vs Coze 核心差异：\n\n1. 定位：Dify/Coze 是通用 LLM 工作流编排 SaaS（写死节点 → 按流程跑）；HiveMTK 是私域部署的 AI 营销 OS（ReAct 自主智能体 + 41 工具 + 七端原生接入）\n\n2. 渠道：Dify/Coze 需自己接渠道；HiveMTK 原生打通抖音/快手/小红书/闲鱼/TikTok/微信企微/短信/邮件七端\n\n3. 数据：Dify/Coze 云端 SaaS 数据可能出域；HiveMTK 100% 私域零出域，全部跑在内网\n\n4. 智能体：Dify/Coze 工作流编排（if-then-else）；HiveMTK ReAct 循环（感知→规划→调工具→反思，最多 5 轮自主决策）\n\n5. 合规：Dify/Coze 多租户 SaaS；HiveMTK 单租户私域，满足等保/数据出境管控\n\n选 HiveMTK 如果你：需要七端渠道 + 数据不出域 + 私域自主部署 + 营销自动化',
            ARRAY['Dify', 'Coze', '区别', '对比', '竞品', '差异', '选择']::text[],
            'overview', 'overview', 0.95, 200, hivemtk_agent_id, TRUE, NOW(), NOW()
        ),
        (
            '和 FastGPT RAGFlow 区别 [seed-034]',
            E'HiveMTK vs FastGPT/RAGFlow：\n\nFastGPT/RAGFlow 专注 RAG 问答编排；HiveMTK 在 RAG 之上叠加了：七端渠道接入、CDP 客户管理、主动触达（短信/邮件/社媒）、营销自动化工作流、ReAct 自主智能体、私域数据安全。\n\n简单说：FastGPT 解决"AI 怎么回答"；HiveMTK 解决"AI 怎么在私域营销全流程干活"。',
            ARRAY['FastGPT', 'RAGFlow', '区别', '对比']::text[],
            'overview', 'overview', 0.93, 150, hivemtk_agent_id, TRUE, NOW(), NOW()
        ),
        (
            'HiveMTK 能做什么业务 [seed-034]',
            E'HiveMTK 覆盖的业务场景：\n\n① 私域客服——网站嵌入、抖音/小红书/微信等七端 AI 自动回复，RAG 知识库自动回答，命中 FAQ 秒回\n② 主动营销——短信群发、邮件营销、社媒私信批量触达（需合规）\n③ 客户管理——统一 CDP 客户视图，全渠道客户画像聚合\n④ 智能营销自动化——可视化 SOP 编排，定时/触发式自动化\n⑤ AI 销售辅助——话术库、异议处理模板、自动跟进建议\n⑥ 数据看板——对话量、命中率、满意度、客户转化等多维报表',
            ARRAY['功能', '业务', '能做什么', '场景']::text[],
            'features', 'features', 0.94, 180, hivemtk_agent_id, TRUE, NOW(), NOW()
        ),
        (
            '私有化部署有什么好处 [seed-034]',
            E'私有化部署核心价值：\n\n1. 数据安全——所有对话、客户、知识库数据不出您的内网，满足等保/数据出境管控\n2. 无第三方依赖——不被 SaaS 厂商卡脖子，不担心服务停摆\n3. 深度定制——可自由修改代码、集成内部系统、接私有模型\n4. 成本可控——一次性投入，无持续 SaaS 订阅费\n5. 合规优先——金融/政务/医疗等行业刚需场景',
            ARRAY['私有化', '部署', '好处', '优势', '私域']::text[],
            'architecture', 'architecture', 0.92, 120, hivemtk_agent_id, TRUE, NOW(), NOW()
        ),
        (
            'ReAct 自主智能体和工作流有什么区别 [seed-034]',
            E'核心区别：谁来决策？\n\n工作流（Dify/Coze）——**人决策**：开发者提前把所有 if-then-else 画成流程图，AI 只是执行器\nReAct 智能体（HiveMTK）——**AI 决策**：给 AI 一套工具（41 个内置），遇到问题自己想："我先查下用户画像→再查下库存→然后推荐产品→最后跟进"\n\n类比：工作流是"预写好的剧本让 AI 演"；ReAct 是"给演员一套道具让他即兴表演"\n\n优势：工作流稳定可控；ReAct 灵活覆盖长尾场景\nHiveMTK 两者都支持：ReAct 做主智能体决策，可视化工作流做确定性的营销自动化',
            ARRAY['ReAct', '智能体', '工作流', '区别', '自主']::text[],
            'architecture', 'architecture', 0.91, 160, hivemtk_agent_id, TRUE, NOW(), NOW()
        ),
        (
            '七端渠道分别支持什么 [seed-034]',
            E'各渠道能力明细：\n\n✅ 抖音/快手：私信自动回复、RAG 客服、直播私信、智能卡片、主动触达（私信）\n✅ 小红书：私信自动回复、RAG 客服、评论监控、主动触达\n✅ 闲鱼：私信自动回复、RAG 客服、商品关联\n✅ TikTok：海外私信自动回复、RAG 客服、主动触达\n✅ 微信/企业微信：社群管理、朋友圈、客户联系、RAG 客服\n✅ 短信：批量营销、验证码、通知类短信（多通道）\n✅ 邮件：SMTP/163/QQ，营销邮件、通知邮件\n✅ 网页：embed-sdk 嵌入任意网站的 iframe 客服',
            ARRAY['渠道', '抖音', '小红书', '微信', '短信', '邮件', '接入']::text[],
            'features', 'features', 0.93, 140, hivemtk_agent_id, TRUE, NOW(), NOW()
        ),
        (
            '41 个内置工具都有什么 [seed-034]',
            E'HiveMTK 智能体内置 41 个工具，按类别：\n\n📊 数据查询（12 个）：查客户画像、查订单、查库存、查物流、查价格、查活动、查优惠券、查积分、查会员等级、查历史对话、查评价、查竞品\n\n✏️ 业务操作（10 个）：创建订单、修改地址、申请退款、加白名单、发优惠券、发会员邀请、建标签、改备注、发送跟进消息、转人工\n\n🔍 检索类（6 个）：RAG 知识库检索、向量搜索、全文关键词搜索、FAQ 匹配、SOP 模板匹配、资产包加载\n\n📢 触达类（7 个）：发短信、发邮件、发社媒私信、发企微消息、发社群通知、发朋友圈、发智能卡片\n\n⚙️ 系统类（6 个）：意图识别、情感分析、话术生成、风险评估、策略决策、审计日志',
            ARRAY['工具', '41', '内置', 'tool', 'ReAct']::text[],
            'features', 'features', 0.9, 100, hivemtk_agent_id, TRUE, NOW(), NOW()
        ),
        (
            '资产市场 ISV 怎么赚钱 [seed-034]',
            E'ISV 盈利模式：\n\n1. 卖资产包——在 Playground 调教好智能体人设/话术库/行业 SOP，提交平台审核上架，商户购买后按次/按月收费\n2. 企业定制——通过商务邮箱 jideilvluoqun@gmail.com 谈大客户定制集成、私有化部署、专属模型训练\n3. 技术咨询——基于自己的行业经验（电子烟/成人/移民/货代等）提供客服体系搭建咨询\n4. 生态分成——未来平台可能提供推荐分成机制\n\n当前阶段种子资产免费，后续会逐步开放定价能力',
            ARRAY['ISV', '赚钱', '盈利', '资产市场', '收入']::text[],
            'asset', 'asset', 0.87, 60, hivemtk_agent_id, TRUE, NOW(), NOW()
        ),
        (
            '部署失败常见原因 [seed-034]',
            E'常见部署坑速查：\n\n① 端口冲突——8202-8209 被占用，lsof -i :8204 检查，改 docker-compose 映射\n② 内存不足——dev 档最低 8GB，跑起来后 free -h 确认\n③ Go 版本不够——需要 1.25+，go version 检查\n④ 构建缓存损坏——go clean -cache 后重试\n⑤ .env 密钥没改——JWT_SECRET 默认值会导致安全警告\n⑥ 推理栈没起——make inference-host-status 检查 8207/8208/8209\n⑦ FRP 穿透失败——frpc.toml 配置、frps 端口、防火墙都要检查\n⑧ PostgreSQL 连接——看 docker-compose.yml 里的 POSTGRES_PASSWORD 和 .env 是否一致',
            ARRAY['失败', '部署', '报错', '问题', '坑', 'troubleshoot']::text[],
            'deploy', 'deploy', 0.9, 180, hivemtk_agent_id, TRUE, NOW(), NOW()
        ),
        (
            '怎么快速验证跑通了 [seed-034]',
            E'5 分钟验证全链路：\n\n1️⃣ make up 启动 → docker ps 看到 user-server/PostgreSQL/Redis 三个容器\n2️⃣ curl http://localhost:8204/health → 返回 {"status":"ok"}\n3️⃣ 浏览器访问 http://localhost:8204/setup → 完成初始化设 admin 密码\n4️⃣ make inference-host-up 启动推理栈 → curl http://localhost:8207/health → ok\n5️⃣ 登录后台 → 打开网页客服 → 发消息"你好" → AI 有回复 ✅\n\n任何一步不对看 make logs 排查',
            ARRAY['验证', '测试', '跑通', '健康检查', 'smoke']::text[],
            'deploy', 'deploy', 0.91, 150, hivemtk_agent_id, TRUE, NOW(), NOW()
        ),
        (
            '三档模型怎么选 [seed-034]',
            E'模型档位选择指南：\n\n🟢 dev 轻量档（默认）\nLLM: Qwen2.5-1.5B-Instruct (Q4)\nEmbedding: Qwen3-Embedding-0.6B\n内存: 8GB 即可\n场景: 个人测试 / 小团队试用 / 先跑通闭环\n\n🟡 prod 重量档\nLLM: Qwen2.5-14B-Instruct (Q4+)\nEmbedding: BAAI/bge-m3 (1024维)\n内存: 16GB+，可选 NVIDIA 16GB+ GPU\n场景: 生产环境 / 中等规模客服\n\n🔵 云端增强档\nLLM: DeepSeek/Claude/GPT-4（云端 API）\nEmbedding/Rerank: 仍本地（数据不出域）\n场景: 高智能要求 / 复杂推理场景\n\n切换：改 .env 里 LLM_BASE_URL / EMBEDDING_BASE_URL',
            ARRAY['模型', '档位', 'llm', 'dev', 'prod', '选择', '推荐']::text[],
            'deploy', 'deploy', 0.93, 170, hivemtk_agent_id, TRUE, NOW(), NOW()
        );

        -- 验证
        RAISE NOTICE '✓ HiveMTK 官方 FAQ 总数: %', (SELECT COUNT(*) FROM faq_entries WHERE agent_id = hivemtk_agent_id);
    END IF;

    -- ================================================================
    -- PART 4: 创建通用电商 RAG 知识库（Layer2 兜底）
    -- ================================================================
    IF NOT EXISTS (SELECT 1 FROM rag_products WHERE id = pid) THEN
        INSERT INTO rag_products (
            id, name, description, category, vector_table,
            embedding_model, embedding_dim, llm_model,
            temperature, max_tokens, top_p, frequency_penalty, presence_penalty,
            response_format, system_prompt,
            top_k, chunk_size, chunk_overlap, similarity_threshold,
            is_active, status, doc_count, chunk_count,
            created_at, updated_at
        ) VALUES (
            pid,
            '通用电商客服知识库',
            '覆盖电商售前咨询（商品规格/材质/价格）、售后（退换货政策/维修）、物流（发货时效/快递查询）、活动（优惠券/满减/会员）、订单（修改/地址/发票）等通用电商客服场景。',
            'ecommerce',
            'rag_ecommerce_general',
            'bge-m3', 1024, 'Qwen2.5-1.5B-Instruct',
            0.3, 1024, 0.9, 0.5, 0.5,
            'text',
            '你是电商客服助手，依据检索到的知识片段回答。原则：1) 严格按政策回答，不超权承诺；2) 涉及具体订单/价格/库存引导用户提供订单号后查询；3) 保持礼貌亲切；4) 若片段未覆盖，诚实说明。',
            5, 800, 100, 0.55,
            TRUE, 1, 0, 0,
            NOW(), NOW()
        );

        -- 文档 1：售前咨询（商品规格/材质/价格/活动）
        DECLARE doc_id1 BIGINT;
        INSERT INTO knowledge_documents (
            product_id, source_type, title, file_name, filename, file_type,
            chunk_count, embed_status, tags, category, priority,
            metadata, imported_by, status, created_at, updated_at
        ) VALUES (
            pid, 'text', '电商售前咨询手册', 'pre_sales.md', 'pre_sales.md', 'md',
            8, 'indexed', '["售前","商品","规格","价格","活动"]', 'pre_sales', 100,
            '{"source":"seed-034"}', 'system_seed', 1, NOW(), NOW()
        ) RETURNING id INTO doc_id1;

        INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, char_count, metadata, created_at) VALUES
        (doc_id1, pid, 0, '商品规格查询标准流程：客户问规格 → 确认具体款号/系列 → 查规格表（尺寸/材质/重量/产地/适用人群）→ 分点列 3-5 个核心参数 → 引导看详情页或发参数图。常用拒绝："具体参数以详情页最新为准，我帮您查一下哈～"', 158, '{"doc":"pre_sales","section":"规格流程"}', NOW()),
        (doc_id1, pid, 1, '价格说明：标价为商品指导价，最终价格以下单时显示为准。活动叠加规则——秒杀/限时折扣与优惠券不可同时使用（二选一）；会员折扣与优惠券可叠加；满减与优惠券看活动详情页说明。任何情况下不口头承诺"这个价买贵了退差价"，引导看价保政策。', 182, '{"doc":"pre_sales","section":"价格"}', NOW()),
        (doc_id1, pid, 2, '材质安全承诺：所有商品材质符合国家相关标准，详情页附检测报告。贴身衣物/接触皮肤类商品特别说明面料成分（A类/B类）；食品类需有 SC 编号和保质期说明；化妆品需有特证编号。客户对材质有疑虑时主动提供检测报告截图。', 168, '{"doc":"pre_sales","section":"材质安全"}', NOW()),
        (doc_id1, pid, 3, '尺码推荐流程：询问身高体重 → 查尺码表 → 推荐正码（不偏大不偏小）→ 特殊体型提醒（大肚腩/孕后期建议大一码）→ 附尺码对照表链接 → 犹豫时引导看用户评价里的尺码反馈。鞋类还需问脚长、是否穿袜子。', 178, '{"doc":"pre_sales","section":"尺码"}', NOW()),
        (doc_id1, pid, 4, '活动话术模板："这款目前有 XX 活动，XX 时间结束，现在下单立省 XX 元 🎉"；"还有 XX 件，库存紧张哦～"；"今天下单还送 XX 赠品"；"会员专享价比普通价低 XX%"。禁止用"最后一件""马上没了"等过度营销词。', 165, '{"doc":"pre_sales","section":"活动话术"}', NOW()),
        (doc_id1, pid, 5, '优惠券使用规则：优惠券分品类券（限特定品类）、满减券（需满足金额门槛）、新人券（限首次下单）。查看方式——用户中心→优惠券；领取方式——首页活动页/粉丝群/会员生日。不叠加规则以优惠券详情页为准。', 172, '{"doc":"pre_sales","section":"优惠券"}', NOW()),
        (doc_id1, pid, 6, '组合推荐策略：客户问单品 → 主动推荐搭配（"这个和 XX 一起买更搭哦，立减 XX 元"）；客户问品类 → 先问用途（送礼还是自用）、送谁、预算多少 → 推荐 2-3 款 → 每款讲一个亮点 → 引导选。', 169, '{"doc":"pre_sales","section":"搭配推荐"}', NOW()),
        (doc_id1, pid, 7, '送礼场景话术："送礼的话推荐 XX 款，包装精美有礼盒 🎁"；"这款是今年爆款，很多人买来送 XX"；"附手写贺卡，写上祝福更有心意～"。主动问送礼对象、预算、是否需要贺卡。', 156, '{"doc":"pre_sales","section":"送礼"}', NOW());

        -- 文档 2：售后退换货
        DECLARE doc_id2 BIGINT;
        INSERT INTO knowledge_documents (
            product_id, source_type, title, file_name, filename, file_type,
            chunk_count, embed_status, tags, category, priority,
            metadata, imported_by, status, created_at, updated_at
        ) VALUES (
            pid, 'text', '电商售后退换货手册', 'after_sales.md', 'after_sales.md', 'md',
            7, 'indexed', '["售后","退换货","退款","维修"]', 'after_sales', 100,
            '{"source":"seed-034"}', 'system_seed', 1, NOW(), NOW()
        ) RETURNING id INTO doc_id2;

        INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, char_count, metadata, created_at) VALUES
        (doc_id2, pid, 0, '7 天无理由退换：适用除贴身内衣/食品/特殊定制外的所有商品；条件——商品完好、吊牌未拆、配件齐全、不影响二次销售；流程——申请→寄回→验收入库→退款（原路返回 1-3 工作日）。运费——质量问题商家承担，个人原因买家承担。', 178, '{"doc":"after_sales","section":"7天无理由"}', NOW()),
        (doc_id2, pid, 1, '质量问题处理：签收后 15 天内可申请质量问题退换；需提供照片/视频证据（外观/功能/尺寸）；商家审核通过后寄回；运费商家承担；特殊情况（小额问题）可协商部分退款不退货。', 162, '{"doc":"after_sales","section":"质量问题"}', NOW()),
        (doc_id2, pid, 2, '退款时效说明：①原路退回（微信/支付宝/银行卡）1-3 工作日到账；②账户余额退款实时到账；③花呗/信用卡 3-5 工作日到账。超 5 工作日未到账引导查支付软件账单。', 158, '{"doc":"after_sales","section":"退款时效"}', NOW()),
        (doc_id2, pid, 3, '商品已发货但想退款：①未签收——直接申请退款，商家联系快递拦截；②已签收——走 7 天无理由流程；③部分发货——可退未发部分，已发部分签收后退。运费：已发货未签收的拦截费由商家承担（非用户原因）。', 168, '{"doc":"after_sales","section":"发货后退款"}', NOW()),
        (doc_id2, pid, 4, '破损/丢失处理：签收时发现破损——拍照留证→当场拒收→联系客服补发；签收后破损——24 小时内联系客服→提供照片→商家走物流理赔流程；整单丢失——商家走物流索赔→同时给用户补发或退款。', 172, '{"doc":"after_sales","section":"破损丢失"}', NOW()),
        (doc_id2, pid, 5, '超售后期沟通话术："理解您的心情～虽然已经过了退换期，我帮您 @售后专员 看看能不能特殊处理一下"——先共情，再转人工，不拒绝。同时可以说"这款有任何使用问题都可以联系我们，终身技术支持"。', 168, '{"doc":"after_sales","section":"超期话术"}', NOW()),
        (doc_id2, pid, 6, '投诉安抚 SOP：①先共情"真的很抱歉给您带来不好的体验 😢"②承认问题（不辩解）③给出具体方案（退款/补发/优惠券）④确认接受⑤跟进结果。禁止：和用户争辩、用"这是规定"搪塞、让用户自己找老板。', 175, '{"doc":"after_sales","section":"投诉安抚"}', NOW());

        -- 文档 3：物流查询与订单
        DECLARE doc_id3 BIGINT;
        INSERT INTO knowledge_documents (
            product_id, source_type, title, file_name, filename, file_type,
            chunk_count, embed_status, tags, category, priority,
            metadata, imported_by, status, created_at, updated_at
        ) VALUES (
            pid, 'text', '电商物流与订单操作手册', 'logistics_order.md', 'logistics_order.md', 'md',
            6, 'indexed', '["物流","快递","订单","发货"]', 'logistics', 95,
            '{"source":"seed-034"}', 'system_seed', 1, NOW(), NOW()
        ) RETURNING id INTO doc_id3;

        INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, char_count, metadata, created_at) VALUES
        (doc_id3, pid, 0, '发货时效规则：①现货——48 小时内发货（工作日 48h）；②预售——以商品详情页标注的发货时间为准（通常 7-15 天）；③大促期间——发货时效可能延长，详情页会公告；④节假日——顺延至节后工作日。', 162, '{"doc":"logistics","section":"发货时效"}', NOW()),
        (doc_id3, pid, 1, '物流查询方式：①发送订单号 → 客服查运单+快递公司 → 返回快递公司官网链接；②提醒用户可自行查——"在订单详情点【查看物流】就能实时追踪啦～"；③超 3 天未更新——建议联系快递公司官网查询，或客服协助联系快递。', 178, '{"doc":"logistics","section":"物流查询"}', NOW()),
        (doc_id3, pid, 2, '地址修改流程：①订单未发货——直接后台改地址（买家/客服都可操作）；②订单已发货——联系快递改址（可能产生改址费，偏远地区无法改）；③建议：下单前仔细核对地址！', 155, '{"doc":"logistics","section":"改地址"}', NOW()),
        (doc_id3, pid, 3, '发票申请流程：①下单时备注发票抬头+税号；②已下单未开票——申请补开，1-3 工作日开出；③电子发票——发到邮箱；④纸质发票——随单寄或单独寄（可能有额外运费）；⑤发票信息有误——作废重开（当月可直接改，跨月需走红冲流程）。', 168, '{"doc":"logistics","section":"发票"}', NOW()),
        (doc_id3, pid, 4, '超区/无网点处理：快递到不了用户地址 → ①提前电话联系用户协商；②转发其他可达快递；③退回后换地址重发；④告知时效延长。禁止静默退回不通知。', 155, '{"doc":"logistics","section":"超区"}', NOW()),
        (doc_id3, pid, 5, '催发货话术："我帮您催一下哈～"→ 内部标记加急；"这款目前现货紧张，正在陆续发出中，您的订单会在 XX 时间前发出"；"给您发最新发货进度截图"。给一个明确时间预期，不模糊。', 148, '{"doc":"logistics","section":"催发货"}', NOW());

        -- 更新统计
        UPDATE rag_products
        SET doc_count = 3, chunk_count = (SELECT COUNT(*) FROM knowledge_chunks WHERE product_id = pid),
            last_import_at = NOW(), updated_at = NOW()
        WHERE id = pid;
        RAISE NOTICE '✓ 通用电商 RAG 知识库已创建，共 % 文档 % 分段', 3, (SELECT COUNT(*) FROM knowledge_chunks WHERE product_id = pid);
    END IF;

    -- ================================================================
    -- PART 5: 为通用电商客服智能体创建 40+ 条产品级 FAQ
    -- ================================================================
    IF cs_agent_id IS NOT NULL THEN
        RAISE NOTICE '✓ 开始写入通用电商 FAQ，agent_id=%', cs_agent_id;

        INSERT INTO faq_entries (question, answer, keywords, category, intent, confidence, hit_count, agent_id, enabled, created_at, updated_at) VALUES
        -- 售前（10 条）
        ('这个产品有什么功能 [seed-034]', '亲，这款商品的核心卖点是：①[卖点1] ②[卖点2] ③[卖点3]。您可以告诉我您的使用场景，我帮您推荐最合适的款～', ARRAY['功能', '卖点', '特点', '好用在哪']::text[], 'pre_sales', 'product_info', 0.88, 50, cs_agent_id, TRUE, NOW(), NOW()),
        ('有没有货 [seed-034]', '让我帮您查一下库存哈～这款目前 [有货/少量库存/预售中，XX 时间发货]。库存紧张的话建议尽早下单哦！', ARRAY['有货', '库存', '现货', '有没有']::text[], 'pre_sales', 'stock', 0.92, 100, cs_agent_id, TRUE, NOW(), NOW()),
        ('什么时候发货 [seed-034]', '现货商品在 48 小时内（工作日）发出；预售商品以详情页标注为准（通常 7-15 天）。大促期间发货时效可能延长，具体看详情页公告哦～', ARRAY['发货', '什么时候发', '时效', '多久发']::text[], 'logistics', 'ship_time', 0.91, 80, cs_agent_id, TRUE, NOW(), NOW()),
        ('发什么快递 [seed-034]', '默认发合作快递（中通/圆通/申通），如需其他快递（顺丰/EMS）请备注，顺丰可能需要补运费差价。大件商品默认发物流，有额外运费。', ARRAY['快递', '发什么', '顺丰', '物流']::text[], 'logistics', 'express', 0.9, 70, cs_agent_id, TRUE, NOW(), NOW()),
        ('多少钱 [seed-034]', '这款标价是 XX 元，目前 [有活动/有优惠券/会员价 XX 元]。不同规格价格不同，您可以看下具体选的那款～活动叠加规则详情页有说明哦。', ARRAY['多少钱', '价格', '便宜', '优惠']::text[], 'pre_sales', 'price', 0.9, 120, cs_agent_id, TRUE, NOW(), NOW()),
        ('有什么活动 [seed-034]', '目前进行中的活动：①[活动1] XX 时间截止 ②[活动2] ③新用户首单立减 XX ④会员专享价 XX。具体在首页/活动页可以看到全部，我也可以帮您算到手价～', ARRAY['活动', '优惠', '满减', '打折', '折扣', '促销']::text[], 'pre_sales', 'promotion', 0.89, 90, cs_agent_id, TRUE, NOW(), NOW()),
        ('可以便宜点吗 [seed-034]', '理解您的心情～商品已经是优惠价了！不过我可以帮您看看：①有没有新人券可以领 ②会员有没有额外折扣 ③凑单满减能省多少。您告诉我预算，我帮您算最优方案！', ARRAY['便宜', '砍价', '优惠点', '划算']::text[], 'pre_sales', 'discount_req', 0.85, 60, cs_agent_id, TRUE, NOW(), NOW()),
        ('尺码怎么选 [seed-034]', '选尺码的话麻烦告诉下您的身高体重～我对照尺码表帮您推荐。一般穿正码就好，但 [特殊情况如怀孕/大肚腩] 建议大一码。鞋子还要看脚长和宽～', ARRAY['尺码', '多大', '大小', '穿多大']::text[], 'pre_sales', 'size', 0.93, 200, cs_agent_id, TRUE, NOW(), NOW()),
        ('是什么材质 [seed-034]', '这款商品材质是：[材质类型]，符合 [A类/B类/食品级] 标准，有检测报告。您对材质有特殊要求吗？（如纯棉/透气/环保）我帮您再推荐～', ARRAY['材质', '材料', '面料', '成分']::text[], 'pre_sales', 'material', 0.9, 85, cs_agent_id, TRUE, NOW(), NOW()),
        ('可以退换吗 [seed-034]', '可以的！适用 7 天无理由退换（除贴身内衣/食品/特殊定制），条件是商品完好吊牌未拆。质量问题 15 天内可申请，运费商家承担。具体退换流程点订单的【申请售后】就行～', ARRAY['退换', '退货', '换货', '退款', '7天']::text[], 'after_sales', 'return_policy', 0.95, 300, cs_agent_id, TRUE, NOW(), NOW()),

        -- 售后（10 条）
        ('怎么退款 [seed-034]', '退款流程：①打开订单→点【申请售后】②选【仅退款/退货退款】③填原因+上传凭证（如需）④提交等待审核⑤审核通过后原路返回（1-3 工作日到账）。有问题我可以一步步引导您操作～', ARRAY['退款', '怎么退', '申请退款', '退货流程']::text[], 'after_sales', 'refund_process', 0.93, 150, cs_agent_id, TRUE, NOW(), NOW()),
        ('退款多久到账 [seed-034]', '退款到账时效：①微信/支付宝原路退——1-3 工作日；②花呗/信用卡——3-5 工作日；③账户余额——实时到账。超 5 工作日没到账建议查下支付软件账单，或联系我帮您跟进～', ARRAY['退款', '到账', '多久', '什么时候到']::text[], 'after_sales', 'refund_time', 0.91, 120, cs_agent_id, TRUE, NOW(), NOW()),
        ('运费谁承担 [seed-034]', '退换货运费承担：①质量问题——商家承担（寄回后运费险理赔给您）；②7 天无理由/个人原因——买家承担（或用运费险抵扣首重）；③错发/漏发——商家承担补发运费。运费险自动理赔，到账后可在运费险详情查看。', ARRAY['运费', '谁承担', '运费险', '寄回去']::text[], 'after_sales', 'shipping_cost', 0.92, 90, cs_agent_id, TRUE, NOW(), NOW()),
        ('商品有问题怎么办 [seed-034]', '真的很抱歉！如果收到商品有问题：①拍照/录像留证（外观/功能/质量问题）②在订单申请【质量问题退换】③等待客服审核。质量问题 15 天内可申请，运费商家承担。小额问题也可以协商部分退款～', ARRAY['有问题', '坏了', '质量', '瑕疵', '坏的']::text[], 'after_sales', 'quality_issue', 0.94, 180, cs_agent_id, TRUE, NOW(), NOW()),
        ('已经签收了还能退吗 [seed-034]', '可以的！签收后仍可走 7 天无理由退换（条件：商品完好、吊牌未拆、配件齐全）。签收不影响退换，只是不能拒收了。申请入口在订单详情的【申请售后】～', ARRAY['签收', '收到了', '已签收', '还能退']::text[], 'after_sales', 'return_after_sign', 0.9, 80, cs_agent_id, TRUE, NOW(), NOW()),
        ('不退可以少退点钱吗 [seed-034]', '理解～如果您觉得商品能用但有小瑕疵，我可以帮您申请部分退款（不退货）。具体能退多少要看问题大小，帮您 @售后专员 评估一下哈～', ARRAY['部分退款', '不退货', '退点钱', '补偿']::text[], 'after_sales', 'partial_refund', 0.85, 40, cs_agent_id, TRUE, NOW(), NOW()),
        ('补发流程 [seed-034]', '补发适用于错发/漏发/破损：①拍照留证→联系客服→确认补发方案→补发寄出→提供新单号。补发运费由商家承担。如果库存紧张可能需要等待，会提前通知您～', ARRAY['补发', '再发一份', '重发']::text[], 'after_sales', 'resend', 0.88, 55, cs_agent_id, TRUE, NOW(), NOW()),
        ('超了退换期怎么办 [seed-034]', '超退换期（超过 7 天无理由/15 天质量）后系统无法直接发起退换，但我可以帮您 @售后专员 看看能不能特殊处理一下。非质量问题的话商品有终身技术支持，使用有任何问题都可以联系～', ARRAY['超期', '过期', '过了退换期', '超过时间']::text[], 'after_sales', 'expired', 0.83, 30, cs_agent_id, TRUE, NOW(), NOW()),
        ('发票怎么开 [seed-034]', '发票申请：①下单时可直接备注抬头+税号；②已下单的话在订单详情→【申请发票】里补开；③电子发票直接发邮箱（1-3 工作日）；④纸质发票随单寄或单独寄。抬头/税号信息有误作废重开（当月可改，跨月红冲）。', ARRAY['发票', '开票', '开发票', '抬头']::text[], 'after_sales', 'invoice', 0.9, 60, cs_agent_id, TRUE, NOW(), NOW()),
        ('评价错了能改吗 [seed-034]', '评价提交后平台规则一般不支持直接修改。如果是误操作或想补充，我可以帮您：①在评价下方追加追评（买家追评可改 1 次）；②联系平台申请删除错误评价（特殊情况）。', ARRAY['评价', '改评价', '差评', '追评']::text[], 'after_sales', 'review_fix', 0.82, 25, cs_agent_id, TRUE, NOW(), NOW()),

        -- 物流（8 条）
        ('快递到哪了 [seed-034]', '麻烦给下订单号～我帮您查下物流轨迹。也可以自己在订单详情点【查看物流】实时追踪哦。超 3 天没更新的话建议联系快递公司官网查询～', ARRAY['快递', '物流', '到哪了', '追踪', '查快递']::text[], 'logistics', 'track', 0.93, 250, cs_agent_id, TRUE, NOW(), NOW()),
        ('可以改地址吗 [seed-034]', '①订单未发货——后台直接改，客服也可以帮您操作；②订单已发货——联系快递改址，可能产生改址费，偏远地区无法改；③已派送中——快递员会联系您确认投递地点。建议下单前仔细核对地址哦！', ARRAY['改地址', '地址错了', '换地址', '改收货']::text[], 'logistics', 'change_address', 0.9, 100, cs_agent_id, TRUE, NOW(), NOW()),
        ('快递慢/不更新 [seed-034]', '抱歉让您久等了！常见原因：①大促期间快递爆仓→耐心等待，超 3 天不更新可联系快递官方；②天气/节假日影响时效；③转运中心分拣延迟。您可以把运单号发我，我帮您协助联系快递公司催促。', ARRAY['慢', '不更新', '快递卡', '太慢']::text[], 'logistics', 'slow_delivery', 0.88, 90, cs_agent_id, TRUE, NOW(), NOW()),
        ('快递丢了/破损 [seed-034]', '真的很抱歉！处理方式：①签收时发现破损→拍照→拒收→联系客服补发；②签收后破损/丢失→24 小时内联系客服→提供照片/视频→商家走物流理赔→同时给您补发或退款。我帮您标记加急处理！', ARRAY['丢了', '破损', '坏了', '快递损坏', '丢失']::text[], 'logistics', 'lost_damaged', 0.94, 70, cs_agent_id, TRUE, NOW(), NOW()),
        ('能指定快递吗 [seed-034]', '可以指定，但要注意：①指定顺丰——需要补运费差价（顺丰比普通快递贵 10-20 元/kg）；②指定其他快递——只要是快递可到的区域都可以，但速度可能不同；③大件指定物流——提前联系确认运费和时效。备注时写清楚指定快递公司哦！', ARRAY['指定快递', '要顺丰', '选快递', '指定']::text[], 'logistics', 'specify_express', 0.85, 35, cs_agent_id, TRUE, NOW(), NOW()),
        ('寄到偏远地区可以吗 [seed-034]', '可以的！但偏远地区（新疆/西藏/内蒙古部分）：①部分快递超区无法派送→可能转发邮政/EMS；②时效延长（通常 7-15 天）；③可能产生额外偏远运费。下单后如果快递无法送达会提前通知您协商处理方案的～', ARRAY['偏远', '新疆', '西藏', '内蒙古', '超区']::text[], 'logistics', 'remote', 0.87, 40, cs_agent_id, TRUE, NOW(), NOW()),
        ('可以上门取件吗 [seed-034]', '退换货时支持上门取件（部分城市/快递）：①申请售后选【上门取件】选项→快递员按预约时间上门→取件时给快递单号→寄回等待处理。偏远地区/大件可能不支持，建议先选【自行寄回】把单号填进来～', ARRAY['上门', '取件', '上门取货']::text[], 'logistics', 'home_pickup', 0.84, 30, cs_agent_id, TRUE, NOW(), NOW()),
        ('可以加钱发更快的吗 [seed-034]', '可以！想加快递有几种方式：①加钱发顺丰空运（通常 +15-25 元/kg，次日达）；②发京东/德邦快；③催当前快递（但不一定有用，爆仓时谁都慢）。您告诉我哪个城市我帮您查最快的方式～', ARRAY['加快', '加钱', '更快', '加急']::text[], 'logistics', 'speed_up', 0.83, 25, cs_agent_id, TRUE, NOW(), NOW()),

        -- 活动/会员/通用（12 条）
        ('优惠券怎么用 [seed-034]', '优惠券使用：①下单时在【优惠券】栏点击选择可用券→自动抵扣；②不可叠加的会显示灰色；③注意有效期和使用门槛（满多少可用）；④新人券限首次下单，品类券限特定品类。有问题把优惠券 ID 发我，我帮您看状态～', ARRAY['优惠券', '怎么用', '怎么用券', '满减']::text[], 'pre_sales', 'coupon', 0.92, 180, cs_agent_id, TRUE, NOW(), NOW()),
        ('优惠券哪里领 [seed-034]', '领券渠道：①首页【领券中心】专区；②粉丝群/社群不定期发放；③会员生日专属券；④新用户注册自动送新人券包；⑤活动页/节日页面领取。错过的话可以关注下一波活动，大促期间券会比较多哦～', ARRAY['领券', '优惠券在哪', '怎么领', '领一下']::text[], 'pre_sales', 'coupon_get', 0.89, 140, cs_agent_id, TRUE, NOW(), NOW()),
        ('会员有什么权益 [seed-034]', '会员核心权益：①会员专享价（比普通价低 5-20%）②生日专属礼包（含大额优惠券）③积分翻倍（消费 1 元积 2 分）④优先发货权⑤专属客服通道⑥定期会员专属活动。会员等级越高权益越多（银/金/钻石）～', ARRAY['会员', '会员权益', 'VIP', '有什么好处']::text[], 'general', 'vip_benefit', 0.9, 100, cs_agent_id, TRUE, NOW(), NOW()),
        ('积分怎么用 [seed-034]', '积分使用方式：①下单时抵扣（100 分=1 元，单笔最高抵 30%）；②积分换购商品（积分商城）；③积分兑换优惠券；④积分抽奖/秒杀。积分有效期 1 年，到期自动清零。查积分：个人中心→我的积分～', ARRAY['积分', '怎么用', '积分兑换', '换购']::text[], 'general', 'points', 0.88, 70, cs_agent_id, TRUE, NOW(), NOW()),
        ('怎么成为会员 [seed-034]', '会员入门方式：①消费满 299 自动成为银卡会员；②购买会员年卡（立得 50 元券包 + 全年专享价）；③邀请 3 位好友注册 → 送 1 个月金卡体验。会员等级成长靠消费积累，升级到金卡享更多权益～', ARRAY['怎么成为会员', '开通会员', '成为VIP', '加入会员']::text[], 'general', 'join_vip', 0.87, 55, cs_agent_id, TRUE, NOW(), NOW()),
        ('新人优惠有什么 [seed-034]', '新用户首次注册/下单福利：①新人券包（通常 30-50 元券）②首单立减（满 99 减 20 之类）③首次评价送积分。没收到的话把手机号发我，我帮您查下账号状态～', ARRAY['新人', '新用户', '新人优惠', '首单']::text[], 'pre_sales', 'new_user', 0.89, 85, cs_agent_id, TRUE, NOW(), NOW()),
        ('可以货到付款吗 [seed-034]', '目前暂不支持货到付款，需要在线支付（微信/支付宝/银行卡都可以）。这样能加快发货速度，也更安全可靠～支付遇到问题随时告诉我。', ARRAY['货到付款', '到付', '签收付钱', 'COD']::text[], 'general', 'cod', 0.82, 30, cs_agent_id, TRUE, NOW(), NOW()),
        ('有微信公众号吗 [seed-034]', '有的！关注公众号可以：①接收新活动推送②领专属优惠券③查订单物流④收到发货通知。您在微信搜【xxx】就能找到～粉丝群不定期发秒杀福利哦！', ARRAY['公众号', '微信', '扫码', '关注']::text[], 'general', 'wechat', 0.84, 40, cs_agent_id, TRUE, NOW(), NOW()),
        ('可以开发票吗 [seed-034]', '可以开！电子发票（默认）直接发邮箱，纸质发票需要随单寄或单独寄（单独寄有邮费）。申请方式：下单时备注，或订单详情→【申请发票】补开。抬头/税号错了可以作废重开，当月可改，跨月走红冲流程～', ARRAY['发票', '能开票吗', '开专票']::text[], 'after_sales', 'invoice', 0.88, 50, cs_agent_id, TRUE, NOW(), NOW()),
        ('评价后有奖励吗 [seed-034]', '评价有奖的哦！①订单评价送积分（通常 100-300 分/条）②追评再送一半积分③带图/带视频评价可能额外送优惠券。在【我的评价】可以看到积分到账情况～', ARRAY['评价', '奖励', '送积分', '好评']::text[], 'general', 'review_reward', 0.85, 35, cs_agent_id, TRUE, NOW(), NOW()),
        ('怎么注销账号 [seed-034]', '账号注销流程：①个人中心→设置→隐私→注销账号；②需确认无未完成订单/无进行中退款；③注销后数据全部清除，不可恢复。建议先处理完所有售后～注销后优惠券/积分会清零哦！', ARRAY['注销', '销户', '退出', '删账号']::text[], 'general', 'delete_account', 0.8, 20, cs_agent_id, TRUE, NOW(), NOW()),
        ('我想投诉 [seed-034]', E'非常理解您想投诉的心情！\n\n先帮您梳理下情况可以吗？是关于：\n①商品质量 → 我帮您走质量问题流程 + 补偿方案\n②物流问题 → 帮您催快递 + 申请物流补偿\n③客服态度 → 我帮您转主管处理\n④活动/优惠券问题 → 我帮您核实活动规则\n\n您把订单号和具体情况告诉我，我来跟进处理！', ARRAY['投诉', '差评', '举报', '我要投诉', '生气']::text[], 'general', 'complaint', 0.9, 80, cs_agent_id, TRUE, NOW(), NOW());

        RAISE NOTICE '✓ 通用电商客服 FAQ 总数: %', (SELECT COUNT(*) FROM faq_entries WHERE agent_id = cs_agent_id);
    END IF;

    -- ================================================================
    -- PART 6: 为电商销售混合智能体创建 30+ FAQ
    -- ================================================================
    IF sales_agent_id IS NOT NULL THEN
        RAISE NOTICE '✓ 开始写入电商销售 FAQ，agent_id=%', sales_agent_id;

        INSERT INTO faq_entries (question, answer, keywords, category, intent, confidence, hit_count, agent_id, enabled, created_at, updated_at) VALUES
        -- 种草推荐（10 条）
        ('给我推荐一下 [seed-034]', '好哒～想听听您的需求：①买来送礼还是自用？②预算大概多少？③有什么特别偏好（颜色/材质/品牌）？告诉我这些我帮您精准推荐 2-3 款，每款讲一个独特亮点～', ARRAY['推荐', '给我推荐', '选一个', '什么好']::text[], 'recommend', 'recommend', 0.93, 100, sales_agent_id, TRUE, NOW(), NOW()),
        ('送礼选哪个好 [seed-034]', '送礼的话我给您推荐几款热门款：\n🎁 送女朋友——XX 款（今年爆款，很多人买）\n🎁 送父母——XX 款（实用不花哨）\n🎁 送小孩——XX 款（安全有趣）\n🎁 送客户——XX 款（高端有面）\n\n告诉我送谁、预算多少、想要什么感觉的，我帮您精准锁定！', ARRAY['送礼', '送人', '送朋友', '礼物', '送爸妈']::text[], 'recommend', 'gift', 0.94, 120, sales_agent_id, TRUE, NOW(), NOW()),
        ('这个适合我吗 [seed-034]', '亲，这款能不能用要看您的具体情况～告诉我几个信息：①您的主要使用场景是什么？②对功能有什么硬性要求？③有没有特别在意的点（续航/重量/外观/价格）？我帮您分析这款适不适合您，不合适我再给您推荐别的～', ARRAY['适合吗', '能用吗', '适合我', '行不行']::text[], 'recommend', 'fit', 0.9, 70, sales_agent_id, TRUE, NOW(), NOW()),
        ('这款值得买吗 [seed-034]', '这款的核心价值点：\n✨ 性能：[优势，如续航 48 小时 / 材质更耐用]\n✨ 体验：[使用场景，如出门不怕没电 / 手感特别好]\n✨ 性价比：同价位对比，这款在 [某方面] 领先\n\n如果您在意 [续航/价格/品质] 这些点，这款绝对值得入～告诉我您的核心需求我帮您确认！', ARRAY['值得买', '划算吗', '性价比', '买不买']::text[], 'recommend', 'worth', 0.89, 85, sales_agent_id, TRUE, NOW(), NOW()),
        ('有新款吗 [seed-034]', '有的！刚上新了几款：\n① [新款 A] XX 元，主打 [卖点]\n② [新款 B] XX 元，主打 [卖点]\n③ [新款 C] XX 元，主打 [卖点]\n\n新款有首发价/赠品，感兴趣我帮您发详细介绍～', ARRAY['新款', '新出的', '刚上', '最新']::text[], 'recommend', 'new_arrival', 0.88, 60, sales_agent_id, TRUE, NOW(), NOW()),
        ('有套装吗 [seed-034]', '有的！精选套装组合：\n① 入门套装——A+B，XX 元（比单买省 XX）\n② 进阶套装——A+B+C，XX 元（送赠品 + 延长保修）\n③ 全家桶——全套组合，XX 元（限时 7 折）\n\n套装更划算，告诉我您的预算我帮您搭配最优组合～', ARRAY['套装', '组合', '搭配', '打包']::text[], 'recommend', 'bundle', 0.91, 75, sales_agent_id, TRUE, NOW(), NOW()),
        ('哪个系列最好 [seed-034]', '各系列定位不同：\n🌟 旗舰系列——品质最好、功能最全、价格也最贵，适合追求顶级体验的\n🌟 进阶系列——性价比高、功能够用、价格适中，适合大多数用户\n🌟 入门系列——价格亲民、核心功能齐全、适合预算有限或首次接触\n\n告诉我您的预算和核心需求，我帮您选对系列～', ARRAY['哪个好', '最好', '旗舰', '系列', '选哪个']::text[], 'recommend', 'series', 0.9, 80, sales_agent_id, TRUE, NOW(), NOW()),
        ('有没有对比 [seed-034]', '热门款对比：\n| 对比项 | A 款 | B 款 |\n|--------|------|------|\n| 价格 | XX | XX |\n| 核心卖点 | XX | XX |\n| 适合人群 | XX | XX |\n\n简单说：要 [品质/价格/外观] 选 A；要 [实用/续航/性价比] 选 B。告诉我您最在意什么我帮您选～', ARRAY['对比', '哪个好', '比较', '选哪个', '哪个性价比']::text[], 'recommend', 'compare', 0.92, 110, sales_agent_id, TRUE, NOW(), NOW()),
        ('为什么比别家贵 [seed-034]', '好问题～我们的定价是因为：\n① [品质优势，如用了更好的材料/工艺]\n② [服务优势，如 7 天无理由 + 1 年保修 + 终身技术支持]\n③ [品牌价值]\n\n现在下单还有 XX 活动，折合下来和别家其实差不多，品质和售后却好很多。要不要我给您算下到手价？', ARRAY['贵', '为什么贵', '比别家贵', '别家便宜']::text[], 'objection', 'price_high', 0.88, 95, sales_agent_id, TRUE, NOW(), NOW()),
        ('功能太多用不上 [seed-034]', '理解！功能多确实可能用不上，但：\n① 贵的其实是 [核心功能]，其他功能算附赠的\n② [未来可能用到的功能] 现在用不上，以后就不好补了\n③ 一步到位不用以后升级，反而省钱\n\n如果预算紧张也可以看看入门款，核心功能一样都有～', ARRAY['功能多', '用不上', '太复杂', '简单点']::text[], 'objection', 'too_complex', 0.85, 45, sales_agent_id, TRUE, NOW(), NOW()),

        -- 逼单/促单（10 条）
        ('什么时候有活动 [seed-034]', '近期活动日历：\n📅 本周——[限时活动名称]，XX 时间截止\n📅 下周——[活动名称]\n📅 大促——XX 节预热中，力度会更大\n\n现在这款正在做 [限时折扣/满减/赠品]，错过可能要等下次活动哦！要不要我帮您算到手价？', ARRAY['活动', '什么时候', '下次', '等活动', '促销']::text[], 'promotion', 'when_activity', 0.87, 50, sales_agent_id, TRUE, NOW(), NOW()),
        ('可以等等再买吗 [seed-034]', '理解，不过有几个点您可以考虑：\n⏰ 当前活动 XX 时间就截止了，现在买立省 XX 元\n📦 目前这款只剩 XX 件，售完恢复原价\n🎁 现在下单还送 [赠品]，活动后就没了\n\n建议您现在下手，真的划算！犹豫久了可能活动就没了～', ARRAY['等等', '不急', '再看看', '不着急']::text[], 'promotion', 'wait_buy', 0.86, 55, sales_agent_id, TRUE, NOW(), NOW()),
        ('现在买有什么好处 [seed-034]', '现在买的专属福利：\n① 活动价 XX（比平时便宜 XX 元）\n② 送 [赠品名称]（价值 XX 元）\n③ 优先发货（不用等预售）\n④ 加赠 [优惠券/积分]\n\n活动就剩 XX 小时了，真的是最后一波～', ARRAY['现在买', '现在下单', '立即下单', '现在好处']::text[], 'promotion', 'now_buy', 0.9, 90, sales_agent_id, TRUE, NOW(), NOW()),
        ('库存还有多少 [seed-034]', '这款目前库存：\n🌟 热门色号——只剩 XX 件！\n📦 其他色号——还有 XX 件\n\n热门色经常断货，建议看中就下手～我帮您标记一下加急发货！', ARRAY['库存', '还有多少', '剩多少', '限量']::text[], 'promotion', 'stock_pressure', 0.85, 40, sales_agent_id, TRUE, NOW(), NOW()),
        ('今天下单送什么 [seed-034]', '今天下单的专属赠品：\n🎁 [赠品 A]——价值 XX 元\n🎁 [赠品 B]——价值 XX 元\n💳 加赠 [XX 优惠券]\n\n赠品随机还是指定款？告诉我您的偏好我帮您选～', ARRAY['送什么', '赠品', '今天下单', '礼包']::text[], 'promotion', 'gift', 0.88, 65, sales_agent_id, TRUE, NOW(), NOW()),
        ('能留个吗 [seed-034]', '可以帮您标记"意向客户"优先发货，但库存不能长期预留哦～您今天能下单的话我帮您备注 VIP 优先处理！错过的话可能要等补货了～', ARRAY['留一个', '预留', '帮我留', '先留着']::text[], 'promotion', 'reserve', 0.83, 25, sales_agent_id, TRUE, NOW(), NOW()),
        ('会员有特价吗 [seed-034]', '会员专享价：\n🌟 银卡会员——立减 XX\n🌟 金卡会员——立减 XX + 双倍积分\n🌟 钻石会员——立减 XX + 三倍积分 + 免运费\n\n现在开通年卡会员，这笔订单直接省更多！我帮您算下最优组合价？', ARRAY['会员', 'VIP', '特价', '专属价', '会员价']::text[], 'promotion', 'vip_price', 0.87, 70, sales_agent_id, TRUE, NOW(), NOW()),
        ('可以分期吗 [seed-034]', '可以分期！支持的分期方式：\n💳 花呗——3/6/12 期 0 手续费\n💳 信用卡——分期手续费 3-6%\n💳 白条——3/6/12 期免息\n\n算下来每月只需要 XX 元，轻松入手～', ARRAY['分期', '月供', '分期付款', '花呗']::text[], 'promotion', 'installment', 0.86, 55, sales_agent_id, TRUE, NOW(), NOW()),
        ('能帮我凑满减吗 [seed-034]', '好的！帮您算凑满减最优方案：\n\n您预算 XX，建议：\n① 加 [X 款 XX 元] → 凑到满减门槛\n② 这样能减 XX 元 → 实际省 XX\n③ 还能顺便凑到包邮条件\n\n要不要我帮您列个具体清单？', ARRAY['凑满减', '满减', '凑单', '怎么凑']::text[], 'promotion', 'bundle_help', 0.91, 100, sales_agent_id, TRUE, NOW(), NOW()),
        ('再送点什么呗 [seed-034]', '理解～不过活动规则里的赠品已经是最大力度了。我帮您申请一下：\n① 能不能多送 [XX]\n② 有没有额外的优惠券\n③ 帮您标记 VIP，下次来直接走绿色通道\n\n您告诉我具体想要什么，我尽量帮您争取！', ARRAY['再送', '多点', '再加', '送两个']::text[], 'objection', 'more_gift', 0.82, 30, sales_agent_id, TRUE, NOW(), NOW()),

        -- 复购/跟进（8 条）
        ('上次买过能优惠吗 [seed-034]', '当然！老客户专属福利：\n① 老客户复购立减 XX 元\n② 专属老客优惠券 XX\n③ 加赠老客积分\n\n您告诉我上次买的是哪款，我帮您算这次买什么搭配最划算～', ARRAY['老客户', '复购', '回头客', '上次买过']::text[], 'followup', 'repeat', 0.89, 60, sales_agent_id, TRUE, NOW(), NOW()),
        ('这个和我上次的搭配好吗 [seed-034]', '好问题！您上次买的是 [XX]，这次看的 [XX]：\n✅ 可以搭配使用，效果更好\n✅ 同系列，风格统一\n✅ 搭配购买还有组合价，比单买省 XX 元\n\n建议您一起入，体验更完整～', ARRAY['搭配', '配合', '一起买', '组合用']::text[], 'followup', 'match', 0.88, 50, sales_agent_id, TRUE, NOW(), NOW()),
        ('用了挺好用的想再买 [seed-034]', '哈哈能帮到您真开心！🎉 复购福利：\n① 老客专属复购券 XX\n② 加赠老客积分翻倍\n③ 推荐好友也能拿奖励～\n\n这次是自己用还是分享给朋友？我帮您看看更划算的组合～', ARRAY['好用', '想再买', '复购', '接着买']::text[], 'followup', 'positive_follow', 0.92, 80, sales_agent_id, TRUE, NOW(), NOW()),
        ('这个有配套的吗 [seed-034]', '有的！周边配套：\n① [配套 A]——XX 元，作用 XX\n② [配套 B]——XX 元，作用 XX\n③ [套餐]——A+B 打包价 XX（省 XX）\n\n推荐买套餐，更划算～要不要我帮您加进购物车？', ARRAY['配套', '搭配', '周边', '有配件吗']::text[], 'followup', 'accessory', 0.87, 45, sales_agent_id, TRUE, NOW(), NOW()),
        ('下次有新的通知我 [seed-034]', '好的！建议您：\n① 关注公众号【XX】——第一时间收到新品推送\n② 加粉丝群【XX】——群里不定期发专属优惠和提前购\n③ 我帮您标记"喜欢新品"，下次有相关活动提醒您\n\n留个联系方式，有新货我帮您留意～', ARRAY['通知', '提醒', '告诉', '下次有']::text[], 'followup', 'notify', 0.86, 40, sales_agent_id, TRUE, NOW(), NOW()),
        ('有推荐朋友的奖励吗 [seed-034]', '有的！推荐好友双份福利：\n① 您推荐的好友——首单立减 XX\n② 您自己——好友下单后送 XX 积分 + XX 优惠券\n\n邀请越多奖励越多！在【我的→邀请好友】里有专属链接～', ARRAY['推荐', '邀请', '介绍朋友', '分享']::text[], 'followup', 'referral', 0.88, 55, sales_agent_id, TRUE, NOW(), NOW()),
        ('想升级/换代 [seed-034]', '升级方案：\n① 旧款抵价——按购买价 XX% 抵扣换新款\n② 直接补差——补差价换新款\n③ 以旧换新活动——节日期间力度更大\n\n告诉我您现在用的是哪款、想换哪款，我帮您算最优方案！', ARRAY['升级', '换代', '以旧换新', '老款换新']::text[], 'followup', 'upgrade', 0.85, 35, sales_agent_id, TRUE, NOW(), NOW()),
        ('这次再买点什么 [seed-034]', '根据您之前的购买记录，这次推荐：\n💡 [搭配 A]——配合您之前买的 XX 一起用效果翻倍\n💡 [补充 B]——上次没买但很实用的补充款\n💡 [套装 C]——现在有满减，比单买划算 XX\n\n预算多少？我帮您精准挑几个最合适的！', ARRAY['再买点', '加购', '凑单', '还能买']::text[], 'followup', 'cross_sell', 0.89, 65, sales_agent_id, TRUE, NOW(), NOW());

        RAISE NOTICE '✓ 电商销售 FAQ 总数: %', (SELECT COUNT(*) FROM faq_entries WHERE agent_id = sales_agent_id);
    END IF;

    -- ================================================================
    -- PART 7: 为通用社群运营智能体创建 25+ FAQ
    -- ================================================================
    IF community_agent_id IS NOT NULL THEN
        RAISE NOTICE '✓ 开始写入社群运营 FAQ，agent_id=%', community_agent_id;

        INSERT INTO faq_entries (question, answer, keywords, category, intent, confidence, hit_count, agent_id, enabled, created_at, updated_at) VALUES
        -- 新人（6 条）
        ('我刚入群 [seed-034]', '欢迎欢迎～👏 新伙伴快来看群规哦：① 禁止广告/外链 ② 友好交流 ③ 不刷屏 ④ 有问题 @管理员 。本周活动预告：[活动名称]。有任何问题随时问我或 @管理员～', ARRAY['入群', '刚进', '新人', '新来的']::text[], 'newbie', 'new_join', 0.95, 200, community_agent_id, TRUE, NOW(), NOW()),
        ('有什么活动 [seed-034]', '近期社群活动：\n📅 [日期] [活动 1]——参与方式 XX\n📅 [日期] [活动 2]——参与方式 XX\n🎁 本周福利：关注公众号领 XX 优惠券\n\n更多活动看群公告哦～记得置顶群消息！', ARRAY['活动', '福利', '抽奖', '发奖品', '红包']::text[], 'activity', 'activity', 0.93, 150, community_agent_id, TRUE, NOW(), NOW()),
        ('我发广告了怎么办 [seed-034]', '理解您可能不太熟悉群规～第一次违规会提醒：请撤回广告内容并说明用途。如果确实是相关内容（如您买的商品心得）可以改成纯文字分享。多次违规会被移出群哦，请自觉遵守群规～', ARRAY['广告', '发了广告', '违规', '被踢']::text[], 'rule', 'advertisement', 0.85, 60, community_agent_id, TRUE, NOW(), NOW()),
        ('怎么发群红包 [seed-034]', '发群红包步骤：① 群聊天框点【+】② 选【红包】③ 输入金额和个数（拼手气/普通）④ 留言（可选，如"感谢大家一直以来的支持！"）⑤ 确认发送。发红包是感谢社群成员的好方式，但不要代替专业问题解答哦～', ARRAY['红包', '发红包', '怎么发']::text[], 'activity', 'red_packet', 0.9, 80, community_agent_id, TRUE, NOW(), NOW()),
        ('想加好友 [seed-034]', '群内成员互相加好友可以的！温馨提醒：① 不要批量加陌生人（容易被微信标记骚扰）② 加的时候说一句"我是 XX 群的 XX"让对方知道来源 ③ 加了好友后不要立刻发广告，先建立联系。', ARRAY['加好友', '私聊', '加微信']::text[], 'rule', 'add_friend', 0.85, 35, community_agent_id, TRUE, NOW(), NOW()),
        ('群里发言有什么规则 [seed-034]', E'社群公约：\n\n✅ 可以的：\n· 分享产品使用心得/真实评价\n· 问问题、互相帮忙\n· 发起话题讨论\n· 参与活动报名\n\n❌ 不可以的：\n· 发广告/外链/二维码\n· 刷屏/恶意灌水\n· 人身攻击/造谣\n· 私加好友后骚扰\n\n违规第一次提醒，二次移出～', ARRAY['群规', '规则', '公约', '注意什么', '怎么发言']::text[], 'rule', 'group_rule', 0.92, 180, community_agent_id, TRUE, NOW(), NOW()),

        -- 活动/运营（8 条）
        ('这周有什么活动 [seed-034]', '本周精彩活动：\n\n🎉 [活动名]——日期 XX，内容 XX，参与方式 XX\n🎁 福利时间——XX\n👑 本周之星——表扬 [XX] 的积极分享\n\n记得常来群里看看哦，错过了可惜～', ARRAY['这周', '本周', '活动', '这周干嘛']::text[], 'activity', 'weekly_activity', 0.9, 100, community_agent_id, TRUE, NOW(), NOW()),
        ('活动怎么参加 [seed-034]', '参与方式：① 看群公告找到活动详情 ② 按要求报名（通常群内接龙/填表单）③ 等管理员确认名单 ④ 按时参加。报名截止日期和要求看公告里的说明哦～有问题 @管理员！', ARRAY['参加', '怎么参加', '报名', '参加活动']::text[], 'activity', 'join_activity', 0.88, 55, community_agent_id, TRUE, NOW(), NOW()),
        ('我中奖了吗 [seed-034]', '中奖名单会在活动截止后 X 小时内公布在群里（@管理员 操作）。发奖需要您提供收货信息（私聊管理员，注意保护隐私）。如果中奖后超过 3 天没提供收货信息视为自动放弃哦～', ARRAY['中奖', '中了吗', '抽奖结果', '获奖']::text[], 'activity', 'lottery_result', 0.89, 70, community_agent_id, TRUE, NOW(), NOW()),
        ('积分怎么换 [seed-034]', '积分兑换方式：①【个人中心→积分商城】选商品→确认兑换 ②兑换后显示消耗积分和剩余积分③实物奖品包邮发出（通常 3-5 天到），虚拟奖品自动到账。积分每年 12 月底清零，尽快兑换哦～', ARRAY['积分', '换积分', '兑换', '积分商城']::text[], 'reward', 'points_exchange', 0.9, 85, community_agent_id, TRUE, NOW(), NOW()),
        ('怎么赚积分 [seed-034]', '积分获取方式：\n① 群内活跃发言 +10 分/次（每天上限 50 分）\n② 参与活动 +50 分/次\n③ 分享产品心得 +100 分/篇（加精翻倍）\n④ 推荐好友入群 +200 分/人\n⑤ 生日当天登录 +500 分\n\n积分每年清零，记得尽快兑换哦～', ARRAY['积分', '怎么赚', '获取积分', '加分', '赚积分']::text[], 'reward', 'earn_points', 0.89, 95, community_agent_id, TRUE, NOW(), NOW()),
        ('这个活动是真的吗 [seed-034]', '活动真实性：① 正规活动都会在群公告/公众号同步 ② 官方活动不会让你转账/加陌生群 ③ 警惕"内部名额""仅群内""限时免费"等诈骗话术。不确定可以 @管理员核实，保护好自己的财产安全哦！', ARRAY['真的吗', '真假', '骗局', '诈骗', '安全']::text[], 'safety', 'verify', 0.92, 80, community_agent_id, TRUE, NOW(), NOW()),
        ('能帮忙宣传我的群吗 [seed-034]', '抱歉哦，本群是品牌官方社群，不接受外部群/广告宣传。如果你有相关产品心得可以分享在群里，纯分享不带广告是可以的～想推广自己的群建议找合作渠道或公众号投稿哦！', ARRAY['宣传', '推广', '拉人', '加群']::text[], 'rule', 'promo_request', 0.82, 25, community_agent_id, TRUE, NOW(), NOW()),
        ('群里发的链接安全吗 [seed-034]', '链接安全提醒：① 官方活动链接一般是 weixin.qq.com / xxx.com 官方域名 ② 不要点陌生短链接、下载未知 APP ③ 看到可疑链接请 @管理员 核实 ④ 保护好个人信息和支付密码！有安全问题随时反馈～', ARRAY['链接', '安全吗', '安全', '恶意链接']::text[], 'safety', 'link_safety', 0.91, 70, community_agent_id, TRUE, NOW(), NOW()),

        -- 社群运营/氛围（6 条）
        ('怎么让群更活跃 [seed-034]', '提升社群活跃度的小建议：\n① 定期发起话题（如"大家周末都干嘛呀"）\n② 每周搞个小投票\n③ 每月选一个活跃之星\n④ 节日搞点小活动\n⑤ 成员的好消息及时表扬\n\n有运营问题可以和 @管理员 聊，社群需要大家一起维护～', ARRAY['活跃', '怎么活跃', '活跃度', '没人说话']::text[], 'operation', 'activity_tip', 0.87, 40, community_agent_id, TRUE, NOW(), NOW()),
        ('我想当管理员 [seed-034]', '感谢您的热情！管理员选拔标准：① 群内活跃（发言/参与活动频率高）② 守规守纪（从未违规）③ 有服务精神（愿意帮大家解答问题）④ 时间充裕（每天能在线 1 小时以上）。感兴趣可以私信 @群主 报名～', ARRAY['管理员', '当管理员', '报名管理员', '想当']::text[], 'operation', 'admin_apply', 0.85, 35, community_agent_id, TRUE, NOW(), NOW()),
        ('有人吵架怎么办 [seed-034]', '群内有人争执：① 先劝和——"大家都是朋友嘛，有话好好说～"② 严重争执 @管理员 介入 ③ 不要加入战团，保持中立 ④ 如果有人人身攻击提醒他违反群规。群里是温暖的大家庭，互相尊重哈！', ARRAY['吵架', '争执', '打架', '对骂', '吵架了']::text[], 'operation', 'fight', 0.9, 50, community_agent_id, TRUE, NOW(), NOW()),
        ('被群成员骚扰了 [seed-034]', '被骚扰处理：① 截图/保存证据 ② @管理员 说明情况 ③ 严重骚扰可拉黑 ④ 多次违规骚扰者会被移出群。保护好自己！也建议私聊时注意隐私，不要轻易加陌生人为好友。', ARRAY['骚扰', '被骚扰', '烦', '拉黑']::text[], 'safety', 'harassment', 0.93, 40, community_agent_id, TRUE, NOW(), NOW()),
        ('能帮我@大家吗 [seed-034]', '@全体成员只有群主/管理员可以操作，用于紧急通知或重要公告。日常话题@一下特定的人就好啦～想通知大家可以私信我或 @管理员 ，看情况帮您协调！', ARRAY['@所有人', '@大家', '全员', '通知全员']::text[], 'rule', 'mention_all', 0.82, 20, community_agent_id, TRUE, NOW(), NOW()),
        ('怎么退出群 [seed-034]', '退群步骤：① 群聊右上角菜单② 滑到最底部选【删除并退出】。退群后积分会清零哦，记得先兑换。如果是被骚扰/有问题，可以 @管理员 反映情况，看能不能解决～我们希望每位成员都开心！', ARRAY['退出', '退群', '不想在这', '退出群聊']::text[], 'operation', 'leave_group', 0.86, 30, community_agent_id, TRUE, NOW(), NOW()),

        -- 投诉/反馈/通用（5 条）
        ('我要投诉 [seed-034]', '先别着急～告诉我具体情况：是遇到什么问题了？是关于活动/群成员/管理员/商品/还是其他？我会帮您记录下来，转给对应负责人处理。也可以私信 @群主 沟通～', ARRAY['投诉', '抱怨', '我要投诉', '生气了']::text[], 'general', 'complaint', 0.93, 60, community_agent_id, TRUE, NOW(), NOW()),
        ('建议可以提吗 [seed-034]', '当然欢迎！您的建议对我们非常重要：① 群运营方式 ② 活动内容 ③ 福利形式 ④ 产品改进 ⑤ 其他任何想法。私信 @群主 或者直接在群里说都可以，我们都会认真考虑的！', ARRAY['建议', '意见', '可以提吗', '想法']::text[], 'general', 'feedback', 0.9, 75, community_agent_id, TRUE, NOW(), NOW()),
        ('为什么被踢了 [seed-034]', '被移出群通常是违反了群规：① 发广告/外链 ② 多次刷屏 ③ 人身攻击 ④ 私加好友骚扰 ⑤ 诈骗行为。如果是误判可以私信 @群主 说明情况，申请重新入群。以后注意守群规哦～', ARRAY['被踢', '踢出去', '为什么被踢', '移出群']::text[], 'rule', 'kicked', 0.88, 45, community_agent_id, TRUE, NOW(), NOW()),
        ('有福利吗 [seed-034]', '当前社群福利：\n🎁 新人入群礼——首次入群领 XX\n🎉 每周三秒杀——群内专属价 XX\n💬 活跃之星——每月表扬并送积分\n🎂 生日专属礼——当天入群/发言有特别福利\n\n记得置顶群消息，不错过任何优惠！', ARRAY['福利', '优惠', '好处', '有啥福利', '优惠吗']::text[], 'activity', 'welfare', 0.9, 90, community_agent_id, TRUE, NOW(), NOW()),
        ('群里说的优惠真的有吗 [seed-034]', '群内优惠都是真实有效的！① 官方活动会在群公告和公众号同步② 有效期/使用限制看活动说明③ 有疑问可以 @管理员 核实④ 注意保护自己，不要转账给陌生人。真优惠不会让你先交钱的哦！', ARRAY['真的吗', '真假', '是真的吗', '靠谱吗']::text[], 'general', 'verify_promo', 0.89, 55, community_agent_id, TRUE, NOW(), NOW());

        RAISE NOTICE '✓ 社群运营 FAQ 总数: %', (SELECT COUNT(*) FROM faq_entries WHERE agent_id = community_agent_id);
    END IF;

    -- ================================================================
    -- PART 8: 为各智能体创建 SOP 模板（销售话术 / 社群引导 / HiveMTK 介绍）
    -- ================================================================

    -- SOP 1: HiveMTK 官方智能体（补齐 SOP + 绑定 agent_id）
    IF hivemtk_agent_id IS NOT NULL THEN
        -- 为已有的 sop_templates（seedTag 开头）绑定 agent_id
        UPDATE sop_templates SET agent_id = hivemtk_agent_id WHERE name LIKE '%HiveMTK%' AND agent_id IS NULL;

        -- 补充更多销售/选型 SOP
        INSERT INTO sop_templates (name, intent, stage, template, vars, priority, confidence, agent_id, enabled, hit_count, created_at, updated_at) VALUES
        (
            'HiveMTK 选型引导 [seed-034]',
            'overview',
            'middle',
            '为了帮您推荐最合适的方案，我想了解下：您是个人试用还是团队部署？对数据安全有没有硬性要求（如数据不出域）？主要想解决什么问题（客服自动化/主动营销/渠道打通）？我根据您的需求给出具体建议。',
            '{}', 90, 0.9, hivemtk_agent_id, TRUE, 30, NOW(), NOW()
        ),
        (
            '竞品差异柔性引导 [seed-034]',
            'overview',
            'objection',
            '您提到的 Dify/Coze 确实很好用，不过两者定位不太一样：Dify 偏通用工作流编排（像写 if-then-else 流程图）；HiveMTK 偏私域营销自动化（七端原生接入 + 数据不出域 + ReAct 自主智能体）。如果您的核心诉求是私域 + 数据安全 + 多渠道，HiveMTK 会更贴合～',
            '{}', 88, 0.92, hivemtk_agent_id, TRUE, 25, NOW(), NOW()
        ),
        (
            '快速上手引导 [seed-034]',
            'deploy',
            'closing',
            '最快上手方式：\n1️⃣ git clone https://gitee.com/xhpmayun/hivemtk.git\n2️⃣ make install\n3️⃣ vim .env 改 4 个密钥（JWT_SECRET/DB密码等）\n4️⃣ make inference-host-up（启动本地推理栈）\n5️⃣ make up 启动全服务\n6️⃣ 浏览器访问 http://localhost:8204/setup → 完成初始化\n\n5 分钟跑通最小闭环！遇到问题随时问我～',
            '{}', 95, 0.93, hivemtk_agent_id, TRUE, 40, NOW(), NOW()
        );

        RAISE NOTICE '✓ HiveMTK SOP 总数: %', (SELECT COUNT(*) FROM sop_templates WHERE agent_id = hivemtk_agent_id);
    END IF;

    -- SOP 2: 电商销售混合智能体（销售话术 SOP）
    IF sales_agent_id IS NOT NULL THEN
        INSERT INTO sop_templates (name, intent, stage, template, vars, priority, confidence, agent_id, enabled, hit_count, created_at, updated_at) VALUES
        -- 开场推荐
        ('电商推荐开场 [seed-034]', 'recommend', 'initial', '您好呀～想听听您的需求：①买来送礼还是自用？②预算大概多少？③有什么特别偏好吗？告诉我这些我帮您精准推荐 2-3 款，每款讲一个独特亮点～', '{}', 95, 0.92, sales_agent_id, TRUE, 60, NOW(), NOW()),
        ('送礼场景开场 [seed-034]', 'recommend', 'initial', '送礼的话我给您精选几款热门款：🎁 送女朋友/男朋友——XX；🎁 送父母——XX；🎁 送孩子——XX；🎁 送领导——XX。告诉我送谁、预算多少，我帮您精准锁定！', '{}', 93, 0.94, sales_agent_id, TRUE, 75, NOW(), NOW()),
        -- 异议处理
        ('价格异议处理 [seed-034]', 'objection', 'objection', '好问题～这款贵是因为：①用了更好的材料/工艺 ②带 [XX 售后服务] ③同价位里 [XX 方面] 最好。现在下单还有活动，折合下来和别家差不多，但品质和售后好很多。要不要我算下到手价？', '{}', 92, 0.9, sales_agent_id, TRUE, 55, NOW(), NOW()),
        ('效果疑虑处理 [seed-034]', 'objection', 'objection', '理解您的顾虑～这款的实际效果可以从：①用户评价里的真实反馈（XX 平台 XX 条好评）②我帮您发几张真实使用图 ③7 天无理由退换，不好可以退。告诉我您最担心什么，我针对性解答～', '{}', 88, 0.88, sales_agent_id, TRUE, 40, NOW(), NOW()),
        -- 逼单话术
        ('限时逼单 [seed-034]', 'promotion', 'closing', '这款目前限时活动！⏰ 还有 XX 小时就结束了～现在下单立省 XX 元，还送 [XX 赠品]。活动后恢复原价，错过可能要等下次大促哦！要帮您标记加急发货吗？', '{}', 94, 0.91, sales_agent_id, TRUE, 80, NOW(), NOW()),
        ('库存逼单 [seed-034]', 'promotion', 'closing', '这款目前只剩 XX 件了！🔥 热门色号经常断货，上次XX就抢不到等了3天。现在下单我帮您标记 VIP 优先处理！', '{}', 90, 0.87, sales_agent_id, TRUE, 45, NOW(), NOW()),
        -- 复购跟进
        ('复购推荐 [seed-034]', 'followup', 'late', '好开心您用得满意！🎉 老客户专属：①复购立减 XX 元 ②老客积分翻倍 ③新品优先体验。根据您上次的购买记录，这次推荐 [XX] 搭配着用效果更好，还能凑组合价！', '{}', 88, 0.89, sales_agent_id, TRUE, 35, NOW(), NOW()),
        -- 凑单帮算
        ('凑满减帮算 [seed-034]', 'promotion', 'middle', '好的！帮您算最优满减方案：\n您当前购物车 [XX] 元，差 [XX] 元到满减门槛。建议加 [XX] 款 [XX] 元 → 凑满 → 减 [XX] → 实际省 [XX]。这样最划算～要不要我帮您确认？', '{}', 91, 0.92, sales_agent_id, TRUE, 50, NOW(), NOW());

        RAISE NOTICE '✓ 电商销售 SOP 总数: %', (SELECT COUNT(*) FROM sop_templates WHERE agent_id = sales_agent_id);
    END IF;

    -- SOP 3: 社群运营智能体（社群引导 SOP）
    IF community_agent_id IS NOT NULL THEN
        INSERT INTO sop_templates (name, intent, stage, template, vars, priority, confidence, agent_id, enabled, hit_count, created_at, updated_at) VALUES
        ('新人入群欢迎 [seed-034]', 'newbie', 'initial', '欢迎欢迎～👏 新伙伴快来看群规哦：\n① 禁止广告/外链\n② 友好交流不吵架\n③ 有问题 @管理员\n\n📅 本周活动预告：[活动名]\n🎁 新人礼：[福利]\n\n有任何问题随时问我或 @管理员～', '{}', 95, 0.95, community_agent_id, TRUE, 100, NOW(), NOW()),
        ('活动通知模板 [seed-034]', 'activity', 'late', '📢 重要活动通知！\n\n🎉 [活动名称]\n⏰ 时间：XX 月 XX 日 XX 时\n📍 地点/形式：线上 XX\n🎁 奖品：[XX]\n\n参与方式：群内接龙或私信报名。错过等下次哦～', '{}', 92, 0.9, community_agent_id, TRUE, 60, NOW(), NOW()),
        ('投诉安抚 SOP [seed-034]', 'general', 'objection', E'非常理解您的心情 😢\n\n先帮您梳理下情况可以吗？是关于：\n① 活动没中奖？\n② 商品/物流问题？\n③ 其他群成员的问题？\n\n您告诉我具体情况，我帮您跟进处理！也可以私信 @群主 沟通～', '{}', 93, 0.94, community_agent_id, TRUE, 45, NOW(), NOW()),
        ('违规温和提醒 [seed-034]', 'rule', 'objection', '温馨提醒～咱们群里不发广告/外链哦，否则会被移出群。如果您是想分享产品使用心得，可以改成纯文字分享，大家会喜欢的～有问题随时 @管理员～', '{}', 85, 0.88, community_agent_id, TRUE, 55, NOW(), NOW()),
        ('活跃话题引导 [seed-034]', 'operation', 'initial', '💬 发起个小话题：大家最近都在用啥？有什么好东西推荐吗？分享你最近发现的好物，精彩分享有积分奖励哦～', '{}', 88, 0.85, community_agent_id, TRUE, 30, NOW(), NOW()),
        ('转人工引导 [seed-034]', 'general', 'closing', '这个问题我帮您 @管理员 来处理～管理员会尽快回复您。也可以私信 @群主 沟通更详细的情况。感谢您的耐心等待！', '{}', 87, 0.9, community_agent_id, TRUE, 40, NOW(), NOW());

        RAISE NOTICE '✓ 社群运营 SOP 总数: %', (SELECT COUNT(*) FROM sop_templates WHERE agent_id = community_agent_id);
    END IF;

    -- ================================================================
    -- PART 9: 通用电商客服 SOP
    -- ================================================================
    IF cs_agent_id IS NOT NULL THEN
        INSERT INTO sop_templates (name, intent, stage, template, vars, priority, confidence, agent_id, enabled, hit_count, created_at, updated_at) VALUES
        -- 售前引导
        ('售前推荐开场 [seed-034]', 'pre_sales', 'initial', '您好！请问有什么可以帮您的？如果需要推荐商品，可以告诉我：①想买什么品类 ②预算多少 ③送礼还是自用？我帮您精准推荐～', '{}', 93, 0.92, cs_agent_id, TRUE, 120, NOW(), NOW()),
        ('售前场景澄清 [seed-034]', 'pre_sales', 'middle', '为了帮您推荐最适合的：请问使用场景是什么（日常/送礼/办公）？对材质/品牌有偏好吗？预算大概多少？告诉我这些我帮您缩小范围～', '{}', 88, 0.9, cs_agent_id, TRUE, 80, NOW(), NOW()),
        -- 售后安抚
        ('售后致歉 SOP [seed-034]', 'after_sales', 'objection', '真的非常抱歉给您带来不好的体验 😢 我帮您处理！方便说下具体是什么问题吗（质量/物流/其他）？发我订单号我帮您走售后流程～', '{}', 94, 0.95, cs_agent_id, TRUE, 150, NOW(), NOW()),
        ('退换货流程引导 [seed-034]', 'after_sales', 'middle', '退换货步骤：\n① 打开订单 → 点【申请售后】\n② 选【仅退款/退货退款】→ 填原因\n③ 上传凭证（质量问题需照片）\n④ 提交等待审核 → 审核通过寄回\n⑤ 验收入库后退款（1-3 工作日到账）\n\n需要我一步步引导您吗？', '{}', 91, 0.93, cs_agent_id, TRUE, 100, NOW(), NOW()),
        -- 物流查询
        ('物流查询引导 [seed-034]', 'logistics', 'middle', '麻烦给下订单号/运单号～我帮您查物流轨迹。也可以在订单详情点【查看物流】自行追踪。超 3 天没更新可以 @快递公司 或让我帮您协助联系！', '{}', 92, 0.91, cs_agent_id, TRUE, 200, NOW(), NOW()),
        -- 活动/优惠券
        ('优惠券使用引导 [seed-034]', 'pre_sales', 'middle', '优惠券使用：① 下单时选可用券 ② 注意门槛和有效期 ③ 部分活动不叠加。您告诉我手里有什么券、想买什么，我帮您算最优方案～', '{}', 89, 0.88, cs_agent_id, TRUE, 140, NOW(), NOW()),
        -- 转人工
        ('转人工引导 [seed-034]', 'general', 'closing', '这个问题我帮您 @人工客服 来处理～稍等，马上帮您接入！也可以告诉我您的联系方式，客服处理完联系您～', '{}', 90, 0.9, cs_agent_id, TRUE, 80, NOW(), NOW()),
        -- 价格异议
        ('价格异议应对 [seed-034]', 'pre_sales', 'objection', '理解您的心情～我帮您看看有没有更划算的方式：① 新人券/优惠券 ② 会员折扣 ③ 凑单满减。您告诉我预算，我帮您算到手价最优方案！', '{}', 85, 0.87, cs_agent_id, TRUE, 70, NOW(), NOW());

        RAISE NOTICE '✓ 通用电商客服 SOP 总数: %', (SELECT COUNT(*) FROM sop_templates WHERE agent_id = cs_agent_id);
    END IF;

    -- ================================================================
    -- 最终统计
    -- ================================================================
    RAISE NOTICE '================== 034 迁移统计 ==================';
    RAISE NOTICE 'FAQ 总数: %', (SELECT COUNT(*) FROM faq_entries WHERE agent_id IS NOT NULL);
    RAISE NOTICE 'SOP 总数: %', (SELECT COUNT(*) FROM sop_templates WHERE agent_id IS NOT NULL);
    RAISE NOTICE 'HiveMTK 智能体 FAQ: %', (SELECT COUNT(*) FROM faq_entries WHERE agent_id = hivemtk_agent_id);
    RAISE NOTICE '电商客服 FAQ: %', (SELECT COUNT(*) FROM faq_entries WHERE agent_id = cs_agent_id);
    RAISE NOTICE '电商销售 FAQ: %', (SELECT COUNT(*) FROM faq_entries WHERE agent_id = sales_agent_id);
    RAISE NOTICE '社群运营 FAQ: %', (SELECT COUNT(*) FROM faq_entries WHERE agent_id = community_agent_id);
    RAISE NOTICE '==================================================';
END $$;

-- ============================================================
-- PART 10: 更新 7 个行业智能体的描述（去掉虚假的 "500+ 组 Q&A"）
-- 并给它们也绑定通用电商 RAG 知识库（作为 Layer2 兜底）
-- ============================================================
UPDATE ai_agents SET
    description = '电子烟行业智能客服，覆盖产品咨询、法规合规、使用指导、故障排查、售后维修等场景。绑定通用电商 RAG 知识库作为 Layer2 兜底。',
    rag_product_ids = ARRAY['ecommerce-general-cs']::text[],
    updated_at = NOW()
WHERE agent_code = 'hivemtk-agent-ecig';

UPDATE ai_agents SET
    description = '成人用品行业智能客服，覆盖产品咨询、材质安全、使用指导、隐私配送、售后退换等场景。绑定通用电商 RAG 知识库作为 Layer2 兜底。',
    rag_product_ids = ARRAY['ecommerce-general-cs']::text[],
    updated_at = NOW()
WHERE agent_code = 'hivemtk-agent-adult';

UPDATE ai_agents SET
    description = '两性健康行业智能客服，覆盖健康咨询、产品推荐、健康指导、隐私保护等场景。绑定通用电商 RAG 知识库作为 Layer2 兜底。',
    rag_product_ids = ARRAY['ecommerce-general-cs']::text[],
    updated_at = NOW()
WHERE agent_code = 'hivemtk-agent-sexhealth';

UPDATE ai_agents SET
    description = '租车行业智能客服，覆盖预订咨询、车型选择、费用说明、取还车流程、保险理赔、违章处理等场景。绑定通用电商 RAG 知识库作为 Layer2 兜底。',
    rag_product_ids = ARRAY['ecommerce-general-cs']::text[],
    updated_at = NOW()
WHERE agent_code = 'hivemtk-agent-carrent';

UPDATE ai_agents SET
    description = '民宿行业智能客服，覆盖预订咨询、房型选择、入住退房、周边游玩、投诉处理等场景。绑定通用电商 RAG 知识库作为 Layer2 兜底。',
    rag_product_ids = ARRAY['ecommerce-general-cs']::text[],
    updated_at = NOW()
WHERE agent_code = 'hivemtk-agent-homestay';

UPDATE ai_agents SET
    description = '国际货代行业智能客服，覆盖海运空运铁运咨询、报价、报关清关、物流追踪、理赔等场景。绑定通用电商 RAG 知识库作为 Layer2 兜底。',
    rag_product_ids = ARRAY['ecommerce-general-cs']::text[],
    updated_at = NOW()
WHERE agent_code = 'hivemtk-agent-freight';

UPDATE ai_agents SET
    description = '移民行业智能客服，覆盖项目咨询、条件评估、申请流程、费用说明、后续服务等场景。绑定通用电商 RAG 知识库作为 Layer2 兜底。',
    rag_product_ids = ARRAY['ecommerce-general-cs']::text[],
    updated_at = NOW()
WHERE agent_code = 'hivemtk-agent-immigra';

-- ============================================================
-- PART 11: 绑定 Telegram 渠道到通用电商客服智能体
-- 如果有 Telegram 渠道账号的话
-- ============================================================
INSERT INTO channel_agent_bindings (channel_type, account_id, agent_id, is_primary, enabled, created_at, updated_at)
SELECT 'telegram', 'default', id, TRUE, TRUE, NOW(), NOW()
FROM ai_agents WHERE agent_code = 'hivemtk-agent-general-cs'
ON CONFLICT DO NOTHING;

-- 绑定 Telegram 群到社群运营智能体
INSERT INTO channel_agent_bindings (channel_type, account_id, agent_id, is_primary, enabled, created_at, updated_at)
SELECT 'telegram_group', 'default', id, TRUE, TRUE, NOW(), NOW()
FROM ai_agents WHERE agent_code = 'hivemtk-agent-community'
ON CONFLICT DO NOTHING;

-- ============================================================
-- PART 12: 最终验证查询
-- ============================================================

-- 12.1 智能体列表
SELECT '=== 智能体列表 ===' AS info;
SELECT agent_code, name, agent_type, agent_mode, status,
       array_to_string(rag_product_ids, ',') AS rag,
       (SELECT COUNT(*) FROM faq_entries f WHERE f.agent_id = a.id) AS faq_count,
       (SELECT COUNT(*) FROM sop_templates s WHERE s.agent_id = a.id) AS sop_count
  FROM ai_agents a
 WHERE agent_code LIKE 'hivemtk%' OR agent_code LIKE 'seed-hivemtk%'
 ORDER BY agent_code;

-- 12.2 知识库列表
SELECT '=== RAG 知识库 ===' AS info;
SELECT id, name, category, doc_count, chunk_count, is_active
  FROM rag_products
 WHERE id LIKE '%hivemtk%' OR id LIKE '%ecommerce%';

-- 12.3 FAQ 按智能体分布
SELECT '=== FAQ/SOP 按智能体分布 ===' AS info;
SELECT a.name, COUNT(f.id) AS faqs, COUNT(s.id) AS sops
  FROM ai_agents a
  LEFT JOIN faq_entries f ON f.agent_id = a.id
  LEFT JOIN sop_templates s ON s.agent_id = a.id
 WHERE a.agent_code LIKE 'hivemtk%' OR a.agent_code LIKE 'seed-hivemtk%'
 GROUP BY a.id, a.name;

-- ============================================================
-- 迁移说明：
--   本迁移幂等，重复执行不会报错。
--   执行后验证命令：
--     psql -c "SELECT name, (SELECT COUNT(*) FROM faq_entries WHERE agent_id = id) FROM ai_agents WHERE agent_code LIKE 'hivemtk%';"
--   Layer1 FAQ/SOP 匹配验证：
--     后端日志里搜 Layer1 命中率，应该能看到 FAQ/SOP 被命中了
-- ============================================================
