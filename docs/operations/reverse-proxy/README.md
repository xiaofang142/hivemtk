# 反代配置模板（v3 审计 P1-A1）

> **关键约束**：长连接（SSE / WebSocket）必须命中模板里对应的 location。命中不到 = 用
> 反代的默认值代理这条连接，而默认值恰好是给短请求调的（nginx `proxy_buffering on`
> 与 `proxy_read_timeout 60s` 同时生效），表现就是「大屏/桥接时好时坏、WS 连不上」。

## 一、真实在跑的长连接路径（现测，别照抄旧文档）

| 类型 | 路径 | 注册位置 |
|---|---|---|
| SSE | `/api/bridge/outbox/sse` | `user-server/internal/router/router.go`（bridgeWS 组） |
| SSE | `/api/dashboard/sse` | `user-server/internal/router/service_routes.go` |
| SSE | `/api/chat/typing-predict/sse` | `user-server/internal/router/business_routes.go` |
| WS | `/api/ws/channel` | `user-server/internal/router/router.go`（bridgeWS 组） |
| WS | `/api/ws/visitor` | `user-server/internal/router/chat_routes.go` |
| WS | `/api/ws/agent` | `user-server/internal/router/service_routes.go` |
| WS | `/api/browser/host-ws` | `user-server/internal/router/browser_automation_routes.go` |

**不在表里的两条**（老文档写过或容易猜错）：

- `/api/sse/*` —— **没有这个前缀**。实测 `GET /api/sse/dashboard` → 404；`/api/sse/health` → 404。
  看板 SSE 的真实路径是 `/api/dashboard/sse`（`sse` 在末尾，不在中间）。
- `/ws/chat` —— 看着像 WS，实测 `GET /ws/chat` 返回 **200 + 前端 index.html**（它落到 SPA 的
  NoRoute 兜底，不是 WebSocket 端点）。`internal/router/ws.go` 的 `RegisterWSRoutes` 没有调用方。

重取这张表的命令（改路由后跑这三条，别相信任何一份静态清单）：

```bash
cd user-server
grep -rn -E '\.(GET|Any)\("[^"]*(sse|ws)' internal/router/ | grep -v _test
# 逐条判存在性：带前缀 /api 的注册了 → 401/400；没注册 → 404
for p in /api/bridge/outbox/sse /api/dashboard/sse /api/chat/typing-predict/sse \
         /api/ws/channel /api/ws/visitor /api/ws/agent /api/browser/host-ws \
         /api/sse/dashboard; do
  printf '%s %s\n' "$(curl -s -o /dev/null -m 3 -w '%{http_code}' "http://127.0.0.1:8204$p")" "$p"
done
```

## 二、心跳/帧节奏（用来判断"是不是被反代缓冲了"）

第一行是实测；后两行是读码得到的常量，**没有**跑过那两条流（看板 SSE 要后台登录态），
别把它们的数字当测量值用。

| 端点 | 建连后 | 空闲时的保活 | 出处 |
|---|---|---|---|
| `/api/bridge/outbox/sse` | 首帧 `retry: 15000` | 注释帧 `: ping`，约每 15s 一帧（40s 窗口实测收到 2 帧） | 实测 + `internal/bridge/sse.go` |
| `/api/dashboard/sse` | 立即一帧 `event: dashboard_update` | 每 2s 推数据；每 15s 推 `event: heartbeat` | 读码 `internal/controller/dashboard_sse.go:39,75,77` |
| `/api/chat/typing-predict/sse` | 立即一帧 | 每 30s 一帧 `type: ping` | 读码 `internal/controller/typing_predict.go:88` |

- 队列里没有待下发消息时，桥接 SSE **只有 `: ping`，没有任何 data 帧** —— 这是正常的"没货"，
  不是"流断了"。要验业务链路请配合 `user-server/scripts/bridge-e2e-sim.sh`。
- 四个流式写入点都自发 `X-Accel-Buffering: no`（`internal/bridge/sse.go:556`、
  `internal/controller/typing_predict.go:74`、`internal/controller/dashboard_sse.go:58`、
  `internal/service/sse_hub.go:379`）。`/api/mcp` 只在 Accept 头校验里出现
  `text/event-stream` 字样，它自己不流式响应，不在这张表里。nginx 会**消费**这个头：
  实测经 nginx 转发后客户端响应里看不到它，这不代表没生效。
- 因为上游发了这个头，「SSE 路径没命中 location」在今天的代码上**不会**表现为缓冲；
  它真正欠的是那条 location 里的 `proxy_read_timeout 24h`。上游哪天少发这个头，
  缓冲就回来了 —— 所以模板里仍然把无缓冲写在反代侧，不依赖上游实现。

## 三、HTTP/2 与 SSE 的张力

