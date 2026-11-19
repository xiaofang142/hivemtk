# Bug Verification Report

- Slug: inference-plan-execution
- Tested: 2026-11-20
- Assessment: ./assessment.md
- Fix: ./fix.md
- Result: pass（本地行为验收与八项全量命令；生产上线另由台账核验）

## Summary

真实默认五阶段查单链在原接口上复现了四个业务断言失败：计划工具数 1 冒充实际调用数 0；内部“客户在查订单。请：1. 调 order.lookup …”提示被当作 final_reply 并发布；缺少规划/未执行元数据；统计将计划记作调用。修复后同一复现通过。定向集 107 个独立叶用例全部通过，无跳过，其中本项 48 个、原 RUN01 回归 59 个。

默认 Process/HTTP 仅返回 mode=planning、execution_status=not_executed、executed=false、plan 和 planned_tool_call_count；final_reply 为空、tool_call_count=0、publishable=false，不调用工具、发布或触发人工交接。显式 execution 必须配置必要端口，并在第一工具调用前完成全部调用的授权审批预检；使用真实结果生成正文，裁剪后的正文通过最终复核才发布。生产没有自动接通观察态全局工具执行器，缺端口返回 503，测试桩验收不等于生产第三方送达。

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| 原接口 RED | `go test ./internal/aiagent/agent/runtime -run '^TestInferenceExecutionDefaultPlanningHasNoSideEffects$' -count=1 -v` | 预期业务失败 | 1 个独立场景内四个断言失败，不是编译失败 |
| 工具原因 RED | 单跑 `TestInferenceExecutionErrorsStopPublication/tool_rejected` | 预期业务失败 | 原始原因未进入内部错误链；修复后稳定代码对外、原因留日志/Unwrap |
| 定向回归 | `go test ./internal/aiagent/agent/runtime ./internal/app -run '^TestInference(Execution|Outcome)' -count=1 -v` | pass | 107 叶用例，0 FAIL，0 SKIP；runtime 0.726s，app 1.017s |
| 三包完整回归 | `go test ./internal/aiagent/agent/runtime ./internal/app ./internal/aiagent/agent/tooluse -count=1 -timeout=1800s` | pass | runtime 2.847s、app 19.881s、tooluse 3.424s |
| Go 全量 | `make test-go-fast` | exit 0 | 726.1s，本地真实 PostgreSQL 测试环境安全注入凭据 |
| 竞态 | `go test -race ./internal/aiagent/agent/runtime ./internal/app ./internal/aiagent/agent/tooluse -count=1 -p 1 -timeout=1800s` | exit 0 | 250.1s，覆盖执行端口及工具门禁所在包 |
| vet | `go vet ./...` | exit 0 | 172.7s |
| build | `go build ./...` | exit 0 | 31.0s |
| lint | `golangci-lint run --timeout=10m` | exit 0 | 17.0s，0 issues |
| layer | `python3 scripts/check-layer-boundary.py` | exit 0 | 0.5s，1596 个非测试 Go 文件，0 违规、0 豁免 |
| architecture | `bash scripts/check-architecture.sh` | exit 0 | 28.7s；保留既有小写 Service 缺接口提示，未更改该文件 |
| audit | `make audit` | exit 0 | 75.7s；未覆盖维度单列，不把 SKIP 当通过 |
| 源码冻结 | 全部文件 SHA-256 对照 | pass | 全量期间 6663 份文件零漂移；最终只补正式报告 |
| 图谱刷新 | 标准 graphify 刷新命令 | 环境不可用 | `ModuleNotFoundError: No module named 'graphify'`，不宣称已更新 |

Go 命令在 user-server 执行；make、分层与架构命令在应用根目录执行。数据库验收仅使用本地 mtk-postgres-test、127.0.0.1:8217、user_db_test，凭据由容器安全注入且未打印。

## 关键行为证据

