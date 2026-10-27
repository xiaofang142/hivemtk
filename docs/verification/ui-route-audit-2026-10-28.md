# 全路由 UI 拟人点击测试报告（2026-10-28）

## 1. 范围与方法

- 路由源：`user-web/src/router/index.js` + `user-web/src/router/modules/*.js`（86 个模块文件，AST 解析父/子路由并拼接完整路径）。
- 路由清单：工作区根 `router-list.md`（245 条：9 条静态 + 236 条模块路由，含动态参数路由）。
- 测试工具：ego-browser（真实 Chromium，复用登录态），**每次调用只测一个路由**，不做批量脚本遍历。
- 每个路由执行：
  1. 打开 `http://127.0.0.1:8211/#<route>` 并刷新，等待网络稳定；
  2. 采集 `window.onerror`、`unhandledrejection`、`console.error`、fetch/XHR ≥400、页面错误/警告提示；
  3. 逐个点击页面上的安全交互元素（标签页、筛选、查询/刷新/详情/编辑/新建/分页等，最多 8 个），每次点击后复查新增错误；删除/发送/支付/停用等破坏性按钮跳过；
  4. 在搜索/名称输入框输入 `test` 并回车，再清空；
  5. 结果落盘到 `evidence-20261028/<route>.json`。
- 动态路由（`:id`）使用数据库中的真实 ID 抽样（如智能体 30、FAQ 2、知识库 2、渠道 3、任务 554、资产包 `ast-general-ecs`、工作流 2、执行 1 等）。

## 2. 环境

| 项 | 值 |
|---|---|
| 前端 | Vite dev server `http://127.0.0.1:8211`（user-web） |
| 后端 | `user-server/bin/user-server`，`APP_ENV=development`，`PORT=8204` |
| 数据库 | Docker `mtk-postgres` → `127.0.0.1:8202`，库 `user_db` |
| 独立部署模式 | `TENANT_MODE=off`（主测形态，245 条路由在此形态下遍历） |
| SaaS 形态 | 额外以 `TENANT_MODE=on` + `PORT=8304` + Vite 8311 起第二实例，验证登录（租户代码 `tei6385`）、`/system/config`、`/module-market`、`/customer360/list`，无 console/API 错误 |
| 登录账号 | 本地测试账号 `e2e_admin`（tenant 1）；测试前重置为本地开发口令（未写入仓库） |

两种形态的差异（实测/文档口径）：

- `off`：登录页无租户代码输入；`/api/public/config` 返回 `edition` 非 saas、`tenant_mode=false`；所有数据归默认租户 1。
- `on`：登录页出现「租户代码（可不填）」；`/api/public/config` 返回 `tenant_mode=true`、`edition=saas`；JWT 带 `tenant_id`，接口按租户注入隔离条件；`/register` 为公开自助开户入口。
- 后端以同一份二进制、仅 `TENANT_MODE` 开关切换；未知取值拒绝启动（fail-safe）。

## 3. 结果

| 结果 | 数量 |
|---|---|
| ✅ 通过（无 console/error/API 4xx-5xx） | 233 |
| ✅ 已修复并回归 | 2（`/kb/connectors`、`/feishu/account`） |
| — 未验证（动态参数无对应业务数据） | 10（15 条动态路由已用真实 ID 抽样通过，见 `router-list.md`） |
| ❌ 未修复失败 | 0 |

所有路由均未出现页面级 JS 异常；没有发现 4xx/5xx（`/kb/connectors` 修复前例外）。

## 4. 发现并修复的问题

### 4.1 飞书账号「刷新Token」返回 500 + 双重错误提示

- 复现：`/feishu/account` 点击任一账号的「刷新Token」（种子账号凭据已失效）。
- 现象：`POST /api/feishu/accounts/16/refresh-token` → 500；页面先弹「服务器内部错误」，再弹「网络错误：刷新失败」两条同义报错。
- 根因：
  1. `service/feishu.go` 把飞书返回的 `code=10003 invalid param` 当成普通 error，控制器回 500；
  2. 前端 `request.js` 把 502/503/504 一律翻译成「服务器内部错误」，丢掉后端可照做的原因；
  3. 页面 catch 又补弹一条，形成重复提示。
- 修复：
  - 后端用 `bizerr.CodePlatformUnavail` 包装飞书不可达/返回错误/空 token → `ErrorFromDB` 映射为 **502**，消息带飞书 code/msg；
  - 前端 502/503/504 改为展示服务端 message（500 仍保留通用内部错误文案，避免泄露内部细节）；
  - `FeishuAccount.vue` 的刷新失败不再二次弹窗，由拦截器统一提示。
- 回归：新增 `user-web/tests/unit/request_502_upstream.test.js`（502 显示原因 / 500 显示通用文案）；真浏览器复测只剩一条「刷新飞书 Token 失败：code=10003 invalid param」。

### 4.2 全量 `go test ./...` 的环境前置

