# user-web 路由清单 (router-list)

> 生成时间: 2026-11-16
> 来源: `user-web/src/router/index.js` + `user-web/src/router/modules/*.js` (89 个模块文件)
> 测试方式: ego-browser 拟人化逐页测试 (禁止脚本批量)
> 状态图例: ⬜ 待测 | 🟨 测试中 | ✅ 已完成 | 🔧 发现问题已修复 | ⛔ 阻塞/跳过(附原因)

**测试账号**: https://work.xapptool.cn/ · jideilvluoqun@gmail.com · <口令经 E2E_ADMIN_PASSWORD 注入，不写入仓库>
**站点类型**: hash 路由 (`/#/path`) · vue-router 4 · 懒加载模块 (ensureRouteLoaded)

## 2026-11-26 · 独立渠道模块

顶级导航只展示一个「渠道」入口。既有 `/channels/manage`、`/channels/accounts`、`/channels/identify-codes` 分别显示平台、账号、识别码页签；仅显示已授权入口，保留原 URL、角色能力和旧 Bridge token 深链。三个页面共同说明渠道与单聊／群聊的关系，SageX 历史接入绑定独立呈现，保留四川同咨的现有业务关联。具体本轮证据见 [缺陷记录](../.specify/bugs/channel-module-semantics-20261126/assessment.md)。

## 2026-11-20 · 菜单、页面职责与路由专项审计

以 `user-server/internal/model/menu.go` 和当前前端源码为准，对照 7 个顶级分组的 196 个有路径的菜单节点（含分组默认路径），共 194 个不同路径，均落在 85 个模块、258 条路由记录的清单内。判重先对照路由与组件，再核对业务对象、页面主操作和 API；共用表单、参数入口和兼容路径不当作重复功能。

| 分组 | 有路径的节点（调整前） | 判定 |
|---|---:|---|
| 工作台 | 10 | 待办、审批及各自详情服务于不同任务阶段，不合并。 |
| 客户 | 11 | 线索池、客户档案和跟进的对象或动作不同，详情不另设入口。 |
| 智能体 | 32 | 智能体新建和编辑归到列表工作流；运行和配置页保留。 |
| 消息与触达 | 36 | 草稿、发送任务、渠道配置和送达报表不同；线索分组选择与群发之间的交接问题单独登记。 |
| 知识中心 | 16 | 常规单条导入和批量导入保留独立能力，从文档管理进入；外部导入任务、开放 API 来源与连接器配置不合并。 |
| 数据分析 | 54 | GEO 的生产、监测与分析保持连续；浏览器自动化转到系统设置。 |
| 系统设置 | 37 | 接纳浏览器自动化；商户新建收回资产包列表，独立开发者 Playground 保留。 |

四个动作都只在**所属列表同时可见**时从侧栏隐藏；仅有动作授权时保留原入口。路由、书签、后端菜单和原有授权判定不变。浏览器任务列表和 GEO 关键词挖掘两处分组默认路径与叶菜单同址，导航以实际叶菜单为准。创建、编辑和详情页跟随已授权的列表上下文，不提供不存在或不可达的面包屑目标。

本轮本地证据：Vitest 100 文件、963 用例通过，含 7 个导航回归；其中使用真实 Element Plus 菜单覆盖跨分组切换时子菜单尚未注册的竞态。ego-browser 用模拟角色显式点验四个列表按钮及其目标页面、批量导入返回列表、浏览器任务新建的所属分组和面包屑，以及仅文档列表与仅批量导入两种授权展示。以上不等同 258 条路由实机逐页验收，也不涵盖线上角色、数据写入或发布验证。

### 二审修正与验收

- 导航分组、授权菜单和路由守卫分别承担不同职责：列表动作看服务端实际返回的菜单，非菜单编辑页沿用现有守卫；仅有动作授权时不指向无权访问的列表。返回与登录兜底共用导航落点选择。
- 邮件拖拽编辑器属于草稿工作流，渠道卡片统计各归所属列表，不能按 URL 前缀把用户带回无关页面。已匹配的静态路由按登记路径定位面包屑，不新增路径别名，也不改变路径或权限匹配规则。
- 浏览器自动化继续在「系统设置」使用；角色配置保留原权限目录并解释分组差别。不合并各有职责的发送任务、草稿、报表和开发者 Playground。
- 二审本地全量 Vitest 103 文件、974 用例通过，导航专项 17 用例通过。ego-browser 使用只读模拟角色对照管理员与员工、知识文档仅列表与仅动作、GEO 模块开关，验证邮件草稿面包屑的 `Enter` 返回、已匹配的大小写不同深链接及角色配置提示。未验证线上租户或实际数据写入。

