# 平台性失败台账（2026-10-11 建立）

> 口径：每个失败一行——发现轮次、根因、修复、回归方式。状态只有 已修复 / 修复中 / 已豁免（带理由）。
> "平台性"= 与业务逻辑无关的环境/平台/时区/权限位/文案差异类失败，以及会在 CI 上假红或假绿的门禁缺陷。

## 已修复（2026-10-10 轮，commit 2db9d679）

| # | 失败 | 根因 | 修复 | 回归 |
|---|---|---|---|---|
| F-01 | service 备份解压测试 Windows 红（权限 666≠600） | Unix 权限位在 Windows 恒报 666 | GOOS 守卫拆断言 | 本地 go test |
| F-02 | service 批量探测"只读目录"档 Windows 红 | Windows 目录只读属性不拦创建文件 | GOOS 守卫 skip | 同上 |
| F-03 | SessionID 列宽守卫 CI 红（120 vs 64） | 40a19abb 放宽 session_id 族到 120 没跟快照 | **采纳 upstream 20ed9fbd 裁定：保持 120 重订快照**（AutoMigrate 只加不删，回 64 会造成新装/升级库形状分叉）——本仓曾短暂回退 64，merge 时纠正 | model 包测试 |
| F-04 | llm dispatcher 不可达端点退避三连（6s） | isUnreachable 只匹配 Unix 文案 "connection refused"，Windows 是 "actively refused" | 改按 syscall/DNSError 判定 + 双平台文案兜底 | llm 包 fail-fast 测试 |
| F-05 | channel_error 拒绝连接漏判 Network | 同 F-04 文案问题 | 匹配集补 "actively refused" | service 包 |
| F-06 | storage RoundTrip Delete 红（句柄占用） | 测试 defer Close 在 Delete 之后，Windows 句柄挡删除 | 读完显式 Close | storage 包 |
| F-07 | platform 商户密钥 0600 断言 ×3 | Unix 权限位 | 统一 requireFilePerm0600（Windows skip） | platform 包 |
| F-08 | install.lock 0644 断言 | 同上 | GOOS 守卫 | install 包 |
| F-09 | rag 会话 UpdatedAt 严格 After 红 | Windows 墙钟粒度 ~15.6ms，三次取时同 tick | Windows 退化"不得回拨"，Unix 保持严格 | customer_service 包 |

## 已修复（2026-10-11 多租户轮）

| # | 失败 | 根因 | 修复 | 回归 |
|---|---|---|---|---|
| F-10 | 大量页面显示异常（6 个视图数据恒空/恒失败） | axios 拦截器已解包 `{code,data}` 直接 resolve 内层，视图按原始信封取值 `res?.data?.list` | AutomationHub/AbStats/Connectors/followups-Today/oneid-Conflicts/leadMining-Index 统一 `res?.data ?? res` | npm build + eslint 0 err |
| F-11 | 后台前端 agent 报告里 git 操作失败一次 | cwd 漂移到 user-server | 主 Agent 约定：所有 git/npm 操作显式 cd 仓库根 | — |

## 已豁免（带理由）

| # | 项 | 理由 |
|---|---|---|
| E-01 | PG 依赖用例本机 skip（testutil.NewTestDB） | 仓库设计：本地无 PG 则 skip，CI 环境下 Fatal（防假绿）——skip≠pass，本地跑 `go test ./...` 时需人工区分 |
| E-02 | 租户裸查询 29 处豁免点 | scripts/tenant-scope.baseline，全部为系统管线（monitor/ops 面板/导入/媒体回写/去重查找），用户面路径已全部挂 scope |

## 监控位

- mcp 审计丢弃计数：`service.MCPAuditDroppedCount()`（P2 接告警 webhook，backlog A-6）
- CI 红规则：门禁测试 skip 数量突增（PG 依赖面扩大）应视为门禁退化信号
