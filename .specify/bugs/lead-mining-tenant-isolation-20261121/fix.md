# Bug Fix: 线索发掘配置缓存与去抖租户隔离

- **Slug**: lead-mining-tenant-isolation-20261121
- **Fixed**: 2026-11-21
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

线索发掘运行时配置缓存现在按租户分区、返回防污染副本，并支持租户定向失效；去抖键增加可信租户身份。SaaS 模式下没有租户归属的入站行在读取配置前拒绝。

## Changes

| File | Change | Notes |
|------|--------|-------|
| `user-server/internal/service/lead_mining.go` | modified | per-tenant TTL cache、并发失效代际保护、深拷贝配置、租户定向 reload、按消息行归属构建上下文及去抖键；保留既有全局 reload API |
| `user-server/internal/service/lead_mining_tenant_isolation_test.go` | added | 独立测试覆盖跨租户缓存/去抖、定向失效、返回副本保护、无租户 fail-closed |

## Tests Added or Updated

- `TestLeadMiningTenant_ConfigCacheAndDebounceAreIsolated` — A/B 同身份消息读取各自配置且相互不去抖。
- `TestLeadMiningTenant_TargetedReloadOnlyInvalidatesSelectedTenant` — 仅刷新目标租户并验证调用方变更不会污染缓存。
- `TestLeadMiningTenant_MissingTenantRejectedBeforeConfigRead` — SaaS 缺少行归属时在配置读取和 Judge 前终止。
- `TestLeadMiningTenant_DebounceKeyIncludesTenant` — 直接检查去抖键租户隔离及默认租户兼容键。

## Local Verification

- Commands run（2026-11-21，读数与日志见 test.md/gates.json）：
  - `go test ./internal/service -run 'TestLeadMiningTenant_' -count=1 -v`：4 PASS / 0 FAIL（green.log）。
  - 同专项 `-race`：4 PASS，无竞争（race.log）。
  - 既有回归 `-run 'TestLeadMining'`：6 PASS + 4 FAIL（全部为 `test password is missing` 数据库门禁，既有环境约束，非本修复引入）。
  - build / vet / lint（0 issues）/ 层边界 / 架构 / naked-goroutine / bare-go / GORM-only：全部 rc=0（gates.json）。
  - gofmt -l 无输出；改动文件 `git diff --check` rc=0。
- Manual checks: 单元与静态门禁；未连接生产库/真实渠道/LLM 上游；未提交/未推送/未部署。

## Deviations from Assessment

无。

## Follow-ups

- 数据库集成面（`TestLeadMining_Integration_*` 4 例）待有 PG 凭据窗口补验；原 FA-17 关闭判据的该子范围保持「不可判断」。
