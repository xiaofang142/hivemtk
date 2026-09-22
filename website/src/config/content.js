
export const brand = {
  name: 'HiveMTK',
  fullName: 'HiveMTK · 私域 AI 营销操作系统',
  slogan: '10+ 渠道打透 · AI 真自主 · 数据封死在域内',
  logoText: 'HiveMTK',
}

export const contact = {
  wechatId: 'xiao142000',
  // 站点在 Pages 子路径下，静态资源必须挂在 BASE_URL 上（写死 '/wechat.jpg' 会 404）
  wechatQrPath: `${import.meta.env.BASE_URL}wechat.jpg`,
  email: '',
  phone: '',
  serviceHours: '工作日 09:00-18:00',
}

// 右下角客服浮标与"在线体验"入口随线上域名下线一起删除：
// 两者都指向已停服的 user-server（iframe 聊天窗 / 演示站），保留即死链。

export const hero = {
  eyebrow: [
    { text: 'AGPL-3.0 开源 · 私有化部署', type: 'primary' },
    { text: '七端打透 · AI 真自主', type: 'accent' },
  ],
  title: ['让 AI 替销售,', '从接待到逼单'],
  gradientIndex: 1,
  description:
    'HiveMTK 不是又一个 SCRM，而是真正会卖货的私域 AI 操作系统。本地模型 + 本地数据库 + 零出域架构，七端社媒全打透；ReAct 智能体 42 个工具自主谈单，94 个业务模块开箱即用，内置 GEO 引擎让品牌在 AI 搜索中被看见。AGPL-3.0 开源，4 步私有化部署，把销冠能力复制给团队里每一个普通人。',
  primaryCta: { text: '立即部署', href: '/deploy' },
  secondaryCta: { text: '了解核心功能', href: '/features' },
  // 历史键名 experienceCta 指向已停服的在线体验站；改为源码入口，占同一个视觉位。
  sourceCta: { text: '查看源码', href: 'https://github.com/xiaofang142/hivemtk' },
  stats: [
    { value: '10+', label: '社媒渠道打透', sub: '抖音/快手/小红书/闲鱼/TikTok/企微/邮件/Telegram/WhatsApp/短信' },
    { value: '5', label: 'Telegram 核心能力', sub: 'Bot 收发 · 群线索挖掘 · 主动 DM 触达 · 群消息分析 · 入群管控' },
    { value: '42', label: '智能体工具', sub: 'ReAct 自主决策 + 统一工具注册表' },
    { value: '94', label: '业务模块', sub: '从线索接入到复购激活全链路' },
  ],
  visualCards: [
    { icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><line x1="9" y1="1" x2="9" y2="4"/><line x1="15" y1="1" x2="15" y2="4"/><line x1="9" y1="20" x2="9" y2="23"/><line x1="15" y1="20" x2="15" y2="23"/><line x1="20" y1="9" x2="23" y2="9"/><line x1="20" y1="14" x2="23" y2="14"/><line x1="1" y1="9" x2="4" y2="9"/><line x1="1" y1="14" x2="4" y2="14"/></svg>', title: 'AI 自动谈单', desc: '意图识别 · 异议处理 · 逼单邀约' },
    { icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="2" width="14" height="20" rx="2" ry="2"/><line x1="12" y1="18" x2="12.01" y2="18"/></svg>', title: '多账号聚合', desc: '企微 + 个微统一收件箱' },
    { icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="6"/><circle cx="12" cy="12" r="2"/></svg>', title: '销冠 SOP 智能体', desc: '可视化编排 · 自主决策' },
    { icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="20" x2="12" y2="10"/><line x1="18" y1="20" x2="18" y2="4"/><line x1="6" y1="20" x2="6" y2="16"/></svg>', title: '客户 CDP', desc: '360 画像 · 意向打分 · 旅程地图' },
  ],
  differentiators: [
    {
      num: '01',
      title: '不是管理工具,是会卖货的 AI',
      desc: '多数 SCRM 只做记录留痕,真正谈单的还是人。HiveMTK 的 ReAct 智能体能替销售完成从接待到逼单的大部分动作。',
    },
    {
      num: '02',
      title: '不是玩具 Demo,是能扛生产的工程',
      desc: '42 工具 + 94 模块 + 全链路 TraceID,每次工具调用经 权限→重试→超时→限流→审计 五重装饰器链防护,为真实业务量设计。',
    },
    {
      num: '03',
      title: '不是 SaaS 黑盒,是数据在你手里的开源系统',
      desc: 'AGPL-3.0 开源,完全私有化部署,本地推理栈数据不出域,云端模型 token 计量与成本归集全透明。',
    },
  ],
}

// 六大卖点:私域 / 本地模型 / 本地数据库 / 数据安全 / Token 节省 / 全自动
export const whyHiveMTK = {
  tag: '为什么选 HiveMTK',
  title: ['不是 SaaS 黑盒,', '是数据在你手里的私域 AI'],
  gradientIndex: 1,
  subtitle: '六个让企业敢用、用得起、用得久的硬核理由。每一条都写进了开源代码,不是 PPT 口号。',
  pillars: [
    {
      num: '01',
      key: '私域',
      title: '完全私有化部署',
      desc: '整套系统部署在你自己的服务器上,不依赖任何第三方云服务。从代码到数据,全部资产归你所有,AGPL-3.0 开源协议保障你永远拥有改和用的自由。',
      metric: '100%',
      metricLabel: '资产自有',
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><polyline points="9 22 9 12 15 12 15 22"/></svg>',
    },
    {
      num: '02',
      key: '本地模型',
      title: '本地 LLM 推理,不依赖云端',
      desc: '内置 llama.cpp(Qwen2.5)+ TEI(bge-m3 / bge-reranker)本地推理栈,全部 OpenAI 兼容接口;也可平滑替换 vLLM / Ollama 等后端。AI 能力跑在你自己的机器上,不调一次云 API 也能完整运转。',
      metric: '0',
      metricLabel: '次云端调用',
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><line x1="9" y1="1" x2="9" y2="4"/><line x1="15" y1="1" x2="15" y2="4"/><line x1="9" y1="20" x2="9" y2="23"/><line x1="15" y1="20" x2="15" y2="23"/><line x1="20" y1="9" x2="23" y2="9"/><line x1="20" y1="14" x2="23" y2="14"/><line x1="1" y1="9" x2="4" y2="9"/><line x1="1" y1="14" x2="4" y2="14"/></svg>',
    },
    {
      num: '03',
      key: '本地数据库',
      title: '数据落在你自己的数据库',
      desc: '所有客户资料、聊天记录、SOP 配置、AI 决策日志全部写入你自己的 PostgreSQL。不进我们的服务器、不进第三方云库、不进任何黑盒,你可以随时导出、备份、审计。',
      metric: '0',
      metricLabel: '字节外传',
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg>',
    },
    {
      num: '04',
      key: '数据安全',
      title: '零出域,数据封死在域内',
      desc: '本地推理 + 本地存储 + 本地 RAG 检索,从架构层面保证数据不出域。每次工具调用经权限校验、失败重试、超时控制、速率限制、审计留痕五层防护,操作逐条可追溯。',
      metric: '5 层',
      metricLabel: '工具调用防护链',
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><polyline points="9 12 11 14 15 10"/></svg>',
    },
    {
      num: '05',
      key: 'Token 节省',
      title: '多级缓存 + 本地模型,高频问题零 Token',
      desc: 'LLM 响应缓存、RAG 热缓存、Embedding 缓存、工具结果缓存四级复用,内存 + Redis 两级管理:命中即返回,不消耗一次 token;本地模型推理零边际成本,token 用量按厂商与模型维度精算。',
      metric: '0 元',
      metricLabel: '本地推理边际成本',
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>',
    },
    {
      num: '06',
      key: '全自动',
      title: 'ReAct 智能体真自主决策',
      desc: '不是关键词匹配的伪 AI,是 ReAct 推理-行动循环的真智能体。42 个工具自主编排,从意图识别、异议处理、SOP 跳转、逼单邀约到售后跟进,无需人工干预,失败自动重试 + 熔断降级 + 审计留痕。',
      metric: '42',
      metricLabel: '自主工具',
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="4"/><line x1="12" y1="2" x2="12" y2="6"/><line x1="12" y1="18" x2="12" y2="22"/><line x1="2" y1="12" x2="6" y2="12"/><line x1="18" y1="12" x2="22" y2="12"/></svg>',
    },
  ],
  comparison: {
    title: '本地部署 vs SaaS,差距一眼看清',
    rows: [
      { aspect: '数据存储', local: '你自己服务器上的 PostgreSQL', saas: '厂商云数据库,你看不到', localWin: true },
      { aspect: 'AI 模型', local: '本地 GPU 跑开源模型,可选云端', saas: '只能用厂商提供的模型', localWin: true },
      { aspect: 'Token 成本', local: '本地模型 0 元 + 缓存命中 0 元', saas: '按调用量付费,无缓存', localWin: true },
      { aspect: '数据安全', local: '架构级零出域,出域审计可追溯', saas: '数据在厂商手里,合规风险', localWin: true },
      { aspect: '定制自由', local: 'AGPL-3.0 开源,任意二次开发', saas: '功能等厂商排期,无法改', localWin: true },
      { aspect: '部署门槛', local: 'Docker 一键,4 步完成', saas: '注册即用,零部署', localWin: false },
    ],
  },
}

export const featuresSection = {
  tag: '核心功能',
  title: ['不是又一个 SCRM，', '是真正会卖货的 AI'],
  gradientIndex: 1,
  subtitle:
    'HiveMTK 覆盖从七端多账号聚合、AI 谈单、销冠 SOP、客户 CDP、全渠道触达、GEO 智能优化到数据驾驶舱的完整链路，94 个业务模块 + 42 个智能体工具，让系统真正能替代销冠的重复劳动。',
  features: [
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="2" width="14" height="20" rx="2" ry="2"/><line x1="12" y1="18" x2="12.01" y2="18"/></svg>',
      title: '多账号聚合中枢',
      industries: ['招商加盟', '医美连锁', '教育培训'],
      pain: '传统企业销售团队普遍使用大量个人微信与企微账号，客户分散在员工手机里。管理层既看不到完整聊天记录，也无法防止员工离职带走客户；销售每天切换数十个账号，响应慢、漏单严重，客户资源实质上是“员工资产”而非“企业资产”。',
      solution: '在一个工作台内同时聚合企业微信与个人微信的多账号消息，形成统一收件箱。所有客户沟通记录自动沉淀到企业侧，支持按账号、渠道、客户优先级智能分配，让管理层真正掌握客户资产。',
      special: '自研消息中台 MQ，多账号消息实时聚合入统一收件箱；支持敏感词监控、离职继承、会话存档，把客户资产从“员工口袋”收回到“企业仓库”。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><line x1="9" y1="1" x2="9" y2="4"/><line x1="15" y1="1" x2="15" y2="4"/><line x1="9" y1="20" x2="9" y2="23"/><line x1="15" y1="20" x2="15" y2="23"/><line x1="20" y1="9" x2="23" y2="9"/><line x1="20" y1="14" x2="23" y2="14"/><line x1="1" y1="9" x2="4" y2="9"/><line x1="1" y1="14" x2="4" y2="14"/></svg>',
      title: 'AI 自动谈单引擎',
      industries: ['高客单服务', 'B2B 销售', '本地生活'],
      pain: '私域销售极度依赖人工 1v1 跟进，夜间与节假日无人响应，客户咨询 5 分钟内未回复流失率极高；销售话术参差不齐，新人不敢谈单、老人不愿重复，导致大量线索被浪费。',
      solution: '智能体 7×24 小时自动承接客户咨询，从接待寒暄、需求探询、异议处理到逼单邀约、成交复购，完整模拟销冠谈单节奏，并在关键节点提醒真人销售介入。',
      special: '接入 DeepSeek / 通义千问 / GPT-4o / 智谱 GLM / Kimi 等多家大模型，Dispatcher 网关按场景动态路由：复杂异议用强模型，常规回复用轻模型，故障自动转移，兼顾效果与成本。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="6"/><circle cx="12" cy="12" r="2"/></svg>',
      title: '销冠 SOP 智能体',
      industries: ['保险经纪', '房产中介', '家居定制'],
      pain: '销冠的成交方法长期停留在个人经验和 PPT 培训里，无法沉淀为可复制的流程；新人成长周期长，团队产能两极分化，管理者只能看结果、无法管控过程。',
      solution: '把销冠成交链路可视化编排为 SOP，AI 根据客户当前阶段与反馈自动判断“下一步该做什么、该说什么、该发什么资料”，让普通销售也能按销冠节奏推进客户。',
      special: '销冠话术沉淀进 SOP 状态记忆（L3），AI 按客户阶段检索最优话术；支持发送前合规审核与敏感词拦截，避免过度承诺和客诉风险。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>',
      title: '客户 CDP / 360° 画像',
      industries: ['耐消零售', '汽车后市场', '健康管理'],
      pain: '客户数据散落在 Excel、微信聊天记录、订单系统、广告投放后台中，无法形成统一视图；销售分不清谁是高意向、谁是沉睡客户，只能凭感觉跟进，大量精力浪费在低质量线索上。',
      solution: '基于 OneID 自动归集多渠道客户数据，构建包含基础资料、行为轨迹、沟通记录、成交历史的 360° 画像，并给出意向评分与客户旅程阶段，帮助销售把有限时间投入到最有价值的客户。',
      special: '融合 RFM 模型与行为预测算法，自动分层高意向/培育中/沉睡客户；高意向客户优先人工跟进，低意向客户进入自动化培育 SOP。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><line x1="9" y1="18" x2="15" y2="18"/><line x1="10" y1="22" x2="14" y2="22"/><path d="M15.09 14c.18-.98.65-1.74 1.41-2.5A4.65 4.65 0 0 0 18 8 6 6 0 0 0 6 8c0 1 .23 2.23 1.5 3.5.76.76 1.23 1.52 1.41 2.5"/></svg>',
      title: '销售意向识别与打分',
      industries: ['在线教培', '家装建材', '金融理财'],
      pain: '销售每天面对大量咨询，全靠主观经验判断谁有意向、该优先跟进谁；高意向客户容易被淹没在海量消息中，跟进不及时被竞品截胡。',
      solution: '基于对话内容、行为事件与历史成交数据，AI 实时识别客户意向等级（高/中/低）并给出意向评分，自动打标签、更新画像，同步触发对应的 SOP 分支。',
      special: '意向等级自动判别并写入画像：高意向客户置顶提醒销售优先跟进，中低意向客户进入自动培育池，避免漏掉每一个潜在成交。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>',
      title: '全渠道触达引擎',
      industries: ['电商品牌', '内容创作者', '跨境出海'],
      pain: '企业需要在抖音、快手、小红书、微信、企微、短信、邮件、钉钉等多个渠道触达客户，但各渠道接口独立、规则不同，运营人员要在多个后台之间来回切换，触达效率低且难以统一追踪效果。',
      solution: '统一 13 类触达通道适配器（抖音/快手/小红书/TikTok/闲鱼/微信/企业微信/钉钉/飞书/Telegram/WhatsApp/短信/邮件），智能体通过同一套 reach.* 工具向任意通道发送消息；权限→重试→超时→限流→审计装饰器链保障每次触达稳定可控。',
      special: '抖音/快手/小红书/闲鱼/TikTok 五端经 Chrome 扩展桥接——用你自己的登录态浏览器收发，无需无头浏览器；每条消息携带唯一 TraceID，从入站到回执全链路可观测。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/><line x1="11" y1="8" x2="11" y2="14"/><line x1="8" y1="11" x2="14" y2="11"/></svg>',
      title: 'GEO 智能优化引擎',
      industries: ['出海品牌', 'B2B 获客', '内容营销'],
      pain: 'ChatGPT Search、Perplexity 等 AI 搜索正在分流传统搜索引擎流量，用户开始直接采信 AI 给出的答案。品牌若没被 AI 推荐，就等于在新入口里“隐身”；而传统 SEO 手段对大模型的语料与引用逻辑几乎无效。',
      solution: '内置 GEO（Generative Engine Optimization，生成式引擎优化）全流程闭环：LLM 关键词挖掘与语义扩展 → SEO/GEO 双优化内容生成（E-E-A-T 权威信号增强 + JSON-LD Schema 结构化标记）→ 多模型模拟 AI 搜索验证品牌提及率 → 一键同步发布到掘金/知乎/CSDN/GitHub 等 12 个平台，形成“优化—验证—再优化”的数据飞轮。',
      special: '负面查询监控预警品牌风险；DAG 工作流引擎把上述步骤编排成自动执行流水线；API 成本按厂商/模型维度精算，投入产出可量化。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>',
      title: '私域自动化运营',
      industries: ['快消品牌', '母婴亲子', '美业门店'],
      pain: '私域运营全靠人工发朋友圈、手动群发、逐个激活沉睡客户，大量精力被机械性重复劳动占用；朋友圈内容质量不一、触达时机靠经验，复购与激活效果难以衡量。',
      solution: 'AI 基于销冠人设自动生成朋友圈文案与配图建议，按客户标签与最佳发布时间自动推送；沉睡客户触发自动激活 SOP，实现“种草—触达—转化—复购”的自动化闭环。',
      special: '支持 A/B 测试不同话术与发布时段，高转化内容模板自动沉淀；激活效果实时回流画像与转化漏斗，持续优化策略。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/></svg>',
      title: '沉睡客户激活引擎',
      industries: ['会员制零售', '医疗美容', '健身教育'],
      pain: '老客户和沉睡客户数量庞大，人工逐一唤醒成本极高；传统的群发消息打开率低、转化率差，客户一旦被判定为沉睡就几乎不再被触达。',
      solution: '基于 RFM 分层、行为预测与流失预警模型，自动识别即将流失或已沉睡的客户，触发个性化激活 SOP：朋友圈种草 → 1v1 私聊 → 优惠券触达 → 人工介入，分层唤醒。',
      special: '结合客户历史偏好生成千人千面的激活话术，避免“模板消息”感；激活效果实时回流到画像与漏斗，持续优化策略。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><line x1="10" y1="9" x2="8" y2="9"/></svg>',
      title: '统一话术与素材中心',
      industries: ['连锁门店', '教培机构', '医美集团'],
      pain: '各门店、各销售的话术与素材版本混乱，总部下发的资料到一线就变样；优质话术无法沉淀复用，新人只能自己摸索。',
      solution: '把销冠话术、朋友圈模板、海报、短视频、案例库统一沉淀为素材中心，按场景与渠道一键调用；支持权限管理与版本控制，确保一线始终用最新、最合规的素材。',
      special: '素材使用数据回流，自动识别高转化素材并推荐给对应场景；支持 A/B 测试不同素材组合的转化效果。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>',
      title: '数据驾驶舱',
      industries: ['集团总部', '区域代理', '品牌方'],
      pain: '管理层看不到实时经营数据，只能等周报月报；各系统数据割裂，无法快速定位“哪个环节掉了单、哪支团队产能下滑”。',
      solution: '聚合线索、对话、成交、复购全链路数据，提供实时数据驾驶舱与多维下钻分析，帮助管理者用数据而非感觉做决策。',
      special: '支持自定义指标看板与异常预警；线索转化漏斗、销售产能榜、渠道 ROI 一键查看，决策周期从周级缩短到分钟级。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>',
      title: '权限控制与合规',
      industries: ['金融', '医疗', '政企'],
      pain: '销售过程涉及大量客户隐私与资金沟通，一旦违规承诺或泄露信息，轻则客诉、重则合规风险；管理者难以全程管控。',
      solution: '每个工具配置所需权限，未授权调用自动拒绝；敏感词实时拦截、会话全量存档、操作留痕审计，把合规要求嵌入到每一次沟通动作中。',
      special: '支持按角色、部门、数据范围精细化授权；关键操作二次确认与审批流，满足金融、医疗等行业合规要求。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M22 2L11 13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/></svg>',
      title: 'Telegram 群增长引擎',
      industries: ['跨境出海', '外贸获客', '加密/Web3', '社群运营'],
      pain: 'Telegram 群里每天大量潜在客户发言（"多少钱""怎么买""求购""合作"），全靠人工盯群根本看不过来；Bot 官方限制不能主动 DM 未交互用户，群里客户只能沉睡。',
      solution: '内置群增长完整闭环：① Bot 协议直连（Webhook/Polling 双模式兜底）→ ② 群发言静默打分（中英双语 + 联系方式信号，0-100）→ ③ 高意向（score≥60）自动触发个性化 DM 触达 → ④ 群消息全量入库 + AI 智能回复 → ⑤ 入群管控强制用户先 /start 打通 DM 前置。',
      special: '解决了 Telegram Bot 不能主动 DM 的官方限制——Gate 管控链（join_request 私密群申请 / mute_unlock 公开群禁言验证）强制用户与 Bot 建立首交互，后续就能自由 DM 触达。静默运行零刷屏，把群里每句"多少钱"自动变成私域线索。',
    },
  ],
}

export const toolchainSection = {
  tag: '工程能力',
  title: ['不是玩具 Demo，', '是能扛生产的工程'],
  gradientIndex: 1,
  subtitle:
    '从消息中台、大模型路由、私域自动化到可观测性，每一层都为真实业务量设计，支持私有化部署与水平扩展。',
  toolchain: [
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/><polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" y1="22.08" x2="12" y2="12"/></svg>',
      name: '消息中台 MQ',
      desc: '自研消息中间件，多账号消息聚合入统一收件箱，WebSocket 实时推送，支撑高并发私域沟通。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M12 1v6m0 6v6m11-7h-6m-6 0H1"/></svg>',
      name: '大模型路由网关',
      desc: 'Dispatcher 按场景动态路由 DeepSeek / 通义千问 / GPT-4o / 智谱 GLM / Kimi / 本地模型，熔断降级故障转移，token 计量成本归集。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>',
      name: '私域自动化引擎',
      desc: 'SOP 编排、定时触达、条件分支、A/B 测试，营销流程可拖拽配置。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>',
      name: '客户 CDP',
      desc: 'OneID 归并、360° 画像、RFM 分层与意向打分，沉淀企业客户资产。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M3 3v18h18"/><path d="M7 14l4-4 4 4 4-6"/></svg>',
      name: '数据驾驶舱',
      desc: '全链路指标看板与异常预警，决策周期从周级缩短到分钟级。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M22 12h-4l-3 9L9 3l-3 9H2"/></svg>',
      name: '可观测 Pipeline',
      desc: '触达全链路 TraceID，限流/重试/降级/审计/计费 9 步保障稳定可控。',
    },
  ],
  architecture: {
    title: '六层协同架构',
    layers: [
      { role: '接入与渠道层', desc: '13 类通道适配器统一入站，Webhook/WebSocket → InboxIngressService。' },
      { role: '智能体运行时', desc: 'AgentRuntime 事件订阅 + AgentContext 加载 + 消息路由。' },
      { role: '智能体引擎', desc: 'InferenceCycle：感知 → 对齐 → 门禁 → 规划 → 行动 → 复盘。' },
      { role: '工具注册表', desc: 'ToolRouter 统一路由，42 个注册工具经 权限→重试→超时→限流→审计 装饰器链防护。' },
      { role: '记忆系统', desc: 'L1 短期 / L2 长期 / L3 SOP / L4 业务 四层记忆，L2 结合 pgvector 语义检索。' },
      { role: 'AI 算力底座', desc: 'Embedding + pgvector + 混合检索 RAG + LLM Dispatcher 调度 + 故障切换。' },
    ],
  },
  example: {
    title: 'ReAct 智能体编排示例',
    desc: '智能体引擎按 Thought → Action → Observation 循环自主决策，调用工具注册表完成多步任务，全程携带 TraceID 可观测。',
    code: `// InferenceCycle.RunOnce 主编排
cycle := &InferenceCycle{Runtime: rt, Ctx: agentCtx}
for !cycle.Done() {
    thought := cycle.Perceive(msg)       // 感知：情绪 + 意图
    aligned := cycle.Align(thought)       // 对齐：6 维拟人度
    if cycle.Gatekeeper(aligned) {        // 门禁：危机 → 转人工
        return handoffToHuman()
    }
    plan := cycle.Plan(aligned)           // 规划：任务/工具
    result := cycle.Bridge.Execute(plan)  // 执行：SalesEngine / SmartCS
    cycle.Observe(result)                 // 观察 → 记忆写入
}
// 全程 TraceID 贯穿，限流/重试/审计/计费由装饰器链保障`,
  },
  guarantees: [
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>',
      title: '全链路 TraceID',
      desc: '每条消息与工具调用携带唯一 TraceID，从入站到回复全链路可追溯。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>',
      title: '限流 / 重试 / 降级',
      desc: 'ToolRouter 内置限流、熔断、重试与降级策略，保障高并发下稳定可控。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/></svg>',
      title: '审计与成本归集',
      desc: '装饰器链自动记录工具调用审计日志，token 成本按厂商/模型维度归集统计。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>',
      title: '私有化部署',
      desc: 'AGPL-3.0 开源，支持完全私有化，本地推理栈数据不出域。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 3 21 3 21 9"/><polyline points="9 21 3 21 3 15"/><line x1="21" y1="3" x2="14" y2="10"/><line x1="3" y1="21" x2="10" y2="14"/></svg>',
      title: '水平扩展',
      desc: '无状态服务 + PostgreSQL + Redis，按业务量水平扩容，支持集群部署。',
    },
  ],
  categories: [
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M22 2v6h-6"/><path d="M2 12a10 10 0 1 1 3 7"/></svg>',
      title: '全渠道触达工具',
      count: 20,
      desc: '13 类通道发送，外加批量、定时、撤回、账号健康度等运营工具。',
      tools: [
        { name: 'reach.douyin.send 等 13 个', desc: '抖音/快手/小红书/TikTok/闲鱼/微信/企微/钉钉/飞书/Telegram/WhatsApp/短信/邮件 单发' },
        { name: 'reach.batch', desc: '多客户批量触达' },
        { name: 'reach.schedule', desc: '定时触达任务' },
        { name: 'reach.recall', desc: '消息撤回' },
      ],
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M12 1v6m0 6v6m11-7h-6m-6 0H1"/></svg>',
      title: '客户资产工具',
      count: 8,
      desc: 'OneID 归并、360° 画像检索、分群与标签管理。',
      tools: [
        { name: 'customer.search', desc: '客户检索与画像查询' },
        { name: 'customer.merge', desc: '多渠道身份归并 OneID' },
        { name: 'customer.segment', desc: '客群分层' },
        { name: 'customer.add_tag', desc: '打标签（含 remove_tag/update/create/get）' },
      ],
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>',
      title: '知识检索工具',
      count: 4,
      desc: '知识库管理与混合检索 RAG。',
      tools: [
        { name: 'rag.search', desc: '向量 + 关键词混合检索知识库' },
        { name: 'knowledge.add_doc', desc: '文档入库' },
        { name: 'knowledge.list_kb', desc: '知识库列表' },
        { name: 'knowledge.feedback', desc: '检索结果反馈' },
      ],
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>',
      title: '业务系统工具',
      count: 6,
      desc: '订单、物流、售后等业务系统直连查询与操作。',
      tools: [
        { name: 'order.lookup', desc: '订单查询' },
        { name: 'logistics.track', desc: '物流跟踪' },
        { name: 'aftersale.query', desc: '售后进度查询' },
        { name: 'aftersale.create', desc: '创建售后工单' },
      ],
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>',
      title: '私信会话工具',
      count: 3,
      desc: '私信发送与会话生命周期管理。',
      tools: [
        { name: 'pm.send_message', desc: '私信发送' },
        { name: 'pm.open_session', desc: '开启会话' },
      ],
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><line x1="3" y1="9" x2="21" y2="9"/><line x1="9" y1="21" x2="9" y2="9"/></svg>',
      title: '展示卡片',
      count: 1,
      desc: '结构化卡片渲染。',
      tools: [
        { name: 'card.show', desc: '商品/名片卡片展示' },
      ],
    },
  ],
}

