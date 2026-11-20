# FA-AGENT-UI-01 验收报告

- 日期：2026-11-20
- Result：verified（代码与受控验收；上线记录另见生产拓扑台账）
- 范围：模式保存、服务端模式校验、生命周期入口权威读取。知识库绑定的读取与部分保存问题属于独立清单项。

## 红绿证据

1. 真实 Edit 组件的初始 6 项测试中，4 项因创建/更新请求缺少 `agent_mode` 失败，2 项保存失败处理通过。
2. PostgreSQL 管理 API 初始接受非法 `hybrid`，HTTP 200 并改动持久化版本；校验修复后非法值返回 400，记录与版本不变。
3. 按生产装配分离管理与运行 service 后，两个方向的更新均复现旧模式缓存；生命周期专用读取绕过本地上下文缓存后通过。其他上下文读取保留原缓存契约。

## 最终自动验证

| 命令 | 结果 | 耗时 |
| --- | --- | --- |
| `make test-go` | exit 0，157 个包输出 ok；含真实 PostgreSQL 测试 | 1033.8 秒 |
| `go test -race ./internal/app ./internal/service -count=1 -p=1 -timeout=1800s` | 两包通过，无竞态报告 | 491.6 秒 |
| `make audit` | exit 0；以下两项明确 SKIP | 125.9 秒 |
| `make lint` | exit 0，golangci-lint 2.10.0，0 issues | 32.4 秒 |
| `make vet` | exit 0 | 35.1 秒 |
| `go build ./...`（user-server） | exit 0 | 18.2 秒 |
| `python3 scripts/check-layer-boundary.py` | 1596 份生产 Go 文件，零跨层违规 | 0.2 秒 |
| `npx vitest run`（user-web） | 100 个文件、978 项测试全部通过；含 6 项模式组件测试 | 63.8 秒 |
| `npx eslint src --max-warnings 80` | exit 0，无诊断 | 7.1 秒 |
| `npm run build` | exit 0，Vite 与 PWA 构建通过 | 11.4 秒 |
| 模式夹具与单测 eslint，`--max-warnings 0` | exit 0，无诊断 | 2.1 秒 |

验证前后 6631 个索引文件逐一核对 SHA-256，`changed_files=[]`。专用测试数据库为本机 8217 端口的 `user_db_test`，不使用生产库；租户隔离测试显式开启 ModeOn 并在结束时恢复。

真实 PostgreSQL + Controller + Repository + Service + 生产生命周期 HTTP 路由覆盖主动/被动创建、双向更新、读取重载、独立 service 暖缓存与冷读取、更新省略模式、非法字符串/空值/null/类型及无效 JSON。生命周期末端采用计数消费者，核对实际分派与调用次数；未调用第三方或外发消息。缺失/跨租户上下文与注入的数据库读取故障均拒绝执行，已有缓存不能放行。

## 真实浏览器验收

使用 ego lite 复用浏览器环境，在本地受控夹具逐元素操作真实 Edit 组件；每次操作后保存完整快照。夹具 API 使用内存记录，浏览器验收不冒充 PostgreSQL 贯通测试。

- 主动模式创建后，请求和已存记录均为 active，重新编辑回显主动模式。
- 更新为被动模式后，请求和已存记录均为 passive，重新编辑回显被动模式。
- 切回主动模式时注入一次 HTTP 503，页面提示保存失败、不返回列表，已存模式仍为 passive；再次保存成功后为 active。
- TaskSpace 23 与本地夹具进程已结束；没有修改生产智能体或发送消息。

## 本地证据摘要

证据位于本机批准的 opencode 临时目录，不伪称为 CI 产物。

| 文件 | SHA-256 |
| --- | --- |
| agent-mode-web-red.log | fb55a36d14bcded24d71e2165935c627cb466749166745e8d5ffadb1668b9149 |
| agent-mode-go-red.log | e6909533efb9305175eb6a75e18474dfeff1a7a258bf0af9624920ba3034e134 |
| agent-mode-cache-red.log | 835bcc689bf2a61453667db7092327278aafbffbd8b4d375daa09d220fad44eb |
| agent-mode-final-test-go.log | e96306ed8398e6e4e42e71fc2e3beca3ce095ccfcb879772e88aabd5a69a5e12 |
| agent-mode-final-race.log | a5866419d0da1d3b851e31d64e460ca862513773c6d85c9678e588cac8eeb528 |
| agent-mode-final-web-unit.log | 11a38bd6e45b9ecec618a6db48fb54861fb64d77ed03199cd408102f913b7a0f |
| agent-mode-final-web-build.log | 16d3428d256053982fd8ab3df14f35dfdde18b951db615e386033485c0f120a0 |
| agent-mode-browser-updated-passive.json | d8bf5c632f918b527b4ecad17ab743219c5e0ccae97a92e539ae3cbcfc071508 |
| agent-mode-browser-save-rejected.json | 5f87a59d7033108aa8a507c72c9ed5a260cc845f6cfcaab5f476fe247268627e |
| agent-mode-verified-source.json | cf4fc405a6690b0f57b734d20e4216d2d957a1e349e1ba888886bc638acb8635 |

## 明确未验维度

无覆盖率 profile，audit 的覆盖率维度为 SKIP；本机 8204 API 未启动，audit 的默认口令探针为 SKIP，正式发布须另行执行线上探针。Graphify 刷新实跑后因缺少 Python graphify 模块失败，未声称图谱已更新。没有新增迁移、配置、依赖、路由或交互文案；原有未知历史模式的安全回退未改变。