### 第三轮：从列表完成表单操作

| 所属列表 | 仍可访问的动作 URL | 列表内行为 |
|---|---|---|
| `/aiAgent/list` | `/aiAgent/create`、`/aiAgent/edit/:id` | 抽屉新增／编辑，保存成功后刷新并关闭。 |
| `/browser-automation/tasks` | `/browser-automation/tasks/create`、`/browser-automation/tasks/:id/edit` | 抽屉新增／编辑，已有任务详情和执行监控不并入表单。 |
| `/asset-bundle/list` | `/asset-bundle/merchant-new`、`/asset-bundle/merchant/:aid` | 抽屉处理商户配置，开发者 Playground 保持独立。 |
| `/chatChannel/list` | `/chatChannel/create`、`/chatChannel/edit/:id` | 抽屉新增／编辑；新建后仍需确认一次性凭据，再返回列表。 |
| `/knowledge/management` | `/knowledge/batch-import` | 抽屉保留批量导入结果；有文档被接收时刷新列表。知识库新增／编辑已在其列表内，不重复迁移。 |

抽屉共用原有表单、路由和 API：保留列表实例及其筛选状态；取消不写入或刷新；保存只刷新有权访问且已挂载的列表。直接访问旧地址也可打开抽屉；只有操作权限时保留独立页、不加载列表数据。邮件拖拽、流程编排等需要完整画布的工作台不做机械合并。

本地验收：Vitest 104 文件、978 用例；ego-browser 在本地模拟数据上点验智能体编辑取消保留筛选和原 URL、旧链接打开编辑且保存后列表显示新值、浏览器任务及客服渠道新建、商户配置旧链接、知识批量导入和仅操作权限。未验证线上租户、真实凭据或实际数据写入。

---

## A. 全局/公开页面 (index.js initRoutes)

| # | 路径 | 名称 | 标题 | 权限 | 状态 | 备注 |
|---|------|------|------|------|------|------|
| A01 | `/setup` | InitSetup | 系统初始化 | public | ⬜ | 干净实例才有意义, 线上已初始化会跳走 |
| A02 | `/login` | Login | 登录 | public | ⬜ | 登录流程 + enable_register 开关 |
| A03 | `/help-center` | HelpCenter | 帮助中心 | public | ⬜ | hideLayout |
| A04 | `/chat/embed/default` | ChatEmbed | 在线客服 | public | ⬜ | 客服挂件默认渠道 |
| A05 | `/chat/embed/:channel_ref` | ChatEmbedChannel | 在线客服 | public | ⬜ | 依赖真实渠道 ref |
| A06 | `/profile` | Profile | 个人资料 | auth | ⬜ | |
| A07 | `/notifications` | Notifications | 通知中心 | auth | ⬜ | |
| A08 | `/*`(NotFound) | NotFound | 页面不存在 | public | ⬜ | 验 404 与 403 两种 query 状态 |
| A09 | `/` (Layout) | — | 重定向 /messageHub/list | — | ⬜ | 非管理员回退 fallbackLandingPath |

## B. 业务模块页面 (按 moduleNames 注册顺序)

### 消息与触达 (reach)

