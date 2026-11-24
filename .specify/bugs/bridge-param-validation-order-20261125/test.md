# Test: bridge 入参/body 校验顺序

- **Slug**: bridge-param-validation-order-20261125
- **Status**: verified (scoped; main independent verification pending)
- **Verdict**: GREEN

## RED Evidence

- `check-e4-param-body-red.log`：缺 account 返回 404、SSE 缺参 404、ingest 超限 404；
- `check-e4-body-bounded-red.log`：未知长度/配置上限/预览无界读取失败；
- 失败原因均与 assessment 的校验顺序或有界读取缺口一致，不是编译/环境错误。

## GREEN Evidence

- `check-e4-param-body-green.log`：outbox/metrics/SSE/body-limit/静态无界读取闸全部 PASS；
- `check-e4-body-valid-probe-green.log`：新增动态合法大小探针 PASS，122-byte 未知长度 body 只读一次，预览缓存原样进入真实 ingest，`calls=1`，无越界；
- `check-e4-e2e-identify-resolution.log`：识别码补全 channel/account 的迁移断言 PASS；
- `check-e4-bridge-full-lane2-final.log`：`go test ./internal/bridge -count=1` rc=0，8.364s；
- 早先 `check-e4-bridge-full-final.log` 亦 rc=0，7.709s。

## Covered Contracts

- 无码缺 channel/account：400；
- 显式不支持渠道：400；
- 缺参拒绝不提交 SSE、不产生在线/写入副作用；
- 已知/未知长度/配置上限 body：413，先于身份解析；
- 有合法识别码：可补全省略身份，合法 body 正常 ingest；
- E3 显式账号、禁用账号、跨租户 fail-closed 回归保持。

## Remaining Boundary

四道静态门与 full channelgw/service 将在 E4 收口时统一重跑；本报告不宣称 E 整体完成。
