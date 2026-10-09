# HiveMtk 性能基准与 SLA 阐明（2026-08-15 M3-P1-E9）

> 性能测试方法论、目标值、实测基线、调优指南。

> ⚠️ **§2 的实测表是 2026-08-15 的历史快照**（单机：4C8G SSD / 无 GPU；3 副本：3×4C8G；hivemtk 仓 `master`，约 commit `7d8ed832` 前后）。本档只重写快照数字之外的可执行部分。2026-10-09 按现行代码逐条复核端点/命令/SQL，结论如下（均为现跑 grep + `information_schema` 取证）：
>
> - **user-server 监听 8204**（`docs/PORT_REGISTRY.md`），不是 8080；§4.4 的 curl/wrk 口已对，pprof 见下。
> - **`POST /api/bridge/ack` 路径有误**：真实注册是 `POST /api/bridge/outbox/ack`（`internal/router/router.go:564`，handler `AckBridgeOutbox`）。
> - **`GET /api/bridge/outbox` 不是长轮询**：`internal/bridge/handler_http.go:782 GetBridgeOutbox` 只做一次 `ClaimPendingOutbound` 快照查询后立即返回，无 `lp`/超时挂起参数；本仓**没有长轮询实现**（`grep -rn "LongPoll\|long_poll\|/poll" internal/router/ internal/controller/` 零命中）。表里那行 "lp=30 / P50 30s" 记的是长轮询时代的语义，现网复现不出，出站实时链路是 SSE（`GET /api/bridge/outbox/sse`，`router.go:566`）。
> - **`GET /api/team/users`、`POST /api/agent/dispatch` 在现行 `internal/router/` 里 0 注册**（`grep` 全树无 `/team`、无 `dispatch` HTTP 路由）；最接近的真实端点分别是 `GET /api/users`（`internal/router/auth_routes.go:56`）和 `POST /api/ai-agents/:id/test`（`internal/router/router.go:721` → `controller/ai_agent.go:39`）。这两行 P99 属历史口径，照抄无法命中。
> - **`go tool pprof http://localhost:8204/debug/pprof/…` 打不通**：user-server 全仓未注册 `net/http/pprof`（`grep -rn pprof user-server --include=*.go` 非测试 0 命中）；要做 CPU/heap profile 需自行挂 pprof 或用调试构建，§4.4 第 6 步已据此改写。
> - **附录 B 的 `bridge_ingest_duration_ms` / `bridge_ingest_logs` / `bridge_ingest_duration_ms_bucket` 三张表都不存在**（`information_schema.tables` 查无）；bridge 时延/错误只进进程内自研指标注册表（`internal/pkg/metrics`，`/metrics` 未挂路由）。可查的性能事实源是 `layer_decision_logs` / `llm_routing_logs` / `rag_query_logs`。附录 B SQL 已改用真实表。
> - **§4.3 测试数据 SQL 的 `-U hivemtk -d hivemtk` 与表名 `conversations` 都是错的**：库角色/名走 `.env`（`POSTGRES_USER`/`USER_DB_NAME`，现值 `admin`/`user_db`，宿主机映射口取 `USER_POSTGRES_HOST_PORT`，现 8202）；无 `conversations` 表，真实会话表是 `inbox_conversations`（`internal/model/ai_sales_champion.go:447`）与 `customer_sessions`。已按真实列改写。

---

## 1. 测试工具

| 工具 | 用途 | 安装 |
|------|------|------|
| k6 | 现代 HTTP 压测，支持 JS 脚本 | `brew install k6` / `apt install k6` |
| wrk | 轻量 HTTP 压测 | `brew install wrk` |
| vegeta | 恒定 RPS 压测 | `brew install vegeta` |
| ab | 简单压测 | 内置 |
| hey | Go 写的压测 | `go install github.com/rakyll/hey@latest` |
| pprof | Go 性能分析 | Go 标准库 |
| pgbench | PostgreSQL 压测 | 内置 |

---

## 2. 性能基线（2026-08-15 实测）

### 2.1 单机（开发机：4C8G SSD，无 GPU）

