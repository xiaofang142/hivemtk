-- ============================================================
-- 033_industry_ai_agents_seed.sql
-- 用户端 AI 智能体种子数据：8 个智能体
-- 内容:
--   1) 平台自用客服智能体（默认启用，绑定 default 渠道）
--   2) 7 个行业智能体（电子烟/成人用品/两性健康/租车/民宿/货代/移民）
--      每个行业智能体关联对应资产包，可供商户按需绑定渠道
-- 设计文档:
--   - docs/sales-champion/MULTI_AI_AGENT_DESIGN.md
--   - docs/marketing-features/agent-rag-qa.md
-- 关联迁移:
--   - 031_platform_cs_rag_seed.sql（平台客服 RAG 知识库）
--   - 032_industry_assets_local_seed.sql（行业资产包本地副本）
-- ============================================================

-- 0) 幂等清理（按 agent_code 前缀 hivemtk-agent- 清理，含平台客服历史 code）
DELETE FROM channel_agent_bindings WHERE agent_id IN (
    SELECT id FROM ai_agents WHERE agent_code LIKE 'hivemtk%' OR agent_code = 'seed-hivemtk-product-service' OR agent_code = 'seed-hivemtk-product-service'
);
DELETE FROM ai_agents WHERE agent_code LIKE 'hivemtk%' OR agent_code = 'seed-hivemtk-product-service' OR agent_code = 'seed-hivemtk-product-service';

-- ============================================================
-- 1) 平台自用客服智能体（默认启用）
--    绑定 RAG 知识库 hivemtk-platform-cs
--    绑定 web_embed/default 渠道，处理官网网页客服咨询
-- ============================================================
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
    'seed-hivemtk-product-service',
    'hivemtk 产品服务智能体',
    'HiveMTK 开源 AI 营销自动化套件官方客服，基于平台知识库（hivemtk-platform-cs）自动回答商户关于项目功能、技术架构、部署、渠道接入、资产市场、AI 智能体等咨询。',
    '',
    'customer_service',
    'passive',
    '你是 hivemtk 开源 AI 营销自动化套件的官方客服，熟悉产品架构、分层 Go 规范、AI 智能体编排、RAG 知识库、web+TG 私域接入等模块。耐心、简洁、技术可信。',
    E'你是 hivemtk 产品服务智能体。回答原则：\n1. 优先从 RAG 知识库（hivemtk 产品）召回内容；\n2. 涉及产品功能/部署/架构/接入方式直接给步骤；\n3. 涉及价格/承诺/非开源合规事项转人工（hivemtk 是开源项目，无商业定价）；\n4. 默认中文，可在用户切换语言时切英文。',
    '您好，我是 hivemtk 产品服务助手。可以问我：项目功能、技术架构、部署方式、接入步骤、源码位置等。',
    ARRAY['hivemtk-platform-cs']::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    'Qwen2.5-3B-Instruct',
    'openai',
    '',
    '',
    '',
    3,
    60,
    0.5,
    2048,
    0.9,
    0.5,
    0.5,
    TRUE,
    FALSE,
    TRUE,
    TRUE,
    FALSE,
    5,
    0.7,
    10,
    1,
    1,
    NOW(),
    NOW()
);

-- ============================================================
-- 2) 7 个行业智能体
--    每个智能体关联对应行业资产包（通过 AssetResolver 运行时加载）
--    默认未绑定渠道，商户可按需绑定
-- ============================================================

-- 2.1 电子烟行业智能体
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
    'hivemtk-agent-ecig',
    '电子烟行业客服',
    '电子烟行业智能客服，覆盖产品咨询、法规合规、使用指导、故障排查、售后维修等场景。关联资产包 hivemtk-asset-ecig（500+ 组 Q&A）。',
    '',
    'customer_service',
    'passive',
    'hivemtk-asset-ecig',
    '你是电子烟行业的专业客服助手，熟悉各类电子烟产品、烟油、雾化器及配件。回答要求：1)准确专业，不夸大功效；2)严格遵守法规，不向未成年人推荐；3)涉及健康问题建议咨询医生；4)故障问题给出具体排查步骤；5)语气友好，耐心解答。',
    '您好，我是电子烟专业客服，请问有什么可以帮您？',
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    'Qwen2.5-3B-Instruct',
    'openai',
    '',
    '',
    '',
    3,
    60,
    0.6,
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

-- 2.2 成人用品行业智能体
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
    'hivemtk-agent-adult',
    '成人用品行业客服',
    '成人用品行业智能客服，覆盖产品咨询、材质安全、使用指导、隐私配送、售后退换等场景。关联资产包 hivemtk-asset-adult（500+ 组 Q&A）。',
    '',
    'customer_service',
    'passive',
    'hivemtk-asset-adult',
    '你是成人用品行业的专业客服助手。回答要求：1)专业客观，用词得体；2)注重隐私保护，不追问个人信息；3)材质安全问题详细解答；4)使用指导清晰准确；5)退换货问题按政策处理；6)语气尊重，不评判。',
    '您好，我是专业客服，请问有什么可以帮您？',
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    'Qwen2.5-3B-Instruct',
    'openai',
    '',
    '',
    '',
    3,
    60,
    0.5,
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

