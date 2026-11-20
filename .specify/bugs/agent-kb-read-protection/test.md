# FA-AGENT-UI-02 · 测试报告

- 日期：2026-11-20
- Result：verified（代码、真实 PostgreSQL 消费者及本地浏览器受控验收；上线另见生产拓扑台账）
- 冻结：6640 个索引文件；最终验证结束及提交前逐文件 SHA-256 核对，漂移 `[]`。

## 红绿与回归

| 检查 | 实际结果 |
| --- | --- |
| 原真实 Vue/Element Plus 组件红灯 | 14 例全部失败：失败按空保存、未改也替换、旧响应覆盖及未知状态可写 |
| 最终定向组件回归 | 本项 18 例 + 模式保存 6 例，共 24 例通过，7.72 秒 |
| 全量前端 Vitest | 101 文件、996 例通过，命令 62.5 秒 |
| 前端 `eslint src --max-warnings 80` | exit 0，6.7 秒 |
| 新夹具及单测严格 ESLint | `--max-warnings 0`，exit 0，1.9 秒 |
| 生产 `npm run build` | exit 0，5.8 秒；九语种键对齐，Vite/PWA 构建成功 |
| `make test-go` | exit 0，1005.8 秒；157 个包 `ok`，零 `FAIL` |
| 新 PG 消费者定向 `go test -race` | exit 0，24.2 秒，服务包 2.180 秒 |
| `make audit` | exit 0，118.7 秒；跳过维度见下文 |
| `make lint` | exit 0，4.2 秒，0 issues |
| `make vet` / `go build ./...` | exit 0，8.4 / 32.8 秒 |
| 分层检查 | exit 0，0.6 秒；1596 个生产 Go 文件，零新增违规 |

组件实际挂载编辑页与 Element Plus，仅 API 边界替身。覆盖读取失败、已知空、未改选择、重试、显式清空确认/取消、待完成读取、旧成功/失败、异常响应及 ID、用户已改选择、来源变更、卸载、新建空与明确选择。主记录请求不再携带 `kb_ids`；没有可信更改时替换接口零调用。

真实 PostgreSQL 使用专用 `127.0.0.1:8217/user_db_test`，测试启用租户隔离并恢复原模式。真实仓储与服务验证绑定读取故障后普通名称更新保留原绑定，以及并发者明确改绑后普通更新不覆盖。生产 `SmartCSOrchestrator.resolveFAQKB` 消费者核对原 KB/version 7、新 KB/version 9 与显式清空后的空结果。

## 真实浏览器受控验收

ego-browser 在同一 TaskSpace 逐元素操作真实编辑组件，每次操作保存语义快照，以本地 `127.0.0.1:18213` 合成 API 留存请求和持久状态：

1. 绑定读取 HTTP 503 后只改名称保存，主记录请求无 `kb_ids`，没有绑定 PUT，原 `['11']` 保持。
2. 开放接口并点击重试，原知识库读回，选择器恢复可用。
3. 移除唯一选择后保存，确认框取消；两个写接口均无新增调用，原绑定仍在。
4. 再次保存并明确确认，主记录更新后仅此时执行绑定 PUT `[]`；重新编辑读回已知空集合。

浏览器接口为本地内存模拟，PG/消费者是另一个真实数据库测试，不把两者描述为同一在线会话。未改生产数据、未调用第三方或外发消息。TaskSpace 与自有夹具进程已结束。

## 边界与未执行维度

- 本项无后端生产代码、迁移、依赖、配置或路由变更；Go 新文件为真实消费者回归。
- 绑定消费者验证限知识库/答案缓存的 KB 与版本选择，不冒充缺少 KB 外键的全部 FAQ/SOP 内容检索。
- 绑定主动改动的版本冲突协议未新增；普通字段保存完全不触碰绑定。两个写请求的部分失败恢复仍由 FA-AGENT-UI-03 独立处理。
- audit 没有覆盖率 profile，本机 API 8204 未启动：两项明确 SKIP，不能当成通过；发布验收单独核对健康与公开默认口令探针。
- 按要求尝试 graphify 刷新，环境返回 `ModuleNotFoundError: No module named 'graphify'`，未声称知识图谱更新。

## 证据索引

日志及快照保存在本机批准临时目录下，前缀 `agent-kb-read-`，不将本机证据当作 CI 产物。

| 文件 | SHA-256 |
| --- | --- |
| `red.log` | `5fc4372f376081f9d437a43b8b700ff3e5332d5b8e8f838d529113df303297b9` |
| `final-web-unit.log` | `943e1cde4550a3299036629ecec0f5d80c2531daf0cf59beda4d578b6d705bbc` |
| `final-web-build.log` | `969af80d9b1cec1128f34ee713b79ac01c0f0c3c23841ed6d4ceef50141fdf8e` |
| `final-test-go.log` | `e14c787e20610dde3bf347069e312f8480b6eb3765295766a7bdfa73b5db1f44` |
| `final-race.log` | `6f027215d154a1d0383e0df983c9e679708d12790796ce938de407a3d52b06b2` |
| `browser-failure-name-saved.json` | `11abb1963f303437d4f4e87b70e3c0781ee7259e207e7946c41194d89b2002e2` |
| `browser-clear-cancelled.json` | `9dca6392d25bd8440b04f45acaeb1c4c76d38266dc07271d41b73aaa816e210c` |
| `browser-clear-confirmed.json` | `10b4a2b6bd16ce4c1dcc13478a349aaef037b100ce77132443078d8cddf53ffc` |
| `verified-source.json` | `3efc8426ca37cf2889c1d1c8900581b297fcb1dcf24503ffb64f5fbb97761f3e` |
