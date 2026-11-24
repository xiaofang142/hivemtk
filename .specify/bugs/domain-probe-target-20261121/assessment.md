# Bug Assessment: 域名池检查与健康探测目标不一致

- **Slug**: domain-probe-target-20261121
- **Created**: 2026-11-21
- **Source**: `docs/未完成任务清单.md` FA-22
- **Verdict**: valid
- **Severity**: medium

## Report (verbatim or summarized)

FA-22：域名手动检查与后台健康探测统一目标和成功口径。现有代码中，手动检查把任意 HTTP 响应都当成健康，后台探测未使用配置端口；后台结果可能与实际配置服务不符并导致错误自动切换。关闭判据要求本地双端口计数器覆盖成功/失败/超时和状态落库失败，不用公网探测复现。

## Symptom

同一条域名池记录的手动检查与健康度检查没有探测相同的协议/端口/路径或采用相同的成功标准。手动检查把 4xx/5xx 也写成可用，健康度检查则固定尝试域名根路径的 80/443，未访问该记录的配置端口，因此状态、评分和自动切换可能基于错误的服务结果。

## Reproduction

1. 查看 `domainPoolService.CheckDomain`：它拼接 `http://<domain>:<configured-port>` 并执行 GET，但只要请求没有网络错误，就写 status=1 并返回 true，不检查响应状态码。
2. 查看 `domainHealthService.checkAndPersist` 与 `ProbeOnce`：调用只传 domain；HTTP HEAD 探测拼接 `http://<domain>/` 和 `https://<domain>/`，没有使用 `DomainPool.Port`，仅把端口附加到结果。
3. 查看两个路径的状态写入：`CheckDomain` 忽略 `UpdateStatus`/`UpdateLastCheck` 错误；健康度路径也仅记录部分持久化错误，调用方仍收到结果。

以上由当前源码直接确认；此阶段未发起公网或生产请求。改后将以两个本地监听端口分别计数，验证实际请求只到配置端口及统一状态判定。

## Suspected Code Paths

- `user-server/internal/service/domain_pool.go:145` — 手动 GET 按记录端口构造 URL，但任意 HTTP 响应均判成功且忽略状态持久化错误。
- `user-server/internal/service/domain_pool.go:175` — 批量手动检查复用相同的判定方式。
- `user-server/internal/service/domain_health.go:127` — 健康度检查从记录取域名后没有把配置端口传入探测。
- `user-server/internal/service/domain_health.go:247` — `ProbeOnce` 固定拼接 80/443 上的根路径 HTTP/HTTPS HEAD。
- `user-server/internal/repository/domain_pool.go:12` — 仓储接口已有状态/检查时间/健康度更新方法；不需要增加数据库模型或迁移。

## Root Cause Hypothesis

两个历史检查实现分别定义了自己的目标与成功规则，手动路径未检查 HTTP 状态码，健康度路径也没有消费 DomainPool.Port。两个实现都吞掉了状态/检查时间的持久化错误。置信度：high。

## Proposed Remediation

**Preferred**: 保持现有模型与外部 API 形状不变，以 DomainPool.Port 为唯一端口来源，统一使用 HTTP、根路径 `/` 和 GET 探测；沿用 Go HTTP 客户端默认有限重定向策略，最终 HTTP 状态在 200–399 判为可用，其他状态、请求失败或超时均判为不可用。健康度探测传入同一配置端口并使用相同请求/状态规则；保留已有 DNS/黑名单/评分职责。状态、最后检查时间及健康度关键写入失败应显式向调用方返回错误，避免返回“检查完成”但落库未成功。

不增加 scheme/path 字段或迁移：现有手动 API 已将 HTTP + 配置端口作为可执行目标，健康度探测应对齐该既有配置契约。HTTPS 专用探测如需支持，应作为独立模型/需求变更，不在此修复中推断。

**Files likely to change**:
- `user-server/internal/service/domain_pool.go`
- `user-server/internal/service/domain_health.go`
- `user-server/internal/service/domain_pool_test.go`
- `user-server/internal/service/domain_health_probe_test.go` (new)

**Tests to add or update**:
- 本地双 HTTP 端口计数夹具验证手动与健康度路径均访问配置端口而非默认端口。
- HTTP 200/3xx 与 4xx/5xx/超时矩阵验证两路径判定一致；不进行外网请求。
- Repository 替身注入状态/检查时间/健康度写失败，验证服务不报告成功并返回可观测错误。
- 批量检查覆盖行级失败报告/错误传播且无数据竞争。

## Risks & Considerations

- 外部探测目标收敛为 HTTP + 已配置端口 + `/` + GET；不再隐式尝试 HTTPS 443。模型没有 scheme/path 字段，贸然增加将扩大范围并涉及数据迁移。
- GET 可能读取响应体；实现应关闭响应并限制排空字节，避免不必要资源消耗。
- HTTP 默认重定向可能改变最终响应目标；本修复沿用客户端当前重定向行为并统一判定，不引入跨主机重定向策略变更。
- 自动切换仍依赖既有健康度评分阈值；只改变输入探测结果的一致性，不调整业务阈值。

## Open Questions

- 无。按当前记录模型和手动检查既有协议收敛，不新增线上配置要求。
