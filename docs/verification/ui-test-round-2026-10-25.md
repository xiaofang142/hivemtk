# UI 拟人点击测试 · 2026-10-25 回合

> 范围：`user-web` 全部页面路由（`router-list.md`，244 条，86 个模块文件，含 2026-10-23 之后新增模块）。
> 两种部署形态都要过：私域独立部署（`TENANT_MODE=off`）与 SaaS 多租户（`TENANT_MODE=on`）。
> 进度以 `router-list.md` 的状态列为准；本文件记录过程、判据与发现。

## 1. 本轮环境（实测）

| 形态 | 后端 | 前端 | 站点标识 |
|---|---|---|---|
| 私域独立部署 | `127.0.0.1:8314`（`TENANT_MODE=off`） | `127.0.0.1:8320` | `tenant_mode=false` / `edition=opensource` |
| SaaS 多租户 | `127.0.0.1:8315`（`TENANT_MODE=on`） | `127.0.0.1:8321` | `tenant_mode=true` / `edition=saas` |

- 后端：当前 HEAD 源码 `CGO_ENABLED=0 go build ./cmd/api`（`/tmp/ui25/api`），DB `127.0.0.1:8202/user_db`，Redis `8203`。
- 前端：`npx vite --port 8320/8321`，`USER_SERVER_PORT` 指到对应后端。
- 浏览器：Qoder 内置 Chrome DevTools MCP（独立 profile，两页签分别对应两种形态）。
- 登录：`uit_admin` / `Hive@Test123`（id=26，tenant_id=1，admin）。
- 端口刻意避开并行泳道占用的 8204 / 8304 / 8214。

## 2. 判据

1. 打开页面 → 等主区渲染 → 关闭新手引导/浮层。
2. 逐个点击主要元素：标签页、筛选下拉、搜索/重置、详情、新增/编辑（弹窗后取消）。
3. 记录：控制台 error/warn、Network 中 `xhr/fetch` 的非 2xx、页面可见的失败文案。
4. 同一路由两种形态各走一遍；形态差异页（租户/商务属性）必须在独立部署下不出现且不报错。
5. 唯一豁免的既有噪声：`[web-vitals][debug]`、`sw.js` 在 dev 下的注册告警、Element Plus 弃用告警。

## 3. 已确认的缺陷 / 待判项

| # | 路由 | 形态 | 现象 | 判定 | 处置 |
|---|---|---|---|---|---|
| F1 | `/messageHub/list` | 两形态 | 新手引导第二步文案写「改完条件记得点**查询**生效」，页面实际按钮是「**搜索**」 | 文案与真实按钮不一致（用户照着找不到按钮） | 待修 |
| F2 | `/chat/embed/:channel_ref` | 两形态 | 选择**未绑定任何智能体**的渠道（如 `ak_yE5wgTSyTPRutWJhR3JVEv2F`）后发消息：接口 200 且 `ai_replied=false`、`transferred=false`，访客端只看到自己那条消息，既无 AI 回复也无转人工提示；同一实例的默认挂件（`web_embed/default` 绑定 agent 53）能正常回复 | 未绑定渠道时「消息进黑洞」；且 `chat_channels.default_rag_product_id` 全仓**无消费点**（仅存取），配置了也不生效 | 待评估（产品/数据侧）；先记 |
| F3 | `/chat/embed/default` | 两形态 | AI 回复正文含模板占位符：`这款核心卖点：①[核心功能A] ②[核心功能B] ③[独特优势]` | 种子 FAQ 数据未替换占位符，直接展示给终端访客 | 待评估（数据）；先记 |
| F4 | `profile` | 私域独立部署 | 角色文案固定显示「商户管理员」（`Profile.vue:336` 硬编码 `admin → 商户管理员`） | 独立部署没有「商户」概念，SaaS 专属措辞泄漏到私域形态 | 待修（按 `tenant_mode` 分形态） |
| F5 | 全站（登录态） | 两形态 | 长时间停留 + 访客挂件收发消息后，约 16 分钟时出现 401 爆发（`notifications`/`current-user`/`refresh-token` 全 401），`localStorage.token` 被清、跳登录 | 并发续期竞态：请求拦截器与 chatSocket 各自发 `/api/auth/refresh-token`，后一次用已拉黑旧令牌必 401，且失败分支**无条件** `clearAuthAndGoLogin` 会把另一次刚换到的新令牌一起抹掉 | **已修**：抽出 `utils/tokenRefresh.js` 单飞续期 + 401 分档（仅「401 且仍是同一把令牌」才清会话）+ 不覆盖并发新令牌；chatSocket 复用同一把锁（1006 不再无条件轮换）；7 枚单测 + 浏览器并发 `force` 双调用实测（一次轮换、双方同值、token/user_info 均在、无跳转） |
| F6 | `/aiAgent/tools` | 两形态 | 页头写死「管理 **40** 个 AI 工具」，实际列表接口 `/api/ai-tools` 只回 1 条，统计卡「工具总数 1」 | 文案常量与数据源脱节（`ToolList.vue:11` 硬编码 40） | 待修（改为动态或对齐口径） |
| F7 | `/aiProductivity/list` | **仅 SaaS** | 同一 DB、同一 JWT（tenant_id=1），独立部署返回 `total_conversations=95/ai_replies=240`，SaaS 返回 **0/0**；接口均 200 | `ops/service/ai_productivity.go:46` 用 `context.Background()` 建报告，SaaS 租户插件按 ctx 注入 `tenant_id`，无租户上下文 ⇒ 查不到任何行；`conversion_funnel.go:72/185` 同族 | 待修（透传 request ctx，并核 conversion funnel） |

