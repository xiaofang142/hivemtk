# Bug Assessment: 客户360服务端唯一客户分页与搜索

- **Slug**: customer360-server-pagination
- **Created**: 2026-11-20
- **Source**: 用户明确指令及当前工作区代码
- **Verdict**: valid
- **Severity**: high
- **Reproduction status**: partial（静态确认；未操作浏览器或运行中业务数据）

## Report

客户360列表控制器未接收 keyword，服务先分页会话再聚合 user_id，total 是会话数。前端本地搜索及分页只覆盖初始20条会话，遗漏其他客户。

## Symptom and Reproduction

1. 同一客户有超过20个会话，另有其他客户。
2. 旧列表第一页聚合不足20个客户，total 却仍是会话数。
3. 输入初始会话页以外客户的名字、手机或邮箱，本地过滤无法命中。

上述路径已由源码确认；真实环境复现未执行，专用测试库8217是否可用待检查。

## Root Cause Hypothesis

高置信度。controller/customer_360.go GetCustomerList 只采集三个画像筛选字段；service/customer_360.go 通过 GetByMerchant/GetByMerchantForAgent 对会话分页，聚合后再筛选，并把带筛选 total 改成本页数量。List.vue 同时使用本地 filteredCustomers、slice 和本地长度。

## Proposed Remediation

Repository 内先对租户及行级权限范围中的非空 user_id 去重计数，名字/手机/邮箱使用参数化、不区分大小写的字面子串搜索。唯一客户按 user_id 稳定排序再分页，随后一次查询这些客户的全部可见会话；搜索不截断同一客户的聚合会话。所有子查询及外层继续使用带 ctx 的 GORM model 查询，并显式租户条件，缺少租户上下文拒绝（单租户沿用既有默认租户口径）。保留 admin 全可见、其他角色自己+未分配池的已有行为。

Service 保留现有 map DTO 信封兼容性。普通搜索直接使用仓储唯一客户分页；platform/activity_level/purchase_power 依赖完整 DTO 的既有语义须在候选客户完整聚合、匹配后才分页并计数，可分批扫描候选，不能再报告当前页数量。零结果返回空 map 与 total=0，越界页保留真实 total。

前端请求携带 page/page_size/keyword，渲染服务端当前页并使用 total，输入变化回第一页，清空搜索重新取列表；列表请求加版本守卫、失败清空及卸载清理，保留原有详情/订单竞态和挂载深链逻辑。

### Files likely to change

- `user-server/internal/controller/customer_360.go`
- `user-server/internal/service/customer_360.go`
- `user-server/internal/repository/customer_session.go`
- `user-server/internal/controller/customer_360_test.go`
- `user-server/internal/service/customer_360_test.go`
- `user-server/internal/repository/customer_session_test.go`
- `user-web/src/views/customer360/List.vue`
- `user-web/tests/unit/customer360_search_isolation.test.js`
- 本目录 `fix.md`、`test.md`

### Tests to add or update

- 无连接 SQL 契约：distinct user_id 计数、分页子查询稳定排序、keyword 三字段与通配符转义、查询参数化、租户和 agent 在内外两层生效、缺租户拒绝。
- 专用 PostgreSQL 集成：一个客户超过20会话仍占一行，跨页无重复，关键字能查初始会话页外客户，全部可见会话聚合，其他租户/坐席不泄漏，零命中及越界页总数正确。
- Controller 的 keyword 透传及分页响应契约；Service 的 DTO 筛选先过滤再计数分页。
- 前端以实际后端 map DTO 模拟分页，断言请求参数及 DOM：翻页、搜索回首页、清空、服务器总数、迟到列表与错误、深链、详情和订单竞态。

## Risks & Considerations

- 不增加依赖、迁移、共享配置，不启动/重启服务，不触碰真实数据、提交、推送或部署。
- DTO 画像筛选需扫描完整候选集以保证准确，普通列表及 keyword 搜索仍在数据库分页；报告该成本。
- 本轮保留 List.vue 既有未提交修改，其他会话文件均不修改。
- 先纯单元/编译，DB 集成只允许专用8217，缺环境如实记为 not-run/blocked。

## Open Questions

无需要用户决策的问题。浏览器和运行中 API 点验由主代理继续负责。
