# 多租户 / RBAC / SaaS 化 Backlog（任务总账）

> 来源：2026-10-11 指令「多租户、RBAC、SaaS、MCP/Skill 鉴权、审计、UI/i18n 与平台性缺陷修复」。
> 实施基线：[multi-tenant-rbac-saas-plan.md](../multi-tenant-rbac-saas-plan.md)（分期方案）。
> 执行口径：先本地实现并验证，通过后再上阿里云生产；每个可验收点独立提交；禁止直接改生产。

## 状态图例

✅ 已完成（本轮之前）  🔨 本轮实施  📋 规划（本轮出方案/脚本，实施另行排期）

## P0 止血与租户隔离地基（阻断级，最优先）

| # | 事项 | 状态 | 依赖 | 验收标准 | 回滚 |
|---|---|---|---|---|---|
| P0-1 | MCP 凭证签发 "Client ID: undefined / API Key: undefined" | ✅ 3dbfef83 修复，本轮复核确认链路无 undefined | — | 签发弹窗与复制按钮拿真实值 | — |
| P0-2 | 登录页 i18n（硬编码中文） | ✅ 91ecac4e 修复，本轮复核 26 个 key × 9 locale 全齐 | — | 登录页无硬编码中文 | — |
| P0-3 | 大量页面显示异常（响应拦截器契约误用：`res?.data?.list` 实际应 `res?.list`） | ✅ 修复 6 视图（AutomationHub/AbStats/Connectors/followups-Today/oneid-Conflicts/leadMining-Index） | — | 三页面数据正常渲染；grep 全仓无同类裸读 | 前端回滚=回退 dist |
| P0-4 | teams 表 + system_users.team_id 语义（0=平台侧）+ `team_admin` 角色 | ✅ | — | 新表/角色可用；角色 CHECK 漂移（migrations/025）放宽钩子 | AutoMigrate 只加不删；角色常量回退即可 |
| P0-5 | JWT 全链路注入 team_id/data_scope/department_id（login/MFA/SSO 现走空 scope） | ✅ | P0-4 | 登录/刷新/MFA/SSO 签发的 token 均带 team_id；LoginResponse 暴露 team_id | 回退签发点即可（claim 向后兼容） |
| P0-6 | 租户上下文 + scope 注入：scope 包新增 TeamInfo/TeamScoped + GORM Create 回调回填 team_id | ✅ | P0-4 | 普通用户写入自动带 team_id；平台管理员（role=admin）直通 | 环境开关 TENANT_ISOLATION=off 一键旁路 |
| P0-7 | 核心域查询强制 team 过滤（customers / inbox conversations / message_hub / clues / kb documents）+ GetByID 越权 404 | ✅ | P0-6 | 跨团队读取返回空/404；平台管理员全量可见；Go 测试覆盖每域 | 同上开关 |
| P0-8 | 存量回填：默认团队 + 全量业务行/普通用户归入默认团队（postMigrate 幂等钩子） | ✅ | P0-4 | 回填后无 team_id=0 的业务行（平台侧语义除外）；重复启动不重复建团队 | 回填脚本可重跑；回滚脚本可还原 team_id |
| P0-9 | CI 守卫：核心表裸查询必须带 team scope（基线文件同 arch/date-bucket 门规格） | ✅ | P0-7 | scripts/check-tenant-scope.sh + baseline 入 CI；裸查新增即红 | 删 workflow 行即解除 |
| P0-10 | 回滚脚本：迁移降级 + feature flag 关闭 + 备份恢复演练步骤 | ✅ | P0-8 | scripts/rollback-tenant-p0.sh 可演练（dry-run） | — |
| P0-11 | 团队管理端点（超管全量 CRUD；团队管理员仅本团队用户/角色） | ✅ | P0-4 | 超管可增删改查所有团队与成员；team_admin 只能管本团队 | 回退路由+控制器 |

## P1 RBAC 权限模型