## 4. 设计内 / 环境限制（不计缺陷）

| 现象 | 判定 |
|---|---|
| 已初始化实例访问 `/setup`：`GET init-status` 后 `POST /api/system/init-complete` → **403**，随后 `ElMessage.info` + 跳登录 | 设计内：`SetupOnlyGuard` 对已初始化实例恒 403，前端在 `initHelper.js` 明确把 403 视为成功（`_silent`） |
| `/chat/embed/*` 响应头 `x-origin-not-allowed: <origin>`，但 `access-control-allow-origin` 正常回显 | 设计内：`app_key_auth.go` 注释「Origin 白名单不匹配仅记录（X-Origin-Not-Allowed），不阻断访问」 |
| 本机未起 LLM（8207/8208），实质性问题常由 FAQ/RAG 兜底或 60s 超时 | 环境限制；两种形态表现一致 |

## 5. 逐页记录

见 `router-list.md` 状态列；异常页面的细节追加到本文件第 6 节。

## 6. 明细（按路由）

（待补）

## 7. 本轮累计缺陷清单（滚动，含已修）

| # | 路由/接口 | 现象 | 根因 | 状态 |
|---|---|---|---|---|
| F1 | `/messageHub/list`、`/abExperiment/list` 导览文案 | 文案让点「查询」，页面按钮是「搜索」（en 亦引号错名） | 导览 i18n 与按钮实际文案脱节 | 已修 zh/en |
| F4 | `profile`（私域） | 角色显示「商户管理员」 | `Profile.vue` 硬编码；私域无商户概念 | 已修（按 tenantMode 分流） |
| F6 | `/aiAgent/tools` | 页头写死「管理 40 个」，实际 1 条 | 文案常量与数据源脱节 | 已修（动态 stats.total） |
| F7 | `/aiProductivity/list`（SaaS） | 同库同租户 off 95 会话/240 AI 回复，on 全 0 | `BuildReport` 用 `context.Background()`，SaaS 租户插件无 ctx 注入 | 已修（透传 request ctx），待重建复测 |
| F8 | `/asset-market/detail/:id` | 不存在的资产返回 200+空对象，页面渲染「?」+免费试用按钮 | 详情页无空/不存在态 | 已修（复核 `Detail.vue:11` 空态 + `fetchDetail` 无资产即置 null） |
| F9 | `/backup/enhanced` | 122 条已完成备份，统计卡「最近成功 -」「总大小 0 B」 | `LastSuccess` 只查 `success`（库里是 `completed`）；总大小取单条而非合计 | 已修（含 SumCompletedSize），待复测 |
| F10 | `/batchOperation/list` | 页面只有操作历史，4 个工具卡全不渲染 | `/api/batch/tools` 返回 `{list,total}`，前端按数组处理；空数组兜底失效 | 已修（映射 list + label），已复测 |
| F11 | `/browser-automation/tasks/create` | 空提交弹原始后端报错「参数不合法：name 为必填项；url 为必填项」，且重复两条 | 前端未做必填校验，直接打后端 | 已修（复核 `Editor.vue:663-667` 前置校验名称/URL，不打后端） |
| F12 | `/conversionFunnel/list`（SaaS） | 五阶段全 0，横幅「部分阶段取数失败: visit/clue/intent/session/opportunity」 | 同 F7 家族（ctx）；并行泳道已加 `BuildFunnelCtx/GetStageDetailsCtx` 并切 controller | 已修（他泳道，待重建复测） |
| F13 | `/conversionFunnel/list`（私域） | 线索 61→意向 1→会话 95，阶段转化率 9500%，总进入量 0 | 五阶段取数口径不单调（访问/会话来自不同表/窗口） | 2026-10-27 复现：off/on 同为 visit 6→clue 61→intent 1→session 107，conversion 1783%。服务层已把 rate>100 的 drop_rate 置 0 并在测试里钉住「口径无单调约定」，**是否改指标属产品口径**（建议页面把列名改成「相邻阶段比值」并加口径说明） |
| F14 | `/customerJourney/dashboard` | 客户总数 0（库内 169 customers） | `GetOverview` / `Funnel` 只读进程内存 `states`（每实例 60s 读缓存），Redis 里明明有权威态 | **已修**：新增 `forEachLiveState`（Redis 阶段索引入口 + 逐成员权威态回读，同一客户只算一次；无缓存时退回 L1 快照），总览与漏斗同口径；顺带修零值 `StageSince` 把平均停留算成天文数字。单测 `TestCustomerJourney_GetOverviewSeesWritesFromAnotherInstance` / `...CountsCustomerOnceAfterStageMove` / `...IgnoresZeroStageSince` / `...FunnelSeesWritesFromAnotherInstance` |
| F15 | `/email/smtp` | 27 条 SMTP 账号全为空字段 | `CreateEmailSmtpRequest` 六个字段带 `binding:"required"`，新建不会再有空记录；历史 27 行是校验上线前的测试数据 | 代码已闭环（复核 dto）；历史空行属数据清理，未动库 |
| F16 | `/geo-tools/keyword-mining` | 私域种子/关联/下拉/长尾全 0，SaaS 有 7530/420/1170/26198 | 私域形态下 GEO 取数上下文/模块装配不明确 | 2026-10-27 复核：同一接口两形态均有数据（off 29113/620/21912/522607，on 7531/620/1208/26221）。off 数值更大属单机形态不做租户过滤（预期），原「私域全 0」读数不可复现 |
| F17 | `/knowledge/crawler-sources` | 「知识库」列显示数字 ID（12、3） | 列表未回填知识库名称 | 已修（复核 `CrawlerSources.vue:71` 走 `kbName(row.kb_id)`） |
| F18 | `/knowledge/qa-candidates` | 创建时间显示 `1/1/1 08:05:43` | 零值时间未判空 | 已修（复核 `QACandidates.vue:200-206` 零值/非法时间回 `-`） |
| F20 | `/reports/llm` | `GET /api/reports/llm/timeseries` 500：`avg_latency_ms "0.5333..." → int64` | SQL 除法 numeric 直接 Scan 到 int64 | 已修（ROUND::bigint），待重建复测 |
| D1 | `/asset-bundle/merchant/:aid` | 危机感阈值显示 0、语气词显示 friendly，选项无此项 | 旧版资产包枚举与前端选项集不一致 | 已修（补 0/friendly 选项+字符串归一），已复测 |
| D2 | `/asset-bundle/playground/:aid` | 不存在的 aid 静默 404，页面降级为空白新建模板可保存 | 加载失败只 toast，不阻断保存 | 已修（加载失败横幅 + 保存/发布禁用），已复测 |

