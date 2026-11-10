# hivemtk Constitution

本文件把散落在 `CLAUDE.md` 中的项目铁律固化为 spec-kit 闸门约束。所有 spec-kit
工作流（bug 修复 / SDD 周期）在动手前须对照本文件，改动不合规即视为未完成。

## Core Principles

### I. 五层架构（NON-NEGOTIABLE）

所有 Go 代码严格遵循 `Router → Controller → Service → Repository → Model`。

| 层 | 职责 | 禁止 |
|----|------|------|
| Router | 仅 URL→Controller 映射 | 业务逻辑、内联闭包 |
| Controller | 参数绑定 + 调 Service + 返回响应 | SQL、业务判断 |
| Service | 业务逻辑 | 直接操作 DB |
| Repository | GORM 数据访问 | 业务判断 |
| Model | 纯数据结构 | 外部依赖 |

**铁律**

1. 禁止跨层调用 —— Controller → Repository 即属违规。
2. 禁止内联闭包（旧称"内联 handler"）—— `func(c *gin.Context){c.JSON(...)}` 不得出现在 router.go。
3. 每域四层齐全 —— 一个业务域的 Controller/Service/Repository/Model 四文件必须完整。
4. Router 只做映射 —— DI 装配在 `main.go` 完成。

**Gate**：改动若新增跨层调用或内联闭包，直接判不合规，不接受"临时"。

### II. 禁止子 Agent 并行（NON-NEGOTIABLE）

因 RPM（请求速率）限制，**禁止调用子 Agent（Task/搜索子代理等）并行执行任务**。
所有任务由主 Agent 直接执行、串行推进，不依赖子 Agent 分担。

本规则**优先于其他一切规则**，与本文件任何条目冲突时以本规则为准。需要
探索/搜索/实现时，直接使用主 Agent 自己的工具（Read/Grep/Glob/LS/WebSearch）。

**Gate**：不得以"并行更快"为由调用子 Agent。

### III. 验证纪律：不许吹大

判断一个修复"已完成"，必须满足：

- 真的执行了验证命令并贴出真实输出，**不得以描述代替执行**。
- 未能运行的检查记为 `skipped` / `not-run` 并写明原因，**禁止编造结果**。
- 未实际复现原症状时，不得标记为 `verified`，应降级为 `partial`。
- 全绿不等于验证过 —— 测试全过只说明"没测出问题"，不等于"证明没问题"。

**Gate**：任何"已修复""已验证"的结论，提问者要求出示命令与输出时必须能给出。

### IV. 契约一致性：外部成败以业务码为准

凡对接第三方平台（飞书 / 企微 / Telegram / WhatsApp），**不得以 HTTP 状态码
作为成败判据**。这类平台成功与失败常用同一个 HTTP 状态码表达，业务成败落在
响应体的业务码字段（如飞书 `code`、企微 `errcode`）。

新增或修改平台调用时，必须先 grep 仓内同类调用的既有口径并与之对齐；仓内已
有的一致做法优先于个人写法。仓内若只有孤例，视为可疑。

**Gate**：新增平台调用若只判 `StatusCode`，视为高危缺陷。

### V. API 契约

路径规范：

```
/api/{domain}/{resource}      # 用户端
/api/manage/{resource}        # 管理端
```

响应格式：

```json
{"code": 0, "data": {...}, "message": "ok"}          // 成功
{"code": 400, "message": "参数错误"}                  // 失败
{"code": 0, "data": {"list":[], "total":N}}          // 列表
```

错误码语义：`0`=成功 `400`=参数 `401`=认证 `403`=权限 `404`=不存在 `409`=冲突 `500`=服务端

## Additional Constraints

- **技术栈**：后端 Go + Gin + GORM；前端 Vue3 + Element Plus + Pinia（管理端）、
  UniApp + Pinia（移动端）。移动端不在本仓。
- **提交**：每次任务完成须自动 `git add` + `commit` + `push`，不等待确认。
  推送到该仓库**自己**的远端；远端名必须以现测 `git remote -v` 为准，不得写死
  `origin`（`hivemtk` 是 `gitee-upstream` + `upstream` 双远端，写死 `origin`
  会直接报 "The origin remote does not exist"）。
