# HiveMtk 服务等级承诺 (SLA)

> 单商户本地部署的服务承诺。适用于私有化部署场景。

---

## 1. 服务等级定义

| 等级 | 可用性 | 响应时间 | 适用场景 |
|------|--------|---------|---------|
| P0 - 紧急 | - | < 1 小时 | 系统完全不可用 |
| P1 - 高 | - | < 4 小时 | 核心功能异常 |
| P2 - 中 | - | < 24 小时 | 非核心功能异常 |
| P3 - 低 | - | < 72 小时 | 界面问题、优化建议 |

## 2. 可用性承诺

| 指标 | 目标 | 说明 | 本仓的取数口径 |
|------|------|------|---------------|
| 系统可用性 | ≥ 99% | 月度计算，排除计划维护 | 无自动打点，由 §3 的维护记录 + 故障工单时长人工汇总 |
| API 响应时间 | P95 < 500ms | 不含 LLM 推理时间 | `api_logs.duration`（`user-server/internal/model` 里的 ApiLog，列 `path/method/status_code/duration/created_at`），按 §6.3 的 SQL 离线算 |
| 数据持久性 | 100% | 备份数据不丢失 | 备份闭环见 [DR_RECOVERY.md](DR_RECOVERY.md)；本机实测的恢复校验（恢复到临时库 + 表数/行数断言）在其 §4.1 |

> 取数现实（必须按这个口径对外承诺，不要按「有监控系统」写）：
> 本仓**不接入任何外部 Prometheus / APM / 托管告警通道**。
> - `internal/pkg/metrics` 的 `/metrics` 文本端点只有包内 `Handler()`，
>   **没有注册进路由**（实测 `curl http://127.0.0.1:8204/metrics` 返回 404），抓不了；
> - `internal/pkg/sla` 的 `SLOTracker` 定义了 SLO/错误预算，但全仓**没有生产装配点**
>   （`grep -rn "hivemtk-user/internal/pkg/sla" --include=*.go` 只命中该包自己和它的测试），
>   所以 §7 的 SLO **不是运行时自动产出的**，只能靠下面的 SQL 离线统计；
> - 唯一的自动告警是应用内的那套：`internal/service/alert_checker.go`
>   （文件头自述「不依赖外部 Alertmanager；通知由注入的 AlertNotifier 实现（邮件 / 钉钉 / webhook）」），
>   规则与历史落在本仓的 `alert_rules` / `alert_histories` 两张表，读取口是 `/api/alerts/rules`、
>   `/api/alerts/histories`、`/api/monitor/alerts/unread`。


## 3. 维护窗口

| 类型 | 时间 | 通知 |
|------|------|------|
| 计划维护 | 每月第一个周六 02:00-04:00 | 提前 24 小时 |
| 紧急修复 | 随时 | 立即通知 |
| 版本升级 | 每月维护窗口 | 提前 7 天 |

## 4. 支持渠道

| 渠道 | 响应时间 | 说明 |
|------|---------|------|
| 飞书群 | < 2 小时 | 工作日 9:00-19:00 |
| 邮件 | < 24 小时 | 商户自有支持邮箱（本表是交付给终端商户的模板，由商户填写） |
| 电话 | < 1 小时 | 仅限 P0 级故障 |

## 5. 故障处理流程

```
发现 → 分类 → 响应 → 修复 → 验证 → 复盘
  │      │      │      │      │      │
  ▼      ▼      ▼      ▼      ▼      ▼
记录   P0-P3  启动   定位   重启   总结
工单         响应   修复   验证   改进
```

### 5.1 常见故障处理

本仓 Docker 只跑数据层两个容器（`mtk-postgres` / `mtk-redis`），
后端与推理三件套都是宿主机进程 —— 所以「服务进程挂掉」不能靠 `docker compose restart` 解决。