| # | 路径 | 标题 | 状态 | 备注 |
|---|------|------|------|------|
| B01 | `/messageHub/list` | 消息中台 | ✅ | 默认落地页; 统计/筛选(22平台)/分页/详情弹窗/推送弹窗+校验/取消 全通过; 无console&API错误; 标记已读/撤回/真实推送为破坏性操作未执行 |
| B02 | `/messageHub/dashboard` | 消息总览 | 🔧 | BUG#1(已本地修复): DLQ 重试/丢弃/批量重试无二次确认+无错误处理 → Dashboard.vue 加 ElMessageBox.confirm+try/catch; 30s自动刷新/空状态/图表正常 |
| B03 | `/email` | 邮件列表 | ⬜ | |
| B03 | `/email` | 邮件列表 | ✅ | 空表+引导弹窗(driver.js)+发送邮件弹窗(校验4项/取消)全正常 |
| B04 | `/email/drafts` | 我的草稿 | ✅ | 新建草稿弹窗(富文本工具栏)+空保存校验(主题/内容必填)+取消正常 |
| B05 | `/email/jobs` | 我的任务 | ✅ | 任务表头完整+空状态+引导弹窗正常 |
| B06 | `/email/smtp` | 邮件账号 | ✅ | 代理列表+新增弹窗(6字段)+空提交校验5项+取消正常 |
| B07 | `/email/info` | 邮件代理 | ✅ | 11行代理列表+前往注册按钮, 无错误 |
| B08 | `/email/guide` | 使用引导 | ✅ | 4节指南+前往操作按钮 |
| B09 | `/email/deliverability` | 送达分析 | ✅ | ECharts图渲染+刷新按钮正常 |
| B10 | `/email/drag-editor` | 拖拽编辑器 | ✅ | 调色板/画布/桌面+移动预览(MJML编译)/发送测试校验/拖入文本块成功(↑↓删控件出现) |
| B11 | `/sms/list` | 短信列表 | ✅ | 发送短信弹窗(手机号正则toast校验,不弹表单错误)+搜索/重置正常 |
| B12 | `/sms/drafts` | 短信草稿 | ✅ | 新建草稿弹窗+校验(标题/内容必填)正常 |
| B13 | `/sms/jobs` | 短信任务 | ✅ | 创建任务按钮+空状态正常 |
| B14 | `/sms/config` | 短信配置 | ✅ | 15输入项配置表单, 原值保存成功toast, 无错误 |
| B15 | `/batchOperation/list` | 批量操作 | 🔧 | BUG#2/#3: 卡片可点击+工具弹窗+历史表正常; BUG#3图标动态字符串已修复; 线上为旧构建(弹窗标题raw key) |
| B16 | `/marketingFlow/list` | 营销自动化 | 🔧 | BUG#4已修复: 新建流程弹窗7字段; 空提交原直打API(400)现已前端拦截; 卡片列表正常 |
| B17 | `/reachPipeline/list` | 触达Pipeline | ✅ | 新建Pipeline弹窗13字段(限流配置齐全)+空列表正常 |
| B18 | `/reachPipeline/editor/:id?` | Pipeline 编辑 | ⬜ | hideInMenu |
| B19 | `/reachPipeline/deferred-outbound` | 延迟出站管理 | ✅ | 空状态文案清晰(合规窗口说明), 刷新/重置正常 |
| B20 | `/workflow-orchestrator/list` | 工作流编排 | ✅ | 新建工作流弹窗(ID/名称/描述)+查询正常 |
| B21 | `/workflow-orchestrator/editor/:workflow_id` | 工作流编辑 | ✅ | 假ID优雅降级(空画布+操作提示), 节点类型面板正常 |
| B22 | `/workflow-orchestrator/execution/:id` | 执行详情 | ✅ | 假ID→404→用户可见报错toast, 停止/刷新按钮正常 |
| B23 | `/tiktok` | TikTok | ✅ | 添加卡片弹窗7字段+校验(标题/图片URL必填)+搜索/重置正常 |
| B24 | `/tiktok/list` | TikTok卡片 | ✅ | 同B23结构, 空列表正常 |
| B25 | `/tiktok/stats` | TikTok统计 | ✅ | 统计筛选+刷新, 空状态正常 |
| B26 | `/tiktok-card-stats/:id` | TikTok卡片统计 | ✅ | 假ID→优雅降级("卡片已被删除或id不属于当前账号"文案+返回按钮), 404处理到位 |

### 社群/IM 渠道 (community)

