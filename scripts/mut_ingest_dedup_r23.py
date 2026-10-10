#!/usr/bin/env python3
"""批23（§6-1/2/3 + §8.3-18 + §8.3-17 + §8.3-9(c)）入站去重那一族变异电池：全格逐格验牙。

格数不写在文案里（写死必过期：每加一档就 +1）——以 `--check` 现算的名单与末行
「N 格逐格被杀」的 N 为准。

**这一份是常驻副本**：首轮 2026-09-22 13:20–13:39 的九格是用一支 /tmp 里的一次性脚本跑的，
那支脚本已经不在这台机器上了（`grep -rl K1-redis-key-loses-conversation /tmp` 零命中），
按 §7.28 批20b 立下的口径——"/tmp 日志只是这一趟的事后誊本，不作为可复核的承诺；
常驻证据只有脚本那一份文件"——这里把九刀的锚点、注码形状与期望红名照首轮日志重建，
之后每加一档改动就往本文件加一刀
（K8/K9/K10/K11＝§8.3-17 发生次数身份，R1/R2/R3＝§8.3-9(c) 落库失败退坑，
 T1＝§6-1 的第三个子句「窗口有界」——它要求把 TTL 做成可注入字段，注释里写了为什么。
 K12/K13＝§8.3-18 那个 `if !decision.IsSelfEcho` 的另一侧：批25 实测「批次回声也落库」
 在 16 格全绿的树上千活，故 dup 侧（K4/K5）与回声侧（K12/K13）各留两刀。）

**每格的重建依据不在本文件里写死，而是两份可对抽的东西**：
`docs/superpowers/specs/ledger/logs/R23dedup/` 下首轮那九格的**逐格日志**
（`K1-redis-key-loses-conversation.log` … `K7-repo-conv-filter-ignored.log`、`L1-*.log`、
`CONTROL.log`）与常驻后的两趟全量誊本（`battery-full-8cells-resident.log` 九格、
`battery-12cells-occurrence.log` 十二格含 K8–K11）。日志里的红名与本文件 `expect` 名单
不一致时，以重新跑一趟为准——本文件的可复核形态是"任何人 `--cells K1` 就能重出现场"，
不是它自己抄一遍数字。

为什么要逐格而不是"整套腿全绿"：本批修的是一串**边界**——去重键带不带会话、回声嗅探看
不看时间界与会话界、判重之后落不落库、占坑失败退不退坑。每一句"看"都可能是"参数接住了
但没用上"，而库里看不出区别：全绿的测试集只证明这些界**在测试写下的那个形状**下成立。
所以每格拆一处，且**同一个界的两层各拆一刀**：

- K2（调用方把窗口折成零时刻）与 K6（仓储侧不看 `since` 参数）打的是同一条时间界。
  只拆调用方，就看不出仓储里那条 `sent_at > ?` 有没有人守；只拆仓储，就看不出调用方
  是不是真的传了窗口进来。任一层"看起来另一层会兜住"，都是这条界今天没有的保险。
- K3（调用方传空串）与 K7（仓储侧不接会话参数）同理，打的是同一条会话界的两层。
  这里要额外记一笔形状：仓储侧那两处 `if conversationID != "" { q = q.Where(...) }`
  **文字完全相同、在同文件里出现三次**（GetByContentHash / GetByPlatformContent /
  GetByPlatformContentNormalized），锚点必须带上各自那行 `Where(...)` SQL 才命中一次。
- K4/K5 打的是同一条规则的两条路径（单条 / 批次）。批次路径有自己的一份 `Blocked` 分支，
  漏一边＝批里的重复无痕，而单条那边照样绿。
- K1 打 Redis 键、L1 打"带平台 id 就跳过内容去重"那半句守卫。L1 这一刀的存在理由记在
  `TestIngress_PlatformIDEventsSkipContentDedup` 的注释里：把"拦截即销毁"改成"重复留痕"之后，
  N-14 原来那三条腿数的是"库里有没有行"，摘掉守卫它们照样绿——必须有一条直接断言判定本身的腿，
  而这条腿又必须有格子来证明它真的有牙。
- K8/K9 打 §8.3-17 同一条豁免的**两个消费点**（入口窗口 + 落库钩子2.5）：只在入口摘半句，
  落库那边还会把第二条同文本吞成一行；只在落库摘，入口就先把第二条拦在库外。两刀各红各的腿。
  K10/K11 打判据本体：恒真＝内容维度嗅探对整个入站流失效（外来 id 也不再被内容兜住），
  过宽＝只看 `#数字` 结尾，任何以它收尾的外来 event_id 都能蹭豁免。
- R1/R2/R3 打 §8.3-9(c) 那笔补偿：R1/R2 是"两条路径各一刀"（与 K4/K5 同形），
  R3 是**反方向**的一刀——把"只在失败时退坑"改成"无条件退坑"。少了 R3，"成功不许退坑"
  那条腿就成了没有格子的断言；而它守的正是"同内容连发两帧只回一遍 AI"，
  把补偿写成 defer／提前删键这类"看起来更保险"的改法都会撞上它。

口径（沿用批16/17/18/19x/20b/20c/20d/20f/22 电池）：
- 控制组必须 rc==0、total>0、skip==0、且不许有任何红名；**total 放刀前现测，不写死常量**
  （共享工作树下别的泳道随时往同一批包里加用例：本族实测 12 → 18 → 21 → 22，写死就把别人的用例算成跑断）；
- 每格断言 PASS+FAIL==控制组数（一条用例 panic 会带走整个二进制，"FAIL=1"看着像杀其实没跑完）；
- BUILD FAILED / panic / 红而没点名单里那条腿 一律判 BROKEN 或"红了但没点出"，**不计入杀掉**；
- 锚点命中必须恰好一次，注码先过 gofmt -e；只在私有 --shared 克隆里注码，绝不碰共享工作树；
  还原后逐文件比 md5。

已知**未覆盖**的格子（写在这里而不是悄悄不留痕）：
- `GetByPlatformContentNormalized`（归一化那条）的时间界与会话界**没有各自的格子**：
  K6/K7 的注码打在 `GetByPlatformContent` 上，而归一化那条是独立的一份 SQL。
  它由 `TestMessageHubRepository_ContentEchoScoping` 的第三条断言（跨会话归一化不得命中）
  与 `TestPersistBridgeHistory_ContentDedupStillHooksFreshEcho`（窗口内归一化回显必须继续被吞）
  两腿守着——两腿都有真断言，只是本电池没有专门把它的 WHERE 摘掉来验牙。
  补法与 K6/K7 同形，登记为下一刀的候选而不是今天已覆盖。
- `ListRecentOutboundInConv` 的窗口（`inbox_ingress_ingest.go` 里"近期出站归一化回声"那段）
  不受本电池管辖：它红的时候需要"同一会话里先有一条出站行"，而 K2/K6/K7 打的都是 hub 层
  钩子2.5 那三条路。那一格由 §7.15（回环嗅探）那批的腿管。
- 内容窗口键的 TTL 那一格**本轮补上了**（T1）：原先 `InboxContentDedupTTL` 是常量，改大改小
  不会让任何腿红（用例都在窗口内跑）。修法是把窗口长度做成服务上的可注入字段，注入秒级值后
  「到期必须放行」才有腿可断言，T1 再把「注入值不生效」注进去证明那条腿有牙。
  这一格首轮（run3）判的是 **存活＝洞**，原因不在产码而在用例：`TestIngress_ContentDedupWindowExpires`
  的等待时长与失败消息都 `svc.contentDedupWindow()` 读回窗口，于是 T1 把同一个函数改成常量后，
  用例跟着改口径、只是跑得慢一点，永远不红（断言与变异读同一个来源＝变异自洽）。run4 把注入值
  钉成用例里的字面量 `const injectedTTL = 2s` 后这一格才杀掉。⇒ 写"可注入参数"类断言时，
  期望值必须是**用例自己写死的字面量**，否则这条腿只是在复述被注入的那个函数。
  注进去的形状只有这一种：`contentDedupWindow()` 的 `> 0` 门决定了"注入 0"取默认值，
  所以"注 0 ⇒ 永不过期"这种在真 Redis 上会犯的错，在这份代码里走不到（不是没测，是构造上不可达）。
- §8.3-9(c) 补偿的**失败半边没有格子**：`releaseInboundDedup` 里 `cache.Delete` 报错时只记一条
  Warn、靠 TTL 自愈，那一支要注码得换一个"删键必失败"的缓存实现（测试用的是 MemoryCache，
  它的 Delete 永不报错）。登记为已知未覆盖，而不是"R1–R3 覆盖了整条补偿"。

用法：python3 scripts/mut_ingest_dedup_r23.py [--keep] [--clone DIR] [--check] [--cells K1,R1,...]

--cells 是**取证侧的分包**，不改判据：格数 × 每格一次全量跑在单次 10 分钟命令预算里可能跑不完
（首轮九格就是分趟跑的；12 格那趟实测 ~13 分钟，放后台跑完），拆趟时**每趟各自重建克隆、
各自重测控制组**，各趟都必须"逐格被杀"才算全杀；报告里要写清是分几趟跑的，而不是"一轮全杀"。
"""
from __future__ import annotations

