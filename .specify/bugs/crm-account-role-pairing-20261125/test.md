# Bug Verification: 账号授权配对

- **Result**: scoped GREEN（纯函数 + 组合限定回归）
- `go test ./internal/service -run 'TestAccountRulePairing_' -count=1 -v`：3/3 PASS，覆盖交叉账号行业拒绝、owner/viewer 混合不互扩、管理员不受影响。
- 组合限定回归（含 CRM-02/03/05/25 全部新增用例与既有 CRM 权限用例）：全绿。
- `go build ./...` rc=0；gofmt clean。
- 未做 UI/真机/生产/外发/部署/提交。

## 2026-11-26 扩展验收（尚未整项关闭）

- PostgreSQL 测试端口 `127.0.0.1:8217`，测试库 `user_db_test`；密码仅通过进程环境注入，不写入报告。无业务库重置或迁移。
- 修复前已观察 RED：孤儿账号被保留、全局行业/owner 扩散、未知别名被接受、合法别名写权限与读取不一致、关联实体跨账号可读写，以及空授权/异常行业/缺租户边界。
- 修复后限定回归 rc=0：`go test ./internal/repository ./internal/service ./internal/controller -run 'TestCRMAccountPairs_|TestCRMAccountSourceReferences_|TestCustomerRecordEndpoints_|TestCustomerPermission_|TestCRMCustomerRepository_ListScoped_AccountRules|TestAccountAuthorizationService_RevokeAffectsPermissionImmediately|TestCustomerPermissionService_TwoEmployeesTwoAccountsAreIsolated' -count=1`。最近一轮仓储 1.135s、服务 1.945s、控制器 1.670s。
- 覆盖真实授权表 → 数字账号/瞬时码解析 → SQL 列表/计数 → 详情/写权限；混合 owner/viewer、当前码/有效旧码/过期码/歧义码/孤儿授权、轮换、撤权与管理员正例。
- 公开导出与去重入口验证交叉配对拒绝；505 条合法数据的第二页 5 条、总数 505 和跨页导出 505 条通过。三类实体分页返回 ID 与总数、360、实体去重、越权旧实体更新/删除及合法更新均覆盖。
- 三个客户记录控制器分别验证授权 200、拒绝 403、解析失败 500，错误响应不包含客户私有资料或数据库错误。
- `go build ./...`、`go vet ./...` rc=0。分层检查、架构检查、GORM-only、裸 goroutine 检查 rc=0。最终 lint 进程退出结果已确认 rc=0、`0 issues.`，见下节最终质量结果。
- 全仓 `make fmt-check` 被 52 个任务外测试文件的格式阻塞，未全局改写这些文件。`make audit` 前置门通过，最终因 i18n `locale_orphans=1` 超过基线 0 失败；未修改无关词条。
- 全后端 `go test ./... -count=1 -p 3 -timeout 1800s` 被宿主 1800 秒时限终止（SIGTERM，143），未取得完整通过结果。终止前已观察任务外的 bridge `TestBindBridgeTenant_ValidIdentifyCodeBindsTenant`、`TestBindBridgeTenant_ChannelFallbackQueryErrorRejected` 失败，以及 controller `TestSageXBootstrapControllerTypedErrors/oversized_installation_id` 在并发改动的渠道解析器路径发生空指针 panic；未改写这些并发工作，不能声称全仓通过。
- graphify 刷新已尝试，环境缺少 Python `graphify` 模块（`ModuleNotFoundError`）；未安装依赖或更改全局环境。
- 只读复核发现并已修复权威渠道无账号时的冗余同码误归属；补丁复核在 CRM-24 范围内未发现剩余可执行问题。复核不代替运行验证，任务清单暂不删除；无 stage/commit/push/deploy/reset/migration。

## 2026-11-26 复核补充与重启恢复

- `TestCRMAccountSourceReferences_OrphanAuthorityBlocksLegacyAlias` 的同租户/跨租户 × 当前码/有效旧码四个真实 PostgreSQL 子用例，先观察四项 RED，修复权威声明校验后全部 GREEN。
- `go test ./internal/repository ./internal/service -run '^TestCRMAccountSourceReferences_|^TestCRMAccountPairs_' -count=1` rc=0，仓储 4.310s、服务 3.355s；包含跨行业检索的授权客户 ID、总数 3/2/1 及交叉表配对检查。
- 后续扩大限定回归中仓储包已完成并通过（11.459s）；服务重启中断其余包和质量检查，不能将中断当作通过。已保留该包结果，只恢复 service/controller/middleware/router 及未完成的质量命令。
- 新增真实 DataScopeGuard 与 CRM 共同作用的三个客户记录入口矩阵，以及关联实体未挂账号继承、显式零账号拒绝、未提交账号时改挂拒绝且原对象不变的测试；对应 controller/middleware/router 恢复回归及最终 service 回归均通过，见下节。

