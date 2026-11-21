# Bug Assessment: 线索发掘配置缓存与去抖租户隔离

- **Slug**: lead-mining-tenant-isolation-20261121
- **Created**: 2026-11-21
- **Source**: `docs/未完成任务清单.md` FA-17
- **Verdict**: valid
- **Severity**: high

## Report (verbatim or summarized)

FA-17 要求线索发掘配置缓存、失效及去抖键包含租户；不接受无租户归属的消息。用 A/B 相反配置、相同外部身份交错消息验证仓储读取、Judge 输入、写入归属、热更新失效范围及租户间不互相节流。

## Symptom

`Service` 当前只缓存一个 `LeadMiningConfig` 和加载时间，所有租户共享；全局 `ReloadConfigCache` 清空该单一项。`process` 的去抖 key 由 platform/sender 或 conversation/sender 构成，不带租户。虽然配置仓储的单例行已按 tenant ID 定位，但服务缓存命中会返回另一租户配置。相同外部平台身份也会跨租户抑制判定。

## Reproduction

1. 在同一服务实例下交错处理 tenant A/B 的消息，并令配置仓储对两租户返回不同配置。
2. 在 A 配置缓存有效时读取 B 配置；当前缓存可能返回 A 的配置且不再访问仓储。
3. 在 A 已处理同 platform/sender 后处理 B 相同身份消息；当前共享去抖键可能跳过 B 的 Judge。
4. SaaS 模式下处理 `MessageHub.TenantID == 0` 的消息；现代码保持原 ctx 但仍继续读取配置及后续链路，最终依赖仓储隔离守卫间接拒绝，无法在该路径入口处确保所有作用域相关工作 fail-closed。

## Suspected Code Paths

- `user-server/internal/service/lead_mining.go:46-60` — 服务缓存与去抖状态为无租户键的单值/映射。
- `user-server/internal/service/lead_mining.go:204-257` — tenant context 由消息 tenant ID 绑定；缺失归属仍继续执行，去抖键缺 tenant。
- `user-server/internal/service/lead_mining.go:260-278` — 缓存读取及覆盖不区分租户。
- `user-server/internal/service/lead_mining.go:89-97,129-135` — 配置保存后的全局缓存失效。
- `user-server/internal/repository/lead_mining_config.go:29-67` — 配置行已使用 `tenant.FromContext` 计算租户行 ID；预期复用，无需修改。
- `user-server/internal/service/lead_mining_tenant_scope_test.go` — 既有 tenant context 回归用例；可在不修改该已有文件的前提下新增独立测试。

## Root Cause Hypothesis

服务层缓存粒度与配置仓储粒度不匹配：仓储按租户读写，缓存只保存一份进程级配置；去抖的业务身份键也没有租户成分。消息租户缺失时依赖下游数据库守卫，而不是在线索链入口明确拒绝。置信度：high（源码可确认）。

## Proposed Remediation

**Preferred**: 对已确认的 tenant ID 使用按租户键控的配置缓存及加载时间，并对返回配置做副本，避免调用方修改共享缓存对象；缓存未命中时按 tenant-bound context 调用已有配置仓储。按租户定向清缓存，保留无参数 `ReloadConfigCache()` 的兼容调用语义（全量失效），同时增加可供保存路径使用的租户定向失效入口。将 tenant ID 纳入 debounce key。SaaS `ModeOn` 下，消息行 tenant ID 为 0 时在配置读取、Judge 和任何写入前直接拒绝；明确绑定的 tenant scope 应移除系统豁免，不能让 SystemScope 跨租户。

**Files likely to change**:
- `user-server/internal/service/lead_mining.go`
- `user-server/internal/service/lead_mining_tenant_scope_test.go` (已有在途测试文件；原则上新增另一个独立测试文件，避免与其他会话差异冲突)
- `user-server/internal/service/lead_mining_tenant_isolation_test.go` (新增)

**Tests to add or update**:
- 交错 tenant A/B 配置且渠道/阈值相反，断言仓储按绑定 tenant 调用、各自 Judge 收到自身配置、客户/线索写入保持消息 tenant。
- 同一 platform/sender 在 A/B 分别处理，确认去抖彼此独立；相同租户在窗口内仍只判定一次。
- 热更新 A 后仅使 A 缓存失效；B 仍命中自身有效缓存；兼容全量 reload 则两者均重新载入。
- SaaS 模式 tenant ID 缺失时，确认不读取配置、不判定、不产生持久化副作用。
- 配置对象不会因返回给消费者后被修改而污染该租户缓存。

## Risks & Considerations

- 配置保存后的调用方目前执行全局 reload；保持它的兼容语义，避免配置更新产生延迟可见或跨租户清缓存副作用。
- worker 的消息归属仅能来自持久化 `MessageHub.TenantID`；不能在未知归属时回退默认租户。
- 将系统 scope 绑定的上下文钉到消息租户，避免以系统豁免绕开租户隔离。
- `lead_mining_test.go` 当前已有不属于本任务的未提交差异，须保留且不得纳入本任务变更。

## Open Questions

- 无。实现按现有 tenant context 契约及 FA-17 清单判据执行；真实生产跨租户安全仍以隔离库/部署门禁证据为准。
