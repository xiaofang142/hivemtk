# C 相 · lane 4 后续（先跑变异证实，再补腿）

## 铁的前提：这一批结论**还没被跑过**

上游只读审查线给了三条"该红的腿没红"的判断，但它自己写明"候选均未实际注码运行"。
本仓口径：二手结论入库前必须自己重验，且**结论必须跑出来**。所以每一步的顺序固定：

1. 先 `cp` 备份目标文件（备份放 `/tmp/lane4-bak/`，记下 md5）。
2. 精确单行注码 → `go test -run '^(点名那条腿)$'` 真跑 → **记下 rc 与 `--- PASS/FAIL/SKIP` 逐字读数**。
3. 立即 `cp` 还原并核 md5 与备份一致（**禁止 git checkout / git restore / git stash**：这些文件在共享工作树里，别的泳道有未提交改动）。
4. 只有"注码后点名腿照样绿"才算缺口成立；注码即红 ⇒ 记「不成立：这条腿本来就有牙」，**不要为凑绿改别人的期望**。

## 环境闸门（先做，做不到就别往下走）

`internal/service` 的绝大多数腿走 `testutil.NewTestDBOrSkip`：连不上 PG 时它们 **Skip 且整包绿**。
所以开工前先证明库在：

```bash
cd /Users/xiaofang/Documents/www/go/hivemtk/hivemtk/user-server
pg_isready -h localhost -p 8232    # 口令只从 .env 取，绝不打印
go test -run '^TestHandleIngress_SelfEchoDecisionPersistsNothing$' -v ./internal/service | tail -20
```

要求看到 `--- PASS`（不是 `--- SKIP`）。若是 SKIP，先按本仓既有办法把 `POSTGRES_PASSWORD` 从
`user-server/.env` 读进 `POSTGRES_TEST_PASSWORD`（用 awk 精确取键，`.env` 有重复键），
再重验。**拿不到库就停下回报，别交一条"跳过即绿"的新腿。**

## 4-A · 承诺 10：钉钉生产者的 channel_msg_id 没有腿

- 注码点：`internal/service/dingtalk_app.go:177` `"channel_msg_id": msg.MsgID,` → `"channel_msg_id": "",`
- 上游说该红却没红的腿：`TestIngress_PlatformIDEventsSkipContentDedup`（`internal/service/inbox_ingress_dedup_scope_test.go:116`）、
  `TestN14_DingTalkRepeatedTextIsNotDropped`（`internal/service/webhook_batchf2_n14_repeat_dedup_test.go:116`）
- 上游给的凭什么不红：这两条都不经过钉钉那个生产者（前者手工塞 `evt.Extra["channel_msg_id"]`，后者只数 `message_hub` 行数）；
  同类四个生产者各有直断 `Extra` 的腿（`webhook_channel_qq.go:114`←`webhook_batchf4_m01_qq_test.go:312`、
  `channelbot/core/core.go:203`←`core_test.go:73`、`qq/qq.go:664`←`qq_test.go:591`、
  `controller/wechat.go:285`←`wechat_batchf4_m01_inbound_test.go:153`），只有钉钉这格没腿。
- 若成立要补的腿：**照抄同类里最窄的那条的形状**（先读 `webhook_batchf4_m01_qq_test.go:312` 那一条怎么写，
  再写钉钉版）——从真入站 payload 构造，断 `Extra["channel_msg_id"] == msg.MsgID`（官方值原样透传、非空、不被清洗改写）。
  新腿文件放 `internal/service/`，命名跟齐 `*_test.go` 现有风格，`-run` 精确点名。

## 4-B · 承诺 18：批次路径的 self-echo 从来没被回声穿过