| 端点 | VU | RPS | P50 | P95 | P99 | 错误率 |
|------|----|----|-----|-----|-----|--------|
| GET /healthz | 100 | 28,000 | 3ms | 8ms | 15ms | 0% |
| GET /readyz | 100 | 1,200 | 80ms | 200ms | 350ms | 0% |
| POST /api/bridge/ingest | 50 | 1,500 | 30ms | 80ms | 150ms | 0% |
| GET /api/bridge/outbox † | 20 | 35 | 30s | 30s | 30s | 0% |
| POST /api/bridge/outbox/ack | 30 | 800 | 35ms | 90ms | 180ms | 0% |
| GET /api/team/users † | 50 | 2,500 | 18ms | 45ms | 80ms | 0% |
| POST /api/agent/dispatch (AI) † | 10 | 8 | 1.2s | 2.5s | 4s | 0% |

> † 端点语义与现行代码不符，见文首勘误：outbox 现为快照查询（非 `lp=30` 长轮询，P50 30s 复现不出）；`/api/team/users`、`/api/agent/dispatch` 现行 `internal/router/` 未注册（真实最近端点 `GET /api/users`、`POST /api/ai-agents/:id/test`）。数字保留为 2026-08-15 历史值。

### 2.2 3 副本（生产推荐配置：3 × 4C8G）

| 端点 | VU | RPS | P50 | P95 | P99 |
|------|----|----|-----|-----|-----|
| GET /healthz | 300 | 80,000 | 3ms | 8ms | 15ms |
| POST /api/bridge/ingest | 150 | 4,500 | 30ms | 80ms | 150ms |
| GET /api/bridge/outbox † | 60 | 100 | 30s | 30s | 30s |
| POST /api/bridge/outbox/ack | 90 | 2,400 | 35ms | 90ms | 180ms |
| POST /api/agent/dispatch † | 30 | 24 | 1.2s | 2.5s | 4s |

> † 同 §2.1 勘误：outbox 非长轮询；`/api/agent/dispatch` 现行路由未注册。

---

## 3. SLO 目标

详见 [SLA_SLO.md](SLA_SLO.md)。核心指标：

| 指标 | 目标 | 备注 |
|------|------|------|
| bridge_ingest availability | 99.9% | 月度 ≤ 43 分钟停机 |
| bridge_ingest latency P95 | ≤ 1s | 1MB body |
| bridge_ingest latency P99 | ≤ 2s | 1MB body |
| bridge_dlq_rate | < 0.1% | 平台可能降权 |
| http_requests availability | 99.9% | 全平台 |

---

## 4. 性能测试方法

### 4.1 测试分类

| 类型 | 目的 | 工具 | 频率 |
|------|------|------|------|
| 冒烟测试 | 验证基本可用 | ab | 每次部署 |
| 基准测试 | 建立基线 | k6 / wrk | 每次发版 |
| 负载测试 | 验证 SLA | k6 / wrk | 每周 |
| 压力测试 | 找瓶颈 | k6 / wrk / vegeta | 每月 |
| 容量测试 | 找容量上限 | k6 阶梯 | 每月 |
| 持久测试 | 测稳定性 | k6 24h soak | 每季度 |
| 峰值测试 | 应对营销活动 | k6 突发 | 季度 |

### 4.2 测试场景

| 场景 | 模拟 | VU | Duration |
|------|------|-----|----------|
| 日常 | 100 个客服在线 | 100 | 30min |
| 高峰 | 500 个客服在线 | 500 | 30min |
| 突发 | 营销活动 1k 客服 | 1000 | 5min |
| 极端 | 双 11 | 3000 | 1min |

### 4.3 测试数据

