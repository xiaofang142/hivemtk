# Test: 多实体去重 / 合并快照 / 人工撤销合并

- **Slug**: crm-dedupe-multientity-20261125
- **Date**: 2026-11-25
- **环境**: 真实 PostgreSQL 测试库 `POSTGRES_TEST_HOST=127.0.0.1 POSTGRES_TEST_PORT=8217`，口令由外层 `../.env` 读取后经环境变量注入；`testutil.NewTestDB` 显式模型列表，`-p 1 -parallel 1` 串行，不并发启动多个 go test。

## RED → GREEN 过程

1. 先写 10 个失败测试（4 个 CRM-06、3 个 CRM-07、3 个 CRM-08），当前实现缺 `FindEntitiesDuplicates`/快照/撤销方法，编译期即失败（RED）。
2. 最小实现后重跑；期间修复 3 个真实 FAIL：
   - 字段前后值测例断言写反（主号空、次号有值时应断言 email 补全）→ 修测例；
   - 存量快照预览因客户 ID 不存在被误报 403 → 不可恢复标记优先于对象存在性检查；
   - 撤销成功路径因 GORM autoUpdateTime 把合并自身写入误判为「合并后新写入」→ 并发版本改为事务内合并后回填；
   - 逆操作测试替身用 `Create` 恢复软删客户撞 `customers_pkey` → 改为 `Unscoped().Updates` 复位 `deleted_at`。
3. 最终限定测试全绿。

## 最终命令与真实结果

```bash
cd user-server
POSTGRES_TEST_HOST=127.0.0.1 POSTGRES_TEST_PORT=8217 \
POSTGRES_TEST_PASSWORD=<从 ../.env 读取> \
go test -p 1 -parallel 1 -count=1 \
  -run 'TestMergeSnapshot_|TestUndoMerge_|TestMultiEntityDedupe_' \
  ./internal/service/ -v
```

结果：`ok hivemtk-user/internal/service 3.211s`，**10 PASS / 0 FAIL**（含 config.yaml 回落默认、JWT 测试密钥两条 WRN，非失败）：

- `TestMultiEntityDedupe_SupplierPhoneHitAndNonDuplicate` PASS（相同手机号供应商成对、不同手机号不成对）
- `TestMultiEntityDedupe_ContactAndServiceProviderAdapters` PASS（邮箱/手机号分别适配联系人、服务商）
- `TestMultiEntityDedupe_RoleCoexistenceNotAutoMerged` PASS（同手机号跨供应商/服务商角色 review + 角色提示）
- `TestMultiEntityDedupe_ScopeAndUnsupportedMarking` PASS（越权客户实体被排除；不支持实体显式 `unsupported`）
- `TestMergeSnapshot_PersistedWithMerge` PASS（快照含主次对象/字段前后值/规则版本/操作者/并发版本/关联计数/Uncaptured）
- `TestMergeSnapshot_RollbackOnSnapshotFailure` PASS（审计写失败注入：无审计行、次客户未软删，整体回滚）
- `TestMergeSnapshot_LegacyMarkedUnrecoverable` PASS（存量无完整快照标记不可恢复）
- `TestUndoMerge_SuccessAndConsentNotWidened` PASS（冲突检查后撤销成功、次客户恢复、重复撤销幂等、许可事实不改写）
- `TestUndoMerge_ConflictRejectsAndHints` PASS（合并后新写入显式拒绝并给人工线索、次客户未被恢复）
- `TestUndoMerge_ForbiddenScope` PASS（越权撤销返回 `ErrCRMCustomerForbidden`）

## 编译与静态检查

- `go build ./internal/service/`：rc=0（最终状态下我的边界文件无编译错误；执行期间其他 agent 在途文件曾短暂报错，已由其自行修复）。
- `gofmt -w`：对 5 个改动的 Go 文件执行，`gofmt -l` 无输出。
- `go vet ./internal/service/`：过滤 `crm_dedupe|crm_merge|customer_dedupe` 无输出（我的文件无 vet 报错）。

## 未执行的检查 / 受限项

- 未跑全量 `go test ./internal/service/`：并行会话 Bridge/Webhook/Lead 等域存在大规模在途基线失败，按指令不跑全量。
- 未跑 UI 真机点验；`user-web/src/views/crm/Dedupe.vue` 仅做 UI 接线，未验证真实接口（实体扫描 HTTP 入口因禁止改 `router/*` 未接线）。
- 未跑 `go test ./...`、未 commit/push/发布。