## 8. 修复与回归（2026-10-26）

全部 244 条路由（两形态）逐条人工点验完毕；本节记录 16 条 `[!]` 的收口证据。

### 8.1 后端

| 修复 | 文件 | 回归证据 |
|---|---|---|
| AI 产能 SaaS 全 0 | `ops/service/ai_productivity.go` 透传 request ctx；`ops/controller/ai_productivity.go` | 重建后 SaaS `/aiProductivity/list` 与私域同为 95 会话/240 AI 回复 |
| 备份统计「最近成功 -」「总大小 0 B」 | `repository/r44_gap.go`（`status IN (success,completed)` + `SumCompletedSize`）；`service/r44_gap_services.go` | `/backup/enhanced`：最近成功 2026-10-26 03:05，总大小 714.6 KB |
| 转化漏斗 SaaS 五段全 0 | 并行泳道已加 `BuildFunnelCtx/GetStageDetailsCtx` 并切 controller；本轮重建验证 | SaaS 恢复 线索 61 / 会话 95，degraded 横幅消失 |
| LLM 报表 timeseries 500 | `repository/llm_usage_report.go`：`ROUND(SUM(latency_sum_ms)::numeric/SUM(call_count))::bigint` | `GET /api/reports/llm/timeseries` 200，返回 `call_count:15` |

