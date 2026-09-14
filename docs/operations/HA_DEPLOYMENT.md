# HiveMtk 高可用部署指南

> 单商户本地部署的高可用方案。涵盖：单机部署 + 定期备份 + 故障恢复。

---

## 1. 部署架构

```
┌─────────────────────────────────────────────────┐
│              部署主机 (单机)                      │
│                                                 │
│  ┌─────────┐  ┌─────────┐  ┌─────────────────┐ │
│  │user-web │  │user-svr │  │  llama.cpp      │ │
│  │ (前端)  │  │ (后端)  │  │  (本地 LLM)     │ │
│  └─────────┘  └─────────┘  └─────────────────┘ │
│       │              │              │           │
│       ▼              ▼              ▼           │
│  ┌─────────────────────────────────────────┐   │
│  │           PostgreSQL (pgvector)          │   │
│  └─────────────────────────────────────────┘   │
│       │                                         │
│       ▼                                         │
│  ┌─────────────────────────────────────────┐   │
│  │              Redis (缓存)                │   │
│  └─────────────────────────────────────────┘   │
│       │                                         │
│       ▼                                         │
│  ┌─────────────────────────────────────────┐   │
│  │     定期备份到本地 / 远程存储             │   │
│  └─────────────────────────────────────────┘   │
└─────────────────────────────────────────────────┘
```

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

```bash
# Ubuntu/Debian
sudo apt update
```

### 3.2 配置服务

```bash
# 克隆项目
git clone https://github.com/your-org/hivemtk.git
cd hivemtk

# 复制配置
cp .env-example .env
cp docker-compose.yml docker-compose.yml

# 编辑配置
vim .env  # 设置 JWT_SECRET、LLM 路径等
```

### 3.3 启动服务

```bash
# 启动所有服务
docker compose up -d

# 查看状态
docker compose ps

# 查看日志
docker compose logs -f user-server
```

### 3.4 配置开机自启

```bash
# 创建 systemd 服务
sudo tee /etc/systemd/system/hivemtk.service << 'EOF'
[Unit]
Description=HiveMtk Service
After=docker.service
Requires=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/docker compose -f /opt/hivemtk/docker-compose.yml up -d
ExecStop=/usr/bin/docker compose -f /opt/hivemtk/docker-compose.yml down

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl enable hivemtk
```

## 4. 定期备份

### 4.1 备份脚本

```bash
#!/bin/bash
# /opt/hivemtk/scripts/backup.sh
# 每日 02:00 执行

set -euo pipefail

BACKUP_DIR="/var/backups/hivemtk/$(date +%Y%m%d_%H%M%S)"
mkdir -p "$BACKUP_DIR"

# PostgreSQL 备份
docker compose exec -T postgres pg_dump -U hivemtk -d hivemtk -Fc > "$BACKUP_DIR/postgres.dump"

# Redis 备份
docker compose exec -T redis redis-cli --rdb /tmp/dump.rdb
docker compose cp redis:/tmp/dump.rdb "$BACKUP_DIR/redis.rdb"

# 上传文件备份
tar czf "$BACKUP_DIR/uploads.tar.gz" /opt/hivemtk/uploads/

# 清理 7 天前的备份
find /var/backups/hivemtk -type d -mtime +7 -exec rm -rf {} +

echo "Backup completed: $BACKUP_DIR"
```

### 4.2 定时任务

```bash
# 添加到 crontab
crontab -e
# 添加：0 2 * * * /opt/hivemtk/scripts/backup.sh >> /var/log/hivemtk-backup.log 2>&1
```

## 5. 故障恢复

### 5.1 服务重启

```bash
# 重启服务
cd /opt/hivemtk
docker compose restart

# 查看状态
docker compose ps
```

### 5.2 数据恢复

```bash
# 停止服务
docker compose down

# 恢复 PostgreSQL
cat /var/backups/hivemtk/YYYYMMDD_HHMMSS/postgres.dump | \
  docker compose exec -T postgres pg_restore -U hivemtk -d hivemtk

# 恢复 Redis
docker compose cp /var/backups/hivemtk/YYYYMMDD_HHMMSS/redis.rdb redis:/tmp/dump.rdb
docker compose restart redis

# 启动服务
docker compose up -d
```

### 5.3 完全恢复

```bash
# 完全恢复到新主机
# 1. 安装依赖
# 2. 克隆项目
# 3. 恢复备份数据
# 4. 启动服务
```

## 6. 健康检查

```bash
# 检查服务状态
curl http://localhost:8204/healthz

# 检查数据库连接
docker compose exec -T postgres pg_isready -U hivemtk

# 检查 Redis 连接
docker compose exec -T redis redis-cli ping

# 检查 LLM 服务
curl http://localhost:8080/health
```

---

*最后更新: 2026-08-16*
