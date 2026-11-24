# Bug Fix: 域名池探测目标与健康判定统一

- **Slug**: domain-probe-target-20261121
- **Fixed**: 2026-11-22
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

域名池记录驱动的手动检查、批量检查与健康度检查现统一为记录配置端口上的 HTTP GET `/`，最终响应状态 200–399 才判为可用；请求绑定调用上下文并有限排空响应体。关键状态/检查时间、健康度、黑名单查询与健康度日志写入错误现在返回调用方，避免落库未确认仍报告成功。

## Changes

| File | Change | Notes |
|------|--------|-------|
| `user-server/internal/service/domain_pool.go` | modified | 新增共享 `probeDomainHTTP`，统一手动检查、批量检查的目标/状态口径；传播状态写入错误；批量按仓储总数翻页并聚合行级落库错误。 |
| `user-server/internal/service/domain_health.go` | modified | CheckOne/CheckAll 对每个 DomainPool.Port 做共享 GET 探测；传播黑名单/健康度/状态/日志错误，关键持久化失败前不进入自动切换；批量检查按总数翻页。窄化日志和黑名单依赖为本服务接口以便注入替身。 |
| `user-server/internal/service/domain_health_probe_test.go` | added | 本地 `httptest` 双端口/重定向/4xx/5xx/超时、落库故障、自动切换保护及手动/健康批量跨页回归。 |

## Tests Added or Updated

- `TestDomainProbe_ManualAndHealthUseConfiguredPort` — 记录手动与健康度 GET `/` 对配置端口的命中，并验证 503 状态持久化为不可用。
- `TestDomainProbe_HTTPStatusRedirectAndTimeout` — 验证最终 204 成功、400/500 失败和客户端超时失败。
- `TestDomainProbe_PersistenceErrorsPreventSuccessAndSwitch` — 覆盖状态/健康度/健康状态/日志/黑名单查询故障传播且不自动切换。
- `TestDomainProbe_CheckAllManualPaginatesAndAggregatesWriteErrors`、`TestDomainProbe_CheckAllHealthPaginates` — 验证 201 行跨越默认 200 页大小后都被检查。

## Local Verification

- Commands run:
  - `go test ./internal/service -run '^TestDomainProbe_' -count=1 -v` → PASS，5 项定向用例。
  - `go test -race ./internal/service -run '^TestDomainProbe_' -count=1` → PASS，`ok hivemtk-user/internal/service 2.069s`。
  - `POSTGRES_TEST_HOST=127.0.0.1 POSTGRES_TEST_PORT=8217 POSTGRES_TEST_PASSWORD=<从 ../.env 的 POSTGRES_PASSWORD 读取> go test ./internal/service -count=1` → PASS，最终兼容调整后 `ok hivemtk-user/internal/service 271.525s`。使用专用快测库，未连接业务库。
  - `go build ./... && go vet ./...` → PASS。
  - `python3 scripts/check-layer-boundary.py` → PASS，1633 个非测试 Go 文件、0 跨层违规。
  - `bash scripts/check-architecture.sh` → PASS，含 1 条既有 warning：`internal/service/webhook_wecomcs_puller.go` 的 `wecomCSService` 缺接口。
  - `bash scripts/check-gorm-only.sh` → PASS，业务包 0 处 `.Exec(`/`.Raw(`。
  - `bash user-server/scripts/check_naked_goroutine.sh` → FAIL，报告多个仓内既有裸 goroutine 文件（含 cmd、middleware、其他 service 等）；不属于本次变更，未扩大范围修改。
  - `git diff --check -- ...` → PASS（域名探测三文件无空白错误）。
- Manual checks: 未访问公网、生产服务、真实渠道或外发消息；本地监听服务和仓储替身全程合成。

## Deviations from Assessment

- 实际代码中批量手动检查原已将 HTTP >=400 判失败，评估稿中“批量手动路径复用任意响应成功”不准确；实现保留其已有状态语义，仅统一到 200–399 的严格区间。
- 公开 `ProbeOnce(ctx, domain)` 不接收 DomainPool 配置，恢复其既有 DNS + 80/443 HEAD 兼容行为；仅 `CheckOne`/`CheckAll` 这类拥有 DomainPool.Port 的健康路径改为统一配置端口 GET。避免改动通用探测器的既有调用契约。
- 发现仓储 `List` 将 pageSize 上限夹到 200，而两个“全部”检查入口原来只请求一页。为满足全部域名检查语义，在本任务范围内按 `total` 翻页，并新增 201 行测试。

## Follow-ups

- 本地修复与门禁已验证；尚未提交、推送或通过链 A 部署，按原清单留作整体交付验收，未声称生产交付完成。
- 真人/线上发布后仍按域名池现有生产观察与回滚程序验收；本轮不做生产探测。
