# Bug Verification: FA-18 / FA-19 营销流程分支与快照隔离

- **Slug**: marketing-flow-branch-snapshot-20261121
- **Tested**: 2026-11-21
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: partial

## Summary

原始分支错误已真实复现：旧调度中yes/no两种输入都触发orphan/no/yes三个短信替身。修后只运行选中分支一次，非法图触发零执行记录；同层深层输入独立、合并确定、执行状态由调度器单写。content四层完整race回归700个PASS条目（含父项/子项），无FAIL或DATA RACE。

结果记partial：本地修复验证成立，但本轮较早完整混合工作树七包失败，未提交/推送/部署；不把局部验证扩大为全仓交付完成。

## Checks Performed

| Check | Command / Action | Result | Notes |
|---|---|---|---|
| 原问题复现 | go test ./internal/content/service -run '^TestMarketingFlowGraph_BranchHasOnlySelectedSideEffects$' -count=1 -v（修前） | reproduced | red.log；yes/no都触发三个短信替身，两个断言FAIL |
| 四层普通完整回归 | go test ./internal/content/... -count=1 -v | pass | green.log；698 PASS条目含父子项；controller3.299s/model0.822s/repository2.861s/service4.176s |
| 最终四层完整race | go test -race ./internal/content/... -count=1 -v | pass | race-full.log；700 PASS条目含父子项；controller10.374s/model1.451s/repository7.425s/service8.446s；0FAIL/无DATA RACE |
| 编译 | go build ./... | pass | 24.238s |
| 静态分析 | go vet ./... | pass | 16.314s |
| lint | make lint | pass | 0 issues，10.980s |
| 分层 | python3 scripts/check-layer-boundary.py | pass | 0.307s |
| 架构 | bash scripts/check-architecture.sh | pass | 18.845s |
| 现行并发门 | python3 scripts/check-naked-goroutine.py；python3 scripts/check-bare-go.py --repo . | pass | 2402文件0 hit；81/81无基线漂移 |
| 差异/格式 | git diff --check；gofmt | pass | 目标文件无格式问题 |
| 本轮完整Go基线（营销修复前） | make test-go-fast | fail | FA-21证据目录full-go.log/full-go-summary.json：152条ok包结果含认证预检、7失败包、85顶层失败用例；未在营销修复后重复全仓运行，不记为最新全仓通过 |
| 总审计（较早实跑） | make audit | fail | FA-21 audit.log：他人在途docs/INDEX.md指向未暂存PILOT-CONTRACT.md；未为过门stage他人文件 |

## Coverage and Semantics

- 六类非法图的校验/触发拒绝，零执行记录；创建、更新、激活拒绝及版本/定义/状态不变。
- 条件真假、嵌套、终止、未选分支汇合和孤立动作；条件选择必须是声明边；控制键不进入共享业务数据。
- 同层嵌套map/slice并发修改互不可见；调用方输入不变；uint64大整数保真。
- 同值/冲突 × a/b完成顺序：同值合并且汇合一次，冲突明确失败不运行后继。
- panic、取消、循环引用输入不启动后继。
- 旧TriggerFlow成功夹具补合法触发器；异步worker使用独立记录/定义/输入，返回的running记录不再与worker共享可变对象。

## Residual Risks

- 同层冲突的动作可能已分别产生副作用，冲突判定不提供事务回滚或外部exactly-once；本次仅承诺共享数据确定性和下一层不执行。
- 预先复制JSON兼容map/slice/array并保留数字类型，不支持指针/函数/循环数据；此类数据在启动前明确拒绝。
- FA-20的短信/邮件统一发送闸口是另一项缺口，本轮未修，不能因分支正确推定其完成。
- 未真实发送、未进行UI或生产验收，未改他人的能力准入、回调、路由在途文件。

## Recommendation

唯一清单FA-18/19原行标记本地修复已验、整体交付待验并保留开放。当前混合工作树整体门禁尚未通过，不能提交或部署；按既有交付规则完成后凭证据销项。
