# user-web 回归测试 SOP（脚本资产 + 操作规程）

> 2026-11-13 固化。来源：L 轮（11-12）258 路由双形态逐页点验 + M 轮（11-13）对抗性抽查的实操沉淀。
> 本目录是「允许的辅助」资产库——**遵守 hivemtk/CLAUDE.md 规则-1**：
> UI 点验只能 `page.goto` → `page.snapshot()` → **显式指定单个元素**（ref/loc/css）点击或填值
> → 观察快照/接口结果 → 下一元素；**禁止「自动枚举元素 + 循环点击」的任何跑批驱动**。
> 每页必须留证据（快照文本、接口状态码、toast、console 计数）才算测过。

## 目录资产

| 文件 | 作用 |
|---|---|
| `ego-helpers.mjs` | ego-browser 助手函数：console/网络采集、双形态登录（逐元素显式填值）、带 token 的接口探针 |
| `../mark.py` | router-list.md 行级状态/证据维护（`--stats` / `--status` / `--note` / `--show`） |
| `../gen_router_list.py` | 由 `user-web/src/router/*` 重建 258 行索引，**重跑保留既有勾选** |
| `nr-batch.mjs` | N2 轮逐路由批次运行器：about:blank 跳板单挂载、单次事件采集（console+Network 同批分类）、安全钮候选依次尝试；只做逐路由串行，无元素枚举循环 |
| `gen_route_queue.py` | 由 router-list.md 生成逐路由导航队列（实参化口径同历轮：:id→1、:channel_ref→k_test_20261110、通配→/no-such-page-xyz） |
| `mark_batch.py` | 读 /tmp/nr-batch.json 批量落证（nr-batch.mjs 每批末尾写它）；即 mark.py 的批处理形态，用法见下 |
| `o-probe.mjs` | O 轮逐路由只读探针（console/非2xx/路由名/主区文本/按钮名）；`__S__`/`__E__` 占位 sed 替换后 `ego-browser nodejs <` 跑 |
| `o-scan.mjs` | O 轮对抗性复扫：采集 #app 全文扫「误报失败」关键词，专抓 API 200 但前端误报失败的盲区缺陷 |
| `o_mark.py` | O 轮落证：读探针 JSON（`PROBE_JSON`，默认 /tmp/o-probe.json）把「11-13 O轮线上」证据追加进 router-list.md |
| `../../user-web/scripts/gen_api_list.cjs` | 由 `src/api/*.js` 重建 api-list.md，**v4 起非破坏式：按（文件\|方法\|动词\|路径）保留既有「实测/问题-修复」列**（v3 曾整表覆写清掉 281 条实测证据，勿回退该行为） |

## 一、环境启动配方（双形态，2026-11-13 实测）

两形态共用一个 `user-server` 二进制、同一 CWD（`user-server/`），差异全在环境变量：

```bash
# 私域独立部署（前端 8320 → 后端 8314；库 user_db_standalone）
cd user-server && \
PORT=8314 TENANT_MODE=off PLATFORM_ENABLED=false \
CONFIG_PATH="$PWD/config.standalone.yaml" \
INSTALL_LOCK_PATH="$PWD/install-standalone.lock" \
[其余共享 env 与 .env 同源] nohup ./bin/user-server > /tmp/user-server-standalone.log 2>&1 &

# SaaS 多租户（前端 8321 → 后端 8204，与桥接 ingest 同实例；库 user_db）
cd user-server && \
PORT=8204 TENANT_MODE=on PLATFORM_ENABLED=true APP_ENV=development \
[其余共享 env 与 .env 同源] nohup ./bin/user-server > /tmp/user-server-saas.log 2>&1 &

# 前端（vite 代理目标见 vite.audit.standalone.config.js / vite.audit.saas.config.js）
cd user-web && npx vite --config vite.audit.standalone.config.js &   # 8320
cd user-web && npx vite --config vite.audit.saas.config.js &         # 8321
```

判活：`curl 127.0.0.1:8314/health`、`:8204/health` 均 200；`/api/public/config`
独立形态=`tenant_mode:false / edition:opensource`、SaaS=`tenant_mode:true / edition:saas`。
健康但模式错 = 配方没带对，先查 env 再动手。

## 二、测试门禁命令（改动后必跑，改哪层跑哪条）