export const workflowSection = {
  tag: '工作流',
  title: ['一条线索，', '从接住到成交的自动化'],
  gradientIndex: 1,
  subtitle:
    'AI 在每一步自动承接、判断与推进，最终把高意向客户交给真人销售，把重复性劳动留给系统。',
  steps: [
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>',
      title: '线索接入与识别',
      desc: '多渠道线索统一接入，AI 实时识别意向等级并自动打标签、更新画像。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>',
      title: 'AI 自动谈单',
      desc: '智能体 7×24 承接咨询，完成寒暄、探需、异议处理与逼单邀约。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>',
      title: 'SOP 智能推进',
      desc: '按客户阶段自动执行对应 SOP 分支，把普通销售带入销冠节奏。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>',
      title: '内容自动生成',
      desc: '销冠人设的朋友圈文案、海报、短视频脚本一键生成并定时发布。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>',
      title: '人工介入成交',
      desc: '高意向客户由真人销售 1v1 推进成交，中低意向客户进入自动培育池。',
    },
    {
      icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M3 3v18h18"/><path d="M19 9l-5 5-4-4-3 3"/></svg>',
      title: '复购与激活',
      desc: '沉睡客户触发自动激活 SOP，成交客户进入复购旅程，形成增长闭环。',
    },
  ],
}