import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path
from mut_dispose import dispose, dispose_at_exit, leave_for_evidence, workdir

# 脚本在 <repo>/scripts/ 下 ⇒ 根 = 上一级。**不硬编码仓名**（改名克隆必须照样能跑）。
ROOT = Path(__file__).resolve().parent.parent

INGEST_REL = Path("user-server/internal/service/inbox_ingress_ingest.go")
PERSIST_REL = Path("user-server/internal/service/inbox_ingress_persist.go")
SVC_REL = Path("user-server/internal/service/inbox_ingress.go")
HUBREL_REL = Path("user-server/internal/repository/message_hub_inbox.go")
# 槽名 → 仓内相对路径。`--check`（工作树）与整跑（克隆）此前各写一份同样的字典，
# 漂了只会以"某一趟锚点没命中"的形式暴露；合成一份，顺带让
# scripts/anchor-preflight.py 也能按它在**工作树**上预验锚点。
SLOT_FILES = {"ingest": INGEST_REL, "persist": PERSIST_REL,
              "svc": SVC_REL, "hubrepo": HUBREL_REL}
# 脏文件按目录取，不手写清单（漏一个用新签名的文件 = 克隆里 [build failed]，电池整个失声）；
# **未跟踪**文件例外，见 OWN_UNTRACKED 与 lane_overlays() 的不对称理由。
LANE_PATHS = ["user-server/internal/service",
              "user-server/internal/repository",
              "user-server/internal/model"]