- **测试基础设施陷阱**：`user-server` 的测试库密码**按设计不从 `.env` 读取**
  （避免密钥泄漏进代码），须显式导出 `POSTGRES_TEST_PASSWORD`；
  `internal/.../testdb.go` 在数据库连接失败时会 `t.Skipf` 一律跳过 —— 这会造出
  **假绿**。看到某包"全过"时先确认它不是被跳过了。
- **安全**：密钥仅走环境变量或 `.env`，**禁止**写入代码、`.air.toml`、或任何
  被版本控制跟踪的文件。air 的 `[env]` 用 `os.Setenv` 会**覆盖**既有环境变量，
  不得在其中硬编码任何占位密钥。
- **graphify 知识图谱已停用**（2026-09-18 处置）。CLI 为失效 shim 且工作区内
  无 `graphify-out/`。**不得**作为强制规则；回答架构问题以直接阅读代码与
  `docs/architecture/`、`hivemtk/docs/` 为准。若未来要恢复，必须先修好安装
  并产出 `graphify-out/GRAPH_REPORT.md`，同时改本节回强制，否则禁止恢复。

## Development Workflow

- 修 bug 走 spec-kit bug 三段式：`speckit.bug.assess` → `speckit.bug.fix` →
  `speckit.bug.test`。其中 assess 与 test **不得改源码**，只有 fix 能改，且
  只能改 assessment 列出的文件。
- `assessment.md` 是**契约**，事后不得编辑。若发现评估错了，停止改代码，
  写进 `fix.md` 的 **Deviations from Assessment** 并建议重跑 assess。
- 新功能走 SDD 周期（specify → plan → tasks → implement），闸门处人工评审，
  `on_reject: abort`。
- API 改动后须跑 `scripts/api_verify_full.py` 做三端验证（入参 / 返回 / 预期 /
  DB 四项）。若该脚本缺失，不得静默跳过，如实记录。

## Quality Gates

改动完成须同时满足：

1. `go build ./...`、`go vet`、`gofmt -l` 全部干净（Go 侧）。
2. 受影响包的测试真实跑过并贴出输出；被跳过的包要说明。
3. 不违反原则 I–V 任何一条。
4. spec-kit 三份报告（assessment / fix / test）齐备且未过度声称。
5. 任务整体完成后才提交：一次性 `git add` 全部变更并 commit + push 到本仓库自己的远端；
   **禁止在任务中途为每个小功能/小改动单独提交**（规则1）。

## Governance

本宪法效力高于其他编码习惯与临时约定。与 `CLAUDE.md` 冲突时，**以仓库内的
`hivemtk/CLAUDE.md` 为准**；与本文件冲突时，以 `CLAUDE.md` 为准。

修订本文件须同步说明：改了什么、为什么、下一步影响哪些存量代码。

**Amendment Log**

- 2026-10-17 · v1.0.1：规则1 从「每次任务完成自动提交推送」改为「任务整体完成才提交推送，
  禁止小步中途提交」。原因：任务执行中频繁提交制造噪音、污染历史；改动留在工作区，
  任务整体完成且验证全绿后一次性 commit + push。影响：spec-kit 各阶段不再各自提交，
  三份报告 + 源码 + 回归测试随整个任务一次提交（与 CLAUDE.md 规则1、3.3.6、4.5.5 同步）。
- 2026-11-10 · v1.0.2：原则 I 层命名 Handler→Controller 对齐 CLAUDE.md 与代码现实
  （`internal/controller/` 全部为 `XxxController`，本仓无 `handler` 包，2026-11-10 复核）。
  原因：宪法仍用 Handler 口径，与 CLAUDE.md「无 handler 包」铁律冲突。影响：纯文档口径，
  无存量代码改动；spec-kit 闸门文案按 Controller 理解。

**Version**: 1.0.2 | **Ratified**: 2026-10-14 | **Last Amended**: 2026-11-10
