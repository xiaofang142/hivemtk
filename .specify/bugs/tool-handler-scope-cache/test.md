# Bug Verification: 工具处理器的租户、来源及代际隔离

- Slug: tool-handler-scope-cache
- Tested: 2026-11-20
- Assessment: [assessment.md](assessment.md)
- Fix: [fix.md](fix.md)
- Result: verified

## Summary

原缓存串用已由真实执行器和注册表动态复现，修复后不再复现。完整工具包竞态及整仓后端验证通过；验证前后 6605 个索引文件的内容摘要无变化。实际部署另以正式拓扑登记为准，本报告不提前宣称已上线。

## Checks Performed

| Check | Command / Action | Result | Notes |
|---|---|---|---|
| 修复前复现 | `go test -count=1 -v ./internal/aiagent/agent/tooluse -run TestToolExecutor_ScopedHandler` | fail, expected | 原五组均失败，B 实际调用 A，更新及来源切换复用旧对象 |
| 修复后回归及竞态 | `go test -race -count=1 -v ./internal/aiagent/agent/tooluse` | pass | 390 组通过，0 失败，无数据竞争；含六组新增作用域测试 |
| 全量 Go | `make test-go` | pass | exit 0，1075.1 秒，157 条 ok 包输出，0 条 FAIL；含专用数据库认证预检 |
| 构建及 vet | `go build ./... && go vet ./...`，在 `user-server/` | pass | exit 0，49.0 秒 |
| Lint | `make lint` | pass | exit 0，12.2 秒，0 issues |
| 静态审计 | `make audit` | pass | exit 0，99.3 秒；下述两个跳过维度单独保留 |
| 架构及分层 | `bash scripts/check-architecture.sh`；`python3 scripts/check-layer-boundary.py` | pass | exit 0；分层命中 0、新增 0，既有非阻断架构警告未隐去 |
| 源码稳定性 | 全部索引文件运行前后 SHA-256 比对 | pass | changed_tracked_files=[]；4458 个 Go/模块文件另生成交付清单 |

## Output Excerpts

修复前：跨租户结果标识错误、来源切换仍返回 builtin、高风险替换仍调用旧只读实例，以及并发首次调用全部落入同一租户，均触发业务断言失败。修复后六组覆盖可信租户/权限与审计、原子替换、来源与同指针重登记、风险升级、64 路首次并发、旧在途调用与更新/注销的边界。

完整日志位于本次执行环境的临时证据目录，名称及 SHA-256 如下，可核对原始输出；这些本地文件不冒充 CI 制品：

| Log | SHA-256 |
|---|---|
| `agent-tool-cache-red.log` | `ae0f566c8838e2db9917a9aeaf2a01df3ae8472774f34f0bd3219c3ea50fac87` |
| `agent-tool-cache-race2.log` | `afa7778ae3a5dd3b0a4dd2a1c4d0815f251dcd044d90bc95b7595fa9b1b74ffd` |
| `agent-tool-frozen-test-go.log` | `a81fb8d2f9eed6263f47182f671942d8f75f3dc628918632171ff56ac1e90afc` |
| `agent-tool-frozen-audit.log` | `fa71dd87c1192211bb5f6139d801257ab08b0cae2e4ce008eb8b01c402aadefc` |

4458 个 Go/模块文件的交付清单 `agent-tool-verified-go.json` 的 SHA-256 为 `0f39eabbd653e1edca403f68168964de639659f3c1c2e163be8efe7ae569fe6c`。发布前须逐文件核对同一内容。

## Residual Risks

- 工具体只执行本地计数和合成标识，不使用真实外部凭据或网络；验证的是实际解析、审批、风险、权限上下文及审计链的一致性，不是第三方服务可用性。
- 登记后的工具对象必须只读，修改通过重新登记生效；不支持并发原地修改对象。快照取得前后定义在途边界，已持旧快照调用可结束，新调用不能复活已撤销工具。
- 本任务解决注册表及处理器缓存，不等同于管理配置表禁用/删除已经接通全部运行消费者；该独立问题仍沿原 FA-AGENT-TOOL-02 跟踪。
- 第一次完整竞态运行因未注入测试库口令而在连接前被拒；安全注入专用 `127.0.0.1:8217` 测试环境后同命令通过，不把环境失败改写成业务通过。
- 审计没有覆盖率 profile，覆盖率维度明确 SKIP；本机 API 未启动，默认口令探针维度明确 SKIP，发布验收须实际执行线上探针。

## Recommendation

代码修复验证通过。按现有链 A 发布同一 Go 内容、记录二进制摘要与回滚证据并核对健康后，再对外层原清单本任务精确销项。