# 本泳道自己新建、还没进 git 的文件（`git status --porcelain -uall` 的 `??` 行里属于我的那些）。
# 判据＝这个改动是我这一批写的、且它的腿在本电池的 GO_RUN/包编译里；不符合的一律不装。
OWN_UNTRACKED = [
    "user-server/internal/service/inbox_ingress_dedup_scope_test.go",
    "user-server/internal/service/inbox_ingress_batch_echo_test.go",
    "user-server/internal/service/inbox_ingress_r23_occurrence_test.go",
    "user-server/internal/service/inbox_ingress_r23_dedup_release_test.go",
    "user-server/internal/repository/message_hub_outbound_push_cap_b20d_test.go",
]

GO_PKGS = ["./internal/service/", "./internal/repository/"]
# 名单是一条跨两包的 `-run` 正则（本族全部腿）：少一条腿就叫不出"哪一格杀了谁"，
# 多一条腿就会把无关用例的红算成牙。名单与 `go test -list` 的差集由控制组把关：
# 名单里写了不存在的名字 ⇒ 那条腿永远不会红 ⇒ expect 对不上时判"红了但没点出"之前先看这里。
GO_RUN = ("TestIngress_ContentDedupKeyCarriesConversation"
          "|TestIngress_ContentDedupWindowExpires"
          "|TestIngress_PlatformIDEventsSkipContentDedup"
          "|TestIngress_OccurrenceSuffixSkipsContentWindowDedup"
          "|TestIngress_PlainContentHashStillContentDeduped"
          "|TestIngress_NonOccurrenceHashLikeIDSuffixStillContentDeduped"
          "|TestPersistBridgeHistory_ContentDedupExpiresAfterWindow"
          "|TestPersistBridgeHistory_ContentDedupStillHooksFreshEcho"
          "|TestPersistBridgeHistory_ContentDedupScopedToConversation"
          "|TestPersistBridgeHistory_OccurrenceSuffixPersistsSecondRow"
          "|TestPersistBridgeHistory_OccurrenceSuffixStillExactIdempotent"
          "|TestPersistBridgeHistory_ForeignIDStillCaughtByContentHash"
          "|TestHandleIngress_DupDecisionPersistsWithAISuppressed"
          "|TestHandleIngressBatch_DupDecisionPersistsWithAISuppressed"
          "|TestHandleIngress_SelfEchoDecisionPersistsNothing"
          "|TestHandleIngressBatch_SelfEchoPersistsNothingAndDupPersists"
          "|TestHandleIngress_PersistFailureReleasesContentWindow"
          "|TestHandleIngressBatch_PersistFailureReleasesContentWindow"
          "|TestHandleIngress_PersistSuccessKeepsContentWindow"
          "|TestMessageHubRepository_ContentEchoScoping"
          "|TestMessageHubRepository_GetByPlatformContent"
          "|TestMessageHubRepository_GetByContentHash_Empty")

# 期望被杀的腿名（简称常量，避免手抖打错整串）
S_KEY_CONV = "TestIngress_ContentDedupKeyCarriesConversation"
S_PLATID = "TestIngress_PlatformIDEventsSkipContentDedup"
O_OCC = "TestIngress_OccurrenceSuffixSkipsContentWindowDedup"
O_PLAIN = "TestIngress_PlainContentHashStillContentDeduped"
O_SHAPE = "TestIngress_NonOccurrenceHashLikeIDSuffixStillContentDeduped"
P_OCC_ROW = "TestPersistBridgeHistory_OccurrenceSuffixPersistsSecondRow"
P_EXACT = "TestPersistBridgeHistory_OccurrenceSuffixStillExactIdempotent"
P_FOREIGN = "TestPersistBridgeHistory_ForeignIDStillCaughtByContentHash"
P_EXPIRED = "TestPersistBridgeHistory_ContentDedupExpiresAfterWindow"
P_SCOPED = "TestPersistBridgeHistory_ContentDedupScopedToConversation"
H_SINGLE = "TestHandleIngress_DupDecisionPersistsWithAISuppressed"
H_BATCH = "TestHandleIngressBatch_DupDecisionPersistsWithAISuppressed"
H_ECHO_SINGLE = "TestHandleIngress_SelfEchoDecisionPersistsNothing"
H_ECHO_BATCH = "TestHandleIngressBatch_SelfEchoPersistsNothingAndDupPersists"
R_ECHO = "TestMessageHubRepository_ContentEchoScoping"
RL_SINGLE = "TestHandleIngress_PersistFailureReleasesContentWindow"
RL_BATCH = "TestHandleIngressBatch_PersistFailureReleasesContentWindow"
RL_KEEP = "TestHandleIngress_PersistSuccessKeepsContentWindow"
T_WINDOW = "TestIngress_ContentDedupWindowExpires"

ANSI = re.compile(r"\x1b\[[0-9;]*m")
CONTROL = {"go": 0}

# ---------------------------------------------------------------- 锚点（取完整语句含缩进）
# §6-1 入口那把 Redis 键
A_KEY_CONV = '\t\tdupKey := InboxSenderContentDedupKey + dedupHash + ":" + event.ConversationID\n'
A_KEY_CONV_NOCONV = "\t\tdupKey := InboxSenderContentDedupKey + dedupHash\n"
# §6-3 「平台给了 id 就别再按内容拦」那半句守卫 + §8.3-17「上报方给了发生次数身份就别按内容拦」。
# 锚点随工作树更新：这两条守卫是叠在同一行 `if` 上的，条件少任一个都测不出另一条有没有牙。
A_GUARD = ('\tif s.cache != nil && chanMsgID == "" && '
           '!eventAssertsDistinctMessage(event.EventID) {\n')
