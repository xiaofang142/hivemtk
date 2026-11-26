# Test: Gateway 可信身份与消费回执

- Date: 2026-11-26
- Result: partial；证明本机适配器边界，不关闭关联业务任务。
- 环境：实际 `HandleGateway` + Gin/httptest；现有 mockHandle 替代 InboxIngress，可信身份测试由请求上下文/中间件上下文 fixture 提供。未调用真实数据库、模型、网络、设备或消息接口。

## RED 与因果论证

1. 07:39:50，`TestGatewayMessagePinsTrustedIdentity`：原 builder 得到 `account=other-account tenant=999`，以及数值账号 `99`；可信参数是 `trusted-account/7`。复制顺序能直接改写后续 Hub 使用的 `Extra.account_id`。
2. 07:41:03，`TestGatewayReportAcknowledgesConsumerResult`：已接通现有 mock seam 后，discarded 和 missing 两条仍收到 accepted；内部错误明文也出现在响应。测试 seam 尚未接通时的 500 不计作该缺陷证据。消费服务真实 discard 分支为 `Accepted=false, nil`，且发生在消息保存之前。
3. 07:43:23，`TestGatewayReportUsesResolvedIdentity`：已解析 wechat 的请求仍按 qq 消费；账号、租户冲突和缺关键字段的身份仍返回 200/一次消费，预期为 404/零消费。两个正常身份正例保留。

## GREEN

修复后的三组专项：

```sh
go test ./internal/bridge -run '^TestGateway(MessagePinsTrustedIdentity|ReportAcknowledgesConsumerResult|ReportUsesResolvedIdentity)$' -count=1
```

末行：`ok hivemtk-user/internal/bridge 1.117s`。

- 元数据三例：缺失、伪字符串、伪类型；可信值一致、诊断字段保留、原 map 不变。
- 同一 HTTP 批次五种消费结果：accepted、明确未接受、无结果、错误、accepted 同时带错误；逐条状态与固定 code 正确，错误细节不外泄。
- 身份七例：canonical 渠道覆盖提示、空提示补齐、账号冲突、租户冲突及三种缺关键字段；拒绝路径零消费。
- 关联 HTTP 超限回归继续验证整批校验、截断后统一消费，以及冲突尾部零调用。
- 最终离线 race 三轮通过：三组 Gateway 加 HTTP limit 共每轮 13 个叶子用例，`ok hivemtk-user/internal/bridge 1.759s`；每批五种回执结果逐条核对，不将它们重复计为五个独立测试。命令为 `go test -p 1 ./internal/bridge -run '^(TestGateway(MessagePinsTrustedIdentity|ReportAcknowledgesConsumerResult|ReportUsesResolvedIdentity)|TestHTTPIngestLimitAppliesBeforeAllConsumers)$' -race -count=3`，使用下述离线工具链。

## 构建与门禁

- 本轮 `go build -p 1 ./...` 通过，使用本地缓存 Go 1.26.11，`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`，不下载工具链或依赖。
- `bash scripts/check-architecture.sh` 十组通过，781 个生产 service 文件；两项既有命名警告保留。`python3 scripts/check-layer-boundary.py` 本次扫描 1786 个非测试 Go 文件，新增跨层违规 0；GORM-only 通过。
- `git diff --check` 通过，两个本轮 Go 文件已 gofmt。
- 首次全仓 vet 遇到并行 `customer_360_crm_scope_test.go` 引用尚不存在的 `Customer360Controller.permission`；首次 race 编译遇到并行 repository 引用尚不存在的 `CRMEntityAccountFilter/applyCRMEntityAccounts`。两次失败发生在测试执行前，不作为本轮回归通过或产品缺陷证据。只读确认对方后续已补齐定义后重新验证，未修改其文件。
- 上述定义补齐后的全仓 `go vet -p 1 ./...` 已 exit 0、无输出；race 重新执行已通过，见上文。其后新增 guard 交接修复的最终检查另在相应 bug 记录回收。
- Graphify 两仓刷新均因 `ModuleNotFoundError: No module named 'graphify'` 未完成，未安装或改动环境依赖。

## 验证边界

SageX 契约 §5 和实际 AckValidator/UploadStore 证明 accepted/duplicate 会停止重传并清除上报 payload，rejected 为 blocked 且不算确认。本组测试没有运行 Android 构建，也没有把 fake 接收等同于数据库已提交。真实数据库持久化、其他入口身份接线、原生数组与全链路幂等仍按原编号验收。