| # | 路径 | 标题 | 状态 | 备注 |
|---|------|------|------|------|
| B27 | `/telegram` | TG 机器人 | ✅ | 添加机器人+刷新, 空列表正常 |
| B28 | `/telegram/account` | 机器人账号 | ✅ | 同结构正常 |
| B29 | `/telegram/gate` | 入群管控 | ⬜→✅ | 添加群组管控+刷新正常 |
| B30 | `/qq` | QQ 机器人 | ✅ | 添加QQ机器人弹窗8字段+确认校验(名称/AppID/AppSecret必填)正常 |
| B31 | `/qq/account` | 机器人账号 | ✅ | 同结构正常 |
| B32 | `/whatsapp` | WhatsApp 社群 | ✅ | 空账号列表+前往Cloud/添加账号正常 |
| B33 | `/whatsapp/account` | 账号管理 | ✅ | 同结构正常 |
| B34 | `/whatsapp/drafts` | 草稿箱 | ✅ | 新建草稿弹窗+校验(标题/内容必填)正常 |
| B35 | `/whatsapp/jobs` | 群发 | ✅ | 新建任务+搜索正常, 空列表 |
| B36 | `/whatsapp/lead-group-selection` | 从线索库选择群体 | ✅ | 线索表20行+筛选27项正常 |
| B37 | `/whatsapp/group-messaging` | 批量消息发送 | ✅ | 编辑器44输入项+新建模板正常 |
| B38 | `/whatsapp/bulk-matrix` | 群发矩阵 | 🔧 | BUG#5已修复: 预览模板死按钮→新增预览弹窗(模板名+变量插值气泡); 统计卡正常 |
| B39 | `/whatsapp-cloud` | WhatsApp Cloud | ✅ | 新增账号弹窗9字段(Phone/WABA/Token等)正常, 空列表 |
| B40 | `/feishu` | 飞书 | ✅ | 添加飞书账号+刷新正常 |
| B41 | `/feishu/account` | 飞书账号 | ✅ | 同结构正常 |
| B42 | `/dingtalk-app` | 钉钉应用 | ✅ | 新增账号正常 |
| B43 | `/community/list` | 社群管理 | ✅ | 导入/导出/新增分组/搜索/重置正常 |
| B44 | `/shortLink` | 短链管理 | ✅ | 添加短链弹窗6字段+校验(短码/URL必填)正常 |
| B45 | `/shortLink/stats` | 短链统计 | ✅ | 总体统计+筛选正常 |
| B46 | `/douyinCard` | 抖音卡片 | ✅ | 添加抖音卡片弹窗7字段+校验正常(与TikTok同构) |
| B47 | `/douyin/stats` | 抖音卡片统计 | ✅ | 图表canvas+刷新正常, 空状态 |
| B48 | `/douyin-card-stats/:id` | 抖音卡片详情统计 | ✅ | 假ID优雅降级+返回按钮 |
| B49 | `/xiaohongshuCard` | 小红书卡片 | ✅ | 同构正常 |
| B50 | `/xiaohongshu/stats` | 小红书卡片统计 | ✅ | 刷新数据+图表正常 |
| B51 | `/xiaohongshu-card-stats/:id` | 小红书卡片详情统计 | ✅ | 假ID优雅降级(文案清晰) |
| B52 | `/kuaishouCard` | 快手卡片 | ✅ | 同构正常 |
| B53 | `/kuaishou/stats` | 快手卡片统计 | ✅ | 图表+刷新正常 |
| B54 | `/kuaishou-card-stats/:id` | 快手卡片详情统计 | ✅ | 假ID优雅降级+返回 |
| B55 | `/xianyuCard` | 闲鱼卡片 | ✅ | 同构正常 |
| B56 | `/xianyu/stats` | 闲鱼卡片统计 | ✅ | 双图表正常 |
| B57 | `/xianyu-card-stats/:id` | 闲鱼卡片详情统计 | ✅ | 假ID优雅降级+返回 |
| B58 | `/livecode` | 活码管理 | ✅ | 新增活码+查询/重置正常 |
| B59 | `/customerService/agentStatus` | 坐席状态 | ✅ | 真实坐席2行; 查看会话弹窗正常; 上线/下线/修改状态为状态变更操作未执行 |
| B60 | `/customerService/quickReply` | 快捷回复 | ✅ | 新增快捷回复正常, 空列表 |
| B61 | `/customerService/sessionTag` | 会话标签 | ✅ | 新增标签正常 |
| B62 | `/customerService/aiSuggestion` | AI 建议 | ✅ | 空ID加载→toast"请先输入会话 ID"校验正常 |
| B63 | `/customerService/csat` | CSAT 看板 | ✅ | 双图表渲染正常 |
| B64 | `/customerSession/list` | 客服会话 | ✅ | 三栏看板+真实会话数据; 新建会话弹窗(客户ID输入)正常; 黑名单管理按钮存在 |
| B65 | `/chatChannel/list` | 客服渠道 | ✅ | 真实渠道1行; 编辑/安装引导可用; 轮换Key/重置Secret/禁用为破坏性未执行 |
| B66 | `/chatChannel/create` | 新建客服渠道 | ✅ | 表单8输入+2文本域, 创建按钮存在(未提交) |
| B67 | `/chatChannel/edit/:id` | 编辑客服渠道 | ✅ | 真实ID表单数据完整回填(名称/白名单/问候语/位置/阈值), 未保存 |
| B68 | `/chatChannel/install-guide/:id?` | Widget 安装引导 | ✅ | 真实渠道嵌入代码+AppKey+复制全部正常 |
| B69 | `/wecomAccount/list` | 企微账号管理 | ✅ | 新增账号+刷新正常, 空列表 |
| B70 | `/wecomAccount/data` | 企微数据看板 | ✅ | 刷新数据正常 |

### 线索/客户 (customer & sales)