A_GUARD_NOPLATID = "\tif s.cache != nil {\n"
# §8.3-17 入口半边：只摘「发生次数身份」那半句，保留平台 id 守卫（否则与 L1 同族、看不出各层）
A_GUARD_OCC = '\tif s.cache != nil && chanMsgID == "" {\n'
# §8.3-17 落库半边：钩子2.5 的入口条件
A_H25 = ('	if s.hubRepo != nil && event.Content != "" && event.Channel != "" && '
         "!eventAssertsDistinctMessage(event.EventID) {\n")
A_H25_NOOCC = "\tif s.hubRepo != nil && event.Content != \"\" && event.Channel != \"\" {\n"
# §8.3-17 判据本体：过宽（内容嗅探对整个入站流失效）与形状放宽（外来 id 蹭豁免）
A_PRED = "\treturn occurrenceMsgIDRe.MatchString(eventID)\n"
A_PRED_ALWAYS = "\treturn true\n"
A_SHAPE = ("var occurrenceMsgIDRe = regexp.MustCompile(`^mh:[0-9a-f]{8}#[1-9][0-9]*$`)\n")
A_SHAPE_LOOSE = "var occurrenceMsgIDRe = regexp.MustCompile(`#[1-9][0-9]*$`)\n"
# §8.3-9(c) 落库失败退坑：两条路径各一刀（少一边＝另一边的补偿没人守），
# 再加一刀把「只在失败时退」改成「无条件退」——那一刀只有"成功不许退坑"那条腿会红。
A_REL_SINGLE = ("\t\t// §8.3-9(c)：入口那把内容窗口键是「先占后写」的，写失败就得把坑退回去 ——\n"
                "\t\t// 否则扩展按重投队列原样再发一次时，会被自己留下的键判成 duplicate，只剩留痕不回 AI。\n"
                "\t\ts.releaseInboundDedup(ctx, decision)\n")
A_REL_SINGLE_NOREL = A_REL_SINGLE.rsplit("\n", 2)[0] + "\n"
A_REL_BATCH = ("\t\t// 与单条路径同一条补偿（§8.3-9(c)）：批次里失败的那一条如果不退坑，\n"
               "\t\t// 同一会话后面每一条同内容重投都会被它毒掉。\n"
               "\t\ts.releaseInboundDedup(ctx, decision)\n")
A_REL_BATCH_NOREL = A_REL_BATCH.rsplit("\n", 2)[0] + "\n"
A_REL_IF_SINGLE = "\tif err := s.persistMessage(ctx, event); err != nil {\n" + A_REL_SINGLE
A_REL_UNCOND = ("\ts.releaseInboundDedup(ctx, decision) // 变异：补偿写成无条件（成功也退坑）\n"
                "\tif err := s.persistMessage(ctx, event); err != nil {\n")
# §6-1 有界性的第三刀：窗口长度本身。默认 5min 没有任何腿跨得过，所以 TTL 做成了可注入字段；
# 这一格把「注入值不生效」注进去，红的正是那条跨界的腿（`TestIngress_ContentDedupWindowExpires`）。
# 默认值那一支在「锁与幂等 TTL 入库」之后从包级常量换成了 `inboxContentDedupTTL()`
# （provider 有值用 provider，否则回落 `InboxContentDedupTTL`），锚点跟着搬，期望不变：
# 这一刀破坏的仍是"注入的窗口长度进不了实际存活时间"这一维，不是默认值从哪来。
A_TTL = ("func (s *InboxIngressService) contentDedupWindow() time.Duration {\n"
         "\tif s.contentDedupTTL > 0 {\n\t\treturn s.contentDedupTTL\n\t}\n"
         "\treturn inboxContentDedupTTL()\n}\n")
A_TTL_IGNORED = ("func (s *InboxIngressService) contentDedupWindow() time.Duration {\n"
                 "\treturn inboxContentDedupTTL() // 变异：注入的窗口长度不生效（TTL 这一维无从断言）\n}\n")
# §6-2 时间界：调用方那一层
A_ECHOSINCE = "\t\techoSince := time.Now().Add(-InboxOutboundEchoWindow)\n"
A_ECHOSINCE_ZERO = "\t\techoSince := time.Time{} // 变异：不看回声窗口，历史里多老的行都算回声\n"
# §6-1 会话界：调用方那一层
A_CALLER_CONV = ("if existing, err := s.hubRepo.GetByPlatformContent(ctx, event.Channel, "
                 "event.Content, event.ConversationID, echoSince); err == nil")
A_CALLER_CONV_BLANK = ("if existing, err := s.hubRepo.GetByPlatformContent(ctx, event.Channel, "
                       "event.Content, \"\", echoSince); err == nil")
# §8.3-18 重复留痕：单条 / 批次两条路径各一刀（锚点带上各自那句注释才命中一次）
A_SINGLE = ("\t\t// 自己的出站回声——那条在库里已经有本体，再落一行就是把自己的话存两遍。\n"
            "\t\tif !decision.IsSelfEcho {\n")
A_SINGLE_DESTROY_DUP = ("\t\t// 自己的出站回声——那条在库里已经有本体，再落一行就是把自己的话存两遍。\n"
                        "\t\tif !decision.IsSelfEcho && !decision.IsDup {\n")
