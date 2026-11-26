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

## 2026-11-26 发布阻塞修复后验证

- 已将独立 `master` 集成至 `1bb9ead90ad793c1612dbc3b09c1da07e127d71c`；上文关于旧基线、格式阻塞及孤儿翻译的记录是历史状态，以本节及后续终态证据为准。
- 渠道解析器 PostgreSQL 新回归先验证未绑定租户与旧系统租户两项 RED，再完成最小修复。随后 `go test ./internal/service ./internal/bridge ./internal/controller -run 'Test(ChannelResolver_|BindBridgeTenant_|BridgeTenantForRequest_|SageX)' -count=1 -timeout=600s` 退出码 0，三包分别为 1.700s、1.442s、1.513s。
- 52 个基线文件的 gofmt 问题已修正；51 个纯格式文件已机械比对验证。未降低断言、关闭安全检查或更新门禁基线。详见 [阻塞验证报告](../crm24-release-blockers-20261126/test.md)。
- 全量 `make test-go`、静态门禁及单 worker 前端测试正在集成快照运行；不将先前并发超时视为通过。尚未提交、推送或部署，生产仍只有发布前只读基线证据。

## 2026-11-26 最终门禁与重启恢复

- 静态门禁已结束：`make audit`、`make fmt-check`、分层、架构、两道实际凭据检查及工作区/索引空白检查均 rc=0；审计中标记 SKIP 的外部输入检查仍未验证。权威 Python 裸 goroutine 检查及其七项自测通过；额外旧版 shell 检查的 61 个词法命中均在本项未改文件，详细差异见阻塞验证报告。
- 全量 Go 与前端测试被服务重启取消。已保留全量日志中 32 个完成包，排除认证预检的重复包结果；195 包中剩余 163 包按原参数恢复，并记录源码指纹与包清单。前端尚无完成结果，待 Go 与质量命令结束后串行重跑。中断不计为通过。
- 发布前浏览器复核继续复用原有会话：当前用户与客户列表 GET 均为 HTTP 200/code 0，角色 admin、列表总数 30/返回 1 行。此时 CRM-24 尚未发布；不以管理员冒烟代替受限授权矩阵，也不声称界面 404/网络超时已解决。

## 2026-11-26 Go 门禁终态与前端恢复

- 剩余 163 包运行耗时 48 分 26 秒，唯一失败包为 bridge，涉及六项 HTTP/确认回执用例。根因是测试服务器给两个抖音账号发送同一个渠道识别码；生产拒绝歧义符合预期。只将测试夹具改为按数字账号选用已有的独立账号识别码，保留全部断言和第二账号。
- `go test ./internal/bridge -count=1 -p 1 -timeout 600s` rc=0，包耗时 22.488s。其余 194 包源码指纹未变；32 个重启前完成包、163 包续跑及完整 bridge 复测组合覆盖 195/195 包，无缺失或未解决失败。这是多轮组合证据，不是单次不中断的全绿 `make test-go`。详见临时目录 `crm24-combined-go-suite-20261126.json` 与 [阻塞验证报告](../crm24-release-blockers-20261126/test.md)。
- `go build ./...`、`make vet`、`make lint` 均 rc=0，lint 为零问题。
- 外层流水线的一小时时限中断了随后进行的前端测试；758 条通过日志缺少完整套件终态，不能计为前端门禁通过。清单已确认 145 文件、1,329 用例，改为每批最多 15 文件、逐批保存原生 JSON 与真实退出码；所有文件重新完成批次验证后再判定门禁。旧前端缓存不用于冒充本轮结果。
- 尚未提交、推送、部署或完成发布后验收；CRM-24 保持未关闭。

## 2026-11-26 已验证快照与较新主分支

- `838ceb4681013e219bb8e70c17b9af0e5d5ea463` 加本项改动的完整 12 项门禁于 `13:17:19` 完成，全部退出码 0；独立校验确认 195 个 Go 包、19 项必需回归及所有子用例无跳过通过。前端原生结果为 145 文件、1,327 用例通过，失败/待执行均为 0，源码指纹未变。证据为批准临时目录的 `crm24-integrated-verified-20261126.json`。
- 其余 Go 记录含 122 条测试跳过；审计缺少覆盖率、本地 API 或兄弟仓输入的检查仍标为 SKIP，Graphify 因缺 Python 模块未刷新。
- 提交前双远端已到 `f0309fea7ca2c5bcdaef5bdfb5fc14456b8002cd`，含并行渠道、导航及部分 CRM 改动。补丁和 stash 已保存，六处重叠协调后无冲突标记；原 85 路径中 20 路径已在上游，其余 65 路径明确暂存。
- 较新源码单独运行 13 项门禁，并按实际收集的 147 个前端文件核对原生结果。生产并行发布基线已只读核对为 `f0309fea7`，管理员冒烟与本地受限授权矩阵分开论证。本项提交、双推、部署与发布后验收完成前，任务继续开放。

