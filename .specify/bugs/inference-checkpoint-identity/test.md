# Bug Verification Report: 检查点身份隔离与安全策略重检

- Slug: inference-checkpoint-identity
- Tested: 2026-11-20
- Assessment: ./assessment.md
- Fix: ./fix.md
- Result: 定向回归与八项门禁通过，生产发布待独立核验

## 回归证据

| 验证 | 结果 |
|---|---|
| 原恢复链 | 七组身份或配置变化仍沿用旧游标，当前门禁可被跳过。八个预期失败。 |
| 原 PostgreSQL 仓储 | 同一线程读取两个租户、缺租户仍能读取、租户 773 写入默认租户 1。三条预期失败断言。 |
| 自定义安全阶段名称 | 修复前恢复时门禁执行零次，复核执行一次。修复后按职责位置执行。 |
| 检查点专项 | 103 个叶用例通过，零失败、零跳过。 |
| 正式源码定向回归 | 四包匹配 `Checkpoint` 或 `^TestInference(Delivery\|Execution\|Outcome)`，230 个叶用例通过，零失败、零跳过。 |
| 本地 PostgreSQL 装配 | 检查点开关关闭写入零行，开启写入五个阶段边界。 |

用例覆盖配置字段与租户、消息、账号隔离，以及传输时间、trace 和配置加载时间改变后同身份稳定恢复。缺可信身份或编码失败时完整执行当前阶段并保持检查点零读写。旧版或无效快照触发全链执行。安全门重检失败、取消或超时均保留成功快照和独立的 `LastAttempt`。流式路径覆盖成功、人工交接、技术错误和复核拒绝。真实 PostgreSQL 验证系统豁免标记下的租户隔离、跨租户更新隔离、缺租户拒绝和取消传递。

## 完整门禁

| 命令 | 退出码 | 耗时 |
|---|---:|---:|
| `make test-go-fast` | 0 | 566.6s |
| `go test -race`（delivery、runtime、repository、service、controller、app、tooluse 七包） | 0 | 823.8s |
| `go vet ./...` | 0 | 18.4s |
| `go build ./...` | 0 | 17.5s |
| `golangci-lint run --timeout=10m` | 0 | 20.8s，0 issues |
| `python3 scripts/check-layer-boundary.py` | 0 | 0.3s，1600 份非测试 Go 文件，0 违规 |
| `bash scripts/check-architecture.sh` | 0 | 32.5s |
| `make audit` | 0 | 100.9s |

运行前后逐份校验 6683 个冻结文件，变更为零。架构检查保留已有 `webhook_wecomcs_puller.go` warning。审计因缺少平台仓库副本、核心域覆盖率文件及本机 8204 服务，分别跳过跨仓核对、覆盖率门和本机口令探针。Graphify 刷新因缺少 Python 模块而失败。生产健康、口令探针、回滚点和既有表约束由发布阶段独立验证。

## 日志摘要

| 文件 | SHA-256 |
|---|---|
| `checkpoint-identity-overlay-red.log` | `1fcef97568ce59158ac128ddc193e8645ec52090a326427b089019cd53e37480` |
| `checkpoint-repository-overlay-red.log` | `9d176908b49126e3c4547ad0627d09d8d63c74ece65f43e63ebe43b8851f045d` |
| `checkpoint-identity-renamed-gate-red.log` | `5e80429c6b85d64fa4f9158b210a2d8c0fe9851448e1fd4a8a03542daab96da6` |
| `checkpoint-identity-formal-boundaries.log` | `389595730edbedeff926aa1fb178385f5c31d13160b4069d72b032d81a0eb302` |
| `checkpoint-identity-final-targeted.log` | `13fd14790be5eb0f902069ead280d54b16936f0c1cdc1273590bc390c012d0ba` |