SSE 基于 HTTP 长连接，要求：
1. **HTTP/1.1 持久连接**（HTTP/2 多路复用可能切断长连接）
2. **响应不缓冲**（nginx `proxy_buffering off`；Caddy `flush_interval -1`）
3. **读超时大于心跳间隔**（心跳最长 30s，nginx 默认 `proxy_read_timeout 60s` 只是勉强够）

## 四、模板清单

| 反代 | 模板 | 强制 HTTP/1.1 的方式 | 本机跑测结论 |
|---|---|---|---|
| nginx | `nginx.conf.template` | `http2 off;`（per-server） | 跑通（1.26.3） |
| Caddy | `caddy.Caddyfile.template` | `transport http { versions 1.1 }` | 跑通（v2.11.4）；`versions h1` 起不来 |
| Traefik | `traefik.yml.template` | 3.5 的静态配置里没有关 HTTP/2 的旋钮（`entrypoints.<name>.http2` 只有 `maxconcurrentstreams`）；长连接改配 `transport.respondingtimeouts.*` | 明文跑通（v3.5.6）；带 TLS 只验到"能加载" |
| FRP | `frpc.toml.template` | 默认 HTTP/1.1，无需额外配置 | 未跑（本机无 frpc） |

旧文档在这一格写过 `versions h1` 和 `entryPoints.websecure.http.http2.enabled: false` ——
两条都会让进程起不来，实测见 §六。

nginx 模板的两条 location 写成模式匹配，新增流式端点不必再改配置：

- `location ~ ^/api/.+/sse$` —— 覆盖上表三条 SSE；普通业务接口不以 `/sse` 结尾，不会被抢。
- `location ~ ^/api/(ws/|browser/host-ws$)` —— 覆盖上表四条 WS。`host-ws` **不在** `/api/ws/`
  前缀下，只写 `/api/ws/` 会把它留给 `location /api/`，那里 `proxy_set_header Connection ""`，
  握手必败（实测见下）。

## 五、验证 SSE 正常（照抄可跑）

```bash
# 凭证闸门只读 X-Bridge-Token（SSE 也支持 ?bridge_token=），不读 Authorization Bearer。
# 取当前生效凭证：后台「桥接凭证」页，或（在仓库根 hivemtk/ 下执行，口令只走环境变量）
PGPASSWORD=$(awk -F= '/^POSTGRES_PASSWORD=/{print $2}' .env | head -1) \
  psql -h localhost -p 8232 -U admin -d user_db -tA \
      -c "SELECT value FROM system_config_kv WHERE key='bridge_ingest_token'" > /tmp/bt
TOK=$(cat /tmp/bt)   # 实测 ${#TOK}=43（/tmp/bt 本身 44 字节，末尾多一个换行）；取空 = 闸门整批 401

# 1) 直连 user-server（基线：应当先看到 retry: 15000，之后每 ~15s 一个 ": ping"）
curl -N -H "X-Bridge-Token: $TOK" \
  "http://127.0.0.1:8204/api/bridge/outbox/sse?channel=douyin&account_id=probe"

# 2) 经反代：把上面 URL 的主机名换成你的域名，其余不变
curl -N -H "X-Bridge-Token: $TOK" \
  "https://your-domain.com/api/bridge/outbox/sse?channel=douyin&account_id=probe"

# 3) 判"被缓冲"vs"没货"：没货 = 连接活着、: ping 按秒表均匀到达；
#    被缓冲 = 一个字节都收不到，直到超时/上游关闭才整块吐出。
```

判读口径：

| 现象 | 结论 |
|---|---|
| 404 | 路径不存在（多半是把 `/api/dashboard/sse` 记成了 `/api/sse/dashboard`） |
| 401 `UNAUTHORIZED_2001` | 路径对，凭证缺失/不对 —— 不是反代问题 |
| 400 `unsupported bridge channel` | 路径对、凭证对，渠道不在桥接五渠道里（`telegram` 等直接投递渠道不走 SSE） |
| 200 但一个字节都不来 | 反代缓冲 —— 检查是否命中 `location ~ ^/api/.+/sse$` |
| 200 + `: ping` 均匀到达 | 正常 |

## 六、实测对照（2026-09-29，三种反代都在本机容器里真跑起来）

取数方式：一个可控上游（逐帧 flush、帧间隔 1s，并会把**收到的 Upgrade/Connection 头**记进日志），
把模板里的 location/matcher 本体逐字搬进去，只替换三类测试值 —— 上游地址与端口、监听端口、
TLS 材料。判据：`帧到达时刻 = 0s/1s/2s` ⇒ 逐帧下发（没被缓冲）；一个字节都收不到 ⇒ 被缓冲。

### 6.1 nginx 1.26.3

