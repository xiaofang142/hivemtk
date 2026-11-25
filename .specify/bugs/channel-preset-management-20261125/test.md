# CH-20261125-01~10 测试证据

日期：2026-11-25 · 单包串行 `-p 1 -parallel 1`，不跑全量。

## 真实命令与结果

### 构建
```
cd /Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user/user-server
go build ./...
```
结果：`rc=0`（最后一次：`/tmp/ch-build12.log` 为空）。
中途两次编译失败均为我边界内的中间态，已当次修复：
- `channel.go:6` 未用 `strconv` → 由 `ParseEnabledValue` 使用；
- `dto.ChannelAccountManagedSave` 未定义 → 改为本包 `ChannelAccountManagedSave`；
- `channel_identify_code.go` 包级 helper 误用 `r` → 改 `gdb`；
- 接口新增 `GetByValidCode`/`RotateCAS` 破坏既有测试替身 → 撤回接口变更，改包级函数/可选端口断言。
另有 LEAD agent 并发产物 `channel_account_reply_binding_test.go` 缺 `Available` 造成一次测试编译失败，非我边界，未越界修。

### 限定测试（核心判据）
```
cd /Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user/user-server
go test ./internal/service/ -run 'TestChannel(Status|Resolver_ResolveTrusted|Identity_|Inbound_|Dispatch_Sticky)|TestChannelAccount(Credential|Manage)_|TestChannelPreset_' -p 1 -parallel 1 -count=1
```
结果：`rc=0`
```
ok  	hivemtk-user/internal/service	1.375s
```
覆盖用例（每项正反例）：
- `TestChannelPreset_ParseEnabledValue`（空/1/0/true/garbage）
- `TestChannelPreset_EmptyBootstrapAndTenantIsolation`（空库播种/幂等/两租户互不影响/选项反映启停）
- `TestChannelPreset_CustomChannelVisibleInOptions`（自定义渠道可见/租户隔离/停用生效）
- `TestChannelPreset_DirtyStateFailClosed`
- `TestChannelAccountCredential_MaskAndMerge` / `_ValidatePlatform` / `_MigrationStableAndIdempotent`
- `TestChannelAccountManage_ListFilterAndPaging` / `_UpdateKeepsUnloadedFields` / `_DisabledAccountRecoverable`
- `TestChannelStatus_RequireRunnableBothSides`（CH-05 状态契约）
- `TestChannelResolver_ResolveTrustedConflictRejects`（CH-07）
- `TestChannelIdentity_StableSelfAndUnknown`（CH-08）
- `TestChannelInbound_ReplayOutOfOrderAndPersistFailure`（CH-09）
- `TestChannelDispatch_StickyChannelAndPagination`（CH-10，600 候选分页）

### RED → GREEN
- 首轮 `TestChannelPreset_EmptyBootstrapAndTenantIsolation` 因租户初始化幂等预期写错而 RED（n=0），已修正测试预期（行为正确：EnsurePresets 已顺手初始化）。
- 第二轮 4 个用例 RED（迁移跨租户记录误报错、管理仓储与写仓储未共享行切片、持久化失败 outcome 未标记、跨渠道粘性静默换号），逐个修复实现后 GREEN。
- 最终上述限定命令全绿。

## 未运行 / 余项
- 未跑真实 PG 测试（未使用 `testutil.NewTestDB`；主 agent 回归窗口 + 测试库槽位冲突），核心判据以纯函数 + 内存替身覆盖。
- 未跑 `go vet ./...`、架构门禁、前端 vitest/eslint（不在本批收口窗口内；`fix.md` 余项已列）。
- 未做 UI 真机点验、commit/push/发布。