| 改动 | 命令 |
|---|---|
| Go 任意层 | `cd user-server && go build ./... && go test ./internal/<包>/...` |
| Go 全量（提交前） | `make test-go`（需先 `export POSTGRES_TEST_HOST=127.0.0.1 POSTGRES_TEST_PORT=8217 POSTGRES_TEST_PASSWORD="$(grep '^POSTGRES_PASSWORD=' ../.env \| head -1 \| cut -d= -f2-)"`；端口钉扎与容器重建处置见 CLAUDE.md §3.5） |
| 提交前总门禁 | `make audit` + `make lint` + `make vet` + `make test-go` |
| user-web 前端 | `cd user-web && npm run test`（vitest）+ `npm run lint`（0 error 红线） |
| bridge | `cd user-web/bridge && npm test && npm run lint && npm run build`（lint 0 错，警告 <80） |
| 清单自一致 | `python3 scripts/gen_router_list.py`（语义 diff 须为 0）· `node user-web/scripts/gen_api_list.cjs`（✅/❌/⬜ 计数与改前一致） |

## 三、逐页点验协议（规则-1 合规）

对 router-list.md 里的**一个**路由：

1. `python3 scripts/gen_router_list.py` 确认索引与源码一致（防漂移）。
2. ego-browser 打开该页（用 `ego-helpers.mjs` 的 `openWithConsole`，自动挂 console 采集）。
3. **逐元素**显式操作：快照 → 读 ref/loc → 单个 click/fill → 再快照。一次只动一个元素。
4. 每页收口四件事：① console.error 计数 ② 非 2xx 请求逐条归因（设计内拒绝 vs 缺陷）
   ③ 关键交互的 toast/页面状态 ④ 变更类操作后 `page.fetch` 复核 API。
5. 落证据：`python3 scripts/mark.py <行号> --status x --note "11-13 M轮：…（round-N）"`。
   全量回归（N2 轮口径）用 `nr-batch.mjs` 批次推进 + `python3 scripts/regression/mark_batch.py <形态标签>` 批量落证
   （读 /tmp/nr-batch.json；导航队列用 `python3 scripts/regression/gen_route_queue.py` 重建，与 nr-batch.mjs 的 6 列解析对齐）。

### N2 轮协议教训（2026-11-13 实测，勿回退）

1. **每路由 about:blank 跳板再 goto**：长 SPA 会话内直接 hash 跳转会让懒加载路由模块
   静默失挂载（/followups/today 主区空渲染实锤）；goto+reload 又会打断在途 XHR 产生
   假 console.error（#82 cerr=4 工件根因）。跳板=单次全新挂载，两头都避。
2. **page.events() 取走即清空**：每路由只许调一次，console 与 Network 同批分类；
   调两次第二次恒空（非 2xx 采集全废的第一版缺陷）。
3. **安全钮候选依次尝试**：首个匹配钮可能在隐藏 tab 容器里不可点（#66）或是
   设计内禁用态（空表单/未选客户，#66/#151）——点不动≠页面坏，结合 contentOK 与
   cerr 判定，如实记「设计内禁用」。
4. **主区空挂载哨兵**（.el-main textContent>20）只适用 Layout 内页；公开页/Widget/登录页
   恒 false（#1-#5/#166-170），按页型豁免。
5. **设计内拒绝的 console 降噪口径**：组件 catch 里对设计内拒绝（404 卡片不存在、
   模块未开通 4302）应 console.warn 而非 error——与 ChatWindow 策略门（#4）同口径；
   L 轮对 #82 的「0 console.error」记录与当时代码不符（组件 catch 用了 error 级），
   已按 warn 口径修正四处 CardStats + AlertCenter。

## 多角色审计矩阵（2026-11-13 审计轮实测口径）

| 角色 | 形态 | 验证点 | 结论 |
|---|---|---|---|
| 独立部署超管 admin | 8320 | N2 全量 258 路由 | 全绿 |
| SaaS 租户 admin（mround_audit01，提权后） | 8321 | N2 全量 258 路由 | 全绿（SaaS 模块门重定向为设计态） |
| **游客（未登录）** | 8320+8321 | 受保护页→#/login 重定向、登录表单渲染（SaaS 多租户代码字段）、公开页可达、API 401 边界 | PASS（curl 独立验证服务端 401；浏览器端见下方方法论） |
| **零菜单用户** | 8321 | 登录落地不落 403 → /profile | PASS（M 轮 fallbackLandingPath 修复） |
| **部分授权角色 customer_service（55 菜单）** | 8321 | 落地=首个授权菜单 /inbox/list；授权页渲染；未授权页 403；全局 GEO 徽标 403 静默降级 | PASS |
| 生产云访客 | work.xapptool.cn | 登录页/帮助中心/聊天挂件只读 | PASS（挂件 401=策略门设计内） |

