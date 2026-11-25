# Bug Verification: 线索客户身份与来源账号

- **Result**: RED → GREEN，真实 PG
- RED：`TestLeadIdentity_UnknownChannelStable` 首次返回随机 UUID，两次不同（复现缺陷）。
- GREEN：`go test ./internal/service -run 'TestLeadIdentity_' -count=1 -v` 3/3 PASS。
- 覆盖：未知渠道稳定键、账号作用域不碰撞、专用渠道不受影响、真实 PG 重复命中归一、SourceAccountID 为真实来源账号、同联系人跨账号独立。
- `go build ./...` rc=0；gofmt clean。
- 未做真机上报/UI/外发/部署/提交。
