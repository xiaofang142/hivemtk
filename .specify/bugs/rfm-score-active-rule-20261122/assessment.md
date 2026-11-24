# Bug Assessment: FA-29 / FA-30 RFM 综合分与激活规则

- Slug: rfm-score-active-rule-20261122
- Created: 2026-11-22
- Source: 外层 docs/未完成任务清单.md FA-29、FA-30
- Verdict: valid
- Severity: medium

## Symptom and root cause

两个生产计算路径均将 1–5 分维度乘 30/30/40 后夹 100，最低分已达 100。ComputeAll 固定 DefaultRFMConfig，不读取当前租户激活规则。原始指标使用 time.Since 而非服务注入时钟，且无订单时 recency 默认为 0，会将未消费客户当成最近消费。以上均由当前源码确认，动态复现尚未运行。

## Proposed Remediation

统一使用 1→0、5→100 的 30/30/40 加权线性归一，舍入到最近整数。ComputeAll 在首个客户读前读取并复制当前租户激活规则，一轮只用同一快照；只有 record-not-found 使用默认规则，其他读错终止，不产生计算写入。原始指标使用 nowFunc，无有效交易日期为 9999 天。

## Files likely to change

- user-server/internal/service/customer_rfm.go
- user-server/internal/service/customer_rfm_rules_test.go

## Tests to add or update

真实服务单客户与全量入口：最低/最高/中间/单维变化、保存值；固定时钟与无订单；真实专用 PG 激活规则按租户读取、重算变化、规则只读一次、读失败零写入。先补失败用例，再改生产源码。

## Risks

既存恒100数据需通过既有重算入口纠正；不改变schema和阈值。保留已有其他会话改动；不连接业务库、不外发。完整门禁与发布必须独立核验，不以专项替代。