### 游客态方法论（必读，否则测了个寂寞）

浏览器里「清掉 localStorage token」≠ 游客：旧文档的轮询 timer 会在 removeItem 与
reload 之间的窗口把**内存态 token 回写** localStorage（启动期无任何 refresh 网络调用，
纯前端回写），假登录态随之复活——2026-11-13 实测连清三次都中招。真游客启动只有一种
做法：**Storage.prototype.setItem 守卫封死 token/user_info 回写 → removeItem → 同 tick
内 location.reload()**。服务端边界另用 curl 无 token 直连验证（401 为正确形态）。
6. 发现缺陷：当轮修复（规则2），修完回归本页 + 相关门禁，才允许标 [x]。
7. 双形态都要过：SaaS 出现的租户/商务属性，独立部署页必须不出现且不报错。

### 助手用法（导入为绝对 file:// URL，ego-browser 脚本无相对导入基）

```bash
ego-browser nodejs <<'EOF'
import { openWithConsole, drainConsoleErrorsAsync, apiProbe } from 'file:///Users/xiaofang/Documents/www/go/hivemtk/hivemtk/scripts/regression/ego-helpers.mjs';
const task = await taskSpace("回归-<轮次>-<形态>");
const page = task.page("p1");
await openWithConsole(page, "http://127.0.0.1:8320/#/system/config-params");
// …逐元素显式操作…
console.log("console.error:", (await drainConsoleErrorsAsync(page)).length);
const res = await apiProbe(page, "POST", "/api/custom-reports", { data_source: "sessions", chart_type: "bar" });
console.log(res.status, res.body.slice(0, 120));
EOF
```

登录态：8320 admin 与 8321 一次性账号的 token 均在各自 origin 的 localStorage `token` 键；
过期重登用 `ego-helpers.mjs` 的 `loginStandalone` / `loginSaaS`（内部逐元素显式填值，无枚举）。

### SaaS 测试账号（一次性，用完可删）

- 注册：`POST /api/auth/register`（on 模式必填 `tenant_code` + 开户登记字段 +
  `regulatory_ack:true`，缺一回 `INVALID_PARAM_1001` 具体项）
- 每轮回归按上行配方**现注册**一次性账号（口令自拟不入库）；提权 admin（用于全页
  回归）在本地 user_db 执行 `UPDATE system_users SET role='admin' WHERE username='<账号>';`
  后重登取新 JWT。历史账号 mround_audit01（tenant tei6385）注册于 11-13 M轮，口令不入库。

## 四、已知设计内拒绝（[~] 的定性依据，回归时勿当缺陷）

- 跟进服务未装配：`/api/followups/*` 503「跟进服务未装配」（followup_test.go 钉的 fail-closed）
- GEO 站点未配置：`/api/geo/site/export` 500「geo_site 未配置」（独立形态 fail-closed）
- LTC 订单草稿：`FF_LTC_ORDER_DRAFT_DB` 未装配 → 页面「底座不可用」降级文案
- 发送测试邮件 503：本地未配发件账号
- 卡片详情 404：`/api/tiktok-card/1` 等 → 页面「这张卡片读不到了」明确降级
- 认证策略门：`/chat/embed/*` visitors 401（AppKeyResolve fail-closed，前端显式捕获展示）
- 短链空列表：`/api/shortlink/list` 空数据返回 `{list:null,total:0}` → 前端须按 `res.list||[]`
  兜底（2026-11-13 O轮修：曾误报「获取短链列表失败」，见下）

## 五、O 轮（2026-11-13，线上 SaaS 全量 + 对抗性复扫）

O 轮 = 用户指令的新一轮全量回归，主验环境=线上 SaaS `https://work.xapptool.cn/`
（jideilvluoqun@gmail.com，ego lite TaskSpace `hivemtk-route-test` p1；线上原构建落后
本地 121 个提交，先部署最新代码再测）。流程与资产：

1. **部署**：`cd user-web && npm run build` → `rsync -az --exclude='.user.ini' -e 'ssh -p 57122'
   dist/ mtk-prod:/www/wwwroot/hivemtk/user-web/`（**不加 --delete**，保留旧 chunk 防 SW 404；
   线上备份在 `user-web.bak.<日期>`）。**回滚三步**（新构建出问题时）：
   ① `ssh mtk-prod 'mv /www/wwwroot/hivemtk/user-web /www/wwwroot/hivemtk/user-web.bad.<日期>'`
   ② `ssh mtk-prod 'mv /www/wwwroot/hivemtk/user-web.bak.<日期> /www/wwwroot/hivemtk/user-web'`
   ③ 浏览器侧 `navigator.serviceWorker.getRegistrations()` 全 unregister + `caches.keys()`
   全 delete + 带 `?cb=<ts>` 重载（SW 修复前旧版会被 precache 钉死；修复后新 sw.js 自动覆盖）。
