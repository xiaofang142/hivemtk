# Assessment — 工具处理器的租户、来源与配置代际

- Date: 2026-11-20
- Source: 外层唯一未完成清单 FA-AGENT-TOOL-01
- Classification: valid / high
- Dynamic reproduction: 尚未运行新增回归；以下为已读源码证据，不宣称测试已复现。

## Root Cause

`ToolExecutor.Execute` 先从执行器注册表、全局注册表或请求租户叠加层解析工具，
但 `getOrBuildHandler` 只按工具名缓存。`buildHandler` 的执行闭包以及审批、风险装饰器
捕获具体工具对象，之后同名的另一租户或更新实例会复用旧对象。
`RegisterTenantTool` 先注销后注册，还会向并发读取暴露一个不必要的空档。

## Decision and Alternatives

选择注册表原子快照：工具对象与注册代际在同一读锁内取得；缓存键使用可信上下文租户、
注册表来源和工具名，缓存值保存代际与整条 handler。同名替换在同一写锁内完成。
不同代际重建整条链，不仅替换 raw Execute。注册表单调代际不因删除而重置，避免删除重建复用旧缓存。
按工具名手工清缓存无法覆盖共享执行器的所有写入入口；仅比较工具指针无法表达同指针重新登记，均不采用。

调用的线性化边界为取得注册表快照：已取得旧快照的在途调用可以按旧实例结束；
更新/注销返回后才开始的调用必须取得新实例/未找到，禁止缓存绕过重新查找。
登记后工具对象视为只读，配置修改须重新登记，不支持并发原地改写对象字段。
既有内置工具优先级、单租户默认作用域、多租户无上下文拒绝叠加层、override 和缓存清理语义保持。

## Allowed Files

- `user-server/internal/aiagent/agent/tooluse/registry.go`
- `user-server/internal/aiagent/agent/tooluse/tenant_registry.go`
- `user-server/internal/aiagent/agent/tooluse/executor.go`
- 新增 `user-server/internal/aiagent/agent/tooluse/executor_scope_cache_test.go`
- 本 Bug 的 `fix.md`、`test.md`

不修改其他会话的业务文件，不新增配置、依赖、迁移或对外工具调用。

## Acceptance

真实执行器与真实注册表配合本地计数工具，覆盖两租户同名交错与并发首次调用、
同租户更新凭据标识、删除重建、内置/租户同名来源切换、多租户缺上下文拒绝、
同指针重新登记以及更新/撤销与在途调用边界。
更新类别及风险声明后，审批/风险链必须重建，拒绝时工具计数为零；成功审计的租户及结果标识
必须对应实际执行实例。模拟标识不是凭据，不使用真实外部服务。
跑工具包竞态回归及所有既有测试，再跑后端 build/vet/lint/架构/全量 Go 与审计门；
完成正常链 A 发布、拓扑登记和外层精确销项后才能领取下一项。
