# Bug Verification: 工具停用与删除的权威运行准入

- Slug: tool-runtime-revocation
- Tested: 2026-11-20
- Assessment: [assessment.md](assessment.md)
- Fix: [fix.md](fix.md)
- Result: verified

## Summary

原问题已在真实管理服务、执行器、注册对象直调与 MCP JSON-RPC 服务端复现。修复后，停用或删除提交之后的新准入均被拒绝，两个暖缓存副本不能继续产生工具副作用；已获准的在途调用可完成。完整后端七项验收通过，运行前后 6614 个索引文件内容未变。正式上线另以拓扑登记和部署状态为准。

## Checks Performed

| Check | Command / Action | Result | Notes |
|---|---|---|---|
| 原托管工具与配置复现 | OpenAPI/MCP 生命周期及真实管理服务测试 | fail, expected | 停用、删除、配置变化和数据库故障时原实现仍产生计数副作用 |
| 原 MCP 直调复现 | `go test -count=1 -v ./internal/app -run TestToolRuntimeWiring_MCPAndDirectRevocation` | fail, expected | 绕过执行器的两条拒绝断言均失败 |
| 五包定向竞态 | `go test -race -count=1 -v ./internal/aiagent/agent/tooluse ./internal/aiagent/mcp ./internal/app ./internal/repository ./internal/service -run 'TestManaged|TestToolRuntime|TestMCPClient_CreateUnreachable'` | pass | 包含真实管理、JSON-RPC、直调、重试和在途边界 |
| 全量 Go | `make test-go` | pass | exit 0，1054.9 秒；157 条 ok 包输出、0 条 FAIL，含专用数据库认证预检 |
| 完整受影响三包竞态 | `go test -race -count=1 ./internal/aiagent/agent/tooluse ./internal/aiagent/mcp ./internal/app` | pass | exit 0，31.7 秒，无数据竞争 |
| 构建与 vet | `go build ./... && go vet ./...`，在 `user-server/` | pass | exit 0，39.5 秒 |
| Lint | `make lint` | pass | exit 0，2.4 秒，0 issues；没有放宽 depguard |
| 静态审计 | `make audit` | pass | exit 0，104.7 秒；下述两个 SKIP 维度另列 |
| 架构及分层 | `bash scripts/check-architecture.sh`；`python3 scripts/check-layer-boundary.py` | pass | exit 0，24.4 秒及 0.3 秒；分层命中及新增均为 0，既有非阻断警告保留 |
| 内容稳定性 | 全索引文件前后 SHA-256 比对 | pass | changed_tracked_files=[]；4464 个 Go/模块文件另作交付清单 |

## Coverage of the Reported Behavior

- 两个独立托管 OpenAPI/MCP 实例及暖缓存执行器：先调用，再停用、删除、同名新 ID 重建、重新加载及发现；旧实例拒绝，新实例按权威状态执行。
- 单独轮换合成 MCP 凭据后，旧实例零调用，重新发现后的实际 JSON-RPC 请求携带新合成凭据；不使用真实外部凭据。
- 实际全局执行器装配、生产注册包装、MCP 服务端和注册对象直调都接入准入；不是仅验证列表查询。
- 真正工具体首次执行后撤销配置，后续重试不再进入工具体；已准入旧调用被通道阻塞期间撤销，新调用拒绝，旧调用释放后仅完成一次。
- 缺失端口、未初始化数据库、缺少可信租户、明确不存在的表、跨租户来源、事实查询故障均拒绝；正常没有配置行仍保持未配置内置工具的既有策略。
- MCP 发现失败仍保留配置保存契约，但本次回执明确 `last_status=error`；保存不冒充工具已就绪。

## Evidence

本次执行环境保留的原始日志名称及 SHA-256 如下。这些是本地证据，不冒充 CI 制品。

| Log | SHA-256 |
|---|---|
| `tool-runtime-red.log` | `ca414733015dd766b917f803b450219474c791a7dab773afd9ab8693e1fe5fac` |
| `tool-runtime-wiring-red.log` | `beeb5f7201698ca4b98adcc550b882e188150eb55ae425f3af29ba25ca133c89` |
| `tool-runtime-direct-red.log` | `4898d4b7ca25e9a3745fcd194c8e07eca6c27bbadfda357f560c8db4e7f576f6` |
| `tool-runtime-final-test-go.log` | `c5cd3b25e865b945c2553868ac384f887c6bbd5df1412a5a5e65d37d4c912eda` |
| `tool-runtime-final-race.log` | `0ceafa84a3bf71c31dc94e8df200e0898fea84c39c015a7ccbe1619b7c6fae9f` |
| `tool-runtime-final-audit.log` | `b7f222d97904726caa2713bc5893131e1fd66a654fd4c582bc804420373ecfe3` |

4464 个 Go/模块文件清单 `tool-runtime-verified-go.json` 的 SHA-256 为 `90bf74252774de0b8c2fc113c567949c5fb9cfcc1dd8744559ac1e7130774b36`，发布前逐文件核对同一内容。

## Residual Risks and Explicit Limits

- HTTP 工具体使用计数传输，MCP 使用本地真实 JSON-RPC 测试服务；模拟的是第三方信号，不等同第三方生产可用性。生产 SSRF 防护未放宽。
- 首轮 lint 拒绝 AI 层反向引用仓储，已改为显式低依赖端口注入；首轮缺表测试误假设共享测试库未建表，已改为查询明确不存在的夹具表，不删除其它测试表。上述失败均保留，最终重新执行完整验收。
- 没有覆盖率 profile，覆盖率维度明确 SKIP；本机 API 未启动，默认口令探针维度明确 SKIP，正式链 A 仍须执行线上探针。
- 本任务无新增表、迁移、生产配置或前端改动；没有声称完成真实浏览器操作验收。管理回执与列表的既有状态字段承载发现失败信息。

## Recommendation

代码验证通过。按原链 A 发布相同 Go 内容，核对线上二进制、健康和回滚证据后，再对外层原清单本任务精确销项。
