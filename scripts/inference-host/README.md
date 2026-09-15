# HiveMtk 宿主机推理栈 —— 使用说明

> ⭐ **推荐方案**(2026-07-24 起):本目录是当前推荐的推理栈部署方式,基于宿主机 llama.cpp,非容器化,节省 CPU/内存。
>
> 📦 **与 `scripts/inference/` 的关系**:`scripts/inference/` 是旧版 Docker 容器化推理栈(已弃用,保留兼容)。新部署请使用本目录;旧部署可参考 `scripts/inference/README.md` 了解历史方案。
>
> 完整方案：[../../docs/architecture/HOST_INFERENCE_PLAN.md](../../docs/architecture/HOST_INFERENCE_PLAN.md)

## 一、文件清单

| 文件 | 用途 |
|------|------|
| `env.sh` | 共享环境变量（路径、端口、profile、llama.cpp 探测） |
| `models.env` | dev / prod 模型定义（仓库 ID + 文件名 + 量化） |
| `install-llama-cpp.sh` | 安装 llama.cpp（brew / apt / 源码兜底） |
| `download-models.sh` | ModelScope 优先下载模型（断点续传） |
| `start-llm.sh` | 启动 LLM llama-server (port 8207) |
| `start-embedding.sh` | 启动 Embedding llama-server (port 8208) |
| `start-rerank.sh` | 启动 Rerank llama-server (port 8209) |
| `start-all.sh` | 一键拉起三服务 |
| `stop-all.sh` | 一键停止三服务 |
| `warmup.sh` | 预热（避免首请求慢） |
| `smoke-test.sh` | 三端点连通性测试 |
| `_common.sh` | start/stop/warmup 共享辅助函数 |

## 二、目录约定

模型文件默认保存在**本项目 `hivemtk/models/` 下**（`.gitignore` 已忽略，不会误提交大文件）；运行时 pid/log 保存在 `~/.hivemtk/runtime/`（运行时产物不进项目仓库）。

```
hivemtk/                          # 项目根
├── models/                       # ← 模型文件（项目内，.gitignore 已忽略）
│   ├── llm/                      # LLM gguf 文件
│   ├── embedding/                # embedding gguf 文件
│   └── rerank/                   # rerank gguf 文件
├── scripts/inference-host/       # ← 启动脚本（本项目内）
└── ...

~/.hivemtk/
└── runtime/                      # pid / log 文件（运行时产物）
    ├── llm.pid  llm.log
    ├── embedding.pid  embedding.log
    └── rerank.pid  rerank.log
```

可通过环境变量覆盖（生产部署常用）：

```bash
# 模型目录：默认 $PROJECT_ROOT/models，生产可指向独立数据盘
export HIVEMTK_MODELS_DIR=/data/hivemtk/models
# 运行时目录：默认 $HOME/.hivemtk/runtime
export HIVEMTK_RUNTIME_DIR=/data/hivemtk/runtime
```

## 三、首次部署

```bash
# 1. 安装 llama.cpp（macOS 走 brew，Linux 走 apt 或源码）
bash scripts/inference-host/install-llama-cpp.sh

# 2. 下载模型（默认 dev 档；切 prod 档用环境变量）
HIVEMTK_PROFILE=dev bash scripts/inference-host/download-models.sh

# 3. 启动推理栈
bash scripts/inference-host/start-all.sh

# 4. 预热
bash scripts/inference-host/warmup.sh

# 5. 验证
bash scripts/inference-host/smoke-test.sh --strict
```

## 四、日常使用

```bash
# 启动
make inference-host-up
# 或
bash scripts/inference-host/start-all.sh

# 停止
make inference-host-down
# 或
bash scripts/inference-host/stop-all.sh

# 查看日志
make inference-host-logs
# 或
tail -F ~/.hivemtk/runtime/{llm,embedding,rerank}.log

# 查看进程
make inference-host-ps
# 或
ps aux | grep llama-server

# 重启单个服务（如 LLM）
bash scripts/inference-host/stop-all.sh
bash scripts/inference-host/start-llm.sh
```

## 五、dev / prod 切换

```bash
# 切到 prod 档（重模型，需 16G+ 内存）
HIVEMTK_PROFILE=prod bash scripts/inference-host/download-models.sh
HIVEMTK_PROFILE=prod bash scripts/inference-host/start-all.sh

# 切回 dev 档
HIVEMTK_PROFILE=dev bash scripts/inference-host/download-models.sh
HIVEMTK_PROFILE=dev bash scripts/inference-host/start-all.sh
```

## 六、模型自定义

如需使用其他 ModelScope / HF 上的 GGUF 仓库，直接覆盖环境变量：

```bash
export LLM_REPO="Qwen/Qwen2.5-7B-Instruct-GGUF"
export LLM_FILE="qwen2.5-7b-instruct-q4_k_m.gguf"
bash scripts/inference-host/download-models.sh llm
bash scripts/inference-host/start-llm.sh
```

**铁律**：
- `EMBEDDING_DIM` 必须保持 1024（与 pgvector `vector(1024)` 一致）。换其他维度需先 `ALTER TABLE`。
- 推荐使用 Q4_K_M 量化（速度 / 质量 / 内存最佳平衡点）。
- GGUF 文件需与 llama.cpp 版本兼容（Q4_K_M 是稳定的低风险选择）。

## 七、端口与依赖