export const faqSection = {
  tag: '常见问题',
  title: ['你可能关心的', '几个问题'],
  gradientIndex: 1,
  subtitle: 'HiveMTK 10+ 渠道打透 + AI 真自主 + 数据封死在域内，落地私域最常被问到的问题一次说清。',
  faqs: [
    {
      q: '和传统 SCRM / 企微 SaaS 有什么区别？',
      a: '多数 SCRM 只是“管理工具”，把记录留痕做得很重，但真正谈单的还是人。HiveMTK 的核心是 ReAct 自主智能体（42 工具）与销冠 SOP——能替销售完成从接待到逼单的大部分动作，把销冠能力复制给团队里每一个普通人。',
    },
    {
      q: '支持哪些大模型？',
      a: '已接入 DeepSeek、通义千问、GPT-4o、智谱 GLM、Kimi 等主流云端模型，并内置 llama.cpp + Qwen2.5 本地推理栈（OpenAI 兼容接口，可替换 vLLM/Ollama）；Dispatcher 网关按场景动态路由、故障自动转移，Embedding/Rerank 默认本地运行。',
    },
    {
      q: '什么是 GEO？和 SEO 有什么区别？',
      a: 'GEO（Generative Engine Optimization，生成式引擎优化）面向 ChatGPT Search、Perplexity 等 AI 搜索引擎——目标是让品牌在被大模型引用作答时被提及、被正面表述。SEO 争的是搜索结果排名，GEO 争的是 AI 答案里的“席位”。HiveMTK 内置关键词蒸馏、内容 E-E-A-T 增强、Schema 结构化标记、多模型验证与 12 平台分发的完整闭环。',
    },
    {
      q: '数据安全如何保障？',
      a: '基于 AGPL-3.0 开源，支持完全私有化部署，数据默认留存企业内部（如启用云端大模型，对话内容将按你配置的 LLM 服务流转）；提供敏感词拦截、会话存档、操作审计与精细化权限控制，满足金融、医疗等行业合规要求。',
    },
    {
      q: '需要技术团队才能部署吗？',
      a: '提供 Docker 一键部署与源码部署，按文档 4 步即可完成部署；如需深度定制或集群部署，我们也提供实施支持。',
    },
    {
      q: '七端是哪七端？',
      a: '抖音、快手、小红书、闲鱼、TikTok、企业微信、邮件，共 7 个社媒/沟通渠道统一接入，多账号聚合到统一消息中台。',
    },
    {
      q: '和现有 CRM / 订单系统能打通吗？',
      a: '提供开放 API 与 Webhook，可对接主流 CRM、ERP 与订单系统，把客户数据与成交结果回流到统一画像。',
    },
    {
      q: 'AI 自动回复会不会被平台封号？',
      a: '社媒五端通过你自己的登录态浏览器 + Chrome 扩展桥接收发消息（非无头浏览器、非第三方协议），内置频率控制、随机时延与话术多样化，贴近真人节奏；关键节点支持人工确认，规避平台风控。',
    },
    {
      q: '开源协议是什么？商用有什么限制？',
      a: 'HiveMTK 基于 AGPL-3.0 开源，可自由 fork、二次开发、商业使用。注意 AGPL-3.0 要求：修改后的网络服务代码必须同样开源。详见仓库根目录 LICENSE 文件。',
    },
    {
      q: '如何开始使用？',
      a: '克隆开源仓库（GitHub / Gitee）按部署文档 4 步即可私有化部署体验完整功能；也可联系作者获取演示环境与行业落地方案。',
    },
    {
      q: '支持多语言 / 出海业务吗？',
      a: '官网与系统支持中 / 英 / 日 / 阿语，全渠道触达引擎可对接 TikTok、WhatsApp、Telegram 等海外平台，适配跨境出海场景。',
    },
    {
      q: '后续会持续更新吗？',
      a: '会持续迭代模型路由、SOP 能力与渠道适配器，并按行业沉淀更多开箱即用的销冠话术模板。',
    },
  ],
}

