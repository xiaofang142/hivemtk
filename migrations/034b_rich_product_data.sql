-- ============================================================
-- 034_rich_product_data.sql
-- 产品级数据丰富：新增 3 个通用智能体 + 绑定 FAQ/SOP + RAG 知识库
-- 纯 SQL 实现（无 DO $$ 块），避免 PL/pgSQL 语法坑
-- ============================================================

-- ============================================================
-- PART 0: 幂等清理
-- ============================================================
DELETE FROM channel_agent_bindings WHERE agent_id IN (
    SELECT id FROM ai_agents WHERE agent_code IN (
        'hivemtk-agent-general-cs', 'hivemtk-agent-ecom-sales', 'hivemtk-agent-community'
    )
);
DELETE FROM ai_agents WHERE agent_code IN (
    'hivemtk-agent-general-cs', 'hivemtk-agent-ecom-sales', 'hivemtk-agent-community'
);
DELETE FROM faq_entries WHERE question LIKE '%[seed-034]%';
DELETE FROM sop_templates WHERE name LIKE '%[seed-034]%';
DELETE FROM knowledge_chunks WHERE product_id = 'ecommerce-general-cs';
DELETE FROM knowledge_documents WHERE product_id = 'ecommerce-general-cs';
DELETE FROM rag_products WHERE id = 'ecommerce-general-cs';

-- ============================================================
-- PART 1: 新增 3 个核心通用智能体
-- ============================================================

-- 1.1 通用电商客服
INSERT INTO ai_agents (
    agent_code, name, description, avatar, agent_type, agent_mode,
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
    '覆盖电商全场景的智能客服：售前商品咨询/规格材质/价格活动/优惠券使用；售后退换货政策/维修保养/使用指导；物流查询/快递追踪/发货说明；订单修改/地址变更/发票申请等。',
    '', 'customer_service', 'passive',
    '你是专业、耐心、高效的电商客服，熟悉商品规格、退换货政策、物流流程、活动规则。回答原则：1) 商品参数准确；2) 价格活动按现行规则；3) 退换货按政策不越权承诺；4) 物流查询给明确追踪方式；5) 语气温和亲切。',
    '你是通用电商客服智能体。优先从 Layer1 FAQ 命中，命中直接返回。FAQ 未命中时，从 RAG 知识库（ecommerce-general-cs）召回。涉及具体价格/库存/订单状态引导用户提供订单号后查询。退换货严格按 7 天无理由/15 天质量政策。转人工关键词：人工/真人/转人工/客服/找人/投诉/差评。',
    '您好，我是电商客服小助手😊 可以帮您：商品咨询、订单查询、物流追踪、退换货说明、活动参与等。请问有什么需要帮忙的？',
    ARRAY['ecommerce-general-cs']::text[], ARRAY[]::text[], ARRAY[]::text[],
    ARRAY[]::text[], ARRAY[]::text[],
    'Qwen2.5-1.5B-Instruct', 'openai', '', '', '', 3, 60,
    0.6, 1000, 0.9, 0.5, 0.5,
    TRUE, TRUE, TRUE, TRUE, FALSE, 3,
    0.7, 8, 1, 1, NOW(), NOW()
) ON CONFLICT (agent_code) DO NOTHING;

-- 1.2 电商销售混合
INSERT INTO ai_agents (
    agent_code, name, description, avatar, agent_type, agent_mode,
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
    '兼具客服与销售能力的混合型智能体：售前种草推荐/痛点挖掘/场景化推荐；异议处理（价格/品质/物流/尺寸）；逼单话术（限时/库存/专属）；复购提醒/会员权益引导；活动预热/节日营销。适合微信/企微/社群私域触达。',
    '', 'hybrid', 'passive',
    '你是资深电商销冠，擅长种草式推荐、场景化话术、异议柔化处理、临门一脚逼单。不是生硬推销，而是基于客户需求的贴心顾问。语气活泼、共情力强、有温度。',
    '你是电商销售智能体。核心能力：1. 种草推荐：问使用场景→匹配系列→讲 1-2 个真实场景→引导选型号；2. 异议处理：价格高→对比价值/分三期/有优惠券；怕不好→讲品质保证/7 天无理由/真实评价；3. 逼单话术：限时优惠/库存紧张/今天下单送赠品；4. 复购引导：提醒上次买了 X，搭配 Y 更划算。禁止夸大功效、虚假承诺。',
    '您好呀～我是小助手，帮您找到最适合的商品 🎯 想买什么品类？送礼还是自用呀？',
    ARRAY['ecommerce-general-cs']::text[], ARRAY[]::text[], ARRAY[]::text[],
    ARRAY[]::text[], ARRAY[]::text[],
    'Qwen2.5-1.5B-Instruct', 'openai', '', '', '', 3, 60,
    0.7, 1200, 0.9, 0.5, 0.5,
    TRUE, TRUE, TRUE, TRUE, FALSE, 3,
    0.65, 6, 1, 1, NOW(), NOW()
) ON CONFLICT (agent_code) DO NOTHING;

-- 1.3 通用社群运营
INSERT INTO ai_agents (
    agent_code, name, description, avatar, agent_type, agent_mode,
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
    '面向品牌私域社群的运营助手：新人入群欢迎/群规说明/活动通知/话题引导/Q&A 答疑/投诉安抚/活跃度提醒/积分查询/会员权益。适合品牌粉丝群/用户交流群/VIP 会员群。',
    '', 'customer_service', 'passive',
    '你是热情、负责、有同理心的社群运营小姐姐，熟悉群规、活动、会员体系。既能温暖关怀，又能维护秩序。遇到敏感/投诉话题先共情再引导，不激化矛盾。',
    '你是社群运营智能体。场景处理：1. 新人入群→发欢迎语+群规+本周活动预告；2. 活动通知→按活动日程推送；3. 群友提问→FAQ 优先命中，复杂问题引导@管理员；4. 投诉/负面→先共情致歉+承诺转给管理员+跟进反馈；5. 广告/违规→温和提醒；6. 活跃度→定时抛话题。语气亲切像邻居小姐姐。',
    '欢迎来到咱们的社群呀～新人记得先看群规哦，有问题随时问我或者 @管理员 😊',
    ARRAY[]::text[], ARRAY[]::text[], ARRAY[]::text[],
    ARRAY[]::text[], ARRAY[]::text[],
    'Qwen2.5-1.5B-Instruct', 'openai', '', '', '', 3, 60,
    0.65, 800, 0.9, 0.5, 0.5,
    TRUE, TRUE, TRUE, TRUE, FALSE, 3,
    0.7, 5, 1, 1, NOW(), NOW()
) ON CONFLICT (agent_code) DO NOTHING;

-- ============================================================
-- PART 2: 绑定已有的 HiveMTK FAQ/SOP 到官方智能体
-- ============================================================
UPDATE faq_entries SET agent_id = (
    SELECT id FROM ai_agents WHERE agent_code IN ('seed-hivemtk-product-service', 'hivemtk_official') LIMIT 1
) WHERE question LIKE '%HiveMTK%' AND agent_id IS NULL;

