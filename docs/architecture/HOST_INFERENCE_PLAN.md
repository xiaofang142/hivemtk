# HiveMtk 宿主机 llama.cpp 推理栈 —— 架构规范与部署指南

> 适用项目：hivemtk 用户端（user-server + user-web）
> 关联文档：[部署方案_用户端.md](./部署方案_用户端.md)、[项目规则](../standards/MASTER_RULES.md)

---

## 一、目标

**全部推理能力切到宿主机 llama.cpp**（统一一个二进制、同一套 OpenAI 兼容协议），Docker 仅承担 PostgreSQL + Redis 两个数据库服务。

| 能力 | 框架 | 部署位置 |
|------|------|---------|
| LLM | llama.cpp (`llama-server` + `--model`) | 宿主机 |
| Embedding | llama.cpp (`llama-server` + `--embeddings`) | 宿主机 |
| Rerank | llama.cpp (`llama-server` + `--reranking`) | 宿主机 |
| PostgreSQL | pgvector 官方镜像 | Docker (port 8202) |
| Redis | redis:7-alpine | Docker (port 8203) |
| user-server | Go 二进制 (`go run` / `air`) | 宿主机 |
| user-web | Vite dev server / 静态构建 | 宿主机 |

---

## 二、目录与文件布局

### 2.1 宿主机模型与二进制位置（约定）

```
# 1) llama.cpp 二进制（统一通过环境变量 LLAMACPP_BIN 控制，默认探测常见路径）
$ which llama-server
/opt/homebrew/bin/llama-server      # macOS Apple Silicon (brew install llama.cpp)
/usr/local/bin/llama-server         # macOS Intel / Linux (源码 make)
/usr/bin/llama-server               # Linux (apt)

# 2) 模型目录（HIVEMTK_MODELS_DIR，默认项目内 $PROJECT_ROOT/models）
#    用户需求 #9：模型文件保存在本项目 hivemtk/models 下
#    .gitignore 已忽略 models/，不会误提交大文件
#    生产部署可通过 HIVEMTK_MODELS_DIR=/data/hivemtk/models 覆盖
$HIVEMTK_MODELS_DIR/
├── llm/                              # Qwen2.5-*-Instruct-GGUF
│   └── qwen2.5-3b-instruct-q4_k_m.gguf
├── embedding/                        # bge-m3-gguf (Q4_K_M 1024 维)
│   └── bge-m3-q4_k_m.gguf
└── rerank/                           # bge-reranker-v2-m3-Q4_K_M-GGUF
    └── bge-reranker-v2-m3-Q4_K_M.gguf

# 3) PID / 日志目录（HIVEMTK_RUNTIME_DIR，默认 ~/.hivemtk/runtime）
#    运行时产物（pid/log）不进项目仓库
$HIVEMTK_RUNTIME_DIR/
├── llm.pid
├── llm.log
├── embedding.pid
├── embedding.log
├── rerank.pid
└── rerank.log
```

### 2.2 项目内新增/修改文件清单

