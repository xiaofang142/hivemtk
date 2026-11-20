# Bug Verification Report: 推理可靠交付

- Slug: inference-reliable-delivery
- Tested: 2026-11-20
- Assessment: ./assessment.md
- Fix: ./fix.md
- Result: 定向与八项全量门禁通过，生产发布待独立核验

## Summary

旧编排接口对相同稳定消息重复执行时发布两次，且不返回持久交付键、外部受理或确认状态。真实默认接口与受控对端复现 RED 后，改为独立持久意向账本；发送前落库并获取租约，调用支持同键原子幂等和权威回执查询的对端，只有已持久确认的同身份回执才显示最终发布成功。

最终六包定向共 173 个独立叶用例通过，0 失败、0 跳过；其中本项 66 个，RUN01/RUN02 107 个。所有外部发送与人工交接验收使用受控信号，不代表第三方生产送达。真实持久化验收使用本地 PostgreSQL：127.0.0.1:8217 / user_db_test；凭据从测试容器安全传入，未打印或写入报告。

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| 原接口 RED | 重复执行相同稳定消息并核对实际发布次数和交付证据 | 预期失败 | 实际发布 2 次，无持久键或确认凭据 |
| 身份与回执 RED | 负版本、回执其他键/其他动作、拒绝统计 | 预期失败 | 4 个真实业务失败后最小修复 |
| 数据库存储证据 RED | MarkSend / Finish 注入写故障 | 预期失败 | 已落库意向丢失查询键；修复后返回真实 sending，不伪造确认 |
| HTTP 存储证据 RED | Controller 返回存储不可用 | 预期失败 | 503 曾丢失 data.key；修复后保留安全查询证据 |
| 六包定向 GREEN | `go test ./internal/aiagent/agent/delivery ./internal/aiagent/agent/runtime ./internal/service ./internal/repository ./internal/controller ./internal/app -run '^TestInference(Delivery\|Execution\|Outcome)' -count=1 -v` | pass | 173 叶用例；delivery 0.105s、runtime 0.397s、service 1.392s、repository 0.467s、controller 1.693s、app 2.453s |
| 全量 Go | `make test-go-fast` | exit 0 | 896.5s |
| 竞态 | `go test -race ./internal/aiagent/agent/delivery ./internal/aiagent/agent/runtime ./internal/repository ./internal/service ./internal/controller ./internal/app ./internal/aiagent/agent/tooluse -count=1 -p=1 -timeout=1800s` | exit 0 | 1434.1s；七包通过 |
| vet | `go vet ./...` | exit 0 | 28.4s |
| build | `go build ./...` | exit 0 | 24.8s |
| lint | `golangci-lint run --timeout=10m` | exit 0 | 27.9s，0 issues |
| layer | `python3 scripts/check-layer-boundary.py` | exit 0 | 0.5s，1600 份非测试文件、0 违规 |
| architecture | `bash scripts/check-architecture.sh` | exit 0 | 60.9s；既有 warning 保留 |
| audit | `make audit` | exit 0 | 175.6s；部分维度 SKIP，见限制 |
| 源码冻结 | 八门运行前后逐文件 SHA-256 | pass | 6677 份冻结文件，0 漂移；仅最终验收报告在收齐后回填 |
| 图谱刷新 | 标准 graphify 刷新命令 | 环境不可用 | 缺 graphify 模块，保存实际失败日志 |

## 关键验收

- 显式租户与稳定入站消息、账号、客户、会话、Agent 身份/版本及动作共同构成交付键；不同身份互不复用。同键修改正文触发冲突，不能以改正文绕过去重。
- PostgreSQL 唯一约束和短事务行锁防止并发重复意向；网络调用在事务之外。认领、发送预留和完成更新使用 token 比较交换，过期旧执行者不能写确认。
- 未装配 sender/policy、策略错误、拒绝、对端拒绝及回执查询失败分别可查；缺端口不外发。重试重新检查当前策略，不把旧授权当作永久通行证。
- 丢回执后先查询对端；已受理后查询不到、回执身份改变或回执不合法均不盲目重发。新建服务和仓储实例后从同一数据库恢复，实际外发仍为一次。
- 意向写失败或发送预留失败不发送；确认写失败不返回成功。错误响应保留查询键并读取数据库事实；数据库也无法读时明确 unknown，不输出未持久化的内存确认。
- 12 个并发请求使用真实 PostgreSQL，只有一次外发；重复提交/查询/重试不重复累计受理与确认。确认统计与回执对应，handoff_to_human 仅是意向。
- 鉴权路由支持查询和重新授权重试。查询 200 本身不代表 confirmed；重试未定 202、确认 200、确定拒绝 409；缺租户 403、其他租户不可见 404、非法键 400、不可用 503，不暴露私有原因或意向正文。
- 原工具授权/审批、实际计数、生成、最终复核及五阶段失败恢复回归保持通过；交付恢复不冒充工具序列恰好执行一次。

## Output Excerpts

```text
最终定向：173 PASS，0 FAIL，0 SKIP。
RUN03：66 叶用例；RUN01/RUN02：107 叶用例。
八项全量门禁：8/8 exit 0。
冻结文件：6677；changed_files=[]。
```

| Evidence log | SHA-256 |
|--------------|---------|
| inference-delivery-red.log | c61de4a251a024d503eb7f878ea835cda6fd583db21f0fc47714308cfd2067cc |
| inference-delivery-boundary-red.log | a80eac23517eb040f6e470cb7cd99fa451e0ca5ec2308adf000d1ab4898bac97 |
| inference-delivery-store-evidence-red.log | 1e9c588baeade50025f431719b09850da8f29938014a5d62f02a9754c614bae0 |
| inference-delivery-http-evidence-red.log | becc0f74278fe4b9748f50382f3cb5bad96df024b4643c3a849ce3584ac9e01e |
| inference-delivery-final-targeted.log | 0f838d1f209ec937a89ca752214aa8cc5d58e1c546efa6fe3050a1955a99abf5 |

## Residual Risks

- sender 必须原子按键去重且提供权威 Lookup；旧 Publish/Trigger 的 nil 错误不能证明送达，未自动桥接。生产 sender/policy 未装配时保持 blocked，其他执行端口未装配仍返回 503。
- accepted 是外部受理，confirmed 是有效确认。丢回执可暂时 unknown，通过同键查证恢复；不保证缺少确认能力的第三方自动重试安全。同步对端须协作处理 context，不宣称强制终止不合作实现。
- 本项不负责工具跨请求幂等，也不修复检查点配置指纹与账号隔离；后者由 CKPT01 独立验收。
- architecture 既有 webhook_wecomcs_puller.go 小写 Service 无 interface 的 warning 未因本项扩大职责而改动。audit 缺对端 hivemtk-platform checkout 的 ports/config/vite 跨仓检查、缺 coverage/go-cover.out 的核心域覆盖率、本机 8204 无实例的公开口令探针均为 SKIP，不能视为相应维度通过；正式部署另跑线上探针。
- Graphify 缺模块不能刷新；没有宣称图谱更新成功。
- 发布须独立核二进制、镜像、启动时间、双健康、公开口令探针、回滚、新表约束及无默认租户；未授权路由只验证鉴权关闭，不替代真实租户功能验收。

## Recommendation

本地门禁已满足，核对只有报告回填后一次提交代码和报告并双推。官方链 A 发布并核新表后登记真实台账，精准销项，再继续 CKPT01；其他并发审计内容保留。