A_BATCH = ("\t\t// 与单条路径同一条规则：dup 判定要落库留痕、只压 AI；什么都不落的只有自己的出站回声。\n"
           "\t\tif !decision.IsSelfEcho {\n")
A_BATCH_DESTROY_DUP = ("\t\t// 与单条路径同一条规则：dup 判定要落库留痕、只压 AI；什么都不落的只有自己的出站回声。\n"
                       "\t\tif !decision.IsSelfEcho && !decision.IsDup {\n")
# §8.3-18 的**反向**半边：K4/K5 打的是「连 dup 都不落」（过度抑制），这一对打「回声也落」
# （欠抑制＝同一句 AI 话术在会话里存两遍）。同一个 `if` 的两侧各要一刀：只留一侧时，
# 另一侧的写法可以随便改都不红（本轮实测：批次回声半边整包注码存活，见 R22-39）。
A_SINGLE_ECHO_TWICE = ("\t\t// 自己的出站回声——那条在库里已经有本体，再落一行就是把自己的话存两遍。\n"
                       "\t\tif true {\n")
A_BATCH_ECHO_TWICE = ("\t\t// 与单条路径同一条规则：dup 判定要落库留痕、只压 AI；什么都不落的只有自己的出站回声。\n"
                      "\t\tif true {\n")
# §6-2 时间界：仓储那一层（锚点必须带上 GetByPlatformContent 那行 SQL，否则三处同款命中 3 次）
A_REPO_WINDOW = ('\t\tWhere("platform = ? AND direction = \'outbound\' AND md5(content) = md5(?)'
                 " AND sent_at > ?\", platform, content, since)\n")
A_REPO_WINDOW_IGN = ('\t\tWhere("platform = ? AND direction = \'outbound\' AND md5(content) = md5(?)'
                     '", platform, content)\n')
# §6-1 会话界：仓储那一层
A_REPO_CONV = (A_REPO_WINDOW
               + '\tif conversationID != "" {\n'
                 '\t\tq = q.Where("conversation_id = ?", conversationID)\n'
                 "\t}\n")
A_REPO_CONV_IGN = (A_REPO_WINDOW
                   + '\tif conversationID != "" && false {\n'
                     '\t\tq = q.Where("conversation_id = ?", conversationID)\n'
                     "\t}\n")


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def verdict(r: dict, expect_total: int) -> str:
    """三态判定：杀掉 / 存活=洞 / BROKEN（红了但判不了）。"""
    if r["rc"] == 0 and not r["killed"]:
        return "存活=洞"
    if r.get("panicked") or r.get("buildfailed") or not r["killed"]:
        return "BROKEN=判不了"
    if r["total"] != expect_total or r["failed"] < 1 or r["skipped"] > 0:
        return "BROKEN=判不了"
    return "杀掉"


def dup_report(kills: dict) -> None:
    seen = {}
    for code, names in kills.items():
        key = tuple(sorted(names))
        seen.setdefault(key, []).append(code)
    for key, codes in seen.items():
        if len(codes) > 1 and key:
            print("[Go] 同族（同一批用例被多格杀掉，判据可能重叠）：" + "≈".join(codes)
                  + f" → {' '.join(sorted(key))[:120]}")


def lane_overlays() -> list[str]:
    """要装进克隆的本泳道文件清单。

    已跟踪的脏文件**按目录整片收**（漏一个用了新签名的文件 = 克隆里 [build failed]，电池整个失声）；
    未跟踪文件里，**只有"外域的 `_test.go`"**不收，见下面的不对称理由。

    为什么只排外域未跟踪**测试**文件：并行泳道按 TDD 新写的测试文件（未跟踪、引用还没落地的
    符号，2026-09-22 17:34 的 `internal/repository/bill_test.go` 即为一例）装进克隆只会让那一包
    `[build failed]`，控制组停机——那既不是本批的错，也修不了（要等对方落实现）。本电池的腿全在
    点名的 `GO_RUN` 名单里，少装别人的测试文件不会少掉任何一条自己的腿，也不可能造成假绿。

    为什么外域未跟踪**实现**文件必须装：2026-09-23 00:14 那趟 16 格全量就是死在把它一起排除了——
    按 TDD 写的实现文件是未跟踪的，而同一泳道**已跟踪**的 `integration.go` 已经改成引用里面的
    `OrderPaymentSink` / `OrderWebhookResult` 等符号；只装已跟踪的那一半、把实现那一半留在门外，
    克隆面就成了"半份改动"，控制组 [build failed]（同一棵树 `go vet ./internal/service/ ./internal/repository/`
    rc=0，证明坏的是克隆的文件面而不是代码）。规则因此改成按**扩展名**分，而不是按"有没有跟踪"分。

    少装自己的文件同样不会假绿：新腿没装进克隆 ⇒ 要么 build 报缺符号，要么该腿的 expect 红名对不上，
    两种都是响的。`OWN_UNTRACKED` 名单里的文件不见了直接停机（说明名单该更新了，而不是悄悄少跑几条腿）。
    """
    r = subprocess.run(["git", "-C", str(ROOT), "status", "--porcelain", "-uall", "--"] + LANE_PATHS,
                       capture_output=True, text=True, timeout=300)
    if r.returncode != 0:
        raise SystemExit("git status 失败，拿不到本泳道脏文件清单：" + r.stderr[-200:])
    out, skipped = [], []
    for line in r.stdout.splitlines():
        state, p = line[:2], line[3:].split(" -> ")[-1].strip().strip('"')
        if not p.endswith(".go"):
            continue
        if state == "??" and p.endswith("_test.go") and p not in OWN_UNTRACKED:
            skipped.append(p)
            continue
        out.append(p)
    for p in OWN_UNTRACKED:
        if not (ROOT / p).exists():
            raise SystemExit(f"OWN_UNTRACKED 点名的文件已不在工作树：{p}（名单要跟着改动走）")
    if skipped:
        print("[Go] 外域未跟踪的**测试**文件不进克隆（其实现文件与已跟踪改动一起装，见 lane_overlays 注释）："
              + " ".join(skipped))
    if not out:
        raise SystemExit("脏文件清单为空——克隆里跑的是 HEAD，测不到本批改动（宁可停机也别假绿）")
    return out


