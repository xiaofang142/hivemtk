# Bug Verification: 域名池探测目标与健康判定统一

- **Slug**: domain-probe-target-20261121
- **Tested**: 2026-11-22
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: verified

## Summary

合成本地双端口复现验证：手动检查和健康度检查分别仅命中其 DomainPool 配置端口，均使用 GET `/` 和统一 200–399 状态规则；错误状态和超时按不可用处理。关键持久化/query 错误可见且阻止自动切换。完整 service 包、race 定向、build/vet、分层与 GORM 门通过。裸 goroutine 静态门仍报仓内其他既有文件；未连接公网/生产。

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| Reproduction (post-fix) | 本地 `httptest` 双端口及请求方法/路径/命中计数 | pass | 手动 503 与健康 200 分别命中各自配置端口；没有公网请求。 |
| New / updated tests | `go test ./internal/service -run '^TestDomainProbe_' -count=1 -v` | pass | 5 项 PASS，覆盖双端口、重定向、400/500、超时、落库失败与分页。 |
| Race check | `go test -race ./internal/service -run '^TestDomainProbe_' -count=1` | pass | `ok hivemtk-user/internal/service 2.069s`。 |
| Regression suite | 使用专用 PostgreSQL 快测库运行 `go test ./internal/service -count=1` | pass | 最终源码复跑 `ok hivemtk-user/internal/service 271.525s`；测试库 host/port 钉在 127.0.0.1:8217。 |
| Build / vet | `go build ./... && go vet ./...` | pass | 两命令成功退出。 |
| Layer boundary | `python3 scripts/check-layer-boundary.py` | pass | 1633 文件、0 违规。 |
| Architecture | `bash scripts/check-architecture.sh` | pass with warning | 架构门通过；1 条既有 `wecomCSService` interface warning。 |
| GORM-only | `bash scripts/check-gorm-only.sh` | pass | 业务包 0 处 `.Exec(`/`.Raw(`。 |
| Naked goroutine | `bash user-server/scripts/check_naked_goroutine.sh` | fail | 命中多个非本任务既有源码文件；未修改这些文件。 |
| Whitespace | `git diff --check -- internal/service/domain_pool.go internal/service/domain_health.go internal/service/domain_health_probe_test.go` | pass | 无空白错误。 |

## Output Excerpts

```text
=== RUN   TestDomainProbe_ManualAndHealthUseConfiguredPort
--- PASS
=== RUN   TestDomainProbe_HTTPStatusRedirectAndTimeout
--- PASS
=== RUN   TestDomainProbe_PersistenceErrorsPreventSuccessAndSwitch
--- PASS
=== RUN   TestDomainProbe_CheckAllManualPaginatesAndAggregatesWriteErrors
--- PASS
=== RUN   TestDomainProbe_CheckAllHealthPaginates
--- PASS
ok hivemtk-user/internal/service 271.525s
```

## Residual Risks

- 尚未提交、双远端推送、链 A 发布；依照仓库规则 0/1，单项本地验收不代表完整工作树可安全提交。内层仓当前有大量并行在途修改，不能将它们混入提交。
- 生产域名、真实 DNS/HTTP 服务与发布后的观察未验；本轮仅验证本地合成流量。
- 裸 goroutine 门的既有失败需要其责任范围单独收敛，不能归因于 FA-22。
- `ProbeOnce(domain)` 是不持有 DomainPool 配置的通用接口，仍保留原 DNS + HTTP/HTTPS 默认端口 HEAD 探测；DomainPool 记录的 CheckOne/CheckAll 已按配置端口统一。

## Recommendation

关闭 FA-22 的本地代码修复与测试判据；任务清单继续标记“本地修复已验/整体交付待验”，待工作区归属核验、整体门禁与获准链 A 交付后再最终销项。不要以本地测试宣称生产服务已验证。
