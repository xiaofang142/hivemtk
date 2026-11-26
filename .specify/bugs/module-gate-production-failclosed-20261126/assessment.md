# Bug Assessment: 模块闸门生产解析故障与兼容边界

- **Slug**: module-gate-production-failclosed-20261126
- **Task**: LEAD-20261125-03
- **Stage**: assess

## Report

2026-11-26 对照 `LEAD-20261125-03`、`.specify/custom-task-crm-lead-channel-20261124/research.md` R8.4 与当前生产代码发现语义漂移：`auto_reply.go` 的纯判定函数将 lookup error 判为 `ModuleGateError`，但生产 `resolveModuleGateDecision` 在账号引用为空、仓储不可用、resolver 返回任何错误时可能判为 `Unmigrated` 并允许模块执行；`EnabledForAccount` 在仓储不可用、查询失败或显式账号不存在时也返回 `true`。生产消费者包括 `inbox_ingress.go:828–842` 的自动回复闸门和 `lead_discovery.go:42–48` 的线索入队闸门。已有纯函数测试不能覆盖这些生产分支。

此外，`ChannelResolver.ResolveByAccountRef` 会忽略识别码初次解析错误并继续按数值 ID/名称回退；对 `ErrChannelIdentityAmbiguous` 的处理可能丢失 CH-07 要求的歧义拒绝语义。需要回归测试确认歧义不会经 fallback 变为未命中并打开模块。

## Evidence and root cause

- `user-server/internal/service/auto_reply.go:41–61,123–156`：数字账号入口在仓储异常/缺记录时放行；字符串入口把空引用、仓储不可用和 resolver 的全部错误映射为 `ModuleGateUnmigrated`。
- `user-server/internal/service/channel_resolver.go:101–177`：`ResolveByAccountRef` 的识别码解析只在无错误且有结果时返回，随后对其它错误继续 fallback。
- `user-server/internal/service/module_gate_decision_test.go:8–28`：当前只测 `decideModuleGate` 纯函数，没有覆盖生产仓储/解析流程。
- `user-server/internal/service/inbox_ingress.go:828–842` 与 `lead_discovery.go:28–48`：两类生产执行入口共享闸门决策。
- `docs/未完成任务清单.md` §3.29 的 `LEAD-20261125-03` 明确要求区分真实未迁移与解析/仓储故障；`.specify/bugs/dual-module-gate-failclosed-20261125/` 是早期实现记录，不作为当前行为已修复的凭据。

## Second review: compatibility and security decision

- 反方依据：R8.4 将“legacy、仓储不可用、测试无 DB”合并为兼容放行，以免新表未就绪时关闭旧链。
- 支持 fail-closed 的依据：较新的 LEAD-03 明确要求错误与真实未迁移分开；CH-07 也规定身份歧义拒绝；无数据库/查询失败无法证明当前消息属于未迁移账号，而空/跨租户身份不应使自动回复或线索分析绕过配置闸门。
- 裁决：按较新任务的明确安全不变量施工。只有在有效租户上下文下，仓储可用且查询成功地确认“未命中主数据”的情况才保留 `Unmigrated` 兼容放行；仓储不可用、查错、歧义、已解析后记录消失或多租户缺 tenant 均 fail-closed。若仍需支持“新表/迁移缺失时旧链放行”，须以后续显式 migration/readiness 信号表示，不把所有错误折叠为未命中；本次不引入该新配置机制。
- 边界：不改变渠道整体停用（CH-05）、Agent 配置/类型校验（LEAD-02/LM-07）、旧入口迁移或外发行为；不做真实数据库、AI、UI、设备、渠道或部署验收。

## Files likely to change in fix stage

- `user-server/internal/service/auto_reply.go`：让生产字符串/数字闸门在故障时 fail-closed，保留明确未命中的兼容状态，并抽出可注入依赖的判定 seam。
- `user-server/internal/service/channel_resolver.go`：保留可继续兼容回退的明确未命中，但不得吞掉身份歧义；只有本 assessment 中经测试证实必要的错误传播改动。
- `user-server/internal/service/module_gate_decision_test.go`：新增字符串和数字生产解析路径的仓储故障、歧义、显式未命中、缺 tenant 与成功/关闭回归。
- `user-server/internal/service/channel_contract_test.go`：验证识别码歧义不会通过名称/账号回退被解析成任一账号。

## Acceptance criteria

1. 账号配置明确开启且身份解析成功时放行；明确关闭时阻断。
2. 经成功查询确认无匹配主数据的 legacy 引用仍按 R8.4 兼容规则放行。
3. 仓储不可用/查询错误、身份歧义、无效多租户上下文、解析后账号缺失均 fail-closed；不能将它们映射为“未迁移”。
4. 字符串与数字两个闸门入口都被测试覆盖；回归测试调用生产实际使用的判定函数，而非仅测纯函数。
5. 不触发模型/API、真实消息、客户数据、DB 测试环境或生产发布；严格执行 SPEC-KIT assess→fix→test 阶段边界。

## Assessment status

Root cause and scope identified. No product code or existing task text has been changed in assess stage. Proceed to fix only within the listed files; if testing shows a wider CH-07 contract change is needed, stop and record the deviation rather than expanding scope.
