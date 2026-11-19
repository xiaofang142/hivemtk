# Bug Assessment: 管理状态未接通工具执行准入

- Slug: tool-runtime-revocation
- Assessed: 2026-11-20
- Validity: valid
- Severity: high
- Source: 外层唯一未完成清单 FA-AGENT-TOOL-02

## Evidence and Root Cause

`AIToolConfigService.UpdateStatus/BatchUpdateStatus` 只更新配置表，全局执行器没有读取它。OpenAPI 导入及启动重建捕获行内连接配置，删除工具集只删表；MCP 停用/删除同样不检查已登记工具，重复发现还会忽略同名注册。管理成功与执行面之间没有权威状态检查，旧实例、暖缓存及另一副本可继续调用。当前为静态证据，动态红灯将记录在 fix.md，不提前称已经复现。

## Selected Fix

1. 新增仓储准入：每次副作用前读取可信租户下的 `ai_tool_configs`；无配置行保留未配置工具策略，缺表、查询失败或空依赖拒绝。配置禁用明确拒绝，不缓存许可。
2. 持久化 OpenAPI/MCP 工具再校验原始资源的租户、ID、当前启用状态和执行字段快照。删除重建的同名资源及已更改凭据/端点不能复活旧实例。不把同步统计等非执行字段当配置变化。
3. 全局执行器生产装配接入准入；在原始处理器内每次实际尝试前检查。持久化工具直接 Execute 同样检查，避免绕开执行器。瞬态未持久化 Provider 保持兼容。
4. MCP 重发现以原子替换更新完整处理器代际；OpenAPI 即时导入和重建都装同一守卫，不把租户 Provider 注入进程全局自动注册切片。
5. 停用/删除响应以数据库已提交为准，新调用无 TTL 地读取同一权威状态。已经通过检查的在途调用可完成，撤销不声称取消远端已开始的副作用。

## Scope

- 新增 `user-server/internal/repository/tool_runtime.go` 和对应测试。
- `user-server/internal/aiagent/agent/tooluse/{executor.go,registry.go,custom_tool_boot.go,openapi_tools.go}`，新增 `managed_tool_revocation_test.go`。
- `user-server/internal/aiagent/mcp/{client.go,config.go}`，新增 `revocation_test.go`；必要时既有持久化执行用例补配置表模型和可信上下文。
- `user-server/internal/app/{tool_executor_wiring.go,tool_provider_wiring.go}`，新增 `tool_runtime_wiring_test.go`。
- `user-server/internal/service/custom_tool.go`、`mcp_client.go`，必要的生命周期回归测试。
- 本 Bug 的 fix.md/test.md；实际交付后按固定拓扑登记。

不新增表、迁移、环境配置或依赖；不放宽 SSRF，不把数据库失败降级成启用，不修改其它会话工作区。

## Verification Plan

先让真实执行路径复现红灯，再实现：两个独立实例/暖缓存先调用，配置停用、资源停用/删除后均零新 HTTP/MCP call；恢复、同名重建、重发现/重启得到当前实例；租户不匹配、缺上下文及数据库故障拒绝；旧凭据对象失效；全局生产装配经配置服务单个/批量开关真实阻断。用本地计数传输与假 MCP 模拟外部依赖，不发送真实客户消息。最后跑受影响包竞态、全量 Go、build/vet/lint/audit/架构门，冻结同一源码发布链 A。

## Evidence Supplement Before Discovery Response Fix

完整受影响包竞态发现旧 MCP 持久化 E2E 夹具只建来源表而未建配置表，准入正确拒绝缺表；该夹具补建 `AIToolConfig`，不降低缺表守卫。生产配置回执另有动态红灯：创建发现失败返回空 `LastStatus`，重新启用发现失败仍返回旧 `ok`。范围内 `mcp_client_test.go` 新增断言已真实失败；只同步本次发现回执状态，保留“配置已保存，发现失败可排障”的既有契约，不将保存成功宣称工具已就绪。

## Evidence Supplement Before Admission Port and Direct Consumer Fix

首轮冻结全门发现 AI 层直接依赖 repository 违反现有 depguard。新增低依赖端口 `user-server/internal/toolruntime/authority.go`，由 app/service 显式注入仓储实现；不增加门禁豁免。允许范围内旧加载夹具及调用点更新参数，确保生产编译要求接入准入依赖。新仓储测试在全包顺序下已存在配置表，不能假设 testutil 清空 schema；改用明确不存在的测试表句柄验证真实缺表，保留正常无行与缺表区别，不删除共享测试表。

另发现 MCP Server 的内置工具调用直接执行注册对象，绕过 Executor。范围内 app 的既有权限包装必须在实际执行前同样读取权威状态，覆盖 MCP/注册表直调；新增真实 JSON-RPC 初始化和 tools/call 回归，先复现停用后仍调用的红灯，再修装配。已经准入的在途边界保持原协议。