| 用例 | 旧模板 | 新模板 |
|---|---|---|
| `/api/bridge/outbox/sse` 帧到达时刻 | **收不到任何响应**（15s 超时，连状态行都没下发） | `0.0s / 1.0s / 2.0s`（逐帧下发） |
| 同上游改为自发 `X-Accel-Buffering: no` | `0.0s / 1.0s / 2.0s`（上游把头救了这条流） | 同左 |
| `Upgrade: websocket` 转发到 `/api/ws/channel` | `Upgrade=websocket, Connection=upgrade` | 同左 |
| `Upgrade: websocket` 转发到 `/api/browser/host-ws` | **`Upgrade=null, Connection=null`**（落进 `location /api/`） | `Upgrade=websocket, Connection=upgrade` |
| 真实 user-server 的桥接 SSE 经新模板 | — | 200，35s 内收到 `retry: 15000` + 2 个 `: ping`（30 字节边收边到） |

旧模板的 `location /api/sse/` 命中的是不存在的前缀 ⇒ 那段 `proxy_buffering off` 从来没生效过；
同块里的 `add_header X-Accel-Buffering no` 也不起作用 —— nginx 只认**上游响应**里的这个头，
往客户端再挂一份不改变 `proxy_buffering` 的行为。
另注：经 nginx 转发后，客户端响应里看不到 `X-Accel-Buffering`（nginx 消费了它），这是正常的。

### 6.2 Caddy v2.11.4

| 用例 | 结果 |
|---|---|
| 旧模板 `caddy validate` | **退出码 1**：`parsing caddyfile tokens for 'reverse_proxy': unrecognized response matcher path`（`@sse` 写在 reverse_proxy 块里） ⇒ 用旧模板的 Caddy 根本起不来 |
| 旧模板的 `transport http { versions h1 }` | 语法能过，**provision 失败**：`unsupported HTTP version: h1, supported version: 1.1, 2, h2c, 3` ⇒ 正确字面量是 `versions 1.1` |
| 新模板 `caddy validate` | 退出码 0，`Valid configuration` |
| 新模板跑起来：`@sse` 是否真命中 | 把兜底 handle 的上游换成第二个端口做分桶 ⇒ SSE 记在长连接上游、`/api/health` 记在兜底上游 |
| 新模板跑起来：`/api/bridge/outbox/sse` | `0.0s / 1.0s / 2.0s` 逐帧下发 |
| 对照：裸 `reverse_proxy`（不写 flush_interval） | 同样 `0.0s / 1.01s / 2.01s` ⇒ Caddy 对 `text/event-stream` 默认就 flush，`flush_interval -1` 不是这条流能活的理由 |
| 删掉旧模板那两行 `header_up Upgrade/Connection` 后 | `/api/ws/channel`、`/api/ws/visitor`、`/api/browser/host-ws` 上游都收到 `Upgrade: websocket` ⇒ Caddy 自带透传，那两行是多余的 |

### 6.3 Traefik v3.5.6

| 用例 | 结果 |
|---|---|
| 旧模板启动 | **退出码 1**：`field not found, node: http2` ⇒ `entryPoints.websecure.http.http2.enabled` 这个键在 3.5 不存在（`traefik --help` 里 `entrypoints.<name>.http2` 只有 `maxconcurrentstreams`） |
| 旧文档写的 `traefik validate` | 3.5 **没有** `validate` 子命令（`command not found: validate`）；静态配置靠启动即失败来验，判据 = 容器是否保持 Up + 日志有无 `field not found` |
| 新模板（明文 :8080 版）跑起来 | SSE `0.0s / 1.0s / 2.01s` 逐帧；两条 WS 路径上游收到 `Upgrade: websocket`；`/api/health` 落到另一份 service（分桶证明 `PathRegexp` 命中） |
| 新模板（本文件这份带 TLS 的形状） | 容器保持 Up、无 `field not found` ⇒ **只验到"配置能加载"**；https 路由没跑通，ACME 账号取不到时 router 被丢弃并回 404（`unable to get ACME account: open …/acme.json: no such file`） |
| 我自己先写的一版 `serversTransport: longLived:` | 也起不来（`field not found, node: longLived`）⇒ v3 静态侧 `serversTransport` 不是具名映射，已从那份模板里删掉 |

### 6.4 FRP

`frpc.toml.template` 只做过路径口径核对（它自己不含 SSE/WS 路径，`type=http` 天然透传
Upgrade，见该文件第 32 行的注），本机没有 frpc 二进制，**未跑**。

## 七、升级指南

反代配置来自老版本时，升级必须：
1. 显式加 `http2 off`（或等价配置）
2. 把 SSE 的 location 从 `/api/sse/` 改成能命中 `/api/**/sse` 的形式
3. WS 的 location 覆盖 `/api/ws/` **和** `/api/browser/host-ws`，并透传 Upgrade 头
