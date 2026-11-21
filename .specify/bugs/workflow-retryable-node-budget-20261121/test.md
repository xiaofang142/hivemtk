# Bug Verification: FA-WF-01

- **Slug**: workflow-retryable-node-budget-20261121
- **Tested**: 2026-11-21
- **Result**: pass（本地全绿；含真库 Run 路径）

## 关闭判据对照

- 「定义 error、无结果、永久拒绝与结果不明的分类」✔ 分类：executor error=瞬时（按预算重试）；failed 结果 Retryable=false=永久拒绝（零重试）；result==nil=瞬时。
- 「尊重 Retryable」✔（不可重试恰好 1 次执行、终态 failed、错误串标 not retryable）
- 「预算以节点/尝试轮次为键并在成功后正确收束」✔（节点键+终态清零+全局键退役断言）
- 「前一节点耗尽部分预算不压缩后一节点预算」✔（双节点脚本：a 重试后完成、b 以自己的键重试至完成、终态无残留键）
- 「不以重试次数日志代替计数接收器」✔（假执行器 atomic 计数断言）
- 「单独约束副作用重试」→ 副作用重试约束沿 FA-WF-03（side-effect 标记语义）各自范围，本项不重复计。

## 边界

- 重试恢复链路中已完成节点可能被再次进入（恢复协议起点），属 FA-WF-02/06 范围，本项不断言。
- 模糊受理（结果不明先对账）未在本项实现（结果不明的对账协议属 FA-WF-02/06 恢复协议）。
- 未提交/推送/部署。

## Evidence Files

fix.md / green.log / race.log / regression.log / gates.json / source-sha256.json
