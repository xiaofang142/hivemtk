# Bug Verification: 公司数据范围

- **Slug**: crm-company-relation-scope-20261125
- **Result**: scoped GREEN（真实 PG + httptest）

## RED

新增 scoped 方法前：`unknown field permission` / `ListScoped undefined` 等编译失败（契约不存在）。

## GREEN

- `go test ./internal/service -run 'TestCompanyScope_' -count=1`：6/6 PASS，约 1.6s。
- `go test ./internal/controller -run 'TestCompanyController_RelationScope' -count=1`：1/1 PASS，约 0.35s；空授权读/写均 403，响应体无私有客户内容。

覆盖：同租户不同员工隔离、未授权公司详情拒绝、未授权客户关联拒绝、未授权公司关联拒绝、合法关联写入、关系列表范围、Controller 403 映射。

## 门禁

- `go build ./...` rc=0；`go vet ./internal/service ./internal/controller` rc=0。
- `python3 scripts/check-layer-boundary.py`：新增跨层违规 0。
- `bash user-server/scripts/check-gorm-only.sh`：业务包 `.Exec/.Raw` 0。
- `bash scripts/check-architecture.sh`：10/10 通过。

## Boundary

仅本机合成数据 + 真实 PG + httptest；未做 UI 逐元素点验、真机/生产/真实外发、部署/提交。