| # | 路径 | 标题 | 状态 | 备注 |
|---|------|------|------|------|
| B71 | `/clue/list` | 线索列表 | ⬜ | |
| B72 | `/clue/statistics` | 线索统计 | ⬜ | |
| B73 | `/lead-mining/index` | 线索发掘 | ⬜ | |
| B74 | `/customer360/list` | 客户 360 | ⬜ | |
| B75 | `/customer360/consents` | 客户同意与偏好 | ⬜ | |
| B76 | `/customerEvent/list` | 客户事件 | ⬜ | |
| B77 | `/customerSession`(见B64) | — | — | |
| B78 | `/customerJourney/dashboard` | 客户旅程大屏 | ⬜ | |
| B79 | `/tagSegmentation/list` | 标签分层 | ⬜ | |
| B80 | `/userSegment/list` | 用户分层 RFM | ⬜ | |
| B81 | `/userSegment/rfm-matrix` | RFM 矩阵 | ⬜ | |
| B82 | `/userSegment/builder` | 分层规则构建 | ⬜ | |
| B83 | `/conversionFunnel/list` | 转化漏斗 | ⬜ | |
| B84 | `/churnPrediction/list` | 流失预警 | ⬜ | |
| B85 | `/abExperiment/list` | A/B 实验 | ⬜ | |
| B86 | `/objection/list` | 异议处理 | ⬜ | |
| B87 | `/persona/list` | 销冠画像 | ⬜ | |
| B88 | `/oneid/list` | OneID 列表 | ⬜ | `/oneid` 重定向至此 |
| B89 | `/oneid/conflicts` | 身份冲突解决 | ⬜ | |
| B90 | `/oneid/merge-rules` | 合并规则配置 | ⬜ | tag OPT-UX-04 |

### 智能体与知识 (aiAgent & knowledge)

| # | 路径 | 标题 | 状态 | 备注 |
|---|------|------|------|------|
| B91 | `/aiAgent` | 智能体 | ⬜ | |
| B92 | `/aiAgent/list` | 智能体列表 | ⬜ | |
| B93 | `/aiAgent/create` | 创建智能体 | ⬜ | 11-20 本地导航点验：智能体列表 → 新建智能体；保留深链与权限。 |
| B94 | `/aiAgent/edit/:id` | 编辑智能体 | ⬜ | 需真实 id |
| B95 | `/aiAgent/integrations` | 工具接入 | ⬜ | |
| B96 | `/aiAgent/tools` | AI 工具管理 | ⬜ | |
| B97 | `/dialogueMemory/list` | 对话记忆 | ⬜ | |
| B98 | `/intentRecognition/list` | 意图识别 | ⬜ | |
| B99 | `/intentRecognition/tool-flow` | 意图工具流 | ⬜ | roles: admin |
| B100 | `/llmRouting/list` | LLM路由 | ⬜ | |
| B101 | `/llmRouting/cost` | 成本看板 | ⬜ | |
| B102 | `/sopAgent/list` | 销冠 SOP 智能体 | ⬜ | |
| B103 | `/scriptTemplate/list` | 话术库 | ⬜ | |
| B104 | `/scriptTemplate/ab-stats` | 话术AB测试 | ⬜ | |
| B105 | `/tuning /confidence/panel` | 置信度运营 | ⬜ | |
| B106 | `/humanize/panel` | 拟人度评估 | ⬜ | |
| B107 | `/feedbackLoop/panel` | 反馈学习闭环 | ⬜ | |
| B108 | `/faq` | FAQ 知识库 | ⬜ | |
| B109 | `/faq/list` | FAQ 列表 | ⬜ | |
| B110 | `/faq/editor` | FAQ 编辑 | ⬜ | hideInMenu |
| B111 | `/faq/editor/:id` | FAQ 编辑 | ⬜ | 需真实 id |
| B112 | `/sop-template` | SOP 模板库 | ⬜ | |
| B113 | `/sop-template/list` | SOP 模板列表 | ⬜ | |
| B114 | `/sop-template/market` | SOP 模板市场 | ⬜ | tag OPT-UX-06 |
| B115 | `/sop-template/editor` | SOP 模板编辑 | ⬜ | hideInMenu |
| B116 | `/sop-template/editor/:id` | SOP 模板编辑 | ⬜ | 需真实 id |
| B117 | `/knowledge/management` | 知识库管理 | ⬜ | |
| B118 | `/knowledge/batch-import` | 批量导入 | ⬜ | 11-20 本地导航点验：文档管理 → 批量导入 → 面包屑返回；仅动作授权时仍可从侧栏进入。 |
| B119 | `/knowledge/playground` | 检索 Playground | ⬜ | |
| B120 | `/knowledge/connectors` | 外部连接器 | ⬜ | |
| B121 | `/knowledge/crawler-sources` | 网站爬取 | ⬜ | |
| B122 | `/knowledge/qa-candidates` | QA 审核队列 | ⬜ | |
| B123 | `/knowledge/feedbacks` | 反馈管理 | ⬜ | |
| B124 | `/knowledge/tokens` | API Token | ⬜ | |
| B125 | `/knowledge/external` | 外部系统接入 | ⬜ | |
| B126 | `/knowledge/statistics` | 知识库统计 | ⬜ | |
| B127 | `/knowledge/openapi` | OpenAPI 集成 | ⬜ | |
| B128 | `/knowledge/rag-eval` | RAG 评测 | ⬜ | |
| B129 | `/knowledgeBase` | 知识库管理 | ⬜ | |
| B130 | `/knowledgeBase/list` | 知识库列表 | ⬜ | |
| B131 | `/knowledgeBase/create` | 新建知识库 | ⬜ | |
| B132 | `/knowledgeBase/edit/:id` | 编辑知识库 | ⬜ | 需真实 id |
| B133 | `/kb/connectors` | 知识库连接器 | ⬜ | |
| B134 | `/cap/matrix` | 能力矩阵 | ⬜ | |
| B135 | `/cap/diagnostics` | 能力运行诊断 | ⬜ | |
| B136 | `/system/rag-overview` | RAG概览 | ⬜ | |
| B137 | `/system/rag-account` | RAG 账号配置 | ⬜ | |
| B138 | `/system/rag-product` | RAG 产品管理 | ⬜ | |
| B139 | `/glossary` | 术语表管理 | ⬜ | |
| B140 | `/mcp/credentials` | MCP凭证 | ⬜ | |
| B141 | `/asset-market` | 资产市场 | ⬜ | |
| B142 | `/asset-market/detail/:id` | 资产详情 | ⬜ | 需真实 id |
| B143 | `/asset-market/my-assets` | 我的资产 | ⬜ | |
| B144 | `/asset-market/sync-log` | 同步日志 | ⬜ | |
| B145 | `/asset-bundle/list` | 资产包管理 | ⬜ | |
| B146 | `/asset-bundle/playground` | 开发者 Playground | ⬜ | |
| B147 | `/asset-bundle/playground/:aid` | 开发者 Playground | ⬜ | 需真实 aid |
| B148 | `/asset-bundle/merchant-new` | 商户新建话术包 | ⬜ | 11-20 本地导航点验：资产包管理 → 商户低代码新建；开发者 Playground 仍为独立入口。 |
| B149 | `/asset-bundle/merchant/:aid` | 商户配置话术包 | ⬜ | 需真实 aid |