UPDATE sop_templates SET agent_id = (
    SELECT id FROM ai_agents WHERE agent_code IN ('seed-hivemtk-product-service', 'hivemtk_official') LIMIT 1
) WHERE name LIKE '%HiveMTK%' AND agent_id IS NULL;

-- ============================================================
-- PART 3: 通用电商 RAG 知识库
-- ============================================================
INSERT INTO rag_products (
    id, name, description, category, vector_table,
    embedding_model, embedding_dim, llm_model,
    temperature, max_tokens, top_p, frequency_penalty, presence_penalty,
    response_format, system_prompt,
    top_k, chunk_size, chunk_overlap, similarity_threshold,
    is_active, status, doc_count, chunk_count,
    created_at, updated_at
) VALUES (
    'ecommerce-general-cs',
    '通用电商客服知识库',
    '覆盖电商售前咨询、售后退换货、物流查询、活动优惠券、订单操作等通用电商客服场景。',
    'ecommerce', 'rag_ecommerce_general',
    'bge-m3', 1024, 'Qwen2.5-1.5B-Instruct',
    0.3, 1024, 0.9, 0.5, 0.5,
    'text',
    '你是电商客服助手，依据检索到的知识片段回答。严格按政策回答，不超权承诺。涉及具体订单/价格/库存引导用户提供订单号后查询。',
    5, 800, 100, 0.55,
    TRUE, 1, 3, 21,
    NOW(), NOW()
) ON CONFLICT (id) DO UPDATE SET updated_at = NOW();

INSERT INTO knowledge_documents (
    product_id, source_type, title, file_name, filename, file_type,
    chunk_count, embed_status, tags, category, priority,
    metadata, imported_by, status, created_at, updated_at
) VALUES
('ecommerce-general-cs', 'text', '电商售前咨询手册', 'pre_sales.md', 'pre_sales.md', 'md', 8, 'indexed', '["售前","商品","规格","价格"]', 'pre_sales', 100, '{"source":"seed-034"}', 'system_seed', 1, NOW(), NOW()),
('ecommerce-general-cs', 'text', '电商售后退换货手册', 'after_sales.md', 'after_sales.md', 'md', 7, 'indexed', '["售后","退换货","退款"]', 'after_sales', 100, '{"source":"seed-034"}', 'system_seed', 1, NOW(), NOW()),
('ecommerce-general-cs', 'text', '电商物流与订单操作手册', 'logistics_order.md', 'logistics_order.md', 'md', 6, 'indexed', '["物流","快递","订单"]', 'logistics', 95, '{"source":"seed-034"}', 'system_seed', 1, NOW(), NOW())
ON CONFLICT DO NOTHING;

INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, char_count, metadata, created_at)
SELECT d.id, 'ecommerce-general-cs', v.idx, v.content, v.cc, v.meta::jsonb, NOW()
FROM knowledge_documents d
JOIN (
    SELECT '电商售前咨询手册' AS title, 0 AS idx, '商品规格查询标准流程：客户问规格 → 确认具体款号/系列 → 查规格表（尺寸/材质/重量/产地）→ 分点列 3-5 个核心参数 → 引导看详情页。' AS content, 120 AS cc, '{"doc":"pre_sales","section":"规格流程"}' AS meta
    UNION ALL SELECT '电商售前咨询手册', 1, '价格说明：标价为商品指导价，最终价格以下单时显示为准。活动叠加规则——秒杀与优惠券不可同时使用；会员折扣与优惠券可叠加；满减规则看活动详情。', 135, '{"doc":"pre_sales","section":"价格"}'
    UNION ALL SELECT '电商售前咨询手册', 2, '尺码推荐：询问身高体重 → 查尺码表 → 推荐正码 → 特殊体型提醒（大肚腩/孕后期建议大一码）→ 鞋类问脚长。', 98, '{"doc":"pre_sales","section":"尺码"}'
    UNION ALL SELECT '电商售前咨询手册', 3, '活动话术："这款目前有 XX 活动，XX 时间结束，现在下单立省 XX 元"；"还有 XX 件，库存紧张"；"今天下单还送 XX 赠品"。', 110, '{"doc":"pre_sales","section":"活动话术"}'
    UNION ALL SELECT '电商售前咨询手册', 4, '优惠券使用：优惠券分品类券（限特定品类）、满减券（需满足金额门槛）、新人券（限首次下单）。查看方式——用户中心→优惠券。', 115, '{"doc":"pre_sales","section":"优惠券"}'
    UNION ALL SELECT '电商售前咨询手册', 5, '组合推荐：客户问单品 → 主动推荐搭配；客户问品类 → 先问用途（送礼还是自用）、预算 → 推荐 2-3 款。', 105, '{"doc":"pre_sales","section":"搭配推荐"}'
    UNION ALL SELECT '电商售前咨询手册', 6, '送礼话术："送礼的话推荐 XX 款，包装精美有礼盒"；"附手写贺卡，写上祝福更有心意"。主动问送礼对象、预算。', 100, '{"doc":"pre_sales","section":"送礼"}'
    UNION ALL SELECT '电商售前咨询手册', 7, '材质安全：所有商品符合国家相关标准，详情页附检测报告。贴身衣物说明面料成分（A类/B类）；食品类有 SC 编号。', 125, '{"doc":"pre_sales","section":"材质"}'
    UNION ALL SELECT '电商售后退换货手册', 0, '7 天无理由退换：适用除贴身内衣/食品/特殊定制外的所有商品；条件——商品完好、吊牌未拆；流程——申请→寄回→验收入库→退款。运费——质量问题商家承担，个人原因买家承担。', 130, '{"doc":"after_sales","section":"7天无理由"}'
    UNION ALL SELECT '电商售后退换货手册', 1, '质量问题处理：签收后 15 天内可申请；需提供照片/视频证据；商家审核通过后寄回；运费商家承担；小额问题可协商部分退款不退货。', 115, '{"doc":"after_sales","section":"质量问题"}'
    UNION ALL SELECT '电商售后退换货手册', 2, '退款时效：原路退回（微信/支付宝/银行卡）1-3 工作日；账户余额实时；花呗/信用卡 3-5 工作日。超 5 工作日引导查账单。', 98, '{"doc":"after_sales","section":"退款时效"}'
    UNION ALL SELECT '电商售后退换货手册', 3, '发货后退款：①未签收——申请退款，快递拦截；②已签收——走 7 天无理由；③部分发货——退未发部分。', 88, '{"doc":"after_sales","section":"发货后退款"}'
    UNION ALL SELECT '电商售后退换货手册', 4, '破损/丢失：签收破损——拒收→补发；签收后破损——24 小时内联系→商家走物流理赔；整单丢失——索赔同时补发或退款。', 118, '{"doc":"after_sales","section":"破损丢失"}'
    UNION ALL SELECT '电商售后退换货手册', 5, '超售后期：先共情"理解您的心情～"→ 转人工看看能不能特殊处理→ 同时承诺终身技术支持。', 95, '{"doc":"after_sales","section":"超期话术"}'
    UNION ALL SELECT '电商售后退换货手册', 6, '投诉安抚 SOP：①先共情致歉 ②承认问题不辩解 ③给出具体方案（退款/补发/优惠券）④确认接受⑤跟进结果。禁止争辩。', 105, '{"doc":"after_sales","section":"投诉安抚"}'
    UNION ALL SELECT '电商物流与订单操作手册', 0, '发货时效：现货 48 小时内；预售按详情页（7-15 天）；大促可能延长；节假日顺延。', 78, '{"doc":"logistics","section":"发货时效"}'
    UNION ALL SELECT '电商物流与订单操作手册', 1, '物流查询：发送订单号→查运单+快递公司→返回官网链接；提醒可在订单详情点【查看物流】自行追踪；超 3 天未更新协助联系快递。', 102, '{"doc":"logistics","section":"物流查询"}'
    UNION ALL SELECT '电商物流与订单操作手册', 2, '地址修改：未发货直接改；已发货联系快递改址（可能产生改址费）；建议下单前仔细核对！', 80, '{"doc":"logistics","section":"改地址"}'
    UNION ALL SELECT '电商物流与订单操作手册', 3, '发票申请：下单备注或订单详情补开；电子发票发邮箱；纸质发票随单寄或单独寄；发票有误当月可改，跨月红冲。', 105, '{"doc":"logistics","section":"发票"}'
    UNION ALL SELECT '电商物流与订单操作手册', 4, '超区/无网点：①提前联系用户协商 ②转发其他快递 ③退回换地址重发 ④告知时效延长。禁止静默退回。', 85, '{"doc":"logistics","section":"超区"}'
    UNION ALL SELECT '电商物流与订单操作手册', 5, '催发货话术："我帮您催一下哈～"→ 内部标记加急；给明确时间预期，不模糊。', 65, '{"doc":"logistics","section":"催发货"}'
) v ON d.title = v.title AND d.product_id = 'ecommerce-general-cs'
ON CONFLICT DO NOTHING;

