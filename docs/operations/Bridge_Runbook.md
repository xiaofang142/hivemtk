# Bridge 桥接系统企业运维手册 (Runbook)

> **版本:** 1.0
> **日期:** 2026-08-15
> **维护:** HiveMTK 运维组
> **适用范围:** HiveBridge Chrome 扩展（user-web/bridge）+ 桥接后端接口（user-server `/api/bridge/*`）+ 宿主机推理栈
> **配套文档:** [SLA/SLO](SLA_SLO.md) · [高可用部署](HA_DEPLOYMENT.md) · [灾难恢复](DR_RECOVERY.md) · [AI 智能体部署](AI_AGENT_PERF_DEPLOY.md) · [反代长连接配置](reverse-proxy/README.md)

---

## 0. 拓扑速览与告警入口

### 0.1 组件与端口

| 组件 | 位置 | 端口 / 协议 | 启停方式 |
|------|------|-------------|----------|
| user-server (Go) | `user-server/` | :8204 HTTP | 当前 dev 形态＝容器 `mtk-user-server-dev`（air 在容器内热重载，源码 bind-mount，`127.0.0.1:8204->8204`）；宿主机 air 走 `make dev`；systemd 部署按站点 unit |
| PostgreSQL 15+ | Docker `mtk-postgres` | 容器内 :8202，宿主机映射 `127.0.0.1:${USER_POSTGRES_HOST_PORT:-8202}`（本机 `.env` 现为 8202） | `make db-up / db-down` |
| Redis 7 | Docker `mtk-redis` | :8203（宿主机同口映射） | `make db-up / db-down` |
| LLM (llama-server) | 宿主机 | :8207/v1 | `make inference-host-up / down` |
| Embedding (TEI/llama) | 宿主机 | :8208/v1 | 同上 |
| Rerank | 宿主机 | :8209 | 同上 |
| HiveBridge 扩展 | 浏览器端 | 三通道 HTTP | 扩展管理页加载/刷新 |

### 0.2 桥接三通道协议（HiveBridge ↔ user-server）

| 通道 | 方向 | 说明 |
|------|------|------|
| uplink | 扩展 → 服务端 | `POST /api/bridge/ingest`，上报会话/消息（`X-Bridge-Token` 头） |
| outbox | 扩展 → 服务端 | `GET /api/bridge/outbox?channel=&account_id=&limit=`，拉取待下发消息。**是非阻塞轮询，不是长轮询**：仓内没有任何长轮询实现（`grep -rn "LongPoll\|long_poll\|/poll" user-server/internal/router/ user-server/internal/controller/` 零命中），空集也立刻返回（本机现测三次 `time_total` = 11.7 / 4.7 / 4.6 ms）。生产默认不走这一口，只有 capabilities 报 `sse_enabled=false` 时扩展才按 1.5s 节拍轮它 |
| ack | 扩展 → 服务端 | `POST /api/bridge/outbox/ack?channel=&account_id=`，确认已下发（`AckOutboundItem` 原子化 `UPDATE...RETURNING`，`user-server/internal/repository/message_hub_inbox_outbound.go:42-43`）。契约（本机现测）：`channel`/`account_id` 缺任一回 400；v2 `items[]` 每项必须同时带 `msg_id` + `conversation_id`，缺 `conversation_id` 回 400 `conversation_id required (v2 items[] must carry msg_id + conversation_id)`；`status` 只认 `delivered`/`failed`（留空按 `delivered`），其它值在任何写库前整批拒 400 `invalid status: ...`；单次条数上限看 `config_params` 的 `bridge.max_ack_msg_ids`（种子缺省 500）。**注意 false green**：ack 一个库里不存在的 msg_id 仍回 200，真话在 body 的 `not_found_count` / `items[].status="not_found"` |

三通道之外还有两条只读口（同一 `X-Bridge-Token` 闸门）：

| 口 | 端点 | 说明 |
|------|------|------|
| SSE 下行 | `GET /api/bridge/outbox/sse?channel=&account_id=` | **生产默认下行形态**：扩展启动时先探 capabilities，`sse_enabled=true` 就走 SSE 并直接 return，轮询定时器根本不创建（`user-web/bridge/src/core/polling-loop.js:88-106`）。`EventSource` 不能带自定义头，凭证走 `?bridge_token=`（闸门同一份提取逻辑，`user-server/internal/middleware/bridge_ingress_guard.go:38-49`）。建连先下发 `retry: 15000\n\n`（本机 `od -c` 实测首帧 14 字节），之后空闲时每 ~15s 一条注释帧 `: ping\n\n`（8 字节；`user-server/internal/bridge/sse.go:20`、`:565`、`:671`）。渠道只认桥接五渠道 `douyin / xiaohongshu / kuaishou / xianyu / tiktok`（`user-server/internal/channelgw/registry.go:105-114`），别名（`douyin_web` 等）服务端会先归一再用（`internal/bridge/channel.go:19-35`），非桥接渠道 `channel=telegram` 现测回 400 `unsupported bridge channel`。经反代必须免缓冲，见 [反代长连接配置](reverse-proxy/README.md) |
| capabilities | `GET /api/bridge/capabilities` | 下行形态协商口，服务端能力自报，现测 `{"poll_interval_ms":1500,"sse_enabled":true,"sse_heartbeat_ms":15000}`（平铺 JSON、无 `code/message/data` 信封，`user-server/internal/controller/bridge_capabilities.go:44-48`）；缺凭证现测回 401 `{"code":"UNAUTHORIZED_2001","message":"缺少 X-Bridge-Token"}`。**扩展侧把它当保守探测**：请求失败或非 2xx 时按 `sse_enabled=false` 处理（`user-web/bridge/src/core/polling-loop.js:50-61`）⇒ 降到 1.5s 轮询。运维判「为什么下行变慢/日志里在打轮询」，先 `curl` 这个口看 `sse_enabled`，再看服务端 `FF_SSE_BRIDGE`。**这个开关不切换服务端传输**：它只在 `bridge_capabilities.go:45` 被读出来写进 `sse_enabled`，`outbox` 与 `outbox/sse` 两条口永远在；定义见 `user-server/internal/pkg/featureflag/flag.go:28`，注册默认 `true` 在 `:69`（同文件 `:27` 那句注释写「false＝长轮询」，与实际不符——轮询腿是上面那种非阻塞轮询）。env 名按 `EnvNameOf`（`flag.go:166-180`）从旗标名 `sse_bridge` 推出，即 `FF_SSE_BRIDGE` |

