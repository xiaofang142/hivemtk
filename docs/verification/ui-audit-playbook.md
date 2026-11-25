# UI 逐页点验 Playbook（可复用）

每轮 UI 审计按本清单执行，避免“只截图不看接口”“只看 SaaS 不看独立部署”“修完不回归”。

## 0. 环境

| 形态 | 启动 | 校验 |
|---|---|---|
| 独立部署 | `npx vite --config vite.audit.standalone.config.js`（8320→8314） | `/api/system/init-status` 返回 `edition=opensource` |
| SaaS | `npx vite --config vite.audit.saas.config.js`（8321→8315） | 同上返回 `edition=saas`，页面有订阅横幅 |
| 后端 | `zsh /tmp/restart-hivemtk-dev.sh`（重建 `/tmp/hivemtk-user-server-ui-audit`，8314 off / 8315 on） | 两端口 `/api/health` ok |

> 前端是 **hash 路由**：必须访问 `http://127.0.0.1:8320/#/path`，不能只写 `/path`。

## 1. 路由清单

```bash
python3 scripts/gen_router_list.py   # 只生成索引，保留既有勾选
grep '^| [0-9]' router-list.md | grep '\[ \]'
```

**注意**：生成器会重排部分备注列（历史备注可能丢失），跑前先 `cp router-list.md /tmp/router-list.bak`；只信 `[x]/[~]` 状态。

## 2. 单页点验协议（每页必做）

1. 打开页面（等 3–4s；`transition mode=out-in` 下新页是在旧页离场后才挂载，2s 会读到上一页 DOM）
2. 采集 `console.error/warn`、`window.onerror`、`unhandledrejection`
3. 采集 Network 非 2xx（fetch + XHR 都要包）
4. 点该页**所有**可点元素：筛选/搜索/重置/分页、主操作/新增（先空提交看校验，再填合法值提交）、行内编辑/详情/删除（确认框文案要读出来）、Tab/抽屉/图表刷新
5. 有写操作就清理测试数据（记下 id，删完回查列表）
6. **双端各做一遍**；SaaS 的租户/商务元素在独立部署不得出现
7. 更新 `router-list.md`：`[x]` 或 `[~]`（设计内拒绝必须写清原因），备注写“验了什么 + 修了什么”

## 3. collector 片段（ego-browser 注入）

```js
const COLLECTOR = `(() => {
  if (window.__AUDIT) return;
  const A = window.__AUDIT = { logs: [], errors: [], net: [] };
  const str = a => { try { return typeof a === 'string' ? a : JSON.stringify(a) } catch { return String(a) } };
  ['error','warn'].forEach(k => { const o = console[k].bind(console); console[k] = (...a) => { A.logs.push({ type:k, msg:a.map(str).join(' ') }); o(...a) } });
  addEventListener('error', e => A.errors.push({ msg:e.message, src:e.filename, line:e.lineno }));
  addEventListener('unhandledrejection', e => A.errors.push({ msg:'unhandledrejection: ' + (e.reason?.message || e.reason) }));
  const rec = (m,u,s,b) => { if (s >= 400) A.net.push({ method:m, url:String(u), status:s, body:String(b||'').slice(0,300) }) };
  const of = window.fetch;
  if (of) window.fetch = async function(...a) {
    const r = await of.apply(this, a);
    try { const u = typeof a[0] === 'string' ? a[0] : a[0]?.url; const c = r.clone();
      c.text().then(t => rec(a[1]?.method || 'GET', u, r.status, t)).catch(()=>rec(a[1]?.method || 'GET', u, r.status, ''));
    } catch {}
    return r;
  };
  const oo = XMLHttpRequest.prototype.open, os = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.open = function(m,u,...r){ this.__m=m; this.__u=u; return oo.call(this,m,u,...r) };
  XMLHttpRequest.prototype.send = function(...a){ this.addEventListener('loadend', ()=>{ try{ rec(this.__m,this.__u,this.status,this.responseText) }catch{} }); return os.apply(this,a) };
})();`
await page.cdp('Page.enable')  // 必须先 enable，否则 addScript 静默不生效
await page.cdp('Page.addScriptToEvaluateOnNewDocument', { source: COLLECTOR })
```

