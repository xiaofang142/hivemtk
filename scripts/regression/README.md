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
- 已有：`mround_audit01 / Audit#20261113 / tenant tei6385`（本地 user_db，role=user，
  零菜单——正好用于回归「新用户落地 /profile 不落 403」这一口径）

## 四、已知设计内拒绝（[~] 的定性依据，回归时勿当缺陷）

- 跟进服务未装配：`/api/followups/*` 503「跟进服务未装配」（followup_test.go 钉的 fail-closed）
- GEO 站点未配置：`/api/geo/site/export` 500「geo_site 未配置」（独立形态 fail-closed）
- LTC 订单草稿：`FF_LTC_ORDER_DRAFT_DB` 未装配 → 页面「底座不可用」降级文案
- 发送测试邮件 503：本地未配发件账号
- 卡片详情 404：`/api/tiktok-card/1` 等 → 页面「这张卡片读不到了」明确降级
- 认证策略门：`/chat/embed/*` visitors 401（AppKeyResolve fail-closed，前端显式捕获展示）

## 五、历史轮次索引

| 轮 | 日期 | 范围 | 结论 |
|---|---|---|---|
| J | 11-10 | 258 路由首建+双形态点验 | 索引与口径立起 |
| K | 11-10 | 线上 SaaS 抽验 | 云端口径补齐 |
| L | 11-12 | 258 路由双形态全覆盖 | 240 [x] + 18 [~]，0 缺陷在册 |
| M | 11-13 | 对抗性抽查 + 门禁收口 | 见 router-list 各行「11-13 M轮」注记 |
