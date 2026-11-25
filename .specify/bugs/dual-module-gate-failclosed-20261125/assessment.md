# Bug Assessment: 双模块执行闸门区分未迁移与解析故障

- **Slug**: dual-module-gate-failclosed-20261125
- **Task**: LEAD-20261125-03

## Report

`resolveModuleChannelAccount` 把未绑定（未迁移账号）、解析失败、仓储不可用统一为 bound=false；`AutoReplyService.Enabled` / `LeadDiscoveryService.Enabled` 因此返回 true（放行）。已纳入新配置的账号在仓储故障/解析失败时会意外开启模块。数字入口 `EnabledForAccount` 同样在查库失败时返回 true。

## Scope

新增显式闸门判定 `ModuleGateDecision`：unmigrated（未迁移，维持旧行为）、enabled、disabled（明确关闭）、error（故障，fail-closed）；两模块各自基于同一判定。不扩大到渠道整体停用（CH-05）或配置契约（LM-07）。
