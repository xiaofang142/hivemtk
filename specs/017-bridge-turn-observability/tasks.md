# 执行清单

## T1 真实计时与关联基础
- [x] T001 新增 observation_test.go：四目标真实RED（0.939秒）；作用域派生作为补充防御回归。
- [x] T002 Model可空时间/turn/attempt/Measured/Version；tracing Span、NodeSpan、ToolTraceEvent、pending与sink传播，NodeSpan无时间默认unknown。
- [x] T003 新观察纯函数、JSON凭证过滤、有界摘要；tracing完整回归GREEN（3.576秒），仅专用PG。

## T2 操作边界及终态
- [x] T004 入站派生turn并入context，出站继承并保存extra；首次/一次重生成answer_attempt，两次都测真实生成且记录决策出口。已执行BridgeObservation受控回归，临时空TraceID另行补强对账。
- [x] T005 真持久化前开启入队span；仅非零可投递ID标记enqueued，审计/失败明确；ACK前开启计时并继承hub范围/claim attempt。真实RED后相关观测/ACK控制通过。
- [x] T006 下发按claim attempt记录，不以全局msgID去重；保持claim/ACK fence及安全回归，先RED后GREEN。批次查询从逐条改一次，完整tracing回归3.223秒。

## T3 统计与消费者
- [x] T007 Repository作用域/turn聚合RED：同会话两轮、跨范围、重复ACK、failed/unknown、缺测不参与；实现coverage和有效服务端接入到ACK指标。完整monitor回归1.805秒，另补跨投递尝试gap控制。
- [x] T008 monitor Lifecycle/TraceTree按turn与answer_attempt归并；历史保留可见但不猜e2e，更新旧测试契约，学习回归不破坏。完整trace_learning回归2.269秒。
- [x] T009 Vue格式与归并RED：未测横线、实测0、attempt工具不混挂；最小实现并运行Vue定向/全量。真实组件/格式/九语言17项通过，管理端全量83文件884项通过；真实浏览器新页面待T011。

## T4 验证与收敛
- [x] T010 tracing/service/bridge/repository/monitor/trace_learning回归、tenant隔离、build/vet/层界及脱敏断言。第三轮正式make test-go退出0；T013之后完整repository/monitor再次通过（27.241/1.523秒），新静态检查另记总台账。
- [ ] T011 ego真实页面逐元素验证或记录阻塞；客户端缺测仍unknown，不虚构平台收件事实。
- [ ] T012 回读spec逐FR核对，写converge.md与主进度；仅全部覆盖后Converged，否则partial。

## Phase 1: Convergence
- [x] T013 [HIGH] 修复新追踪查询的上下文边界（FR-005、US3、边界“空context”）：`internal/repository/trace_observation.go` 的 `scoped` 拒绝空/取消/系统豁免与多租户未绑定上下文；单租户无绑定时明确限定默认租户。新增 `internal/repository/trace_observation_scope_test.go`，以真实专用PG数据验证全部查询入口不会崩溃或读到其它租户；先RED后最小实现，并运行完整 repository/monitor 回归。不修改既有缺测、轮次与回执统计契约。真实RED（1.182秒）后完整两包GREEN（27.241/1.523秒）。