- 每次导航后 `window.__AUDIT = { logs:[],errors:[],net:[] }` 清零，否则会带上一页的旧响应
- 关键约定：`net` 只记 ≥400；页面里“设计内拒绝”要看 response body 确认原因，不要只看状态码

## 4. 判定

| 结果 | 条件 |
|---|---|
| `[x]` | 控制台无 error、无非预期非 2xx、主操作闭环、测试数据已清理 |
| `[~]` | 页面/文案正确，失败来自未装配/未配凭证/模拟探针，且页面已给出可执行说明 |
| `[!]` | 有缺陷 → 修复 → 双端回归 → 才允许改 `[x]` |

## 5. 常见坑

- 弹窗内按钮文案不一定是“确定”：可能是“保存/创建/确认删除”，按文案点，别猜
- 行内删除确认框按钮可能是“确认删除”，不是“确定”
- 剪贴板 API 在无头/无权限环境会失败：失败提示存在即可，不算缺陷
- `window.open` 在自动化环境被拦截；用 `task.tabs()` 或直接验证 API 200 + 报告内容
- 前端构建必须过：`npx vite build --mode production`（HMR 能跑不代表能打包）
- Go 测试要用专用快测库：
  ```bash
  password=$(docker exec mtk-postgres-test printenv POSTGRES_PASSWORD)
  user=$(docker exec mtk-postgres-test printenv POSTGRES_USER)
  export POSTGRES_TEST_DSN= POSTGRES_TEST_HOST=127.0.0.1 POSTGRES_TEST_PORT=8217 \
         POSTGRES_TEST_USER="$user" POSTGRES_TEST_PASSWORD="$password" \
         POSTGRES_TEST_DBNAME=user_db_test EMBEDDING_ALLOW_FALLBACK=true
  go test ./internal/geo/service ./internal/service ./internal/controller -count=1
  ```
  （首次先跑 `go test ./internal/pkg/testutil -run '^TestTestDBAuthenticationPreflight$'` 建库）

## 6. C 轮全量复测回归要点（2026-11-25）

### 环境与账号
- 本地 SaaS：`127.0.0.1:8321` → 后端 `127.0.0.1:8204`；登录态用 `go run ./cmd/minttoken` 铸本地 admin JWT（uid=26 / uit_admin），注入 cookie `hmtk_token` + localStorage `user_info`。**token 只经脚本内变量传递，不落盘、不打印。**
- 线上 SaaS：`https://work.xapptool.cn/`，租户 18（tz3rmdj / admin），复用浏览器登录态；生产环境只做只读点验，零写入。
- 路由权威台账：产品仓根 `router-list.md`（280 条，`scripts/gen_router_list.py --out router-list.md` 生成，`scripts/mark.py` 落证据）。

### 单页采集模板（逐元素显式操作，禁止批量枚举点击）
```js
await p.events();                                  // 清空事件缓冲
await p.goto(BASE + '/?a=' + Date.now() + '#' + R);
await new Promise(r => setTimeout(r, 2000));       // out-in 过渡，2s 以上再读 DOM
const info = await p.evaluate(() => ({ hash: location.hash,
  txt: (document.body.innerText||'').replace(/\s+/g,' ').slice(0,150),
  btns: [...document.querySelectorAll('button')].filter(x=>x.getClientRects().length>0).map(x=>x.innerText.trim()).filter(Boolean).slice(0,6) }));
// 点击目标由操作者阅读快照后逐个明确指定（刷新/查询/筛选/详情/重置），不写循环遍历
const ev = await p.events();
const cerr = ev.filter(e => e.method==='Runtime.exceptionThrown' ||
  (e.method==='Runtime.consoleAPICalled' && e.params?.type==='error'));
const bad = ev.filter(e => e.method==='Network.responseReceived' && e.params.response?.status>=400)
  .map(e => e.params.response.status + ' ' + new URL(e.params.response.url).pathname);
```