### 0.3 一键巡检命令

```bash
make inference-host-status   # 检查 8204/8207/8208/8209 四个端点连通性（Makefile:168-186）
make db-ps                   # 检查 PG + Redis 容器
bash scripts/bridge-monitor.sh          # 桥接健康巡检，默认 30m 窗口
bash scripts/bridge-monitor.sh 90m      # 窗口只认 <整数><s|m|h|d>；1h30m 这类复合写法在入口就被拒
# 开发态日志源二选一（脚本默认容器名 `mtk-user-server` 在本机不存在，得覆盖）：
BRIDGE_CONTAINER=mtk-user-server-dev bash scripts/bridge-monitor.sh          # 模式 C：后端在容器里
BRIDGE_LOG_FILE=$PWD/user-server/logs/user-server.log bash scripts/bridge-monitor.sh  # 宿主机 air / 文件路
```

`bridge-monitor.sh` 的日志源两条路：给了 `BRIDGE_LOG_FILE` 就读该文件；没给则找 docker 容器
（容器名由 `BRIDGE_CONTAINER` 指定，脚本缺省 `mtk-user-server`）。开发态要分清自己是哪一种：
模式 C（`docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d user-server-dev`，容器名
`mtk-user-server-dev`，本机现测 8204 就是它 `127.0.0.1:8204->8204`）用容器那一路，但必须显式覆盖容器名；
宿主机 `make dev`（air 直接把 Go 进程拉在 `user-server/`）没有容器，`docker logs mtk-user-server` 现测退
`Error response from daemon: No such container: mtk-user-server`，脚本随之只打一行
`[WARN] 无日志数据可分析`（rc 仍是 0），这时必须给 `BRIDGE_LOG_FILE`。
文件那一路按每行 `"time":"..."` 字段筛，**没有 time 字段的行（air 自己的输出，落在
`user-server/tmp/air.log`）不计入**——所以别把 air.log 当 `BRIDGE_LOG_FILE`，应用 JSON 日志在
`user-server/logs/user-server.log`（`user-server/config.yaml:46-51`，`logging.file`）。
本机现测以它作数据源跑 2h 窗口：`窗口内日志行数 8394 / http_ingest_request 3 / api_interaction 579`。

窗口写法在两条路上必须是同一个答案，所以脚本做了两件事：
1. **入口先拦语法**：只认 `<整数><s|m|h|d>`（`30m`/`2h`/`45s`/`1d`）。`1h30m` 这类复合写法 docker 能吞、
   文件那一路换算不出，不设这道闸就是一条命令两种"参数合不合法"；复合写法请自己换算（`1h30m` ⇒ `90m`）。
2. **两条路都先把窗口换算成秒**：文件路用秒数算起点时间戳，容器路把原样写法换成 `--since <秒>s` 交给 docker。
   第二条是必需的——`docker logs --since` 用 Go 的 `ParseDuration`，**实测不认 `d`**（`--since 1d` 退 1、
   报 `invalid value for "since"`），而按本脚本的语法 `1d` 是合法窗口；改传 `86400s` 后同一条 `1d`
   在容器路径实测读到 315,094 行。

退出码三档：判 FAIL ⇒ `1`（业务结论「Bridge 功能异常」）；OK/WARN ⇒ `0`（cron/CI 读 rc，不读颜色）；
窗口写法不合法 ⇒ `2` 且**不产出任何报告**（这是"脚本没跑起来"，不是"bridge 坏了"；敲错参数报成 1 会让
cron 拿一次笔误去喊线上故障）。实测：`bash scripts/bridge-monitor.sh 1h30m` ⇒ stderr 一行原因、stdout 0 字节、`rc=2`。

