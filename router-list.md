# 前端页面路由清单（任务四逐页点验 + 任务六新手引导文案）

- 来源：`user-web/src/router/index.js` + `src/router/modules/*.js`（86 个模块文件）
- 规模：**260 条页面路由**
- 生成方式：`scripts/gen_router_list.py`（只做索引，不开浏览器；重跑保留既有勾选状态）
- 校验口径：逐条路由**拟人点击**页面元素/表单/交互（每个元素显式指定，禁止枚举循环），
  比对**浏览器控制台报错**与 **Network 里的非 2xx API 响应**；发现错误即本地修复并回归

## 测试环境（2026-11-06 全量复测轮）

| 形态 | 访问入口 | 后端 | 账号 | 标识 |
|---|---|---|---|---|
| SaaS 云端（线上） | `https://work.xapptool.cn/` | 线上租户 18（tz3rmdj） | `jideilvluoqun@gmail.com`（口令不入库） | `tenant_mode=true` / `edition=saas` |
| SaaS 本地 | `127.0.0.1:8321` | `127.0.0.1:8315`（`TENANT_MODE=on`） | `uit_admin`（tenant_id=1；口令不入库） | `tenant_mode=true` / `edition=saas` |
| 私域独立部署 | `127.0.0.1:8320` | `127.0.0.1:8314`（`TENANT_MODE=off`） | `uit_admin`（口令不入库） | `tenant_mode=false` / `edition=opensource` |

- 浏览器：ego lite（`ego-browser`，复用登录态；控制台 + Network 全量采集）
- 两种形态**都要过**：SaaS 页出现的租户/商务属性，独立部署页必须不出现且不报错

## 状态图例

| 标记 | 含义 |
|---|---|
| `[x]` | 已逐页打开并点击验证：控制台无报错、API 无非 2xx |
| `[~]` | 已开页，响应属设计内拒绝（如参数必填校验、无权限），非缺陷 |
| `[!]` | 已点出缺陷，待修复 |
| `[ ]` | 未测 |

## 任务六 · 新手引导（页面导览）状态图例

「引导」列记的是**这一页的新手引导文案与步骤**写没写、写没写对：
读懂该页的功能/业务/使用/配置逻辑 → 写这一页的定位说明与关键操作说明 →
把说明挂到该页**真实存在**的 DOM 元素上 → 浏览器里点开确认高亮正确、× 能关掉。

| 标记 | 含义 |
|---|---|
| `G` | 已写导览：文案讲的是这一页的真实功能/操作/配置，高亮元素在浏览器里点过，关闭按钮生效 |
| `-` | 本页不需要导览（公共页/重定向/无业务语义），或尚未开写 |

## 清单

