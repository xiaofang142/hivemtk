# Bug Assessment: 共享上下文读取缺少员工账号授权

- **Slug**: conversation-context-employee-scope-20261125
- **Created**: 2026-11-25
- **Source**: 工作区 `docs/未完成任务清单.md` CRM-20261125-04
- **Verdict**: valid
- **Severity**: high

## Report / Root Cause

`controller/conversation_context.go:Get` 从查询参数取得账号、会话和平台后直接调用内部 `Snapshot`。LM-01 已使内部读取验证真实账号的租户和会话作用域，但同租户的登录员工仍未经过账号授权检查，即可取得其他账号的消息、摘要、标签、成员角色与双方备注。

## Reproduction

准备同租户两个真实渠道账号及私有共享上下文；仅给员工授权 A，调用 GET 读取 B。通过真实 PostgreSQL 和 httptest 验证当前返回私有内容，再修复。此 assessment 只完成静态根因定位，执行复现状态为 not-run。

## Fix Scope

1. 外部入口只接受认证中间件的员工身份和租户上下文，拒绝缺失身份、缺租户、系统作用域以及跨租户账号。
2. Service 在读取任何上下文前验证真实账号、真实渠道与当前员工授权，复用现有 BusinessScope 和 ChannelAccountAuthorization 两套授权的并集语义；旧授权保留渠道约束，不把识别码作为授权凭据。
3. 管理员/既有全量数据作用域保留本租户访问，撤权后下一次读取立即拒绝；权限解析异常 fail-closed。
4. 保留后台双模块内部 Snapshot，不把后台调用伪装成员工，不更改 CRM-24 行业/owner 配对规则或 LM-12 并发更新逻辑。

## Verification Plan

真实 PG + Controller 测试覆盖：授权正例、同租户未授权、空授权、跨租户、伪造 user/tenant/role 查询、识别码授权拒绝、旧渠道约束、旧新授权并集、撤权、管理员、缺登录/租户、权限存储故障。所有拒绝响应不得带任何私有内容。

## Hooks / Risks

`.specify/extensions.yml` 的 hooks 为空。无迁移、无新路由、无 UI、无外发或部署；保留其他会话在途差异。LM-01 的会话作用域改动为当前磁盘依赖，完整服务包测试另受其他会话 ReAct 测试编译漂移阻断，不能据此声称全仓通过。