服务端端到端模拟（`user-server/scripts/bridge-e2e-sim.sh`，退出码见该文件 `:749-752`）的退出码分三档，
用来把"环境红"和"业务红"分开：`0` 全绿 / `1` 有业务失败 / `3` 所有失败判定时 `/api/health` 都不是 200
（服务没起或正被热重载换 PID，先恢复服务再复跑，不要按功能回归处理）。这里的 `/api/health` 是**进程可达性**
探针，不是依赖探针：`user-server/internal/controller/system_info.go:39-41` 无条件回
`{"code":0,"message":"ok","data":{"status":"ok"}}`，依赖掉线它照样 200（依赖真值看 `/health`，见 §1.1）。
默认 `BASE_URL=http://localhost:8204`，凭证缺省从库里 `system_config_kv.bridge_ingest_token` 现取，
取不到直接 `exit 2` 并让人 `BRIDGE_TOKEN=xxx` 显式给。撞在这一档上的实测样本：一轮 e2e 里 8 枚 ingest 报红，
真因是跑中途 air 换了监听进程，同脚本对同一份码复跑 ⇒ 88 通过 / 0 失败。

### 0.4 出站行的状态语义（读积压数字前先看这节）

`message_hub` 里 `direction='outbound'` 的每一行代表"服务端欠客户的一条回复"。状态含义与写入方：

| status | 含义 | 谁写 |
|--------|------|------|
| `pending` | 欠交付，等待被认领/补投 | 建行默认值 |
| `inflight` | 已被某次取行认领，30s 租约内（`service.InboxOutboundClaimTimeout`，`internal/service/inbox_ingress_outbound.go:164`） | 三条取行路径的 CAS：`ClaimPendingOutbound` / `ClaimOutboundForPush` / `FetchOutboundUndelivered`（都在 `internal/repository/message_hub_inbox_outbound.go`） |
| `delivered` | 桥端 ack 已下发；直接投递渠道在发送 API 成功后由写侧结算 | ack 接口 / 各渠道 Send |
| `failed` | 终态，不再重推 | 两条自动结算路径（见下）或桥端主动 ack failed |
| `send_failed` | 渠道侧返回失败（走 trace 链路，不进队列） | `PushSendFailureTrace` |

两条**自动**把行判成 `failed` 的路径，靠 `push_error` 列区分：

| `push_error` | 触发条件 | 上界/阈值配置 |
|--------------|----------|----------------|
| `outbound_push_exhausted` | 账号还在拉，但同一条推 20 次仍失败 | 常量 `MaxOutboundPushAttempts`（代码内，无旋钮） |
| `outbound_orphan_expired` | 账号连续 `outbound_orphan_ttl` 未注册/未同步，且行本身也老于此阈值 | `bridge.outbound_orphan_ttl`（秒，缺省 604800＝7 天；0＝停用） |

两种判弃都**不写 `sent_at`**（这条从未交付出去，盖上时间戳会让后续回复被误判成"自己发过的回显"而吞掉）。

**孤儿结算默认只报数**：`bridge.outbound_orphan_dry_run` 缺省 `true`，后台回扫每轮只在日志里打印
`[BridgeReplay] 孤儿出站结算 dry-run（未写库）: ttl=… groups=… candidates=… skipped_reachable=…`，
一行都不改。放量步骤＝读着这行日志确认候选口径无误 → `config_params` 里把该键置 `false` → 下一轮真正落 `failed`。
为什么默认要停在报数侧：这道写入不可逆，而回扫是 5 分钟一跑的后台任务，开发态存盘即热重载进真实例、连真库
（2026-09-28 实测：闸门上线前的一轮 cron 把 38 行历史 pending 直接烧成 failed，事后已逐行还原）。
现成的达标读数（2026-09-29 本机，另用一条独立 SQL 按同一判据复算过，两个数逐字一致）：
`ttl=604800s groups=30 candidates=38 skipped_reachable=0` —— 要放量就是把这 38 行落 `failed`，先核对是不是都该弃。

### 0.5 「在线」只有一个数：按 `last_sync_at` 的宽限窗判，不按 `status` 列

`bridge_accounts.status` 是**粘住的列**：`internal/bridge/account_repo.go:124`（`SetOffline`）和 `:140`
（`TouchLastSync`，只动 `last_sync_at`）是这两列各自的写入点——也就是只有 SSE 正常收尾时才会把它改回
`offline`，扩展崩溃、浏览器被杀、断网、机器休眠都不走那条路径，列就长期停在 `online`。
历史读数：本机 171 行里 159 行标 online，按最后同步时间判定的真值是 **0**。2026-10-09 复现同一条漂移：
198 行里 190 行标 `online`，`bridge-monitor.sh` 报「桥接账号 总数/在线: 198 / 0」。自查用这条：

```bash
# 连接参数取自 .env：DB_HOST/DB_PORT/POSTGRES_USER/USER_DB_NAME（口令只走 PGPASSWORD，不落文档）
PGPASSWORD="$POSTGRES_PASSWORD" psql -h 127.0.0.1 -p "${USER_POSTGRES_HOST_PORT:-8202}" \
  -U "${POSTGRES_USER:-admin}" -d "${USER_DB_NAME:-user_db}" -c \
  "SELECT count(*) AS total,
          count(*) FILTER (WHERE status='online') AS sticky_online,
          count(*) FILTER (WHERE status <> 'offline' AND last_sync_at IS NOT NULL
                             AND now() - last_sync_at < interval '30 second') AS real_online
   FROM bridge_accounts;"
```