| 类别 | 路径 | 状态 | 说明 |
|------|------|------|------|
| 脚本 | `hivemtk/scripts/inference-host/README.md` | 新增 | 宿主机推理栈使用说明 |
| 脚本 | `hivemtk/scripts/inference-host/env.sh` | 新增 | 共享环境变量（端口/路径/profile） |
| 脚本 | `hivemtk/scripts/inference-host/models.env` | 新增 | dev/prod 模型定义（仓库ID + 文件名 + 量化） |
| 脚本 | `hivemtk/scripts/inference-host/install-llama-cpp.sh` | 新增 | 安装 llama.cpp（brew/apt/source） |
| 脚本 | `hivemtk/scripts/inference-host/download-models.sh` | 新增 | ModelScope 优先 + HF 回退，GGUF 量化 |
| 脚本 | `hivemtk/scripts/inference-host/start-llm.sh` | 新增 | 启动 LLM llama-server |
| 脚本 | `hivemtk/scripts/inference-host/start-embedding.sh` | 新增 | 启动 Embedding llama-server |
| 脚本 | `hivemtk/scripts/inference-host/start-rerank.sh` | 新增 | 启动 Rerank llama-server |
| 脚本 | `hivemtk/scripts/inference-host/start-all.sh` | 新增 | 一键拉起三服务 |
| 脚本 | `hivemtk/scripts/inference-host/stop-all.sh` | 新增 | 一键停止三服务 |
| 脚本 | `hivemtk/scripts/inference-host/smoke-test.sh` | 新增 | OpenAI 兼容三端点连通性测试 |
| 脚本 | `hivemtk/scripts/inference-host/warmup.sh` | 新增 | 预热三端点（首请求 KV-cache 编译） |
| Docker | `hivemtk/docker-compose.yml` | 新增 | 仅 PG+Redis 宿主机部署 compose |
| Docker | `hivemtk/docker-compose.yml` | 保留 | 旧全栈 compose（向后兼容） |
| 配置 | `hivemtk/user-server/config.yaml` | 修改 | inference.base_url 改为 `http://127.0.0.1:8207/v1` 等 |
| 配置 | `hivemtk/user-server/config-host.yaml` | 新增 | 宿主机开发配置（127.0.0.1 全部） |
| 配置 | `hivemtk/user-server/.air.toml` | 修改 | air env 增加 LLAMACPP 端点 + 注释 |
| 配置 | `hivemtk/user-server/Dockerfile` | 删除 | 2026-08-17 重构：user-server 宿主机二进制部署，不再容器化 |
| Make | `hivemtk/Makefile` | 修改 | 新增 inference-host-up/down/logs/ps/test/install-llamacpp |
| 环境 | `hivemtk/.env-example` | 修改 | 移除 TEI 相关变量，新增 HIVEMTK_MODELS_DIR / LLAMACPP_BIN |
| 前端 | `hivemtk/user-web/.env.development` | 修改 | 保持相对路径，无需改动 |
| 前端 | `hivemtk/user-web/.env.example` | 保留 | 解释注释补充 |
| 文档 | `hivemtk/docs/architecture/HOST_SETUP.md` | 新增 | 宿主机一键部署说明（端到端） |

### 2.3 不动的文件

- `hivemtk/user-server/internal/**`：所有 LLM/embedding/rerank 调用方都走 OpenAI 兼容 HTTP，无需改 Go 代码。
- `hivemtk/user-server/cmd/api/main.go`：`llm.NewDispatcherFromConfig` 仍读 `config.yaml.inference.*`，无需改。
- `hivemtk/user-web/src/**`：前端只调 user-server `/api/...`，不直接接触 llama-server，0 改动（仅注释与 .env 解释）。
- `hivemtk-platform/**`：本次范围仅限用户端。

---

## 三、关键设计

### 3.1 llama.cpp 安装方式

通过环境变量 `LLAMACPP_BIN` 指定路径，或脚本自动探测：

```bash
# 探测顺序
1. $LLAMACPP_BIN  环境变量
2. /opt/homebrew/bin/llama-server   (Apple Silicon brew)
3. /usr/local/bin/llama-server     (Intel brew)
4. /usr/bin/llama-server           (apt)
5. which llama-server              (PATH 内)
```

若都失败，`install-llama-cpp.sh` 自动安装（macOS 走 brew，Linux 走源码 cmake）。

### 3.2 端口分配（与旧架构一致，零业务侧改动）