```bash
# 准备 1 万测试账号 / 10 万会话（仅 dev 环境）
# 连接参数走 .env：库角色/名不是 hivemtk，宿主机映射口也不是默认的 5432。在仓库根执行：
set -a; . ./.env; set +a
export PGHOST="${DB_HOST:-127.0.0.1}" PGPORT="${USER_POSTGRES_HOST_PORT:-8202}" \
       PGUSER="$POSTGRES_USER" PGDATABASE="$USER_DB_NAME"
export PGPASSWORD="$POSTGRES_PASSWORD"   # 口令只进环境变量，别写进命令行/日志/本文档

psql -v ON_ERROR_STOP=1 <<'SQL'
-- bridge_accounts 的真实 NOT NULL 列是 user_id / channel / account_id / account_name /
-- agent_id / status（id 走 bridge_accounts_id_seq 默认，无需手填；agent_id 是 bigint，
-- 旧写法把 'test-agent-N' 字符串塞进去会类型报错）。以下已在 dev 库 EXPLAIN 校验通过：
INSERT INTO bridge_accounts (user_id, channel, account_id, account_name, agent_id, status, created_at)
SELECT
  (i % 10) + 1,
  (ARRAY['douyin','xiaohongshu','tiktok','xianyu'])[1 + (i % 4)],
  'test-acc-' || i,
  '测试账号-' || i,
  (i % 10) + 1,
  'active',
  NOW()
FROM generate_series(1, 10000) AS s(i);

-- 没有 conversations 这张表；真实会话表是 inbox_conversations（NOT NULL：
-- platform / account_id / customer_id）。旧列名 channel→platform，并补上必填的 customer_id。
INSERT INTO inbox_conversations (platform, account_id, customer_id, conversation_id, created_at)
SELECT
  (ARRAY['douyin','xiaohongshu','tiktok','xianyu'])[1 + (i % 4)],
  'test-acc-' || ((i % 10000) + 1),
  'test-cust-' || i,
  'test-conv-' || i,
  NOW()
FROM generate_series(1, 100000) AS s(i);
SQL
```

### 4.4 测试流程

```bash
# 1. 准备
make dev
# 等待 /readyz 通过
until curl -fsS http://localhost:8204/readyz; do sleep 2; done

# 2. 冷启动基线（无 DB 连接池）
wrk -t4 -c100 -d30s --latency http://localhost:8204/healthz

# 3. 业务基线
k6 run --vus 50 --duration 60s scripts/perf/bridge-load.js

# 4. 阶梯压测（找拐点）
for vus in 10 50 100 200 500 1000; do
    echo "=== VU=$vus ==="
    k6 run --vus $vus --duration 30s scripts/perf/bridge-load.js
done

# 5. 持久测试
k6 run --vus 100 --duration 24h scripts/perf/bridge-load.js

# 6. 收集 pprof —— 注意：user-server 未注册 net/http/pprof（全仓 grep 非测试 0 命中），
#    直接打 http://localhost:8204/debug/pprof/... 会得到 SPA 兜底页而非 profile。
#    要 CPU/heap 采样，先在调试分支给 main 挂上 pprof（import _ "net/http/pprof" + 独占监听口），
#    或改用 OS 级采样。下面这行是「挂上之后」的形态，现网默认跑不通：
# go tool pprof http://localhost:8204/debug/pprof/profile?seconds=30
# 未接线时的 OS 级替代（按宿主系统择一，无需改应用）：Linux 用 perf record -g -p $(pgrep -f user-server) -- sleep 30；macOS 用 sample $(pgrep -f user-server) 30
```

---

## 5. 关键优化点

### 5.1 数据库

| 优化 | 效果 |
|------|------|
| 索引 | P95 从 200ms → 20ms |
| 连接池调优 | 减少连接等待 |
| 预编译语句 | 减少 30% 查询时间 |
| 批量插入 | 100x 提升 |
| 读副本分离 | 减少主库压力 |

### 5.2 缓存

| 优化 | 效果 |
|------|------|
| Redis 缓存热点 | 减少 80% DB 查询 |
| 本地缓存 (in-memory LRU) | 减少 95% Redis 查询 |
| 缓存预热 | 避免冷启动慢 |

### 5.3 并发

| 优化 | 效果 |
|------|------|
| goroutine pool | 减少内存占用 |
| 协程复用 | 减少创建销毁 |
| 异步写入 | 不阻塞请求 |

### 5.4 网络

| 优化 | 效果 |
|------|------|
| HTTP keep-alive | 减少 50% RTT |
| SSE / WebSocket 长连接（本仓实况：出站 `/api/bridge/outbox/sse`、访客 `/api/ws/visitor`；未实现长轮询）| 减少 90% 轮询开销 |
| 批量 API | 减少 N+1 调用 |
| gzip / brotli | 减少 70% 流量 |

### 5.5 AI 推理