所以任何"在线数"都必须按 `status <> 'offline' AND last_sync_at IS NOT NULL
AND now() - last_sync_at < grace` 算，grace 取 `config_params` 的 `bridge/online_grace_window`
（裸数字按秒，种子缺省 30，见 `internal/service/config_param_seeds.go:71-73`；判定串本身
`internal/repository/bridge_online_predicate.go:15`）。

这一条现在有四个读数点，全部同源，对不上就是 bug：
- 服务端渠道总览计数（`repository/channel_overview.go:125` `CountBridgeOnline`）
- 主动触达选号（`repository/proactive_reach_repo.go:70` `FindActiveAccountID`，挑中不可达账号等于发一条永不投递的出站行）
- 离线回扫报告（`service/bridge_offline_replay.go:291` 的 `[BridgeReplay] 回扫完成: … online=… offline=… grace=…s`，日志里带 grace 是为了能和另两处对账）
- 巡检脚本（`bridge-monitor.sh` 的「桥接账号 总数/在线」，窗口同样现读配置）

三处 Go 侧共用一条判定串（`repository/bridge_online_predicate.go`），改它会让四个用例一起红；
补投门另有一道 per-account 探针（活 SSE 订阅 OR 窗内同步过），所以"报告说在线"从来不是投递的前置条件——
坏的是读数，不是投递。
`unreachable_channels` 一格不等于"没人连 SSE"：轮询式下发的账号是靠 `last_sync_at` 落在宽限窗内才算在线的。

---

## 1. 故障一：服务起不来（user-server / 推理栈）

### 1.1 现象
- `curl http://127.0.0.1:8204/healthz` 连接拒绝（进程没起）；注意 `/health` **进程活着就回 200**，
  依赖掉线时它照样 200，真话在 body：`{"code":50301,"message":"service degraded","data":{"status":"degraded",...}}`
  （本机现测：LLM/Embedding 掉线时 HTTP 码仍是 200）。判"起没起"用 `/healthz`，判就绪用 `/readyz`（掉线回 503）
- 页面 502 / 无法登录
- `make dev` 报错退出

### 1.2 根因（按概率排序）
1. `.env` 缺失或密钥不合法（user-server 的 JWT 密钥读的是 `USER_JWT_SECRET`，回落 `JWT_SECRET`，
   少于 32 字符启动即 panic，`internal/pkg/utils/jwt.go:62-79`；`PLATFORM_JWT_SECRET` 是 platform-server 的键，
   本仓 user-server 代码里没有任何读取点）
2. 端口被占用（8204 / 8202 / 8203 / 8207-8209）
3. Go 依赖未下载 / 编译错误
4. 配置文件 `config.yaml` 语法错误

### 1.3 排查步骤
```bash
# 1. 端口占用
lsof -i :8204 -i :8202 -i :8203 -i :8207 -i :8208 -i :8209

# 2. 查看进程
ps aux | grep -E "user-server|air|llama-server" | grep -v grep

# 3. 检查 .env 是否存在、密钥长度（两个键任一即可，USER_JWT_SECRET 优先）
[ -f .env ] && echo ".env 存在" || echo ".env 缺失 → cp .env-example .env"
awk -F= '/^(USER_)?JWT_SECRET=/ {print $1": "length($2)" 字符"}' .env

# 4. 查看服务日志。当前 dev 形态是容器（`docker ps` 现测 8204 由 mtk-user-server-dev 监听）：
docker logs --tail 100 mtk-user-server-dev
# systemd 部署时才有下面这条；仓内不发布 unit 文件（`find deploy -name "*.service"` 零命中，deploy/ 只有 helm），
# unit 名以站点自己的部署为准。宿主机 air（`make dev`）的进程日志看 user-server/logs/user-server.log
journalctl -u <站点 unit 名> -n 100 --no-pager
```

### 1.4 恢复命令
```bash
# 方式 A：宿主机热更新（air，工作目录 user-server/）
cd user-server && air          # 或仓库根 make dev

# 方式 B：systemd 生产部署（unit 名按站点；仓内不提供 unit）
sudo systemctl restart <站点 unit 名>
sudo systemctl status <站点 unit 名> --no-pager

# 方式 C：Docker 数据层 + 后端容器（当前唯一 dev 形态，Makefile 里没有 docker-up 这个目标）
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d
docker compose -f docker-compose.yml -f docker-compose.dev.yml ps
# 只重启后端容器：
docker compose -f docker-compose.yml -f docker-compose.dev.yml restart user-server-dev

# 数据层单独拉起（若 PG/Redis 未起）
make db-up
```

### 1.5 责任人
应用运维工程师（On-call A 角）；首次 15 分钟内响应。

---

## 2. 故障二：数据库断连（PostgreSQL / Redis）

### 2.1 现象
- 接口大面积 5xx，日志报 `connection refused`。连接串是 GORM/postgres（底层 pgx，DSN 见
  `user-server/internal/pkg/db/db.go:51`），所以实形是 ``failed to connect to `host=... user=admin database=user_db`: ...``；
  `pq:` 前缀只出现在唯一键冲突那类错误码判定里（`internal/repository/db_util.go` 用 `github.com/lib/pq` 读 23505），
  别把 `pq: could not connect` 当连接失败的检索词，会漏
- 桥接 uplink/ack 写入失败
- 登录超时

