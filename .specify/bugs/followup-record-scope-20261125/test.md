# Bug Verification: 跟进记录范围

- **Result**: scoped GREEN（真实 PG）
- `go test ./internal/service -run 'TestFollowupScope_' -count=1 -v`：4/4 PASS（约 1.6s）。
- 覆盖：列表按客户范围 + 总数不泄露、创建关联未授权客户拒绝、改挂未授权客户拒绝且旧记录不变、合法创建成功。
- 组合限定回归（含前序 CRM 任务）：全绿。
- `go build ./...` rc=0；gofmt clean。
- 未做 UI 点验/真机/生产/外发/部署/提交。