## 2026-11-26 稳定账号边界回归

- 恢复的限定回归中 controller 7.183s、middleware 0.929s、router 4.310s 通过，包含真实 DataScopeGuard 与 CRM 的共同授权矩阵。service 中既有 `TestCRMAccountRulePairAllowsWrite/rule_cannot_widen_restricted_accounts` 失败，确认规则不能越过受限 scope 的稳定账号边界；未将此失败归为任务外问题。
- 新增真实 PostgreSQL `TestCRMAccountPairs_StableAccountBoundary`，先验证冲突账号集合、空集合、仅含别名的集合和越界附加规则会错误放行，RED 已复现。测试同时比较详情、写权限、SQL 列表与总数，并保留合法数字账号映射别名和非受限手工规则的正例。
- `ToFilter` 和 `CanAccess` 的规则循环已增加相同的稳定账号成员检查；最终服务回归 `go test ./internal/service -run 'Test.*(CRM|Customer|AccountRulePair|AccountAuthorization|DataScope|CrossIndustry|IndustryScope|SessionScope)' -count=1 -timeout600s` rc=0（27.356s），上述既有失败及新增边界矩阵已转 GREEN，包含实体未挂账号/零账号/改挂边界。
- 最终质量结果：在稳定账号边界补丁后，使用 `GOTOOLCHAIN=go1.26.11` 依次执行 `go build ./...`、`go vet ./...`、`golangci-lint run ./... --allow-parallel-runners --timeout 20m`，三项退出码均为 0，lint 输出 `0 issues.`。日志：`/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/crm24-stable-boundary-quality-20261126.log`。
- 上一轮恢复质量检查中的分层、架构、GORM-only、裸 goroutine 均为 rc=0；本次边界补丁仅在既有两处规则循环增加稳定账号成员检查。19 个任务内 Go 文件的格式检查和任务内差异空白检查通过，未格式化任务外文件。
- 当前结论：CRM-24 实现与上述限定验收已通过，补丁只读复核未发现剩余可执行问题；全仓验收仍受前述格式、i18n、全后端测试及 graphify 环境问题限制，不能声明全仓通过或整项关闭。任务清单继续保留；无 stage/commit/push/deploy/reset/migration。

## 2026-11-26 自动发布准备与基线对照

- 用户已授权继续本项集成、提交、发布与线上验收。独立 master 副本基于 `15376884ae9596d244a7426899bbac355085f262`，仅暂存本项 19 个 Go 文件和 3 份报告；主工作区及其空索引未改变，未混入并发修改。
- 独立副本的新一轮 `go build ./...`、`make lint`（`0 issues.`）、`make vet` 均为 rc=0。已暂存 Markdown 扫描 839 个文件、断链 0；两道真实凭据泄漏门均已执行通过。
- `make audit` rc=2：唯一孤儿词条为 `多账号聚合`，i18n 全部输入文件均与 HEAD 相同。`make fmt-check` rc=2：52 个失败文件均与 HEAD 字节相同，且逐一验证其 HEAD 内容本身未格式化。未更新基线或改写任务外文件以获得绿灯。
- 全量 `make test-go` 已观察两项 bridge 租户绑定失败和 SageX 控制器 panic。另从纯 HEAD 导出源码，运行同名失败用例，rc=1 且复现相同结果：`TestBindBridgeTenant_ValidIdentifyCodeBindsTenant`、`TestBindBridgeTenant_ChannelFallbackQueryErrorRejected`、`TestSageXBootstrapControllerTypedErrors/oversized_installation_id`。这确认上述失败不由 CRM-24 补丁引入；全量运行最终状态仍待收集。
- 使用官方 `deploy/test_gate.sh` 对 HEAD 对照日志及真实退出码 1 分类，结果为 `BLOCK`。未使用 `--skip-tests`、`--force` 或缺失数据库凭据方式绕过发布门禁，尚未 commit/push/deploy。
- 发布前只读生产基线：既有管理员会话的 `/api/auth/current-user` 和 `/api/manage/crm/customers?page=1&page_size=1` 均为 HTTP 200/code 0，列表总数 30、返回 1 行；未输出客户内容或凭据。此证据仅代表旧版本发布前可用，不代表新补丁已上线或权限矩阵已完成生产验收。
- 当前日志位于批准的临时目录：`crm24-release-quality-20261126.log`、`crm24-release-full-tests-20261126.log`、`crm24-release-staged-and-secret-gates-20261126.log`、`crm24-release-head-baseline-failures-20261126.log`。保留 CRM-24 清单条目，未开始其他任务。