### 8.2 前端

| 修复 | 文件 | 回归证据 |
|---|---|---|
| 导览「查询」→「搜索」 | `i18n/locales/zh.json`、`en.json` | 文案与页面按钮一致 |
| 私域角色「商户管理员」 | `Profile.vue`（tenantMode 分流） | 私域显示「管理员」，SaaS 仍「商户管理员」 |
| AI 工具页头写死 40 | `aiAgent/ToolManagement/ToolList.vue` | 副标题动态「管理 1 个 AI 工具」 |
| 批量操作工具卡不渲染 | `batchOperation/List.vue`（解包 `{list}` + label/icon 映射） | 4 张工具卡与对话框正常 |
| 大屏构建「添加组件」空实现 | `dashboardScreen/Builder.vue` | 点击即加折线块 + 「已添加组件」 |
| 商户门禁回填非法值 | `assetBundle/MerchantEditor.vue`（补 0/friendly 选项 + 字符串归一） | 显示「关闭（不触发…）」「友好亲切（兼容旧版配置）」 |
| 资产详情空对象假资产 | `assetMarket/Detail.vue`（无可识别字段视为不存在 + `el-empty`） | 不存在 id 显示「资产不存在，或当前部署未接入资产市场」 |
| Playground 404 仍可保存 | `assetBundle/Playground.vue`（loadFailed 横幅 + 禁用保存/发布 + handler 兜底） | 不存在 aid：横幅出现，保存按钮 disabled |
| 零值时间显示 1/1/1 / 0001-01-01 | `sms/List.vue`、`KnowledgeWorkspace/QACandidates.vue`（year<=1 视为 '-'） | 两页时间列显示 '-' |
| 爬取源知识库列显示裸 ID | `KnowledgeWorkspace/CrawlerSources.vue`（kbName 映射） | 显示「R85 RAG实战KB」等名称 |
| 知识库漏斗进页全 0 | `geo/KeywordMining.vue`（onMounted 补 loadFunnel） | 私域进页即 22407/420/17344/368369 |
| TikTok 统计全 0 | `tiktokCard/Stats.vue`（snake_case 兼容：total_cards/daily_stats/popular_cards…） | 24 卡/22 激活/列表与最近活动恢复 |
| LLM 报表 KPI 全 0 | `reports/LlmUsageDashboard.vue`（去掉 `const {data}=...` 误拆） | KPI 15 调用/53.33%/210 tokens |
| 增强日志列表恒空 + 时间空 | `operationLog/Enhanced.vue`（读 `list` + createdAt/user 映射 + 默认近 7 天） | 20 行、时间与操作人正常 |
| 销冠画像「5 维度」与 7 维不符 | `persona/List.vue` | 文案改 7 维，与「评估维度 7」一致 |
| SaaS 设置页出现私域文案 | `system/Config.vue`（tenantMode 分流） | SaaS 显示「按订阅套餐限制」 |
| 旅程大屏 0 值无解释 | `customerJourney/Dashboard.vue`（零值时 info 横幅说明内存态） | 横幅「本进程尚未捕获客户旅程状态」 |
| 浏览器任务空提交英文键名报错 | `browserAutomation/Editor.vue`（name/url 前置校验） | 空提交仅「请填写任务名称」一条 toast，不发请求 |
| 登录态偶发被清（401 爆发） | 新增 `utils/tokenRefresh.js`；`request.js` 续期改用该单飞锁；`chatSocket.js` 1006 不再无条件轮换、4001 才 force，且复用同一锁 | 7 枚单测全绿；浏览器内并发 `refreshAccessToken({force:true})` ×2：仅一次轮换、双方同值、`token`/`user_info` 均在、未跳登录 |

