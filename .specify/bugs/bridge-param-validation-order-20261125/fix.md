# Fix: bridge 入参/body 校验顺序

- **Slug**: bridge-param-validation-order-20261125
- **Status**: applied
- **Scope**: `user-server/internal/bridge/{handler_http.go,sse.go}` 的 unstaged 修复；对应测试迁移

## Applied Changes

1. `HandleHTTPIngest`
   - 先用 `Content-Length` 与 `maxBodyBytes+1` 有界读取拒绝超限 body（413），再解析租户；
   - 日志预览与真实 ingest 共用同一份有界 body 缓存，不恢复第二次无上限 `ReadAll`；
   - 无 identify_code 且缺 channel/account 时先返回 400；
   - 显式不支持渠道先返回 400；
   - 合法 identify_code 仍可补全省略的 channel/account；
   - 结构、身份、scope 全部通过后才调用 `deliverInbound` 与后续写入。

2. `GetBridgeOutbox` / outbox metrics
   - 无识别码的必填参数错误先返回 400，不让租户解析抢先返回 404；
   - 识别码可补全身份，显式不支持渠道仍 400；
   - 保持 E3 显式账号 fail-closed 与 registry 权威。

3. `HandleOutboxSSE`
   - 缺参且无识别码时先返回 400，不提交 SSE 流、不刷在线位；
   - 显式不支持渠道先返回 400；
   - 合法识别码仍先绑定并补全身份。

4. 旧 E2E 断言迁移
   - 携带合法识别码但省略 channel/account 的两条旧 400 断言改为 200 + `ok=true`，与 identify_code-only 契约一致；
   - 未放宽无码缺参、错误账号、禁用账号或跨租户拒绝。

## Boundary Preservation

- `handler_http.go` staged blob：`a9ca29d6cd2bcfde5bcd36f57d4c0c1c420e18c586c74a5034cdd8e9dbae554b`（未变）。
- `sse.go` staged blob：`161a8da59f0ab7aefe70ebae08e31736d334e0fe7f64fff24c9c1cc0ebc02619`（未变）。
- 外部 staged `tenant_binding.go` blob `8276275a46375d424093cfd62f0f71cb86fb759e9a5e5cd4ba98570ab16da760` 与 channelgw registry 文件未覆盖。
- 未 commit/push/deploy、未真实外发、未 UI。