export const techSpecs = {
  tag: '技术规格',
  title: ['私有化部署，', '数据与模型都留在你手里'],
  gradientIndex: 1,
  specs: [
    { label: '部署形态', value: 'Docker / 源码，支持私有化与集群，4 步完成部署' },
    { label: '前端', value: 'Vue 3 + Vite + JavaScript（Element Plus + Pinia）' },
    { label: '后端', value: 'Go 1.25 + 五层架构（Controller→Service→Repository→Model）+ PostgreSQL 15 + Redis 7' },
    { label: '智能体', value: 'ReAct 自主智能体（42 注册工具）+ 94 业务模块' },
    { label: 'RAG 检索', value: 'pgvector HNSW 向量 + BM25 关键词混合召回（RRF 融合）+ bge-reranker-v2-m3 精排' },
    { label: 'GEO 优化', value: '关键词蒸馏 → 内容生成（E-E-A-T / Schema）→ AI 搜索验证 → 12 平台分发闭环' },
    { label: '模型接入', value: 'DeepSeek / 通义千问 / GPT-4o / 智谱 GLM / Kimi / 本地 llama.cpp（Qwen2.5）' },
    { label: '渠道适配', value: '抖音/快手/小红书/闲鱼/TikTok/企微/邮件/Telegram/WhatsApp/短信 等 10+ 社媒渠道' },
    { label: '可观测', value: '全链路 TraceID、权限/重试/超时/限流/审计装饰器链' },
    { label: '开源协议', value: 'AGPL-3.0（强 copyleft 开源协议，默认本地推理栈数据不出域）' },
  ],
}