| # | 状态 | 路径 | 名称 | 标题 | 需登录 | 组件 | 源文件 | 引导 |
|---|---|---|---|---|---|---|---|---|
| 1 | [x] | `/setup` | InitSetup | 系统初始化 | - | `views/setup/InitSetup.vue` | `src/router/index.js` | - |
| 2 | [x] | `/login` | Login | 登录 | - | `views/Login.vue` | `src/router/index.js` | - |
| 3 | [x] | `/help-center` | HelpCenter | 帮助中心 | 否 | `views/public/HelpCenter.vue` | `src/router/index.js` | - |
| 4 | [x] | `/chat/embed/default` | ChatEmbed | 在线客服 | 否 | `views/chat/embed/Index.vue` | `src/router/index.js` | - |
| 5 | [x] | `/chat/embed/:channel_ref` | ChatEmbedChannel | 在线客服 | 否 | `views/chat/embed/Index.vue` | `src/router/index.js` | - |
| 6 | [x] | `profile` | Profile | 个人资料 | 是 | `views/Profile.vue` | `src/router/index.js` | - |
| 7 | [x] | `notifications` | Notifications | 通知中心 | 是 | `views/Notifications.vue` | `src/router/index.js` | - |
| 8 | [x] | `/:pathMatch(.*)*` | NotFound | 页面不存在 | - | `views/NotFound.vue` | `src/router/index.js` | - |
| 9 | [x] | `/` | Layout |  | - | `layout/Layout.vue` | `src/router/index.js` | - |
| 10 | [x] | `/chat/embed` | - |  | - | `-` | `src/router/index.js` | - |
| 11 | [x] | `/chat/embed/` | - |  | - | `-` | `src/router/index.js` | - |
| 12 | [x] | `/telegram/group` | - |  | - | `-` | `src/router/index.js` | - |
| 13 | [x] | `/oneid` | - |  | - | `-` | `src/router/index.js` | - |
| 14 | [x] | `/abExperiment/list` | AbExperimentList | A/B 实验 | 是 | `views/abExperiment/List.vue` | `src/router/modules/abExperiment.js` | 筛选/搜索/新建校验/详情5类分析/编辑/启停/删除全通；修复状态下拉英文选项、空表 No data、Winner 未本地化 |
| 15 | [x] | `/aiAgent` | AIAgent | 智能体 | - | `views/aiAgent/List.vue` | `src/router/modules/aiAgent.js` | 搜索/类型/状态筛选/重置/刷新/测试智能体/绑定关系全通；双端加载正常 |
| 16 | [x] | `/aiAgent/list` | AIAgentList | 智能体列表 | - | `views/aiAgent/List.vue` | `src/router/modules/aiAgent.js` | 搜索/类型/状态筛选/重置/刷新/测试智能体/绑定关系全通；双端加载正常 |
| 17 | [x] | `/aiAgent/create` | AIAgentCreate | 创建智能体 | - | `views/aiAgent/Edit.vue` | `src/router/modules/aiAgent.js` | 必填校验/创建/编辑(编码禁用、预填)/保存更新/删除全通，测试数据已清理 |
| 18 | [x] | `/aiAgent/edit/:id` | AIAgentEdit | 编辑智能体 | - | `views/aiAgent/Edit.vue` | `src/router/modules/aiAgent.js` | 必填校验/创建/编辑(编码禁用、预填)/保存更新/删除全通，测试数据已清理 |
| 19 | [x] | `/aiAgent/integrations` | AICustomIntegration | 工具接入 | - | `views/aiAgent/CustomIntegration.vue` | `src/router/modules/aiAgent.js` | OpenAPI/MCP 双 tab：空表单校验、非法 JSON 报错、合法导入(1 工具)、MCP 创建/启停/连通测试(失败提示)/取消/删除全通；双端加载正常，测试数据已清理 |
| 20 | [x] | `/aiAgent/tools` | AIToolManagement | AI 工具管理 | - | `views/aiAgent/ToolManagement/ToolList.vue` | `src/router/modules/aiAgent.js` | 统计卡/分类筛选(触达→空)/状态筛选(已启用1行/已禁用0行)/重置/刷新/详情弹窗/启用禁用切换(禁用成功/启用成功)全通；双端加载正常 |
| 21 | [x] | `/aiProductivity/list` | AiProductivityList | AI产能分析 | 是 | `views/aiProductivity/List.vue` | `src/router/modules/aiProductivity.js` | 8 KPI 卡(总会话117/AI占比100%/均响42s等)+30天趋势图渲染、悬浮 tooltip(会话数/AI回复数/转化数)、刷新按钮全通；双端加载正常 |
| 22 | [x] | `/analytics/cohort-path` | CohortPath | 留存与路径 | 是 | `views/analytics/CohortPath.vue` | `src/router/modules/analytics.js` | Cohort 留存热力图(3 canvas)+行为路径 sankey(default--page_view 流量1,悬浮 tooltip 正常)、双 tab 切换重绘正常；双端加载正常 |
| 23 | [x] | `/approvalTask/list` | ApprovalTaskList | 待办中心 | - | `views/approvalTask/List.vue` | `src/router/modules/approvalTask.js` | 计数条/类型多选筛选(5行)/状态多选/归属=挂在我名下(认领后1行)/认领/释放/完成/撤销(空理由校验+有理由成功)/详情抽屉/裁决抽屉(审批底座503 fail-closed 提示正确)/刷新全通；双端加载正常 |
| 24 | [x] | `/asset-bundle/list` | AssetBundleList | 资产包管理 | 是 | `views/assetBundle/List.vue` | `src/router/modules/assetBundle.js` | 筛选/搜索/分页/热启用条/热启停/发布/删除/Playground跳转/商户编辑全通；修复缺陷#4(keyword不搜asset_id,后端repo)+缺陷#5(hotPlugCache candidateIDs去重循环自我复制致enabled/list翻倍膨胀,后端service)；双端回归通过 |
| 25 | [x] | `/asset-bundle/playground` | AssetBundlePlayground | 开发者 Playground | 是 | `views/assetBundle/Playground.vue` | `src/router/modules/assetBundle.js` | 新建(SaaS:名称+asset_id 保存跳编辑)/Few-Shot增删(user/assistant/system+成对)/空保存校验「请填写资产 ID」/沙箱对话(未配Ollama报错友好)/保存/发布(确认框+本地已发布)全通；双端无错误 |
| 26 | [x] | `/asset-bundle/playground/:aid` | AssetBundlePlaygroundEdit | 开发者 Playground | 是 | `views/assetBundle/Playground.vue` | `src/router/modules/assetBundle.js` | aid=asset_id 预填正确+行业编辑保存「更新成功」；无效 aid 数字id 防御良好(禁用保存/发布+提示重新选择)；SaaS 编辑预填正常；双端无错误 |
| 27 | [x] | `/asset-bundle/merchant-new` | AssetBundleMerchantNew | 商户新建话术包 | 是 | `views/assetBundle/MerchantEditor.vue` | `src/router/modules/assetBundle.js` | 空保存校验/新建→跳编辑/暂停·恢复托管/QA卡增删/应答持久化/动作按钮链/语气·反审查·门禁下拉单选全通；双端验证，测试包已删 |
| 28 | [x] | `/asset-bundle/merchant/:aid` | AssetBundleMerchantEdit | 商户配置话术包 | 是 | `views/assetBundle/MerchantEditor.vue` | `src/router/modules/assetBundle.js` | 同 MerchantEditor：编辑预填(资产ID禁改)、保存热更新、托管启停、QA卡与按钮链持久化全通；双端验证，测试包已删 |
| 29 | [x] | `/asset-market` | AssetMarket | 资产市场 | 是 | `views/assetMarket/Market.vue` | `src/router/modules/assetMarket.js` | 市场双端空态正常(共0条/暂无资产)；类型下拉5项/查询/我的资产跳转全通，无错误 |
| 30 | [x] | `/asset-market/detail/:id` | AssetMarketDetail | 资产详情 | 是 | `views/assetMarket/Detail.vue` | `src/router/modules/assetMarket.js` | 有效id详情完整(封面占位/类型/行业/版本/免费试用/本地已同步提示/JSON预览)；无效id→「资产不存在或不可用」+alert；返回=$router.back 回市场；页面导览两步+完成/×关闭正常(缺陷#1无复发)；免费试用确认→平台集成未启用提示(设计内 fail-closed)；双端验证 |
| 31 | [x] | `/asset-market/my-assets` | AssetMyAssets | 我的资产 | 是 | `views/assetMarket/MyAssets.vue` | `src/router/modules/assetMarket.js` | 双端列表/筛选(类型·来源·关键词)/查看drawer JSON/停用·启用/删除(取消+确认)/自建资产(名称校验·JSON校验缺system_prompt·创建成功后已删)/同步·上报使用(平台集成未启用提示=设计内 fail-closed)全通 |
| 32 | [x] | `/asset-market/sync-log` | AssetSyncLog | 同步日志 | 是 | `views/assetMarket/SyncLog.vue` | `src/router/modules/assetMarket.js` | 双端日志表/按资产ID查询命中3行/返回按钮全通；历史 failed 记录为 platform error 401 签名错误(平台未启用期间遗留) |
| 33 | [x] | `/backup/list` | BackupList | 备份恢复 | 是 | `views/backup/List.vue` | `src/router/modules/backup.js` | 搜索/类型/状态筛选、立即备份(创建+轮询completed)、详情/恢复dialog、删除(测试数据已清)、恢复记录tab筛选全通；双端验证 |
| 34 | [x] | `/backup/enhanced` | BackupEnhanced | 备份管理 | 是 | `views/backup/Enhanced.vue` | `src/router/modules/backup.js` | 统计卡/策略卡(switch·周几·保留天数)保存并持久化/预览影响/恢复危险确认/立即备份/删除全通；206行系vite重启前SPA残留态，硬刷新后与API一致；双端 |
| 35 | [x] | `/badCase/list` | BadCaseList | Bad Case 队列 | - | `views/badCase/List.vue` | `src/router/modules/badCase.js` | 打标/撤销/补录/导出评测集/详情 drawer/来源·状态·归因·责任层筛选/分页全通；双端验证 |
| 36 | [x] | `/batchOperation/list` | BatchOperationList | 批量操作 | 是 | `views/batchOperation/List.vue` | `src/router/modules/batchOperation.js` | 工具卡/预览(dryRun)/详情弹窗全通；修复缺陷#6：历史表与详情绑定字段与后端蛇形 JSON 不符致整列空白 |
| 37 | [x] | `/bridge/token` | BridgeTokenManagement | 桥接凭证 | - | `views/bridge/TokenManagement.vue` | `src/router/modules/bridgeToken.js` | 状态标签/灰度提示/轮换(明文仅显示一次)/复制按钮禁用态与成功全通；轮换会持续写 PREV 属设计内；双端验证 |
| 38 | [x] | `/bridge/extension` | BridgeExtensionPackage | 扩展安装包 | 是 | `views/bridge/ExtensionPackage.vue` | `src/router/modules/bridgeToken.js` | 未接平台提示/安装步骤/双复制/刷新全通；双端验证 |
| 39 | [x] | `/bridge` | BridgeRoot | 渠道接入 | - | `-` | `src/router/modules/bridgeToken.js` | 重定向 → /bridge/token，无错误 |
| 40 | [x] | `/token` | - |  | - | `-` | `src/router/modules/bridgeToken.js` | 重定向 → /bridge/token，无错误 |
| 41 | [x] | `/browser-automation` | BrowserAutomation | 浏览器自动化 | 是 | `-` | `src/router/modules/browserAutomation.js` | 重定向 → /browser-automation/tasks；双端无错误 |
| 42 | [x] | `/browser-automation/tasks` | BrowserAutomationList | 任务列表 | 是 | `views/browserAutomation/List.vue` | `src/router/modules/browserAutomation.js` | 状态筛选(失败→20行failed)/分页(第2页525)/执行·详情·编辑跳转；无Host引导弹窗8001；双端 |
| 43 | [x] | `/browser-automation/tasks/create` | BrowserAutomationCreate | 新建任务 | 是 | `views/browserAutomation/Editor.vue` | `src/router/modules/browserAutomation.js` | 空保存校验(请填写起始URL)/预设模板套用/创建→列表draft/编辑预填改存/删除确认；测试数据已删；双端 |
| 44 | [x] | `/browser-automation/tasks/:id/edit` | BrowserAutomationEditor | 编辑任务 | 是 | `views/browserAutomation/Editor.vue` | `src/router/modules/browserAutomation.js` | 编辑预填正确、改名保存→已保存→回列表；双端 |
| 45 | [x] | `/browser-automation/tasks/:id` | BrowserAutomationDetail | 任务详情 | 是 | `views/browserAutomation/Detail.vue` | `src/router/modules/browserAutomation.js` | 信息表/步骤编排/执行历史/回执/定时触发器启停删除全通；双端验证 |
| 46 | [x] | `/browser-automation/sessions/:id` | BrowserAutomationMonitor | 执行监控 | 是 | `views/browserAutomation/Monitor.vue` | `src/router/modules/browserAutomation.js` | 耗时/成功率/错误表、步骤执行、命令流方向筛选(command/event/judge 全验证)、导出审计包(「审计包已导出」)全通；小瑕疵：方向筛选空值时显示「请选择」而非「全部」(EP 空串=未选中，装饰性)；双端验证 |
| 47 | [x] | `/browser-automation/cron` | BrowserAutomationCron | 定时触发器 | 是 | `views/browserAutomation/Cron.vue` | `src/router/modules/browserAutomation.js` | 启停/编辑改表达式/新建校验与取消/删除确认取消/锚点跳任务、新建cron任务跳create全通；SaaS双行一致 |
| 48 | [x] | `/browser-automation/status` | BrowserAutomationStatus | Host 状态 | 是 | `views/browserAutomation/Status.vue` | `src/router/modules/browserAutomation.js` | 离线alert+四步安装指引/刷新/重置Token确认框(取消+确定,API确认轮换)全通；SaaS一致 |
| 49 | [x] | `/browser-automation/ego/flows` | EgoFlows | 我的流程 | 是 | `views/browserAutomation/ego/Flows.vue` | `src/router/modules/browserAutomation.js` | 新建（契约说明/空提交校验/保存即静态扫描通过）/重扫/试跑下发(created态提示)/发布/提交审核(仅published可点)/删除全通；SaaS空态一致。测试流程已删 |
| 50 | [x] | `/browser-automation/ego/sessions` | EgoSessions | 通用会话 | 是 | `views/browserAutomation/ego/Sessions.vue` | `src/router/modules/browserAutomation.js` | 新建会话（空域名白名单→「域名白名单必填」校验；runner 未连接→拒绝创建 toast）、取消关闭、空列表/刷新全通；SaaS一致 |
| 51 | [x] | `/browser-automation/ego/executions` | EgoExecutions | 执行监控 | 是 | `views/browserAutomation/ego/Executions.vue` | `src/router/modules/browserAutomation.js` | 状态筛选8项/详情弹窗(状态轮次事件流)/重发(toast已重发)/停止(已停止,API fail_reason=用户停止)/已停止筛选回显全通；SaaS一致。测试执行#11留存无破坏 |
| 52 | [x] | `/browser-automation/ego/runner` | EgoRunner | Runner 状态 | 是 | `views/browserAutomation/ego/Runner.vue` | `src/router/modules/browserAutomation.js` | 离线alert+四步安装指引/令牌表(签发明文只显一次+复制toast+撤销确认→已撤销状态)全通；测试令牌已撤销；SaaS一致 |
| 53 | [x] | `/browser-automation/ego/assets` | EgoAssets | 资产中心 | 是 | `views/browserAutomation/ego/Assets.vue` | `src/router/modules/browserAutomation.js` | 空态提示「管理员在资产发布台发布后可见」+刷新按钮；双端一致，无错误 |
| 54 | [x] | `/browser-automation/ego/assets/admin` | EgoAssetAdmin | 资产发布台 | 是 | `views/browserAutomation/ego/AssetAdmin.vue` | `src/router/modules/browserAutomation.js` | 三tab：待审流程空态/直接发布脚本包(空提交→「请输入资产名称」,填写发布→资产编码slug化「已上架」)/资产目录(下线确认→已下线)；测试资产已下线；SaaS一致 |
| 55 | [x] | `/browser-automation/ego/telemetry` | EgoTelemetry | 遥测看板 | 是 | `views/browserAutomation/ego/Telemetry.vue` | `src/router/modules/browserAutomation.js` | 4 个时间窗（1h/24h/7d/30d）radio-button 切换生效、刷新正常、空态文案正确（无在线 runner 无样本）；双端一致，无 BAD |
| 56 | [x] | `/chatChannel/list` | ChatChannelList | 客服渠道 | 是 | `views/chatChannel/List.vue` | `src/router/modules/chatChannel.js` | 关键词(名称/AppKey)搜索、状态筛选+清除、复制AppKey、编辑/安装引导跳转、禁用·启用确认、轮换Key(新旧替换)、重置Secret(仅显示一次)、分页(10/页×2页)、刷新全通；双端验证 |
| 57 | [x] | `/chatChannel/create` | ChatChannelCreate | 新建客服渠道 | 是 | `views/chatChannel/Create.vue` | `src/router/modules/chatChannel.js` | 空提交校验(请输入渠道名称/至少配置一个origin)、创建成功dialog(AppKey/AppSecret仅显示一次+复制+集成代码)、关闭后列表新增验证；双端校验一致 |
| 58 | [x] | `/chatChannel/edit/:id` | ChatChannelEdit | 编辑客服渠道 | 是 | `views/chatChannel/Edit.vue` | `src/router/modules/chatChannel.js` | 预填(渠道ID/AppKey只读+名称/origin/欢迎语)、欢迎语修改保存→已保存→回列表、持久化验证；双端一致 |
| 59 | [x] | `/chatChannel/install-guide/:id?` | ChatChannelInstallGuide | Widget 安装引导 | 是 | `views/chatChannel/InstallGuide.vue` | `src/router/modules/chatChannel.js` | 渠道单选(默认首条/切换)、嵌入代码(含AppKey+HOST)、复制全部、白名单预填+重置、实时预览浮标开关(欢迎语正确)、测试API连通性→连接成功；双端验证 |
| 60 | [x] | `/churnPrediction/list` | ChurnPredictionList | 流失预警 | 是 | `views/churnPrediction/List.vue` | `src/router/modules/churnPrediction.js` | 双tab+统计卡；用户ID搜索、详情弹窗(评分/原因分析)、干预弹窗(4种方式)、预警列表标记已处理(备注)、模型配置权重/阈值修改保存持久化+恢复、运行预测→预测完成；双端验证 |
| 61 | [x] | `/clue/list` | ClueList | 线索列表 | - | `views/clue/List.vue` | `src/router/modules/clue.js` | 统计卡/关键字搜索(名称·账号·城市·地址下推DB)/类型·验证状态筛选/日期范围/重置/详情弹窗/导入(校验+无效行跳过+成功导入+列表刷新)/UI删除确认全通；分配·编辑为 stub toast(ElMessage.info, 已触发分配：/编辑线索：, i18n 键已存在)——功能性缺口但为既有设计占位；测试线索已清理 |
| 62 | [x] | `/clue/statistics` | ClueStatistics | 线索统计 | 图表实例 Map 修复(4图全渲染)/日期区间真实聚合/趋势·来源假数据改真实聚合/本月新增真实值/trend line·bar 切换/清空恢复全量；双端验证 | `views/clue/Statistics.vue` | `src/router/modules/clue.js` | - |
| 63 | [x] | `/community/list` | CommunityList | 社群管理 | 缺陷#10修复：状态筛选/状态列/消息数列/编辑状态radio对后端不存在的字段，已按后端真实模型删除；搜索/重置/成员·消息·统计弹窗/新增分组(空校验+创建+编辑+删除)/导出(下载)/分页全通；双端 | `views/community/List.vue` | `src/router/modules/community.js` | - |
| 64 | [x] | `/conversionFunnel/list` | ConversionFunnelList | 转化漏斗 | 是 | `views/conversionFunnel/List.vue` | `src/router/modules/conversionFunnel.js` | KPI/漏斗图/阶段明细/阶段流失分析(阶段切换+查询)全通；修复上一阶段为0时比值·流失率误显0.0%(改—)，双端验证 |
| 65 | [x] | `/customReport/list` | CustomReportList | 自定义报表 | 是 | `views/customReport/List.vue` | `src/router/modules/customReport.js` | 新建(必填校验)/维度·指标多选/图表类型/查看/编辑/删除全通；修复查看弹窗表头显示内部字段名(count/date→中文标签)、导出仅弹toast不落盘(改真实CSV下载)；双端验证 |
| 66 | [x] | `/customer360/list` | Customer360List | 客户 360 | 是 | `views/customer360/List.vue` | `src/router/modules/customer360.js` | 左栏搜索/选择客户/5 tab 切换/标签添加删除持久化/联系客户空消息校验/订单空态全通；修复行为轨迹·沟通记录永不加载(接 events+messages 接口+空态)；双端验证 |
| 67 | [x] | `/customer360/consents` | CustomerConsents | 客户同意与偏好 | 是 | `views/customer360/Consents.vue` | `src/router/modules/customer360.js` | one_id 加载/授权·撤回追加(append-only 事实账)/偏好新增·编辑/空 ID 与无效 ID 防御全通；测试 revoke 事件与偏好快照留在 r53user3（设计内无删除）；双端验证 |
| 68 | [x] | `/customerEvent/list` | CustomerEventList | 客户事件 | 是 | `views/customerEvent/List.vue` | `src/router/modules/customerEvent.js` | 统计卡联动日期区间(126/7/1/126 对照)/类型筛选/详情/创建自定义事件/分页全通；修复统计卡窗口口径提示+分页size笔误+创建事件客户下拉全undefined(list是map未拍平)；双端验证 |
| 69 | [x] | `/customerJourney/dashboard` | CustomerJourneyDashboard | 客户旅程大屏 | 是 | `views/customerJourney/Dashboard.vue` | `src/router/modules/customerJourney.js` |KPI/10阶段漏斗/阶段点击客户明细(已留资1人·已成交2人)/阶段配置表/分布表/事件流空态/刷新时间戳更新/自动刷新30s开关(31s后时间戳00:15:13变化)全通；双端验证 |
| 70 | [x] | `/customerService/agentStatus` | AgentStatus | 坐席状态 | 是 | `views/customerService/AgentStatus.vue` | `src/router/modules/customerService.js` | 统计卡/新增坐席(必填校验)/修改状态(在线·忙碌·离线+上限)/上下线(有会话下线保护)/查看会话全通；测试坐席 DB 软删清理；双端验证 |
| 71 | [x] | `/customerService/quickReply` | QuickReply | 快捷回复 | 是 | `views/customerService/QuickReply.vue` | `src/router/modules/customerService.js` | 分类筛选(选项=后端[]string契约)/关键词搜索/新增(必填校验)/编辑/删除(确认取消+确定)全通；测试数据已清理；双端验证 |
| 72 | [x] | `/customerService/sessionTag` | SessionTag | 会话标签 | 是 | `views/customerService/SessionTag.vue` | `src/router/modules/customerService.js` | 分组筛选/搜索/新增/编辑(标识禁改+预填)/删除全通，测试数据已清理；双端验证 |
| 73 | [x] | `/customerService/aiSuggestion` | AISuggestion | AI 建议 | 是 | `views/customerService/AISuggestion.vue` | `src/router/modules/customerService.js` | 空ID拦截/会话加载(776·962行数据)/置信度进度条/采纳→已使用(测试后DB还原is_used=false)/空态全通；双端验证 |
| 74 | [x] | `/customerService/csat` | CsatDashboard | CSAT 看板 | 是 | `views/customerService/CsatDashboard.vue` | `src/router/modules/customerService.js` | 统计卡(均分5/好评率100%/总评1/差评0)/趋势图+分布图 canvas/差评列表空态全通；双端验证 |
| 75 | [x] | `/customerSession/list` | CustomerSessionList | 客服会话 | 是 | `views/customerSession/List.vue` | `src/router/modules/customerSession.js` | 状态筛选/快捷回复搜索插入/SOP阶段保存/黑名单查看/新建会话/接管/发消息/标签增删落库/发券·商品卡·AI摘要·转录·暂缓·优先级·拉黑·结束全通；修复#16(composable漏导出add/removeSessionTag+tags number[]→String[]、回显String比对)；测试会话已结束；双端验证 |
| 76 | [x] | `/dashboardScreen/list` | DashboardScreenList | 数据大屏 | 是 | `views/dashboardScreen/List.vue` | `src/router/modules/dashboardScreen.js` | 6KPI/6图表canvas/实时活动/时钟5s/全屏切换/大屏构建跳转全通；双端验证 |
| 77 | [x] | `/dashboardScreen/builder` | DashboardScreenBuilder | 大屏构建 | 是 | `views/dashboardScreen/Builder.vue` | `src/router/modules/dashboardScreen.js` | 组件库6类添加(折线/柱状/饼图/数字卡/漏斗/地图占位)/↔宽度50%↔100%/×删块/保存确认框全通；修复#17保存成功toast误报失败(ElMessage第二参是appContext非options,改单参对象)；测试看板已删；双端验证 |
| 78 | [x] | `/dialogueMemory/list` | DialogueMemoryList | 对话记忆 | 是 | `views/dialogueMemory/List.vue` | `src/router/modules/dialogueMemory.js` | 列表/搜索/详情7tab/SOP历史(字符串形态修复#18)/构建上下文/意向手动调整(修复#19 session_id+level 契约)/客户列空ID数据态全通；双端验证 |
| 79 | [x] | `/dingtalk-app` | DingtalkApp | 钉钉应用 | - | `views/dingtalkApp/DingtalkAppAccount.vue` | `src/router/modules/dingtalkApp.js` | 新增/编辑(预填·AppSecret掩码)/删除/复制回调URL/连通测试全通；修复「绑定AI」弹窗打不开(组件prop是modelValue却用v-model:visible，另account-name/current-agent-id错传)，改后弹窗正常、添加绑定→绑定成功→解绑成功；双端验证 |
| 80 | [x] | `/domainPool` | DomainPool | 域名池管理 | 是 | `views/domainPool/List.vue` | `src/router/modules/domainPool.js` | 新增(空校验+创建)/单条检查/编辑/删除(确认)/检查全部/状态筛选(正常=1条)/关键字搜索/重置/翻页全通；双端无控制台报错、无非2xx |
| 81 | [x] | `/douyinCard` | DouyinCard | 抖音卡片 | - | `views/douyinCard/List.vue` | `src/router/modules/douyinCard.js` | 新增(标题/图片必填校验+创建)/编辑预填/删除(确认)/统计跳转/复制链接(非安全上下文失败有提示)/关键词搜索/状态筛选/重置/分页全通；双端无非2xx |
| 82 | [x] | `/douyin/stats` | DouyinStats | 抖音卡片统计 | - | `views/douyinCard/Stats.vue` | `src/router/modules/douyinCard.js` | KPI卡/访问趋势/热门卡片/最近活动/刷新/按天·按周·按月切换/日期区间选择(2026-10-21~27)/单卡统计跳转全通；双端无报错 |
| 83 | [x] | `/douyin-card-stats/:id` | DouyinCardStats | 抖音卡片详情统计 | - | `views/douyinCard/CardStats.vue` | `src/router/modules/douyinCard.js` | 卡片信息/总浏览量/访问趋势/最近活动表/刷新/返回(router.go(-1))/图片缺失占位全通；双端无报错 |
| 84 | [x] | `/email` | Email | 邮件列表 | - | `views/email/EmailList.vue` | `src/router/modules/email.js` | 缺陷修复：发送弹窗缺收件人/发件账号，恒 400(to/subject/content/smtpId必填)；已补收件人+代理下拉+四项校验+immediateSend，实发成功；列表/刷新全通；双端 |
| 85 | [x] | `/email/drafts` | EmailDrafts | 我的草稿 | - | `views/email/Drafts.vue` | `src/router/modules/email.js` | 缺陷修复：草稿“发送”直发无收件人/代理恒 400；改为发送弹窗(收件人+代理+校验)；新建(空校验/富文本)/编辑预填/删除(确认)/拖拽编辑器跳转全通；双端 |
| 86 | [x] | `/email/jobs` | EmailJobs | 我的任务 | - | `views/email/Jobs.vue` | `src/router/modules/email.js` | 任务表8列/空值/分页1→2切换全通；双端无报错 |
| 87 | [x] | `/email/smtp` | EmailSmtp | 邮件账号 | - | `views/email/Smtp.vue` | `src/router/modules/email.js` | 新增(5项必填校验+创建)/编辑预填+更新/删除(确认)全通；27条存量渲染正常；双端 |
| 88 | [x] | `/email/info` | EmailInfo | 邮件代理 | - | `views/email/Info.vue` | `src/router/modules/email.js` | 11家服务商对照表(厂家/SMTP/端口/认证/官网链接)渲染正常，前往注册链接地址核实正确；双端 |
| 89 | [x] | `/email/guide` | EmailGuide | 使用引导 | - | `views/email/Guide.vue` | `src/router/modules/email.js` | 4步引导+常见问题渲染；4个“前往操作”分别跳 /email/smtp、/email/list、/email/drafts、/email/jobs 全部命中；双端 |
| 90 | [x] | `/email/deliverability` | EmailDeliverability | 送达分析 | 是 | `views/email/Deliverability.vue` | `src/router/modules/email.js` | 4张KPI(送达/打开/软退/硬退)+域名信誉空态+退信分类图表+刷新全通；双端无报错 |
| 91 | [~] | `/email/drag-editor` | EmailDragEditor | 拖拽编辑器 | 是 | `views/email/DragEditor.vue` | `src/router/modules/email.js` | 6类内容块添加/上下移/删除/主题+发件人/保存草稿/桌面·移动预览全通；发送测试503为本地未配发件账号(设计内拒绝，提示清晰)；双端 |
| 92 | [x] | `/faq` | FAQ | FAQ 知识库 | - | `views/faq/Index.vue` | `src/router/modules/faq.js` | 三张能力卡(进入/打开/去绑定)跳转验证：进入→/faq/list、打开→/faq/list、去绑定→/aiAgent/list；双端无报错（“打开”未直接弹匹配测试，文案略含糊，非功能缺陷） |
| 93 | [x] | `/faq/list` | FaqList | FAQ 列表 | - | `views/faq/List.vue` | `src/router/modules/faq.js` | 关键词/分类/状态筛选+搜索重置、行内测试弹窗、匹配测试(智能体+问句)全通；修复分页绑定错 ref 导致翻页不刷新(缺陷#26)；双端 13393/13343 条 |
| 94 | [x] | `/faq/editor` | FAQEditor | FAQ 编辑 | - | `views/faq/Editor.vue` | `src/router/modules/faq.js` | 空提交校验(问题·标准答案)/关键词·分类·意图·智能体·置信度·启用全字段/创建成功跳列表；测试条目已清理 |
| 95 | [x] | `/faq/editor/:id` | FAQEditorEdit | FAQ 编辑 | - | `views/faq/Editor.vue` | `src/router/modules/faq.js` | 编辑预填(问题·答案·关键词·置信度)/改答案保存「FAQ 更新成功」/跳回列表；测试条目已删除 |
| 96 | [x] | `/feishu` | Feishu | 飞书 | - | `views/feishu/FeishuAccount.vue` | `src/router/modules/feishu.js` | 新增(3项必填校验+创建)/编辑预填/删除(确认)/刷新全通；16条存量正常；双端 |
| 97 | [~] | `/feishu/account` | FeishuAccount | 飞书账号 | - | `views/feishu/FeishuAccount.vue` | `src/router/modules/feishu.js` | 同 #96 组件：测试发送弹窗(接收者/内容/类型)与刷新Token走线正常，假凭证返回 502 code=10003 属设计内上游拒绝；保存绑定无智能体时接受(空绑定)；双端 |
| 98 | [~] | `/followups/today` | FollowUpToday | 今日跟进 | 是 | `views/followups/Today.vue` | `src/router/modules/followups.js` | 页面三块(已逾期/今日日历/全部待办)+刷新渲染正常；/api/followups/{today,pending,overdue} 双端均 503“跟进服务未装配”，页面已显式说明(设计内未装配，非空数据错觉) |
| 99 | [~] | `/followups/:id` | FollowUpDetail | 跟进详情 | 是 | `views/followups/Today.vue` | `src/router/modules/followups.js` | /followups/1 命中同 Today 组件(组件不消费 :id，等同今日跟进页)；服务未装配同上，无报错；双端 |
| 100 | [x] | `/geo-tools/audit-center` | GeoAuditCenter | 审计中心 | 是 | `views/geo/AuditCenter.vue` | `src/router/modules/geoTools.js` | - 缺陷修复：报告按钮 await 后 window.open 被弹窗拦截(无反应)，改为手势内占位开窗+失败回退 a[target=_blank]；详情/查询/复查对比(类型不同给出不可比)/开始体检空 URL 校验全通；报告 API 200 HTML；双端 |
| 101 | [x] | `/geo-tools/brand-config` | GeoBrandConfig | 品牌与域名 | 是 | `views/geo/BrandConfig.vue` | `src/router/modules/geoTools.js` | - llms.txt v2/robots.txt 双块生成预览/规范检查清单渲染正常；双端无报错 |
| 102 | [x] | `/geo-tools/seo-infra` | GeoSeoInfra | SEO 基础设施 | 是 | `views/geo/SeoInfra.vue` | `src/router/modules/geoTools.js` | - llms.txt v2 / robots.txt / sitemap.xml 三 tab 切换与生成全通(robots 白名单、sitemap 全量 URL)；双端无报错 |
| 103 | [x] | `/geo-tools/pusher-config` | GeoPusherConfig | 蜘蛛推送配置 | 是 | `views/geo/PusherConfig.vue` | `src/router/modules/geoTools.js` | - 缺陷修复：同平台重复新增撞唯一索引原提示“操作失败，请稍后重试”，后端映射为“平台 X 已存在配置，请直接编辑现有配置”+单测；新增/编辑/删除/JSON 格式校验全通；双端 |
| 104 | [x] | `/geo-tools/site-config` | GeoSiteConfig | 静态站部署 | 是 | `views/geo/SiteConfig.vue` | `src/router/modules/geoTools.js` | - 缺陷修复：必填校验英文 “domain required” 且重复弹两次，后端改为“域名不能为空”；新增/编辑/删除/路径白名单约束提示全通；双端 |
| 105 | [x] | `/geo-tools/schema-templates` | GeoSchemaTemplates | Schema 模板 | 是 | `views/geo/SchemaTemplates.vue` | `src/router/modules/geoTools.js` | - 新增(空校验+JSON模板)/编辑预填/删除全通；模板 JSON 校验文案已中文化；双端 |
| 106 | [x] | `/geo-tools/competitors` | GeoCompetitors | 竞品管理 | 是 | `views/geo/CompetitorManage.vue` | `src/router/modules/geoTools.js` | - 新增(名称+域名必填)/编辑预填/删除(确认)全通；5条存量正常；双端 |
| 107 | [~] | `/geo-tools/config` | GeoConfigOptimizer | 配置优化 | 是 | `views/geo/ConfigOptimizer.vue` | `src/router/modules/geoTools.js` | - 保存配置/+优势·竞品·负面词 chip 增删全通；优化配置在无大模型时明确提示“请先在模型供应商中启用”(设计内)；双端 |
| 108 | [x] | `/geo-tools/autopilot` | GeoAutopilot | 自动驾驶（一键） | 是 | `views/geo/Autopilot.vue` | `src/router/modules/geoTools.js` | - 空关键词时启动按钮禁用；运行历史详情时间线/步骤状态/查看关键词跳转全通；双端 |
| 109 | [x] | `/geo-tools/keyword-mining` | GeoKeywordMining | 关键词蒸馏 | 是 | `views/geo/KeywordMining.vue` | `src/router/modules/geoTools.js` | - 三个动作空种子词校验/刷新/查询全通(空库空态)；双端无报错 |
| 110 | [x] | `/geo-tools/content-creation` | GeoContentCreation | 内容创作 | 是 | `views/geo/ContentCreation.vue` | `src/router/modules/geoTools.js` | - 生成配置/空校验/生成内容(骨架模板降级)/内容评分/复制(环境剪贴板受限有提示)全通；双端 |
| 111 | [x] | `/geo-tools/content-optimize` | GeoContentOptimize | 文章优化 | 是 | `views/geo/ContentOptimize.vue` | `src/router/modules/geoTools.js` | - 缺陷修复：评分接口分数在 data.scores.total，页面读 res.total 永远显示“—”；已回填。heuristic_fallback 时原提示“优化完成”改为“仅规则检查、正文未改写”；校验/评分/优化/复制全通；双端 |
| 112 | [~] | `/geo-tools/site-publish` | GeoSitePublish | 发布到官网 | 是 | `views/geo/SitePublish.vue` | `src/router/modules/geoTools.js` | - 缺陷修复：部署/全链路 500 只给通用文案且页面无反馈；改为 _silent + 展示具体命令错误；仅导出25篇/仅部署/全链路走线正常，测试站无 git 远端属设计内失败；双端 |
| 113 | [x] | `/geo-tools/push-center` | GeoPushCenter | 蜘蛛推送 | 是 | `views/geo/PushCenter.vue` | `src/router/modules/geoTools.js` | - 缺陷修复：空 URL 点推送无任何反馈；补 URL/平台必选提示。配额卡/三平台勾选/推送走线正常(未启用平台 400 明确)；双端 |
| 114 | [~] | `/geo-tools/platform-publish` | GeoPlatformPublish | 多平台发布 | 是 | `views/geo/PlatformPublish.vue` | `src/router/modules/geoTools.js` | 平台表/配置账号弹窗/测试发布fail-closed提示/发布记录表全通 缺陷确认：执行发布在未选文章/平台时禁用(正确)；配置账号弹窗(GitHub README 等)校验正常；测试发布无凭证时明确提示“未配置 GitHub 凭证…”，设计内拒绝；双端 |
| 115 | [x] | `/geo-tools/workflow` | GeoWorkflow | 工作流 | 是 | `views/geo/WorkflowEditor.vue` | `src/router/modules/geoTools.js` | 工作流列表切换/步骤编排/节点面板/执行监控/保存成功 新建/添加触发器·动作·结束节点/保存/删除/执行监控表全通(删除无二次确认，风险已记录)；双端 |
| 116 | [x] | `/geo-tools/knowledge-base` | GeoKnowledgeBase | GEO 知识库 | 是 | `views/geo/KnowledgeBase.vue` | `src/router/modules/geoTools.js` | 统计卡/560行文档列表(等级·公开/抽取实体·删除)全通 缺陷修复：KB 检索只做整句子串匹配，Ask “HiveMTK 是什么？”永远答“暂无相关内容”；整句0命中时按词元(≥2字符)兜底并对标题加权，新增单测。上传(仅文本格式校验)/搜索(回车)/抽取实体/Ask 全通；双端 |
| 117 | [x] | `/geo-tools/entity-graph` | GeoEntityGraph | 实体图谱 | 是 | `views/geo/EntityGraph.vue` | `src/router/modules/geoTools.js` | 实体列表/分页/点击实体关系图/生成JSON-LD Schema预览全通 缺陷修复：未选实体点“生成 Schema”按钮为禁用态(本来就正确)，另补了守卫提示。358实体分页/选实体关系图/生成 JSON-LD/复制全通；双端 |
| 118 | [x] | `/geo-tools/funnel-dashboard` | GeoFunnelDashboard | 漏斗总览 | 是 | `views/geo/FunnelDashboard.vue` | `src/router/modules/geoTools.js` | KPI(种子词/文章/已部署)/搜索引擎收录表/AI引擎引用表/转化比/刷新全通；无canvas，无BAD 漏斗 KPI(种子词/文章/已部署/长尾→文章/文章→部署)/收录表空态/AI引用空态/刷新全通；双端 |
| 119 | [x] | `/geo-tools/index-tracking` | GeoIndexTracking | 收录与引用 | 是 | `views/geo/IndexTracking.vue` | `src/router/modules/geoTools.js` | 域名收录表+URL引用表(25行)/刷新/一键全量验证按钮；无BAD 三tab(收录列表/引用漂移/引用分析)/刷新/一键全量验证按钮/两次快照对比(新增·消失·计数·排名四表)/域名CSV下载全通；双端 |
| 120 | [x] | `/geo-tools/visibility` | GeoVisibilityBoard | 可见性趋势 | 是 | `views/geo/VisibilityBoard.vue` | `src/router/modules/geoTools.js` | AI可见率KPI(4卡)/7·30·90天时间窗/品牌可见率趋势+引擎对比canvas/引擎对比4行/探针记录54行+引擎筛选/刷新全通 立即探测一轮(落库成功)/刷新/54条记录分页全通；双端 |
| 121 | [~] | `/geo-tools/sov-board` | GeoSovBoard | 竞品 SOV | 是 | `views/geo/SovBoard.vue` | `src/router/modules/geoTools.js` | SOV KPI(88.50%/竞品3)/1 canvas/竞品表4行/刷新全通(模拟数据提示已展示) 页面明确标注“当前全部为模拟探针(LLM扮演AI搜索)”，SOV/品牌明细空态+刷新正常；无真实AI引擎读数属设计内；双端 |
| 122 | [x] | `/geo-tools/crawler-stats` | GeoCrawlerStats | 爬虫统计 | 是 | `views/geo/CrawlerStats.vue` | `src/router/modules/geoTools.js` | AI Bot覆盖度KPI(48.5%等7卡)/表86行/刷新/跑一轮爬虫按钮；无BAD 86条爬虫UA记录/刷新/跑一轮爬虫全通；双端 |
| 123 | [~] | `/geo-tools/verification` | GeoVerification | 多模型验证 | 是 | `views/geo/Verification.vue` | `src/router/modules/geoTools.js` | 验证配置表单(文章/品牌/模型/问题)+验证+导入真实结果+负面监控空态/检测负面；空提交提示"请输入测试问题" 验证缺问题提示“请输入测试问题”；文章/品牌/模型/问题表单与导入真实结果、负面检测走线正常；探针为模拟(页面已声明)；双端 |
| 124 | [x] | `/geo/decision-report` | GeoDecisionReport | 决策链报表 | 是 | `views/geo/DecisionReport.vue` | `src/router/modules/geoTools.js` | 捕获线索(K4)1/缺口任务0/已补位1 + 信源缺口补位队列表空态；无BAD |
| 125 | [x] | `/geo-tools/reports` | GeoReports | 成本报表 | 是 | `views/geo/Reports.vue` | `src/router/modules/geoTools.js` | KPI 571/586946/10/200/1640/¥58 与成本明细12行一致；早前全0为加载瞬时；无BAD |
| 126 | [x] | `/geo-tools/dual-engine` | GeoDualEngine | 双端监测 | 是 | `views/geo/DualEngineBoard.vue` | `src/router/modules/geoTools.js` | AI探针KPI/引擎明细4行/GSC查询明细/导入GSC弹窗/刷新/近30天窗口；无BAD |
| 127 | [x] | `/geo-tools/capture-links` | GeoCaptureLinks | 承接链接 | 是 | `views/geo/CaptureLinks.vue` | `src/router/modules/geoTools.js` | 新建承接链接(目标链接必填校验+取消)/列表1行/搜索框；行内统计无可见弹窗(待定)；无BAD |
| 128 | [x] | `/geo-tools/alerts` | GeoAlertCenter | 告警中心 | 是 | `views/geo/AlertCenter.vue` | `src/router/modules/geoTools.js` | 预警中心表20行(已确认·未确认/级别/类型/引擎/关键词/摘要)+刷新+本页全部确认；无BAD |
| 129 | [x] | `/glossary` | GlossaryList | 术语表管理 | 是 | `views/glossary/List.vue` | `src/router/modules/glossary.js` | 新增术语弹窗(术语ID/分类/保留原样/译法/正则)/筛选/表格2行/刷新全通；无 BAD |
| 130 | [x] | `/i18n/dashboard` | I18nDashboard | 多语言监控 | 是 | `views/i18n/Dashboard.vue` | `src/router/modules/i18nStats.js` | 时间窗/刷新/KPI(无翻译流量真实空态)/术语覆盖表(English 75%·简体中文66.7%)全通；无 BAD |
| 131 | [x] | `/inbox/list` | InboxList | 统一收件箱 | - | `views/inbox/List.vue` | `src/router/modules/inbox.js` |  |
| 132 | [x] | `/integration/list` | IntegrationList | 第三方对接 | 是 | `views/integration/List.vue` | `src/router/modules/integration.js` | - 19条集成/添加集成弹窗(平台/账号/Key/Secret/回调/JSON扩展)/日志弹窗/配置弹窗全通；双端 |
| 133 | [x] | `/intentRecognition/list` | IntentRecognitionList | 意图识别 | 是 | `views/intentRecognition/List.vue` | `src/router/modules/intentRecognition.js` | - 46条意图词表/词表配置弹窗/填入示例/开始识别/批量识别/查询全通；双端 |
| 134 | [x] | `/kb/connectors` | KbConnectors | 知识库连接器 | 是 | `views/kb/Connectors.vue` | `src/router/modules/kbConnector.js` | - 连接器列表/新建弹窗(IMA/webhttp 类型字段)/同步失败给出可执行原因(HTML 清单非 JSON)全通；双端 |
| 135 | [x] | `/cap/matrix` | CapabilityMatrix | 能力矩阵 | 是 | `views/cap/Matrix.vue` | `src/router/modules/kbConnector.js` | - 32条能力矩阵/运行诊断跳转 /cap/diagnostics 命中；双端 |
| 136 | [x] | `/cap/diagnostics` | CapabilityDiagnostics | 能力运行诊断 | 是 | `views/cap/Diagnostics.vue` | `src/router/modules/kbConnector.js` | - 32条诊断表(就绪/通道/依赖)/刷新/重置全通；双端 |
| 137 | [x] | `/knowledge/management` | KnowledgeManagement | 知识库管理 | 是 | `views/KnowledgeWorkspace/KnowledgeManagement.vue` | `src/router/modules/knowledge.js` | - 43条知识库分页/搜索(标题)/详情(文档明细)/导入资料弹窗(文件+文本+URL)/重建索引与删除按钮在位全通；双端 |
| 138 | [x] | `/knowledge/batch-import` | KnowledgeBatchImport | 批量导入 | 是 | `views/KnowledgeWorkspace/BatchImport.vue` | `src/router/modules/knowledge.js` | - 载入模板/解析预览(2条CSV模板)/确认导入(计数)全通；双端 |
| 139 | [x] | `/knowledge/playground` | KnowledgePlayground | 检索 Playground | 是 | `views/KnowledgeWorkspace/Playground.vue` | `src/router/modules/knowledge.js` | - 产品选择(重名追加ID)/查询必填禁用/开始检索(5条命中+相关度+调试信息)/相关·一般·不相关反馈按钮全通；双端 |
| 140 | [x] | `/knowledge/connectors` | KnowledgeConnectors | 外部连接器 | 是 | `views/KnowledgeWorkspace/Connectors.vue` | `src/router/modules/knowledge.js` | - Notion/钉钉/自定义CRM 三连接器状态与凭据配置/测试连接(401明确)/一键拉取导入全通；双端 |
| 141 | [x] | `/knowledge/crawler-sources` | KnowledgeCrawlerSources | 网站爬取 | 是 | `views/KnowledgeWorkspace/CrawlerSources.vue` | `src/router/modules/knowledge.js` | - 5条爬取源(Sitemap/URL/已验证/异常/暂停恢复)/新建爬取源弹窗(知识库/类型/起始地址/白名单/页数/周期/QA拆分)/立即同步·验证站点·批次·删除按钮在位；双端 |
| 142 | [x] | `/knowledge/qa-candidates` | KnowledgeQACandidates | QA 审核队列 | 是 | `views/KnowledgeWorkspace/QACandidates.vue` | `src/router/modules/knowledge.js` | - 筛选(全部/网站拆分/客户反馈/手动)+刷新/1条候选(问题/答案/锚点/状态)全通；双端 |
| 143 | [x] | `/knowledge/feedbacks` | KnowledgeFeedbacks | 反馈管理 | 是 | `views/KnowledgeWorkspace/FeedbackList.vue` | `src/router/modules/knowledge.js` | - 8条反馈(星级/问题/答案/用户/会话/评论)搜索+刷新全通；双端 |
| 144 | [x] | `/knowledge/tokens` | KnowledgeTokens | API Token | 是 | `views/KnowledgeWorkspace/ApiToken.vue` | `src/router/modules/knowledge.js` | - 5条Token列表/按产品筛选/新建Token(名称必填+产品+读写权限+过期时间)/一次性明文弹窗/吊销(确认)全通；测试Token已吊销清理；双端 |
| 145 | [~] | `/knowledge/external` | KnowledgeExternal | 外部系统接入 | 是 | `views/KnowledgeWorkspace/ExternalImport.vue` | `src/router/modules/knowledge.js` | - 外部导入: 载入模板(JSON)/历史任务(EXT-* 2条)/刷新正常；仅载入模板未选关联产品时提交无反馈(需先选产品)，双端无报错 |
| 146 | [x] | `/knowledge/statistics` | KnowledgeStatistics | 知识库统计 | 是 | `views/KnowledgeWorkspace/KnowledgeStatistics.vue` | `src/router/modules/knowledge.js` | - 12条导入任务(批量/文本/API来源)/OpenAPI同步状态 2/2/0/刷新全通；双端 |
| 147 | [~] | `/knowledge/openapi` | KnowledgeOpenAPI | OpenAPI 集成 | 是 | `views/KnowledgeWorkspace/OpenAPIIntegration.vue` | `src/router/modules/knowledge.js` | - 2条OpenAPI数据源/新建数据源弹窗(名称/产品/URL/方法/认证/响应路径/映射/定时)全通；测试与立即同步指向 api.example.com 无响应，上游不可达属设计内；双端 |
| 148 | [~] | `/knowledge/rag-eval` | RagEvaluation | RAG 评测 | 是 | `views/knowledge/RagEvaluation.vue` | `src/router/modules/knowledge.js` | - 20条评估历史(失败态)/上传评估集/运行评估/对比当前在位；未执行真实评估(需评估集+LLM)，双端无报错 |
| 149 | [x] | `/knowledgeBase` | KnowledgeBase | 知识库管理 | - | `views/knowledgeBase/List.vue` | `src/router/modules/knowledgeBase.js` | - 知识库列表(2条)/搜索/重置/新建知识库弹窗(KB编码必填校验)/详情/编辑/禁用/删除按钮在位；双端 |
| 150 | [x] | `/knowledgeBase/list` | KnowledgeBaseList | 知识库列表 | - | `views/knowledgeBase/List.vue` | `src/router/modules/knowledgeBase.js` | - 同 #149 组件列表；双端 |
| 151 | [x] | `/knowledgeBase/create` | KnowledgeBaseCreate | 新建知识库 | - | `views/knowledgeBase/List.vue` | `src/router/modules/knowledgeBase.js` | - 新建知识库弹窗(KB编码/类型/名称/描述)空校验提示“请输入 KB 编码”；双端 |
| 152 | [x] | `/knowledgeBase/edit/:id` | KnowledgeBaseEdit | 编辑知识库 | - | `views/knowledgeBase/List.vue` | `src/router/modules/knowledgeBase.js` | - 按路由 :id 单条读取并打开编辑弹窗(id=12 预填 kb_code/名称)；非法 id 提示不存在的文案清晰；双端 |
| 153 | [x] | `/kuaishouCard` | KuaishouCard | 快手卡片 | - | `views/kuaishouCard/List.vue` | `src/router/modules/kuaishouCard.js` | - 20条快手卡片/搜索/重置/分页/添加卡片弹窗(标题/描述/图片/跳转/域名池/标签/激活)/编辑·统计·复制链接·删除在位；双端 |
| 154 | [x] | `/kuaishou/stats` | KuaishouStats | 快手卡片统计 | - | `views/kuaishouCard/Stats.vue` | `src/router/modules/kuaishouCard.js` | - 快手统计KPI/访问趋势/热门卡片/最近活动/刷新/单卡统计跳转全通；双端 |
| 155 | [x] | `/kuaishou-card-stats/:id` | KuaishouCardStats | 快手卡片详情统计 | - | `views/kuaishouCard/CardStats.vue` | `src/router/modules/kuaishouCard.js` | - 有效卡片 id=22 详情/浏览趋势/最近活动/刷新/返回全通；无效 id 显示“卡片已被删除”而非假零数据；双端 |
| 156 | [x] | `/lead-mining/index` | LeadMining | 线索发掘 | - | `views/leadMining/Index.vue` | `src/router/modules/leadMining.js` | - 线索挖掘渠道勾选/关键词/标签/意向阈值/模型/保存设置/重置/刷新全通；双端 |
| 157 | [x] | `/livecode` | LiveCodeManagement | 活码管理 | - | `views/livecode/LiveCodeManagement.vue` | `src/router/modules/livecode.js` | - 活码列表(空态)/新增活码/名称·状态筛选/查询/重置/分页正常 |
| 158 | [x] | `/reports/llm` | LlmUsageDashboard | LLM用量报表 | 是 | `views/reports/LlmUsageDashboard.vue` | `src/router/modules/llmReport.js` | - LLM 用量报表(6227字/20行 含上游 invalid_api_key 明细)/查询/分页正常；双端 |
| 159 | [x] | `/llmRouting/list` | LlmRoutingList | LLM路由 | 是 | `views/llmRouting/List.vue` | `src/router/modules/llmRouting.js` | - 104 条模型路由(供应商/模型/启用/配额/端点/能力)/新增模型·成本看板·测试·启用·编辑·删除在位 |
| 160 | [x] | `/llmRouting/cost` | LlmRoutingCostDashboard | 成本看板 | 是 | `views/llmRouting/CostDashboard.vue` | `src/router/modules/llmRouting.js` | - 成本看板(30天 ¥0.00/10.4万 Token/成功率100%/按场景·模型·智能体三维度)渲染正常 |
| 161 | [x] | `/marketingFlow/list` | MarketingFlowList | 营销自动化 | 是 | `views/marketingFlow/List.vue` | `src/router/modules/marketingFlow.js` | - 营销流程空态/新建流程入口正常 |
| 162 | [x] | `/mcp/credentials` | McpCredentials | MCP凭证 | 是 | `views/mcp/Credentials.vue` | `src/router/modules/mcpCredential.js` | - 4 条 MCP 凭证/签发新凭证/停用/删除在位 |
| 163 | [x] | `/messageHub/list` | MessageHubList | 消息中台 | - | `views/messageHub/List.vue` | `src/router/modules/messageHub.js` | - 579 条消息(平台/方向/类型/会话/AI/已读)分页/搜索/重置/详情/撤回/推送消息在位 |
| 164 | [x] | `/messageHub/dashboard` | MessageHubDashboard | 消息总览 | - | `views/messageHub/Dashboard.vue` | `src/router/modules/messageHub.js` | - 消息中台看板(近1h窗口/方向类型分布/DLQ 0条/批量重试)渲染正常，空态有解释 |
| 165 | [x] | `/module-market` | ModuleMarket | 功能模块市场 | 否 | `views/moduleMarket/Market.vue` | `src/router/modules/moduleMarket.js` | - 模块市场卡片(已开通/免费开通/依赖/页面数)/查询/刷新/我的模块·订单/查看详情正常 |
| 166 | [x] | `/module-market/detail/:key` | ModuleMarketDetail | 模块详情 | 否 | `views/moduleMarket/Detail.vue` | `src/router/modules/moduleMarket.js` | - 模块详情(core): 含页面与依赖说明、已开通/订阅按钮与“未连接平台”提示正常 |
| 167 | [x] | `/module-market/my` | ModuleMy | 我的模块 | 是 | `views/moduleMarket/MyModules.vue` | `src/router/modules/moduleMarket.js` | - 我的模块 44 行(同步状态/耗时)/立即同步/去市场/我的订单/详情正常 |
| 168 | [x] | `/module-market/orders` | ModuleOrders | 我的订单 | 是 | `views/moduleMarket/Orders.vue` | `src/router/modules/moduleMarket.js` | - 32 条订单(模块/金额/状态/付款指引)/刷新/分页正常 |
| 169 | [x] | `/subscription-wall` | SubscriptionWall | 订阅续费 | 是 | `views/subscription/Wall.vue` | `src/router/modules/moduleMarket.js` | - 订阅墙: 当前体验期/截止时间/价格 ¥9/月/镜像同步提示/返回工作台(公共独立页)正常 |
| 170 | [x] | `/register` | Register | 商户注册 | 否 | `views/register/Register.vue` | `src/router/modules/moduleMarket.js` | - 商户注册 3 步(账户信息/开户登记/确认告知)/空提交 4 项必填校验/去登录正常 |
| 171 | [x] | `/objection/list` | ObjectionList | 异议处理 | 是 | `views/objection/List.vue` | `src/router/modules/objection.js` | - 异议处理: 智能处理/刷新分类/智能匹配/仅分类/清空 + 会话使用记录空态正常 |
| 172 | [x] | `/observability-dashboard` | ObservabilityDashboard | AI 观测与渠道健康 | - | `views/observabilityDashboard/Index.vue` | `src/router/modules/observabilityDashboard.js` | - 可观测看板: 25 行模型调用(Tokens/成本/均延迟 1037ms)渲染正常 |
| 173 | [x] | `/oneid/list` | OneIDList | OneID 列表 | 是 | `views/oneid/List.vue` | `src/router/modules/oneid.js` | - 132 条 OneID 身份/搜索/解析·创建 OneID/查看身份/链接身份/分页正常 |
| 174 | [x] | `/oneid/conflicts` | OneIDConflicts | 身份冲突解决 | 是 | `views/oneid/Conflicts.vue` | `src/router/modules/oneid.js` | - 身份冲突空态+刷新正常 |
| 175 | [x] | `/oneid/merge-rules` | OneIDMergeRules | 合并规则配置 | 是 | `views/oneid/MergeRuleConfig.vue` | `src/router/modules/oneid.js` | - 6 条合并规则(确定性/概率/相似度阈值)/新增·编辑·删除/命中预览空态正常 |
| 176 | [x] | `/operationLog/list` | OperationLogList | 操作日志 | 是 | `views/operationLog/List.vue` | `src/router/modules/operationLog.js` | - 72079 条操作日志/增强视图/导出/刷新/详情/分页正常 |
| 177 | [x] | `/operationLog/enhanced` | OperationLogEnhanced | 操作日志(增强) | 是 | `views/operationLog/Enhanced.vue` | `src/router/modules/operationLog.js` | - 增强视图 20 行(模块/动作/回滚/详情)/导出/刷新/查询正常 |
| 178 | [x] | `/ops-overview` | OpsOverview | 运维总览 | - | `views/OpsOverview/Index.vue` | `src/router/modules/ops.js` | - 运维总览 18 行(账号/状态/卡死/失败/最近活跃)渲染正常 |
| 179 | [x] | `/sales-cockpit` | SalesCockpitIndex | AI 销冠驾驶舱 | - | `views/SalesCockpit/Index.vue` | `src/router/modules/ops.js` | - 销售驾驶舱 14 行(会话链路/各环节计数)渲染正常 |
| 180 | [~] | `/dashboard/drafts` | OrderDraftList | 订单草稿 | 是 | `views/orderDraft/List.vue` | `src/router/modules/orderDraft.js` | - 草稿池空态+归属筛选+刷新；“本进程未装配草稿运行时⇒计数未读取”显式区分非0(设计内) |
| 181 | [~] | `/dashboard/drafts/:id` | OrderDraftDetail | 草稿详情 | 是 | `views/orderDraft/Detail.vue` | `src/router/modules/orderDraft.js` | - 草稿详情页命中 DRAFT_DB=off 的装配提示，文案明确“不是草稿没了、重试无用、需重启”(设计内) |
| 182 | [x] | `/system/permissions` | PermissionPanel | permission.title | 是 | `views/system/PermissionPanel.vue` | `src/router/modules/permission.js` | - 8 条权限审计(账号/权限/时间)/启用账号/重置密码/刷新正常 |
| 183 | [x] | `/persona/list` | PersonaList | 销冠画像 | 是 | `views/persona/List.vue` | `src/router/modules/persona.js` | - 10 条销冠画像指标(满意度/跟进及时性/稳定·上升)/刷新/对比模式/重新生成正常 |
| 184 | [x] | `/platformAccount/list` | PlatformAccountList | 平台账号 | 是 | `views/platformAccount/List.vue` | `src/router/modules/platformAccount.js` | - 30 条平台账号(平台/状态/最近错误)/支持平台/新增账号/搜索/重置/登录·检查·删除正常 |
| 185 | [x] | `/qq` | QQBot | QQ 机器人 | - | `views/qq/account.vue` | `src/router/modules/qq.js` | - QQ 机器人空态+添加机器人/刷新正常 |
| 186 | [x] | `/qq/account` | QQAccount | 机器人账号 | - | `views/qq/account.vue` | `src/router/modules/qq.js` | - 同 #185 组件；双端正常 |
| 187 | [x] | `/system/rag-product-config` | RagProductConfigIndex | RAG 主配置 | 是 | `views/RagProductConfig/index.vue` | `src/router/modules/ragProductConfig.js` | - 14 条 RAG 产品配置(模型/阈值/配额)/新增产品/编辑/停用/删除/测试正常 |
| 188 | [x] | `/system/rag-account` | RagProductAccountConfig | RAG 账号配置 | 是 | `views/RagProductConfig/AccountConfig.vue` | `src/router/modules/ragProductConfig.js` | - RAG 账号配置表单(平台/账号ID/关联产品/每日上限/功能开关/回复规则)/添加规则/保存/重置正常 |
| 189 | [x] | `/system/rag-product` | RagProductManagement | RAG 产品管理 | 是 | `views/RagProductConfig/RagProductManagement.vue` | `src/router/modules/ragProductConfig.js` | - 同 #187 产品列表；双端正常 |
| 190 | [x] | `/reachPipeline/list` | ReachPipelineList | 触达Pipeline | - | `views/reachPipeline/List.vue` | `src/router/modules/reachPipeline.js` | - 3 条 Pipeline(渠道/状态/限流)/重置限流/可视化编辑器/新建/查看·暂停·归档·删除正常 |
| 191 | [x] | `/reachPipeline/editor/:id?` | ReachPipelineEditor | Pipeline 编辑 | 是 | `views/reachPipeline/Editor.vue` | `src/router/modules/reachPipeline.js` | - 可视化编辑器(节点库:触发器/渠道/延时/条件/A-B/AI生成/结束 + 校验·保存)；id=20 正常加载，非法 id 提示不存在 |
| 192 | [x] | `/reachPipeline/deferred-outbound` | ReachDeferredOutbound | 延迟出站管理 | 是 | `views/reachPipeline/DeferredOutbound.vue` | `src/router/modules/reachPipeline.js` | - 延迟出站空态+状态·渠道筛选+重置正常 |
| 193 | [x] | `/system/roles` | RoleList | role.title | 是 | `views/system/RoleList.vue` | `src/router/modules/role.js` | - 角色卡(超级管理员/客服/员工, 成员数/查看成员/分配菜单权限)正常 |
| 194 | [x] | `/sales-workbench` | SalesWorkbenchIndex | 销售工作台 | 是 | `views/salesWorkbench/Index.vue` | `src/router/modules/salesWorkbench.js` | - 销售工作台(新建订单/AI接管/今日跟进 + AI成单/应答率/转化/Token 指标)正常 |
| 195 | [x] | `/scriptTemplate/list` | ScriptTemplateList | 话术库 | 是 | `views/scriptTemplate/List.vue` | `src/router/modules/scriptTemplate.js` | - 10 条话术模板(使用/编辑/复制/删除)/新增话术正常 |
| 196 | [x] | `/scriptTemplate/ab-stats` | ScriptTemplateAbStats | 话术AB测试 | 是 | `views/scriptTemplate/AbStats.vue` | `src/router/modules/scriptTemplate.js` | - 话术 AB 统计: 选择话术后展示版本/曝光/转化(未选时提示“请先选择一个话术”)正常 |
| 197 | [x] | `/securityAudit/list` | SecurityAuditList | 安全审计 | 是 | `views/securityAudit/List.vue` | `src/router/modules/securityAudit.js` | - 70 条安全审计(风险分/等级/详情)/刷新/立即审计/查询/分页正常 |
| 198 | [x] | `/shortLink` | ShortLink | 短链管理 | - | `views/shortLink/List.vue` | `src/router/modules/shortLink.js` | - 176 条短链(短码/目标/访问/状态/过期)/添加·搜索·重置·编辑·统计·分享·删除/分页正常 |
| 199 | [x] | `/shortLink/stats` | ShortLinkStats | 短链统计 | - | `views/shortLink/Stats.vue` | `src/router/modules/shortLink.js` | - 短链统计(总体统计/搜索/重置/单条统计/分享)正常 |
| 200 | [x] | `/sms/list` | SmsList | 短信列表 | - | `views/sms/List.vue` | `src/router/modules/sms.js` | - 43 条短信(手机号/内容/状态/查看/重发)/发送短信/搜索/重置正常 |
| 201 | [x] | `/sms/drafts` | SmsDrafts | 短信草稿 | - | `views/sms/Drafts.vue` | `src/router/modules/sms.js` | - 19 条短信草稿(编辑/发送/删除)/新建草稿/分页正常 |
| 202 | [x] | `/sms/jobs` | SmsJobs | 短信任务 | - | `views/sms/Jobs.vue` | `src/router/modules/sms.js` | - 短信任务 1 条(计划/已发/完成状态)/创建任务/查看/删除正常 |
| 203 | [x] | `/sms/config` | SmsConfig | 短信配置 | - | `views/sms/Config.vue` | `src/router/modules/sms.js` | - 短信配置(阿里云/腾讯云/华为云 三套密钥与签名)/保存配置正常 |
| 204 | [x] | `/sopAgent/list` | SopAgentList | 销冠 SOP 智能体 | 是 | `views/sopAgent/List.vue` | `src/router/modules/sopAgent.js` | - 5 条 SOP(已激活/详情/编辑/停用/删除)/刷新/查询/创建 SOP 正常 |
| 205 | [x] | `/sop-template` | SOPTemplate | SOP 模板库 | - | `views/sopTemplate/Index.vue` | `src/router/modules/sopTemplateKb.js` | - SOP 落地页(进入/打开/去绑定) 正常 |
| 206 | [x] | `/sop-template/list` | SOPTemplateList | SOP 模板列表 | - | `views/sopTemplate/List.vue` | `src/router/modules/sopTemplateKb.js` | - 368 条 SOP 模板(意图/阶段/置信度/命中)/刷新/新建/搜索/重置/分页/编辑/删除正常 |
| 207 | [x] | `/sop-template/market` | SOPTemplateMarket | SOP 模板市场 | - | `views/sopTemplate/Market.vue` | `src/router/modules/sopTemplateKb.js` | - 模板市场(搜索/重置/一键导入/详情)正常 |
| 208 | [x] | `/sop-template/editor` | SOPTemplateEditor | SOP 模板编辑 | - | `views/sopTemplate/Editor.vue` | `src/router/modules/sopTemplateKb.js` | - SOP 编辑器(新建): 模板/变量/匹配参数/置信度/启用 + 取消·创建模板正常 |
| 209 | [x] | `/sop-template/editor/:id` | SOPTemplateEditorEdit | SOP 模板编辑 | - | `views/sopTemplate/Editor.vue` | `src/router/modules/sopTemplateKb.js` | - 编辑器编辑态: id=4340 预填并显示“保存更新”；非法 id 提示模板不存在 |
| 210 | [x] | `/system/config` | SystemConfig | 站点设置 | 是 | `views/system/Config.vue` | `src/router/modules/system.js` | - 站点配置(站点/备案/客服/功能开关/限制/上传大小)/保存·清空正常 |
| 211 | [x] | `/system/obs-config` | SystemObsConfig | 存储配置 | 是 | `views/system/ObsConfig.vue` | `src/router/modules/system.js` | - 3 条存储配置(本地/S3等)/新增/测试/编辑/设为默认/删除正常 |
| 212 | [x] | `/system/material-library` | SystemMaterialLibrary | 素材库 | 是 | `views/system/MaterialLibrary.vue` | `src/router/modules/system.js` | - 素材库空态(上传素材/新建分类/分类·类型·关键词筛选/分页)正常 |
| 213 | [x] | `/system/monitor` | SystemMonitor | 监控 | 是 | `views/system/Monitor.vue` | `src/router/modules/system.js` | - 监控(运行时间/服务器时间/主机名/Go版本)/刷新正常 |
| 214 | [x] | `/system/trace` | TraceMonitor | 链路追踪 | 是 | `views/system/TraceMonitor.vue` | `src/router/modules/system.js` | - 链路追踪(Trace/会话/消息/渠道 查询/重置 + 手动触发评估)正常 |
| 215 | [x] | `/system/guide` | SystemGuide | 使用引导 | 是 | `views/system/Guide.vue` | `src/router/modules/system.js` | - 系统使用引导 5 步 + 前往操作正常 |
| 216 | [x] | `/system/rag-overview` | RagOverview | RAG概览 | 是 | `views/system/RagOverview.vue` | `src/router/modules/system.js` | - RAG 概览 5 行同步记录(产品/system/成功时间)正常 |
| 217 | [x] | `/system/automation-hub` | AutomationHub | 自动化中心 | 是 | `views/system/AutomationHub.vue` | `src/router/modules/system.js` | - 自动化中心(时段/策略/宏/邮箱频率 + AI代理绩效7/30天)正常 |
| 218 | [x] | `/system/config-params` | ConfigParams | 动态阈值参数 | 是 | `views/system/ConfigParams.vue` | `src/router/modules/system.js` | - 19 条动态阈值参数(键/值/范围/编辑/重置/审计日志)正常 |
| 219 | [x] | `/system/dictionaries` | SystemDictionaries | 业务字典 | 是 | `views/system/DictionaryManager.vue` | `src/router/modules/system.js` | - 2 条业务字典 + 状态跃迁(纯枚举域提示)正常 |
| 220 | [x] | `/system/feature-flags` | SystemFeatureFlags | 功能开关 | 是 | `views/system/FeatureFlags.vue` | `src/router/modules/system.js` | - 6 条功能开关(开/疑似闲置/全量/从未)/刷新/新建/编辑/日志/删除正常 |
| 221 | [x] | `/system/ltc-config` | LTCConfig | LTC 运营开关 | 是 | `views/system/LTCConfig.vue` | `src/router/modules/system.js` | - LTC 运营开关策略表单/KV键·上限·TTL 说明/保存整份策略/重新读取正常 |
| 222 | [x] | `/system/users` | SystemUserList | systemUser.title | 是 | `views/system/UserList.vue` | `src/router/modules/systemUser.js` | - 57 条系统用户(角色/状态)/新建·编辑·重置密码·删除/分页正常 |
| 223 | [x] | `/tagSegmentation/list` | TagSegmentationList | 标签分层 | 是 | `views/tagSegmentation/List.vue` | `src/router/modules/tagSegmentation.js` | - 31 条标签(手动/默认/启用禁用)/新增标签/刷新/编辑/删除正常 |
| 224 | [x] | `/telegram` | Telegram | TG 机器人 | - | `views/telegram/account.vue` | `src/router/modules/telegram.js` | - 4 条 Telegram 机器人(状态/最近错误 connection refused)/添加/刷新/编辑/注册Webhook/测试发送/绑定AI/删除正常 |
| 225 | [x] | `/telegram/account` | TelegramAccount | 机器人账号 | - | `views/telegram/account.vue` | `src/router/modules/telegram.js` | - 同 #224 组件；双端正常 |
| 226 | [x] | `/telegram/gate` | TelegramGate | 入群管控 | - | `views/telegram/gate.vue` | `src/router/modules/telegram.js` | - 群组管控 2 条(群名/ID/禁言解锁策略/成员台账)/添加/刷新/编辑/删除正常 |
| 227 | [x] | `/template-market` | TemplateMarket | 模板/技能市场 | 否 | `views/templateMarket/Market.vue` | `src/router/modules/templateMarket.js` | - 模板市场(发布/审核/刷新 + 内置可用/即将上线/查看详情)正常 |
| 228 | [x] | `/template-market/my` | TemplateMarketMy | 我的发布/订单 | 是 | `views/templateMarket/MyWorkspace.vue` | `src/router/modules/templateMarket.js` | - 我的发布/订单空态 + 我要发布正常 |
| 229 | [x] | `/template-market/review` | TemplateMarketReview | 发布审核 | 是 | `views/templateMarket/ReviewConsole.vue` | `src/router/modules/templateMarket.js` | - 发布审核 5 条(订单/金额/状态/下架)正常 |
| 230 | [x] | `/tiktok/list` | TikTokList | TikTok卡片 | 是 | `views/tiktokCard/List.vue` | `src/router/modules/tiktok.js` | 11-09双端复验：卡片列表/操作，0报错 |
| 231 | [x] | `/tiktok/stats` | TikTokStats | TikTok统计 | 是 | `views/tiktokCard/Stats.vue` | `src/router/modules/tiktok.js` | 11-09双端复验：统计 KPI/热门卡片，0报错 |
| 232 | [~] | `/tiktok-card-stats/:id` | TikTokCardStats | TikTok卡片统计 | 是 | `views/tiktokCard/CardStats.vue` | `src/router/modules/tiktok.js` | 11-09双端复验：上轮 id=16 数据已清→404 数据态优雅处理，0报错 |
| 233 | [x] | `/tiktok` | TikTok | TikTok | - | `-` | `src/router/modules/tiktok.js` | 11-09双端：重定向列表，0报错 |
| 234 | [x] | `/confidence/panel` | ConfidencePanel | 置信度运营 | 是 | `views/confidence/Panel.vue` | `src/router/modules/tuning.js` | 11-09双端复验：策略看板/阈值，0报错 |
| 235 | [x] | `/humanize/panel` | HumanizePanel | 拟人度评估 | 是 | `views/humanize/Panel.vue` | `src/router/modules/tuning.js` | 11-09双端复验：拟人度看板，0报错 |
| 236 | [x] | `/feedbackLoop/panel` | FeedbackLoopPanel | 反馈学习闭环 | 是 | `views/feedbackLoop/Panel.vue` | `src/router/modules/tuning.js` | 11-09双端复验：反馈闭环列表，0报错 |
| 237 | [x] | `/unifiedMessage/list` | UnifiedMessageList | 统一消息 | - | `views/unifiedMessage/List.vue` | `src/router/modules/unifiedMessage.js` | 11-09双端复验：消息列表/重发，0报错 |
| 238 | [x] | `/unknown-questions` | UnknownQuestionList | 未知问题运营 | 是 | `views/unknownQuestion/List.vue` | `src/router/modules/unknownQuestion.js` | 11-09双端复验：未知问题列表，0报错 |
| 239 | [x] | `/userSegment/list` | UserSegmentList | 用户分层 RFM | 是 | `views/userSegment/List.vue` | `src/router/modules/userSegment.js` | 11-09双端复验：分群列表/创建，0报错 |
| 240 | [x] | `/userSegment/rfm-matrix` | RfmMatrix | RFM 矩阵 | 是 | `views/userSegment/RfmMatrix.vue` | `src/router/modules/userSegment.js` | 11-09双端复验：矩阵/导出触达，0报错 |
| 241 | [x] | `/userSegment/builder` | UserSegmentBuilder | 分层规则构建 | 是 | `views/userSegment/Builder.vue` | `src/router/modules/userSegment.js` | 11-09双端复验：规则构建器，0报错 |
| 242 | [x] | `/wecomAccount/list` | WecomAccountList | 企微账号管理 | - | `views/wecomAccount/List.vue` | `src/router/modules/wecomAccount.js` | 11-09双端复验：账号列表/操作，0报错 |
| 243 | [x] | `/wecomAccount/data` | WecomAccountData | 企微数据看板 | - | `views/wecomAccount/Data.vue` | `src/router/modules/wecomAccount.js` | 11-09双端复验：4 tab 数据看板，0报错 |
| 244 | [x] | `/whatsapp` | Whatsapp | WhatsApp 社群 | - | `views/whatsapp/WhatsappAccount.vue` | `src/router/modules/whatsapp.js` | 11-09双端复验：账号列表，0报错 |
| 245 | [x] | `/whatsapp/account` | WhatsappAccount | 账号管理 | - | `views/whatsapp/WhatsappAccount.vue` | `src/router/modules/whatsapp.js` | 11-09双端复验：同组件（A-2 修复保持），0报错 |
| 246 | [x] | `/whatsapp/drafts` | WhatsappDrafts | 草稿箱 | - | `views/whatsapp/WhatsappDrafts.vue` | `src/router/modules/whatsapp.js` | 11-09双端复验：草稿列表，0报错 |
| 247 | [x] | `/whatsapp/jobs` | WhatsappJobs | 群发 | - | `views/whatsapp/WhatsappJobs.vue` | `src/router/modules/whatsapp.js` | 11-09双端复验：群发任务，0报错 |
| 248 | [x] | `/whatsapp/lead-group-selection` | LeadGroupSelection | 从线索库选择群体 | 是 | `views/whatsappBot/LeadGroupSelection.vue` | `src/router/modules/whatsapp.js` | 11-09双端复验：分组选择，0报错 |
| 249 | [x] | `/whatsapp/group-messaging` | BulkMessaging | 批量消息发送 | 是 | `views/whatsappBot/BulkMessaging.vue` | `src/router/modules/whatsapp.js` | 11-09双端复验：群发表单，0报错 |
| 250 | [x] | `/whatsapp/bulk-matrix` | WhatsappBulkMatrix | 群发矩阵 | 是 | `views/whatsapp/BulkMatrix.vue` | `src/router/modules/whatsapp.js` | 11-09双端复验：矩阵配置，0报错 |
| 251 | [x] | `/whatsapp-cloud` | WhatsappCloud | WhatsApp Cloud | - | `views/whatsappCloud/WhatsappCloudAccount.vue` | `src/router/modules/whatsappCloud.js` | 11-09双端复验：账号空态+新增（A-2 三键合并修复保持），0报错 |
| 252 | [x] | `/workflow-orchestrator/list` | WorkflowOrchestratorList | 工作流编排 | 是 | `views/workflowOrchestrator/List.vue` | `src/router/modules/workflowOrchestrator.js` | 11-09双端复验：工作流列表，0报错 |
| 253 | [x] | `/workflow-orchestrator/editor/:workflow_id` | WorkflowOrchestratorEditor | 工作流编辑 | 是 | `views/workflowOrchestrator/Editor.vue` | `src/router/modules/workflowOrchestrator.js` | 11-09双端复验：画布/节点库/保存入口，0报错 |
| 254 | [~] | `/workflow-orchestrator/execution/:id` | WorkflowOrchestratorExecution | 执行详情 | 是 | `views/workflowOrchestrator/Execution.vue` | `src/router/modules/workflowOrchestrator.js` | 11-09双端复验：无效 id 404 数据态优雅处理，0报错 |
| 255 | [x] | `/xianyuCard` | XianyuCard | 闲鱼卡片 | - | `views/xianyuCard/List.vue` | `src/router/modules/xianyuCard.js` | 11-09双端复验：卡片列表/操作，0报错 |
| 256 | [x] | `/xianyu/stats` | XianyuStats | 闲鱼卡片统计 | - | `views/xianyuCard/Stats.vue` | `src/router/modules/xianyuCard.js` | 11-09双端复验：统计 KPI/趋势，0报错 |
| 257 | [~] | `/xianyu-card-stats/:id` | XianyuCardStats | 闲鱼卡片详情统计 | - | `views/xianyuCard/CardStats.vue` | `src/router/modules/xianyuCard.js` | 11-09双端复验：上轮 id=29 已清→404「记录不存在」优雅，0报错 |
| 258 | [x] | `/xiaohongshuCard` | XiaohongshuCard | 小红书卡片 | - | `views/xiaohongshuCard/List.vue` | `src/router/modules/xiaohongshuCard.js` | 11-09双端复验：卡片列表，0报错 |
| 259 | [x] | `/xiaohongshu/stats` | XiaohongshuStats | 小红书卡片统计 | - | `views/xiaohongshuCard/Stats.vue` | `src/router/modules/xiaohongshuCard.js` | 11-09双端复验：统计渲染，0报错 |
| 260 | [~] | `/xiaohongshu-card-stats/:id` | XiaohongshuCardStats | 小红书卡片详情统计 | - | `views/xiaohongshuCard/CardStats.vue` | `src/router/modules/xiaohongshuCard.js` | 11-09双端复验：数据已清→404「记录不存在」优雅，0报错 |

## 进度（2026-11-09/10 全量回归轮：双形态逐路由复验完成）

- 本轮复验通过 `[x]`：**242** 条
- 本轮复验·设计内拒绝/数据态 `[~]`：**18** 条
- 本轮点出缺陷并当轮修复：**4** 个（H-1 4301 轮询逃逸 / H-2 AppKeyResolve 路径参数 / H-3 VISITOR_TOKEN_SECRET 沉默失败 / H-4 LLM 报表与审计 schema 漂移双 500），详见未完成任务清单.md H 区
- 待点验 `[ ]`：**0** 条；缺陷未闭环 `[!]`：**0** 条