### 8.3 判为「非缺陷 / 设计口径」

- 转化漏斗阶段转化率 9500%：`conversion_funnel.go` 明确写「阶段间无单调约定」，`NonMonotonicStagesBaseline` 测试钉住该现状；属口径而非坏数（F13 撤销）。
- `/email/smtp` 27 条空记录：为历史测试数据残留；`CreateEmailSmtpRequest` 六个字段均 `binding:"required"`，实测空提交 400（F15 撤销）。
- `/sopAgent/list`「已激活 0」：统计请求返回前截图的瞬时态；`/api/sop/stats` 实测 `active:5`（F23 撤销）。
- `/followups/*`、`/dashboard/drafts*`：特性开关未装配，页面按 503/档位分别措辞，`[~]`。

## 9. 二次审核 / 对抗性检查 / CI

### 9.1 对抗性抽查（主智能体亲自跑）

| 检查 | 方法 | 结果 |
|---|---|---|
| 租户隔离未因 ctx 透传而放宽 | SaaS `/api/ai-productivity/overview` | 95 会话 / 240 AI 回复（与私域同值，未跨租户合并） |
| 备份统计在 SaaS 的租户范围 | SaaS `/api/backup/stats` | total=124、lastSuccess=2026-10-26 03:05、totalSize=731724 |
| 增强日志时间过滤 | SaaS `/api/operation-logs?start=…&end=…` | total=54400，当前页 2 行（非空） |
| TikTok 统计字段兼容 | SaaS `/api/tiktok-card/stats/overall` | total_cards=24 / active_cards=22 / total_views=1 |
| 空对象资产详情 | SaaS `/api/v1/asset-market/detail/nope` | 200 + `data:{}`；前端按「不存在」空态渲染，不再出假资产 |
| Playground 合法 aid | `/asset-bundle/playground/starter-bundle` | 正常加载，`loadFailed=false`，保存可用 |
| 报表四腿解包 | `/reports/llm` | KPI 15 调用 / 53.33% / 210 tokens，趋势与明细非空 |

### 9.2 构建与测试（合并后的上游树）

- `go build ./...`：rc=0
- `go test ./internal/ops/... ./internal/repository/...`：全绿（ops 4 包 + repository 152s）
- `go test ./internal/service`：**ok 843.4s**
- `user-web npm run build`：rc=0（PWA 608 precache）
- `user-web npm run lint:check`：**0 errors**（1209 warnings 为存量）

### 9.3 提交与推送

- `8ddbff49` 主修复（27 文件 + 点验报告）
- `f0d1973c` 清 ESLint 5 处 error（存量债，Lint 作业常年红）
- `dfd8504c` 修 lint.yml 三处 `$VAR（` → `${VAR}`（macOS bash3.2 + UTF-8 吃掉变量名，控制组全 PASS 却 exit 1）
- `a0aa75da` 修 RELEASE_NOTES 全角括号被 lychee 当端口（Docs Link 常年红）
- 双远端 master 均已推至 `a0aa75da`

### 9.4 仍为红/已知存量（非本批引入，已逐条归因）

| 作业 | 现象 | 归因证据 |
|---|---|---|
| Docs Consistency · `check-no-xapptool` | 140 处仍指向已下线线上域（`website/`、`docs/OPEN_ITEMS.md（历史台账已归档）`） | 命中集中在 website 构建/内容与旧任务清单；本批 diff 不含这些文件 |
| Static gates · 日期口径守卫 | 24 处 vs 基线 21 | **在上游基线 `dc021489` 的独立 worktree 复跑同为 24/21（rc=1）**，与本批无关 |
| Security scans · govulncheck | Go 依赖漏洞 | `go.mod/go.sum` 未在本批变更 |
| npm audit (user-web) | 依赖公告 | `package-lock.json` 未在本批变更 |
| user-server-ci · Unit tests -race | `TestSSEScopedPublisher_ReceivesTenantBoundContext`（bridge_outbound 全局 SSE publisher）与 `TestTenantService_LoginSuspendedTenant`（module gate 全局）DATA RACE | **在上游基线 `dc021489` worktree 以同命令复跑 `-race` 同样 FAIL（rc=1）**；两处文件本批未改 |
| user-server-ci · Coverage | 随 service 包失败级联 | 本机 `go test ./internal/service`（非 -race）843s 全绿 |
| Docs Link Check | 已随 `a0aa75da` 修复并通过 | — |
| Lint（含电池控制组） | 已随 `f0d1973c`（ESLint）与 `dfd8504c`（bash3.2 `$VAR（`）修复并通过 | — |
| ci-bridge | 已随 `60895bfc`（navigator getter-only）修复并通过（73 文件/898 通过） | — |

