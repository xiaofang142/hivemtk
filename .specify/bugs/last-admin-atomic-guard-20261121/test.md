# Bug Verification: FA-AUTH-01 原子守卫

- **Slug**: last-admin-atomic-guard-20261121
- **Tested**: 2026-11-21
- **Result**: pass（本地全绿；含并发夹具）

## 关闭判据对照

- 「按租户原子保护至少一个启用管理员」⚠ 口径偏差如实登记：现行为与既有 CountEnabledAdmins 一致，是**全局**至少一个启用超管（错误文案"系统至少需要保留一个启用的超管账号"），未改为按租户切分；守卫锁键因此为全局常量（不变量全局 ⇒ 锁全局，语义自洽）。若将来改为按租户不变量，锁键随租户扇出即可。
- 「统一禁用、删除、降级角色路径」✔（三入口共用 lockLastAdminGuardTx + countOtherEnabledAdminsTx）
- 「并发夹具验证只允许一个冲突操作成功」✔（barrier 并发禁用/删除两用例：恰好一成一败 + 剩一个启用超管）
- 「租户互不阻塞」→ 不变量全局化后不存在按租户并行；非冲突操作（禁用普通用户）不受锁影响的用例通过（<10s 阈值，实际毫秒级）。
- 「审计和令牌吊销与结果一致」✔（service 回归 39 PASS 含 token_revocation_callsites 套件：禁用吊销/启用不吊销语义保持）

## Checks

| Check | Command | Result | Notes |
|---|---|---|---|
| 并发守卫专项 | go test ./internal/repository -run 'TestSetEnabledGuarded\|TestDeleteSafe_Concurrent\|TestUpdateGuarded' -count=1 -v | pass | green.log 4 PASS |
| 并发 race | 两并发用例 -race | pass | race.log |
| repository 回归 | -run 'SystemUser' | pass | regression-repo.log 3 PASS |
| service 回归 | TestSystemUserService\|TestAuthInit\|TestAuthorizationService\|TestSystemUser\|TestTokenRevocation | pass | regression-service.log 39 PASS / 0 FAIL |

## 边界

- 咨询锁语义仅在 PG 上成立（全仓生产与测试库均为 PG）。
- 服务层 UpdateByAdmin 的 username/email 预检仍在事务外（与本守卫无关的唯一性检查，保持既有语义）。
- 未提交/推送/部署。

## Evidence Files

fix.md / green.log / race.log / regression-repo.log / regression-service.log / source-sha256.json

## 追加边界（2026-11-21 复核）

- gorm `Select` 直排咨询锁函数取不到 boolean 返回值（驱动层 NULL）：最终形态为恒一行子查询承载 `pg_try_advisory_xact_lock` + 100 次有界自旋（纯 GORM API 过 gorm-only 门）；`UpdateGuarded` 回调收紧为只改字段（过 GORM 架构棘轮，service 层不接触事务句柄）。
- 架构棘轮（TestArchServiceNoNewGormDB）与 gorm-only 门在本形态下实测通过。