| 故障 | 处理方式 | 恢复时间 |
|------|---------|---------|
| user-server 进程挂掉 | systemd 场景 `systemctl restart hivemtk-user`；手工场景 `cd /opt/hivemtk/user-server && ./bin/user-server`（`docker compose restart` 对它无效，compose 里没这个服务） | < 5 分钟 |
| 数据层容器不健康 | `docker compose restart mtk-postgres mtk-redis`，再 `make db-ps` 看 healthy 状态 | < 15 分钟 |
| 数据库连不上 | `docker compose exec -T mtk-postgres pg_isready -U "${POSTGRES_USER}" -h 127.0.0.1 -p 8202`；注意端口是 8202 不是默认 5432 | < 15 分钟 |
| Redis 连不上 | `docker compose exec -T mtk-redis redis-cli -p 8203 -a "${REDIS_PASSWORD}" --no-auth-warning ping`（容器内监听 8203 且开了 requirepass） | < 15 分钟 |
| 磁盘空间不足 | 日志在 `user-server/logs/user-server.log`（`config.yaml` 单文件 200MB 上限）、上传在 `user-server/uploads/`、数据在卷 `mtk_user_pg_data`；`df -h` 定位后再清 | < 30 分钟 |
| LLM 推理超时 | 三件套是宿主机 llama-server；Makefile **没有 `inference-host-restart` 目标**，整组重启 = `make inference-host-down && make inference-host-up`，再 `make inference-host-status` | < 10 分钟 |
| 数据丢失 | 按 [DR_RECOVERY.md §2](DR_RECOVERY.md) 从 `pg_dump` 备份恢复（无 WAL 归档，恢复点粒度 = 上次备份时间） | < 2 小时 |


## 6. 日志与监控

### 6.1 关键日志路径

```bash
# 后端应用日志（唯一落盘文件；config.yaml: logging.output=both, level=info,
# file=logs/user-server.log, max_size=200 —— 相对 user-server 工作目录）
# 旧写法 /var/log/hivemtk/app.log 与 /var/log/hivemtk/error.log 在本仓没有任何产出点：
# 日志器只写 os.Stdout + 上面这一个文件（internal/pkg/utils/logger/logger.go），
# 错误级别不单独分文件，按 level 字段在同一份 JSON 里过滤。
tail -n 100 /opt/hivemtk/user-server/logs/user-server.log

# 数据层日志：compose 用 json-file 驱动（x-logging，max-size 100m × max-file 5），
# 容器里 PostgreSQL 的 logging_collector 实测为 off，所以宿主机没有 PG 日志文件可 tail
make db-logs                                   # PG + Redis 一起跟
docker compose logs --tail=100 mtk-postgres    # 单看 PG

# llama-server 日志（三个推理服务）
make inference-host-logs

# 审计：**没有 audit_logs 这张表**（实测 ERROR: relation "audit_logs" does not exist）。
# 真实表按 GORM TableName() 分三条，别混用：
#   operation_logs      人工操作审计（internal/model 的 TableName 返回该值，本机现 45144 行）
#   tool_call_audits    AI 工具调用审计
#   api_logs            接口访问与耗时（P95 用它）
```

```bash
cd /opt/hivemtk && set -a && . ./.env && set +a

# 操作审计（表名 operation_logs，不是 audit_logs）
docker compose exec -T mtk-postgres psql \
  -U "${POSTGRES_USER:-admin}" -d "${USER_DB_NAME:-user_db}" -h 127.0.0.1 -p 8202 \
  -c "SELECT count(*) FROM operation_logs;"

# 工具调用审计（不存在 audit_logs / tool_audit_logs / agent_tool_audit_logs 这三个名字）
docker compose exec -T mtk-postgres psql \
  -U "${POSTGRES_USER:-admin}" -d "${USER_DB_NAME:-user_db}" -h 127.0.0.1 -p 8202 \
  -c "SELECT count(*) FROM tool_call_audits;"
```

### 6.2 常用排查命令

