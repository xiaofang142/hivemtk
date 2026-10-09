# HiveMtk 高可用部署指南

> 单商户本地部署的高可用方案。涵盖：单机部署 + 定期备份 + 故障恢复。

---

## 1. 部署架构

`docker-compose.yml` 只承载**数据层两个服务**（`mtk-postgres` / `mtk-redis`）；
后端、前端和推理栈都是**宿主机进程**。下面这张图按 `docker compose config` 与
`docker ps` 现测重画，旧图里的「user-web 独立服务」在本仓并不存在。

```
┌────────────────────────────────────────────────────────────────┐
│                     部署主机 (单机)                             │
│                                                                │
│  宿主机进程                                                     │
│  ┌───────────────────────────┐  ┌────────────────────────────┐ │
│  │ user-server  :8204        │  │ llama-server 三件套        │ │
│  │  · /api/* HTTP + WS 同口  │  │  LLM      127.0.0.1:8207   │ │
│  │  · 托管 user-web/dist     │◄─┤  Embedding 127.0.0.1:8208  │ │
│  │    （前端不是独立服务）   │  │  Rerank   127.0.0.1:8209   │ │
│  └────────────┬──────────────┘  └────────────────────────────┘ │
│               │  127.0.0.1                                      │
│               ▼  Docker 容器（仅数据层）                        │
│  ┌────────────────────────────┐ ┌────────────────────────────┐ │
│  │ mtk-postgres               │ │ mtk-redis                  │ │
│  │ pgvector/pgvector:pg15     │ │ redis:7-alpine             │ │
│  │ 127.0.0.1:8202 · user_db   │ │ 127.0.0.1:8203             │ │
│  │ 卷 mtk_user_pg_data        │ │ 卷 mtk_user_redis_data     │ │
│  └────────────────────────────┘ └────────────────────────────┘ │
│               │                                                │
│               ▼                                                │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ 定期备份：pg_dump + Redis /data + user-server/uploads    │  │
│  │ → 本地 /var/backups/hivemtk（远程存储需自行加同步任务）  │  │
│  └──────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────┘
```

> 推理三件套是宿主机进程，容器网络里没有 `llama-server` 这个 DNS 名；
> 配置里必须写 `127.0.0.1`（`.env` 现值：`LLM_BASE_URL=http://127.0.0.1:8207/v1`），
> 写成 `http://llama-server:8207` 解析不到。


## 2. 硬件要求

| 组件 | 最低配置 | 推荐配置 |
|------|---------|---------|
| 主机 | 8C16G + 500G SSD | 16C32G + 1T NVMe |
| GPU | 可选（llama.cpp CPU 推理） | RTX 3060 12G+ |

## 反代配置

部署前请先选择反代（nginx / Caddy / Traefik / FRP）并按
[反代配置模板](reverse-proxy/README.md) 配置。**关键约束**：HTTP/2 必须
显式关闭，否则 SSE 大屏数据延迟/丢失。

> 另见：[私域部署强制要求](../../user-server/docs/operations/PRIVATE_NETWORK_REQUIRED.md)（user-server 禁止直接公网暴露）、
> [LICENSE 合规自检](LICENSE_COMPLIANCE.md)（AGPL-3.0 第 13 条）。

## 3. 部署步骤

### 3.1 安装依赖

版本要求来自 [DEPLOYMENT_GUIDE.md §4.1](../DEPLOYMENT_GUIDE.md)（Go 按 `user-server/go.mod`
的 `go 1.26.0` / `toolchain go1.26.6`；`user-web/package.json` 没有 `engines` 段，
Node 版本按文档口径取 18 LTS+）：

| 软件 | 版本 | 用途 | 验证 |
|------|------|------|------|
| Docker + Compose 插件 | 稳定版 | 只跑数据层两个容器 | `docker compose version` |
| Go | `user-server/go.mod` 声明的版本 | 编译 user-server | `go version` |
| Node.js | 18 LTS+ | 构建 user-web / embed-sdk | `node -v` |
| psql 客户端 | 与容器同大版本（本机容器是 PG 15） | 初始化 SQL、跨版本备份 | `psql --version` |
| make / git / curl | 系统自带 | 运维入口 | `make --version` |