| 服务 | 端口 | 协议 | user-server 配置项 |
|------|------|------|-------------------|
| LLM (llama-server) | **8207** | OpenAI `/v1/chat/completions` | `inference.llm.base_url=http://127.0.0.1:8207/v1` |
| Embedding (llama-server) | **8208** | OpenAI `/v1/embeddings` | `inference.embedding.base_url=http://127.0.0.1:8208/v1` |
| Rerank (llama-server) | **8209** | OpenAI `/v1/rerank` | `inference.rerank.base_url=http://127.0.0.1:8209` |
| mtk-postgres | **8202** (host) → 5432 (container) | TCP | `database.postgres.host=127.0.0.1,port=8202` |
| mtk-redis | **8203** (host) → 6379 (container) | TCP | `REDIS_HOST=127.0.0.1,REDIS_PORT=8203` |
| user-server | 8204 | HTTP | `PORT=8204`（监听主机默认 `0.0.0.0`，`SERVER_HOST=127.0.0.1` 可收回本机） |
| user-web (Vite dev) | 5173 | HTTP | Vite 默认 |

### 3.3 Dev / Prod 模型选型

#### dev（默认，个人电脑 / CI）

| 角色 | ModelScope 仓库 | 文件 | 量化 | 内存 |
|------|----------------|------|------|------|
| LLM | `Qwen/Qwen2.5-3B-Instruct-GGUF` | `qwen2.5-3b-instruct-q4_k_m.gguf` | Q4_K_M | ~2.5G |
| Embedding | `Xorbits/bge-m3-gguf` | `bge-m3-q4_k_m.gguf` | Q4_K_M | ~2.5G (1024 维) |
| Rerank | `gaoshengwinner/bge-reranker-v2-m3-Q4_K_M-GGUF` | `bge-reranker-v2-m3-Q4_K_M.gguf` | Q4_K_M | ~1.0G |

#### prod（推荐，8C16G+ 生产服务器）

| 角色 | ModelScope 仓库 | 文件 | 量化 | 内存 |
|------|----------------|------|------|------|
| LLM | `Qwen/Qwen2.5-14B-Instruct-GGUF` | `qwen2.5-14b-instruct-q4_k_m.gguf` | Q4_K_M | ~10G |
| Embedding | `Xorbits/bge-m3-gguf` | `bge-m3-f16.gguf`（高精度） | F16 | ~4.5G (1024 维) |
| Rerank | `gaoshengwinner/bge-reranker-v2-m3-Q4_K_M-GGUF` | `bge-reranker-v2-m3-Q8_0.gguf` | Q8_0 | ~1.6G |

> 允许覆盖：在 `models.env` 中改 `LLM_REPO` / `LLM_FILE` / `EMBEDDING_REPO` / `EMBEDDING_FILE` / `RERANK_REPO` / `RERANK_FILE` 即可。
>
> 关键铁律：**embedding 维度必须 1024**（pgvector `vector(1024)`），因此不允许换其他维度的模型；如需替换请同步执行 `ALTER TABLE knowledge_embeddings ALTER COLUMN embedding TYPE vector(NEW_DIM)`。

### 3.4 下载源策略（ModelScope 优先）

**顺序**（`DOWNLOAD_SOURCE` 环境变量，默认 `modelscope,hf-mirror,hf`）：

1. `modelscope`：`https://modelscope.cn/models/{repo}/resolve/master/{file}`（国内可达、稳定、速度快）
2. `hf-mirror`：`https://hf-mirror.com/{repo}/resolve/main/{file}`（HF 国内镜像）
3. `hf`：`https://huggingface.co/{repo}/resolve/main/{file}`（海外，需 `HF_TOKEN`）

**下载逻辑**（继承自 `scripts/inference/download_models.sh`）：
- 断点续传（`curl -C -`）
- 校验非空
- 已存在非空则跳过
- 失败可读错误信息

### 3.5 user-server 与 user-web 配置策略

#### user-server 端

**两种模式**：
- **宿主机开发模式**：`config-host.yaml`（air 热更新用），所有 base_url 指向 `127.0.0.1`
- **Docker 全栈模式**：`config.yaml`（保留，向后兼容），所有 base_url 指向容器服务名

**默认 `config.yaml`**（user-server/cmd/api 启动时优先读）改为宿主机版本（因为现在 user-server 默认跑在宿主机）。

#### user-web 端