```bash
cd /opt/hivemtk
set -a
. ./.env
set +a

# 查看数据层服务状态（compose 里只有这两个服务）
docker compose ps

# 查看后端最近错误（同一份文件里按 level 过滤，没有独立的 error.log）
grep '"level":"error"' user-server/logs/user-server.log | tail -n 50

# 查看数据库状态：服务名 mtk-postgres，PG 在容器里被改成监听 8202，
# 不带 -p 会去撞 /var/run/postgresql/.s.PGSQL.5432 直接失败
docker compose exec -T mtk-postgres pg_isready -U "${POSTGRES_USER:-admin}" -h 127.0.0.1 -p 8202

# 查看 Redis 状态：服务名 mtk-redis，端口 8203，开了 requirepass
docker compose exec -T mtk-redis redis-cli -p 8203 -a "${REDIS_PASSWORD}" --no-auth-warning ping

# 后端三个探针（8204 是 user-server 的发布口，8080 在本仓不是任何服务的端口）
curl -s http://127.0.0.1:8204/healthz   # 存活：只看进程，不看依赖
curl -s http://127.0.0.1:8204/health    # 依赖详情：database/redis/inference/embedding
curl -s http://127.0.0.1:8204/readyz    # 就绪：依赖没齐回 503

# 推理栈三个端口（宿主机进程，容器 DNS 名解析不到，写 127.0.0.1）
make inference-host-status
```

### 6.3 SLO 取数：P95 与错误率

没有 APM，`api_logs` 就是 P95 的唯一取数口。以下两条 SQL 在本机 `user_db`（8202）实测通过。

```bash
cd /opt/hivemtk && set -a && . ./.env && set +a

# 接口 P95 耗时（api_logs.duration 单位 ms）与 5xx 错误率
docker compose exec -T mtk-postgres psql \
  -U "${POSTGRES_USER:-admin}" -d "${USER_DB_NAME:-user_db}" -h 127.0.0.1 -p 8202 -tA \
  -c "SELECT count(*) AS n,
             percentile_cont(0.95) WITHIN GROUP (ORDER BY duration) AS p95_ms,
             round(100.0 * count(*) FILTER (WHERE status_code >= 500) / NULLIF(count(*),0), 2) AS err5xx_pct
      FROM api_logs
      WHERE created_at >= now() - interval '1 day';"

# 分层链路耗时（layer_decision_logs 只有 trace_id/session_id/customer_id/layer/reason/
# intent/conf_in/conf_out/wall_ms/llm_skipped/extra/created_at/deleted_at 这些列，
# 没有 lcp_ms / status / fallback_chain / from_layer / to_layer，写了就是 ERROR）
docker compose exec -T mtk-postgres psql \
  -U "${POSTGRES_USER:-admin}" -d "${USER_DB_NAME:-user_db}" -h 127.0.0.1 -p 8202 -tA \
  -c "SELECT layer, count(*), percentile_cont(0.95) WITHIN GROUP (ORDER BY wall_ms) AS p95_wall_ms
      FROM layer_decision_logs
      WHERE created_at >= now() - interval '1 day'
      GROUP BY layer;"
```


---

*最后更新: 2026-08-16*

---

## 7. SLO 分域口径（C6/C10，T-P8-06）

> 本节是 C6/C10 的唯一口径源。每个指标 = 分子/分母 + 数据源 + 代码锚点。
> 三率（闭环/回款/逾期）的生产聚合是 `user-server/internal/ops/service/ltc_rates.go`
> （取数 `internal/ops/repository/ltc_rates.go`，T-P8-05），文档 SQL 与该聚合逐行对齐，
> 由 `internal/ops/service/ltc_rates_slo_test.go` 锁死（同一 fixture 下文档 SQL 结果
> 必须等于 service 输出，否则测试红灯）。

### 7.1 北极星：闭环率（C6）

- 定义：已闭环商机 / 全部商机。闭环 = `won` + `lost`（`cancelled` 作废不算闭环）。
- 分子：`SELECT COUNT(*) FROM opportunities WHERE status IN ('won','lost')`
- 分母：`SELECT COUNT(*) FROM opportunities`（三表均无软删列，全表即全量）
- 锚点：`CountOpportunitiesByStatus` + service `closed = won + lost`；看板北极星首位（T-P8-05）。

### 7.2 回款率

- 定义：已确认回款总额 / 应收总额（作废账单不该收，分母剔除）。
- 分子：`SELECT COALESCE(SUM(amount),0) FROM payments WHERE status IN ('confirmed')`
  （`model.PaymentStatusesCounted`，今天只有 confirmed；reversed 冲销不算数）
