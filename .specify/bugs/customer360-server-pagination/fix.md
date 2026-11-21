# Bug Fix: 客户360服务端唯一客户分页与搜索

- **Slug**: customer360-server-pagination
- **Fixed**: 2026-11-20
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

客户列表先在既有租户、坐席权限范围中按唯一 user_id 搜索、计数和分页，再回查当前页客户的全部可见会话。前端改用服务端页码与 total，不再二次过滤或切片。

## Changes

| File | Change | Notes |
|------|--------|-------|
| `user-server/internal/controller/customer_360.go` | 接收 trim 后的 keyword | 保留 list/total/page/page_size 兼容信封 |
| `user-server/internal/repository/customer_session.go` | 新增 ListCustomerPage、共用范围与搜索构造 | 参数化 ILIKE 三字段，字面转义反斜杠/%/_；distinct user_id 稳定排序分页；内外查询都有租户和坐席范围，保留软删除过滤；空/越界页返回真实 total |
| `user-server/internal/service/customer_360.go` | 客户分页替换会话分页 | 普通查询数据库分页；画像筛选分批扫描完整候选，先匹配再计数分页；空页不调用其他聚合仓储 |
| `user-server/internal/repository/customer_session_test.go` | 新增无库 SQL 契约与8217集成 | 原有测试保留 |
| `user-server/internal/service/customer_360_test.go` | 新增纯分页单元与服务聚合集成 | 原有测试保留 |
| `user-server/internal/controller/customer_360_test.go` | 新增 HTTP keyword/分页信封契约 | 使用 httptest，不触碰运行中8401 |
| `user-web/src/views/customer360/List.vue` | 请求 page/page_size/keyword，使用服务端 total | 搜索回首页、清空重查、迟到请求/错误守卫、卸载失效；保留已有详情/订单竞态及深链改动 |
| `user-web/tests/unit/customer360_search_isolation.test.js` | 更新为后端 map DTO 模拟，10项行为断言 | 未放宽断言，无 skip/only 或延长超时 |

## Local Verification

已执行并通过：

- `npm --prefix user-web run test -- tests/unit/customer360_search_isolation.test.js`：10/10，1文件通过；全量运行到该文件时再次10/10通过。
- `npm --prefix user-web exec -- eslint <user-web/src> --max-warnings 80`：exit 0。
- 无库 Go 契约命令的 repository 部分：`TestCustomerListQueryContract`、`TestCustomerListMissingTenant`，包通过（2.289s）。同命令的 service 部分仍在编译，最终状态见 test.md。
- 仅绑定127.0.0.1:8217的 `go test ./internal/repository -run '^TestCustomerListPageIntegration$' -count=1 -v`：PASS，测试5.58s，包7.449s。无跳过；独立测试库由既有 testutil 创建与清理，未使用业务库。
- `python -I -X utf8 scripts/check-layer-boundary.py`：扫描1604文件，0跨层违规。
- `bash user-server/scripts/check-gorm-only.sh`：业务包0处违规。
- 本轮六个Go文件 `gofmt -l` 无输出；`git diff --check` 无输出。

其他检查状态：

- 全量 `npm run test` 在其他模块出现超时（agent_mode_persistence 4、agent_save_recovery 1、agent_kb_read_protection 2、inbox_tag_binding 1），后按主代理要求停止重型全量，未完成，不能记为全绿。未改相关文件或放宽测试。
- 全量 `go build ./...`、`go vet ./...` 因资源竞争已停止，后续按必要范围串行验证，最终结果见 test.md。
- `check-architecture.sh` 检查到第4组时按主代理要求停止；已完成的controller/service/repository检查通过，不宣称全量通过。
- `check_naked_goroutine.sh` 因仓库其他既有cmd/service文件命中失败，本轮客户360文件没有新增裸协程；不越界修改。
- 首次架构Python脚本在GBK输出符号时失败；UTF-8重跑通过，没有改共享配置。

## Deviations from Assessment

无实现范围偏离。验证执行根据主代理资源协调要求停止重复重型全量，保留真实输出，并优先跑8217仓储契约。

## Boundaries and Remaining Verification

未提交、推送、部署、修改共享配置或真实数据，未重启PG8202/Redis8203/API8401。8401运行二进制未更新，新增后端实现尚未在运行中真实HTTP验证；浏览器操作留给主代理。`scripts/api_verify_full.py` 包含业务写入全链，不符合本轮禁止改真实数据的范围，未执行。画像筛选为了准确 total 会扫描完整候选集，普通keyword查询保持数据库分页。