- 实际调用 0/1/2 次与 spy、结果列表和统计一致。第二工具失败保留第一真实结果及两次尝试计数，后续生成、复核和发布不发生。
- 所有调用先授权；第二调用待批、拒绝或未知状态时，第一调用不得抢跑。缺门禁、工具、生成、复核或发布端口，在第一副作用前失败。
- 工具报错或 Success=false、生成失败/空回复、复核报错/空结果/拒绝/反复修改、发布失败均不返回最终成功回复；原始错误不泄露到结果与 HTTP 封套。
- 授权、工具、生成、复核的取消或协作超时均停止后续动作；不宣称可以强制终止不协作的同步代码。
- 复核对象是实际裁剪正文。复核器修改正文后再复核一次，第二次仍变化则失败。无工具且确有 SkipLLM 正文也要经过授权和最终复核。
- 六组人工交接区分规划、缺端口、待批、拒绝、批准及端口失败；handoff_to_human 是意向，executed/execution_status 表示真实端口调用状态。
- 九组原 HTTP 路由用例覆盖默认/显式规划、执行、待批、拒绝、缺配置、非法模式、工具失败和生成失败，分别验证 200/202/409/503/400/500 及计数、正文、无泄漏。

## Output Excerpts

```text
原始默认链：1 个独立场景 FAIL，包含 4 个真实业务断言失败。
工具错误链边界：1 个独立场景真实 RED，修复后通过。
最终定向：107 PASS / 0 FAIL / 0 SKIP，其中 RUN02 48、RUN01 59。
完整 runtime/app/tooluse：exit 0。
全量八门：8 个命令 exit 0；冻结文件 6663，changed_files=[]。
```

本地日志位于受控临时目录，以下 SHA-256 可复核：

- `inference-execution-red.log`：`9c40a8aaeba45f4e001b40df1613f497c5034500d831e6415e97f7f619824c43`。
- `inference-execution-boundary-red.log`：`758659881cc04cf79ca1602774ad8e1f5f7df9d2f2604e9bc55805bbf714a7ec`。
- `inference-execution-final-targeted.log`：`2287d1a4ffcc89ddf0633536359a0aa4aa10d39378396e5eadc1b546b34ca0a2`。
- `inference-execution-runtime-app-tooluse.log`：`2b3a8f9db48faef64c444ad5e0757616c26bc05bafaa05cea1b5d6cb5ad2b5c2`。
- `inference-execution-validation-results.json`：保存八门各自退出码、耗时、日志位置及冻结文件核对结果。

## Residual Risks

- 工具、生成、发布和授权使用本地可计数信号以及真实默认链/HTTP 验收，不表示第三方生产送达。生产执行端口未配置时仍关闭。
- 202 仅表示本次请求待审批未执行，不建立持久审批队列。正式执行门须委托既有身份、权限、风险和审批策略，不能使用 approved 测试端口替代。
- 部分执行失败后保留实际结果，不自动重试整个序列；不把工具结果写进推理检查点冒充可安全重放。跨进程幂等、可靠受理及重启恢复属于 RUN03，配置指纹及账号隔离属于 CKPT01。
- 本轮未改 SPA、扩展、SDK。API 默认规划是有意修正旧伪执行结果；实际执行须显式选择 execution 并配置必要端口。
- architecture 的未修改文件 webhook_wecomcs_puller.go 有既有小写 Service 缺接口提示。audit 对端仓缺 checkout 的跨仓核对、无 coverage/go-cover.out 的核心域覆盖率、本机 8204 无实例的口令探针未执行，不能计作已通过；正式部署另实跑线上双健康和公开口令探针。
- Graphify 缺模块，刷新未完成；没有将图谱状态当作代码正确性的证据。

## Recommendation

源码与报告一次提交双远端后，仅 API 链 A 发布，独立验证线上二进制、镜像、启动时间、双健康、公开口令探针及回滚点，确认 SPA 摘要未变；真实上线结果另记 PROD_TOPOLOGY_CHANGES 台账。精准销 RUN02 后继续唯一清单，遇并发占用保留现场并推进下一独立项。