- 分母：`SELECT COALESCE(SUM(amount),0) FROM bills WHERE status <> 'voided'`
- 锚点：`SumPaymentsAmount` / `SumBillsAmount`。

### 7.3 逾期率

- 定义：已逾期 open 商机 / 全部 open 商机。没定关单日（NULL）的不算逾期。
- 分子：`SELECT COUNT(*) FROM opportunities WHERE status='open' AND expected_close_at IS NOT NULL AND expected_close_at < :now`
- 分母：`SELECT COUNT(*) FROM opportunities WHERE status='open'`
- 锚点：`CountOverdueOpportunities` / `CountOpenOpportunities`。

### 7.4 被动应答 P95（适用 CS-61 3s，C6/C10）

- 定义：自动回复链路 P95 ≤ 3s。**审批卡点明确不含在内**（审批走 §7.5 独立指标）。
- 分子（延迟样本）：`unified_replies.sent_at - unified_messages.received_at`，
  `JOIN ON unified_replies.message_id = unified_messages.message_id`，
  仅 `unified_replies.status='sent'` 行；P95 取该样本集的 95 分位。
- 分母：同上过滤的行数（P95 的样本集即分母）。
- 代码现状：`UnifiedMessage.ReceivedAt` + `UnifiedReply.SentAt/MessageID/Status`
  字段齐备；`reply_type` 无代码常量，自动/人工拆分待建（未闭环，当前口径按 sent 全集）。

### 7.5 审批卡点：待审批 24h 达标率（不适用 P95）

- 定义：终态审批行中 24h 内办结的占比。
- 分子：`SELECT COUNT(*) FROM approval_requests WHERE decided_at IS NOT NULL AND decided_at - created_at <= INTERVAL '24 hours'`
- 分母：`SELECT COUNT(*) FROM approval_requests WHERE decided_at IS NOT NULL`
- 字段锚点：`ApprovalRequest.CreatedAt/DecidedAt/ExpiresAt`（`internal/model/approval_request.go:59-63`）
  + 四态 `pending / approved / rejected / expired`（同文件 :79-82）
  + `decided_by` 是两个常量 —— `policy:auto`（:157）与 `system:ttl`（:160）—— 加上人工裁决时的操作者 ID。
- 24h 是**代码常量**，不是政策目标（本节旧写法「代码中无 TTL 常量」已被代码推翻）：
  `internal/service/approval_request.go:42` `DefaultApprovalRequestTTL = 24 * time.Hour`，
  :43 `MaxApprovalRequestTTL = 30 * 24 * time.Hour`，越界返回 `ErrApprovalTTLTooLong`（:58）而不是夹取。
  创建路径按 `in.TTL <= 0 → DefaultApprovalRequestTTL` 取值并在 :469 写入 `req.ExpiresAt`；
  到期由 `internal/repository/approval_request.go:291 ExpirePendingBatch`（`expires_at <= now` 的同一判据）
  翻成 `expired` 并置 `decided_by = system:ttl`（:321/:330），服务侧入口
  `internal/service/approval_request.go:654 ExpireOverdue`。
  本机 `user_db` 现测 7 行审批全部有 `expires_at`，其中 1 行已是 `expired` ⇒
  「ExpiresAt 当前无生产赋值」这句话现在照抄是错的。
- 达标率仍由本 SQL 离线/脚本统计，不进 P95（没有运行时 SLO 打点，见 §2 的取数现实）。


### 7.6 外联触达与硬预算（C10）

- 送达率：分子 `status='sent'` 行数，分母 `sent + failed` 行数（`unified_replies`；
  pending 未尝试、discarded 发送前丢弃，均不计入）。
- 骚扰投诉率：分子 `feedback_events.signal_key='complaint'` 事件数，
  分母同期 `unified_replies` sent 行数；红线 0。
- 硬预算：7 日外发 ≤ N（N 为运营配置，非代码常量，手工核）；
  DNC 名单命中发送数 = 0（数据源 `customer_do_not_contacts` + `unified_replies`）；
  投诉红线见上。
