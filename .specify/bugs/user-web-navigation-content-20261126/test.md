# Bug Verification: 用户端导航、权限与页面内容

- **Slug**: user-web-navigation-content-20261126
- **Tested**: 2026-11-26
- **Assessment**: [assessment.md](assessment.md)
- **Fix**: [fix.md](fix.md)
- **Result**: pass（本轮修复、发布与下列关键页面验收；环境未执行项另列）

## Summary

导航、页面内容、探测来源与引导的专项回归、前后端全量和完整静态审计已通过，平台模块目录已同步并完成数据库对账。API 和 SPA 已发布，二进制、运行镜像、709 个前端产物及公网资源已独立核对；发布后八组切换及关键页面交互逐元素验收通过，见 [发布验收](production-verification.md)。环境未执行项不包含在通过结论中。

## Checks Performed

| Check | Command / Action | Result | Notes |
|---|---|---|---|
| 导航与页内入口 | 导航、单子目录、布局与 WorkflowNavigation 专项 | pass | 4 文件、17 项；覆盖详情归属、受限菜单和入口 |
| 页面内容与状态 | 驾驶舱、GEO 预览、大屏示例布局及相邻回归 | pass | 新增 13 项、相邻 3 项 |
| GEO 来源 | `npx vitest run tests/unit/crawler_source_contract.test.js` | pass | 6 项；中英说明、旧数据隐藏、刷新失败、受理语义 |
| GEO 后端 | `go test ./internal/geo/... -count=1` | pass | 专用 PostgreSQL 8217；迁移幂等、来源分类与租户隔离 |
| 文案回归 | 业务文案相关现有测试 | pass | 9 文件、89 项 |
| 引导回归 | 模块、页面、驱动钩子及接线测试 | pass | 6 文件、42 项 |
| 跨模块导览清理 | 模块导览、驱动钩子、页面导览及真实 Layout 路由回归 | pass | 4 文件、37 项；同模块保留、跨模块销毁、全局导览保留，独立复核已关闭发现 |
| 中断用例复核 | `npx vitest run tests/unit/agent_mode_persistence.test.js tests/unit/bulk_lead_handoff.test.js tests/unit/messageHub_request_state.test.js tests/unit/messageHub_dashboard_render.test.js --maxWorkers=1` | pass | 4 文件、39 项，49.18 秒；未放大测试超时 |
| 前端全量 | `npm test -- --maxWorkers=1` | pass | 最终 145 文件、1329 项全部通过，257.28 秒；包含导览修复，未放大测试超时 |
| 前端 lint | `npx eslint src --max-warnings 80` | pass | 退出 0、无输出；最终接线缩进已统一 |
| i18n 总门 | `python3 scripts/check-i18n-coverage.py` | pass | 9 语言、2434 调用键、无法解析 0、孤儿 0；基线未放宽 |
| 生产构建 | 最终提交下 `npm run build` | pass | 37.43 秒；buildId `1bb9ead90-1795658076649`；709 个实际产物，PWA 706 个预缓存条目 |
| 最终产物秘密门 | `bash scripts/check-secrets-artifacts.sh <主仓 .env> user-web/dist` | pass | 7 枚真值、709 个文件；私有真值、source map 和有效 sourceMappingURL 均为 0 |
| Go 构建与静态检查 | `go build ./... && go vet ./...`、Go lint | pass | 构建和 vet 退出 0，lint 0 issues |
| 分层与架构 | `python3 scripts/check-layer-boundary.py` 及五层架构检查 | pass | 1785 个非测试 Go 文件，跨层违规 0 |
| Go 全量 | 专用 PostgreSQL 8217 下 `make test-go` | pass | 认证预检与全部测试成功；195 个包全部出现在结果中，缺失 0、失败 0；前次中断不计通过 |
| 跨栈权限门 | 干净整合副本执行 `menu-route-role-audit.py --check-go user-server/internal/model/menu_capability_routes.go` | pass | 2197 条路由、212 个菜单路径、252 个前端页面；353 条能力规则逐字一致；未收口 LEAK 0、整页 BLOCK 0、UNROUTED 0，23 条页内限制与既有基线一致；42 项自检全过 |
| 全仓静态审计 | `make audit` | pass | 修复陈旧 MenuRouteAudit 豁免后，完整命令通过；覆盖率因无 profile、跨仓端口比对因对端不在相邻路径明确跳过，未计通过 |
| 秘密与默认口令门 | `scripts/pre-push-secrets.sh`、本地默认口令探针 | pass | A+B 与 containment 均通过；本地 8204 对公开 seed 口令返回 401 |
| 平台同步 | 管理员会话 `POST /platform/module-catalog/seed` 后只读查库 | pass | HTTP 200；24 模块、250 绑定、53 依赖；目录价格和 589 条授权逐行不变 |
| Graphify | 规定的 `_rebuild_code(Path('.'))` 命令 | not-run | 实际尝试报 `ModuleNotFoundError: No module named 'graphify'`，图谱未刷新 |
| API / SPA 发布 | 链 A / 链 D | pass | 双远端发布提交一致；API 内外 HTTP 200、默认口令 401，运行进程摘要一致；SPA 709 个远端文件与 12 个公网资源摘要匹配，回滚点已留存 |
| 线上 GEO 迁移 | 生产库只读核验列、索引及来源分布 | pass | source 非空默认 unknown；复合索引存在；1,150,460 条历史记录保留 unknown |
| 线上页面 | ego-browser 逐元素验证 | pass | 八组分别切换；CRM／线索入口、驾驶舱、模块导览、两类 GEO 预览、来源与刷新、大屏示例及公开／私有确认取消均有页面证据；不含真实业务写入与受限角色点验 |
| 收尾文档格式 | `npx -y markdownlint-cli2` | pass | 835 个文件，0 issues；修正 12 处既有加号转义和末尾换行，正文含义不变 |
| 收尾文档链接 | `python3 scripts/check-md-links-offline.py` | pass | 848 个已纳入索引的 Markdown 文件，断链 0 |

## Output Excerpts

```text
Test Files  145 passed (145)
Tests       1329 passed (1329)
Go packages 195 reported / 195 expected / missing=0 / failed=0
完整静态审计：通过（覆盖率与跨仓端口比对的跳过项另列）
跨栈权限门：353 条能力规则逐字一致 / 未收口 LEAK=0 / 整页 BLOCK=0 / UNROUTED=0
i18n：9 个语言；无法解析键 0；孤儿词条 0；门通过
平台 seed：HTTP 200 / 目录已同步 / modules=24
平台查库：catalog=24 / bindings=250 / dependencies=53 / grants=589
catalog_unchanged=true / dependencies_unchanged=true / grants_unchanged=true
```

平台 SQL 备份位于 `/www/wwwroot/hivemtk/platform-server/backups/navigation-20261126-084141/module-catalog-before.sql`，SHA-256 为 `04a5170cf8c13b15ea73d6fa46e5dc4d6d0c01b0cf45be2bfbbaf9bf87950189`；同目录保留同步前后的 JSON 快照。

## Residual Risks

- 本地覆盖率没有 profile，跨仓端口比对缺少相邻目录中的对端，均未判定。
- 页面验证使用现有管理员会话，未在生产新建受限角色、注入预览失败、发起 UA 探测、发送客户消息、购买或实际保存大屏；相关权限与失败行为的自动回归另列于上表。
- Graphify 因缺少本机 Python 模块未更新。

## Recommendation

本轮修复及既定关键页面验收通过。保留自动回归、产物摘要、平台授权对账和逐元素页面证据；三个环境未执行项不计通过。重启续作阶段只完成点验与文档，未重复构建、全测或发布。