| 服务 | 端口 | 协议 | user-server 配置 |
|------|------|------|-----------------|
| LLM | 8207 | OpenAI `/v1/chat/completions` | `inference.llm.base_url=http://127.0.0.1:8207/v1` |
| Embedding | 8208 | OpenAI `/v1/embeddings` | `inference.embedding.base_url=http://127.0.0.1:8208/v1` |
| Rerank | 8209 | OpenAI `/v1/rerank` | `inference.rerank.base_url=http://127.0.0.1:8209/v1` |
| PostgreSQL | 8202 | TCP | `database.postgres.host=127.0.0.1` |
| Redis | 8203 | TCP | `REDIS_HOST=127.0.0.1` |

## 八、故障排查

| 现象 | 排查 |
|------|------|
| 启动后立即退出 | 查看 `~/.hivemtk/runtime/<role>.log` |
| /health 200 但 /v1/... 返回 503 | 显存/内存不足，调小 `-b` 或 `--ctx-size` |
| 模型下载失败 | 检查网络；尝试 `DOWNLOAD_SOURCE=hf` |
| embedding 维度不匹配 | 改用 `bge-m3` 系列（1024 维）；或同步修改 pgvector 维度 |
| llama-server 启动报 unknown argument | 旧版 llama.cpp 不支持 `--jinja` / `--reranking` / `--flash-attn`；升级到 b3000+ |
| `--mlock` 启动失败 | 物理内存不足；设 `USE_MLOCK=false` 关闭 |
| `--flash-attn` 不兼容 | 部分老模型不支持；设 `FLASH_ATTN=off` 关闭 |

## 九、性能调优

### 9.1 默认已启用的优化

以下优化参数默认启用（可通过环境变量关闭）：

| 参数 | 环境变量 | 默认值 | 效果 |
|------|----------|--------|------|
| `--flash-attn on` | `FLASH_ATTN` | `on` | Flash Attention 2，加速推理 2-4x，减 KV cache 内存 50%+ |
| `--mlock` | `USE_MLOCK` | `true` | 锁定模型在 RAM，防止换页导致延迟飙升 |
| `--timeout 300` | `SERVER_TIMEOUT` | `300` | HTTP 读写超时 300s，防止慢请求占用资源 |
| `--metrics` | `ENABLE_METRICS` | `false` | llama.cpp 内置 /metrics 端点 (私域: 默认关闭) |
| `--alias` | `USE_ALIAS` | `true` | 模型别名，确保 API 的 `model` 字段与 config.yaml 一致 |
| `--ubatch` | `UBATCH_SIZE` | `512` | 物理批处理大小（GPU 可调到 1024） |
| LLM `--cont-batching` | `LLM_CONT_BATCHING` | `true` | 连续批处理，新请求插入正在处理的批次 |
| LLM `--parallel 2` | `LLM_PARALLEL` | `2` | LLM 并行槽位数，允许同时处理 2 个请求 |
| LLM `-ctk/-ctv f16` | `CACHE_TYPE_K/V` | `f16` | KV cache 量化（内存紧张可改 `q8_0` 减 50% 几乎无损） |

### 9.2 并发调优

embedding/rerank 默认单并发（`EMBEDDING_PARALLEL=1` / `RERANK_PARALLEL=1`）。
如需提高 embedding 并发（如多文档批量上传场景），需**同步**调高两端：

```bash
# 1. llama-server 端（env.sh）
export EMBEDDING_PARALLEL=4        # llama-server --parallel 4

# 2. Go 客户端闸门（user-server 端）
export EMBEDDING_CONCURRENCY=4     # Go embeddingSem 容量 4

# 3. 重启两端
bash scripts/inference-host/stop-all.sh
bash scripts/inference-host/start-all.sh
# 重启 user-server（air 会自动重启）
```

> 注意：embedding 是 CPU bound，并发过高会因内存带宽竞争反而变慢。建议 2-4 即可。

### 9.3 GPU 加速

Apple Silicon（M1/M2/M3/M4）默认已启用 Metal 加速（`-ngl 999`）：
```bash
export NGL=999    # 全部层卸载到 GPU
```

NVIDIA GPU 需安装 CUDA 版 llama.cpp，然后：
```bash
export NGL=999    # 全部层卸载到 VRAM
export UBATCH_SIZE=1024  # GPU 可处理更大 ubatch
```

### 9.4 warmup 预热策略

`warmup.sh` 已优化为**并行预热**三服务（总耗时 = max 而非 sum），每服务发多轮不同长度请求：
- LLM：3 轮（短/中/长 prompt + system 消息）
- Embedding：2 轮（单条/批量 5 条）
- Rerank：2 轮（短候选 2 条/长候选 8 条）

预热后业务首请求可享受亚秒级响应（避免 KV-cache 首次编译延迟）。

### 9.5 内置 /metrics 端点 (默认关闭)

> 私域部署: `ENABLE_METRICS` 默认 `false`, **不启用** llama.cpp `/metrics` 端点。
> 三服务 (LLM / Embedding / Rerank) 的容量指标通过:
> - `slots_in_use` / `n_slots` 探针: `curl http://127.0.0.1:8207/health` 返回 JSON
> - 应用层日志: `user-server` 每次 LLM 调用落库 `llm_routing_logs`, 巡检通过 SQL
> - 健康检查: `/health` `/healthz` `/readyz` 端点

如需开启, 需显式设置 `ENABLE_METRICS=true` 并自行对接外部抓取, 私域基线**不依赖**此通道。

## 十、依赖

- llama.cpp ≥ b3000（支持 `--flash-attn` / `--cont-batching` / `--metrics`）
- macOS 13+ / Ubuntu 22.04+ / Debian 12+
- 内存：dev 档 ≥ 8G，prod 档 ≥ 16G
- 磁盘：dev 档 ≥ 6G，prod 档 ≥ 18G（仅模型）

## License

本项目以 AGPL-3.0 发布，详见 [../LICENSE](../../LICENSE) 与 [../NOTICE](../../NOTICE)。
