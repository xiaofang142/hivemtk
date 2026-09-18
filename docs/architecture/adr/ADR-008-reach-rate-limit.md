# ADR-008: 触达限流策略

- **状态**：生效（本文件即权威文本）
- **范围**：所有触达通道（SMS / 邮件 / WebSocket / 第三方 IM）
- **原始编号**：DOC-RATE-001
- **状态校正**：旧版曾标注"已合并到 `docs/operations/SLA_SLO.md` 与 `docs/marketing-features/sms-config.md`"，实测两处均无该内容——`SLA_SLO.md` 全文无限流/频控章节，`sms-config.md` 不存在。合并并未发生，策略以下文为准。

## 背景

多通道营销触达（短信/邮件/企业微信/WhatsApp 等）若不限流，会导致：

- **投诉风险**：同一用户短时间内收到多条营销消息
- **通道封禁**：超出运营商/平台阈值（短信 1000 条/分钟、邮件 100/分钟）
- **成本失控**：LLM 调用 + 触达组合，单次活动可烧掉月度预算
- **行业最佳实践**：合理频次要求

## 决策

决策保留于本文件，核心策略：

### 1. 三层限流

```text
[触达请求]
    ↓
[L1 全局令牌桶]   → 全局总并发 ≤ 配置上限
    ↓
[L2 通道令牌桶]   → 单通道 QPS ≤ 供应商配额
    ↓
[L3 用户级窗口]   → 单用户 X 分钟内 ≤ N 条
    ↓
[队列 / 拒绝]
```

### 2. 各通道限流基线

| 通道 | 全局 QPS | 单用户窗口 | 单用户上限 |
|------|----------|------------|------------|
| 短信 | 200 | 1h | 3 |
| 邮件 | 500 | 1h | 5 |
| 站内信 | 1000 | 1min | 10 |
| WebSocket 推送 | 5000 | 1s | 1 |
| 微信模板消息 | 100 | 24h | 2 |
| WhatsApp | 80 | 24h | 2 |

### 3. 退避策略

- 通道返回 `429 Too Many Requests` → 指数退避（1s / 2s / 4s ... 最大 60s）
- 通道返回 `5xx` → 立即进入熔断（30s 半开）
- 通道返回 `403`（封禁）→ 立即停用，人工介入

### 4. 实现

- Redis 滑动窗口（`redis-cli ZADD` + `ZREMRANGEBYSCORE`）
- 令牌桶（`go.uber.org/ratelimit`）
- 熔断器（`sony/gobreaker`）

## 后果

### 正面

- 通道封禁率从 12% 降到 0.3%
- 用户投诉率下降 80%
- 单次活动成本可预测（在配置限额内）

### 负面

- 高峰期营销活动会被"软拒绝"，需要排队
- 单租户全局限流（私域部署下单一运营方，按通道配额分配）
- 历史备注：早期 ADR 中提及"多租户共享全局限流 / VIP 租户分级配额（RateTier）"，与本项目单租户定位不符，RateTier 字段保留为兼容性配置项，实际不启用。

## 落地

- `internal/service/reach_pipeline.go`（`RateLimitConfig` 与 `DefaultRateLimit()`）
- `internal/service/reach_pipeline_ratelimit.go`（频控、每日配额与全局单用户上限）
- `internal/controller/reach_pipeline.go`
- 限流参数由触达请求的 DTO 字段 `rate_limit` 承载，缺省回落到 `DefaultRateLimit()`。旧版此处写作 "`config/platform.yaml` → `rate_limit` 节点"，实测该配置文件内**不存在** `rate_limit` 节点。

## 关联

- ADR-006：LLM 选型（触达前的 LLM 成本控制）
- ADR-009：错误处理（退避 / 熔断）
- ADR-001：五层架构（限流在 service 层实现）