### 工作台 (workspace)

| # | 路径 | 标题 | 状态 | 备注 |
|---|------|------|------|------|
| B150 | `/inbox/list` | 统一收件箱 | ⬜ | |
| B151 | `/approvalTask/list` | 待办中心 | ⬜ | |
| B152 | `/badCase/list` | Bad Case 队列 | ⬜ | |
| B153 | `/followups/today` | 今日跟进 | ⬜ | |
| B154 | `/followups/:id` | 跟进详情 | ⬜ | 需真实 id |
| B155 | `/dashboard/drafts` | 订单草稿 | ⬜ | 路径映射 dashboard→orderDraft |
| B156 | `/dashboard/drafts/:id` | 草稿详情 | ⬜ | 需真实 id |
| B157 | `/sales-workbench` | 销售工作台 | ⬜ | |

### 数据分析 (analytics/dataAnalysis)

| # | 路径 | 标题 | 状态 | 备注 |
|---|------|------|------|------|
| B158 | `/analytics/cohort-path` | 留存与路径 | ⬜ | |
| B159 | `/aiProductivity/list` | AI产能分析 | ⬜ | |
| B160 | `/customReport/list` | 自定义报表 | ⬜ | |
| B161 | `/dashboardScreen/list` | 数据大屏 | ⬜ | |
| B162 | `/dashboardScreen/builder` | 大屏构建 | ⬜ | |
| B163 | `/reports/llm` | LLM用量报表 | ⬜ | |

### 系统管理 (system, 多 requiresAdmin)