-- ============================================================
-- PART 4: 通用电商客服 FAQ（agent 通过子查询关联）
-- ============================================================
INSERT INTO faq_entries (question, answer, keywords, category, intent, confidence, hit_count, agent_id, enabled, created_at, updated_at)
SELECT v.question, v.answer, v.keywords, v.category, v.intent, v.confidence, v.hit_count,
       (SELECT id FROM ai_agents WHERE agent_code = 'hivemtk-agent-general-cs' LIMIT 1),
       v.enabled, NOW(), NOW()
FROM (VALUES
    -- 售前 10 条
    ('这个产品有什么功能 [seed-034]', '亲，这款商品的核心卖点：①[卖点1] ②[卖点2] ③[卖点3]。您告诉我使用场景，我帮您推荐最合适的款～', ARRAY['功能','卖点','特点','好用在哪']::text[], 'pre_sales', 'product_info', 0.88, 50, TRUE),
    ('有没有货 [seed-034]', '让我帮您查一下库存～这款目前 [有货/少量库存/预售中 XX 时间发货]。库存紧张建议尽早下单哦！', ARRAY['有货','库存','现货','有没有']::text[], 'pre_sales', 'stock', 0.92, 100, TRUE),
    ('什么时候发货 [seed-034]', '现货商品 48 小时内发出；预售以详情页为准（通常 7-15 天）；大促期间可能延长。', ARRAY['发货','什么时候发','时效']::text[], 'logistics', 'ship_time', 0.91, 80, TRUE),
    ('发什么快递 [seed-034]', '默认发中通/圆通/申通，如需顺丰请备注（需补运费差价）。大件默认物流。', ARRAY['快递','发什么','顺丰','物流']::text[], 'logistics', 'express', 0.9, 70, TRUE),
    ('多少钱 [seed-034]', '这款标价 XX 元，目前 [有活动/有优惠券/会员价 XX 元]。不同规格价格不同，活动叠加规则看详情页哦。', ARRAY['多少钱','价格','便宜','优惠']::text[], 'pre_sales', 'price', 0.9, 120, TRUE),
    ('有什么活动 [seed-034]', '当前活动：①[活动1] XX 截止 ②[活动2] ③新用户首单立减 XX ④会员专享价 XX。首页可以看全部，我帮您算到手价～', ARRAY['活动','优惠','满减','打折','促销']::text[], 'pre_sales', 'promotion', 0.89, 90, TRUE),
    ('可以便宜点吗 [seed-034]', '理解～帮您看看更划算的方式：①有没有新人券 ②会员有没有额外折扣 ③凑单满减能省多少。预算多少我帮您算最优方案！', ARRAY['便宜','砍价','优惠点','划算']::text[], 'pre_sales', 'discount_req', 0.85, 60, TRUE),
    ('尺码怎么选 [seed-034]', '告诉我身高体重～对照尺码表帮您推荐。一般穿正码，特殊情况（怀孕/大肚腩）建议大一码。鞋子还要看脚长。', ARRAY['尺码','多大','大小','穿多大']::text[], 'pre_sales', 'size', 0.93, 200, TRUE),
    ('是什么材质 [seed-034]', '材质是：[材质类型]，符合 [A类/B类/食品级] 标准，有检测报告。对材质有特殊要求吗（纯棉/透气/环保）？', ARRAY['材质','材料','面料','成分']::text[], 'pre_sales', 'material', 0.9, 85, TRUE),
    ('可以退换吗 [seed-034]', '可以的！适用 7 天无理由（除贴身内衣/食品/特殊定制），条件是商品完好吊牌未拆。质量问题 15 天内可申请，运费商家承担。入口在订单的【申请售后】～', ARRAY['退换','退货','换货','退款','7天']::text[], 'after_sales', 'return_policy', 0.95, 300, TRUE),
    -- 售后 10 条
    ('怎么退款 [seed-034]', '退款流程：①打开订单→【申请售后】②选退款类型→填原因+上传凭证 ③提交等待审核 ④审核通过原路返回（1-3 工作日）。一步步引导您操作～', ARRAY['退款','怎么退','申请退款']::text[], 'after_sales', 'refund_process', 0.93, 150, TRUE),
    ('退款多久到账 [seed-034]', '到账时效：微信/支付宝原路退 1-3 工作日；花呗/信用卡 3-5 工作日；余额实时。超 5 工作日查账单或联系我跟进～', ARRAY['退款','到账','多久','什么时候到']::text[], 'after_sales', 'refund_time', 0.91, 120, TRUE),
    ('运费谁承担 [seed-034]', '退换货运费：①质量问题——商家承担（运费险理赔）②7 天无理由——买家承担（或用运费险抵扣）③错发漏发——商家承担补发运费。', ARRAY['运费','谁承担','运费险','寄回去']::text[], 'after_sales', 'shipping_cost', 0.92, 90, TRUE),
    ('商品有问题怎么办 [seed-034]', '真的抱歉！收到有问题：①拍照/录像留证 ②订单申请【质量问题退换】③等待客服审核。15 天内可申请，运费商家承担。小额问题可协商部分退款～', ARRAY['有问题','坏了','质量','瑕疵']::text[], 'after_sales', 'quality_issue', 0.94, 180, TRUE),
    ('已经签收了还能退吗 [seed-034]', '可以的！签收后仍可走 7 天无理由，条件：商品完好、吊牌未拆。签收不影响退换，入口在订单详情【申请售后】～', ARRAY['签收','收到了','已签收','还能退']::text[], 'after_sales', 'return_after_sign', 0.9, 80, TRUE),
    ('不退可以少退点钱吗 [seed-034]', '理解～如果商品能用但有小瑕疵，我帮您申请部分退款（不退货）。具体退多少要看问题大小，帮您 @售后专员 评估～', ARRAY['部分退款','不退货','退点钱','补偿']::text[], 'after_sales', 'partial_refund', 0.85, 40, TRUE),
    ('补发流程 [seed-034]', '补发适用于错发/漏发/破损：①拍照→联系客服→确认补发方案→寄出新单号。运费商家承担。库存紧张可能需要等待～', ARRAY['补发','再发一份','重发']::text[], 'after_sales', 'resend', 0.88, 55, TRUE),
    ('超了退换期怎么办 [seed-034]', '超退换期后系统无法直接发起，但我可以帮您 @售后专员 看看能不能特殊处理。商品有终身技术支持，使用问题随时联系～', ARRAY['超期','过期','过了退换期','超过时间']::text[], 'after_sales', 'expired', 0.83, 30, TRUE),
    ('发票怎么开 [seed-034]', '发票申请：①下单时备注抬头+税号 ②已下单在订单详情→【申请发票】补开；电子发票发邮箱（1-3 工作日）；纸质发票随单寄或单独寄。', ARRAY['发票','开票','开发票','抬头']::text[], 'after_sales', 'invoice', 0.9, 60, TRUE),
    ('评价错了能改吗 [seed-034]', '平台一般不支持直接修改。如果误操作，我帮您：①追加追评（买家追评可改 1 次）②联系平台申请删除错误评价（特殊情况）。', ARRAY['评价','改评价','差评','追评']::text[], 'after_sales', 'review_fix', 0.82, 25, TRUE),
    -- 物流 8 条
    ('快递到哪了 [seed-034]', '给下订单号～我帮您查物流轨迹。也可以在订单详情点【查看物流】自行追踪。超 3 天没更新建议联系快递官网～', ARRAY['快递','物流','到哪了','追踪','查快递']::text[], 'logistics', 'track', 0.93, 250, TRUE),
    ('可以改地址吗 [seed-034]', '①未发货——后台直接改；②已发货——联系快递改址（可能产生改址费，偏远地区无法改）；③已派送——快递员会联系您。下单前仔细核对！', ARRAY['改地址','地址错了','换地址']::text[], 'logistics', 'change_address', 0.9, 100, TRUE),
    ('快递慢/不更新 [seed-034]', '抱歉久等了！常见原因：①大促爆仓→超 3 天可联系快递官方 ②天气/节假日影响 ③转运中心分拣延迟。把运单发我协助联系～', ARRAY['慢','不更新','快递卡','太慢']::text[], 'logistics', 'slow_delivery', 0.88, 90, TRUE),
    ('快递丢了/破损 [seed-034]', '真的抱歉！处理：①签收破损→拍照→拒收→补发 ②签收后破损/丢失→24 小时内联系→走物流理赔→同时补发或退款。', ARRAY['丢了','破损','坏了','快递损坏']::text[], 'logistics', 'lost_damaged', 0.94, 70, TRUE),
    ('能指定快递吗 [seed-034]', '可以，但注意：①指定顺丰——补运费差价（+10-20 元/kg）；②其他快递——只要可达都可以；③大件指定物流——提前确认运费时效。备注时写清楚哦！', ARRAY['指定快递','要顺丰','选快递']::text[], 'logistics', 'specify_express', 0.85, 35, TRUE),
    ('寄到偏远地区可以吗 [seed-034]', '可以！但偏远地区（新疆/西藏等）：①部分快递超区→转发邮政 ②时效延长（7-15 天）③可能产生额外运费。下单后如果快递无法送达会提前通知～', ARRAY['偏远','新疆','西藏','超区']::text[], 'logistics', 'remote', 0.87, 40, TRUE),
    ('可以上门取件吗 [seed-034]', '退换货时部分城市支持：申请售后选【上门取件】→快递员按时上门→给快递单号。偏远/大件可能不支持，建议选【自行寄回】～', ARRAY['上门','取件','上门取货']::text[], 'logistics', 'home_pickup', 0.84, 30, TRUE),
    ('可以加钱发更快的吗 [seed-034]', '可以！方式：①加钱发顺丰空运（通常 +15-25 元/kg，次日达）②发京东/德邦快。告诉我城市我帮您查最快方式～', ARRAY['加快','加钱','更快','加急']::text[], 'logistics', 'speed_up', 0.83, 25, TRUE),
    -- 活动/通用 12 条
    ('优惠券怎么用 [seed-034]', '使用：①下单选可用券→自动抵扣 ②注意门槛有效期 ③部分活动不叠加。手里有什么券想买什么，我帮您算最优～', ARRAY['优惠券','怎么用','满减']::text[], 'pre_sales', 'coupon', 0.92, 180, TRUE),
    ('优惠券哪里领 [seed-034]', '渠道：①首页【领券中心】②粉丝群不定期发 ③会员生日专属 ④新用户注册自动送新人券包。错过关注下一波大促哦～', ARRAY['领券','优惠券在哪','怎么领']::text[], 'pre_sales', 'coupon_get', 0.89, 140, TRUE),
    ('会员有什么权益 [seed-034]', '权益：①会员专享价（低 5-20%）②生日专属礼包 ③积分翻倍 ④优先发货权 ⑤专属客服通道。等级越高权益越多（银/金/钻石）～', ARRAY['会员','VIP','有什么好处']::text[], 'general', 'vip_benefit', 0.9, 100, TRUE),
    ('积分怎么用 [seed-034]', '使用方式：①下单抵扣（100 分=1 元，单笔抵 30%）②积分商城换购 ③兑换优惠券/抽奖。有效期 1 年，到期清零哦～', ARRAY['积分','怎么用','积分兑换']::text[], 'general', 'points', 0.88, 70, TRUE),
    ('怎么成为会员 [seed-034]', '入门方式：①消费满 299 自动银卡 ②购买年卡会员（立得券包+全年专享价）③邀 3 人注册→送 1 个月金卡。升级靠消费积累～', ARRAY['怎么成为会员','开通会员','加入会员']::text[], 'general', 'join_vip', 0.87, 55, TRUE),
    ('新人优惠有什么 [seed-034]', '新用户福利：①新人券包（30-50 元）②首单立减 ③首次评价送积分。没收到发手机号帮您查账号状态～', ARRAY['新人','新用户','新人优惠','首单']::text[], 'pre_sales', 'new_user', 0.89, 85, TRUE),
    ('可以货到付款吗 [seed-034]', '暂不支持货到付款，需在线支付（微信/支付宝/银行卡都可）。这样能加快发货速度，也更安全～', ARRAY['货到付款','到付','COD']::text[], 'general', 'cod', 0.82, 30, TRUE),
    ('有微信公众号吗 [seed-034]', '有的！搜【xxx】就能找到～关注可：收活动推送/领专属券/查订单物流/收发货通知。粉丝群不定期发秒杀哦！', ARRAY['公众号','微信','扫码','关注']::text[], 'general', 'wechat', 0.84, 40, TRUE),
    ('可以开发票吗 [seed-034]', '可以开！电子发票默认发邮箱，纸质随单寄或单独寄（有邮费）。申请方式：下单备注或订单详情补开。有误当月可改，跨月红冲～', ARRAY['发票','能开票吗','开专票']::text[], 'after_sales', 'invoice', 0.88, 50, TRUE),
    ('评价后有奖励吗 [seed-034]', '评价有奖！①订单评价送积分（100-300 分/条）②追评再送一半 ③带图/视频额外送优惠券。在【我的评价】看积分到账～', ARRAY['评价','奖励','送积分','好评']::text[], 'general', 'review_reward', 0.85, 35, TRUE),
    ('怎么注销账号 [seed-034]', '流程：①个人中心→设置→隐私→注销账号；②需无未完成订单/退款；③注销数据全清不可恢复。先处理完售后哦！注销后券/积分清零～', ARRAY['注销','销户','退出','删账号']::text[], 'general', 'delete_account', 0.8, 20, TRUE),
    ('我想投诉 [seed-034]', '先别着急！告诉我具体情况：①商品质量 ②物流问题 ③客服态度 ④活动/优惠券。把订单号和情况告诉我，我来跟进！', ARRAY['投诉','差评','举报','我要投诉']::text[], 'general', 'complaint', 0.9, 80, TRUE)
) AS v(question, answer, keywords, category, intent, confidence, hit_count, enabled)
ON CONFLICT DO NOTHING;