def syntax_ok_go(src: str) -> tuple[bool, str]:
    p = subprocess.run(["gofmt", "-e"], input=src, capture_output=True, text=True, timeout=120)
    return p.returncode == 0, p.stderr.strip()


def go_prepare(dst: Path, owned: bool = False) -> Path:

    def bail(msg: str) -> None:
        """克隆已经建起来之后的中止路：先回收私有克隆，再出声。

        收尾闸原先只接在 `main()` 的出口上，装架函数里克隆之后的每一条 raise 都把整份
        私有克隆留在临时目录（一轮 50–70MB，而磁盘常态 98% 满）。2026-09-28 在
        `mut_bill_p701.py` 上实测一次 DIRTY 停机留 72M，这一族按同一形状补齐。
        三条**不**走这里："已存在"（那份 clone/ 不是本电池建的）、"克隆失败"（目录归属
        还没定）、"md5 不一致"（"覆盖后还是不对"的字节只活在克隆里，删了就只剩一句
        "当时红过"——与 `main()` 侧还原校验同一取舍）。
        """
        if (dst / "clone").exists():
            dispose(dst, owned=owned, keep=False, repo_root=ROOT)
        raise SystemExit(msg)
    clone = dst / "clone"
    if clone.exists():
        raise SystemExit(f"{clone} 已存在（换 --clone 目录或先删）")
    r = subprocess.run(["git", "clone", "--shared", "--no-checkout", str(ROOT), str(clone)],
                       capture_output=True, text=True, timeout=900)
    if r.returncode != 0:
        raise SystemExit("克隆失败：" + (r.stdout + r.stderr)[-400:])
    # 分支从**工作树**读，不从克隆读：--no-checkout 的克隆里 HEAD 是未 born 的符号引用，
    # `branch --show-current` 可能给空串 ⇒ 退回 master，而本仓当前分支未必是 master。
    b = subprocess.run(["git", "-C", str(ROOT), "branch", "--show-current"],
                       capture_output=True, text=True, timeout=60)
    branch = b.stdout.strip() or "master"
    c = subprocess.run(["git", "checkout", "-f", branch], cwd=clone,
                       capture_output=True, text=True, timeout=900)
    if c.returncode != 0:
        bail("checkout 失败：" + (c.stdout + c.stderr)[-400:])
    for rel in lane_overlays():
        src = ROOT / rel
        if not src.exists():
            bail(f"覆盖源缺失：{src}")
        tgt = clone / rel
        tgt.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, tgt)
        if md5_bytes(src) != md5_bytes(tgt):
            leave_for_evidence("还原后 md5 不一致：现场只活在克隆里")
            raise SystemExit(f"覆盖后 md5 不一致（装错树/写盘失败）：{rel}")
    # .env 不进 git ⇒ 克隆里没有则依赖 DB 的用例会 skip，控制组就不干净（skip==0 是硬门）。
    hostenv = ROOT / "user-server" / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / "user-server" / ".env")
    return clone


def go_run(clone: Path) -> dict:
    root = clone / "user-server"
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOCACHE", "/tmp/gocache-r23dedup")
    env.setdefault("POSTGRES_TEST_PORT", "8232")
    envf = root / ".env"
    if envf.exists():
        for line in read(envf).splitlines():
            if line.startswith("POSTGRES_PASSWORD=") and "POSTGRES_TEST_PASSWORD" not in env:
                env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
    p = subprocess.run(["go", "test", "-p", "1", "-count=1", "-v"] + GO_PKGS + ["-run", GO_RUN],
                       cwd=root, capture_output=True, text=True, timeout=2400, env=env)
    out = ANSI.sub("", p.stdout + p.stderr)
    killed = sorted(set(re.findall(r"^    --- FAIL: (\S+)", out, re.M)) |
                    set(re.findall(r"^--- FAIL: (\S+)", out, re.M)))
    passed = len(re.findall(r"^--- PASS: ", out, re.M))
    failed = len(re.findall(r"^--- FAIL: ", out, re.M))
    skipped = len(re.findall(r"^--- SKIP: ", out, re.M))
    return {"rc": p.returncode, "killed": killed, "total": passed + failed + skipped,
            "passed": passed, "failed": failed, "skipped": skipped, "out": out,
            "panicked": bool(re.search(r"^panic: |^fatal error: ", out, re.M)),
            "buildfailed": "[build failed]" in out or "cannot use" in out or "undefined:" in out}