### 已定性的设计内非 2xx（不要误判为缺陷）
| 路由 | 非 2xx | 判定依据 |
|---|---|---|
| `/chat/embed/*` | 401 `/api/chat/public/*` | 公开页游客探针 |
| `/asset-market` | 403 `/api/v1/asset-market/credits` | 平台集成未启用 |
| `/bridge/extension` | 404 `/api/bridge/extension/package` | 扩展包未生成 fail-closed |
| `/followups/today` | 503 `/api/followups/*` | 跟进服务未装配，页面有明示 |
| `/backup/list`（线上） | 403 `/api/backups` | 平台运营权限，页面明示「仅平台运营方可执行」 |
| `/ops-overview`（线上） | 403 `/api/system/stats` | 平台运营专属 |
| `/aiAgent/edit/:id`、`/browser-automation/tasks/:id` | 404 | id 不存在，fail-closed 降级；查列表 API 取真实 id 复验 |
| `/geo-tools/site-publish`、`/geo-tools/platform-publish` | — | 外部发布操作设计内拒绝，写操作从不执行（`[~]`） |

### 路由行为校准
- `/bridge`、`/token` 当前 redirect 到 `/channels/identify-codes`（源码 `bridgeToken.js` 明确定义），与历史台账写的 `/bridge/token` 不同；页面正常，非缺陷。
- `/crm/import-export` 的 CSV 导出（`responseType: blob` 修复）本轮回归正常。

### C 轮 go test 全量结果（2026-11-25 21:02）
- 命令：`POSTGRES_TEST_*` 快测库 + `EMBEDDING_ALLOW_FALLBACK=true`，`go test ./... -count=1 -p 1 -timeout 1800s`（user-server 目录）。
- 结果：156 包 ok、2 包 FAIL、8 个测试失败。失败与 `.planning/2026-11-25-workspace-commit/focused-failure-repro.log` / `user-go-isolated-final-tests.log` 历史记录一致，均位于本轮未改动的 Go 源码（本轮改动仅 Markdown）。
- `internal/router`（3）：`TestRouteGuardChain_AnonymousSurfaceIsExactlyTheAllowlist`、`...AdminOnlyRoutesCarrySuperAdminGuard`、`...CapabilityRoutesCarryGuard`—`sagex/upload`、`sagex/bootstrap`、`POST /api/mcp` 注册在 `InitGuard` 组之外，缺身份/超管/能力守卫。
- `internal/service`（5）：`TestChannelDispatchService_SelectionMatrix/preferred`、`TestCRMEntityWrite_AllowedAccountAndClear`、`TestCRMEntityWriteAccountRulePairIsApplied`（3 子用例）、`TestLeadHistory_CanonicalAccountAliases`、`TestSageXBootstrapInstallationCrossTenantUUIDWithRealRepository`—并行开发线的 CRM 账号规则/渠道分发/线索历史/sagex 认证问题，非 UI 测试引入。
- 前端 `npx vite build --mode production` 通过（PWA 705 entries）。

### INV-04a/b/c（3 个 router 守卫失败）根因、误修警示与修复状态
这 3 个失败原登记于 `docs/architecture/FULL_FEATURE_INVENTORY.md:250-252`，**不是本轮 UI 测试引入，也不能靠"补守卫"修**。已于 **2026-11-25 `2656c5452` 修复并链 A 上线**（见 `PROD_TOPOLOGY_CHANGES.md` 当日条目）；下表为历史根因与实际采用的修法（非最初的设想方案）：

| 缺陷 | 路由 | 根因 | 实际修法（`2656c5452`） |
|---|---|---|---|
| INV-04a | `POST /api/integrations/sagex/{upload,bootstrap}` | 只挂 `InitGuard`（安装态，非身份守卫）；鉴权在 controller/service 用 `identify_code` 经 ChannelResolver 反查租户/账号完成 | 在 `route_policy_gate_test.go` 登记为 `external-credential` 档位并写明 identify_code 自证理由，新增 `TestRouteGuardChain_ExternalCredentialRoutesKeepTheirOwnGuards` 断言 |
| INV-04b | 同上 | 命中 `middleware/route_policy.go:108` 的 `/api/integrations/**` 仅超管表但链上无超管守卫 | 给 `adminRouteRule` 增加 `ExceptPatterns`，只精确排除这两条外部凭证端点；其余集成管理端点仍受超管策略保护 |
| INV-04c | `POST /api/mcp` | 在生成的能力表 `menu_capability_routes.go:303` 里但独立组无 `RoutePolicyGuard`；`MCPGuard` 本身已是专用凭证身份门（凭证对/单值密钥 + tenant_code，无凭证 fail-closed 503） | 从生成的能力表删除 `/api/mcp` 行 + 加 `CapabilityOwnerKeysFor("/api/mcp")==0` 断言。**生成器 `scripts/menu-route-role-audit.py` 没有"专用凭证排除名单"机制**（只有强制纳入的 `FORCED_CAPABILITY_SHAPES`），早期"加排除名单重跑"的设想不成立，勿照做 |

