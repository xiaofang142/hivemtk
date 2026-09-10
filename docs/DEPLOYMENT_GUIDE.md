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
│  ├─ user-server 二进制        监听 127.0.0.1:8204         │
│  │   （make user-build 产物，air 热重载用于开发）           │
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

**唯一权威来源：[ports.go](file:///Users/xiaofang/Documents/www/go/hivemtk/hivemtk/user-server/internal/config/ports.go)。网上任何资料与本表冲突时，以代码为准。**

| 服务 | 端口 | 绑定 | 说明 |
|------|------|------|------|
| user-server API | **8204** | 宿主机监听 | 主 API（HTTP + WebSocket 同端口） |
| PostgreSQL（Docker） | **8202** | 127.0.0.1 | 容器映射自 5432，宿主机侧永远是 8202 |
| Redis（Docker） | **8203** | 127.0.0.1 | 容器映射自 6379，宿主机侧永远是 8203 |
| platform-server | 8205 | 可选组件 | 平台端 API 网关（`PLATFORM_API_HOST` 指向它） |
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
# 至少修改以下字段（全部要求强随机值，可用 openssl rand -hex 32 生成）
POSTGRES_PASSWORD=
REDIS_PASSWORD=
JWT_SECRET=               # ≥32 字符，不足启动时直接 panic
FIELD_ENCRYPTION_KEY=     # ≥32 字符
MERCHANT_API_SECRET=      # ≥32 字符
PLATFORM_LICENSE_SECRET=
PLATFORM_ADMIN_PASSWORD=
```

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
| `JWT_SECRET` | ≥32 字符 | user-server 启动 panic（测试专用短密钥仅在 test 模式放行） |
| `FIELD_ENCRYPTION_KEY` | ≥32 字符 | 加密字段功能不可用；**轮换会使既有加密数据失效** |
| `MERCHANT_API_SECRET` | ≥32 字符 | merchant-api 签名鉴权失败（user-server 经 config/platform.yaml `secret: "${MERCHANT_API_SECRET}"` 消费） |
| `DB_HOST` / `DB_PORT` | 默认 `127.0.0.1:8202` | 连不上库直接启动失败 |

### 6.2 按需设置

| 变量 | 默认 | 何时需要设 |
|------|------|-----------|
| `PUBLIC_BASE_URL` | 空 | **部署 Telegram/飞书/钉钉等被动回调渠道时必填**。格式 `https://域名`（不带路径、不带尾斜杠），系统会用它注册 Webhook；留空则这些渠道自动降级 polling 模式（仅单实例可用） |
| `PLATFORM_API_HOST` | `http://127.0.0.1:8205` | 平台端不在本机时改为其实际地址。注意实际读取的是 `PLATFORM_API_HOST` 不是 `PLATFORM_API_URL` |
| `CORS_ALLOW_ORIGINS_USER` | 见 .env-example | 前端域名与 API 不同源时，把前端 Origin 加入白名单 |
| `DEEPL_API_KEY` | 空 | 启用低资源语言（ar/th/vi/hi/tr）DeepL 翻译降级时 |
| `QINIU_ACCESS_KEY` / `QINIU_SECRET_KEY` | 空 | 使用七牛云对象存储时 |
| `LLM_*` / `EMBEDDING_*` / `RERANK_*` | 见 .env-example | 控制推理栈下载哪个模型、监听哪个端口 |

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