```bash
# Ubuntu/Debian
sudo apt update
sudo apt-get install -y curl wget git make postgresql-client ca-certificates
# Go 与 Node 请按官方渠道装，发行版仓库版本通常过旧
# llama.cpp 由 make inference-host-install 现场编译，不需要 apt 包
```

> 宿主机 `pg_dump` / `psql` 的大版本必须 **不高于**容器服务端版本。本机实测：
> 宿主机 Homebrew 客户端 16.14、容器 `pgvector/pgvector:pg15` 服务端 15.18，
> 宿主机 `pg_dump -Fc` 出来的文件在容器里 `pg_restore` 直接报
> `unsupported version (1.15) in file header`。要么在容器内跑备份（见 §4.1），
> 要么把客户端降到 15。

### 3.2 配置服务

```bash
# 克隆项目（与本仓现有 remote 一致的两个镜像，见 MERCHANT_DEPLOYMENT.md）
git clone https://github.com/xiaofang142/hivemtk.git hivemtk   # GitHub
# git clone https://gitee.com/xhpmayun/hivemtk.git hivemtk     # Gitee（国内更快）
cd hivemtk

# 生成 .env。.env 不存在时 make install 只做 cp .env-example .env + 提示改密钥，
# 之后还会串起 web-build / sdk-build / inference-host-install / inference-host-models / db-up
# （hivemtk/Makefile:76-88），所以要连着 npm / Go / llama.cpp 工具链一起准备。
make install

# 必改的密钥（全部用 openssl rand -hex 32 生成；值不要写进任何文档）
#   POSTGRES_PASSWORD  REDIS_PASSWORD  JWT_SECRET(>=32，不足启动 panic)
#   FIELD_ENCRYPTION_KEY
# 推理栈地址保持 127.0.0.1 + 8207/8208/8209，见 §1 的注记
vim .env
```

### 3.3 启动服务

```bash
# 1) 数据层（Docker 只跑这两个；等价于 docker compose up -d）
make db-up
make db-ps            # 确认 mtk-postgres / mtk-redis 都 healthy

# 2) 推理栈（宿主机 llama-server 三件套）
make inference-host-install
make inference-host-models
make inference-host-up
make inference-host-status

# 3) 构建并启动后端 + 前端（都是宿主机进程，不在 compose 里）
make user-build       # → user-server/bin/user-server
make web-build        # → user-web/dist，由 user-server 以 ../user-web/dist 托管
cd user-server && ./bin/user-server

# 一键全栈（PG + Redis + 推理 + user-server + user-web）
make dev-all
```

看日志（**compose 里没有叫 `user-server` 的服务**，`docker compose logs -f user-server`
会直接报 `no such service`）：

```bash
make db-logs                                  # PG + Redis 容器日志
docker compose logs -f mtk-postgres           # 指定单个服务
tail -f user-server/logs/user-server.log      # 后端日志（config.yaml logging.file）
make inference-host-logs                      # llama-server 日志
# 只有开了模式 C（后端进容器 air 热重载）才有后端容器日志，服务名是 user-server-dev：
docker compose -f docker-compose.yml -f docker-compose.dev.yml logs -f user-server-dev
```

### 3.4 配置开机自启

`docker compose up -d` 在这里只等价于「起数据层」，后端不在 compose 管辖内，
所以单个 unit 是不够的：要么一个 unit 里按顺序把数据层 + 推理 + 后端都起来，
要么拆成三个。下面给拆开的写法（后端那段依赖 `user-server/bin/user-server` 已 `make user-build`）。

```bash
# 数据层（PG + Redis）
sudo tee /etc/systemd/system/hivemtk-data.service << 'EOF'
[Unit]
Description=HiveMtk data layer (mtk-postgres + mtk-redis)
After=docker.service
Requires=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/opt/hivemtk
ExecStart=/usr/bin/docker compose up -d
ExecStop=/usr/bin/docker compose down

[Install]
WantedBy=multi-user.target
EOF

# 宿主机后端进程
sudo tee /etc/systemd/system/hivemtk-user.service << 'EOF'
[Unit]
Description=HiveMtk user-server
After=network-online.target hivemtk-data.service
Requires=hivemtk-data.service

[Service]
Type=simple
WorkingDirectory=/opt/hivemtk/user-server
ExecStartPre=/usr/bin/make -C /opt/hivemtk user-build
ExecStart=/opt/hivemtk/user-server/bin/user-server
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now hivemtk-data
sudo systemctl enable --now hivemtk-user
systemctl status hivemtk-user
```

