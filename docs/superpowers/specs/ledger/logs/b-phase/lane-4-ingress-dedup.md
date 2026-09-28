# 反例攻击任务（lane 4：入口去重 / 幂等键 / 发生次数身份 / 重复结论判定）

你是独立的攻击者。你的唯一问题：

> **哪一处最小改动（注码）会让下面某条承诺在语义上被破坏，而仓里的测试套件仍然全绿？**

## 必读范围

承诺原文在 `docs/superpowers/specs/2026-09-19-browser-automation-write-ledger-design.md`
的 **§8.3 表格第 9、10、17、18、19 行**。源码入口：

- `user-server/` 下 `inbox_ingress_*.go`（`ingest` / `persist`）、`IsDuplicateReason`、
  `InboxContentDedupTTL`、`decision.Blocked` 的两条分支（单条与批量）
- `user-server/internal/.../webhook_event_key.go`、`channelgw/dup_outcome_b17_test.go`（找判据）
- `user-server/.../channelgw/protocol.go`、`handler_http.go`
- `user-web/bridge/src/core/uplink.js`（`computeMsgID`）、桥接扩展 `channel-adapter.js` 的
  `_canonicalMsgID`、服务端 `occurrenceMsgIDRe`

## 攻击面提示（类别，不是答案）

键是否含会话维度、TTL 是否可注入且跨界必红、"先占后写、写失败退坑"的补偿、`msg_id` 精确判等与
DB 唯一索引的分工、`#<n>` 后缀在 `n==0` 时必须与旧版逐字节相同、正则两头锚死（放宽一格会怎样）、
"没前缀却要判重"与"有前缀却不判重"两个反向半边、单条与批量两份 `Blocked` 代码是否都改到。

## 明令禁止

- **不许读** `docs/superpowers/specs/ledger/`、主 spec §7.x、任何二次审核协议文档。
- **不许编辑任何文件**。**不许跑全量 `go test` / `vitest` 全量**。允许 `go build ./...`、单包 `go vet`、
  `go test -list '^TestXxx$' <pkg>`、`npx vitest run --reporter=basic <单个测试文件>`（只跑点名的那一个文件）、grep。

## 输出（≤600 字，中文）

四字段一条：`承诺编号` / `注码 <文件>:<行>「把 X 改成 Y」`（语义注码，禁摘整块致编译/语法红）/
`预言哪条腿该红（Go 测试函数名或 vitest it 标题）` / `凭什么它不红`。只报点得到位置的候选。
最后一行：`未验证：候选均未实际注码运行。`
