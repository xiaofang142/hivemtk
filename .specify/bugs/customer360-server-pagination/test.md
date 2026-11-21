# Bug Verification: 客户360服务端唯一客户分页与搜索

- **Slug**: customer360-server-pagination
- **Tested**: 2026-11-20
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: partial

## Summary

代码修复已落地，前端10项定向测试、后端3项无库契约、8217真实PostgreSQL仓储/服务/控制器契约全部通过，无跳过；三个修改包build/vet通过。整体仍记 partial：8401运行二进制未更新，未进行真实运行HTTP或浏览器点验，且全量回归按资源协调要求中止。

## Checks Performed

以下命令从仓库根理解路径，Go命令在 `user-server` 中执行，前端命令在 `user-web` 中执行。测试凭据经环境变量注入，不记录其值。数据库集成显式固定 `POSTGRES_TEST_HOST=127.0.0.1`、`POSTGRES_TEST_PORT=8217`、`POSTGRES_TEST_DBNAME=user_db_test`，清除其他DSN覆盖。

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| 前端行为专项 | `npm run test -- tests/unit/customer360_search_isolation.test.js` | pass | 1文件10项通过，原详情/订单回归断言保留并新增深链/列表竞态断言 |
| 无库SQL/分页单元 | `go test ./internal/repository ./internal/service -run '^(TestCustomerListQueryContract\|TestCustomerListMissingTenant\|TestCustomerListFilteredPagination)$' -count=1` | pass | repository 2.289s；service 1.432s，无跳过 |
| 真实PG仓储 | `go test ./internal/repository -run '^TestCustomerListPageIntegration$' -count=1 -v` | pass | 专用8217，测试5.58s，包7.449s |
| 真实PG服务/HTTP | `go test ./internal/service ./internal/controller -p 1 -run '^(TestCustomerListServiceIntegration\|TestCustomerListControllerIntegration)$' -count=1 -v` | pass | service 2.633s；controller 2.644s，7个HTTP子用例全部通过 |
| 修改包编译 | `go build -p 1 ./internal/repository ./internal/service ./internal/controller` | pass | 串行命令exit 0；测试命令亦已编译全部3包及测试源码 |
| 修改包静态分析 | `go vet -p 1 ./internal/repository ./internal/service ./internal/controller` | pass | exit 0，无输出 |
| 前端源码lint | `npm exec -- eslint <user-web/src> --max-warnings 80` | pass | exit 0 |
| 五层边界 | `python -I -X utf8 scripts/check-layer-boundary.py` | pass | 1604文件，0违规；首次GBK输出错误经UTF8重跑消除 |
| GORM门 | `bash user-server/scripts/check-gorm-only.sh` | pass | 0业务包Raw/Exec违规 |
| 格式/差异 | 六个Go文件`gofmt -l`；`git diff --check` | pass | 均无输出 |
| 前端全量 | `npm run test` | stopped / inconclusive | 出现其他模块超时后按主代理要求停止，客户360在该轮再次10/10通过；没有全量汇总 |
| 全后端build/vet | `go build ./...`、`go vet ./...` | stopped | 资源竞争下停止重复重型任务，改为上述3包串行验证 |
| 架构10组shell门 | `bash scripts/check-architecture.sh` | stopped | 已完成controller/service/repository各项通过，第4组运行时协调停止 |
| 裸协程全仓门 | `bash user-server/scripts/check_naked_goroutine.sh` | fail, unrelated | 命中其他既有cmd/service文件，本轮客户360文件没有新增裸协程 |
| 运行中8401HTTP与浏览器 | 未操作 | not-run | 用户禁止重启；8401仍为旧二进制，浏览器由主代理负责 |
| 全链验收脚本 | `scripts/api_verify_full.py` | not-run | 包含业务写入链，不符合本轮禁止改真实数据范围 |

## Contract Evidence

- 同租户坐席可见22个唯一客户，其中一位有25会话：第一页20个客户、44条会话；第二页2个客户、3条会话，无跨页重复，total始终22。
- 回查仍受坐席与租户条件限制：同user_id下他人会话、他租户会话不会进入聚合；隐藏会话的关键词不能旁路命中客户。
- 管理员仍限制当前租户，可见23个客户49会话。
- 名字、手机和邮箱服务端搜索通过；邮箱不区分大小写，空格trim；`%`、`_`、反斜杠按字面搜索，SQL参数化。
- 服务集成中的3个客户各25会话：keyword只命中最后一条会话时，返回仍聚合25会话；画像筛选先匹配再分页，前三页各1客户，第4页空且total=3。
- Controller的7个httptest场景覆盖两页、姓名/手机/邮箱、零匹配、搜索越界页，信封list/total/page/page_size准确。
- 前端以真实map DTO形态模拟：传page/page_size/keyword，使用服务器total及当前页数据，无二次过滤/切片；搜索回首页、清空重查、迟到结果及失败隔离均通过；原详情/订单竞态与深链通过。

## Output Excerpts

```text
Test Files  1 passed (1)
Tests       10 passed (10)
ok hivemtk-user/internal/repository 2.289s
ok hivemtk-user/internal/service 1.432s
--- PASS: TestCustomerListPageIntegration (5.58s)
ok hivemtk-user/internal/repository 7.449s
--- PASS: TestCustomerListServiceIntegration (1.94s)
ok hivemtk-user/internal/service 2.633s
--- PASS: TestCustomerListControllerIntegration (1.78s)
ok hivemtk-user/internal/controller 2.644s
```

全量前端中观察到的其他文件超时：agent_mode_persistence（4项）、agent_save_recovery（1项）、agent_kb_read_protection（2项）、inbox_tag_binding（1项）。未放大超时、删断言、加skip或修改这些文件；主代理已有agent_mode独立6/6证据，本轮不重复追绿。

## Residual Risks and Recommendation

运行中8401尚未装载新实现，不能据此声称真实HTTP上线验证完成；后续由主代理在授权更新运行服务后继续点验。普通keyword走数据库唯一客户分页；兼容的platform/activity_level/purchase_power画像筛选为保证准确total需分批扫描完整候选集，成本随候选客户数增长。

报告、源码及针对性测试均保留在工作区。未提交、推送、部署，未修改共享配置或真实业务数据，未重启8202/8203/8401。专项自动化证据已齐，交回主代理继续全页审计。