export const deploySection = {
  tag: '部署指南',
  title: ['4 步完成部署', '开箱即用'],
  gradientIndex: 1,
  subtitle: '基于 AGPL-3.0 开源，无授权码、无版本下载、无任何收费环节。Docker 一键启动或源码部署，源码与文档均在 GitHub / Gitee 公开托管。',
  cards: [
    { icon: 'package', title: 'Docker 部署', desc: '环境隔离、可一键启停、无需手动配置依赖。', cta: '查看 Docker 部署', link: '/deploy' },
    { icon: 'code', title: '源码仓库', desc: '完整源码，可自由修改、二次开发与贡献。', cta: '前往 Gitee', link: 'https://gitee.com/xhpmayun/hivemtk' },
    { icon: 'book', title: '安装文档', desc: 'Docker 部署、源码部署、FRP 穿透与配置说明。', cta: '查看安装文档', link: '/docs' },
  ],
}

export const footer = {
  resourceLinks: [
    { label: '业务流程', href: '#workflow' },
  ],
  contactCol: {
    title: '获取源码',
    desc: '基于 AGPL-3.0 开源，克隆仓库即可私有化部署，数据与模型均留存企业内部。',
    cta: { label: '前往 Gitee 仓库', href: 'https://gitee.com/xhpmayun/hivemtk' },
  },
  copyright: '© 2026 HiveMTK · 私域 AI 营销操作系统（AGPL-3.0 开源）',
  icp: '',
  police: '',
  officialAccount: '',
  officialGroup: '',
  officialLinks: [],
}

export const nav = {
  brand: 'HiveMTK',
  links: [
    { label: '核心功能', href: '/features', type: 'route' },
    { label: '工程能力', href: '/toolchain', type: 'route' },
    { label: '工作流', href: '/workflow', type: 'route' },
    { label: '常见问题', href: '/faq', type: 'route' },
    { label: '部署指南', href: '/deploy', type: 'route' },
    { label: '安装文档', href: '/docs', type: 'route' },
  ],
  cta: { label: '立即部署', href: '/deploy', type: 'route' },
}