def cells() -> list[tuple[str, str, list[tuple[str, str, str]], tuple[str, ...]]]:
    """(格, 说明, [(文件槽, 原文, 注码), ...], 该红的腿)"""
    return [
        # ---- §6-1 入口那把键：带不带会话
        ("K1", "Redis 去重键丢掉会话（换会话的同一句话被共用一把键，客户「说了没回」）",
         [("ingest", A_KEY_CONV, A_KEY_CONV_NOCONV)], (S_KEY_CONV,)),
        # ---- §6-2 时间界：两层各一刀
        ("K2", "调用方把回声窗口折成零时刻（历史里多老的行都算回声）",
         [("persist", A_ECHOSINCE, A_ECHOSINCE_ZERO)], (P_EXPIRED,)),
        ("K6", "仓储侧不看 since 参数（调用方传了窗口，SQL 里那条 `sent_at > ?` 其实没人守）",
         [("hubrepo", A_REPO_WINDOW, A_REPO_WINDOW_IGN)], (P_EXPIRED, R_ECHO)),
        # ---- §6-1 会话界：两层各一刀
        ("K3", "调用方给 hub 层传空会话（会话界在这一跳被丢掉）",
         [("persist", A_CALLER_CONV, A_CALLER_CONV_BLANK)], (P_SCOPED,)),
        ("K7", "仓储侧不接会话参数（跨会话同文本互吞）",
         [("hubrepo", A_REPO_CONV, A_REPO_CONV_IGN)], (P_SCOPED, R_ECHO)),
        # ---- §8.3-18 重复留痕：两条路径各一刀
        ("K4", "单条路径退回「拦截即销毁」（判重的那句话在库里查不到任何痕迹）",
         [("svc", A_SINGLE, A_SINGLE_DESTROY_DUP)], (H_SINGLE,)),
        ("K5", "批次路径退回「拦截即销毁」（单条半边照样绿，批里的重复无痕）",
         [("svc", A_BATCH, A_BATCH_DESTROY_DUP)], (H_BATCH,)),
        # ---- §8.3-18 同一个 `if` 的另一侧：回声那一支不许落库，两条路径各一刀
        ("K12", "单条路径把出站回声也落库（同一句 AI 话术在会话里存两遍）",
         [("svc", A_SINGLE, A_SINGLE_ECHO_TWICE)], (H_ECHO_SINGLE,)),
        ("K13", "批次路径把出站回声也落库（单条半边照样绿，批里存两遍＝本轮实测的存活格）",
         [("svc", A_BATCH, A_BATCH_ECHO_TWICE)], (H_ECHO_BATCH,)),
        # ---- §6-3 优先级：带平台 id 的事件不得再走内容窗口去重
        ("L1", "摘掉「平台给了 id 就跳过内容去重」那半句守卫",
         [("ingest", A_GUARD, A_GUARD_NOPLATID)], (S_PLATID,)),
        # ---- §8.3-17 发生次数身份：两处消费点各一刀（只拆判据本体会看不出哪一处没接上）
        ("K8", "入口只摘「发生次数身份」那半句（平台 id 守卫留着）⇒ 第二条又在入库前被吞",
         [("ingest", A_GUARD, A_GUARD_OCC)], (O_OCC,)),
        ("K9", "落库只摘钩子2.5 的豁免那半句 ⇒ 内容哈希又把第二条同文本吞掉",
         [("persist", A_H25, A_H25_NOOCC)], (P_OCC_ROW,)),
        ("K10", "豁免判据恒真（内容维度嗅探对整个入站流失效：回环防护与重投兜底一起没）",
         [("ingest", A_PRED, A_PRED_ALWAYS)], (P_FOREIGN, O_PLAIN, O_SHAPE)),
        ("K11", "豁免只看「以 #数字 结尾」这一截（丢掉 mh:<8hex> 前缀界，外来 id 蹭豁免）",
         [("ingest", A_SHAPE, A_SHAPE_LOOSE)], (O_SHAPE,)),
        # ---- §8.3-9(c) 占坑之后落库失败要退坑：两条路径各一刀 + 一刀打"补偿写过头"
        ("R1", "单条路径不退坑（那条重投回来会被自己留下的键判成 duplicate：只留痕不回 AI）",
         [("svc", A_REL_SINGLE, A_REL_SINGLE_NOREL)], (RL_SINGLE,)),
        ("R2", "批次路径不退坑（单条半边照样绿，批里失败的那条毒掉后面每一次重投）",
         [("svc", A_REL_BATCH, A_REL_BATCH_NOREL)], (RL_BATCH,)),
        ("R3", "补偿写成无条件退坑（落库成功也把键删掉，同内容连发两帧会回两遍 AI）",
         [("svc", A_REL_IF_SINGLE, A_REL_UNCOND)], (RL_KEEP,)),
        # ---- §6-1 有界性：窗口长度这一维（TTL 做成字段之后才有这一格）
        ("T1", "注入的窗口长度不生效（TTL 这一维退回常量，跨界那条腿就成了没人守的断言）",
         [("svc", A_TTL, A_TTL_IGNORED)], (T_WINDOW,)),
    ]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    ap.add_argument("--check", action="store_true",
                    help="只做注码前置（锚点命中一次 + 语法可解析），不跑测试、不建克隆")
    ap.add_argument("--cells", default="", help="逗号分隔的格名；留空=全跑")
    args = ap.parse_args()
    want = [c.strip().upper() for c in args.cells.split(",") if c.strip()]
    unknown = [c for c in want if c not in {d[0] for d in cells()}]
    if unknown:
        raise SystemExit("--cells 里有未知格名：" + " ".join(unknown) + "（宁可停机也别悄悄少跑几格）")

    def prepared_cells():
        acc_cells = []
        for code, desc, edits, expect in cells():
            if want and code not in want:
                continue
            acc: dict[str, str] = {}
            for slot, old, new in edits:
                cur = acc.get(slot, originals[slot])
                hit = cur.count(old)
                if hit != 1:
                    raise SystemExit(f"{code} 锚点在 {slot} 里命中 {hit} 次（要求恰好 1）：{old[:70]!r}")
                if old == new:
                    raise SystemExit(f"{code} 注码无效（原文与注码后一致）")
                acc[slot] = cur.replace(old, new, 1)
            for slot, src in acc.items():
                if src == originals[slot]:
                    raise SystemExit(f"{code} 注码无效（{slot} 替换后与原文件一致）")
                ok, err = syntax_ok_go(src)
                if not ok:
                    raise SystemExit(f"{code} 注码语法坏，跑出来只会是 build failed：{err[:160]}")
            acc_cells.append((code, desc, acc, expect))
        return acc_cells

    if args.check:
        files = {slot: ROOT / rel for slot, rel in SLOT_FILES.items()}
        originals = {name: read(p) for name, p in files.items()}
        got = prepared_cells()
        print(f"[Go] 注码前置（对**当前工作树**，不跑测试）：{len(got)}/{len(cells())} 格锚点各命中一次"
              " + 注码后语法可解析")
        for code, desc, _, _ in got:
            print(f"  {code:<3} {desc}")
        return 0

    tmp, owned = workdir(args.clone or None, prefix="r23dedup-", repo_root=ROOT)
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}", flush=True)
    if want:
        print(f"[Go] 本趟只跑 {len(want)} 格：{' '.join(want)}", flush=True)
    problems: list[str] = []

    clone = go_prepare(tmp, owned)
    dispose_at_exit(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
    files = {slot: clone / rel for slot, rel in SLOT_FILES.items()}
    for name, p in files.items():
        if not p.exists():
            raise SystemExit(f"注码目标文件不在克隆里：{p}")
    originals = {name: read(p) for name, p in files.items()}
    basemd5 = {name: md5_bytes(p) for name, p in files.items()}

    prepared = prepared_cells()
    print(f"\n[Go] 注码前置：{len(prepared)} 格锚点各命中一次 + 注码后语法可解析")

    r = go_run(clone)
    CONTROL["go"] = r["total"]
    print(f"[Go] 控制组 rc={r['rc']} total={r['total']} passed={r['passed']} "
          f"skip={r['skipped']} FAIL={r['killed']}")
    if r["rc"] != 0 or r["total"] == 0 or r["skipped"] > 0 or r["killed"]:
        # build failed 的因在输出**最前面**（`undefined: X` 那批），用 tail 摘会把唯一变了的那行截掉；
        # 而且红因要写成"克隆的文件面"，别让它看起来像本批注入出来的错。
        print(r["out"][:6000] if r["buildfailed"] else r["out"][-4000:])
        if r["buildfailed"]:
            raise SystemExit("[Go] 控制组 build failed——克隆面编译不过，先核对 lane_overlays() 装了哪些文件"
                             "（同一棵工作树 `go vet` 绿 ⇒ 坏的是文件面不是代码）")
        raise SystemExit("[Go] 控制组不干净——后面所有红/绿都不可信")

    gkill: dict[str, set] = {}
    for code, desc, acc, expect in prepared:
        for slot, src in acc.items():
            files[slot].write_text(src, encoding="utf-8")
        for slot in acc:
            if md5_bytes(files[slot]) == basemd5[slot]:
                raise SystemExit(f"{code} 注码未生效（{slot} 与原内容一致）")
        r = go_run(clone)
        v = verdict(r, CONTROL["go"])
        if v == "杀掉" and not all(e in r["killed"] for e in expect):
            miss = [e for e in expect if e not in r["killed"]]
            v = f"红了但没点出 {' '.join(miss)}"
        print(f"{code:<4} {desc[:58]:<60} {v:<7} total={r['total']} fail={r['failed']} "
              f"skip={r['skipped']} ｜ " + " | ".join(k[:60] for k in r["killed"][:3]))
        if not v.startswith("杀掉"):
            problems.append(f"[Go] {code} {v}：{desc}")
            print(r["out"][-3000:])
        gkill[code] = set(r["killed"])
        for slot in acc:
            files[slot].write_text(originals[slot], encoding="utf-8")
        for slot in acc:
            if md5_bytes(files[slot]) != basemd5[slot]:
                leave_for_evidence("还原后 md5 不一致：现场只活在克隆里")
                raise SystemExit(f"{code} 还原后 md5 不一致，停机")

    dup_report(gkill)
    print("[Go] 已全量还原（md5 一致）")

    dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
    if problems:
        print("\n===== 电池判定：有洞 =====")
        for x in problems:
            print("  ✗", x)
        return 1
    print(f"\n===== 电池判定：Go {len(prepared)} 格逐格被杀，无存活"
          + ("（本趟是 --cells 子集，不等于全电池）" if want else "（全电池）") + " =====")
    return 0


if __name__ == "__main__":
    sys.exit(main())
