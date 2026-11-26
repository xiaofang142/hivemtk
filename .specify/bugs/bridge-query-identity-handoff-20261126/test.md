# Test: query 识别码在 guard 后的交接

- Date: 2026-11-26
- Result: partial；真实 Gin middleware + 下游请求观察，候选源为纯 fake，无 DB/HTTP 网络/模型/设备。

## RED

08:00:28+08，先执行实际中间件测试，尚未增加 header 交接：

```sh
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off /Users/xiaofang/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.11.darwin-arm64/bin/go test -p 1 ./internal/middleware -run '^TestBridgeIngressGuard(PreservesVerifiedIdentity|NoCandidatesKeepsExplicitPolicy)$' -count=1
```

exit 1，`FAIL hivemtk-user/internal/middleware 1.215s`。仅 query_only 子例报 `verified identity missing from downstream request header`；URL 已无识别码，直接证明验证后凭据丢失。其余 header-only、header 优先、错误 header 不回退 query、无效/缺失凭据及无候选 on/off 兼容断言通过。早先 URL 断言经 Gin Query 缓存产生的噪声已改为读取真实 URL，不将缓存结果计作产品缺陷。

## GREEN 与最终检查

修复并 gofmt 后执行：

```sh
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off /Users/xiaofang/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.11.darwin-arm64/bin/go test -p 1 ./internal/middleware -run '^TestBridgeIngressGuard(PreservesVerifiedIdentity|NoCandidatesKeepsExplicitPolicy)$' -race -count=3
```

exit 0，`ok hivemtk-user/internal/middleware 2.396s`。6 个凭据形状及 2 个无候选策略连续三轮，共 24 个叶子用例通过。

- 最后一次生产代码修改后，以同一本地 Go 工具链及离线环境执行 `go build -p 1 ./...`、`go vet -p 1 ./...`，均 exit 0。
- 仓库根目录 `bash scripts/check-architecture.sh` 十组通过；两项既有 interface 命名 warning 保留。分层检查扫描 1787 个非测试 Go 文件，违规及新增违规均为 0；GORM-only 检查通过。
- 本轮文件 gofmt 无差异，四仓 `git diff --check` 通过。Gateway 与 HTTP limit 的定向 race 三轮另已通过，1.759s，详见对应 bug 记录。
- 全仓门禁仍有此前已记录的基线问题；两仓 Graphify 刷新均因 `ModuleNotFoundError: No module named 'graphify'` 失败，未据专项通过宣称全仓绿灯。

## 边界

路由中真实顺序为 InitGuard → BridgeIngressGuard → handler；下游识别码读取函数只从相同 header/query 取值。本测试证明这一交接契约与零下游拒绝路径，不证明真实识别码仓储、完整租户绑定、持久化或设备链路。只使用合成凭据，未记录真实密钥；未提交、推送或部署。
