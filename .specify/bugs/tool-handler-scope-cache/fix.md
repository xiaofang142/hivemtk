# Fix — 工具处理器作用域与登记代际

- Date: 2026-11-20
- Assessment: [assessment.md](assessment.md)
- Stage: implementation complete; complete delivery gates remain to be recorded in test.md.

## Changes

- 注册表同锁读取实例、来源、名称和登记代际；每次成功登记增加单调代际，同名替换不再先注销。
- handler 缓存绑定可信请求租户、注册表和名称；代际不同时重建完整执行/审批/风险链。
- 旧在途快照可以结束，但不覆盖已缓存的新代际；每次新调用先解析注册表，撤销不能被缓存绕过。
- 按名称的 override 变更清理该名称在所有作用域的缓存，既有重试策略/全缓存清理保持。
- 新增六组真实执行器回归：双租户交错及审计/权限上下文、原子替换、来源切换与同指针重新登记、
  风险升级、64 路并发首次调用、更新/撤销与旧在途调用边界。参数中的假 tenant_id 不参与作用域解析。
- 无配置、迁移、依赖或生产凭据变更；工具体仅返回本地版本标识并计数，没有外部发送。

## Red / Green Evidence

修复前 `go test -count=1 -v ./internal/aiagent/agent/tooluse -run TestToolExecutor_ScopedHandler`
退出 1，原五组均失败：B 返回 A 标识、来源切换复用 builtin、高风险更新复用旧只读实例、
并发 64 次全落一个租户、更新后在途测试再次执行旧体。真实动态复现日志保存在本次运行临时证据中。
修复后相同五组退出 0；再增加原子替换与权限上下文覆盖。

完整工具包首次竞态运行中 384 组通过、6 组因未注入测试库口令而在连接前被环境门拒绝，
不是业务断言失败。补齐独立测试库 127.0.0.1:8217 环境后，
`go test -race -count=1 -v ./internal/aiagent/agent/tooluse` 退出 0，390 组通过、0 失败、无数据竞争。
其余整仓门、实际提交与发布事实在测试报告及正式拓扑登记中如实记录，不能以局部通过代替上线。