## 2026-11-26 合并后控制器夹具 RED/GREEN

- `13:34:40` 编译 RED 确认两个上传夹具引用旧类型名，先停止并保留该不完整验证批次，再仅把两处构造器改为现有 `ctrlBootstrapChannelRepo`。未改任何断言或生产代码。
- `go test ./internal/controller -run TestSageX -count=1` 于 `13:37:46` 退出 0，包耗时 `0.798s`。证据：`crm24-f030-controller-fixture-green-20261126.{json,log}`。
- 修复后完整门禁单独使用 `f030-fixturefix` 证据，仍须等待终态与独立验证；未将旧快照通过或中止记录代替本轮结果。

## 2026-11-26 七项失败诊断与新验证快照

- 完整 f030-fixturefix Go RED 已记录 195 个包终态及七项失败。诊断证据为 `crm24-f030-fixturefix-go-failure-analysis-20261126.json`，不把该批次记为通过。
- 仅四个测试文件新增修复，原权限角色/叶子/路径指纹和生产校验保留；KB 断言对齐现有 embedding 审计契约，线索夹具补完整检测证据，另补缺失、不完整、降级、报错四类证据下不产生客户/线索写入的回归。
- 14:18:49 定向 GREEN 为四包、70 条通过事件、零失败/跳过、退出码 0。证据：`crm24-f030-seven-blockers-focused-green-v2-20261126.{json,log}`。此前测试编译错误及改正单独保留。
- `crm24-f030-seven-blockers-repair-scope-20261126.json` 确认自完整 RED 后只有四个测试文件变化，归属路径共 69 个。14:19:34 启动唯一新完整 13 项门禁 f030-contractfix；前端收集 147 文件，完整终态和独立的 195 包/39 项必需回归及子用例/源码指纹核验尚待完成。任务保持开放。

## 2026-11-26 前端完整 RED 与定向 RED/GREEN

- f030-contractfix 于 14:44:34 结束为 RED。独立核验确认全部 195 个 Go 包、39 项必需回归及子用例通过，Go 构建/vet/lint 通过；其他 122 条 Go 测试跳过单列保留。前端原生结果 147 文件、1,335 用例：1,332 通过、三项失败、零待定；后续门禁未执行。
- 两个线索列表超时用例连同该文件其余七项在源码/测试/超时配置未改时全部通过。14:47:42 定向 RED 中，现有权威菜单断言及新增渠道聚合与分组共存断言失败，25/27 通过，证明修复目标；未削弱断言。
- 最小删除旧菜单迁移后，14:48:50 四文件 36/36 通过、零失败/待定，包含导航、原内容回归、线索筛选和引导。证据为 crm24-f030-navigation-focused-green-20261126.{json,log} 及原生 JSON。
- 新快照以 f030-navfix 重新执行完整 13 项门禁及独立核验，71 个归属路径须与提交源码一致。API 链 A 和前端链 D 发布、独立制品/运行态核验及只读验收完成前，不关闭 CRM24。

## 2026-11-26 14:58 完整 GREEN 与独立核验

- f030-navfix 全部 13 项门禁于 14:58:44 退出 0，独立核验于 14:58:46 全部通过。完整 195 个 Go 包：158 pass、37 无测试文件 skip；21,064 条测试事件通过、122 条可选 skip。39 项必需回归及所有子用例全部通过，无跳过。
- 前端精确覆盖收集的 147 个文件，1,336 总数/通过，失败/待定均 0，原生 success=true。构建、vet、lint、audit、格式、索引 Markdown 链接及两类空白门均通过；audit SKIP 仍属未验证。
- 原始证据：crm24-integrated-final-gates-f030-navfix-20261126.json、crm24-integrated-verified-f030-navfix-20261126.json、crm24-integrated-frontend-native-f030-navfix-20261126.json；源码/HEAD 未漂移，旧 RED 分批保留。
- 图谱刷新 5,863 跟踪代码文件，58,847 节点、98,685 边、1,525 社区；仅可选问题排序使用 64 点采样（seed=0）。双凭证门通过，零私有真值入档。提交和两面发布必须保持已测源码；线上权限受限矩阵仍由本地 PostgreSQL 回归证明，生产验收仅管理员只读冒烟，关单待发布验收。