- 注码点：`internal/service/inbox_ingress.go:991` `if !decision.IsSelfEcho {` → `if true {`（出站回声也落库）
- 上游说的现状：单条路径有腿（`inbox_ingress_dedup_scope_test.go:333` `TestHandleIngress_SelfEchoDecisionPersistsNothing`，
  走 `svc.HandleIngressMessage`），批里那份 `handleIngressSingleForBatch` 从未被回声事件穿过；
  批里的正向腿只断"dup 判定要落库"，注码后照样成立 ⇒「单条半边绿、批里存两遍」。
- 若成立要补的腿：在批入口造一条与已记录出站内容精确回显的 inbound 事件，断
  ① `inbound` 行数仍为 0（不落库，与单条同规则）② 不排队 AI。
  **注意**：单条那条腿手工塞了 `evt.Extra["channel_msg_id"]`（`:332`）——批里的新腿必须走真回环识别路径，
  否则它验的是夹具而不是判定；参考该文件里 `r23SeedOutbound` 的用法把回环的"因"造出来。
  两半都要测（回声不落库 + dup 要落库），缺后一半就是"只在靶子上红"。

## 4-C · 承诺 19：闭集文案只靠手抄对齐

- 位置：`internal/channelgw/protocol.go:336-344` `duplicateOutcomePrefixes`（含 `"self-echo"`、`"duplicate"` 两项）
  vs 生产侧 `internal/service/inbox_ingress.go:519`、`:983` 实际包装出的 `Reason`（上游说一律带
  `"intercepted by middleware: "` 前缀）。
- 上游判断：这一格**没有可存活注码**（两个方向各有不连库的腿：`channelgw/dup_outcome_b17_test.go:14`、
  `bridge/handler_http_ack_test.go:15`），真正的隐忧是"闭集与真产出从没被同一条测试穿过"——
  把闭集里 `"self-echo"`/`"duplicate"` 删掉照样绿，因为真产出走的是另一串字面量。
- 先验证这个判断本身：注码＝从 `duplicateOutcomePrefixes` 里删掉 `"intercepted by middleware",` 这一项，
  看点名的两条腿红不红。三种结果分三种处置，**逐条写清实际看到哪一种**：
  ① 注码即红 ⇒ 不成立，`nore`，只留否决理由；
  ② 注码不红 ⇒ 缺口成立，补一条"拿真 `inbox_ingress` 产出的 Reason 字符串喂真 `IsDuplicateReason`"的腿
    （跨包：若 `internal/service` 与 `internal/channelgw` 互相 import 会成环，就用 service 侧测试断真产出串的前缀
    常量、channelgw 侧断该常量在闭集里——两侧各钉一半，中间用一个导出的常量或 golden 字符串接上，**不许复制两份字面量**）；
  ③ 结构上补不动（成环且无导出面）⇒ 记 `blocked`，**且必须写清是哪一面挡住**（具体符号 + 文件:行），
    owner 指向本泳道，不许写成"待评估"。

## 边界（越界纪律，协议 §5）

- 不许碰 `user-web/bridge/**`（4-D 的 `_dedupKey` 形状那条已单独入队，等另一条泳道改完 `channel-adapter.js` 再做，
  避免两个改动者同时写同一文件）。
- 不许碰 `internal/browser_automation/**`、`internal/repository/message_hub_inbox_outbound.go`（另有泳道在跑）。
- 只允许**单行、最小精确匹配**的注码；每处注码/还原各留一行台账（文件:行 + 注码前后 + 实际读数）。
- 若发现某条腿的断言本身写坏了（比如它其实断的是夹具塞进去的值），修它要单独说明，
  不许为了"我的注码红了"去改期望。

## 交付

一份报告：三条各给「注码 → 实际读数（逐字）→ 成立/不成立 → 若成立：新腿文件:行 + 它红过一次的真读数 + 还原后绿读数」。
新腿跑完要 `go vet ./internal/service ./internal/channelgw` + 两条包全量（`-timeout 40m`，全量才是门禁口径；
`-run` 子集只用于迭代，不许拿子集绿当结论）。