## 8. 2026-10-26 晚复跑（ego lite 真点，off + SaaS 两形态）

> 触发：`docs/任务总表-2026-10-24.md` 收口后的一次性 UI 回归。浏览器按全局规则走 ego lite（space 13），不是 Playwright 新 profile。

| 项 | 值 |
|---|---|
| 前端 | `127.0.0.1:8211`（→8424 off）/ `127.0.0.1:8212`（→8425 on），`USER_SERVER_PORT` 分别指向 |
| 后端 | 当前 HEAD 源码构建；旗子同 API 轮（`FF_LTC_ORDER_DRAFT_DB=on`、`FF_LTC_APPROVAL_RESUME=on`） |
| 账号 | `uit_admin` / `Hive@Test123`；off 直接登录，on 留空租户代码登录 |
| 取证 | DOM 快照 + 逐行单元格文本；截图在 `/tmp/ego-ui-1026/*.png` |

### 8.1 B2 撤回（本轮重点）

| 步骤 | 结果 |
|---|---|
| 出站已读行「撤回」按钮 | 仅在 `direction === 'outbound' && !recalled_at` 时出现（入站行无按钮） |
| 二次确认 | `确定撤回消息 #1383（抖音）？撤回后平台侧那条消息会被删除，此操作不可撤销。` 取消/撤回两键 |
| 渠道不支持（douyin 桥接） | 顶部 warning：`recall not supported for channel（桥接渠道 msgID 是合成键，不是平台消息标识）` —— **修复前这句哨兵文案会复读两遍**（本轮已修，见 API 报告 §7.1） |
| 失败留痕 | 打开 #1383 详情，「撤回状态 = 上次撤回失败」（`recall_error` 落库并在抽屉展示） |
| 终态渲染 | 临时把该行 `recalled_at` 置为 now（测后已还原为空）：刷新后状态列 `已撤回 已读`，操作列「撤回」按钮消失；详情显示「已撤回（时间）」 |

> 成功出网路径未验：本机无 Telegram/飞书/企微凭据，且可撤窗口（TG 48h）内的行不存在。属环境前提，不记缺陷。

### 8.2 上一轮遗留项复核

| # | 复核读数 |
|---|---|
| F4 profile 角色措辞 | off=`管理员`，on=`商户管理员` —— 已按 `tenant_mode` 分形态，不再泄漏 |
| F6 AI 工具页头 | `管理 1 个 AI 工具` / `工具总数 1`，与列表 1 行一致 —— 硬编码 40 已消除 |
| F1 导览文案 | 本轮未再触发（引导页首次进即关），无「查询/搜索」错位复现 |
| i18n（reachPipeline） | 三个 tab `Pipeline 管理 / 任务监控 / 统计分析`，页内 `reachPipeline.*` 原始 key 泄漏 **0** 处 |

### 8.3 各页复跑

| 路由 | off | on | 备注 |
|---|---|---|---|
| `/messageHub/list` | 20 行，无失败提示 | 20 行，同 | 含撤回列与详情抽屉 |
| `/notifications` | 20 行，表头 状态/类型/来源/标题/内容/时间/操作；标签 `警告/系统` | 20 行，统计卡 `20 总消息 / 0 未读` | 004 的 source/level 展示在位 |
| `/reachPipeline/list` | 7 行 / 14 卡，无原始 key | 未单独跑 | W-1 基线未回退 |
| `/aiAgent/tools` | 1 行，页头 1 | 未单独跑 | F6 复核 |
| `/profile` | 模块 4 卡，角色「管理员」 | 角色「商户管理员」 | F4 复核 |
| `/dashboardScreen/list` | 7 卡，无错误 | 未单独跑 | — |
| `/aiProductivity/list` | 未单独跑 | 正常渲染（无 0/0 空报告提示） | A4/A7 修复未回退 |

> 本轮未逐条覆盖 244 条路由；`router-list.md` 的进度列不回退，新增读数只针对上表与 B2。

