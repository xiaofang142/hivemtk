# Bug Fix: FA-18 / FA-19 营销流程分支与快照隔离

- **Slug**: marketing-flow-branch-snapshot-20261121
- **Fixed**: 2026-11-21
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

调度器仅执行触发器可达、条件选中的后继；未选分支和孤立动作不执行。每层节点获得独立的深层输入副本，层内结果收齐后由单一调度器确定性合并，冲突明确失败。

## Changes

| File | Change | Notes |
|---|---|---|
| user-server/internal/content/service/marketing_flow.go | modified | 创建/更新/激活/触发/执行共用图校验；触发前复制输入，异步使用独立记录；替换旧整图调度 |
| user-server/internal/content/service/marketing_flow_graph.go | added | DAG验证、按ID稳定层序、可达选边、快照隔离、层后结果提交与冲突拒绝 |
| user-server/internal/content/service/marketing_flow_graph_test.go | added | 实际条件/短信spy、非法图零记录、并发隔离、汇合/冲突/取消/panic/更新拒绝 |
| user-server/internal/content/service/marketing_flow_test.go | modified | 旧触发夹具补合法trigger，不再以缺触发器图验证成功路径 |

## Tests Added or Updated

- 修前 red.log：条件yes/no都实际调用orphan/no/yes三个短信替身，两个分支断言均FAIL，问题复现。
- 六类非法图：cycle、disconnected_cycle、dangling、duplicate ID、blank ID、duplicate edge；校验和触发均拒绝、执行记录为0。
- true/false只调用选中短信替身一次；不真实发短信。
- 深层map与slice的并发写入互不可见，调用方输入不变，uint64大整数精度不丢失。
- 完成顺序a/b与同值/冲突四格：同值合并、汇合一次；冲突确定性失败、不运行后继。
- 嵌套条件、未选分支汇合、终止条件及控制键清理。
- 非法创建/更新/激活拒绝且无定义/版本/状态写入；取消、panic、循环输入不执行后继。

## Local Verification

- green.log：content四层完整普通回归通过，controller3.299s、model0.822s、repository2.861s、service4.176s（698 PASS条目含父子项）。
- race-full.log：最终四层完整race通过，controller10.374s、model1.451s、repository7.425s、service8.446s（700 PASS条目含父子项，0 FAIL，无DATA RACE）。
- gates.json：build0/24.238s、vet0/16.314s、lint0/10.980s（0 issues）、layer-boundary0/0.307s、architecture0/18.845s、现行goroutines0/1.943s、bare-go0/2.376s。
- 源码摘要见source-sha256.json。

## Deviations from Assessment

层内按节点ID排序（而非输入数组顺序），使定义数组排列也不影响合并或错误选择；条件NextNodes顺序仍保留真假含义。更新已激活流程也能替换图，故同一文件的UpdateFlow一并校验，防止绕过激活门。未改条件表达式或发送闸口；FA-20保持独立未完成。

## Follow-ups

本地修复证据同步唯一清单FA-18/19原行。当前混合工作树此前完整Go七包失败、audit另有他人未暂存文档断链（FA-21 test.md详列）；本轮未覆盖这些在途文件，也未提交/推送/部署。交付待验不另立任务。