| # | 事项 | 状态 | 依赖 | 验收 |
|---|---|---|---|---|
| P1-1 | roles/permissions/role_permissions/user_roles 表 + RequirePermission("resource.action") 中间件替换粗粒度 admin 检查 | 📋（P0 的 team_admin 只做角色轴，权限轴按计划文档 P1） | P0 | admin/* 路由逐一映射权限点，默认行为不回退 |
| P1-2 | 前端菜单/按钮按权限点显隐（requiresAuth → requiresPermission 扩展） | 📋 | P1-1 | 无权限按钮不可见且服务端仍强制 |
| P1-3 | API Key reveal/copy 需要 `api_key:reveal` 权限点 | 📋（当前 admin 路由 + 强制审计已达标 P0） | P1-1 | 无权限者 403 且审计留 403 记录 |

## P2 SaaS 化

| # | 事项 | 状态 |
|---|---|---|
| P2-1 | 注册即建团队（注册人=团队管理员）；邀请码/审批准入（register_mode 配置） | 📋 |
| P2-2 | 租户生命周期：启用/停用/配额（用户数、消息量），超配限流 | 📋 |
| P2-3 | 品牌化（团队级站点名/Logo）+ 计费预留（teams.plan + 用量计量表） | 📋 |

## MCP/Skill 鉴权与 API Key 生命周期

| # | 事项 | 状态 |
|---|---|---|
| M-1 | 鉴权四件套：入口与权限口径一致 / 凭证级 scope / 调用审计页 / Skill 注册表与管理页 | ✅ 91ecac4e + 本轮复核（路由/字段/页面全对账） |
| M-2 | Client ID ↔ 账户绑定（角色/权限/功能/业务/付费继承） | ✅ P0 范围：凭证继承 owner 的 team 进 MCP 调用链；角色/付费细化随 P1-1 |
| M-3 | 凭证过期时间：默认 90 天 + 手动自定义/no_expiry | ✅ 91ecac4e |
| M-4 | 签发后手动改 Client ID | ✅ 91ecac4e（PUT client_id + 缓存失效） |
| M-5 | API Key 1:N + 状态机（active/disabled/suspended/revoked + 软删除）+ 显示/隐藏/复制 + reveal 强制审计 | ✅ 2db9d679 + 7a8a666d |
| M-6 | 明文双轨存储（sha256 + AES-GCM），明文永不入审计 | ✅ 2db9d679（禁令持续生效） |

## 审计增强

| # | 事项 | 状态 | 说明 |
|---|---|---|---|
| A-1 | 字段补齐：team_id / user_agent / request_id(trace_id) 入 mcp_call_audits 与 mcp_credential_audit_logs | 🔨 | request_id 复用 TraceMiddleware 已在 gin ctx 的 trace_id |
| A-2 | 异步写入 + 失败重试 + 丢弃计数（复用 AuditManager 形态） | ✅ | MCP 审计从同步 inline 改异步批量 |
| A-3 | 查询/筛选增强：时间范围、操作人、team 过滤 | ✅ | |
| A-4 | CSV 导出（含筛选生效 + UA/detail 列） | ✅ | operation_logs 现有导出忽略筛选、缺列，一并修 |
| A-5 | 保留策略：保留天数配置 + 运维清理脚本 | ✅ | scripts/audit-retention.sh（main.go 禁碰 → 不注册常驻 cron，P2 再入） |
| A-6 | 告警（webhook on auth-fail 突增/丢弃计数）与防篡改（hash-chain/WORM） | 📋 P2/P3 | 防篡改成本高，先以「API 面只读 + DB 备份」为底线 |

## 平台性失败台账

见 [platform-failures.md](platform-failures.md)（本轮建立，含已修复 9 条与本轮新增）。

## 统一平台词汇表 + 注册表（第二阶段）

| # | 事项 | 状态 |
|---|---|---|
| R-1 | 设计决策：ADR-017（对接既有 internal/capregistry，补动作词汇层） | ✅ 2db9d679 |
| R-2 | registry.yaml 单一事实源 + 生成器（rbac 权限常量/角色矩阵 + 文档）+ 新鲜度门禁 | ✅ 本轮落地生成闭环；运行时行为接入（RequirePermission 消费 rbac 常量）随 P1-1 |
| R-3 | catbus 借鉴评估落档 docs/adr/catbus-borrowings.md | ✅ 本轮（ADR-018） |

## 回归门禁（按仓库实际命令）

- Go：`go vet ./...`、`go build ./...`、`go test ./...`（PG 依赖用例本机 skip 属设计，CI 强制）、`go test -race ./internal/middleware/ ./internal/service/`
- 前端（仓库用 npm，非 pnpm）：`npm run build`（含 menu:check/i18n 门）、`npx eslint`（0 errors）、`npm run test`（vitest，如涉及）
- 脚本门禁：`scripts/check-tenant-scope.sh`（新增）、既有 arch/date-bucket 门
- 迁移/回滚：回填钩子幂等测试 + `scripts/rollback-tenant-p0.sh --dry-run` 演练
- 生产验证（阿里云）：P0 本地全绿 + 回滚演练通过后，按 step4 流程部署并验证
