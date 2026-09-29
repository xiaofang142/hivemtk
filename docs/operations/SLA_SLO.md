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

| 指标 | 目标 | 说明 |
|------|------|------|
| 系统可用性 | ≥ 99% | 月度计算，排除计划维护 |
| API 响应时间 | P95 < 500ms | 不含 LLM 推理时间 |
| 数据持久性 | 100% | 备份数据不丢失 |

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

| 故障 | 处理方式 | 恢复时间 |
|------|---------|---------|
| 服务进程挂掉 | `docker compose restart` | < 5 分钟 |
| 数据库连接丢失 | 检查网络 + 重启 PG | < 15 分钟 |
| 磁盘空间不足 | 清理日志 + 扩容 | < 30 分钟 |
| LLM 推理超时 | 重启 llama.cpp | < 10 分钟 |
| 数据丢失 | 从备份恢复 | < 2 小时 |

## 6. 日志与监控

### 6.1 关键日志路径

```bash
# 应用日志
/var/log/hivemtk/app.log

# 错误日志
/var/log/hivemtk/error.log

# 审计日志
数据库 audit_logs 表
```

### 6.2 常用排查命令

```bash
# 查看服务状态
docker compose ps

# 查看最近错误
tail -f /var/log/hivemtk/error.log

# 查看数据库状态
docker compose exec postgres pg_isready

# 查看 Redis 状态
docker compose exec redis redis-cli ping
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
- 字段锚点：`ApprovalRequest.CreatedAt/DecidedAt/ExpiresAt` + 四态 + decided_by 三值常量。
- 诚实标注：24h 是政策目标，**代码中无 TTL 常量**（ExpiresAt 当前无生产赋值），
  达标率由本 SQL 离线/脚本统计，不进 P95。

### 7.6 外联触达与硬预算（C10）

- 送达率：分子 `status='sent'` 行数，分母 `sent + failed` 行数（`unified_replies`；
  pending 未尝试、discarded 发送前丢弃，均不计入）。
- 骚扰投诉率：分子 `feedback_events.signal_key='complaint'` 事件数，
  分母同期 `unified_replies` sent 行数；红线 0。
- 硬预算：7 日外发 ≤ N（N 为运营配置，非代码常量，手工核）；
  DNC 名单命中发送数 = 0（数据源 `customer_do_not_contacts` + `unified_replies`）；
  投诉红线见上。
