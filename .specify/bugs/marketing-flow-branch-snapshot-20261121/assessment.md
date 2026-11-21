# Bug Assessment: FA-18/FA-19 营销流程分支与同层数据隔离

- **Slug**: marketing-flow-branch-snapshot-20261121
- **Created**: 2026-11-21
- **Source**: 外层唯一清单 FA-18 / FA-19；用户要求逐步推进
- **Verdict**: valid
- **Severity**: high

## Report

营销流程必须消费条件结果选边，非法图在发布/触发/执行前拒绝，同层节点不共享可变输入，执行状态单写、结果确定性合并。

## Symptom / Reproduction

content/service/marketing_flow.go:309–365对整图按拓扑层逐个启动所有节点，不读evaluateCondition的_next_node，导致未选分支也外发；同层goroutine无锁写execution.CurrentNode并读写同一executionData。validateFlowDefinition只判触发器存在；topologicalLevels对环回退全图执行并追加未访问节点。评估是源码确认，fix阶段新增无真实外发夹具实跑RED后修复。

## Root Cause Hypothesis

高置信：拓扑顺序被误用为可执行节点集合；层内执行与结果发布不隔离。必须区别结构依赖与实际被选入边，等待全层结果后由调度器统一提交结果并激活后继；条件选择为节点局部控制信息，不能复用前一条件残留_next_node。

## Proposed Remediation

建立确定性DAG验证/层序（按节点定义顺序），拒绝空/重复ID、悬空引用、重复边、环；保留已有允许未连接节点的存储兼容性，但仅trigger起始可达节点可执行。条件只激活其_next_node对应的已声明边；未选分支及其专属后继零执行，共享汇合节点按被选入边OR可达，等待其拓扑层后最多执行一次。

每层执行前递归复制JSON兼容输入，为每个节点提供独立快照；层内结果先收集，层后按节点ID确定性合并；同键相同值允许，同层不同值冲突明确失败，不发布不确定结果；_next_node/_condition_matched仅节点局部控制，不进入共享业务数据。调度器单写CurrentNode，不把execution指针交给工作goroutine；触发时先校验并复制初始输入与执行记录，避免请求与异步worker共享可变对象。保留并发，不新增基础设施。

**Files likely to change**:
- user-server/internal/content/service/marketing_flow.go
- user-server/internal/content/service/marketing_flow_condition.go（仅节点局部控制结果清理如必要）
- user-server/internal/content/service/marketing_flow_graph.go（新增确定性验证与执行调度）
- user-server/internal/content/service/marketing_flow_graph_test.go（新增纯调度夹具）
- user-server/internal/content/service/marketing_flow_test.go（必要的真实仓储验证）
- .specify/bugs/marketing-flow-branch-snapshot-20261121/ 阶段报告/日志
- 外层 docs/未完成任务清单.md 对应原行与索引

**Tests to add or update**:
- true/false真实条件与无外发动作spy分别断对应动作一次，未选分支及其后继零执行；嵌套条件/汇合/无边条件/多个trigger/孤立动作。
- 环、悬空和重复ID在校验及实际触发拒绝，零执行/零执行记录。
- 同层节点各自修改嵌套map/切片，观察不到对方写入，输入不变；相同/冲突结果与不同完成顺序的最终结果一致；-race。
- 已有content service/controller/model/repository包完整回归；build/vet/lint/边界门。

## Risks & Considerations

同层冲突原来是随机覆盖，现改为明确失败；条件内部控制键不再向下游泄露。当前整个混合工作树在前一FA-21全量回归有7包失败，详见其test.md，不覆盖他人在途能力准入、回调、路由文件。营销相关目标文件开工时git status干净。

## Open Questions

无阻塞决策。真实发送用例全为替身；不发布混合工作树。
