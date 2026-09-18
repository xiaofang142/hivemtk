# HiveMtk 用户端 - 部署手册

> 用户端独立部署（私域模式，开源版）
> 适用对象：商户 / 终端用户

---

## 一、部署模式

HiveMtk 用户端采用**私域独立部署**模式：

- 部署在商户自己的服务器（或私有云、混合云）
- 数据库、推理栈、用户数据全部本地化
- 平台端（安装信息收集、心跳上报）通过 `PLATFORM_API_URL` 低频 HTTPS 调用，数据不落地平台端
- 每个商户独立一套完整系统（user-server + PostgreSQL + Redis + 推理栈）

> **禁止 SaaS / 多租户模式**：无 `merchant_id` 字段，所有数据归属当前部署实例。
> **开源版无 License 校验**：所有功能全开放，无 7 天试用、无授权码、无强制首登改密。

---

## 二、硬件最低要求

| 资源 | 最低 | 推荐（生产）| 备注 |
|------|------|------------|------|
| CPU  | 2 核 | 8 核+      | LLM 推理需 4 核+ |
| 内存 | 4GB  | 16GB+      | 14B 模型需 12GB+ |
| 磁盘 | 50GB | 200GB+     | 模型文件 ~10GB |
| 网络 | 内网 | 公网/FRP   | 公网对话需 HTTPS |

**GPU 加速（可选）**：
- NVIDIA 8GB+（dev 档 3B 模型）
- NVIDIA 16GB+（prod 档 14B 模型）

---

## 三、5 步完成部署

### 步骤 1：克隆代码

```bash
git clone https://gitee.com/your-org/hivemtk.git
cd hivemtk
```

### 步骤 2：准备环境变量

```bash
cp .env-example .env

# 关键变量（必须修改）
POSTGRES_PASSWORD=$(openssl rand -hex 24)
JWT_SECRET=$(openssl rand -hex 32)
PLATFORM_ADMIN_PASSWORD=$(openssl rand -hex 16)

# 平台端地址（同机部署用 127.0.0.1）
PLATFORM_API_URL=http://127.0.0.1:8205
# 跨机/生产：改为平台端公网域名，如 https://api.example.com
```

> 开源版**无需**生成 `PLATFORM_LICENSE_SECRET`、`LicenseKey` 等授权相关密钥（已下线）。

### 步骤 3：启动本地推理栈

```bash
# 推荐：宿主机 llama.cpp 三件套（LLM + Embedding + Rerank）
make inference-host-install        # 安装 llama.cpp 二进制（首次）
make inference-host-models         # 下载 dev 档模型（Qwen2.5-3B + bge-m3 + bge-reranker-v2-m3）
make inference-host-up             # 启动三服务
make inference-host-warmup         # 预热首请求（避免首调用冷启动慢）

# 也可直接调用底座脚本
# bash scripts/inference-host/install-llama-cpp.sh
# bash scripts/inference-host/download-models.sh
# bash scripts/inference-host/start-all.sh
```

等待三个推理服务（LLM/Embedding/Rerank）health-check 通过后继续。可用 `make inference-host-status` 统一查看。

### 步骤 4：启动用户端服务

```bash
# 方式 A：宿主机开发模式（推荐，air 热更新，1~2s 增量重编）
make dev                            # 自动装 air + 启动热更新

# 方式 B：宿主机二进制模式（生产）
make user-build                     # 输出 user-server/bin/user-server
cd user-server && ./bin/user-server

# 方式 C：纯数据层 Docker + 全栈宿主机
docker compose up -d                # 仅拉起 PG + Redis
make dev                            # user-server 跑宿主机
```

> **重要**：本项目**不在 Docker 中跑 user-server / 前端 / 推理栈**。`docker compose up -d` 只拉起 PG + Redis 两个数据层容器。user-server 由宿主机直接运行（见 `Makefile` 与 `docker-compose.yml` 头注释）。

### 步骤 5：完成初始化流程

浏览器访问 `http://your-server-ip:8204/setup`：

1. 创建超级管理员账号（用户名 + 密码 + 选填联系方式）
2. 提交后立即生效，系统进入 `INITIALIZED` 状态
3. 跳转登录页，用刚创建的超管账号登录