- **无需改代码**：user-web 不直接接触 llama-server，只走 user-server。
- **无需改 .env**：开发模式 Vite 代理 `/api` → `http://localhost:8204`（user-server），user-server 再转 llama-server。
- **可选增强**：在用户端"系统设置 → LLM 接入"页面（已存在）允许运行时覆盖 base_url。

### 3.6 启动与停止

**一键启动**：

```bash
# 1. 安装 llama.cpp（首次）
bash scripts/inference-host/install-llama-cpp.sh

# 2. 下载模型（首次或切换 profile）
HIVEMTK_PROFILE=dev bash scripts/inference-host/download-models.sh

# 3. 启动 PG + Redis（docker compose 仅 2 个服务）
docker compose -f docker-compose.yml up -d

# 4. 启动 llama-server 三件套
bash scripts/inference-host/start-all.sh

# 5. 启动 user-server（air 热更新）
cd user-server && air

# 6. 启动 user-web
cd user-web && npm run dev
```

**等价 Makefile**：

```bash
make install-llamacpp
make models-download
make db-up
make inference-host-up
make dev
```

### 3.7 预热（避免首请求慢）

llama.cpp 在首次推理时需 KV-cache 编译（约 1~5s）。`warmup.sh` 在三服务 health-check 通过后对每个端点各发一个极小请求：

```bash
# 触发模型实际计算（仅一次）
curl -s -X POST http://127.0.0.1:8207/v1/chat/completions \
  -d '{"model":"local","messages":[{"role":"user","content":"hi"}],"max_tokens":2}' >/dev/null
curl -s -X POST http://127.0.0.1:8208/v1/embeddings \
  -d '{"model":"bge-m3","input":"warmup"}' >/dev/null
curl -s -X POST http://127.0.0.1:8209/v1/rerank \
  -d '{"model":"bge-reranker-v2-m3","query":"hi","documents":["hello"]}' >/dev/null
```

### 3.8 llama-server 关键参数

```bash
# LLM
--model $LLM_FILE
--host 0.0.0.0
--port 8207
-c 8192              # 上下文窗口
-ngl 999             # GPU 全部卸载（CPU 时设 0）
--threads 0          # 自动检测
--jinja              # Qwen2.5 chat template（最新版需要）

# Embedding
--model $EMB_FILE
--host 0.0.0.0
--port 8208
--embeddings         # 开启 embeddings 模式
--pooling mean       # bge-m3 用 mean pooling（llama.cpp 默认 mean）

# Rerank
--model $RERANK_FILE
--host 0.0.0.0
--port 8209
--reranking          # 开启 rerank 模式
```

> 注：bge-m3 与 bge-reranker-v2-m3 都用 `--pooling mean`（llama.cpp 当前默认即 mean，无需显式）。llama.cpp 端点 `/v1/embeddings` 与 `/v1/rerank` 都已内置 OpenAI 兼容。

---

## 四、验证

### 4.1 静态检查

```bash
# bash 语法
bash -n scripts/inference-host/*.sh

# go build
cd user-server && go build ./...
```

### 4.2 运行时 smoke test

```bash
# 1. 启动数据库
docker compose -f docker-compose.yml up -d

# 2. 启动推理
make inference-host-up

# 3. 等待 health
for p in 8207 8208 8209; do
  until curl -fsS http://127.0.0.1:$p/health >/dev/null 2>&1; do sleep 1; done
done

# 4. 预热
make inference-host-warmup

# 5. 端点连通
make inference-host-test
# 期望：3 个端点全 ✅，embedding 维度=1024
```

### 4.3 业务侧端到端

```bash
# 启动 user-server (air)
cd user-server && air &

# 启动 user-web
cd user-web && npm run dev &

# 模拟用户聊天
curl -X POST http://localhost:8204/api/ai/chat \
  -H "Content-Type: application/json" \
  -d '{"message":"用一句话介绍杭州"}'
# 期望：返回 Qwen2.5 生成的回复
```
