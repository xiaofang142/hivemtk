<!-- 恢复件：C 相复验子代理 `456671c1efa` 的回报正文（取该会话最长的一条 assistant 文本）。活动窗口 09-22T18:46→09-22T19:17Z（本地 +8）；装它的任务书＝lane-4-verify-then-fix.md。正文一字未改，由主会话 09-23 05:0x 从子代理逐字记录抽出落盘。 -->

三件事全部跑完。两条成立、一条否决，全部读数来自真跑，源码三处注码均已 `cp` 还原并核 md5 一致（未用 git checkout/restore/stash，未提交）。

## 环境闸门（先过再动）
- `pg_isready -h localhost -p 8232` → `localhost:8232 - 接受连接`，rc=0
- 口令：`POSTGRES_TEST_PASSWORD="$(awk -F'= *' '/^POSTGRES_PASSWORD=/{print $2}' .env | tail -1)"`，只回显 `len=48`，未打印值
- 闸门腿 `--- PASS: TestHandleIngress_SelfEchoDecisionPersistsNothing (0.58s)`（是 PASS 不是 SKIP）

## 4-A 钉钉 channel_msg_id —— **成立**
注码台账：`internal/service/dingtalk_app.go:177` `"channel_msg_id":             msg.MsgID,` → `"channel_msg_id":             "",`（diff 单行）｜备份 md5 `f3e5939a7d5cc7c1b6c867134a0782a8`，还原后同值。

注码下点名腿读数（逐字）：
```
--- PASS: TestIngress_PlatformIDEventsSkipContentDedup (0.86s)
--- PASS: TestN14_DingTalkRepeatedTextIsNotDropped (0.19s)
ok  	hivemtk-user/internal/service	1.842s
```
⇒ 该红的腿没红，成立。

新腿：`internal/service/dingtalk_channel_msg_id_test.go:28` `TestDingTalkInboundChannelMsgIDIsOfficialMsgID`（断言在 :45/:49/:53；照抄同类最窄腿 `webhook_batchf4_m01_qq_test.go:312` 的形状：真回调 payload → `f4DtSetup` 真管线 → 读回落库行 Extra，官方 msgId 取带 `+` `/` `==` 的形态，一次钉住非空+原样+不被清洗）。
- 同一注码下红过一次（真读数）：`--- FAIL: TestDingTalkInboundChannelMsgIDIsOfficialMsgID (0.35s)` + `dingtalk_channel_msg_id_test.go:50: 承诺 10 未达成：channel_msg_id 是空串（官方 msgId="msgAb3d/Efh+DlLtRjPrdtcmdid…Bw==" 没被带走）` + `:54: hub.Extra[channel_msg_id] = "", want "msgAb3d/…"`，`FAIL hivemtk-user/internal/service 1.101s`
- 还原后绿：全量里 `--- PASS: TestDingTalkInboundChannelMsgIDIsOfficialMsgID (0.19s)`

## 4-B 批次路径的 self-echo —— **成立**
注码台账：`internal/service/inbox_ingress.go:991` `if !decision.IsSelfEcho {` → `if true {`（`diff` 只出 `991c991`）｜备份 md5 `0ed83c722edea94931bf1edd8848ec17`，还原后同值。为避免把注码留在共享树里跑长批，注码态只用来 `go test -c -o /tmp/lane4-bak/service.4b.test`，随后立刻还原源码、全部读数由该二进制产出。

点名既有腿在注码态读数（逐字，`-test.run` 四条）：
```
--- FAIL: TestHandleIngressBatch_SelfEchoPersistsNothingAndDupPersists (0.26s)
--- PASS: TestHandleIngressBatch_DupDecisionPersistsWithAISuppressed (0.08s)
--- PASS: TestHandleIngress_SelfEchoDecisionPersistsNothing (0.06s)
--- PASS: TestInboxIngress_HandleIngress_SelfEcho_NotQueuedForAI (0.07s)
```
整包复验（同一 mutated 二进制，`-test.count=1 -test.timeout 40m`）：全 `internal/service` 只有 1 条 `--- FAIL`，就是我新增的那条，其余全绿（日志 `/tmp/lane4-bak/full-4b.log`，收尾 `FAIL` / `rc=1`）⇒ 批里那份确实从没被回声穿过，成立。

