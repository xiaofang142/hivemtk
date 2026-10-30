
// 云端版（SaaS 多租户形态）对外入口：全站单点常量（= 用户端应用 work 子域）
export const CLOUD_URL = 'https://work.xapptool.cn'

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
    'HiveMTK 不是又一个 SCRM，而是真正会卖货的私域 AI 操作系统。本地模型 + 本地数据库 + 零出域架构，七端社媒全打透；ReAct 智能体 45 个工具自主谈单，94 个业务模块开箱即用，内置 GEO 引擎让品牌在 AI 搜索中被看见。AGPL-3.0 开源，4 步私有化部署，把销冠能力复制给团队里每一个普通人。',
  primaryCta: { text: '立即部署', href: '/deploy' },
  secondaryCta: { text: '了解核心功能', href: '/features' },
  // 历史键名 experienceCta 指向已停服的在线体验站；改为源码入口，占同一个视觉位。
  sourceCta: { text: '查看源码', href: 'https://github.com/xiaofang142/hivemtk' },
  // 云端版在线使用入口（单点常量 CLOUD_URL），复用 sourceCta 同级样式类
  cloudCta: { text: '云端版在线使用', href: CLOUD_URL },
  stats: [
    { value: '10+', label: '社媒渠道打透', sub: '抖音/快手/小红书/闲鱼/TikTok/企微/邮件/Telegram/WhatsApp/短信' },
    { value: '5', label: 'Telegram 核心能力', sub: 'Bot 收发 · 群线索挖掘 · 主动 DM 触达 · 群消息分析 · 入群管控' },
    { value: '45', label: '智能体工具', sub: 'ReAct 自主决策 + 统一工具注册表' },
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
      desc: '45 工具 + 94 模块 + 全链路 TraceID,每次工具调用经 权限→重试→超时→限流→审计 五重装饰器链防护,为真实业务量设计。',
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
      desc: '不是关键词匹配的伪 AI,是 ReAct 推理-行动循环的真智能体。45 个工具自主编排,从意图识别、异议处理、SOP 跳转、逼单邀约到售后跟进,无需人工干预,失败自动重试 + 熔断降级 + 审计留痕。',
      metric: '45',
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
    'HiveMTK 覆盖从七端多账号聚合、AI 谈单、销冠 SOP、客户 CDP、全渠道触达、GEO 智能优化到数据驾驶舱的完整链路，94 个业务模块 + 45 个智能体工具，让系统真正能替代销冠的重复劳动。',
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
      { role: '工具注册表', desc: 'ToolRouter 统一路由，45 个注册工具经 权限→重试→超时→限流→审计 装饰器链防护。' },
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
      a: '多数 SCRM 只是“管理工具”，把记录留痕做得很重，但真正谈单的还是人。HiveMTK 的核心是 ReAct 自主智能体（45 工具）与销冠 SOP——能替销售完成从接待到逼单的大部分动作，把销冠能力复制给团队里每一个普通人。',
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
      a: '提供 Docker 部署与源码部署，按文档 4 步即可完成部署；如需深度定制或集群部署，我们也提供实施支持。',
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
    { label: '智能体', value: 'ReAct 自主智能体（45 注册工具）+ 94 业务模块' },
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
  subtitle: '基于 AGPL-3.0 开源，无授权码、无版本下载：代码与全部功能模块免费开放，17 个增值模块中 16 个 0 元、仅浏览器自动化 ¥1 买断；云端版软件订阅 9 元，新商户 15 天体验期并赠 10M 模型 token。make install 起服务或源码部署，源码与文档均在 GitHub / Gitee 公开托管。',
  cards: [
    { icon: 'package', title: 'Docker 部署', desc: '数据层容器一键启停，推理栈与后端跑在宿主机。', cta: '查看 Docker 部署', link: '/deploy' },
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

// —— 功能模块清单（事实源：user-server/internal/model/module_bindings.go）——
// 24 个模块 = 7 基础（免费常开）+ 17 增值（按需开通）；menus 为该模块绑定的菜单节点数，
// 合计 212 个节点，由测试 TestModuleMenuBindingsCoverAllMenuKeys 钉死全覆盖。
// priceCents 为源码内置默认定价（整数分）；与源码核对日期：2026-10-30
// （2026-10-30 用户裁决：7 个基础模块 0 元常开；17 个增值模块中 16 个 0 元，
//   仅浏览器自动化 ¥1 买断）。
export const moduleCatalog = [
  { key: 'core', name: '系统基座', category: 'base', priceCents: 0, billing: 'base', menus: 27,
    summary: '账号权限、站点配置、审计与备份，系统可运行的底线', depends: [] },
  { key: 'customer', name: '客户中心', category: 'base', priceCents: 0, billing: 'base', menus: 13,
    summary: '线索、客户 360、标签分层、RFM 分群与 OneID 身份归一', depends: ['系统基座'] },
  { key: 'inbox', name: '会话工作台', category: 'base', priceCents: 0, billing: 'base', menus: 11,
    summary: '统一收件箱、客服会话、坐席状态与快捷回复', depends: ['系统基座', '客户中心'] },
  { key: 'knowledge', name: '知识库', category: 'base', priceCents: 0, billing: 'base', menus: 18,
    summary: '文档入库、分段检索、RAG 评测与对外 API Token', depends: ['系统基座'] },
  { key: 'asset', name: '资产与话术', category: 'base', priceCents: 0, billing: 'base', menus: 9,
    summary: '资产市场、资产包 Playground 与销冠话术库', depends: ['系统基座', '知识库'] },
  { key: 'llm-self', name: '自有模型接入', category: 'base', priceCents: 0, billing: 'base', menus: 1,
    summary: '用自己的 API Key 接入大模型，用量与成本可视化，不额外计费', depends: ['系统基座'] },
  { key: 'platform-link', name: '平台对接', category: 'base', priceCents: 0, billing: 'base', menus: 4,
    summary: '平台心跳、功能模块下发与开通状态同步；离线部署时全部基础能力直接可用', depends: ['系统基座'] },

  { key: 'ai-agent', name: 'AI 智能体', category: 'paid', priceCents: 0, billing: 'yearly', menus: 5,
    summary: '7×24 自动应答真实客户消息，可绑定不同模型、知识库与 SOP',
    depends: ['系统基座', '客户中心', '会话工作台', '知识库', '自有模型接入'] },
  { key: 'ai-sales', name: 'AI 销冠', category: 'paid', priceCents: 0, billing: 'yearly', menus: 14,
    summary: '意图识别、异议应对、销冠画像与 SOP 自动推进成交',
    depends: ['系统基座', '客户中心', '会话工作台', '自有模型接入', 'AI 智能体'] },
  { key: 'llm-cloud', name: '平台模型算力', category: 'paid', priceCents: 0, billing: 'metered', menus: 0,
    summary: '不用自己申请 Key，用平台垫资的大模型；按 token 预付费，输入输出分别计价',
    depends: ['系统基座', '自有模型接入'] },
  { key: 'reach-auto', name: '营销自动化', category: 'paid', priceCents: 0, billing: 'yearly', menus: 6,
    summary: '线索进来自动跑多轮触达与工作流编排',
    depends: ['系统基座', '客户中心'] },
  { key: 'reach-sms', name: '短信触达', category: 'paid', priceCents: 0, billing: 'yearly', menus: 5,
    summary: '国内短信发送、任务调度与退订管理',
    depends: ['系统基座', '客户中心', '营销自动化'] },
  { key: 'reach-email', name: '邮件触达', category: 'paid', priceCents: 0, billing: 'yearly', menus: 8,
    summary: '邮件列表、SMTP、草稿任务与送达率追踪',
    depends: ['系统基座', '客户中心', '营销自动化'] },
  { key: 'reach-social', name: '社媒引流卡片', category: 'paid', priceCents: 0, billing: 'yearly', menus: 15,
    summary: '抖音 / 快手 / 小红书 / 闲鱼 / TikTok 引流卡片与自动私信回复',
    depends: ['系统基座', '客户中心', '营销自动化'] },
  { key: 'reach-link', name: '短链活码', category: 'paid', priceCents: 0, billing: 'yearly', menus: 5,
    summary: '可追踪短链、活码投放与域名池健康检测', depends: ['系统基座'] },
  { key: 'im-wecom', name: '企业微信', category: 'paid', priceCents: 0, billing: 'yearly', menus: 1,
    summary: '企微账号托管、客户群运营与健康度风控',
    depends: ['系统基座', '客户中心', '会话工作台'] },
  { key: 'im-intl', name: '海外与协作 IM', category: 'paid', priceCents: 0, billing: 'yearly', menus: 17,
    summary: 'WhatsApp / Telegram / QQ / 飞书 / 钉钉 / 社群 多通道收发',
    depends: ['系统基座', '客户中心', '会话工作台'] },
  { key: 'analytics', name: '数据分析', category: 'paid', priceCents: 0, billing: 'yearly', menus: 7,
    summary: '数据大屏、转化漏斗、AI 产能与自定义报表',
    depends: ['系统基座', '客户中心', '会话工作台'] },
  { key: 'experiment', name: '实验与预测', category: 'paid', priceCents: 0, billing: 'yearly', menus: 2,
    summary: 'A/B 实验（含多臂老虎机）与流失预警',
    depends: ['系统基座', '客户中心', '数据分析'] },
  { key: 'browser-automation', name: '浏览器自动化', category: 'paid', priceCents: 100, billing: 'once', menus: 5,
    summary: '常驻浏览器接管真实客户端做私信与评论',
    depends: ['系统基座', '客户中心'] },
  { key: 'geo', name: 'GEO 智能优化', category: 'paid', priceCents: 0, billing: 'yearly', menus: 30,
    summary: 'AI 搜索可见性优化：关键词蒸馏、内容生成、收录与 SOV 监控',
    depends: ['系统基座', '知识库'] },
  { key: 'sales-crm', name: '销售工作台', category: 'paid', priceCents: 0, billing: 'yearly', menus: 4,
    summary: '销售驾驶舱、跟进、商机报价与回款闭环',
    depends: ['系统基座', '客户中心', 'AI 销冠'] },
  { key: 'approval', name: '审批与待办', category: 'paid', priceCents: 0, billing: 'yearly', menus: 2,
    summary: '待办中心、Bad Case 队列与人工任务分派', depends: ['系统基座'] },
  { key: 'i18n', name: '多语言管理', category: 'paid', priceCents: 0, billing: 'yearly', menus: 3,
    summary: '术语表与多语言监控看板', depends: ['系统基座'] },
]

// —— /modules 功能架构页文案 ——
export const modulesPage = {
  kicker: '功能架构',
  title: ['24 个功能模块，', '212 个菜单节点'],
  gradientIndex: 1,
  subtitle:
    'HiveMTK 的功能不是堆在一起的：212 个页面节点全部归属到 24 个功能模块，模块决定菜单、接口、定时任务与智能体工具是否放行。7 个基础模块免费常开；17 个增值模块中 16 个 0 元、仅浏览器自动化 ¥1 买断——以下清单与源码 module_bindings.go 一一对应。',
  stats: [
    { value: '24', label: '功能模块', sub: '7 基础 + 17 增值' },
    { value: '212', label: '菜单节点', sub: '测试钉死全覆盖' },
    { value: '45', label: '智能体工具', sub: '按模块过滤放行' },
    { value: '2', label: '部署形态', sub: '离线单机 / SaaS 多租户' },
  ],
  modesTitle: ['一套代码，', '两种部署形态'],
  modesDesc:
    '开关决定形态，功能对齐：离线单机是默认形态（零出域），SaaS 多租户面向自己运营多租户平台的场景，数据都留在你部署的数据库里。',
  modes: [
    {
      title: '离线单机（默认形态）',
      tags: ['TENANT_MODE=off', '零出域', '注册入口关闭'],
      desc: '单租户运行，租户自助注册恒返回 4104；PLATFORM_ENABLED=false 时基础模块全开、0 元模块自动开通、定价大于 0 的增值模块锁定，系统不发出任何平台请求。',
    },
    {
      title: 'SaaS 多租户形态',
      tags: ['TENANT_MODE=on', '行级数据隔离', '监管登记'],
      desc: '注册即开户：登记公司名称、负责人、电话、邮箱并完成监管告知勾选；数据隔离由数据访问层统一强制（业务代码零手工隔离逻辑），用户名租户内唯一、邮箱跨租户全局唯一。',
    },
  ],
  baseTitle: ['基础模块 ·', '免费常开'],
  baseDesc: '7 个基础模块构成系统基座：免费、不可关闭——关掉任何一个，用户都无法完成最小业务闭环。',
  paidTitle: ['增值模块 ·', '16 个 0 元'],
  paidDesc:
    '17 个增值模块中 16 个 0 元开通（平台模型算力模块本身 0 元，token 按输入 / 输出预付费计量），仅浏览器自动化 ¥1 买断。开通时基础依赖自动补齐，付费依赖须显式勾选。',
  cycleYearly: '年付',
  cycleMetered: '按量预付费',
  cycleOnce: '买断',
  mechTitle: ['运行机制 ·', '闸写在代码里'],
  mechanics: [
    {
      title: '模块闸',
      desc: '执行顺序为 登录态 → 模块闸 → 角色权限，超管也过闸；未开通模块的接口返回 403 与业务码 module_not_enabled，不会出现“菜单看得见、点进去才报错”。',
    },
    {
      title: '菜单与任务过滤',
      desc: '菜单树先按已开通模块裁剪、再按角色授权裁剪；定时任务、智能体工具与事件订阅同样按开通状态过滤，未开通的能力不会在后台空转。',
    },
    {
      title: '依赖闭包',
      desc: '每个模块声明直接依赖（如 AI 销冠依赖 AI 智能体）；开通时基础依赖自动开通，付费依赖必须显式勾选并在确认弹窗列明，避免意外收费。',
    },
    {
      title: '同步与降级',
      desc: '开通状态与心跳同频拉取（默认 10 分钟），平台不可达时自动降级用本地镜像，已开通能力不中断。',
    },
  ],
  sourceNote:
    '清单事实源：user-server/internal/model/module_bindings.go（24 个模块 × 212 个菜单节点），由测试 TestModuleMenuBindingsCoverAllMenuKeys 钉死全覆盖；与源码核对日期 2026-10-30（24 个模块中仅浏览器自动化 ¥1 买断，其余全部 0 元）。',
  ctaModules: { label: '查看核心功能', href: '/features' },
  ctaPricing: { label: '了解按需订阅', href: '/pricing' },
}

// —— /pricing 按需订阅页文案 ——
export const pricingPage = {
  kicker: '按需订阅',
  title: ['功能模块 23 个免费，', '仅浏览器自动化 ¥1'],
  gradientIndex: 1,
  subtitle:
    'AGPL-3.0 源码全量免费；24 个功能模块中 7 个基础模块 0 元常开、16 个增值模块 0 元，仅浏览器自动化 ¥1 买断。云端版软件订阅 9 元，新商户 15 天体验期并赠 10M 模型 token；模型算力按输入 / 输出 token 分列计费。不打包套餐、不做功能阉割，代码始终全量开源。',
  // —— 页顶云端引导条（T024 四入口之一，复用 page-cta-band 现有样式）——
  cloudBand: {
    desc: '不想自运维？云端版（SaaS 多租户形态）在线使用：7 个基础模块免费常开、16 个增值模块 0 元，仅浏览器自动化 ¥1 买断，软件订阅 9 元，新商户注册即 15 天体验期并赠 10M 模型 token。',
    label: '云端版在线使用',
    href: CLOUD_URL,
  },
  models: [
    {
      title: '开源基座',
      price: '免费',
      note: '无授权码 · 无版本下载',
      desc: 'AGPL-3.0 全量源码，GitHub / Gitee 直接克隆；私有化部署、二次开发、商用均自由，仅需遵循协议的开源义务。',
    },
    {
      title: '基础功能模块',
      price: '免费',
      note: '7 个模块 · 永久常开',
      desc: '系统基座、客户中心、会话工作台、知识库、资产与话术、自有模型接入、平台对接——免费且不可关闭；自带 API Key 调用大模型只统计用量、不额外计费。',
    },
    {
      title: '增值功能模块',
      price: '免费',
      note: '24 个中 23 个 0 元 · 无套餐包',
      desc: '16 个增值模块 0 元开通，仅浏览器自动化 ¥1 买断；平台模型算力模块本身 0 元，token 按输入 / 输出预付费计量。',
    },
  ],
  tableTitle: ['增值模块价目表', '（默认定价）'],
  tableDesc:
    '以下为源码内置的默认定价（人民币，按整数分存储）：24 个功能模块中 23 个 0 元（7 个基础模块常开 + 16 个增值模块），仅浏览器自动化 ¥1 买断、不设到期日；订阅前可在系统内的功能模块市场查看详情、依赖闭包与覆盖页面。',
  cycleYearly: '年付',
  cycleMetered: '按量预付费',
  cycleOnce: '买断',
  colModule: '模块',
  colSummary: '能力',
  colPrice: '订阅价',
  colCycle: '计费',
  colMenus: '覆盖页面',
  // —— 计费口径（003 计费二期重定价后的三条花费路线，金额为默认定价，非套餐价）——
  budgetTitle: ['计费口径 ·', '仅浏览器自动化收费'],
  budgetDesc:
    '2026-10-30 定价收口：24 个模块中 23 个 0 元（7 个基础模块常开 + 16 个增值模块），仅浏览器自动化 ¥1 买断；软件订阅云端版 9 元，新商户 15 天体验期并赠 10M 模型 token；LLM 按输入 / 输出 token 分列计费，套餐体验包 ¥1 / 标准包 ¥3 / 专业包 ¥6 / 旗舰包 ¥9。以下三条路线的金额均为源码与平台的默认定价，不是套餐价，也没有必须一次买齐的门槛。',
  budgetPaths: [
    {
      name: '¥0 私有化线',
      subtitle: '离线单机 · 开源免费',
      price: '¥0',
      priceNote: '功能模块全部免费',
      includes: [
        'AGPL-3.0 全量源码 + 7 个基础模块 0 元常开 + 16 个增值模块 0 元开通',
        '自带 API Key 只统计不计费，或直接跑本地推理栈，模型成本为零',
        '浏览器自动化（¥1 买断）在离线部署下保持锁定——本地无支付通道，不做授权码旁路',
      ],
      scene: '适合：技术型团队 / 数据零出域要求',
    },
    {
      name: '9 元 云端版',
      subtitle: '云端版在线使用 · 按月订阅',
      price: '9 元',
      priceNote: '新商户 15 天体验期',
      includes: [
        '新商户注册即 15 天体验期，体验期内等同订阅有效，全功能可试',
        '按月续费顺延（¥9 / 30 天）；到期整站业务锁定（登录、模块市场、续费与支付查询仍可用），续费即恢复、数据不删',
        '云端版内 7 个基础模块 0 元常开、16 个增值模块 0 元，仅浏览器自动化 ¥1 买断；软件订阅按月计费、价格后台可配；新商户另赠 10M 模型 token',
      ],
      scene: '适合：不想自运维 / 想先试用再决定的团队',
    },
    {
      name: '¥1 浏览器自动化',
      subtitle: '一次性买断 · 永不续费',
      price: '¥1',
      priceNote: 'once · 不设到期日',
      includes: [
        '常驻浏览器接管真实客户端做私信与评论，一次付费永久可用，不写到期日',
        '24 个功能模块里唯一买断的一个，在系统内功能模块市场明码标价开通',
        '模型 Token 成本仍自控：自带 Key 免费，或平台算力按输入 / 输出分列预付费',
      ],
      scene: '适合：需要真人登录态自动化的运营团队',
    },
  ],
  budgetPoints: [
    {
      title: '23 个模块免费，仅浏览器自动化 ¥1',
      desc: '24 个模块中 7 个基础模块 0 元常开、16 个增值模块 0 元开通，仅浏览器自动化 ¥1 买断；平台端保留改价开关，运行时定价以上面市场页为准。',
    },
    {
      title: '浏览器自动化 ¥1 买断',
      desc: '浏览器自动化是唯一买断的模块（billing=once），¥1 开通后不写 ExpiresAt、永不续费；离线部署因无支付通道保持锁定，云端版在模块市场下单即开通。',
    },
    {
      title: '软件订阅 ¥9',
      desc: '云端版软件订阅 9 元（按 30 天计），新商户 15 天体验期；到期进入整站业务锁定，白名单保留登录、模块市场、续费与支付查询，续费即恢复，数据不删不迁。',
    },
    {
      title: '模型成本自控',
      desc: '自带 Key 只统计不计费；平台算力预付费、输入与输出分别计价、余额不足自动阻断或降级——Token 账单可预估、可封顶。',
    },
  ],
  budgetNote:
    '口径：模块价取自源码内置默认定价（user-server/internal/model/module_bindings.go，2026-10-30 核对）——24 个模块中 23 个 0 元（7 基础常开 + 16 增值），仅 browser-automation 为 ¥1 且 once 买断；软件订阅 9 元为平台级商品、价格后台可配；LLM 输入 / 输出分列定价为运营配置项（体验包 ¥1 / 标准包 ¥3 / 专业包 ¥6 / 旗舰包 ¥9）。',
  flowTitle: ['订阅流程', '（一期口径）'],
  flowDesc:
    '一期未接入在线支付通道：下单后由平台运营人工确认收款，权益自动开通；线上支付回调已预留同一入账入口与幂等键。',
  flow: [
    { title: '浏览市场', desc: '系统内功能模块市场未登录即可浏览列表，登录后查看详情、依赖闭包与定价。' },
    { title: '选择并确认', desc: '勾选目标模块，二次确认弹窗列明自动补齐的基础依赖、需自购的付费依赖与总价。' },
    { title: '提交订单', desc: '填写联系方式（手机或邮箱）提交，订单号幂等生成，重试不会重复下单。' },
    { title: '线下付款', desc: '按付款指引联系平台运营完成线下付款；离线部署（未连接平台）时无法下单，市场页显示平台未连接空态。' },
    { title: '自动开通', desc: '平台确认收款后权益即时生效，用户端随模块同步感知变化，无需重装、无需改代码。' },
  ],
  llmTitle: ['模型算力双轨', '自带 Key 或平台垫资'],
  llmDesc: '大模型接入有两条路：用自己的 Key（免费只统计），或用平台垫付的算力（预付费按量扣减）。',
  llmTracks: [
    {
      title: '自有模型接入（自带 Key）',
      price: '免费 · 只统计不计费',
      desc: '用自己的 API Key 接入 DeepSeek / 通义千问 / GPT-4o / 智谱 GLM / Kimi 等，用量与成本可视化；不扣任何钱包，适合已有 Key 或合规要求数据按所配服务流转的场景。',
    },
    {
      title: '平台模型算力（llm-cloud）',
      price: '0 元开通 · 按 token 预付费',
      desc: '不用自己申请 Key，用平台垫资的大模型：钱包按输入 / 输出 token 分列记账与实时扣减，余额不足自动阻断或降级到自带 Key，用量明细逐条可查。',
    },
  ],
  offlineTitle: '离线部署口径',
  offlineDesc:
    'PLATFORM_ENABLED=false 的离线部署不发出任何出站请求：7 个基础模块 0 元常开，16 个增值模块 0 元自动开通，仅浏览器自动化（¥1 买断）保持锁定并在界面明示——本地无支付通道，不做授权码旁路；功能模块市场与订单页显示平台未连接空态。',
  saasTitle: ['SaaS 功能', '（多租户形态）'],
  saasDesc:
    '同一份代码支持离线单机（默认）与 SaaS 多租户（TENANT_MODE=on）两种形态：多租户形态下注册即开户、租户间数据行级隔离（数据访问层统一强制）、后台流程按数据自带的租户归属执行；平台端（可选本地组件）提供功能模块目录、租户登记审核、订单确认收款、LLM 套餐钱包与运行监控的管理面。数据始终留在你部署的数据库里——这是你运营的 SaaS，不是把数据交给别人的黑盒。',
  saasPoints: [
    { title: '注册即开户', desc: '填写企业 / 团队信息与管理员凭证即可创建独立租户并自动登录，注册限流防滥用、过程原子。' },
    { title: '数据行级隔离', desc: '隔离在数据访问层统一实施，上下文缺租户时拒绝服务；定时任务、队列、回调等后台流程按数据自带归属执行。' },
    { title: '双形态一套代码', desc: '单机与多租户功能清单一致（差异仅注册入口与租户运营视图），由一项部署配置切换。' },
  ],
  // —— SaaS 云端版开箱试用入口（注册即开户，15 天体验期全功能可试）——
  saasTrial: {
    desc: '开箱试用：云端版注册即开户，新商户 15 天体验期等同订阅有效、全功能可试，并赠 10M 模型 token——无需部署、无需信用卡，到期数据不删，续费 ¥9 / 30 天即恢复。',
    label: '免费试用云端版',
    href: CLOUD_URL,
  },
  faqsTitle: ['订阅常见问题'],
  faqs: [
    {
      q: '订阅和开源冲突吗？',
      a: '不冲突。代码按 AGPL-3.0 全量开源，离线部署下你拥有全部源码，7 个基础模块 0 元常开、16 个增值模块 0 元；收费只发生在三处——浏览器自动化 ¥1 买断、云端版软件订阅 9 元、平台垫付的模型算力（按输入 / 输出 token 预付费）。模块化设计里这些是「开通位」，不是「代码下载」。',
    },
    {
      q: '离线部署能用增值模块吗？',
      a: '增值模块的代码同样在开源仓里。离线且不连接平台时，7 个基础模块与 16 个 0 元增值模块自动开通；仅浏览器自动化（¥1 买断）默认关闭，本地无支付通道、不做授权码旁路，连接云端版后在模块市场下单即开通。',
    },
    {
      q: '一定要买平台算力吗？',
      a: '不用。自有模型接入是免费的基础能力，只统计用量；平台模型算力是“不想自己申请 Key”时的可选项，预付费按 token 计量、输入与输出分别计价。',
    },
    {
      q: '一期怎么付款？',
      a: '订单提交后按付款指引联系平台运营线下付款，平台确认收款后自动开通（重复确认返回 409，不会重复开通）；支付契约已为线上回调预留，后续可接入线上支付通道。',
    },
    {
      q: '预算有限，最小可用的花钱路线是什么？',
      a: '先走 ¥0 线：离线部署 + 7 个基础模块与 16 个增值模块 0 元开通 + 自带 API Key，成本接近零；需要云端在线使用时，云端版软件订阅 9 元（新商户 15 天体验期并赠 10M 模型 token）；需要常驻浏览器自动化时，单买 ¥1 买断、一次付费永不续费。模型 Token 仍按输入 / 输出自控。',
    },
  ],
  ctaDeploy: { label: '立即部署', href: '/deploy' },
  ctaModules: { label: '查看功能架构清单', href: '/modules' },
}

// —— /features 页尾追加引流带 ——
export const featuresBand = {
  title: ['不止 13 张功能卡，', '还有模块化的功能架构'],
  desc: '上述为核心能力切片。完整系统由 24 个功能模块承载（7 基础 + 17 增值），212 个页面节点全部归属模块，菜单、接口、定时任务与智能体工具按开通状态放行；7 个基础模块 0 元常开、16 个增值模块 0 元开通，仅浏览器自动化 ¥1 买断，云端版软件订阅 9 元，模型算力按输入 / 输出 token 计费。',
  ctaArchitecture: { label: '查看功能架构清单', href: '/modules' },
  ctaPricing: { label: '了解按需订阅与 SaaS', href: '/pricing' },
}

export const nav = {
  brand: 'HiveMTK',
  links: [
    { label: '核心功能', href: '/features', type: 'route' },
    { label: '功能架构', href: '/modules', type: 'route' },
    { label: '按需订阅', href: '/pricing', type: 'route' },
    { label: '云端版', href: CLOUD_URL, type: 'external' },
    { label: '工程能力', href: '/toolchain', type: 'route' },
    { label: '工作流', href: '/workflow', type: 'route' },
    { label: '常见问题', href: '/faq', type: 'route' },
    { label: '部署指南', href: '/deploy', type: 'route' },
  ],
}

