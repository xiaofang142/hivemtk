# Bug Assessment: 消息中台请求与详情状态

- Verdict: valid（源码确认；真实首屏与分页数据已核对，故障/竞态用组件复现）
- Severity: medium

## Root cause

messageHub/List.vue 列表请求无代次保护；失败保留旧行且没有常驻错误，快速搜索可能被旧响应覆盖。详情打开不清上条消息，失败回落行数据并隐藏失败，迟到详情可覆盖新详情。手动推送在异步校验后才上锁，无函数级重入闸。已读操作只更新行，未读筛选下不刷新集合。统计失败不标旧值。

## Files

user-web/src/views/messageHub/List.vue；tests/unit/messageHub_request_state.test.js；审计台账 live-pages.md/README.md。

## Remediation

列表读取版本/严格 list,total 形状与错误态、清空旧行/选择项，失败重试；详情清空、错误重试、ID和版本隔离，关闭废弃请求。推送校验前锁、失败释放。已读成功后刷新列表和统计以遵循筛选。统计失败保留旧值但常驻错误可见。仅单测模拟写，不真实外发或撤回。

## Verification

并发列表顺序、失败不留旧可操作数据、详情切换/失败与重试、推送重入、未读筛选刷新。保留既有 unknown 状态回归。真实浏览器消息首屏/20条翻页验证通过，后续工具内部错误需恢复后续验。