-- ============================================================
-- PART 5: 电商销售混合 FAQ
-- ============================================================
INSERT INTO faq_entries (question, answer, keywords, category, intent, confidence, hit_count, agent_id, enabled, created_at, updated_at)
SELECT v.question, v.answer, v.keywords, v.category, v.intent, v.confidence, v.hit_count,
       (SELECT id FROM ai_agents WHERE agent_code = 'hivemtk-agent-ecom-sales' LIMIT 1),
       v.enabled, NOW(), NOW()
FROM (VALUES
    ('给我推荐一下 [seed-034]', '好哒～告诉我：①送礼还是自用？②预算多少？③特别偏好（颜色/材质/品牌）？帮您精准推荐 2-3 款，每款讲一个独特亮点～', ARRAY['推荐','给我推荐','选一个','什么好']::text[], 'recommend', 'recommend', 0.93, 100, TRUE),
    ('送礼选哪个好 [seed-034]', '送礼精选：🎁 送女朋友/男朋友——XX；🎁 送父母——XX；🎁 送孩子——XX；🎁 送领导——XX。告诉我送谁、预算多少帮您锁定！', ARRAY['送礼','送人','送朋友','礼物']::text[], 'recommend', 'gift', 0.94, 120, TRUE),
    ('这款值得买吗 [seed-034]', '核心价值点：✨ 性能：[优势]；✨ 体验：[使用场景]；✨ 性价比：同价位对比。在意 [续航/价格/品质] 这款绝对值得入～', ARRAY['值得买','划算吗','性价比','买不买']::text[], 'recommend', 'worth', 0.89, 85, TRUE),
    ('有新款吗 [seed-034]', '刚上新：① [新款 A] XX 元，主打 [卖点]；② [新款 B] XX 元。新款有首发价/赠品，感兴趣发详细介绍给您～', ARRAY['新款','新出的','刚上','最新']::text[], 'recommend', 'new_arrival', 0.88, 60, TRUE),
    ('有套装吗 [seed-034]', '精选套装：① 入门 A+B XX 元（省 XX）② 进阶 A+B+C XX 元（送赠品+延长保修）③ 全家桶 XX 元（限时 7 折）。套装更划算，预算多少帮您搭配～', ARRAY['套装','组合','搭配','打包']::text[], 'recommend', 'bundle', 0.91, 75, TRUE),
    ('哪个系列最好 [seed-034]', '定位不同：🌟 旗舰——品质最好、功能最全、最贵；🌟 进阶——性价比高、功能够用；🌟 入门——价格亲民、核心功能齐全。预算和需求告诉我选对系列～', ARRAY['哪个好','最好','旗舰','系列','选哪个']::text[], 'recommend', 'series', 0.9, 80, TRUE),
    ('为什么比别家贵 [seed-034]', '好问题～定价因为：①用了更好的材料/工艺 ②带 [售后服务] ③品牌价值。现在下单还有活动，折合下来和别家差不多，品质售后好很多。算下到手价？', ARRAY['贵','为什么贵','比别家贵']::text[], 'objection', 'price_high', 0.88, 95, TRUE),
    ('什么时候有活动 [seed-034]', '近期活动：📅 本周 [活动名] XX 截止；📅 下周 [活动]；📅 大促预热中力度更大。现在这款正在做限时折扣，错过等下次～', ARRAY['活动','什么时候','下次','等活动']::text[], 'promotion', 'when_activity', 0.87, 50, TRUE),
    ('可以等等再买吗 [seed-034]', '理解，不过考虑：⏰ 活动 XX 时间截止，立省 XX；📦 只剩 XX 件，售完恢复原价；🎁 现在送 XX 赠品。建议现在下手真的划算！', ARRAY['等等','不急','再看看']::text[], 'promotion', 'wait_buy', 0.86, 55, TRUE),
    ('现在买有什么好处 [seed-034]', '现在买专属福利：①活动价 XX（省 XX）②送 [赠品]（值 XX）③优先发货 ④加赠优惠券/积分。活动就剩 XX 小时了～', ARRAY['现在买','现在下单','立即下单']::text[], 'promotion', 'now_buy', 0.9, 90, TRUE),
    ('库存还有多少 [seed-034]', '这款库存：🌟 热门色号只剩 XX 件；📦 其他色还有 XX。热门色经常断货，看中就下手～帮您标记 VIP 优先处理！', ARRAY['库存','还有多少','剩多少','限量']::text[], 'promotion', 'stock_pressure', 0.85, 40, TRUE),
    ('能留个吗 [seed-034]', '可以标记意向客户优先发货，但库存不能长期预留哦～今天能下单帮您备注 VIP！错过可能要等补货～', ARRAY['留一个','预留','帮我留']::text[], 'promotion', 'reserve', 0.83, 25, TRUE),
    ('会员有特价吗 [seed-034]', '会员专享：🌟 银卡减 XX；🌟 金卡减 XX + 双倍积分；🌟 钻石减 XX + 三倍积分 + 免运费。开通年卡这笔订单省更多！算下最优组合？', ARRAY['会员','VIP','特价','会员价']::text[], 'promotion', 'vip_price', 0.87, 70, TRUE),
    ('可以分期吗 [seed-034]', '可以！支持：💳 花呗 3/6/12 期 0 手续费；💳 信用卡 3-6% 手续费；💳 白条 免息。算下来每月 XX 元轻松入手～', ARRAY['分期','月供','分期付款','花呗']::text[], 'promotion', 'installment', 0.86, 55, TRUE),
    ('能帮我凑满减吗 [seed-034]', '帮您算最优：购物车 XX 元，差 XX 到门槛。建议加 [X 款 XX 元]→凑满→减 XX→实际省 XX。要不要帮您列清单？', ARRAY['凑满减','满减','凑单','怎么凑']::text[], 'promotion', 'bundle_help', 0.91, 100, TRUE),
    ('上次买过能优惠吗 [seed-034]', '当然！老客专属：①复购立减 XX ②老客积分翻倍 ③加赠老客券。上次买的哪款，帮您算这次搭配最划算～', ARRAY['老客户','复购','回头客']::text[], 'followup', 'repeat', 0.89, 60, TRUE),
    ('这个和我上次的搭配好吗 [seed-034]', '好问题！上次买的 [XX]，这次看的 [XX]：✅ 可以搭配，效果更好；✅ 同系列风格统一；✅ 组合价省 XX。建议一起入体验更完整～', ARRAY['搭配','配合','一起买','组合用']::text[], 'followup', 'match', 0.88, 50, TRUE),
    ('想升级/换代 [seed-034]', '升级方案：①旧款抵价——按购买价 XX% 抵扣；②直接补差——补差价换新款；③以旧换新节日力度更大。现在用的哪款想换哪款算最优？', ARRAY['升级','换代','以旧换新']::text[], 'followup', 'upgrade', 0.85, 35, TRUE),
    ('有推荐朋友的奖励吗 [seed-034]', '有的！双份福利：①好友首单立减 XX；②您送 XX 积分 + XX 优惠券。在【我的→邀请好友】有专属链接～', ARRAY['推荐','邀请','介绍朋友','分享']::text[], 'followup', 'referral', 0.88, 55, TRUE),
    ('这次再买点什么 [seed-034]', '根据您之前购买：💡 [搭配 A]——配合之前买的一起用效果翻倍；💡 [补充 B]——上次没买但很实用；💡 [套装 C]——有满减比单买省。预算多少精准挑～', ARRAY['再买点','加购','凑单','还能买']::text[], 'followup', 'cross_sell', 0.89, 65, TRUE)
) AS v(question, answer, keywords, category, intent, confidence, hit_count, enabled)
ON CONFLICT DO NOTHING;

