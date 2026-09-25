# 反例攻击任务（lane 5：B 链路出站重推 / 尝试上界 / SentCache TTL / DOM 复核）

你是独立的攻击者。你的唯一问题：

> **哪一处最小改动（注码）会让下面某条承诺在语义上被破坏，而仓里的测试套件仍然全绿？**

## 必读范围

承诺原文在 `docs/superpowers/specs/2026-09-19-browser-automation-write-ledger-design.md`
的 **§8.3 表格第 3、4、7、8 行**。源码入口：

- `user-server/` 下 `FetchOutboundUndelivered`、`MaxOutboundPushAttempts`、`push_error`、
  三条认领路径（grep 符号定位）、SSE 与轮询两条下行（`handler_http.go` 附近）
- 桥接扩展侧：`SentCache`（`downlink.js`）、`sentCacheTtlMs` / `sentCacheMax`（`constants.js`）、
  `add()` 的插入/命中语义、`reAckSentDuplicates`、`_sentKeys`
- 发送后 DOM 复核：`channel-adapter.js` 的 `sendVerified` 三态、`extra.send_verified` 的产出与消费

## 攻击面提示（类别，不是答案）

回收 `inflight` 时是否重置计数（不重置才有上界）、终态化 `failed` 的谓词、TTL 读侧与写侧是否都过、
淘汰从"插入序"变"最后命中序 + 到期"、`sendVerified` **永不参与 ack / markSent / 重推**（这条是
"结论位不得变成第二个闸门"，攻击方向是**把它接进去**看有没有东西红）、回查必须纯只读
（攻击方向：让回查写 `_sentKeys` / occurrence / `seenNodes`）。

## 明令禁止

- **不许读** `docs/superpowers/specs/ledger/`、主 spec §7.x、任何二次审核协议文档。
- **不许编辑任何文件**。**不许跑全量 `go test` / 全量 `vitest`**。允许 `go build ./...`、单包 `go vet`、
  `go test -list '^TestXxx$' <pkg>`、`npx vitest run --reporter=basic <单个文件>`、grep。

## 输出（≤600 字，中文）

四字段一条：`承诺编号` / `注码 <文件>:<行>「把 X 改成 Y」`（语义注码，禁摘整块）/
`预言哪条腿该红（函数名或 it 标题）` / `凭什么它不红`。只报点得到位置的候选；找不到就说找不到。
最后一行：`未验证：候选均未实际注码运行。`