| 优化 | 效果 |
|------|------|
| 模型量化 (Q5_K_M) | 内存 50%，速度 30% |
| 批量推理 | 4x 吞吐 |
| KV cache 复用 | 减少 30% 时间 |
| 流式响应 | TTFT < 200ms |

---

## 6. 性能基线对标

| 平台 | 私域 RPS | 1k 客服 | 1w 客服 | 备注 |
|------|---------|---------|---------|------|
| HiveMtk (本项目) | 80k | ✓ | ✓ | 单机 28k / 3 副本 80k |
| 某 SaaS 客服 | - | ✓ | ✗ | 公有云 |
| 套壳 AI | - | ✗ | ✗ | 单机 100 |
| 自动化脚本 | - | △ | ✗ | 取决于脚本 |

---

## 7. 性能退化告警

| 指标 | 基线 | 告警阈值 |
|------|------|---------|
| P95 latency | 80ms | > 200ms |
| 错误率 | 0% | > 0.1% |
| CPU 使用率 | 50% | > 80% |
| 内存使用率 | 60% | > 85% |
| DB 连接数 | 50 | > 200 |
| 缓存命中率 | 90% | < 70% |

---

## 8. 性能报告

每次发版必须出性能报告：

```markdown
## 性能测试报告 - v1.1.0 (2026-08-15)

### 测试环境
- 机器：3 副本 × 4C8G
- 数据：1 万账号 / 10 万会话
- 工具：k6 v0.50

### 测试结果
| 场景 | 目标 | 实测 | 通过 |
|------|------|------|------|
| ingest P95 | ≤ 1s | 80ms | ✓ |
| ingest availability | 99.9% | 100% | ✓ |
| 1000 VU 突发 | P95 ≤ 2s | 1.2s | ✓ |

### 回归
- 比 v1.0.0 P95 提升 15ms（优化了 audit 写入）
- 内存占用减少 50MB（去除了旧的 logger 中间件）

### 结论
发布通过
```

---

## 9. 附录

### 附录 A：测试账号生成

```bash
# 生成 1000 个测试 token
for i in $(seq 1 1000); do
    echo "test-token-$i"
done > /tmp/test-tokens.txt
```

### 附录 B：性能指标 SQL 查询

> **勘误**：不存在 `bridge_ingest_duration_ms` / `bridge_ingest_logs` / `bridge_ingest_duration_ms_bucket` 这些表（`information_schema.tables` 现查无）；bridge 入站/出站的时延与错误只写进进程内自研指标 `internal/pkg/metrics`（`/metrics` 未挂路由，不能 SQL 查）。可 SQL 查的性能事实源是：`layer_decision_logs`（`wall_ms`）、`llm_routing_logs`（`latency_ms` / `success` / `is_fallback`）、`rag_query_logs`（`latency_ms` / `hit_count` / `precision` / `recall`）。连接参数见 §4.3 的 `.env` 导出块；以下四条已在 dev 库 `user_db` 跑通（无近期数据时返回空值，非报错）。

```sql
-- LLM 推理时延 P95（最近 5 分钟）
SELECT percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms) AS llm_p95_ms
FROM llm_routing_logs
WHERE created_at > NOW() - INTERVAL '5 minutes';

-- LLM 错误率（success 是 boolean；最近 5 分钟）
SELECT COUNT(*) FILTER (WHERE NOT success)::float / NULLIF(COUNT(*), 0) AS llm_error_rate
FROM llm_routing_logs
WHERE created_at > NOW() - INTERVAL '5 minutes';

-- 吞吐量（决策条数，最接近旧「throughput」口径；最近 5 分钟）
SELECT COUNT(*) AS decisions
FROM layer_decision_logs
WHERE created_at > NOW() - INTERVAL '5 minutes';

-- RAG 检索时延 P95 + 平均命中数（最近 5 分钟）
SELECT percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms) AS rag_p95_ms,
       AVG(hit_count) AS avg_hit_count
FROM rag_query_logs
WHERE created_at > NOW() - INTERVAL '5 minutes';
```

---

> 配套：[k6 压测脚本](../../scripts/perf/bridge-load.js) · [wrk 压测脚本](../../scripts/perf/wrk-bench.sh) · [SLA 承诺](SLA_SLO.md)
