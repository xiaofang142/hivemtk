# Bug Verification Report

- Slug: inference-stage-outcomes
- Tested: 2026-11-20
- Assessment: [assessment.md](./assessment.md)
- Fix: [fix.md](./fix.md)
- Result: verified（功能与本地门禁；生产发布及回滚核验另行登记）

## Summary

原调度器的十三项独立复现均出现预期业务红灯；修复后的五阶段故障、超时、复核拒绝、流式及恢复验证通过。最终定向集共五十九项独立叶用例通过，其中包括七组真实 HTTP 路由测试和十组真实 PostgreSQL 故障/超时恢复测试；无跳过。八项完整门禁均退出 0，验证前后 6657 份冻结文件的 SHA-256 无变化。

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| 原始复现 RED | `go test ./internal/aiagent/agent/runtime -run '^TestInferenceOutcome' -count=1 -v` | 预期失败 | 十个阶段错误/超时及检查点、流式复核、拒绝外发三个独立场景，共十三例 |
| 修复后定向回归 | `go test ./internal/aiagent/agent/runtime ./internal/app -run '^TestInferenceOutcome' -count=1 -v` | pass | 五十九项叶用例，无跳过；runtime 0.662s，app 0.884s |
| 真实 PostgreSQL | `go test ./internal/app -run '^TestInferenceOutcomePostgres' -count=1 -v` | pass | 十个子用例；真实 repository/service/装配端口，app 1.541s |
| runtime 与 app 完整回归 | `go test ./internal/aiagent/agent/runtime ./internal/app -count=1 -timeout=1800s` | pass | runtime 2.429s，app 14.223s；安全注入本地测试库配置后执行 |
| 全量 Go | `make test-go-fast` | exit 0，485.0s | 官方测试端口 8217、认证预检、全量包；未把环境缺密码当产品故障 |
| 竞态 | `go test -race ./internal/aiagent/agent/runtime ./internal/app -count=1 -p=1 -timeout=1800s` | exit 0，84.7s | runtime 4.159s，app 27.975s |
| vet | `go vet ./...` | exit 0，26.4s | 无诊断 |
| build | `go build ./...` | exit 0，16.3s | 构建成功 |
| lint | `golangci-lint run --timeout=10m` | exit 0，14.5s | 0 issues |
| 分层 | `python3 scripts/check-layer-boundary.py` | exit 0，0.3s | 1596 个非测试 Go 文件；新增跨层违规 0 |
| 架构 | `bash scripts/check-architecture.sh` | exit 0，29.2s | 警告已人工复核，见下 |
| 静态审计 | `make audit` | exit 0，65.5s | 明确未执行维度逐项列于下文，退出 0 不代表这些维度通过 |
| 图谱刷新 | 标准 graphify 刷新命令 | 环境不可用 | `ModuleNotFoundError: No module named 'graphify'`，未刷新，不冒充通过 |

## Output Excerpts

```text
原始复现：13 个独立叶用例 FAIL，0 PASS，0 SKIP。
最终定向：59 个独立叶用例 PASS，0 FAIL，0 SKIP。
真实 PostgreSQL：10 个阶段错误/超时恢复子用例 PASS，0 SKIP。
golangci-lint: 0 issues.
跨层违规：命中 0 处；基线豁免 0；新增 0。
冻结文件：6657；changed_files=[]。
```

原始日志 `inference-outcome-red.log` SHA-256：`55383521bc18220647125062735a3671fe48d981a344d221c159c88856b5f446`。

最终定向日志 `inference-outcome-final-targeted.log` SHA-256：`4dd84a9c6b4d7c7302ca5258274b934b445613a35797c0dd9d0d68cf4ccf2740`。

真实数据库日志 `inference-outcome-postgres.log` SHA-256：`39ba9b2f3c6064649d2c32133b96d9db689826e227919bcb3d9746fa2ad256d2`。

完整两包回归日志 `inference-outcome-runtime-app.log` SHA-256：`895cb5471f1eb02107600c251b2bd75b08c04ceaad15cee05129a360e1f3c683`。

八项门禁的退出码、耗时及日志位置记录在 `inference-outcome-validation-results.json`；验证源码清单为 `inference-outcome-validation-source.json`。日志存放于本轮 OpenCode 临时证据目录，测试源代码与本报告随提交保存。

边界红灯单独定位到复核拒绝的 stage_end 错误状态和 reviewer StopResult 被误计转人工，已分别修复并回归。该轮六组 HTTP 红灯来自测试把既有字符串错误码误按整数解码，已修正测试以遵守原响应协议，不能把这些解码错误当作产品缺陷证据。成功响应仍为数字 code 0；阶段失败为 HTTP 500，业务拒绝为 HTTP 409，均附安全阶段诊断且不泄露原始提供方错误。

## Warning and Skipped Dimensions

- 架构警告为既有 `service/webhook_wecomcs_puller.go` 中小写私有 wecomCSService 拉取助手没有 interface；已核对其为 WebhookService 私有助手，本轮未改动，不是新增跨层或接口违规。
- worktree 缺少对端 hivemtk-platform checkout，ports 的 Step 2/5 跨仓核对及平台配置/Vite 核对未执行，不能算通过。
- 工作流扫描中可复用工作流、本地动作或容器动作存在一处无法静态判定；19 个二进制和 4 个不可读 node_modules 链接未纳入文本扫描。
- 无 `user-server/coverage/go-cover.out`，核心域分档覆盖率为 `SKIP(未判定)`；未声称覆盖率达标。
- 本机 127.0.0.1:8204 无实例，默认口令探针该维度未运行；正式发布必须执行线上探针并取得拒绝默认口令的真实证据。
- Graphify Python 模块缺失，标准刷新命令已尝试但未成功；不影响已运行的 Go 和静态门结论，不声称图谱已更新。

## Residual Risks

- 测试使用本地受控阶段端口和独立 PostgreSQL；未模拟成真实第三方生产送达，也未对生产客户发消息。
- 同步阶段必须协作遵守 context；没有通过遗留 goroutine 伪装对不协作代码的强制终止。
- 首阶段失败没有最后成功游标，诊断通过返回结果、流式及服务日志提供，不伪造已完成检查点。
- 检查点存储不可用时显式降级，不能承诺故障存储仍能持久化失败诊断；版本一快照重跑，不混用版本二状态。
- 内置流式通道保留既有非阻塞缓冲语义，满缓冲可能丢事件；失败/拒绝会关闭内置通道，不发成功 complete。外部自定义 handler 的生命周期由调用方管理。
- 规划与实际工具执行契约属于 RUN-02，外发/人工可靠受理及回执属于 RUN-03，检查点账号和配置指纹作用域属于 CKPT-01。本项不宣称解决这些独立问题，也不宣称跨进程恰好执行一次。
- 本报告仅记录功能与门禁验证；生产二进制、镜像、启动时间、公开口令探针、回滚点及清单销项须以随后真实上线记录为准。

## Recommendation

功能修复验证通过。保持产品源码与冻结摘要一致，完成报告链接检查后提交双远端，使用唯一 API 链 A 发布并独立核验线上二进制、镜像、双健康、公开口令探针和回滚点。上线记录完成后精准删除唯一清单的 RUN01 主行，继续 RUN02。
