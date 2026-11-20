# Bug Test: FA-15 同意查询故障拒发

- **Slug**: consent-query-fail-closed
- **Date**: 2026-11-20
- **Status**: verified
- **Contract**: [assessment](assessment.md) / [fix](fix.md)

## Original Reproduction

修复前真实执行新增回归，退出 1：同意事件查询超时被当作 missing，偏好查询超时仍为 granted，主发送适配器被调用；空仓储及未知判定也未拒发。该次是业务断言失败，非编译或数据库连接失败。原日志 SHA-256：`98fcf1918376c5bb3e6d4e0bb91079ab84839d86b2a917c3536a0df9d5332614`。

## Checks Executed

| 检查 | 结果 |
| --- | --- |
| `go test -count=1 -v ./internal/service ./internal/repository -run Consent` | 退出 0，119 条含子例 PASS、0 FAIL |
| 隔离检出中 `go test -race -count=1 -v ./internal/service ./internal/repository -run Consent` | 退出 0，119 条含子例 PASS、0 FAIL |
| 隔离检出中 `make test-go` | 退出 0，1363.7 秒；包括专用测试库认证预检和完整 Go 测试 |
| `go build ./...`、`go vet ./...` | 退出 0 |
| `make lint` | 退出 0，0 issues |
| `bash scripts/check-architecture.sh` | 退出 0；既有非阻断 warning 保留 |
| 修复隐藏上层路径扫描后 `make audit` | 退出 0；分层、GORM、上下文、配置、链接等静态门通过 |

所有数据库用例使用 `127.0.0.1:8217` 专用测试 PostgreSQL；口令仅由本机环境提供，不进入报告。完整测试使用独立 Go/lint 缓存。

## Behavioral Evidence

两种同意模式 × 两类查询故障 × 28 个渠道名称均在 permission 步骤停止，主适配器与回退适配器调用数为零，不成功、不入队；回执与审计保留事件/偏好来源原因及可重试语义。恢复查询后，同一管线按真实 grant 发送，revoke/paused 仍拒发；正常无历史记录保持 revocation_only/enforce 原策略。空依赖、未知状态以及空 permission checker 均不能绕过已装配的同意闸。

## Source and Log Integrity

冻结清单中的 4457 个 Go 源码/模块文件在完整测试前后及交付前一致；按路径排序的 JSON 校验清单 SHA-256 为 `05abbd5d3514487d25887aa722d89b860e06c1054ee2e8affe15461345b13d00`。完整运行期间仅质量 Bug 的两个说明文件和两个审计脚本变化，未改变 Go 源码；原审计失败如实保留，修复后单独重跑完整审计通过。

- 竞态回归日志 SHA-256：`a8ddd6631a255f6a32b57bd3b220e93d4c2c3075e81e41cb36a3e37c935acc75`。
- 完整 Go 测试日志 SHA-256：`c86a82bfdc748fdc3ad0b72a1a58fa01b1e6c24f48e3c4c454cc188c2ee2ac5b`。
- 最终审计日志 SHA-256：`4a24821e391fde245291dc7dfac3daa8d237e120c4fccf477d8957428cd2a408`。

## Limits

故障信号为独立测试连接的受控查询超时，渠道适配器为可计数模拟；不代表第三方真实送达。未新增自动重试或修改客户事实。本地审计因无覆盖率制品跳过覆盖率阈值、因未起本机 API 跳过默认口令探针，均未宣称通过；线上探针由正规发布执行器执行。此报告验证代码修复，实际发布版本与回滚证据以发布状态文件及拓扑台账为准。