| # | 路径 | 标题 | 状态 | 备注 |
|---|------|------|------|------|
| B164 | `/system/config` | 站点设置 | ⬜ | admin |
| B165 | `/system/obs-config` | 存储配置 | ⬜ | admin |
| B166 | `/system/material-library` | 素材库 | ⬜ | admin |
| B167 | `/system/monitor` | 监控 | ⬜ | admin |
| B168 | `/system/trace` | 链路追踪 | ⬜ | admin |
| B169 | `/system/guide` | 使用引导 | ⬜ | admin |
| B170 | `/system/automation-hub` | 自动化中心 | ⬜ | admin |
| B171 | `/system/config-params` | 动态阈值参数 | ⬜ | admin |
| B172 | `/system/dictionaries` | 业务字典 | ⬜ | admin |
| B173 | `/system/feature-flags` | 功能开关 | ⬜ | admin |
| B174 | `/system/ltc-config` | LTC 运营开关 | ⬜ | admin |
| B175 | `/system/users` | 用户管理 | ⬜ | admin |
| B176 | `/system/roles` | 角色管理 | ⬜ | admin |
| B177 | `/system/permissions` | 权限面板 | ⬜ | admin |
| B178 | `/domainPool` | 域名池管理 | ⬜ | admin |
| B179 | `/integration/list` | 第三方对接 | ⬜ | admin |
| B180 | `/operationLog/list` | 操作日志 | ⬜ | admin |
| B181 | `/operationLog/enhanced` | 操作日志(增强) | ⬜ | admin |
| B182 | `/platformAccount/list` | 平台账号 | ⬜ | admin |
| B183 | `/securityAudit/list` | 安全审计 | ⬜ | admin |
| B184 | `/backup/list` | 备份恢复 | ⬜ | admin |
| B185 | `/ops-overview` | 运维总览 | ⬜ | admin |
| B186 | `/sales-cockpit` | AI 销冠驾驶舱 | ⬜ | |
| B187 | `/observability-dashboard` | AI 观测与渠道健康 | ⬜ | admin |
| B188 | `/bridge` | 渠道接入 | ⬜ | admin |
| B189 | `/bridge/token` | 桥接凭证 | ⬜ | admin |
| B190 | `/bridge/extension` | 扩展安装包 | ⬜ | |
| B191 | `/token` | (bridgeToken 别名) | ⬜ | |

### GEO-Tools (28 页)

| # | 路径 | 标题 | 状态 | 备注 |
|---|------|------|------|------|
| B192 | `/geo-tools/audit-center` | 审计中心 | ⬜ | |
| B193 | `/geo-tools/brand-config` | 品牌与域名 | ⬜ | |
| B194 | `/geo-tools/seo-infra` | SEO 基础设施 | ⬜ | |
| B195 | `/geo-tools/pusher-config` | 蜘蛛推送配置 | ⬜ | |
| B196 | `/geo-tools/site-config` | 静态站部署 | ⬜ | |
| B197 | `/geo-tools/schema-templates` | Schema 模板 | ⬜ | |
| B198 | `/geo-tools/competitors` | 竞品管理 | ⬜ | |
| B199 | `/geo-tools/config` | 配置优化 | ⬜ | |
| B200 | `/geo-tools/autopilot` | 自动驾驶（一键） | ⬜ | |
| B201 | `/geo-tools/keyword-mining` | 关键词蒸馏 | ⬜ | |
| B202 | `/geo-tools/content-creation` | 内容创作 | ⬜ | |
| B203 | `/geo-tools/content-optimize` | 文章优化 | ⬜ | |
| B204 | `/geo-tools/site-publish` | 发布到官网 | ⬜ | |
| B205 | `/geo-tools/push-center` | 蜘蛛推送 | ⬜ | |
| B206 | `/geo-tools/platform-publish` | 多平台发布 | ⬜ | |
| B207 | `/geo-tools/workflow` | 工作流 | ⬜ | |
| B208 | `/geo-tools/knowledge-base` | GEO 知识库 | ⬜ | |
| B209 | `/geo-tools/entity-graph` | 实体图谱 | ⬜ | |
| B210 | `/geo-tools/funnel-dashboard` | 漏斗总览 | ⬜ | |
| B211 | `/geo-tools/index-tracking` | 收录与引用 | ⬜ | |
| B212 | `/geo-tools/visibility` | 可见性趋势 | ⬜ | |
| B213 | `/geo-tools/sov-board` | 竞品 SOV | ⬜ | |
| B214 | `/geo-tools/crawler-stats` | 爬虫统计 | ⬜ | |
| B215 | `/geo-tools/verification` | 多模型验证 | ⬜ | |
| B216 | `/geo/decision-report` | 决策链报表 | ⬜ | |
| B217 | `/geo-tools/reports` | 成本报表 | ⬜ | |
| B218 | `/geo-tools/dual-engine` | 双端监测 | ⬜ | |
| B219 | `/geo-tools/capture-links` | 承接链接 | ⬜ | |
| B220 | `/geo-tools/alerts` | 告警中心 | ⬜ | |

### 浏览器自动化 (browserAutomation, 15 页；用户端导航归入系统设置)

