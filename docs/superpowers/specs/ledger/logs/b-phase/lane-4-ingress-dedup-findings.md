<!-- 恢复件：本文件是 B 相 lane 4 子代理的**原样回报正文**，由主会话于 2026-09-23 04:2x 从子代理逐字记录 `agent-ageneral-purpose-5a33f776bb17873e.jsonl` 的最后一条 assistant 文本里抽出落盘（派发时间 02:2x，报告当时只留在会话里、没进磁盘）。它是**二手候选**：每条都带「未验证：候选均未实际注码运行」的自陈，逐条的重验结果在 `logs/c-phase/` 与主 spec §八/§7.29。-->

## 反例攻击结论（lane 4 · §8.3 第 9/10/17/18/19 行）

**1. 承诺 10**
- 注码：`user-server/internal/service/dingtalk_app.go:177`「把 `"channel_msg_id": msg.MsgID` 改成 `"channel_msg_id": ""`」
- 该红的腿：`TestIngress_PlatformIDEventsSkipContentDedup`（`inbox_ingress_dedup_scope_test.go:116`）与 `TestN14_DingTalkRepeatedTextIsNotDropped`（`webhook_batchf2_n14_repeat_dedup_test.go:116`）
- 凭什么不红：官方 id 守卫读的是 `Extra["channel_msg_id"]`（`inbox_ingress_ingest.go:57`），L1 自己手工塞这个键（`inbox_ingress_dedup_scope_test.go:127`），从不走过生产者；N14 那条只数 `message_hub` 行数（`:138`），而承诺 18 落地后「被判重复也要留痕入库」⇒ 第二条照样成行，行数仍是 2，AI 却被压掉。同类四个生产者都有直断 Extra 的腿（`webhook_channel_qq.go:114`←`webhook_batchf4_m01_qq_test.go:312`、`channelbot/core/core.go:203`←`core_test.go:73`、`qq/qq.go:664`←`qq_test.go:591`、`controller/wechat.go:285`←`wechat_batchf4_m01_inbound_test.go:153`），只有钉钉这一格没腿。

**2. 承诺 18**
- 注码：`user-server/internal/service/inbox_ingress.go:991`「把 `if !decision.IsSelfEcho {` 改成 `if true {`」（批次路径回声也落库）
- 该红的腿：应有一条 `TestHandleIngressBatch_SelfEchoDecisionPersistsNothing`
- 凭什么不红：`go test -list '^Test.*SelfEcho.*' ./internal/service` 实测只有单条腿——`TestHandleIngress_SelfEchoDecisionPersistsNothing` 走 `svc.HandleIngressMessage`（`inbox_ingress_dedup_scope_test.go:333`），批次那份 `handleIngressSingleForBatch` 从未被回声事件穿过；批里的正向腿（`:283`）断的是 dup 要落库，注码后照样成立。正是「单条半边绿、批里存两遍」。

**3. 承诺 17**
- 注码：`user-web/bridge/src/core/channel-adapter.js:162`「把 `occurrence > 0 ? \`${base}#${occurrence}\` : base` 改成 `occurrence >= 0 ? …`」（首条 `_sentKeys` 键形变 `base#0`）
- 该红的腿：`test/adapter-r23-occurrence-identity.test.js` 的四条 it（实跑 4 passed），尤其「增量路径…event_id 为裸哈希与 #1」的 `expect(first.event_id).toBe(base)`
- 凭什么不红：该文件断言的全是 `_canonicalMsgId`（`:191-197`，注码未碰）产出的线上 `event_id`；`_dedupKey` 只喂 `_hasSent/_markSent`（`:199-213`）与 `_bumpOccurrence`，其字符串形状全仓零断言（b24 两条只比 keys 前后相等/清空）。而 `:157` 注释把它写成硬承诺：「键形一变就等于全部重报一遍」。

**承诺 9：无可存活注码。** 三条子句正反两腿都点到位置：会话维度 `inbox_ingress_dedup_scope_test.go:34` + hub 层时间/会话界 `repository/message_hub_core_test.go:712`；TTL 跨界 `inbox_ingress_dedup_scope_test.go:72`（睡眠写死字面量 ⇒ 摘注入即红）；先占后写退坑单条/批量双腿 `inbox_ingress_r23_dedup_release_test.go:34`、`:68` + 反向 `:113`。

**承诺 19：无可存活注码。** 两个方向各有不连库的腿：`channelgw/dup_outcome_b17_test.go:14`（PG 冲突原文钉在 `wantFalse:39`，加 `"batch handle error"` 前缀即红）与 `bridge/handler_http_ack_test.go:15`（同形表 `:43`）。结构性隐忧（非注码）：`protocol.go:341-343` 的 `"self-echo"`/`"duplicate"` 两条从真实出路够不到——生产侧一律被 `inbox_ingress.go:519`、`:983` 的 `"intercepted by middleware: "` 包一层，闭集与文案只靠两处手抄字面量对齐，没有任何测试把真产出 Reason 喂进真判定。

**共同前提**：上列 Go 腿除 `TestChannelMsgIDOf_FiltersPlaceholder` 外均经 `testutil.NewTestDBOrSkip`（`testdb.go:303`，无 PG 即 Skip），无库树上一律「绿」。

未验证：候选均未实际注码运行。
