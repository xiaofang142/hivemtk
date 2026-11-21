# Bug Fix: 最后一名启用超管的原子守卫（FA-AUTH-01）

- **Slug**: last-admin-atomic-guard-20261121
- **Fixed**: 2026-11-21
- **Status**: applied

## Summary

禁用/删除/降级三条路径此前都是"先计数后写"：并发下两名启用超管可互相禁用/删除，两边都读到 count=2 而都放行 ⇒ 系统零启用超管。现把守卫与写入收进同一事务 + `pg_advisory_xact_lock`（全局咨询锁键，与既有全局不变量口径一致）：锁内读目标现值 → 判定是否移除启用超管 → 锁内复查"其他启用超管数"不足即拒绝。SetEnabledGuarded（禁用）、DeleteSafe（删除）、UpdateGuarded（降级角色/状态禁用整行更新）三入口共用同一把锁与同一判定。

## Changes

| File | Change |
|---|---|
| `internal/repository/system_user.go` | 接口增 SetEnabledGuarded/UpdateGuarded；lastAdminAdvisoryLockKey + 事务内锁/计数助手；DeleteSafe 重构为事务守卫版 |
| `internal/service/permission.go` | SetEnabled 删除先数后写，改调 SetEnabledGuarded（ErrLastAdmin 友好映射） |
| `internal/service/system_user.go` | UpdateByAdmin 保存改走 UpdateGuarded（降级/禁用原子复查） |
| `internal/repository/system_user_last_admin_guard_test.go` | 新增 4 用例：并发禁用/并发删除恰好一成一败、降级最后超管拒绝、非冲突操作不受阻 |
| `internal/service/token_revocation_callsites_test.go` | 假仓储补 SetEnabledGuarded 同语义应答 |

## Local Verification（2026-11-21 复核更新）

- 锁形态修正：gorm `Select` 直排函数链路取不到 boolean 返回值（驱动层 NULL ⇒ unsupported data type），改为恒一行子查询承载 `pg_try_advisory_xact_lock`（纯 GORM API，过 gorm-only 门）+ 100 次有界自旋实现阻塞语义；`UpdateGuarded` 回调收紧为只改字段（不暴露事务句柄给 service 层，过 GORM 架构棘轮）。
- 专项 4 用例通过（green.log）；并发两用例 `-race` 通过（race.log）；service 鉴权/令牌吊销回归 40 PASS（regression-service.log）。
- 回归：repository SystemUser 3 PASS；service 系统用户/鉴权/令牌吊销 39 PASS / 0 FAIL。
- 全仓 build 通过。真实 PG（咨询锁语义），凭据由容器环境读入。