> 本仓 `deploy/` 下只有一个 Helm **骨架**（`deploy/helm/hivemtk/Chart.yaml` 自述
> 「状态: 骨架 (skeleton)，仅覆盖 user-server Deployment + Service + Ingress」），
> **不代表现网形态**；单机高可用请按上面的 systemd 路线，不要按 k8s 写运维口径。
> 真要按该 chart 起：`templates/deployment.yaml:5` 的名字是 `{{ .Chart.Name }}-user-server`
> ⇒ `kubectl get deploy hivemtk-user-server`，写成 `deployment/user-server` 找不到资源。


## 4. 定期备份

### 4.1 备份脚本

```bash
#!/bin/bash
# /opt/hivemtk/scripts/backup.sh
# 每日 02:00 执行

set -euo pipefail

# compose 与 .env 都在仓库根；脚本自己也要 source 一次，后面的变量才有值
cd /opt/hivemtk
set -a
. ./.env
set +a

BACKUP_DIR="/var/backups/hivemtk/$(date +%Y%m%d_%H%M%S)"
mkdir -p "$BACKUP_DIR"

# PostgreSQL 备份：服务名 mtk-postgres（不是 postgres），角色/库名取 .env 的
# POSTGRES_USER / USER_DB_NAME（不是 hivemtk/hivemtk），端口必须显式给 8202
docker compose exec -T mtk-postgres \
  pg_dump -U "${POSTGRES_USER:-admin}" -h 127.0.0.1 -p 8202 -d "${USER_DB_NAME:-user_db}" -Fc \
  > "$BACKUP_DIR/postgres.dump"

# Redis 备份：appendonly yes，AOF 优先于 RDB —— 只拷 dump.rdb 等于没备，整 /data 目录打包
docker compose exec -T mtk-redis \
  redis-cli -p 8203 -a "${REDIS_PASSWORD:?REDIS_PASSWORD must be set in .env}" \
  --no-auth-warning BGSAVE
sleep 2
docker compose cp mtk-redis:/data "$BACKUP_DIR/redis_data"
tar czf "$BACKUP_DIR/redis_data.tar.gz" -C "$BACKUP_DIR" redis_data
rm -rf "$BACKUP_DIR/redis_data"

# 上传文件备份（本地存储根 = user-server/uploads，可用 STORAGE_LOCAL_BASE_DIR 覆盖）
tar czf "$BACKUP_DIR/uploads.tar.gz" -C /opt/hivemtk/user-server uploads

# 配置备份（.env 含口令，落盘目录权限请自行收紧到 700）
cp /opt/hivemtk/.env /opt/hivemtk/docker-compose.yml "$BACKUP_DIR/"

# 清理 7 天前的备份
find /var/backups/hivemtk -maxdepth 1 -type d -mtime +7 -exec rm -rf {} +

echo "Backup completed: $BACKUP_DIR"
```

### 4.2 定时任务

```bash
# 添加到 crontab
crontab -e
# 添加：0 2 * * * /opt/hivemtk/scripts/backup.sh >> /var/log/hivemtk-backup.log 2>&1
```

完整口径（含备份校验、误删回捞、pg_resetwal）见
[DR_RECOVERY.md](DR_RECOVERY.md)，本节只给日常最小闭环。

## 5. 故障恢复

### 5.1 服务重启

三类组件的归属不同，`docker compose restart` 只会重启 PG + Redis，
后端和推理栈它管不到。

```bash
cd /opt/hivemtk

# 数据层（容器）
docker compose restart mtk-postgres mtk-redis
make db-ps

# 后端（宿主机进程；systemd 场景改用 systemctl restart hivemtk-user）
pkill -f 'user-server/bin/user-server' || true
cd user-server && nohup ./bin/user-server >> logs/user-server.log 2>&1 &

# 推理栈（宿主机 llama-server）
# Makefile 里没有 inference-host-restart 这个目标，整组重启 = 先停再起
make inference-host-down && make inference-host-up
make inference-host-status

# 查看状态
docker compose ps
```

### 5.2 数据恢复