### 2.2 根因
1. 容器崩溃或 OOM（内存不足被杀）
2. 磁盘满导致 PG 无法写 WAL（见故障六）
3. 连接池耗尽（`database.pool.max_open_conns` 过小；缺省 200，`config/server.go:78-83`；
   配置里写成 0 时按 `db.go:97-99` 回落成 20，比缺省低一个量级）
4. Redis 密码变更后 `.env` 未同步

### 2.3 排查步骤
```bash
# 1. 容器状态
make db-ps

# 2. 容器日志（OOM / 报错）
make db-logs

# 3. PG 连通性：/health 的 HTTP 码判不了依赖（降级仍 200），读 body 里那一格
curl -s http://127.0.0.1:8204/health | python3 -c \
  'import json,sys; d=json.load(sys.stdin); print(d["data"]["checks"]["database"])'
# 期望 {"status":"ok","error":""}；down 时 error 带真实原因。就绪口 /readyz 掉线时回 503 + code 50303

# 4. 连接数（连接池是否耗尽）。容器里 PG 监听 8202 且没有 unix socket 目录，
#    裸 `psql -U admin -d user_db` 现测报 `connection to server on socket "/var/run/postgresql/.s.PGSQL.5432"`，
#    必须显式给 -h/-p
docker compose -f docker-compose.yml exec -T mtk-postgres \
  psql -h 127.0.0.1 -p 8202 -U admin -d user_db -c \
  "SELECT count(*), state FROM pg_stat_activity GROUP BY state;"
```

### 2.4 恢复命令
```bash
# 1. 重启数据层（会连带打断容器里的 user-server-dev 连接，恢复后按 §1.4 方式 C 确认后端在不在）
make db-down && make db-up
sleep 5

# 2. 验证（同 §2.3 第 3 条，读 body，不看状态码）
curl -s http://127.0.0.1:8204/readyz -w "\n[%{http_code}]\n"   # 就绪=200；503 时 data.reason 写明哪个依赖

# 3. 若连接池耗尽，调 user-server/config.yaml（键在 database.pool 下，缩进两级）：
#   database:
#     pool:
#       max_open_conns: 50
#       max_idle_conns: 10
#    读点 config/server.go:59-71（DatabaseConfig.Pool），改完重启 user-server（见 §1.4）

# 4. Redis 密码校验（.env 与容器一致）。容器里 Redis 监听 8203，裸 redis-cli 现测连 6379 被拒
docker compose -f docker-compose.yml exec -T mtk-redis \
  redis-cli -p 8203 -a "$REDIS_PASSWORD" ping   # 期望 PONG（AUTH 是必开的，见 compose 健康检查）
```

### 2.5 责任人
DBA / 运维工程师；数据库故障 P1 级 30 分钟恢复目标。

---

## 3. 故障三：LLM 不可用（推理栈掉线）

### 3.1 现象
- AI 回复超时 / 报错，桥接自动回复停滞
- 日志报 LLM 连接失败或槽位打满
- 权威读数不是 `:8207`：LLM 提供商的真相源在数据库表 `llm_providers` / `llm_routing_rules`，启动时
  `LoadProvidersFromDB` 覆盖 `user-server/config.yaml` 的 `inference.llm` 那一段（该文件 `:88-90` 自己写明了这条），
  所以「`:8207` 是否 200」只反映 llama.cpp 那条腿在不在。判 LLM 是否可用看
  `curl -s http://127.0.0.1:8204/readyz`（现测回 503 + `{"code":50305,"data":{"reason":"inference: no healthy LLM provider"}}`）
  或 `/health` 的 `data.checks.inference`（`internal/router/health.go:25-39` 遍历提供商健康表，任一 up 即 up）
- 本机当前档位（照抄前先确认）：`config.yaml:107-113` 注着「本地 llama-server(8207) 已下线省资源，LLM 统一走
  freeapi 网关 `http://127.0.0.1:8787/v1`」，Embedding/Rerank 仍指 8208/8209（`:97`、`:103`）

### 3.2 根因
1. llama-server 进程崩溃或内存不足被 OOM Killer 杀掉
2. 模型文件缺失 / 路径错误
3. 上下文槽位打满（并发过高）
4. GPU / CPU 资源被其他进程抢占

### 3.3 排查步骤
```bash
# 1. 端点健康（这三个是 llama.cpp 三件套的宿主机进程；:8204 是 user-server 本身，不是依赖）
for p in 8207 8208 8209; do \
  echo "$p: $(curl -s -o /dev/null -w '%{http_code}' --max-time 3 http://127.0.0.1:$p/health)"; \
done

# 2. 生效的 LLM 提供商（DB 覆盖 config.yaml，判"现在打哪儿"以这里为准）
PGPASSWORD="$POSTGRES_PASSWORD" psql -h 127.0.0.1 -p "${USER_POSTGRES_HOST_PORT:-8202}" \
  -U "${POSTGRES_USER:-admin}" -d "${USER_DB_NAME:-user_db}" -c \
  "SELECT sort_order, name, model, base_url, enabled FROM llm_providers ORDER BY sort_order;"

# 3. 进程
make inference-host-ps

# 4. 日志（LLM 推理日志，tail 观察报错）
make inference-host-logs

# 5. 槽位占用。注意 `ENABLE_METRICS` 缺省是 false（scripts/inference-host/env.sh:122、README §9.5），
#    没显式置 true 时 /metrics 根本没开；仓内给的替代探针是 /health 的 JSON（同节 :216）：
curl -s http://127.0.0.1:8207/health | python3 -m json.tool
# 本机现测 8207 无监听 ⇒ 上面这条回空；要读 slots_*（llama.cpp 的 Prometheus 名）必须先
# ENABLE_METRICS=true 重启推理栈——这一步未实测（要把实例的推理栈拉起来，会动开发机），口径见 README §9.5
```

