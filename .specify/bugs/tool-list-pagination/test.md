# Bug Verification: 工具列表分页与可靠读取

- Slug: tool-list-pagination
- Tested: 2026-11-20
- Assessment: [assessment.md](assessment.md)
- Fix: [fix.md](fix.md)
- Result: verified

## Summary

原组件不传分页参数、忽略服务端 total 且没有分页入口，21 条以上工具不可完整管理。原实现组件回归出现 10 项业务失败；修复后 16 项组件回归及全量前端 99 文件、972 测试通过。真实 ego lite 浏览器挂载生产 Vue 组件，使用本地模拟接口逐元素验证第 21、101 项可达和状态保存、筛选、失败恢复及末页删除恢复。仓库 Go 门禁与生产前端构建均通过；正式发布另以生产拓扑登记为准。

## Checks Performed

| Check | Command / Action | Result | Notes |
|---|---|---|---|
| 原缺陷复现 | `npx vitest run tests/unit/tool_list_pagination.test.js`，原组件 | fail, expected | 10 failed、1 passed；缺分页、总数错误及过时响应覆盖均实际红灯 |
| 定向组件回归 | 同上，真实 Vue、Element Plus 与 API 边界模拟 | pass | 16 项；覆盖 21/101 条、所有六页、页外保存、筛选/重置/刷新/条数、保存失败、读取失败、响应竞争与越界恢复 |
| 完整前端回归 | `npx vitest run` | pass | 最终 exit 0，42.9 秒；99 文件、972 测试，无失败 |
| 前端 lint | `npx eslint src --max-warnings 80` | pass | 最终 exit 0，19.9 秒，无输出 |
| 测试代码 lint | `npx eslint tests/fixtures/tool-pagination tests/unit/tool_list_pagination.test.js --max-warnings 0` | pass | 最终 exit 0，1.7 秒，无输出 |
| 生产构建 | `npm run build` | pass | 最终 exit 0，17.3 秒，含 i18n prebuild、Vite 及 PWA 产物；没有放宽阈值 |
| 全量 Go | `make test-go` | pass | exit 0，1001.5 秒，157 条 ok 包输出；含专用测试库认证预检 |
| 静态审计 | `make audit` | pass | exit 0，92.4 秒；两项未运行维度见下文 |
| Go lint / vet | `make lint`；`make vet` | pass | exit 0，24.7 秒、2.3 秒；golangci-lint 2.10.0，0 issues |
| 分层边界 | `python3 scripts/check-layer-boundary.py` | pass | exit 0，1596 个生产 Go 文件，命中、新增均为 0 |
| 源码稳定性 | 各轮执行前后逐文件 SHA-256 | pass | 完整 Go 门禁前后未变；最终前端轮 6619 个文件前后未变。补译后另核对 4464 个 Go/模块文件与本次完整 Go 验收一致 |
| Markdown 链接 | `python3 scripts/check-md-links-offline.py` | pass | 提交前再次按最终索引扫描 |

## Browser Evidence

- 21 条：从第一页翻到第二页，只有 tool-21；保存禁用后 HTTP 200 的模拟事件记录 `operation=status`、`name=tool-21`、`enabled=false`，保持第二页，总数 21、本页启用 0、禁用 1。
- 101 条：先显示六页，切换每页 100 后翻到第二页管理 tool-101；实际状态更新后重读 page 2/page_size 100。再按已禁用筛选时回到第一页，total 1；启用唯一结果后 total 0，与读取失败分开显示。
- 读取失败：夹具下一次 GET 返回 HTTP 503，持久错误及刷新指引可见，可操作旧行清空；刷新后重新出现真实组件工具行。
- 末页并发删除：移除合成第 101 项后刷新，接口事件记录先读 page 2，再回读 page 1；总数及行数均为 100。最终截图中分页完整可见，无重叠或裁切。
- 每次具体元素点击后保存快照；没有枚举或循环点击，没有新增浏览器 profile、重新登录或清理用户状态。TaskSpace 22 已关闭，本地夹具服务已停止。

## Evidence Integrity

原始日志和浏览器快照保留于本次本地执行环境，以下摘要不冒充 CI 制品。

| Evidence | SHA-256 |
|---|---|
| `tool-pagination-red.log` | `1bfd9f77bb7e32927550b8462119111261b0ab778ea02ea9d9352e9b407c44cd` |
| `tool-pagination-corrected-web-unit.log` | `822d5c91b0fb08f8a4f95205f7f85459d8572a05a83d150925b34766fccaa923` |
| `tool-pagination-corrected-web-build.log` | `fcf7ced445bcf1989ea0330bad7a328aedb68ce33b08419008ed64a741bad06c` |
| `tool-pagination-final-test-go.log` | `0a7b5cd0362c730561b025003650770761ff8ebc6244b729bd07abd7e5f9c24e` |
| `tool-pagination-final-audit.log` | `7484793d353f8614ee6ddcf019c24b9c6fa6727665c561260ac1bf6fc4a42c0b` |
| `tool-pagination-browser-disabled101.json` | `6bf9782a7ad0238169df8942855049626218be59ccffdf8e6510db2a37ac09b6` |
| `tool-pagination-browser-read-failure.json` | `31efa5f1ea140f096821db2b3cc0c4573ed545e22531f5790e71102210a534ef` |
| `tool-pagination-browser-delete-recovery.json` | `5d6f17bd9754db8baf373dd31bff1ee4dc79c39570b1a86644579532009a9a58` |
| `tool-pagination-browser-final.png` | `a8f9be249dfba5cae7716068f32323ab4d51d41b1470882638a05d6f274875bf` |

最终前端轮源码清单 `tool-pagination-final-source.json` 的 SHA-256 为 `fb65d5510ba365e41cf9d6b0f4a32dbf8b6202511085b7840ef943e63606e130`。

## Residual Risks and Explicit Limits

- 首次构建被七个语种缺失共 56 个新 key 阻断，返回修复阶段补齐九语种八条文案后重新执行完整前端回归、lint 和构建，全部通过；Go 文件未变，没有把旧构建失败隐去或重复宣称重跑 Go。
- 浏览器接口是本地内存模拟信号，验证生产组件与分页契约，不代表真实生产工具被修改或第三方生产送达。测试夹具没有进入生产入口或改变认证链。
- `make audit` 的覆盖率 profile 不存在、本机 API 未启动，两维明确 SKIP；未声称这两维通过。正式发布须核验公网健康、资产及回滚点。
- graphify-out 图谱不存在，要求的 rebuild 在默认及 CLI 对应 Python 均因 `ModuleNotFoundError: graphify` 未运行，未声称已刷新图谱。
- 无新增后端接口、依赖、迁移或生产配置。按链 D 交付相同前端源码，核对公网 index、实际工具页面分块与版本摘要，再对原清单精确销项。
