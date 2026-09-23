# HiveMtk 部署运维手册

> **定位**：回答两个问题——**如何安装**、**装好之后日常怎么运维**。
> **事实基准**：本文所有端口、命令、环境变量均来自仓库源码（`user-server/internal/config/ports.go`、`Makefile`、`.env-example`、`docker-compose.yml`、`scripts/inference-host/`），未做任何推测性描述。
> **架构基线**：2026-08-17 推理栈宿主机化重构之后。

---

## 目录

1. [部署架构总览](#一部署架构总览)
2. [硬件需求](#二硬件需求)
3. [端口分配](#三端口分配)
4. [安装前准备](#四安装前准备)
5. [标准安装流程](#五标准安装流程)
6. [环境变量与配置](#六环境变量与配置)
7. [对外发布模式](#七对外发布模式)
8. [健康检查端点](#八健康检查端点)
9. [日常运维操作](#九日常运维操作)
10. [备份与恢复](#十备份与恢复)
11. [升级与回滚](#十一升级与回滚)

---

## 一、部署架构总览

**核心结论先讲清楚：本项目的 Docker 只承载数据层（PostgreSQL + Redis），应用与推理全部跑在宿主机。**

```
┌─────────────────────── 宿主机 ───────────────────────────┐
│                                                          │
│  应用层                                                   │
│  ├─ user-server 二进制        默认监听 0.0.0.0:8204        │
│  │   （make user-build 产物，air 热重载用于开发；           │
│  │     SERVER_HOST=127.0.0.1 可把网口收回本机，见 §6.2）    │
│  └─ user-web 静态产物         由 反向代理层 或任意静态服务托管   │
│                                                          │
│  推理层（scripts/inference-host/）                        │
│  ├─ llama-server · LLM       127.0.0.1:8207              │
│  ├─ llama-server · Embedding 127.0.0.1:8208              │
│  └─ llama-server · Rerank    127.0.0.1:8209              │
│                                                          │
│  数据层（Docker）                                          │
│  ├─ mtk-postgres (pgvector)  127.0.0.1:8202              │
│  └─ mtk-redis                127.0.0.1:8203              │
└──────────────────────────────────────────────────────────┘
```

为什么这样设计（摘自 `.env-example` 头部注释）：

- Docker 仅提供数据层，PG/Redis 端口绑定 `127.0.0.1`，不暴露公网；
- 推理走宿主机 llama.cpp（`scripts/inference-host/`），避免容器内 GPU/内存调度的额外复杂度；
- user-server 用二进制（生产）或 air 热重载（开发）运行；
- LLM 提供商配置不写死在文件里，而是通过后台「LLM 路由」页面写入数据库表 `llm_providers`，user-server 启动时经 `LoadProvidersFromDB` 加载；
- 合规基线：API Key 不落配置文件，`cloud_providers` 必须为空（禁止云端回落）。

## 二、硬件需求

| 资源 | 最低 | 推荐 | 说明 |
|------|------|------|------|
| CPU | 4 核 | 8 核 | x86_64 或 Apple Silicon（MLX 分支仅限 Mac） |
| 内存 | 8 GB | 16 GB | 三个 llama-server 进程常驻是内存大头 |
| 磁盘 | 60 GB SSD | 200 GB SSD | 含模型目录，建议给模型单独预留 ≥10 GB |
| GPU | 不需要 | 可选 | llama.cpp 无 GPU 也能跑，仅影响生成速度 |

> 数据层容器自身资源受限：docker-compose.yml 中 mtk-postgres 限制 768M、mtk-redis 限制 512M，不是资源消耗主体。

## 三、端口分配

**唯一权威来源：[ports.go](../user-server/internal/config/ports.go)。网上任何资料与本表冲突时，以代码为准。**

| 服务 | 端口 | 绑定 | 说明 |
|------|------|------|------|
| user-server API | **8204** | `0.0.0.0`（默认，全网卡） | 主 API（HTTP + WebSocket 同端口）。只给本机/内网代理用时设 `SERVER_HOST=127.0.0.1` 收回本机，见 §6.2 |
| PostgreSQL（Docker） | **8202** | 127.0.0.1 | 容器映射自 5432，宿主机侧永远是 8202 |
| Redis（Docker） | **8203** | 127.0.0.1 | 容器映射自 6379，宿主机侧永远是 8203 |
| platform-server | 8205 | `0.0.0.0`（默认，全网卡） | 可选组件，离线部署默认不启动。绑定地址走它自己的 `config.yaml` `server.host`（`internal/config/config_viper.go:188` 的 viper 默认值），**不读 `SERVER_HOST`** |
| Chromium CDP | 8206 | 内部 | 浏览器自动化调试口 |
| LLM (llama-server) | **8207** | 127.0.0.1 | 主对话模型 |
| Embedding (llama-server) | **8208** | 127.0.0.1 | bge-m3，1024 维 |
| Rerank (llama-server) | **8209** | 127.0.0.1 | bge-reranker-v2-m3 |
| PostgreSQL（本地开发直装） | 8232 | 127.0.0.1 | 仅 `DB_PORT` 未被 .env 覆盖时的开发默认值 |

> ⚠️ 常见误解纠正：**不存在"WebSocket 独立端口 8205"**。8204 同时承载 HTTP 与 WS；8205 是平台端端口。

## 四、安装前准备

### 4.1 软件依赖

| 软件 | 版本要求 | 用途 | 验证命令 |
|------|---------|------|---------|
| Go | **1.25+**（go.mod 声明 1.25.0） | 编译 user-server | `go version` |
| Node.js | 18 LTS+ | 构建 user-web / embed-sdk | `node -v` |
| Docker + Compose | 稳定版 | 数据层容器 | `docker compose version` |
| psql 客户端 | 14+ | 执行初始化 SQL | `psql --version` |
| git | 任意近期版本 | 拉取代码 | `git --version` |
| make | 系统自带 | 所有运维入口 | `make --version` |

```bash
# Ubuntu 22.04 / Debian 12 参考安装
apt-get update && apt-get install -y \
  curl wget git make postgresql-client \
# Go 1.25 与 Node 18 请按官方渠道安装，发行版仓库版本可能过旧
```

### 4.2 获取代码

```bash
git clone <你的仓库地址> hivemtk
cd hivemtk
```

## 五、标准安装流程

以下六步即 `make help` 体系覆盖的完整链路。**每一步都有对应 Makefile 目标，不要手拼 docker 命令。**

### 第 1 步：生成环境配置

```bash
make install
```

行为：复制 `.env-example` 为 `.env` 并提示你修改敏感字段。**必须**手工编辑 `.env`：

```bash
# 三把必改（全部要求强随机值，可用 openssl rand -hex 32 生成）
POSTGRES_PASSWORD=
REDIS_PASSWORD=
JWT_SECRET=               # ≥32 字符，不足启动时直接 panic
FIELD_ENCRYPTION_KEY=     # 第四把：只服务"加密落库"这条路径。启动不校验它，
                          # 但没配的话写 SMTP 凭据时会 fail-closed 直接报错
                          # （internal/email/service/email_smtp.go:24），
                          # GEO 平台凭据则降级明文存储并打告警
PLATFORM_ADMIN_PASSWORD=  # 仅开启平台集成（PLATFORM_ENABLED=true）时需要
MERCHANT_API_SECRET=      # 仅开启平台集成时需要：出站请求的 HMAC 签名密钥（internal/platform/client.go:79）
```

> 历史版本这里还要求 `PLATFORM_LICENSE_SECRET`（商户授权签名）。该键在 user-server 与
> platform-server 两侧代码里都已无任何读取点（2026-09-21 逐仓 grep 复核），授权流程整体下线后不再需要设置。

### 第 2 步：启动数据层

```bash
make db-up        # 拉起 mtk-postgres + mtk-redis（缺密码会启动失败）
make db-ps        # 确认两个容器 healthy
```

### 第 3 步：初始化数据库

```bash
PGPASSWORD=<你的POSTGRES_PASSWORD> psql -h 127.0.0.1 -p 8202 -U admin -d user_db \
  -f migrations/init-user-db.sql
```

该脚本做的事：启用 `vector` / `uuid-ossp` 扩展，创建 `knowledge_embeddings`（1024 维）、`rag_products`、`knowledge_documents` 等核心表。**其余业务表由 user-server 启动时的 GORM AutoMigrate 自动建齐，无需手动跑全量迁移。** 编号迁移脚本（`migrations/001~055_*.sql`)供需要精确复现历史结构时按序手工执行。

### 第 4 步：下载模型并启动推理栈

```bash
make inference-host-install   # 编译/安装 llama.cpp，写入 env.sh
make inference-host-models    # 按 .env 中 profile 下载 LLM/Embedding/Rerank 三个 GGUF
make inference-host-up        # 拉起 8207/8208/8209 三个 llama-server
make inference-host-status    # 确认三个端口就绪
make inference-host-warmup    # 可选：预热，消除首请求冷启动延迟
```

模型档位由 `.env` 的 `HIVEMTK_PROFILE` 控制（dev/prod 等），调参只改 `.env`，不改 `scripts/inference-host/models.env`。

### 第 5 步：构建并启动后端与前端

```bash
make user-build    # CGO_ENABLED=0 go build → user-server/bin/user-server
make web-build     # cd user-web && npm install && npm run build
make sdk-build     # 可选：构建 embed-sdk 网页挂件

# 启动后端（前台运行便于首次观察日志）
cd user-server && ./bin/user-server
```

前端构建产物（`user-web/dist`）部署到 反向代理层 或任意静态服务器即可，user-server 当前配置中不含静态托管段。

### 第 6 步：验证

```bash
curl http://127.0.0.1:8204/healthz      # 存活探针，期望 HTTP 200
curl http://127.0.0.1:8204/readyz       # 就绪探针
curl http://127.0.0.1:8204/health       # 含数据层依赖状态
curl http://127.0.0.1:8208/v1/models    # Embedding 服务模型清单
```

四个请求都通，即完成最小可运行部署。浏览器打开前端地址登录后台。

## 六、环境变量与配置

配置读取优先级：**环境变量 > config.yaml > 代码内默认值**。`.env` 由 `make install` 生成、进程启动前 source 注入。

### 6.1 必须正确设置

| 变量 | 要求 | 缺失/非法后果 |
|------|------|--------------|
| `POSTGRES_PASSWORD` | 强密码 | mtk-postgres 容器拒绝启动 |
| `REDIS_PASSWORD` | 强密码 | mtk-redis 容器拒绝启动 |
| `JWT_SECRET` | ≥32 字符 | user-server 启动 panic（测试专用短密钥仅在 test 模式放行）。代码先读 **`USER_JWT_SECRET`**，为空才回落到 `JWT_SECRET`（`internal/pkg/utils/jwt.go`）；两个都没有或都短于 32 字符即 panic，**不存在硬编码兜底密钥** |
| `FIELD_ENCRYPTION_KEY` | ≥32 字符 | 加密字段功能不可用；**轮换会使既有加密数据失效** |
| `MERCHANT_API_SECRET` | ≥32 字符 | **仅 `PLATFORM_ENABLED=true` 时才是必须**（离线部署默认关态，整条签名链路不装配，此键不参与读取）。开启后缺失/与平台端不一致 ⇒ merchant-api HMAC 签名鉴权失败（user-server 经 config/platform.yaml `secret: "${MERCHANT_API_SECRET}"` 消费） |
| `DB_HOST` / `DB_PORT` | 默认 `127.0.0.1:8202` | 连不上库直接启动失败 |

### 6.2 按需设置

| 变量 | 默认 | 何时需要设 |
|------|------|-----------|
| `PUBLIC_BASE_URL` | 空 | **部署 Telegram/飞书/钉钉等被动回调渠道时必填**。格式 `https://域名`（不带路径、不带尾斜杠），系统会用它注册 Webhook；留空则这些渠道自动降级 polling 模式（仅单实例可用） |
| `PLATFORM_API_HOST` | `http://127.0.0.1:8205` | 平台端不在本机时改为其实际地址。注意实际读取的是 `PLATFORM_API_HOST` 不是 `PLATFORM_API_URL` |
| `PLATFORM_URL` | 空 | 平台地址的**末位回落**：`platform.yaml` 的 `api_url` 为空、`PLATFORM_API_URL` 也为空时才轮到它，再为空则用编译期默认值。平时不需要设；它同时决定启动日志里"平台配置来源"标注成哪一档 |
| `SERVER_HOST` | `0.0.0.0` | **要把 user-server 网口收回本机时设 `127.0.0.1`**（存量实例不换超管口令时的加固路线，见 §6.2 末「换掉已经装好的那台」段）。改了要重启进程才生效；同机不同端口的数据层不受影响。设了它，`user-web/bridge`、真机浏览器扩展等**从另一台机器**打 8204 的用法会全部连不上（这正是"收回"的含义），单人本机部署才设 |
| `CORS_ALLOW_ORIGINS_USER` | 见 .env-example | 前端域名与 API 不同源时，把前端 Origin 加入白名单 |
| `DEEPL_API_KEY` | 空 | 启用低资源语言（ar/th/vi/hi/tr）DeepL 翻译降级时 |
| `QINIU_ACCESS_KEY` / `QINIU_SECRET_KEY` | 空 | 使用七牛云对象存储时 |
| `LLM_*` / `EMBEDDING_*` / `RERANK_*` | 见 .env-example | 控制推理栈下载哪个模型、监听哪个端口 |
| `MAX_JSON_BODY_MB` | `8` | 全局 JSON/表单请求体上限（MB），超限直接 413。只兜"没另设上界的内部口"，比各端点自己的封顶更宽时不参与；迁移期要灌大 payload 时**显式设 0 关闭**（负数同义），别改成改代码 |
| `SEED_PASSWORD` | `Seed@123456` | seed 写进 `system_users` 的 admin 与演示坐席统一口令，优先级 `SEED_PASSWORD` > `ADMIN_PASSWORD` > 该默认值（`cmd/seed/seed_users.go:32`）。**只有跑 `scripts/bootstrap.sh` 或 `go run ./cmd/seed` 才会写入它**——`make install` / `make dev` 不 seed（docker-compose 里的 `admin` 是数据库用户，不是后台账号）。这个默认值随开源仓库公开（文档与 FAQ 种子数据里都写着），**装到别人连得上的机器上就必须显式设**；bootstrap 用默认值启动时会打 warn（本文件下方那条"换掉已经装好的那台"注记解释了为什么**存量实例重跑无效**），直接 `go run ./cmd/seed` 则不会。取值走 `SEED_PASSWORD="…"` 注入，勿改成代码常量 |

以下这些键此前**只存在于代码里**（`.env-example` 与本文都没提），列出来是因为每一个都会改变安全姿态或身份口径，运维不知道它们存在比知道更危险。默认值一律取最严/最窄的一侧。

| 变量 | 默认 | 作用与设置后果 |
|------|------|-----------|
| `INGRESS_API_KEY` | 空 | 统一入站口 `POST /api/chat/ingress` 的 `X-Ingress-Secret` 密钥。**空即 503 拒绝**（fail-closed），要用该入口必须配。503 文案一度误印 `INGRESS_SECRET`（全仓无人读该键），照提示配置会永远 503 —— 以代码读取的 `INGRESS_API_KEY` 为准 |
| `BRIDGE_INGEST_TOKEN` / `BRIDGE_INGEST_TOKEN_PREV` | 空 | 桥接 WS 入站鉴权 token。DB 里的 `bridge_ingest_token` 优先，DB 无值才读 env；`_PREV` 供轮换期同时接受旧 token。两者都缺 ⇒ 接口 503 拒绝 |
| `BRIDGE_INGEST_AUTH` | 非 `off` | 只有在上面两个 token 全缺、且该端口**只暴露于可信内网**时才设 `off`，它把入站退回"无鉴权放行"。别拿它当配置缺失的 workaround |
| `ONEID_SALT` | 源码固定串 | OneID 手机号/邮箱哈希的盐。**只能在库里还没有客户之前设定**：换值会让存量 `customers.phone_hash` 与 `unified_id` 全体错位（按哈希查不到人 ⇒ 同一客户被建成第二条记录）。多实例必须同值，否则两台机器给同一手机号算出两个 OneID |
| `BRUTE_FORCE_DISABLED` | 关 | `1`/`true` 关闭登录爆破锁定。该判定在**包级变量初始化时求值**，运行中改环境变量无效，只能改完重启 |
| `ALLOW_INSECURE_WEBHOOK` | 关 | `true` 时对"渠道账号压根没配密钥"的回调跳过验签（每次跳过打 warn；已配密钥的账号不受该开关影响）。受启动护栏约束：`APP_ENV` 非开发值时进程**直接拒绝启动** |
| `ALLOW_INSECURE_TELEGRAM_WEBHOOK` | 关 | Telegram 专用：`true` 跳过 secret 校验，同样只在联调用 |
| `MARKETING_WEBHOOK_ALLOW_INSECURE` | 关 | 营销流 webhook 动作的 SSRF 闸门（只允许 https + 非内网地址）豁免开关。**只在 `APP_ENV=development`（或 `GIN_MODE=debug`）下生效**；生产设了也不放行，每次豁免打 warn |
| `ALLOW_SELF_RESTART` | 关 | `true` 才允许「系统运维」接口让本进程退出重启 |
| `WS_AGENT_ALLOW_ALL_USERS` | 关 | `true` 放开坐席通知订阅的角色限制（默认仅 admin/manager/staff/customer_service，见 R14-3 的 403 重连循环） |
| `TOOL_PERMISSION_DEFAULT_DENY` | 关 | 工具权限白名单的缺省姿态：**不设＝白名单外放行**，设 `true` 才拒绝。生产建议设 |
| `ORDER_WEBHOOK_NONCE_STRICT` | 关 | 设 `on` 开启商机回调 nonce 严格重放校验 |
| `SMS_ALLOW_NIGHT_SEND` | 关 | `true` 绕开 22:00–08:00（CST）夜间不发短信的限制 |
| `APP_ENV` / `MODE` / `GIN_MODE` | 无 | **开发环境判定**（`config.IsDevelopmentEnv`）：按 `APP_ENV` → `MODE` 取第一个非空值，`dev|development|debug|test|testing|local` 算开发；三者都空时再看 `GIN_MODE=debug`。都不设 ⇒ 按**生产**姿态走，这决定了多把安全闸的强度：`MASTER_KEY` 缺失时生产拒绝启动、`ALLOW_INSECURE_WEBHOOK=true` 时生产拒绝启动、`MARKETING_WEBHOOK_ALLOW_INSECURE` 只在开发姿态下才放行内网 webhook。**别指望"没设就是开发"**——没设恰恰是最严的那一侧 |
| `EMAIL_TRACKING_SECRET` | 空 | 邮件追踪 token 的 HMAC-SHA256 密钥（`internal/service/email_tracking.go`）。**未配置时签发与校验双双 fail-closed**：签发返回错误、校验直接拒。此前它退化成"空密钥自签自验"，任何人按公开的 claim 结构都能算出合法签名 ⇒ 伪造打开/点击事件、伪签他人邮箱的退订。token 有效期 90 天，轮换即让存量追踪链接失效 |
| `EMAIL_UNSUBSCRIBE_SECRET` | 空 | 邮件退订链接 token 的 HMAC-SHA256 密钥（`internal/service/email_unsubscribe.go`）。**未配置时签发与校验双双 fail-closed**：签发返回错误、校验侧拒绝**所有** token（包括 `payload.` 这种空签名——空密钥下 `hmac.Equal(空,空)` 为真，所以"没配密钥"绝不能当成一种校验，否则任何人都能伪签别人的退订链接）。有效期 30 天，轮换即让存量退订链接失效 |
| `MASTER_KEY` | 空 | 凭证盘 AES-256-GCM 主密钥，**≥32 字节**（`internal/secrets/aesgcm.go`）。缺失/过短时 `Ready()` 为 false，加解密降级为明文读写 + WARN；**生产环境（`APP_ENV`/`MODE` 非开发值）装配层据此拒绝启动**。任意路径泄露即整盘作废，建议由 secret manager 注入；改值不会自动重加密存量 |
| `FF_LTC_REACH_GATE` | `off` | 外发审批闸门模式 `off|shadow|block`（`internal/app/reach_gate_wiring.go`）。`shadow` 只留痕不拦，`block` 真拦；写布尔真值（`true`/`1`）一律按 `shadow` 处理并告警——给真人发短信不可撤回，转阻断必须在 env 里写出 `block` 这个词。依赖 `FF_LTC_APPROVAL_GATE` 未接线时**拒绝装门**（没有裁决来源的门只能恒放或恒拒，两种都长得像在拦） |
| `FF_TOOL_PERMISSION_ENFORCE` | `off` | 工具风险判定层 `off|shadow`（`internal/app/permission_wiring.go`）。**这个构建里没有阻断态**：写 `enforce`/`block`/`true` 一律按 `shadow` 挂载并显式告警"它拦不住任何东西"（转阻断排在 P9）。别以为写了 `enforce` 就在拦 |
| `LTC_RECOVERY_WORKER_BATCH` | `20` | 挽回队列单轮处理上限，可用区间 `[1,500]`；非整数或超界 ⇒ 告警并沿用默认（`internal/service/recovery_queue_worker.go`）。前提是 `FF_LTC_RECOVERY_WORKER=enforce` |
| `LTC_RECOVERY_WORKER_INTERVAL` | `5m` | 挽回队列轮询间隔（Go duration 写法，如 `30s`/`5m`）。低于 `30s` 抬到 `30s`，否则一轮没跑完下一轮就起、同一条会被两轮领走 |
| `LTC_RECOVERY_WORKER_BACKOFF` | `24h` | 重试退避基数（同时是无文案项的推后幅度）。小于触达冷却窗口时抬到"冷却窗口 + 余量"，否则每次到期都只换来一次 cooldown 拒绝，白耗一轮 |
| `TOOL_CIRCUIT_BASE_COOLDOWN` | `30s` | 按工具熔断的起始冷却，可用区间 `[1ms,1h]`。非法时长或超界 ⇒ 告警并沿用默认；五项参数各自校验，配错一项不拖累其余（`internal/app/tool_circuit_breaker_wiring.go`） |
| `TOOL_CIRCUIT_MAX_COOLDOWN` | `5m` | 熔断冷却的指数退避上限，可用区间 `[1ms,24h]`。小于 `TOOL_CIRCUIT_BASE_COOLDOWN` 时抬到 base，否则退避被反向夹住 |
| `TOOL_CIRCUIT_BACKOFF_MULTIPLIER` | `2.0` | 每多熔断一次的冷却倍率，可用区间 `[1,100]`；非数字或超界 ⇒ 沿用默认 |
| `TELEGRAM_POLLING_ENABLED` | 未设置 | `1`/`true`/`yes` 强制启用 polling，`0`/`false`/`no` 强制禁用；**未设置则自动判定**：配了 `external.public_base_url` 就注册 webhook 并禁用 polling，没配（内网/本地）自动启用 polling（`internal/service/telegram_polling.go`）。polling 只能单实例跑，多实例部署须显式设 `0`，否则同一消息被多台机器各拉一遍 |

> **换掉"已经装好的那台"的超管口令，不能靠重跑 bootstrap。** `SEED_PASSWORD=… bash scripts/bootstrap.sh`
> 只在**新装 / 库里还没有 admin 行**时决定口令；存量实例上它三重失效：`system_users` 上有 v3_36.0 装的两把守卫触发器
> （`trg_guard_initial_admin_password` 拒绝对 id=1 的 `password` 做任何 UPDATE，`trg_guard_initial_admin_delete` 拒删 id=1，
> 连"停用"这条路也没有——`enabled=false` / `status<>1` 同样被拒），而 seed 的 `Clean` 只删演示形状的行
> （`real_name LIKE '%[seed-demo]%'` / `phone LIKE '138000000%'` / `email LIKE '%@hivemtk.demo'`）。本机 id=1 是
> `InitAdmin` 建的（它只收 username/password/email，`phone` 为空），三条谓词一条都不命中 ⇒ `Clean` 一句没删，
> 随后它 `Create` 新 admin 撞 `idx_system_users_username` 唯一索引；若你那台的 id=1 是 `cmd/seed` 写的那行
> （`cmd/seed/seed_users.go:99` 的 phone 就是 `13800000001`），它会落进谓词 ⇒ 整条 DELETE 被删除触发器顶回来、seed 直接失败。
> 两条锁的活库实测（只回滚事务，前后 `count(*)` 均为 12）：`UPDATE ... SET password` 抛
> `初始超管账号(id=1)的密码不允许被修改`，`DELETE ... WHERE id=1` 抛 `初始超管账号(id=1)不允许被删除`。
> 存量实例二选一：
> ① 不碰口令，先把网口收回 `127.0.0.1`：`SERVER_HOST=127.0.0.1` 起进程（读取点 `user-server/cmd/api/main.go`
> 的 `resolveListenAddr`，缺省仍是 `0.0.0.0`，见 §三 端口分配与 §6.2 该行说明）。收回后本机
> `curl http://127.0.0.1:8204/healthz` 仍通、局域网侧不再可达；代价是同机之外的浏览器扩展 / 真机
> 联调全部要改走本机代理，所以这条只在"单人本机部署"时用；
> ② 换掉那一行的口令：`SEED_PASSWORD='<新口令>' bash scripts/rotate-admin-password.sh --with-env`。
> 它把"摘两把触发器 → `cmd/pwtool` 出 bcrypt → `UPDATE system_users SET password WHERE username='admin'` →
> 按 `internal/migration/migrations/v3_36_0_admin_password_guard_migration.go` 的守卫 DDL 把两把触发器建回"
> 放在**同一个事务**里（四步漏做任何一步的后果都是静默的：要么语句被触发器顶回、要么库留在无守卫状态），
> 并且顺带把新口令写进 `.env` 的 `SEED_PASSWORD` —— 不写这一笔，下一次 `bootstrap.sh` / `cmd/seed`
> 会把公开的默认值重新写回库里。口令自己决定（脚本不替你定，且拒绝长度 <12 或就是那四个公开字面量的值），
> 生一个：`openssl rand -base64 18`。
>
> 换没换成不要手工 curl，跑探针：`bash scripts/check-admin-default-credential.sh`
> （判据就是本段上面那句"拿仓库公开的默认值试登录，**从 200 变 401 才算换成功**"；
> 它默认只发**一次**登录请求——`/api/auth/login` 挂着 `BruteForceGuard("auth.login")`，
> 口径 5 次/15m 触发即锁 30m，探针扫满候选会把同机别的 e2e 一起锁在门外；`--full-ladder` 才扫满，
> `--list` 只印它从 `cmd/seed/seed_users.go` 与 `user-web/tests/auth.setup.spec.js` 抽到了哪些公开字面量。
> 手工等价式：`curl -s -o /dev/null -w '%{http_code}\n' -X POST http://127.0.0.1:8204/api/auth/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"<仓库公开的默认值>"}'`）。
> 轮换后 `bootstrap.sh`、`user-server/tests/e2e/deep_lib.sh`、`deep_trace_v2.sh`、`scripts/geo_full_test.py`
> 不需要跟着改：它们的口令链都是"显式入参 > `SEED_PASSWORD`/`ADMIN_PASSWORD` 环境 > 公开默认值"，
> 而这几个脚本的既定跑法本来就要求先 `set -a && . ./.env && set +a`（`POSTGRES_PASSWORD` 是硬前置），
> `.env` 一更新它们读到的就是新口令。`user-web/tests/auth.setup.spec.js` 也已接上同一条链
> （显式入参 > `SEED_PASSWORD` 环境 > 仓根 `.env` 现取**首条** > 公开历史值；注入口 `HIVEMTK_ENV_FILE`，
> 它的候选数组从"3 个写死字面量"变成"环境档 + `.env` 档 + 那 3 个字面量"）。
> **轮换后会红的是剩下那批仍写死 `['Admin@12345678', …]` 的 dev-only 审计夹具**
> （`grep -rl Admin@12345678 user-web/tests user-server/tests` 现数 21 份，减去已接链的 `auth.setup.spec.js` 自己 = 20 份；
> 它们打的也是 `E2E_BASE_URL` → `/api/auth/login`，而轮换把库里那一行改成了随机值 ⇒ 这批必然 401）。
> 本批不代改那 20 份，理由有三条实测：其中 5 份此刻正被别的泳道改着（`git diff --name-only` 命中）、
> 整套 `npx playwright test --list` 在这棵树上本就收不起（两份用例在模块顶层读 `/tmp` 取证文件：
> `user-web/tests/e2e/walk-all-routes.spec.js:9` 读 `/tmp/route-paths.json`、
> `user-web/tests/e2e/ui-audit.spec.js:15` 读 `/tmp/uiwalk/routes.json`，两个文件此刻都不存在 ⇒ 收集阶段直接崩、
> 实测 `Total: 0 tests in 0 files` rc=1）、改完没有任何可跑的门。
> 补法照 `auth.setup.spec.js` 那一格：把写死的那一个值换成"环境档 + 公开档"的候选数组。
> 另见 `docs/superpowers/specs/2026-09-21-offline-deployment-design.md` §7 的"另一泳道夹具"口径。

### 6.3 config.yaml 要点（user-server/config.yaml）

- `inference.*`：本地推理栈回落默认值，必须与代码 `DefaultInferenceConfig()` 一致（有测试断言），**一般不改**；
- 运行时 LLM 参数（temperature/max_tokens/提供商切换）走后台「LLM 路由」页面写库，重启后从 `llm_providers` 表加载；
- `i18n.fallback.enabled`：DeepL 降级总开关，默认关闭，开启还需 `DEEPL_API_KEY`；
- `logging.output: both`：日志同时写 stdout 和 `logs/user-server.log`，生产保持默认即可。

## 七、对外发布模式

三种模式按需选择，可以叠加（例如反代 + FRP）。

### 模式 A：本机 / 局域网使用

什么都不用配。访问 `http://<内网IP>:8204` 即可。适合个人体验与内网测试。注意此模式下被动渠道 Webhook 不可用（无公网 HTTPS 地址），相关渠道自动走轮询。

### 模式 B：反向代理层 反向代理 + HTTPS（公网标准部署）

证书签发：

```bash
```

配套两件事：

1. `.env` 设置 `PUBLIC_BASE_URL=https://chat.example.com`（被动渠道 Webhook 依赖它）；
2. `CORS_ALLOW_ORIGINS_USER` 加入前端实际 Origin。

### 模式 C：FRP 私域部署（服务在内网、无公网 IP）

适用：数据必须留在内网的合规场景。云端 VPS 只跑 frps 做隧道。

```ini
# 云端 VPS /etc/frp/frps.ini
[common]
bind_port = 7000
vhost_https_port = 443
vhost_http_port = 80
```

```toml
# 内网机器 frpc.toml
serverAddr = "your-vps-ip"
serverPort = 7000

[[proxies]]
name = "user-server"
type = "http"
localPort = 8204
customDomains = ["chat.example.com"]
```

完整拓扑、TLS 与域名解析细节见 [architecture/FRP私域部署指南.md](architecture/FRP私域部署指南.md)。

## 八、健康检查端点

路由注册于 `internal/server/router.go`，共三个，**没有 `/metrics` 端点**：

| 路径 | 类型 | 用途 |
|------|------|------|
| `/healthz` | 存活 | 进程活着即可过；适合 K8s liveness / 进程守护 |
| `/readyz` | 就绪 | 依赖就绪才返回 200；适合负载均衡摘流判断 |
| `/health` | 综合 | 附带数据层依赖检查详情；人工巡检首选 |

接入示例（systemd 或 supervisor 心跳检测用 `/healthz`；反向代理层 upstream 健康检查用 `/readyz`）。

## 九、日常运维操作

所有操作入口都在仓库根目录 `Makefile`，先看一遍全量帮助：

```bash
make dev-help        # 开发类目标说明
```

### 9.1 数据层

```bash
make db-up / db-down / db-ps / db-logs
make db-backup                 # 备份（见第十节）
make db-restore FILE=/path/to/dump
```

### 9.2 推理栈

```bash
make inference-host-status     # 三端口健康一览
make inference-host-logs       # 跟踪 llama-server 日志
make inference-host-restart    # 整组重启
make inference-host-warmup     # 冷启动预热
make inference-host-test       # 冒烟测试（含 smoke-test.sh）
make inference-host-down       # 整组停止
make inference-host-models-prod # 切换生产档位模型
```

### 9.3 应用

```bash
make user-build                # 重编译后端二进制
make web-build                 # 重构建前端
make lint && make vet && make test-go   # 发布前质量门禁
```

### 9.4 开发热加载

```bash
make dev          # air 热重载（监听 *.go *.yaml *.html 及 ../.env 变更）
make dev-stop
make dev-clean
```

### 9.5 日志位置

- user-server：stdout + `user-server/logs/user-server.log`（滚动上限 200MB × 保留策略见 config.yaml logging 段）；
- llama-server：`make inference-host-logs` 查看；
- 数据层：`make db-logs` 查看。

## 十、备份与恢复

### 10.1 备份范围

| 对象 | 工具 | 说明 |
|------|------|------|
| PostgreSQL 全库 | `make db-backup`（pg_dump） | 业务数据主体，最高优先级 |
| Redis | RDB 快照 | 会话/缓存性质为主，丢失可接受时可不备 |

### 10.2 定时备份示例

```bash
#!/bin/bash
# /opt/hivemtk/scripts/backup.sh —— crontab: 0 2 * * *
set -euo pipefail
cd /opt/hivemtk                      # 仓库根目录，保证 Makefile 可寻址
BACKUP_DIR=/var/backups/hivemtk/$(date +%Y%m%d)
mkdir -p "$BACKUP_DIR"

source .env                          # 注入 POSTGRES_PASSWORD
PGPASSWORD="$POSTGRES_PASSWORD" pg_dump \
  -h 127.0.0.1 -p 8202 -U admin -d user_db \
  | gzip > "$BACKUP_DIR/user_db.sql.gz"

redis-cli -h 127.0.0.1 -p 8203 -a "$REDIS_PASSWORD" --no-auth-warning BGSAVE
sleep 3
docker cp mtk-redis:/data/dump.rdb "$BACKUP_DIR/" 2>/dev/null || true

find /var/backups/hivemtk/ -maxdepth 1 -mtime +7 -exec rm -rf {} +
```

### 10.3 恢复

```bash
# PostgreSQL
gunzip -c user_db.sql.gz | PGPASSWORD=<pwd> psql -h 127.0.0.1 -p 8202 -U admin -d user_db

# Redis（停容器 → 覆盖 dump.rdb → 起容器）
make db-down
docker cp dump.rdb mtk-redis:/data/dump.rdb   # 或直接替换对应 volume 内容
make db-up
```

> 恢复后务必跑一次 `curl :8204/health` 确认依赖连通，再放流量。

## 十一、升级与回滚

### 升级步骤

```bash
cd /opt/hivemtk
git pull

# 1. 质量门禁（可选但强烈建议）
make lint vet test-go

# 2. 构建新版本
make user-build web-build

# 3. 停旧进程 → 起新进程
#    （systemd/supervisor 场景：systemctl restart hivemtk-user）

# 4. 新表结构由启动时 AutoMigrate 自动对齐，无需手工迁移

# 5. 验证
curl http://127.0.0.1:8204/healthz
make inference-host-test
```

### 回滚

```bash
git checkout <上一个发布 tag>
make user-build web-build
# 重启进程；AutoMigrate 只加列不删列，回滚旧代码兼容新表结构
```

### 密钥轮换提醒

- `FIELD_ENCRYPTION_KEY` **不可直接轮换**：会使 api_logs / audit_logs 已加密字段无法解密。必须先解密存量数据再换钥；
- `JWT_SECRET` 轮换会使所有在线登录态失效（用户需重新登录），选择低峰期操作。

---

## 相关文档

- [产品功能总览（官方文档）](marketing-features/README.md)
- [故障排查手册](TROUBLESHOOTING.md)
- [FRP 私域部署指南](architecture/FRP私域部署指南.md)
- [推理栈脚本说明](../scripts/inference-host/README.md)

---

*最后更新：2026-08-26 · 基于 ports.go / Makefile / docker-compose.yml / .env-example / config.yaml 源码核对*
