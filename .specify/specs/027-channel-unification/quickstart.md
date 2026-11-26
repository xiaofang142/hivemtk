# Quickstart: 027-channel-unification

前置：PostgreSQL 测试库与 `POSTGRES_TEST_*` 环境变量（见 CLAUDE.md §3.5）。

1. 后端编译与静态门：
   `cd user-server && go build ./... && go vet ./... && python3 ../scripts/check-layer-boundary.py`
2. 渠道域单测（含新增 catalog / toggle / managed）：
   `go test ./internal/service/... ./internal/controller/... ./internal/router/...`
3. 迁移验证：对测试库执行 `psql -v ON_ERROR_STOP=1 -f migrations/100_channel_code_unify.sql` 两次，确认第二次零变更、规范码存在、旧码归零。
4. 前端：`cd user-web && npm run lint`；逐页静态 grep 确认无自由输入渠道/账号、无本地渠道数组。
5. UI 验证须用 ego-browser 逐元素显式操作（遵守全局规则）。