- 首次全量跑失败于 `aiagent/knowledge/*`：本机 embedding 服务 8208 未启动；启动 `scripts/inference-host/start-embedding.sh` 后相关包通过。
- `internal/pkg/db` 的静态锁 `TestBatchM_MigrateIsWiredIntoAPILifecycle` 仍按旧 seed 写法断言，已同步为当前 `InitDefaultStorageIfEmpty(repository.NewObsConfigRepositoryWithDB(db.GetDB()))` 并保持「顶层独占一行、位于 AutoMigrate 之后」的原判据。

### 4.3 `/kb/connectors` 同步 HTML 清单返回 500「服务器内部错误」

- 复现：连接器 `webhttp` 配置 `{"url":"https://example.com"}`，点击「同步」。
- 现象：`POST /api/kb-connectors/1/sync` → 500；页面同时弹「服务器内部错误」和 `invalid character '<' looking for beginning of value`。
- 根因：上游 URL 返回 HTML 网页，`json.Unmarshal` 失败后按普通 error 上抛，控制器一律映射 500。
- 修复：
  - `internal/kbconnector/provider.go` 新增哨兵错误 `ErrInvalidInput`；
  - `webhttp.go` 对缺 `manifest_url`、非法 URL、HTTP 非 200、Content-Type/首字符为 HTML、清单 JSON 格式错误统一归为配置类错误，并给出可操作提示；
  - `sync.go` 对未知类型/非法 config 同样归类；
  - `controller/kb_connector.go` 将 `ErrInvalidInput` 映射为 HTTP 400。
- 回归：新增 `internal/kbconnector/webhttp_test.go`（HTML 清单、缺 URL、合法清单 3 例）；重新构建后真浏览器点击「同步」显示「清单 URL 返回的是 HTML 网页而不是 JSON 清单（请填写 .json 清单地址…）」，不再出现 500。

### 4.4 存量测试与实现口径漂移（回归时发现，非 UI 运行时缺陷）

- `user-web/tests/unit/api_abExperimentPlus.test.js`：实现已把 `method` 放进 `{params:{method}}`（否则 axios 顶层配置会丢查询参数），测试仍断言旧形态；已同步断言。
- `user-web/tests/unit/conversionFunnel_summary.test.js`：组件使用 `$t` 但测试未安装 i18n 插件导致 `$t is not a function`；已安装 i18n，并把「端到端转化率」旧标题断言改为按值断言（标题已诚实化为「端到端比值（会话/访问…）」）。
- `user-server/internal/app/tool_alert_wiring_test.go`：告警 handler 是 goroutine 异步派发，测试立即读计数在机器繁忙时假红；改为轮询等待（2s 上限）+ shadow 后留 150ms 观察窗。

### 4.5 导出类按钮用 window.open 直链导致 401（对抗性复查发现）

- `/operationLog/enhanced` 点「导出」：`window.open('/api/operation-logs/export?...')` 新标签页不带 `Authorization`，后端恒回 401；原探测脚本把新标签页当 popup 关闭，因此第一轮未把它计为页面错误。
- `/customerSession/list` 的「导出会话记录」同一写法（`/api/customer-sessions/:id/transcript`）。
- 修复：两处改为 axios 带令牌请求 `responseType: 'blob'`，用一次性 object URL 下载；失败由请求拦截器统一提示。
- 回归：真浏览器点「导出」返回 200 且提示「导出已开始」；后端日志不再出现该端点 401。

## 5. 证据

- 逐路由结果：`evidence-20261028/*.json`（每个路由一份，含 clicks、API、console、alerts）
- 路由清单与逐条状态：工作区根 `router-list.md` 末节「逐路由实测状态」
- 后端日志：`/tmp/hivemtk-user-server.log`、`/tmp/hivemtk-user-server-saas.log`

## 6. 最终验证

| 项 | 命令 | 结果 |
|---|---|---|
| Go 全量 | `POSTGRES_TEST_PORT=8202 POSTGRES_TEST_PASSWORD=<local> go test -timeout 30m ./...` | ✅ exit=0（148 个包 ok，0 FAIL） |
| 前端单测 | `npx vitest run` | ✅ 71 files / 705 tests 全通过 |
| 前端构建 | `npm run dev`（Vite 8211）+ ego-browser 真浏览器 | ✅ 页面正常、无 console error |

## 7. 未覆盖与限制

- 10 条动态参数路由缺少可用业务数据（如订单草稿表为空、部分卡片详情/embed 渠道无记录、模块 key 无目录数据），未做真实点击；其余 15 条用真实 ID 抽样验证通过。
- 语音/视频、真实第三方外发（短信/邮件/WhatsApp 发送）、支付与退款等破坏性链路未触发，避免产生真实外发或资金副作用；相关页面只做只读加载与安全按钮点击。
- SaaS 形态只抽测 4 个代表性路由与租户代码登录，未重复遍历全部 245 条；跨租户越权仅做了接口级 404 抽查（tenant 1 token 访问 tenant 2 资源），未构造第二个租户的完整登录会话。
- 推理栈未启动，`/readyz` 为 503（inference 未就绪），属于环境限制，非本次 UI 缺陷；依赖 LLM 的生成类按钮只验证到「参数校验/请求发起」层。