> **开源版特性**：
> - 无需输入 LicenseKey
> - 无 7 天免费试用限制（功能全开放）
> - 无强制首登改密（`must_change_password` 机制已下线）
> - 详见 `user-server/internal/config/init.go` 中的初始化流程实现

---

## 四、关键端口

> 当前部署形态：**PG + Redis 走 Docker（端口绑 127.0.0.1）；user-server / 推理栈 / 前端 跑宿主机**。

| 端口 | 服务 | 部署位置 |
|------|------|---------|
| 8202 | PostgreSQL (user_db) | Docker 容器 `mtk-postgres`，绑 `127.0.0.1` |
| 8203 | Redis | Docker 容器 `mtk-redis`，绑 `127.0.0.1` |
| 8204 | user-server API（RESTful + HTTP 长轮询 + WebSocket） | 宿主机（`make dev` 或 `./bin/user-server`） |
| 8207 | llama-server (LLM) | 宿主机 llama.cpp（`scripts/inference-host/start-llm.sh`） |
| 8208 | llama-server (Embedding) | 宿主机 llama.cpp 向量化（1024 维，bge-m3） |
| 8209 | llama-server (Rerank) | 宿主机 llama.cpp 重排 |

---

## 五、数据持久化

通过命名卷持久化（仅数据层，user-server / 推理栈 / 前端跑宿主机）：

| 卷名 | 用途 | 对应容器 |
|------|------|---------|
| `mtk_user_pg_data` | PostgreSQL 数据 | `mtk-postgres` |
| `mtk_user_redis_data` | Redis 数据 | `mtk-redis` |

宿主机运行时目录（由 `scripts/inference-host/*.sh` 自动管理）：

| 路径 | 用途 |
|------|------|
| `~/.hivemtk/runtime/llm.log` | LLM 推理日志 |
| `~/.hivemtk/runtime/embedding.log` | Embedding 日志 |
| `~/.hivemtk/runtime/rerank.log` | Rerank 日志 |
| `user-server/tmp/` | air 热更新临时二进制 + 日志 |

> **不要**用 bind mount 替换 Docker 卷，否则数据可能丢失。
> **不要**假设有 `mtk_user_logs` / `mtk_user_uploads` / `mtk_user_data` 卷——这些命名只属于旧版容器化部署，**当前重构版已不再使用**（见 `docker-compose.yml` 头注释）。

---

## 六、运维命令

> 当前重构版（2026-08-17）以 `Makefile` 为运维入口（详见 `Makefile` 中 `help` target）。

```bash
# === 数据层 ===
make db-up              # 启动 PG + Redis 容器（mtk-postgres / mtk-redis）
make db-down            # 停止 PG + Redis 容器
make db-ps              # 查看数据层容器状态
make db-logs            # tail PG + Redis 日志

# === 宿主机推理栈 ===
make inference-host-up      # 启动 LLM + Embedding + Rerank
make inference-host-down    # 停止三服务
make inference-host-status  # 统一查看状态 + 端点健康检查
make inference-host-logs    # tail 三个推理服务日志

# === user-server ===
make user-build         # 编译二进制到 user-server/bin/user-server
make dev                # 启动 air 热更新（开发用）
make dev-stop           # 停止 air

# === 代码质量 ===
make lint               # golangci-lint 架构护栏
make vet                # go vet
make test-go            # go test ./...

# === 原始 docker compose 命令（仅数据层）===
docker compose ps
docker compose logs -f mtk-postgres
docker compose restart mtk-postgres
docker compose down        # 停止并保留数据
docker compose down -v     # 停止并清理数据（危险）
```

---

## 七、备份与恢复

### 备份

```bash
# 备份 PostgreSQL（Makefile 推荐）
make db-backup   # 生成 backup_YYYYMMDD_HHMMSS.sql
# 等价命令：
docker compose exec -T mtk-postgres \
  pg_dump -U $${POSTGRES_USER:-admin} $${USER_DB_NAME:-user_db} \
  > backup-$(date +%Y%m%d).sql

# 备份推理栈日志
tar czf hivemtk-runtime-backup-$(date +%Y%m%d).tar.gz ~/.hivemtk/runtime/
```

### 恢复