**误修警示**：给 sagex 组挂 `RoutePolicyGuard`/JWT 会因读不到 role 直接 401，打断真机上报；给 mcp 组挂能力守卫会因无授予 403 打断外部 AI 客户端调用（能力表命中还要求 JWT，而 MCP 调用方没有登录态）。运行时探针实证：无凭证访问 upload/mcp 返回 401、bootstrap 404（配置未启用），并非真实匿名漏洞。现这三条已修复上线，若 go test 再报同类失败应查新回归而非按旧基线处理。

## 7. D 轮深度点验方法（数据三源核对，2026-11-25）

C 轮只做"加载观察 + 只读点击"；D 轮追加**数据正确性**核对，按以下三源交叉（每页只核对一两个最关键的数值，不做脚本批点）：

1. **API**：`page.fetch(页面实际请求的路径)` 取 `data.total` / `data.list.length` / 统计值（注意方法是 GET 还是 POST——`/api/asset-bundle/list` 必须 POST + JSON body，GET 会 400）。
2. **DOM**：页面"共 N 条"文字、`table tbody tr` 行数、统计卡片数值。采集行数时**只取第一个真实数据表**（`querySelectorAll('table tbody tr')` 会把日期面板的 7×2 行算进去，造成虚高）。
3. **DB**：本机 `psycopg2` 直连 `127.0.0.1:8202`（user_db），查对应表行数/字段值；比对时先排除软删除：例如 `clues` 表物理 320 行、`deleted_at IS NOT NULL` 293 行，GORM 自动追加 `deleted_at IS NULL` 后未软删 27 行 = API total 27（`model.Clue.DeletedAt` 是 `gorm.DeletedAt`；`repository/clue_query.go` / `service/clue.go` 没有 `data_scope` 条件，不要拿这个差值论证 US1 数据权限）。
4. **表单/下拉**：空提交看 `.el-form-item__error` + toast + 是否发 POST（前端校验应拦截）；下拉展开核对选项来源（硬编码 preset / API / allow-create），区分多个 select 的 Teleport dropdown 别混采。

### D 轮代表性核对结果（API total = 页面分页 = DB）
- `/profile`：API current-user(id26/uit_admin/UI测试管理员/email) = DOM 值 = DB `system_users` row26。
- `/channels/accounts`：DB `channel_accounts` 3 行 = API 3 条 = 表格 3 行；搜"deepverify"→1 行、重置→3 行。
- `/aiProductivity/list`：overview API 53/96.29%/3.145s/340145 = DOM 53/96%/3/340,145。
- `/faq/list` 13393、`/operationLog/list` 54506、`/geo-tools/alerts` 17747、`/messageHub/list` 785、`/badCase/list` 264、`/crm/list` 139、`/oneid/list` 141、`/clue/list` 27。
- 线上（租户18）：clue 43、crm 30、faq 30、badCase 235、opLog 4980、c360 54——全部 API total = 页面分页。

### 口径说明
- 实际分级（逐行统计，不夸大）：`D轮深度` 167 条中，真正含 DB 数字的只有 **9 条**（API/DOM/DB 三源）；含 API/统计数值的 **约 158 条**（API↔DOM 核对）；4 条纯表单结构复核（行 17/29/43/61，无数值）。另有 `D轮复核` 113 条（导航 + 只读交互）、12 条 `D轮线上深度`。写报告时按此分级表述，不能把 167 条统称为"三源核对"。
- C 轮 280 行中 267 行是**批量导航基线证据**（"逐条显式导航 + 只读元素点击；0 cerr/0 非2xx"的模板文字），它证明整站无加载级错误，但**不能当作逐元素操作证据**；具体元素与数据的证据以 D 轮带 API 数值/具体交互对象的条目为准。
- 表单页写操作一律不提交；深链 `:id` 用列表真实 id 复验，不存在的 id 返回 404 属 fail-closed 降级。
- 当前本地 8204 实例 `TENANT_MODE=off / edition=opensource`（`.env` 被并行会话改过），渠道下拉为空是版本过滤设计内；线上是 SaaS 租户 18。
