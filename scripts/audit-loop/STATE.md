# 永动审计循环 — 状态与报告（STATE）

> 机器状态见同目录 `state.json`；执行指令见 `PLAN.md`。本文件追加每轮审计报告，最新在上。

## 循环总览

- 循环启动：2026-09-08，由 ZCode 自动化每 30 分钟触发一轮
- 已完成轮次：1 / 角度序列：security → authz → architecture → error-handling → concurrency → data-integrity → api-contract → frontend → perf → test-coverage → config-deploy → docs-consistency →（循环）
- 累计发现 / 修复：2 / 2
- 下一轮角度：authz

## 轮次报告

### R1 — security（2026-09-08）

**审计范围**：硬编码密钥、SQL 注入、路径穿越、JWT/会话、CORS、SSRF 面、上传校验、exec 注入、.env 追踪、敏感日志、seed 凭据、JWT 中间件测试后门。

**发现与处置（2 项，均已修复）**：

1. `user-server/internal/service/email_tracking.go:28` — `emailTrackingDefaultSecret` 硬编码默认 HMAC 密钥常量（P2）。核实为**死代码**（secret 实际只从 `EMAIL_TRACKING_SECRET` 环境变量读取，未配置时签名为空串直接拒绝），但常量留在源码中会成为未来误用的种子。**处置：删除**。
2. `user-server/internal/service/email_unsubscribe.go:26` — `emailUnsubscribeDefaultSecret` 同上（P2）。**处置：删除**。

**核查通过项（无需修复）**：
- SQL 拼接点全部为内部常量表名/受控参数（migration/rag 索引/备份表），无用户输入直达
- `resolveLogPath` 有 `..` 检测 + 绝对路径白名单 + 相对路径 `logs/` 前缀三重守卫
- JWT secret：无 env 时非测试进程 panic，禁止硬编码兜底；黑名单吊销存在
- `IsTestMode` 生产默认 false 且 `testModeGate` 默认返回 false，无生产后门
- 上传链路：扩展名黑名单 + magic number 校验 + SVG 拒绝 + MIME 白名单 + 本地驱动 UUID 重命名
- CORS：默认拒绝，Origin 显式白名单 + 同源校验
- `.env` 均未入库（仅 example/production 模板，值为占位符）
- 密码重置日志只记 email 哈希/事件，无 token 落日志
- 登录/MFA/注册/忘记密码均已挂 `BruteForceGuard`

**观察项（不构成代码缺陷，留给对应角度轮次）**：
- `scripts/bootstrap.sh:38` 与 `scripts/geo_full_test.py:101` 内置 seed 密码 `Seed@123456`（公开开源仓库自举默认凭据，config.yaml 已明示固定标记，属产品决策而非泄漏；config-deploy 轮再评估是否加首次登录强制改密）
- govulncheck 在本机 Git Bash 下因路径解析问题无法运行（`no go.mod file` 假报错），下轮尝试 `go vet`+`golangci-lint` 替代或直接 Windows cmd 运行

**验证证据**：`go build ./...` OK；`go vet ./...` OK；`go test ./internal/service/ -run Email` OK。

**Commit**：见 git log `fix(security): 审计R1-security: 删除email追踪/退订服务死代码默认密钥常量`
