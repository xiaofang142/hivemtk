# Bug Fix: 工具来源时间精度

- Fixed: 2026-11-20
- Status: applied
- Assessment: ./assessment.md

## Changes

自定义工具和MCP来源的CreatedAt比较按PostgreSQL微秒精度统一截断，其余ID、租户、启用状态、凭据和执行配置比较保持严格。新增固定纳秒时间夹具，验证创建后立即调用通过、一微秒实际代际变化仍拒绝。

## Verification

- 未改动远端4ebf64ff复现MCP初始暖缓存拒绝。
- 新增TestToolRuntime_CreatedAtDatabasePrecision修复前RED，稳定复现自定义工具误拒。
- 修复后定向测试PASS：repository31.348s、mcp0.768s、app8.866s、approval0.312s。包含原MCP多副本停用/改配置/改凭据/删除重建防复活，以及app管理撤销。
- 测试仅加载专用8217数据库凭据，不加载部署功能旗子。

## Scope

本缺陷由提交前全量回归发现并即时修复。不更改其他会话文件。完整Go测试最终结果见本轮verification报告。
