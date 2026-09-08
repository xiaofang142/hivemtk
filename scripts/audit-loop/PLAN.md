# 永动审计循环 — 执行手册（PLAN）

> 本文件是无状态自动审计循环的**唯一权威指令**。每次触发（自动化或手动）时，agent 必须：
> 1. 通读本文件，2. 读取 `scripts/audit-loop/STATE.md` 与 `scripts/audit-loop/state.json` 确定轮次与角度，3. 执行该角度审计，4. 发现问题立即修复并回归验证，5. 提交推送，6. 推进状态。
>
> 本手册与 CLAUDE.md 同级生效：发现问题必须当轮修复（禁止记录待办），每轮必须提交，推送 Gitee + GitHub 双远端。

---

## 角度清单（12 角度，每轮一个，依次循环）

| # | 角度 ID | 名称 | 审计范围与要点 |
|---|---------|------|----------------|
| 1 | `security` | 安全 | 硬编码密钥/token/密码；SQL 拼接注入；路径穿越；SSRF；不安全反序列化；JWT/会话缺陷；CORS 过宽；敏感信息泄漏到日志；`os/exec` 注入；上传文件校验。工具：`govulncheck`、grep 套件 |
| 2 | `authz` | 认证与授权 | 每个管理端路由 `/api/manage/*` 是否有权限中间件；越权（水平：他人资源 ID 直取；垂直：普通用户调管理接口）；公开路由清单是否最小化；token 刷新/吊销 |
| 3 | `architecture` | 架构分层 | 五层铁律：Router 无内联 handler、Handler 不写 SQL、Service 不直接操作 DB、Repository 无业务判断；每域四层齐全。跑 `scripts/check-architecture.sh` |
| 4 | `error-handling` | 错误处理 | Go 忽略的 `err`（`_ =` / 裸调用）；panic 风险（数组越界/nil 解引用/类型断言无 ok）；前端未捕获 Promise；错误吞掉后继续执行 |
| 5 | `concurrency` | 并发与竞态 | map 并发写；goroutine 泄漏（无退出通道）；channel 死锁；`go vet -race` 可疑点；全局可变状态；WS/SSE 连接清理 |
| 6 | `data-integrity` | 数据完整性 | 缺失唯一约束导致重复行（参考 daily_stats 教训）；事务缺失（多表写无 tx）；外键/级联删除孤儿；迁移与模型不一致；decimal 浮点误用 |
| 7 | `api-contract` | API 契约 | 前端 `src/api/*.js` 调用路径 vs 后端 router 注册一一对应；响应格式 `{code,data,message}` 一致；错误码语义正确；Swagger 漂移；404/死接口 |
| 8 | `frontend` | 前端质量 | Vue 组件空指针渲染（`undefined.toFixed` 等）；el-tag 空 type 家族；v-for 缺 key；内存泄漏（未清理定时器/监听器）；eslint 归零 |
| 9 | `perf` | 性能 | N+1 查询（循环内 DB 调用）；缺索引高频查询（看 migrations）；无分页大列表；内存无限增长缓存；前端包体积/重复请求 |
| 10 | `test-coverage` | 测试缺口 | 每域 service 有无测试；关键链路（渠道入站→AI 承接→出站）回归脚本存在且可跑；测试失败/flaky；`go test ./...` 全绿 |
| 11 | `config-deploy` | 配置与部署 | config.yaml 与代码字段一致；.env 键名与引用一致；docker-compose 端口 vs `docs/PORT_REGISTRY.md`；生产默认值（debug 模式/默认密码）；迁移幂等性 |
| 12 | `docs-consistency` | 文档一致性 | 跑 `scripts/check-doc-consistency.sh` 与 `check-feature-doc.sh`；README/DEV_DOCS_INDEX 与实际路由/端口/功能对齐；CHANGELOG 更新 |

## 轮次算法（每次触发执行一遍）

```
0. 读 STATE.md + state.json → 确定本轮 angle（按上表循环取模）与 round 号
1. git pull gitee master --ff-only（远端有新提交则先同步，冲突则停轮报告）
2. 执行该角度审计（PLAN 表格中该角度的要点全部过一遍），产出问题清单
3. 每个问题：立即修复 → 回归验证：
   - Go: cd user-server && go build ./... && go vet ./... && go test ./...
   - Web: cd user-web && npx eslint src && npx vitest run（若改动前端）
   - 全量: python scripts/api_verify_full.py（若有运行环境，跳过需注明原因）
4. 验证全绿 → git add -A && git commit（规范：<fix|chore|test>(<scope>): 审计R<n>-<角度>: <摘要>）→ git push gitee master && git push github master
5. 更新 state.json（round+1、angle 序号+1 取模、时间戳、发现/修复计数）与 STATE.md（追加本轮报告段落：发现什么→修了什么→验证证据→commit hash）
6. 若本角度发现 0 问题：也要提交（仅状态文件变更，chore 前缀），保证每轮留痕、状态推进不丢失
7. 异常处理：构建红/测试失败修不动 → STATE.md 记录"阻塞"段落，commit 留痕后正常推进状态，下轮换个角度可能解开后回来补修
```

## 防丢失与可监测设计

- **状态持久化**：全部进度在 `scripts/audit-loop/state.json`（机器读）+ `scripts/audit-loop/STATE.md`（人类读），随 git 提交推送 — 仓库即状态，任何会话丢失都可从 git 恢复。
- **断点续传**：每次触发以 `state.json.next_angle` 为准，不依赖会话记忆。
- **审计留痕**：每角度报告追加在 `STATE.md`，commit message 带 `审计R<n>-<角度>` 前缀，`git log --grep` 可检索全部历史。
- **循环触发**：ZCode 自动化每 30 分钟触发一轮；prompt 内联指回本文件。

## 角度闸门要点（各角度最低验证命令）

| 角度 | 最低验证 |
|------|----------|
| security | `govulncheck ./...` + grep 套件归零 + build/test 绿 |
| authz | 管理路由清单核对表 + build/test 绿 |
| architecture | `scripts/check-architecture.sh` 绿 |
| error-handling | `go vet ./...` 绿 + grep 归零 |
| concurrency | `go build -race && go test -race ./...`（至少改动包）|
| data-integrity | 迁移可重放检查 + build/test 绿 |
| api-contract | 前后端路由对照脚本/人工核对 + build 绿 |
| frontend | `npx eslint src` 归零 + `npx vitest run` 绿 |
| perf | 热点查询 explain/索引核对 + build 绿 |
| test-coverage | `go test ./...` 全绿 + 新增缺口补测 |
| config-deploy | 端口/键名对照表 + build 绿 |
| docs-consistency | `check-doc-consistency.sh` + `check-feature-doc.sh` 绿 |