### 3.4 恢复命令
```bash
# 1. 重启推理栈
make inference-host-down && make inference-host-up
sleep 5

# 2. 预热（避免首请求慢）
make inference-host-warmup

# 3. 端到端 smoke test
make inference-host-test

# 4. 降级策略：FeatureFlag 关并行/流式/一层（见 AI_AGENT_PERF_DEPLOY.md 4.1）。
#    开关只有这六个，名字→env 名由 featureflag.EnvNameOf 从旗标名推（flag.go:166-180）：
#      parallel / stream / layer1 / fallback_chain / debug_log（缺省 false）+ sse_bridge（缺省 true）
#    ⇒ env 名 FF_PARALLEL / FF_STREAM / FF_LAYER1 / FF_FALLBACK_CHAIN / FF_DEBUG_LOG / FF_SSE_BRIDGE；
#    值按 strconv.ParseBool 认（"0"/"false" 关，"1"/"true" 开，`flag.go:196-215`）。
#    落点：写进 .env（宿主机 air 由 .air.toml:8/13 source ../.env）或 compose 的 environment:，
#    **然后重启进程**。仓内没有配置热加载：`viper.WatchConfig` 零命中、SIGHUP 无 handler
#    （`grep -rn "SIGHUP\|signal.Notify" user-server/internal user-server/cmd` 只命中注释与 bridge-mock），
#    `systemctl reload` 因此什么也不会发生；`flag.go:92-107` 那个 5s poller 只是每 5s 重读**同一份进程 env**
#    （`os.LookupEnv`），外部改不到它——所以判"开关为什么没生效"，先确认进程是不是带着新 env 起来的。
export FF_PARALLEL=0 FF_STREAM=0 FF_LAYER1=0
make dev                     # 宿主机 air 形态：Ctrl+C 停掉再起，env 才会重新注入
# 容器形态（模式 C）：改了 .env 要 up -d 重建，restart 沿用旧容器 env（此步未实测，会打断实例）
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d user-server-dev
# 验证生效：capabilities 平铺 JSON 里的 sse_enabled 是唯一能远程读的一格
curl -s http://127.0.0.1:8204/api/bridge/capabilities -H "X-Bridge-Token: <TOKEN>"
```

### 3.5 责任人
AI 平台工程师；LLM 故障 P1 级。

---

## 4. 故障四：桥接全渠道掉线（HiveBridge 失联）

### 4.1 现象
- 所有渠道（抖音/小红书/TikTok/闲鱼）扩展图标变灰，无自动回复
- user-server 日志无 uplink/outbox 心跳
- popup 健康度面板显示熔断（circuit-breaker open）

### 4.2 根因
1. 桥接 Token 失效 / 被吊销 / 压根没填（`X-Bridge-Token` 闸门见 `user-server/internal/middleware/bridge_ingress_guard.go:51-86`）。
   401 有两句文案，但**这两句不能当成"头有没有发出去"的证据**：`extractBridgeToken`（同文件 `:38-49`）
   对空值头做的是 `if v := c.GetHeader(...); v != ""` 的判空再 `TrimSpace`，
   所以「没发这个头」和「发了一个空值/纯空格的头」在服务端是同一条路径，现测两条形都是
   `{"code":"UNAUTHORIZED_2001","message":"缺少 X-Bridge-Token"}`（后者实测见 §4.3 第 1 条）。
   判据只能是：`缺少 X-Bridge-Token` ⇒ 客户端这枚头是空的（扩展没配 Token / 配了空串 / 被反代吃掉了这枚头，
   三者要按顺序排），`bridge token 无效` ⇒ 头确实发出去了且非空，值不对（被轮换过 / 拷错 / 只更新了 `..._PREV`）。
   还有第三种：服务端**没配任何凭证**时不是 401 而是 503
   `桥接通道需配置 BRIDGE_INGEST_TOKEN（或显式 BRIDGE_INGEST_AUTH=off 以接受无鉴权模式）`（`:55-62`，fail-closed）
2. user-server `/api/bridge/*` 路由不可达（未开 / 被反代拦截）
3. 扩展侧死开关（Dead Man's Switch）触发，自动停摆
4. 账号被平台风控（高频巡检触发）

