# 架构审计修复验证

日期：2026-11-20；全量验证基线 `5f3e2cf93`，随后整合共同远端 `3d731f77c` 的可靠交付改动并复跑受影响范围。日志保存在本机临时目录 `architecture-audit-ih7yqv`，关键复跑命令与判据同时保留于此，临时目录不作为永久依赖。

## 先失败再通过

- 分层门新增嵌套违规及合法前缀对照后，旧实现漏掉四个违规并误判合法 `controllerkit`；修复后 `python3 scripts/check-layer-boundary.py --selftest` 的 17 个逐文件夹具通过。
- 阻塞任务测试先观察到 cron 包装器提前返回；修复后真实 Skip 覆盖任务体。
- 真实 AddTask 包装器中先观察到 panic 后调用次数仅 1；调整 Recover / Skip 顺序后为 2。取消、排空、拒绝新增及未配合取消的等待错误均有持久回归。
- http-core 原生命令先因导入 Vitest 报错；九个既有断言迁移后 `node --test packages/http-core/index.test.js` 为 9 PASS / 0 FAIL。
- 隔离副本复制真实管理端 `tests/unit/http-core.test.js` 和共享源码，正常实现 9 PASS；将成功码判断故意改为恒 false 后，真实消费用例以 AssertionError 失败，退出 1。未修改工作区共享实现。
- 干净检出复跑 SDK 时，原独立 CI 命令因没有 IIFE 产物退出 1；补齐 npm ci / build 后，在无 node_modules、无 dist 的独立副本按完整顺序通过 unit 112、boundary 62、共享 HTTP 9 项断言。

## 已运行的验证

Go 测试使用文档规定的专用 PostgreSQL 测试实例（127.0.0.1:8217），凭证从本机忽略配置读取，不写入仓库；复跑先按 CLAUDE.md 设置 POSTGRES_TEST_*。

| 命令 / 范围 | 结果 |
|---|---|
| `python3 scripts/check-layer-boundary.py --selftest` | 17 夹具通过 |
| `python3 scripts/check-layer-boundary.py` | 整合前 1600、可靠交付整合后 1604 个生产 Go；均 0 违规、0 豁免 |
| `bash scripts/check-architecture.sh` | 通过；既有 webhook service interface warning 保留 |
| `make lint` | golangci-lint 2.10.0：0 issues |
| `go build ./...`、`make vet` | 通过 |
| `make test-go`（`5f3e2cf93` 加本轮修复） | 专用 PostgreSQL 实例上全量通过；158 条包通过记录、35 条无测试文件记录，0 FAIL / panic |
| `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' ./cmd/api` | 交叉编译通过；产物为静态链接的 Linux x86-64 ELF |
| cron、contentguard 全包、controller、router 测试 | 通过；Controller 112.292s、Router 39.270s |
| `go test -race ./internal/pkg/cron -run '^(TestGoTask_|TestTaskManager_)' -count=3 -timeout=120s` | 通过，13.873s；三轮均无数据竞争 |
| `go test ./internal/app -count=1 -timeout=180s` | 通过，42.936s；含同期推理规划/执行/审批/失败封套与生命周期测试 |
| 可靠交付整合后的 app / runtime / controller / router / repository 全包及 service 的 `TestInferenceDelivery` | 全部通过，依次 41.701s / 2.757s / 67.531s / 9.625s / 33.386s / 1.487s；包含 PostgreSQL 交付账本和丢失回执恢复 |
| `make audit` | 可靠交付整合后再次通过；覆盖率文件缺失、本机 API 未启动、对端仓未检出的跨仓比对均明确跳过，不记为相应维度验收 |
| 管理端全量 Vitest | 最新集成目录 102 文件、1014 PASS |
| 桥扩展全量 Vitest | 107 文件、1323 PASS、7 SKIP |
| 浏览器自动化全量 Vitest / 构建 | 21 文件、222 PASS；构建通过 |
| SDK 干净副本 `npm ci && npm run build && npm run test:all` | unit 112 PASS、boundary 62 PASS、0 FAIL；随后共享 HTTP 9 PASS |
| workflow YAML 与路径选择 | 四份 YAML 可解析；两个共享包的单文件修改均选中 SDK / 主前端 / 桥与自动化作业 |

JavaScript 消费者已在最新集成目录重跑，SDK 另从无依赖及构建产物的副本验证完整 CI 顺序。Go 及架构门也在独立目录基于最新远端整合后验证，避免以其他会话在途差异代替可重现提交。

整合可靠交付改动前，本机发布候选产物 SHA-256 为 `1b8c775213f231a363a68ea50434c8536ef1d3705e6845e1e60b9a7e093747c4`；它仅保留为该次编译证据，不代表最终候选或线上版本。

## 全量与交付状态

`make test-go` 全量已通过；随后可靠交付整合的受影响六个包范围全部通过。暂存文档链接已扫描 481 个 Markdown、0 断链。标准发布执行器 `./deploy/deploy-api.sh --dry-run` 曾在预检拒绝本隔离目录的 detached HEAD：只允许 master；未进入上传或生产重启。提交同步后从干净 master 检出运行标准发布预检，原主目录的其他会话在途修改保持原样。正式上线按用户后续限制等待明确批准，发布状态以发布执行器及拓扑台账为准。

## 未验证边界

没有运行真实浏览器页面、真实消息/模型外发、支付结算；没有把其他 FA 待验项标为通过。全树门禁不代表完整业务验收。graphify 重建命令已尝试，本机未安装 graphify 模块，未伪造图谱更新。