-- ============================================================
-- PART 6: 通用社群运营 FAQ
-- ============================================================
INSERT INTO faq_entries (question, answer, keywords, category, intent, confidence, hit_count, agent_id, enabled, created_at, updated_at)
SELECT v.question, v.answer, v.keywords, v.category, v.intent, v.confidence, v.hit_count,
       (SELECT id FROM ai_agents WHERE agent_code = 'hivemtk-agent-community' LIMIT 1),
       v.enabled, NOW(), NOW()
FROM (VALUES
    ('我刚入群 [seed-034]', '欢迎欢迎👏 新伙伴看群规：① 禁止广告/外链 ② 友好交流 ③ 有问题 @管理员。本周活动预告：[活动名]。问题随时问我～', ARRAY['入群','刚进','新人','新来的']::text[], 'newbie', 'new_join', 0.95, 200, TRUE),
    ('有什么活动 [seed-034]', '近期活动：📅 [日期] [活动1]；📅 [日期] [活动2]；🎁 本周福利：领 XX 优惠券。更多看群公告哦～', ARRAY['活动','福利','抽奖','红包']::text[], 'activity', 'activity', 0.93, 150, TRUE),
    ('我发广告了怎么办 [seed-034]', '理解您可能不熟群规～第一次会提醒撤回。如果是相关内容（如产品心得）改成纯文字分享。多次违规会被移出哦～', ARRAY['广告','发了广告','违规']::text[], 'rule', 'advertisement', 0.85, 60, TRUE),
    ('怎么发群红包 [seed-034]', '发红包：① 聊天框点【+】② 选【红包】③ 金额个数（拼手气/普通）④ 留言（可选）⑤ 确认。发红包是感谢好方式但别代替解答哦～', ARRAY['红包','发红包','怎么发']::text[], 'activity', 'red_packet', 0.9, 80, TRUE),
    ('想加好友 [seed-034]', '可以互相加！温馨提醒：① 不要批量加陌生人 ② 加时说"XX 群的 XX"让对方知道 ③ 加了别立刻发广告，先建立联系～', ARRAY['加好友','私聊','加微信']::text[], 'rule', 'add_friend', 0.85, 35, TRUE),
    ('群里发言有什么规则 [seed-034]', '✅ 可以：分享心得/问问题/发起讨论 ❌ 不可以：广告/刷屏/人身攻击/骚扰。第一次提醒，二次移出～', ARRAY['群规','规则','公约']::text[], 'rule', 'group_rule', 0.92, 180, TRUE),
    ('这周有什么活动 [seed-034]', '本周精彩：🎉 [活动名]——日期 XX，参与方式 XX；🎁 福利时间；👑 本周之星表扬。置顶群消息不错过～', ARRAY['这周','本周','本周活动']::text[], 'activity', 'weekly_activity', 0.9, 100, TRUE),
    ('活动怎么参加 [seed-034]', '参与：① 看群公告找详情 ② 按要求报名（群内接龙/表单）③ 等管理员确认 ④ 按时参加。截止日期看公告，问题 @管理员～', ARRAY['参加','怎么参加','报名']::text[], 'activity', 'join_activity', 0.88, 55, TRUE),
    ('我中奖了吗 [seed-034]', '中奖名单活动截止后 X 小时内公布在群里。发奖需私聊管理员提供收货信息，超 3 天未提供视为放弃哦～', ARRAY['中奖','中了吗','抽奖结果']::text[], 'activity', 'lottery_result', 0.89, 70, TRUE),
    ('积分怎么换 [seed-034]', '积分兑换：①【个人中心→积分商城】选商品→确认；②实物包邮 3-5 天到，虚拟自动到账。每年 12 月清零尽快兑换～', ARRAY['积分','换积分','兑换','积分商城']::text[], 'reward', 'points_exchange', 0.9, 85, TRUE),
    ('怎么赚积分 [seed-034]', '赚积分：① 群活跃发言 +10/次（日上限 50）② 参与活动 +50/次 ③ 分享心得 +100/篇（加精翻倍）④ 推好友入群 +200/人 ⑤ 生日登录 +500。每年清零哦～', ARRAY['积分','怎么赚','获取积分','赚积分']::text[], 'reward', 'earn_points', 0.89, 95, TRUE),
    ('这个活动是真的吗 [seed-034]', '真实性：① 正规活动群公告+公众号同步 ② 官方不会让你转账 ③ 警惕"内部名额""限时免费"话术。不确定 @管理员核实～', ARRAY['真的吗','真假','骗局','安全']::text[], 'safety', 'verify', 0.92, 80, TRUE),
    ('能帮我宣传吗 [seed-034]', '抱歉，本群是品牌官方社群，不接受外部群/广告宣传。产品心得纯文字分享可以～推广建议找合作渠道或公众号投稿～', ARRAY['宣传','推广','拉人']::text[], 'rule', 'promo_request', 0.82, 25, TRUE),
    ('群里发的链接安全吗 [seed-034]', '链接提醒：① 官方域名是 weixin.qq.com / xxx.com ② 不点陌生短链接 ③ 不下未知 APP ④ 可疑 @管理员核实 ⑤ 保护个人信息～', ARRAY['链接','安全吗','安全']::text[], 'safety', 'link_safety', 0.91, 70, TRUE),
    ('怎么让群更活跃 [seed-034]', '活跃建议：① 定期发起话题（"大家周末干嘛呀"）② 每周小投票 ③ 每月活跃之星 ④ 节日小活动 ⑤ 好消息及时表扬。有运营问题和 @管理员 聊～', ARRAY['活跃','怎么活跃','没人说话']::text[], 'operation', 'activity_tip', 0.87, 40, TRUE),
    ('有人吵架怎么办 [seed-034]', '处理：① 先劝和"大家都是朋友嘛"② 严重争执 @管理员 ③ 别加入战团保持中立 ④ 人身攻击提醒群规。温暖大家庭互相尊重哈～', ARRAY['吵架','争执','打架','对骂']::text[], 'operation', 'fight', 0.9, 50, TRUE),
    ('被群成员骚扰了 [seed-034]', '被骚扰处理：① 截图保存证据 ② @管理员说明 ③ 严重骚扰可拉黑 ④ 多次违规移出群。保护好自己！不要轻易加陌生人为好友～', ARRAY['骚扰','被骚扰','烦','拉黑']::text[], 'safety', 'harassment', 0.93, 40, TRUE),
    ('怎么退出群 [seed-034]', '退群：群聊右上角→滑到底选【删除并退出】。积分会清零哦先兑换。如果是被骚扰可以 @管理员 沟通看能不能解决～', ARRAY['退出','退群','不想在这']::text[], 'operation', 'leave_group', 0.86, 30, TRUE),
    ('我要投诉 [seed-034]', '先别着急！告诉我具体情况：活动/群成员/管理员/商品/其他？我帮您记录转给负责人，也可以私信 @群主 沟通～', ARRAY['投诉','抱怨','我要投诉']::text[], 'general', 'complaint', 0.93, 60, TRUE),
    ('建议可以提吗 [seed-034]', '当然欢迎！群运营/活动/福利/产品/任何想法。私信 @群主 或直接群里说都可以，我们认真考虑～', ARRAY['建议','意见','想法']::text[], 'general', 'feedback', 0.9, 75, TRUE),
    ('为什么被踢了 [seed-034]', '移出群通常是违反群规：广告/刷屏/人身攻击/骚扰/诈骗。误判可以私信 @群主 申请重新入群。以后注意守群规哦～', ARRAY['被踢','踢出去','为什么被踢']::text[], 'rule', 'kicked', 0.88, 45, TRUE),
    ('有福利吗 [seed-034]', '当前福利：🎁 新人入群礼 🎉 每周三秒杀 💬 活跃之星积分 🎂 生日专属礼。置顶群消息不错过任何优惠～', ARRAY['福利','优惠','好处']::text[], 'activity', 'welfare', 0.9, 90, TRUE)
) AS v(question, answer, keywords, category, intent, confidence, hit_count, enabled)
ON CONFLICT DO NOTHING;

