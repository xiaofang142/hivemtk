# 反例攻击任务（lane 2：D7 人工确认闸门 / 载荷哈希 / 挂起态可查证）

你是独立的攻击者。你不知道、也不需要知道本仓的审核结论。你的唯一问题：

> **哪一处最小改动（注码）会让下面某条承诺在语义上被破坏，而仓里的测试套件仍然全绿？**

## 必读范围

承诺原文在 `docs/superpowers/specs/2026-09-19-browser-automation-write-ledger-design.md`
的 **§8.3 表格第 5、14、15 行**（"同行口径"与"本仓实测"两列 + "取舍"列里写明的落地形状）。
源码入口（`user-server/` 下）：

- `internal/service/`：`confirmGate` / `SignalConfirm` / `SessionService.Confirm` / `d7_confirm` /
  `d7_wait` 的产出点（grep 符号定位，别信文件名）
- `internal/model/session.go`（`ConfirmPending`）
- `internal/controller/session.go`（`confirm-gate` 读侧端点、归属校验）
- `pkg/` 或 `internal/` 里的 `payload_hash` / `HashWriteText` 定义与消费点

## 攻击面提示（不是答案，只是"从哪里下刀"的类别）

载荷绑定的三态判定、一次性消费（先摘后关）、跨实例判别位（`gate_on_another_instance`）、
`expires_at` 与内存真值的 AND、审计帧的三条出路（放行/被拒/超时）、正文是否泄漏进导出。

## 明令禁止

- **不许读** `docs/superpowers/specs/ledger/` 下任何文件、**不许读**主 spec §7.x、
  **不许读**任何二次审核协议文档。
- **不许编辑任何文件**。注码是描述出来的，不是动手注的。
- **不许跑全量 `go test`**。允许 `go build ./...`、`go vet` 单包、`go test -list '^TestXxx$' <pkg>`、grep。

## 输出（≤600 字，中文）

每条候选四字段：`承诺编号` / `注码 <文件>:<行>「把 X 改成 Y」`（必须语义注码，不许摘整块致编译红）/
`预言哪条测试腿该红（写函数名）` / `凭什么它不红`。
只报在代码里点得到位置的候选。找不到就说找不到。
最后一行加：`未验证：所有候选均未在真树上注码跑过。`