### 4.3 排查步骤
```bash
# 0. 先取当前生效凭证（管理面在 user-server 自己这边，不是"平台端"）：
#    GET/POST /api/bridge/token/{status,reset}，要 JWT + 管理员角色（router.go:368-369）
#    应急排查时直接从库里读现值（值不落文档）：
PGPASSWORD="$POSTGRES_PASSWORD" psql -h 127.0.0.1 -p "${USER_POSTGRES_HOST_PORT:-8202}" \
  -U "${POSTGRES_USER:-admin}" -d "${USER_DB_NAME:-user_db}" -t -A -c \
  "SELECT value FROM system_config_kv WHERE key='bridge_ingest_token'"

# 1. 桥接接口可达性。闸门只认 X-Bridge-Token 或 ?bridge_token=（发 Authorization: Bearer 一样算没带，
#    实测回"缺少 X-Bridge-Token"）；outbox 另要求 channel + account_id。
#    200=通 / 400=通但少参或渠道名不在五渠道 / 401=凭证问题（见 §4.2 的两句判据）/
#    503=服务端没配凭证（fail-closed）/ 000=进程没起来
curl -s -o /dev/null -w "%{http_code}\n" -H "X-Bridge-Token: <TOKEN>" \
  "http://127.0.0.1:8204/api/bridge/outbox?channel=douyin&account_id=<ACCOUNT_ID>"
# 1b. 分辨"空值头"和"没头"这两件事在响应上做不到，只能在请求侧钉死：下面三条应全部回 401 且文案相同，
#     第 4 条（带值）才回 200。若第 4 条也 401，问题在值；若第 1、2 条里有任一变成 200，说明你打到的是
#     BRIDGE_INGEST_AUTH=off 的实例。
curl -s -w "[%{http_code}]\n" "http://127.0.0.1:8204/api/bridge/capabilities"                       # 不带
curl -s -w "[%{http_code}]\n" -H "X-Bridge-Token: " "http://127.0.0.1:8204/api/bridge/capabilities"  # 空值
curl -s -w "[%{http_code}]\n" -H "Authorization: Bearer <TOKEN>" "http://127.0.0.1:8204/api/bridge/capabilities"
curl -s -w "[%{http_code}]\n" -H "X-Bridge-Token: <TOKEN>" "http://127.0.0.1:8204/api/bridge/capabilities"

# 2. 查看桥接相关日志（当前 dev 形态是容器；仓内不发布 systemd unit，journalctl 只在站点自建 unit 时可用）
docker logs mtk-user-server-dev 2>&1 | grep -iE "bridge|uplink|outbox|ack" | tail -50
#    宿主机 air 形态改读文件：grep -iE "bridge|uplink|outbox|ack" user-server/logs/user-server.log | tail -50

# 3. 检查扩展 popup 健康度面板：熔断状态 / 延迟 P50/P95 / 错码分布
# 4. 检查浏览器控制台：chrome://extensions → 背景页 Console
#    SSE 建连被拒时，控制台现在带服务端回的原因（`SSE 连接失败: HTTP 401 — bridge token 无效`），
#    不再只剩一个状态码数字。见 user-web/bridge/src/core/sse-fetch-client.js 的 buildSSEError（:129、:236）。
#    4xx（408/429 除外）判为不可重试：循环会打一条
#    `SSE 被服务端拒绝（不可重试），降为 60s 慢重连`（downlink.js:1003，60s 常量在同文件 :843），
#    此后每 60s 才再试一次——这是预期降档，不是"卡住不动"，别按重连失败去重启服务。
#    在选项页把 Token 改对后不需要刷新页面，下一个 60s 试探自己会接上。

# 5. Token 被轮换过：POST /api/bridge/token/reset 后旧值进灰度窗口。灰度候选是**库里两枚键优先**
#    （`system_config_kv` 的 `bridge_ingest_token` + `bridge_ingest_token_prev`，见 bridge_ingress_guard.go:16-36），
#    env 的 `BRIDGE_INGEST_TOKEN` / `BRIDGE_INGEST_TOKEN_PREV` 只在库里读不到时才兜底——
#    所以"我在 .env 里配了 token 为什么还是 401"的标准答案是：库里那枚生效了，env 那份根本没被读。
#    扩展那边必须换成 reset 返回的新值，别指望旧值长期可用（灰度窗口只救回滚，不救慢客户端）。
```

### 4.4 恢复命令
```bash
# 1. 重置桥接 Token：这一对在 user-server 自己的管理面，不在平台端
#    GET  /api/bridge/token/status      （JWT + 管理员，router.go:368）
#    POST /api/bridge/token/reset       （同上，router.go:369；返回体只印新 token 一次）
curl -s -X POST -H "Authorization: Bearer <管理员 JWT>" http://127.0.0.1:8204/api/bridge/token/reset
# 2. 扩展 popup → 重新配置 Token → 保存（config-store.js:86-88 的 chrome.storage.onChanged 监听生效，
#    不必重装；正在慢重连的 SSE 腿按 §4.3 第 4 条自己接上）
# 3. 若熔断器已 open：等冷却窗口（core/circuit-breaker.js:9 缺省 openDurationMs=30000，半开需 1 次成功），
#    或点 popup「紧急停止」（src/popup/emergency-stop.js）→ 重新「启动」
# 4. 若平台风控：调低巡检频率（src/core/constants.js:79-93 PATROL_DEFAULTS：intervalMs 30000 + jitterMs 30000
#    ⇒ 轮间隔 30-60s；maxConversationsPerRound 6；conversationCooldownMs 120000）
```

### 4.5 责任人
桥接运维工程师；全渠道掉线 P1 级 2 小时恢复目标。

---

## 5. 故障五：内存暴涨