-- ============================================================
-- PART 7: 电商客服 SOP
-- ============================================================
INSERT INTO sop_templates (name, intent, stage, template, vars, priority, confidence, agent_id, enabled, hit_count, created_at, updated_at)
SELECT v.name, v.intent, v.stage, v.template, v.vars, v.priority, v.confidence,
       (SELECT id FROM ai_agents WHERE agent_code = 'hivemtk-agent-general-cs' LIMIT 1),
       v.enabled, v.hit_count, NOW(), NOW()
FROM (VALUES
    ('售前推荐开场 [seed-034]', 'pre_sales', 'initial', '您好！请问有什么可以帮您的？需要推荐商品告诉我品类/预算/送礼还是自用？帮您精准推荐～', '{}', 93, 0.92, TRUE, 120),
    ('售后致歉 SOP [seed-034]', 'after_sales', 'objection', '真的非常抱歉给您带来不好的体验 😢 我帮您处理！方便说下具体问题吗（质量/物流/其他）？发订单号走售后流程～', '{}', 94, 0.95, TRUE, 150),
    ('退换货流程引导 [seed-034]', 'after_sales', 'middle', '退换货步骤：① 订单→【申请售后】② 选退款类型→填原因 ③ 上传凭证 ④ 审核通过寄回 ⑤ 验收入库退款（1-3 工作日）。一步步引导您？', '{}', 91, 0.93, TRUE, 100),
    ('物流查询引导 [seed-034]', 'logistics', 'middle', '给下订单号/运单号～帮您查轨迹。也可以在订单详情点【查看物流】自行追踪。超 3 天没更新协助联系快递！', '{}', 92, 0.91, TRUE, 200),
    ('优惠券使用引导 [seed-034]', 'pre_sales', 'middle', '使用：① 下单选可用券 ② 注意门槛有效期 ③ 部分不叠加。手里有什么券想买什么，帮您算最优～', '{}', 89, 0.88, TRUE, 140),
    ('转人工引导 [seed-034]', 'general', 'closing', '这个问题帮您 @人工客服 处理～稍等马上接入！也可以留联系方式客服处理完联系您～', '{}', 90, 0.9, TRUE, 80),
    ('价格异议应对 [seed-034]', 'pre_sales', 'objection', '理解心情～帮您看更划算的方式：①新人券/优惠券 ②会员折扣 ③凑单满减。预算多少算到手价最优？', '{}', 85, 0.87, TRUE, 70)
) AS v(name, intent, stage, template, vars, priority, confidence, enabled, hit_count)
ON CONFLICT DO NOTHING;

