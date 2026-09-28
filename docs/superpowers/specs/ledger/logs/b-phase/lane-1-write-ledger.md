# 反例攻击任务（lane 1：写台账 / 双发闸门 / 副作用分类 / 唯一约束）

你是独立的攻击者。你不知道、也**不需要**知道本仓的审核结论。你的唯一问题：

> **哪一处最小改动（注码）会让下面某条承诺在语义上被破坏，而仓里的测试套件仍然全绿？**

## 必读范围（只看代码与这些承诺）

承诺原文在 `docs/superpowers/specs/2026-09-19-browser-automation-write-ledger-design.md`
的 **§8.2 编号列表第 2 条** 与 **§8.3 表格第 12、13、16、20 行**（"同行口径"与"本仓实测"两列）。
源码入口（`user-server/` 下）：

- `internal/service/write_ledger.go`（`recordSubmitState` / `guardResubmit` / `isWriteStep` /
  `classifyStepEffect` / `stepCouldBeSubmit`）
- `internal/service/executor.go`（写步派发前后的调用点）
- `internal/repository/step.go`（`FindSubmitAttempt` / 台账写入 / `ON CONFLICT`）
- `internal/model/`（`browser_steps` 相关模型与索引标签）

## 明令禁止

- **不许读** `docs/superpowers/specs/ledger/` 下任何文件（台账、日志）、**不许读**主 spec 的
  §7.x（那是别人的证据）、**不许读**任何 `*-review-*` / 二次审核协议文档。读了你的结论就不独立了。
- **不许编辑任何文件**。你是只读攻击者：注码是**描述**出来的，不是动手注的。
- **不许跑全量 `go test`**（共享树、别的泳道在飞）。允许 `go build ./...`、`go vet` 单包、
  `go test -list '^TestXxx$' ./internal/service/`、grep。判断"有没有腿"靠 `-list` 点名 + 读测试源码，
  不靠跑。

## 输出（≤600 字，中文）

每条候选一行，四字段，缺一不可：

1. `承诺` ：编号（如 §8.3-13）。
2. `注码` ：`<文件>:<行>` + 「把 X 改成 Y」。必须是**语义注码**（改判据、改条件、改取值、
   把 error 丢弃、把 fail-close 换成 fail-open），**禁止**摘掉整个函数/整块（那会编译红，编译红不算牙）。
3. `预言` ：哪一条（或哪几条）测试该红 —— 写测试函数名。
4. `凭什么不红` ：为什么全套仍绿（没有腿 / 有腿但走不到这一格 / 断言不含这个维度）。

只报你**在代码里点得到位置**的候选。找不到就说找不到，不要编。
最后加一行：`未验证：所有候选均未实际注入运行。`