新腿：`internal/service/inbox_ingress_batch_echo_test.go:30` `TestHandleIngressBatch_SelfEchoPersistsNothingAndDupPersists`。两半都在：①回声段 :57-76（回环的"因"用 `r23SeedOutbound` 预置同会话近期 outbound + 入站同文，走真识别路径 `self-echo(recent outbound normalized match)`，刻意不塞 `Extra["channel_msg_id"]`，并先断 Reason 真含 `self-echo` 才允许后面断"不落库"）②反向段 :91-105（批里 `dup=true` 那条必须照样留痕），防"只在靶子上红"。
- 注码态红过一次（真读数）：`--- FAIL: … (0.54s)` + `inbox_ingress_batch_echo_test.go:74: §8.3-18 批次半边未达成：出站回声在批里又被当成客户消息存了一遍（inbound=1，Reason="intercepted by middleware: self-echo(recent outbound normalized match) (self_echo=true dup=false)"）`
- 还原后绿：全量里 `--- PASS: TestHandleIngressBatch_SelfEchoPersistsNothingAndDupPersists (0.08s)`

## 4-C 闭集与真产出的手抄对齐 —— **不成立（结果①，nore）**
注码台账：`internal/channelgw/protocol.go` 删掉第 340 行 `	"intercepted by middleware",`（`diff` 只出 `340d339`）｜备份 md5 `1a86e01be5e798fef88255dcca3c7f9d`，还原后同值。

点名两条腿在注码态读数（逐字，全部即红）：
```
--- FAIL: TestIsDuplicateReason_只认重复结论短语不认任意子串 (0.00s)
    dup_outcome_b17_test.go:28: 应判重复但判成非重复: "intercepted by middleware: self-echo(platform msg_id exact match) (self_echo=true dup=false)"
    dup_outcome_b17_test.go:28: 应判重复但判成非重复: "intercepted by middleware: duplicate(channel+sender+content) within window (self_echo=false dup=true)"
--- FAIL: TestIsDuplicateReason (0.00s)
    protocol_test.go:182: IsDuplicateReason("intercepted by middleware") = false, want true
--- FAIL: TestIsIngestDuplicate_ReasonKeywords (0.00s)
    handler_http_ack_test.go:27: 应判定为重复: "intercepted by middleware: self-echo(platform msg_id exact match) (self_echo=true dup=false)"
    handler_http_ack_test.go:27: 应判定为重复: "intercepted by middleware: duplicate(channel+sender+content) within window (self_echo=false dup=true)"
```
⇒ 属任务书里的结果 ①：注码即红，缺口不成立，不补跨包桥接腿，也不改任何既有期望。否决理由：`internal/bridge/handler_http.go:725-727` 的 `isIngestDuplicate` 整函数就是 `return channelgw.IsDuplicateReason(reason)`（不是第二份手抄表），所以"闭集 ↔ 真产出前缀"这格在两个包里各有一条不连库的腿正钉着那串字面量，抄错即红。

附带把上游那句话钉准（同一格的第二枚注码：删 `"self-echo",` + `"duplicate",`，随后同样还原核 md5）：`dup_outcome_b17_test.go:14` 那条**照样绿**（上游这半句成立），但 `protocol_test.go:169` 与 bridge 那条因表里的 `"duplicate delivery"` / `"duplicate delivery in 5min"` 变红；全仓 Go 侧没有任何产出方产出以 `self-echo`/`duplicate` 开头的 Reason（`interceptInbound` 仅两个调用点 `inbox_ingress.go:511`、`:975`，一律包 `intercepted by middleware: `）⇒ 这两项是被"非产出文案"钉住的冗余项，属文案卫生，不是缺腿。

## 门禁口径读数
- `go vet ./internal/service ./internal/channelgw` → **rc=0，零输出**
- `go test -count=1 -timeout 40m -v ./internal/service ./internal/channelgw`（含两条新腿，全量＝门禁口径，非 `-run` 子集）：
  `ok  	hivemtk-user/internal/service	604.115s` / `ok  	hivemtk-user/internal/channelgw	6.858s`，`gate_rc=0`；`--- PASS` 4072 条、`--- FAIL` 0 条、`--- SKIP` 4 条（`TestAIAgent_AssetBundleBinding`/`TestAIAgent_FullChain`/`TestPlatformAccountService_Login`/`TestWSTransport_NilPipeline`，均为既有无关跳过）
- 全程未遇到需归因的假红（唯一红都出现在注码态且逐条对得上注码），未触碰 `internal/browser_automation/**`、`internal/repository/message_hub_inbox_outbound.go`、`user-web/bridge/**`

## 顺手发现（未处置，只报告）
`internal/service/webhook_batchf2_n14_repeat_dedup_test.go:114` 的 docstring 写着"钉钉入站事件不带 channel_msg_id"，与 `dingtalk_app.go:177` 现状相反（注释漂移，不是断言写坏）。按边界我没动它，留给该文件 owner。