-- ============================================================
-- PART 8: 电商销售 SOP
-- ============================================================
INSERT INTO sop_templates (name, intent, stage, template, vars, priority, confidence, agent_id, enabled, hit_count, created_at, updated_at)
SELECT v.name, v.intent, v.stage, v.template, v.vars, v.priority, v.confidence,
       (SELECT id FROM ai_agents WHERE agent_code = 'hivemtk-agent-ecom-sales' LIMIT 1),
       v.enabled, v.hit_count, NOW(), NOW()
FROM (VALUES
    ('推荐开场 [seed-034]', 'recommend', 'initial', '您好呀～想买什么品类？送礼还是自用？预算多少？帮您精准推荐 2-3 款，每款讲一个独特亮点～', '{}', 95, 0.92, TRUE, 60),
    ('送礼开场 [seed-034]', 'recommend', 'initial', '送礼精选：🎁 送女友/男友 XX；🎁 送父母 XX；🎁 送孩子 XX；🎁 送领导 XX。送谁、预算多少帮您锁定！', '{}', 93, 0.94, TRUE, 75),
    ('价格异议 [seed-034]', 'objection', 'objection', '好问题～贵是因为①更好的材料/工艺 ②带售后服务 ③品牌价值。现在下单有活动，折合和别家差不多，品质售后好很多。算下到手价？', '{}', 92, 0.9, TRUE, 55),
    ('限时逼单 [seed-034]', 'promotion', 'closing', '限时活动！⏰ 还有 XX 小时截止～现在下单立省 XX，还送 [赠品]。活动后恢复原价，错过等下次大促！标记加急发货？', '{}', 94, 0.91, TRUE, 80),
    ('库存逼单 [seed-034]', 'promotion', 'closing', '只剩 XX 件了！🔥 热门色常断货，上次抢不到等了 3 天。现在下单标记 VIP 优先处理！', '{}', 90, 0.87, TRUE, 45),
    ('复购推荐 [seed-034]', 'followup', 'late', '好开心您用得满意！🎉 老客专属：①复购立减 XX ②积分翻倍 ③新品优先体验。上次买的 XX，这次搭配 XX 更划算！', '{}', 88, 0.89, TRUE, 35),
    ('凑满减帮算 [seed-034]', 'promotion', 'middle', '帮您算最优：购物车 XX 元，差 XX 到门槛。建议加 [X 款 XX 元]→凑满→减 XX→实际省 XX。这样最划算～', '{}', 91, 0.92, TRUE, 50)
) AS v(name, intent, stage, template, vars, priority, confidence, enabled, hit_count)
ON CONFLICT DO NOTHING;