```bash
make db-restore FILE=backup_20260101_120000.sql
# 等价命令：
cat backup-20260101_120000.sql | docker exec -i mtk-postgres psql -U admin -d user_db
```

---

## 八、升级

> 开源版采用**纯 git 提交推送**发布：客户自行 `git pull` 拉取升级，无 OTA 下发。

```bash
# 1. 备份数据
make db-backup

# 2. 拉取新代码
git pull

# 3. 重新构建前端（如有变更）
make web-build          # user-web
make sdk-build          # embed-sdk

# 4. 重新构建并启动 user-server
make user-build && make dev    # 或直接重启宿主机进程

# 5. 执行新迁移
ls migrations/ | sort | tail -n 5
for f in migrations/0XX_*.sql; do
  docker exec -i mtk-postgres psql -U $${POSTGRES_USER:-admin} -d $${USER_DB_NAME:-user_db} < "$f"
done
```

---

## 九、常见问题

### 9.1 RAG 检索失败

```bash
# 检查 embedding 服务
curl http://localhost:8208/health
# 检查 PG 中向量维度（bge-m3 为 1024 维）
docker exec mtk-postgres psql -U admin -d user_db \
  -c "SELECT column_name, format_type(udt_name, udt_typmod) FROM information_schema.columns WHERE table_name='knowledge_embeddings' AND column_name='embedding';"
```

### 9.2 LLM 响应慢

```bash
# 检查推理服务状态
curl http://localhost:8207/health
make inference-host-status
# 检查 GPU 是否被使用（仅当启用 Metal/CUDA 后端时）
nvidia-smi    # NVIDIA
```

### 9.3 平台端心跳上报失败

> 平台端心跳为 best-effort，失败仅 Warn 日志，**不阻塞**本地业务。

```bash
# 检查 PLATFORM_API_URL 配置
grep PLATFORM_API_URL .env
# 手动测试连通性
curl -s $PLATFORM_API_URL/health
```

### 9.4 学习洞察 / 行业相关功能「静默无输出」

`Industry`（行业）**不是环境变量**，而是随请求 / 商户记录传入的业务字段
（`trace_learning`、SalesEngine 等以它作分桶键）。当调用方传入的 `industry` 为空时：

- **写入侧**（`insights.go` `ExtractErrorPattern` 落库）：空 `industry` 归一为 `"general"` 桶，
  洞察仍会保存；
- **读取侧**（`TopInsights` 供 SalesEngine 组 prompt 注入）：`if industry == "" { return nil }`
  —— **直接返回空**，不查询任何桶。

后果：未设置行业的商户，其洞察写进 `"general"` 桶，但注入侧读 `""` 桶 → 永远拿不到，
表现为「经验沉淀功能静默关闭」。

**启用方式**：为商户 / 请求显式配置非空 `industry`（如 `retail`、`finance`）。
这不是缺陷，是「按行业分桶」的设计约束；跨行业共享洞察需调用方显式传 `"general"`。

---

## 十、迁移到生产

参考 `docs/architecture/部署方案_用户端.md` 了解详细论证。

生产环境额外建议：

1. **HTTPS 终结**：使用外部反代（CDN / 云负载均衡），自行配置 TLS 证书
2. **公网 IP / FRP 穿透**：无公网 IP 时使用 FRP 把 8204 端口穿透出去（详见 `docs/architecture/FRP私域部署指南.md`）
3. **备份策略**：每日 `make db-backup` 全量备份 PostgreSQL，每周异地备份
4. **健康监控**: 通过 `user-server` 的 `/healthz` 端点检查服务健康；关键性能指标从 PG 表 SQL 查询（参见 `docs/operations/SLA_SLO.md`）
5. **日志收集**: 使用 ELK / Loki 等集中收集日志（数据层容器日志已 JSON 化，见 `docker-compose.yml` 的 `x-logging`）

---

## 十一、技术支持

- 官网：https://hivemtk.com
- 文档：本目录 `docs/INDEX.md`
- 邮箱：`jideilvluoqun@gmail.com`
- 开源仓库：
  - GitHub：https://github.com/xiaofang142/hivemtk
  - Gitee：https://gitee.com/xhpmayun/hivemtk