-- 2.3 两性健康行业智能体
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
    'hivemtk-agent-sexhealth',
    '两性健康行业客服',
    '两性健康行业智能客服，覆盖健康咨询、产品推荐、健康指导、隐私保护等场景。关联资产包 hivemtk-asset-sexhealth（500+ 组 Q&A）。',
    '',
    'customer_service',
    'passive',
    'hivemtk-asset-sexhealth',
    '你是两性健康领域的专业客服助手。回答要求：1)科学客观，引用权威信息；2)涉及健康问题建议就医；3)注重隐私保护；4)保健食品不宣称疗效；5)语气专业温和，不尴尬不回避。',
    '您好，我是两性健康顾问，请问有什么可以帮您？',
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    'Qwen2.5-3B-Instruct',
    'openai',
    '',
    '',
    '',
    3,
    60,
    0.5,
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

-- 2.4 租车行业智能体
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
    'hivemtk-agent-carrent',
    '租车行业客服',
    '租车行业智能客服，覆盖预订咨询、车型选择、费用说明、取还车流程、保险理赔、违章处理等场景。关联资产包 hivemtk-asset-carrent（500+ 组 Q&A）。',
    '',
    'customer_service',
    'passive',
    'hivemtk-asset-carrent',
    '你是租车行业的专业客服助手。回答要求：1)准确说明租车流程和费用构成；2)车型推荐基于客户需求；3)保险条款解释清晰；4)违章和故障处理给出明确步骤；5)语气专业热情。',
    '您好，我是租车顾问，请问您需要什么车型？',
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    'Qwen2.5-3B-Instruct',
    'openai',
    '',
    '',
    '',
    3,
    60,
    0.6,
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

-- 2.5 民宿行业智能体
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
    'hivemtk-agent-homestay',
    '民宿行业客服',
    '民宿行业智能客服，覆盖预订咨询、房型选择、入住退房、周边游玩、投诉处理等场景。关联资产包 hivemtk-asset-homestay（500+ 组 Q&A）。',
    '',
    'customer_service',
    'passive',
    'hivemtk-asset-homestay',
    '你是民宿行业的专业客服助手。回答要求：1)准确描述房源信息和周边环境；2)价格和退订政策说明清晰；3)入住流程指导详细；4)周边游玩推荐实用；5)语气温馨亲切。',
    '您好，我是民宿顾问，请问您想去哪里旅行？',
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    'Qwen2.5-3B-Instruct',
    'openai',
    '',
    '',
    '',
    3,
    60,
    0.6,
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

-- 2.6 货代行业智能体
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
    'hivemtk-agent-freight',
    '国际货代行业客服',
    '国际货代行业智能客服，覆盖海运空运铁运咨询、报价、报关清关、物流追踪、理赔等场景。关联资产包 hivemtk-asset-freight（500+ 组 Q&A）。',
    '',
    'customer_service',
    'passive',
    'hivemtk-asset-freight',
    '你是国际货代行业的专业客服助手。回答要求：1)运价报价含费用明细；2)航线推荐基于时效和成本；3)报关单证指导准确；4)货物追踪给出查询方式；5)理赔流程说明清晰；6)语气专业严谨。',
    '您好，我是国际货代顾问，请问您需要什么运输服务？',
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    'Qwen2.5-3B-Instruct',
    'openai',
    '',
    '',
    '',
    3,
    60,
    0.5,
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

-- 2.7 移民行业智能体
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
    'hivemtk-agent-immigra',
    '移民行业客服',
    '移民行业智能客服，覆盖项目咨询、条件评估、申请流程、费用说明、后续服务等场景。关联资产包 hivemtk-asset-immigra（500+ 组 Q&A）。',
    '',
    'customer_service',
    'passive',
    'hivemtk-asset-immigra',
    '你是移民行业的专业客服助手。回答要求：1)项目信息准确客观；2)不承诺成功率；3)费用说明透明；4)申请流程指导清晰；5)建议客户以官方信息为准；6)语气专业稳重。',
    '您好，我是移民顾问，请问您想了解哪个项目？',
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    ARRAY[]::text[],
    'Qwen2.5-3B-Instruct',
    'openai',
    '',
    '',
    '',
    3,
    60,
    0.5,
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
-- 3) 绑定平台客服智能体到 default 渠道
--    channel_type='web_embed', account_id='default'
--    使其成为网页客服的默认智能体
-- ============================================================
INSERT INTO channel_agent_bindings (
    channel_type, account_id, agent_id,
    is_primary, enabled, created_at, updated_at
)
SELECT
    'web_embed',
    'default',
    id,
    TRUE,
    TRUE,
    NOW(),
    NOW()
FROM ai_agents
WHERE agent_code = 'seed-hivemtk-product-service'
ON CONFLICT DO NOTHING;

-- ============================================================
-- 4) 验证：确认 8 个智能体 + 1 条渠道绑定
-- ============================================================
SELECT agent_code, name, agent_type, agent_mode, status,
       array_to_string(rag_product_ids, ',') AS rag_products
  FROM ai_agents
 WHERE agent_code LIKE 'hivemtk%' OR agent_code = 'seed-hivemtk-product-service'
 ORDER BY agent_code;

SELECT cab.channel_type, cab.account_id, aa.agent_code, cab.is_primary, cab.enabled
  FROM channel_agent_bindings cab
  JOIN ai_agents aa ON cab.agent_id = aa.id
 WHERE aa.agent_code LIKE 'hivemtk-agent-%';
