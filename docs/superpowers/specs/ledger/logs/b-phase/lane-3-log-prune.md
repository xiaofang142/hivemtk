# 反例攻击任务（lane 3：命令日志三态 / 裁剪摘要链 / 审计证据不消失）

你是独立的攻击者。你的唯一问题：

> **哪一处最小改动（注码）会让下面某条承诺在语义上被破坏，而仓里的测试套件仍然全绿？**

## 必读范围

承诺原文在 `docs/superpowers/specs/2026-09-19-browser-automation-write-ledger-design.md`
的 **§8.3 表格第 2、6 行**（外加 §8.2 编号第 3 条：`ok bool` 一列混装"跑了"和"成了"）。
源码入口（`user-server/` 下）：

- `internal/model/command_log.go`（`Ok *bool` 三态、`direction` 轴、`PruneBefore`）
- `internal/repository/command_log*.go`、`internal/service/` 里写 `write_confirm` judge 帧的位置
- `browser_audit_digests` / `browser_audit_prune_runs` 的模型与写入路径（grep 表名定位）
- `internal/service/retention.go`
- 迁移文件（grep `v3.44` 或 `browser_audit_digests`）

## 攻击面提示（类别，不是答案）

"摘要与 DELETE 同事务"、"每次裁剪扫描都落一行 run"、"计数与算术闭合"、`batch_digest` 的聚合顺序、
`chain_hash = sha256(prev ‖ batch_digest)`、存量假 `✓` 置 NULL 的谓词边界、
"0 行也要落 run 行"、`Ignore` 语义与摘要写入的交互。

## 明令禁止

- **不许读** `docs/superpowers/specs/ledger/` 下任何文件、主 spec §7.x、任何二次审核协议文档。
- **不许编辑任何文件**。
- **不许跑全量 `go test`**。允许 `go build ./...`、`go vet` 单包、`go test -list '^TestXxx$' <pkg>`、grep。

## 输出（≤600 字，中文）

四字段一条：`承诺编号` / `注码 <文件>:<行>「把 X 改成 Y」`（语义注码，禁摘整块）/
`预言哪条腿该红（函数名）` / `凭什么它不红`。只报点得到位置的候选；找不到就说找不到。
最后一行：`未验证：候选均未实际注码运行。`