`docker compose down` 之后容器就没了，紧接着的 `docker compose exec` 必然失败；
恢复的顺序是「先把数据层起来，再灌备份」。完整带校验的版本在
[DR_RECOVERY.md §2.1](DR_RECOVERY.md)，这里是等价的最小写法。

```bash
cd /opt/hivemtk
set -a
. ./.env
set +a

# 0. 停后端写入
pkill -f 'user-server/bin/user-server' || true

# 1. 起数据层并等 PG 就绪
docker compose up -d
until docker compose exec -T mtk-postgres \
        pg_isready -U "${POSTGRES_USER:-admin}" -h 127.0.0.1 -p 8202 -q; do sleep 2; done

# 2. 恢复 PostgreSQL：先建空库，pg_restore 走 stdin（容器读不到宿主机路径）
BK=/var/backups/hivemtk/YYYYMMDD_HHMMSS
docker compose exec -T mtk-postgres psql \
  -U "${POSTGRES_USER:-admin}" -d postgres -h 127.0.0.1 -p 8202 \
  -c "DROP DATABASE IF EXISTS ${USER_DB_NAME:-user_db};" \
  -c "CREATE DATABASE ${USER_DB_NAME:-user_db};"
cat "$BK/postgres.dump" | docker compose exec -T mtk-postgres \
  pg_restore -U "${POSTGRES_USER:-admin}" -h 127.0.0.1 -p 8202 \
  -d "${USER_DB_NAME:-user_db}" --no-owner --no-privileges

# 3. 恢复 Redis：停容器 → 换掉整个 /data（含 appendonlydir，只放 dump.rdb 会被 AOF 顶掉）
docker compose stop mtk-redis
docker run --rm \
  -v mtk_user_redis_data:/data \
  -v "$BK":/backup \
  alpine:3 sh -c 'rm -rf /data/appendonlydir /data/dump.rdb && \
                   tar xzf /backup/redis_data.tar.gz -C /data --strip-components=1'
docker compose up -d mtk-redis

# 4. 恢复上传文件
tar xzf "$BK/uploads.tar.gz" -C /opt/hivemtk/user-server

# 5. 起后端
cd /opt/hivemtk/user-server && nohup ./bin/user-server >> logs/user-server.log 2>&1 &
```

### 5.3 完全恢复

```bash
# 完全恢复到新主机
# 1. 安装依赖（§3.1）
# 2. 克隆项目 + make install 生成 .env（§3.2）
# 3. make db-up 起数据层，再按 §5.2 恢复备份数据
# 4. make inference-host-up + 起后端（§3.3 / §5.1）
# 5. 按 DR_RECOVERY.md §2.2 验证
```

## 6. 健康检查

```bash
# 存活探针：只回 alive，不看依赖（internal/router/health.go LivenessCheck）
curl -s http://127.0.0.1:8204/healthz
#   {"code":0,"message":"ok","data":{"status":"alive","timestamp":...}}

# 依赖详情：database / redis / inference / embedding 四项，人工巡检看这个
curl -s http://127.0.0.1:8204/health

# 就绪探针：依赖没齐回 503（摘流用）
curl -s http://127.0.0.1:8204/readyz

# 检查数据库连接（服务名 + 端口 + 角色 + 库名都要显式给）
docker compose exec -T mtk-postgres pg_isready -U "${POSTGRES_USER:-admin}" -h 127.0.0.1 -p 8202

# 检查 Redis 连接（容器内监听 8203，且 requirepass 已开）
docker compose exec -T mtk-redis redis-cli -p 8203 -a "${REDIS_PASSWORD}" --no-auth-warning ping

# 检查推理栈：三件套是宿主机进程，端口 8207/8208/8209，健康路径 /health
# （旧写法 http://localhost:8080/health 的 8080 在本仓不是任何服务的端口）
curl -s http://127.0.0.1:8207/health   # LLM
curl -s http://127.0.0.1:8208/health   # Embedding
curl -s http://127.0.0.1:8209/health   # Rerank
# 一条命令看全三个端口
make inference-host-status
```

---

*最后更新: 2026-10-09（按 `docker-compose.yml`、`hivemtk/Makefile`、`internal/router/`
与本机在跑的 `mtk-postgres` / `mtk-redis` / `mtk-user-server-dev` 逐条重跑校正）*