### 5.1 现象
- 宿主机内存使用率 > 90%，`free -h` 显示接近耗尽
- 扩展侧页面卡顿、OOM（`oom-patrol` 相关报错）
- llama-server / user-server 被 OOM Killer 杀掉

### 5.2 根因
1. LLM 并发槽位过多 + 上下文窗口过大
2. 扩展侧 RateLimiter LRU 缓存 / `_pendingAck` 堆积（TTL 未生效）
3. 推理栈 dev/prod 模型档位与机器内存不匹配
4. 连接池 / 缓存无上限

### 5.3 排查步骤
```bash
# 1. 内存占用 Top 进程
ps aux --sort=-%mem | head -10

# 2. 推理栈模型档位（确认是否误用 prod 档）
cat scripts/inference-host/models.env

# 3. 扩展侧：popup 健康度面板查看请求堆积；后台日志 grep pendingAck
# 4. 检查 OOM 记录
dmesg | grep -i "out of memory" | tail -10
```

### 5.4 恢复命令
```bash
# 1. 立即回收：重启推理栈（释放 llama 内存）
make inference-host-down && make inference-host-up

# 2. 切换更小模型档（dev：Qwen2.5-3B Q4_K_M）
HIVEMTK_PROFILE=dev make inference-host-models
HIVEMTK_PROFILE=dev make inference-host-up

# 3. 收紧 LLM 参数（config.yaml）
#   inference.llm.max_tokens: 512
#   inference.llm.context_size: 2048

# 4. 扩展侧：等待 _pendingAck TTL（24h）自清理；必要时卸载重装扩展清空状态
```

### 5.5 责任人
运维工程师 + 前端工程师（扩展侧）；P2 级 4 小时。

---

## 6. 故障六：磁盘满

### 6.1 现象
- `df -h` 显示 `/` 使用率 100%
- PG 写 WAL 失败 → 数据库只读 / 拒绝连接
- 备份失败、日志无法写入

### 6.2 根因
1. 日志文件无限增长（`$HOME/.hivemtk/runtime/*.log`）
2. PG WAL 堆积（未开启归档清理）
3. 模型文件 / 备份文件占用
4. 扩展 `dist/` 构建产物累积

### 6.3 排查步骤
```bash
# 1. 磁盘使用率
df -h

# 2. 大目录定位
du -sh $HOME/.hivemtk/runtime user-server user-web/bridge/dist 2>/dev/null | sort -h

# 3. 日志大小
ls -lh $HOME/.hivemtk/runtime/*.log 2>/dev/null

# 4. PG 数据目录
docker compose -f docker-compose.yml exec -T mtk-postgres df -h /var/lib/postgresql
```

### 6.4 恢复命令
```bash
# 1. 清理旧日志（保留最近 7 天）
find $HOME/.hivemtk/runtime -name "*.log" -mtime +7 -delete

# 2. 清理旧备份（保留最近 30 天）
find . -maxdepth 1 -name "backup_*.sql" -mtime +30 -delete

# 3. 清理扩展构建产物（按需）
rm -rf user-web/bridge/dist user-web/bridge/dist-zip

# 4. 日志轮转（建议配置 logrotate，示例 /etc/logrotate.d/hivemtk）：
#   $HOME/.hivemtk/runtime/*.log {
#       daily
#       rotate 7
#       compress
#       missingok
#   }
```

### 6.5 责任人
系统运维工程师；磁盘满 P1 级 1 小时恢复目标。

---

## 7. 故障升级矩阵

| 故障 | 级别 | 首次响应 | 恢复目标 | 升级路径 |
|------|------|---------|---------|---------|
| 服务起不来 | P1 | 15 min | 1 h | 运维 → 后端组 |
| 数据库断连 | P1 | 15 min | 30 min | 运维 → DBA |
| LLM 不可用 | P1 | 15 min | 1 h | AI 平台工程师 |
| 桥接全渠道掉线 | P1 | 15 min | 2 h | 桥接运维 → 前端组 |
| 内存暴涨 | P2 | 30 min | 4 h | 运维 → 前端组 |
| 磁盘满 | P1 | 15 min | 1 h | 系统运维 |

> 桥接全渠道掉线判定为 P1：直接影响自动获客/自动回复核心链路。其余参考 [SLA/SLO](SLA_SLO.md) 约定。

---

## 8. 运维检查清单（建议频率）

| 频率 | 动作 | 命令 |
|------|------|------|
| 每小时 | 端点巡检 | `make inference-host-status` |
| 每小时 | LLM 槽位水位 | `curl -s :8207/metrics \| grep slots` |
| 每日 | 数据库备份 | `make db-backup` |
| 每日 | 磁盘水位 | `df -h` |
| 每日 | 桥接日志异常扫描 | `grep -iE "error\|fail\|timeout" $HOME/.hivemtk/runtime/*.log` |
| 每周 | 备份恢复演练 | 见 [DR_RECOVERY.md](DR_RECOVERY.md) |
| 每周 | 依赖漏洞扫描 | Dependabot PR 合并 |

---

**版本历史**
- v1.0 (2026-08-15)：初版，覆盖 6 类高频故障（起不来/断连/LLM 掉线/桥接全掉/内存/磁盘），命令基于 Makefile 与宿主机推理栈实测。
