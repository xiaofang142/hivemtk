# Bug Verification: 账号授权配对

- **Result**: scoped GREEN（纯函数 + 组合限定回归）
- `go test ./internal/service -run 'TestAccountRulePairing_' -count=1 -v`：3/3 PASS，覆盖交叉账号行业拒绝、owner/viewer 混合不互扩、管理员不受影响。
- 组合限定回归（含 CRM-02/03/05/25 全部新增用例与既有 CRM 权限用例）：全绿。
- `go build ./...` rc=0；gofmt clean。
- 未做 UI/真机/生产/外发/部署/提交。