-- ============================================================
-- PART 9: 社群运营 SOP
-- ============================================================
INSERT INTO sop_templates (name, intent, stage, template, vars, priority, confidence, agent_id, enabled, hit_count, created_at, updated_at)
SELECT v.name, v.intent, v.stage, v.template, v.vars, v.priority, v.confidence,
       (SELECT id FROM ai_agents WHERE agent_code = 'hivemtk-agent-community' LIMIT 1),
       v.enabled, v.hit_count, NOW(), NOW()
FROM (VALUES
    ('新人欢迎 [seed-034]', 'newbie', 'initial', '欢迎欢迎👏 看群规：① 禁止广告 ② 友好交流 ③ 问题 @管理员。本周活动：[活动名]。有问题随时问我～', '{}', 95, 0.95, TRUE, 100),
    ('活动通知 [seed-034]', 'activity', 'late', '📢 重要通知！🎉 [活动名] ⏰ 时间 XX 📍 形式 XX 🎁 奖品 XX。群内接龙或私信报名～', '{}', 92, 0.9, TRUE, 60),
    ('投诉安抚 [seed-034]', 'general', 'objection', '非常理解 😢 具体是关于：活动没中奖/商品物流/其他群成员？告诉我情况，帮您跟进处理！也可以私信 @群主～', '{}', 93, 0.94, TRUE, 45),
    ('违规温和提醒 [seed-034]', 'rule', 'objection', '温馨提醒～本群不发广告/外链哦，否则会被移出群。产品心得纯文字分享可以～问题 @管理员～', '{}', 85, 0.88, TRUE, 55),
    ('活跃话题 [seed-034]', 'operation', 'initial', '💬 小话题：大家最近都在用啥？有什么好东西推荐？分享好物有积分奖励哦～', '{}', 88, 0.85, TRUE, 30),
    ('转人工引导 [seed-034]', 'general', 'closing', '这个问题帮您 @管理员 处理～管理员尽快回复。也可以私信 @群主 详细沟通。感谢耐心等待！', '{}', 87, 0.9, TRUE, 40)
) AS v(name, intent, stage, template, vars, priority, confidence, enabled, hit_count)
ON CONFLICT DO NOTHING;

-- ============================================================
-- PART 10: 给 7 个行业智能体绑定通用电商 RAG
-- ============================================================
UPDATE ai_agents SET
    rag_product_ids = ARRAY['ecommerce-general-cs']::text[],
    updated_at = NOW()
WHERE agent_code IN (
    'hivemtk-agent-ecig', 'hivemtk-agent-adult', 'hivemtk-agent-sexhealth',
    'hivemtk-agent-carrent', 'hivemtk-agent-homestay',
    'hivemtk-agent-freight', 'hivemtk-agent-immigra'
)
AND (rag_product_ids IS NULL OR rag_product_ids = ARRAY[]::text[]);

-- ============================================================
-- PART 11: 渠道绑定
-- ============================================================
INSERT INTO channel_agent_bindings (channel_type, account_id, agent_id, is_primary, enabled, created_at, updated_at)
SELECT 'telegram', 'default', id, TRUE, TRUE, NOW(), NOW()
FROM ai_agents WHERE agent_code = 'hivemtk-agent-general-cs'
ON CONFLICT DO NOTHING;

INSERT INTO channel_agent_bindings (channel_type, account_id, agent_id, is_primary, enabled, created_at, updated_at)
SELECT 'telegram_group', 'default', id, TRUE, TRUE, NOW(), NOW()
FROM ai_agents WHERE agent_code = 'hivemtk-agent-community'
ON CONFLICT DO NOTHING;

-- ============================================================
-- PART 12: 最终统计查询
-- ============================================================
SELECT '=== 智能体 & FAQ/SOP 统计 ===' AS info;
SELECT a.name, a.agent_code,
       (SELECT COUNT(*) FROM faq_entries f WHERE f.agent_id = a.id) AS faqs,
       (SELECT COUNT(*) FROM sop_templates s WHERE s.agent_id = a.id) AS sops,
       array_to_string(a.rag_product_ids, ',') AS rag
FROM ai_agents a
WHERE a.agent_code LIKE 'hivemtk%' OR a.agent_code LIKE 'seed%'
ORDER BY a.name;

SELECT '=== RAG 知识库 ===' AS info;
SELECT id, name, doc_count, chunk_count, is_active FROM rag_products WHERE id LIKE '%ecommerce%';