| # | 路径 | 标题 | 状态 | 备注 |
|---|------|------|------|------|
| B221 | `/browser-automation` | 浏览器自动化 | ⬜ | |
| B222 | `/browser-automation/tasks` | 任务列表 | ⬜ | |
| B223 | `/browser-automation/tasks/create` | 新建任务 | ⬜ | 11-20 本地导航点验：系统设置 → 浏览器自动化 → 任务列表 → 新建任务。 |
| B224 | `/browser-automation/tasks/:id/edit` | 编辑任务 | ⬜ | 需真实 id |
| B225 | `/browser-automation/tasks/:id` | 任务详情 | ⬜ | 需真实 id |
| B226 | `/browser-automation/sessions/:id` | 执行监控 | ⬜ | 需真实 id |
| B227 | `/browser-automation/cron` | 定时触发器 | ⬜ | |
| B228 | `/browser-automation/status` | Host 状态 | ⬜ | |
| B229 | `/browser-automation/ego/flows` | 我的流程 | ⬜ | |
| B230 | `/browser-automation/ego/sessions` | 通用会话 | ⬜ | |
| B231 | `/browser-automation/ego/executions` | 执行监控 | ⬜ | |
| B232 | `/browser-automation/ego/runner` | Runner 状态 | ⬜ | |
| B233 | `/browser-automation/ego/assets` | 资产中心 | ⬜ | |
| B234 | `/browser-automation/ego/assets/admin` | 资产发布台 | ⬜ | admin |
| B235 | `/browser-automation/ego/telemetry` | 遥测看板 | ⬜ | admin |

### 模块市场/商户 (moduleMarket & templateMarket)

| # | 路径 | 标题 | 状态 | 备注 |
|---|------|------|------|------|
| B236 | `/module-market` | 功能模块市场 | ⬜ | public |
| B237 | `/module-market/detail/:key` | 模块详情 | ⬜ | public, 需真实 key |
| B238 | `/module-market/my` | 我的模块 | ⬜ | |
| B239 | `/module-market/orders` | 我的订单 | ⬜ | |
| B240 | `/subscription-wall` | 订阅续费 | ⬜ | hideLayout |
| B241 | `/register` | 商户注册 | ⬜ | public, hideLayout |
| B242 | `/template-market` | 模板/技能市场 | ⬜ | public |
| B243 | `/template-market/my` | 我的发布/订单 | ⬜ | |
| B244 | `/template-market/review` | 发布审核 | ⬜ | admin |

### 其他

| # | 路径 | 标题 | 状态 | 备注 |
|---|------|------|------|------|
| B245 | `/i18n/dashboard` | 多语言监控 | ⬜ | |
| B246 | `/unknown-questions` | 未知问题运营 | ⬜ | |

---

## 缺陷台账 (本地修复, 待发布回归)

| # | 页面 | 严重度 | 问题 | 修复文件 | 状态 |
|---|------|--------|------|----------|------|
| BUG#1 | B02 /messageHub/dashboard | P1 | DLQ 重试/丢弃/批量重试无二次确认(丢弃是不可逆DELETE)+无错误处理 | `src/views/messageHub/Dashboard.vue` | 已修复待回归 |
| BUG#2 | B15 /batchOperation/list | 信息 | 线上旧构建与源码行为不一致(弹窗标题raw key "import"); 源码已正确 | (部署侧) | 线上旧版待发布 |
| BUG#3 | B15 /batchOperation/list | P2 | 动态 `:is="'Upload'"` 字符串绕过 unplugin 自动导入→四张工具卡图标全空+预览 Check/Close 缺失 | `src/views/batchOperation/List.vue` | 已修复待回归 |
| BUG#4 | B16 /marketingFlow/list | P2 | 模板 `:rules="formRules"` 引用未定义变量→校验形同虚设, 空流程名称直接提交后端400 | `src/views/marketingFlow/List.vue` | 已修复待回归 |
| BUG#5 | B38 /whatsapp/bulk-matrix | P2 | 「预览模板」死按钮: showPreview ref 无组件消费, 点击无任何反应 | `src/views/whatsapp/BulkMatrix.vue` (新增预览弹窗+变量插值) | 已修复待回归 |

## 测试进度统计

- 公开/全局: 0/9
- 业务模块: 0/246
- **总计: 0/255**

## 2026-11-22 电商操作工作台

| 状态 | 路由 | 组件 | 验证范围 |
|---|---|---|---|
| ⬜ | `/commerce/operations` | `views/commerce/OperationsPanel.vue` | 按店铺审批、执行与证据；浏览器逐元素验收及真实平台验证未完成 |