2. **只读探针批跑**（规则-1 合规：全程不点任何元素）：`o-probe.mjs`（console/非2xx/路由名/
   主区文本/可见按钮名 → JSON）+ `o_mark.py` 落证（本轮在 /tmp，用法见 git log 与 router-list.md
   「O轮线上」行）。258 条分 10 批跑完，逐行落证。
3. **对抗性复扫**（堵「API 200 但前端误报失败」盲区）：`o-scan.mjs` 采集 #app 全文扫 13 个
   失败关键词。258 条复扫结论：0 console.error；命中 4 处全部设计内（#26 资产包 playground
   :id=1 不存在、#82/#155/#231 抖音/快手/TikTok 卡片统计 :id=1 不存在，页面均明示
   「读不到 ≠ 真的没有」并禁用保存）。
4. **核心路由显式单元素点击**（读快照后逐个指定 ref/loc）：messageHub「刷新统计」、
   inbox「刷新」、notifications「刷新」均 0 console.error、0 非2xx。

### O 轮修的两个真 bug（勿回退）

1. **短链空数据误报失败**（`user-web/src/views/shortLink/List.vue`）：`if (res && res.list)`
   在 API 返回 `list:null` 时走 else 分支误报「获取短链列表失败」。修：`if (res) { list =
   res.list || []; total = res.total || 0 }`。**同类模式回归口径**：grep `res && res.` 于
   `views/`，凡「判真后才赋值、else 报错」的形态都必须确认有空值兜底；
   `whatsapp/WhatsappDrafts.vue`、`WhatsappJobs.vue` 的 `else if (res && res.list)` 已有兜底。
2. **PWA 自更新被自己的 precache 打死**（`user-web/vite.config.js`）：
   `workbox.additionalManifestEntries` 把 `version.json` 显式加进 precache，旧 SW 把旧
   version.json 返回给 main.js 的版本自检 ⇒ localBuildId===remoteBuildId ⇒ 永不
   unregister/reload，用户被钉死旧版（实测停留 4 天）。修：删除该条目（precache 路由按
   URL 匹配，与 main.js 的 `cache:no-store` 无关）。自愈链靠新 sw.js 字节变化触发
   skipWaiting/clientsClaim。**回归口径**：部署后 `grep -c version.json dist/sw.js` 必须为 0。

### O 轮环境与账号备忘

| 项 | 值 |
|---|---|
| 线上主验 | `https://work.xapptool.cn/`（hash 路由：所有深链必须 `/#/path`） |
| 账号 | `jideilvluoqun@gmail.com`（生产租户 18 tz3rmdj 超管，口令不入库） |
| SSH 部署 | 别名 `mtk-prod` = root@101.35.252.112:57122；站点根 `/www/wwwroot/hivemtk/user-web` |
| ego TaskSpace | `hivemtk-route-test`（page p1；同轮复用同一 space） |
| CDP 打补丁教训 | `Page.addScriptToEvaluateOnNewDocument` 在 ego-browser 不生效；须 goto 后 `page.evaluate` 打补丁 + 同文档 hash 导航 |
| goto 等待 | `waitUntil:'load'`（本应用有长连接，networkidle 常超时；不支持 networkidle2） |

## 六、历史轮次索引

| 轮 | 日期 | 范围 | 结论 |
|---|---|---|---|
| J | 11-10 | 258 路由首建+双形态点验 | 索引与口径立起 |
| K | 11-10 | 线上 SaaS 抽验 | 云端口径补齐 |
| L | 11-12 | 258 路由双形态全覆盖 | 240 [x] + 18 [~]，0 缺陷在册 |
| M | 11-13 | 对抗性抽查 + 门禁收口 | 见 router-list 各行「11-13 M轮」注记 |
| N2 | 11-13 | 258 路由 × 双形态逐路由 reload 单挂载探针 | 全绿；nr-batch.mjs/mark_batch.py 固化 |
| O | 11-13 | 线上 SaaS 258 路由只读点验 + 对抗复扫 + 核心路由显式点击 | 258/258 落证；修短链误报 + SW 自更新两真 bug（见第五节） |
