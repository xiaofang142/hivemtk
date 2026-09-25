#!/usr/bin/env python3
"""R30 族（测试侧真实出站 + 测试库连接池上界）变异电池：逐刀验"这一批的每句承诺都有一条腿"。

## 这批改了什么， hence 为什么要电池

第四十五轮把"整包跑时测试自己把外部世界打穿"这一族收了口，四处改动各带一套判据：

1. **短信三家网关的接口域**（`internal/service/sms.go`）：三家 URL 从写死的官方常量改成实例字段，
   `NewSmsService` 填官方值、测试把字段指向 httptest。顺带把"渠道失败但 code/msg 交回空串"补成
   错误本身（`smsErrCodeUnspecified` + `err.Error()`），因为两个调用方都是直接把这两个值落台账 ⇒
   以前会出现"status=failed、原因一片空白"的行。
2. **测试库句柄的连接池上界**（`internal/pkg/testutil/testdb.go`）：`SetMaxOpenConns(32)`。
   它不改变任何业务断言的通过与否（探针在两格下都照旧 PASS）⇒ 只有 `testdb_pool_test.go` 那条腿守。
3. **TG 建号的两条外部腿**（`internal/service/feishu.go`）：`tgGetBotUsernameFn` / `tgSetWebhookFn`
   两个注入点，后者进协程前先快照。
4. **飞书媒体三条腿的替身**（`internal/service/webhook_channel_feishu.go` + 承载用例）：
   断言"取凭证替身被走到 4 次"，用来证明"零真实出站"不是"根本没进那个分支"的另一种写法。

"门绿了"在这批里尤其不算依据：这四处的共同点是**删掉它们，全套用例仍然绿**，红的只有
CI 的 53300 连接数错误、和下一次官方站点抖动带来的假红。所以每处都要一格"把它拆掉，必须有人红"。

## 判据形状（三族三种判据，各有理由）

- **test 族**（S/P/T1/T2/F1）：期望"红名集合恰好等于推演的那几条" **且** 红因里要点名到
  本格那条断言。只判"红了"会把连带面当证据（例：S6 摘掉 errMsg 补码，同时会红到 UnknownProvider
  那条腿——那是真实的连带，红因必须落在 `传输层失败也必须留下原因` 而不是别的句子）。
- **gate 族**（T3/F2）：变异是**行为不变**的（把快照挪进协程体），所以用例必须照旧绿，
  判据是 `scripts/check-async-global-read.py` 退 rc=1 且点名那个文件。本地 `-race` 撞不上
  这类形状（第四十二轮实测），所以这里刻意不用编译期判据。
- **race 族**（Q）：`qsScripts` 那把锁只在竞争里显形 ⇒ A/B 两相同一条命令：
  装锁 0 朵、摘锁 ≥1 朵，且竞争块要归属到本腿 + 本家文件名。**判据不锚在标识符上**（口径，
  不是"不可能命中"）：`-race` 的 `Write at 0x…` 一行只有地址、没有字段名，但它会印栈帧的
  函数符号，本腿恰好是 `(*qsScripts).ActiveQuoteScript()` ⇒ 全文 grep `qsScripts` 真能命中 4 行
  （full-2/race_B_Q-off.log 实测）。偶然可行不等于可依赖：符号身份来自**帧**而不是被竞争的那个字段，
  竞争点换进别的函数就没那个标识符；且摘锁后的单行 getter 会被内联、帧本身可以不出现（§23.18 那批 `-race` 取证实测）。

## 为什么每格还要盯 CONNECT

这批的"绿"里最容易作假的一条是**零真实出站**。所以本电池自带一个只记录不放行的 CONNECT 代理：
- 控制组（未注码）必须 **0 条** CONNECT —— 这条红就等于"真出站没修干净"，不用人肉普查；
- 反向格（把字段改回官方常量）必须 **≥1 条**，且主机名要等于那家的官方域 ——
  否则"用例红了"可能只是编译或环境问题，而不是"真的又打回官方站点"。
代理只回 502 不建隧道 ⇒ 任何被判据需要的官方请求都拿不到真回包，取证不会依赖外网可达性。

## 三面第一轮就写错了的判据（full-1 读数逼出来的，写在这里免得下一个人重踩）

- **race A 相的分母不是 1**：那一相跑的是 `-count=10`，同一条用例落 10 行 `--- PASS` ⇒ `settled=10`
  才是健康读数。驱动原先拿 1 当上界，把"锁有效、10 趟零竞争"判成"过滤器没匹配到用例"，
  连带 B 相被 SKIPPED ⇒ **判据自己制造了一条假红**（口径：断言"必非空"时，界要按本格实际跑了几趟算）。
- **S4/S5 的 CONNECT 期望原先写成"无出站"，是推演错不是实测错**：把 tencent 那条腿改成读 aliyun 的字段，
  剩下没被测试覆写的 `aliyunAPIURL` 就是官方域 ⇒ 请求**改道到另一家的官方网关**（实测 `dysmsapi` /
  `sms.tencentcloudapi` 各一次 CONNECT）。"改道"正是这一格要钉的失效形状，所以修的是期望、不是变异。
- **F2 原先不是"行为不变"的变异**：只摘 `tenantTokenFn` 一条腿的引用、留 `mediaFetchFn/mediaStoreFn`
  没有声明 ⇒ `undefined:` 编译红，格子被自己写的变异带跑。三处引用要一起改道，快照行才删得干净。

## 不配格的两面（写明，别把"没数到"印成"没问题"）

- `SetMaxIdleConns(8)`：不承责任何性质（`testdb.go` 的注释自己写明"只为少建几次后端"）。
  给它配一格 ⇒ 那格的红没有含义。它的失效形状由 S/P 两族的红名集合间接覆盖（上界才是闸）。
- `async_db_handle_probe_test.go` 的三枚扇出探针：探针"有无上界都 PASS"正是这条腿存在的理由，
  已在 `testdb.go:120-131` 那段实测注释里单独取过证（无上限峰值连接数与 53300 的因果、
  三枚探针两格都 PASS）⇒ 不在这里重复配格。

## 口径（照本仓既有电池的规矩）

- 只在 `git clone --shared` 出来的私有克隆里注码，脏文件按 `git status --porcelain -uall` 覆盖进去
  （含未跟踪的新文件），删除面同步删掉；
- 控制组**现测**，红名/计数不写死（共享树下别人加用例会让写死的数字漂）；
- 一格多处编辑要在**内存里叠完一次写盘**（逐编辑写盘会把上一刀的还原状态混进来）；
- 每刀还原后逐文件比 md5，不等即停机；
- BUILD-BROKEN / panic 带走整包（settled 掉）/ 红而不点名 ⇒ 一律 BROKEN，不计入杀掉；
- 环境前提不满足（盘、库、代理）退 ENV-BROKEN 并**不**印"全杀"；
- 逐格原始输出落到 `docs/superpowers/specs/ledger/logs/R30/<tag>/`，tag 默认取本地时间戳，
  复跑不覆盖上一轮（文档里的读数要能找回产物）。产物能被找回还有个前提：仓库根的 `.gitignore`
  在日志块尾给了这条例外（`!docs/.../ledger/logs/` + `!**/*.log`）——`logs/` 与 `*.log` 是
  运行时日志规则，没有例外时 117 份取证全在库外，读数的产物只在作者机器上存在（第一趟跑完
  用 `git check-ignore -v` 才数出来，见审计文档 §23.19 第 1-补 段）。

用法：
    python3 scripts/mut_egress_pool_r30.py --check         # 只做静态自检，不放刀不跑用例
    python3 scripts/mut_egress_pool_r30.py                 # 全族
    python3 scripts/mut_egress_pool_r30.py --only S1,S6    # 只跑指定格（终态会写明本趟是子集）
    python3 scripts/mut_egress_pool_r30.py --skip-race     # 不跑 race 族（改完驱动先用它验形状）

`--check` 这一档是 full-1/succ-1 两趟的教训换来的：那两趟里各有"跑完二十分钟才发现是驱动或
期望写错"的格（S16/S17 的锚点在产码里命中 2 次、S13–S15 的期望红因落在另一条分支上、
S17/S19 的变异让 `sentTime` 变成 declared-and-not-used）。它只核磁盘上就能核的五件事：
锚点命中数、红因字面量在某个用例文件里查得见、期望红名在该格的过滤器名单内、
名单里的腿真有 `func` 定义、gate 格点名的文件存在。
**边界要写明**：第二条只挡"字面量根本不存在"（抄错字/漏字），挡不住 P1 那种"字面量在仓库里
查得到、但不是这格该开火的那条分支"（`MaxOpenConnections` 确实在 `testdb_pool_test.go` 里，
只是用例红因印的是另一句话术）⇒ 这一类只有真跑能照出来，`--check` 绿**不是**"电池有牙"的证据。
"""
from __future__ import annotations

import argparse
import hashlib
import os
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time
from pathlib import Path
from redact import scrub  # 落盘前脱敏：常驻产物要过 gitleaks（见 scripts/redact.py 的 why）


ROOT = Path(__file__).resolve().parent.parent
US = "user-server"
LANE_PATHS = [f"{US}/internal"]
DEFAULT_LOGS = "docs/superpowers/specs/ledger/logs/R30"
ANSI = re.compile(r"\x1b\[[0-9;]*m")

SMS_GO = f"{US}/internal/service/sms.go"
TESTDB_GO = f"{US}/internal/pkg/testutil/testdb.go"
FEISHU_GO = f"{US}/internal/service/feishu.go"
FMEDIA_GO = f"{US}/internal/service/webhook_channel_feishu.go"
QUOTE_TEST = f"{US}/internal/service/quote_test.go"

PKG_SVC = "./internal/service/"
PKG_UTIL = "./internal/pkg/testutil/"

# 域名常量（判据里的主机名要等于这些；与 sms.go 的官方常量刻意同源，改常量时这里会红在 CONN-MISMATCH 上）
ALIYUN_HOST = "dysmsapi.aliyuncs.com"
TENCENT_HOST = "sms.tencentcloudapi.com"
HUAWEI_HOST = "smsapi.cn-north-4.boe-business.huaweicloud.com"
TG_HOST = "api.telegram.org"

L_SENDSMS = "TestSmsService_SendSms"
L_TENCENT = "TestSmsService_SendSms_TencentOfficialError"
L_HUAWEI = "TestSmsService_SendSms_HuaweiOfficialError"
L_RESEND = "TestSmsService_ResendSms"
L_TRANSPORT = "TestSmsService_ResendSms_TransportFailureRecordsReason"
L_UNKNOWN = "TestSmsService_SendSms_UnknownProviderRecordsReason"
L_DRAFT = "TestSmsService_SendDraft"
L_DEFAULTS = "TestNewSmsServiceDefaultsToOfficialGatewayURLs"
# 三家**成功**分支的腿（full-1 那趟推演格时暴露：本文件六条桩腿全回业务错，`err == nil` 那一路
# ——也就是台账上 `Status="sent"` 与 `SendTime` 两列——从来没被执行过）。见 sms_success_test.go。
L_SUCC_ALIYUN = "TestSmsService_SendSms_AliyunSuccess"
L_SUCC_TENCENT = "TestSmsService_SendSms_TencentSuccess"
L_SUCC_HUAWEI = "TestSmsService_SendSms_HuaweiSuccess"
# 台账那两行的第二站点（ResendSms 里的逐字重复）与"重发先清上一轮失败原因"这条只有重发才有的承诺。
L_SUCC_RESEND = "TestSmsService_ResendSms_SuccessClearsFailureAndMarksSent"
L_POOL = "TestNewTestDBBoundsPool"
L_TG = "TestE2E_Telegram_AccountCreateAndGet"
L_F17 = "TestN17_FeishuVoiceAndMediaLandInHubWithRightType"
L_QUOTE = "TestQuoteService_ReviseConcurrentSecondLoser"

SMS_LEGS = [L_SENDSMS, L_TENCENT, L_HUAWEI, L_RESEND, L_TRANSPORT, L_UNKNOWN, L_DRAFT, L_DEFAULTS,
            L_SUCC_ALIYUN, L_SUCC_TENCENT, L_SUCC_HUAWEI, L_SUCC_RESEND]
F_SMS = "^(" + "|".join(SMS_LEGS) + ")$"
F_POOL = f"^({L_POOL})$"
F_TG = f"^({L_TG})$"
F_F17 = f"^({L_F17})$"

R_NO_LAND = "失败原因没落账"
R_TRANSPORT_MSG = "传输层失败也必须留下原因"
R_TRANSPORT_CODE = "传输层失败也要留下错误码"
R_UNKNOWN_CODE = "占位错误码"
R_DEFAULTS_ALIYUN = "aliyun 默认接口域"
R_DEFAULTS_TENCENT = "tencent 默认接口域"
R_DEFAULTS_HUAWEI = "huawei 默认接口域"
R_TG_GETME = "getMe 腿没被走到"
R_TG_SETHOOK = "setWebhook 腿没被走到"
R_F17_TOKEN = "取凭证替身只被走到"
# 不是 "MaxOpenConnections"（那是 Go 的 getter 名，用例红因里印的是被测字段话术）。
R_POOL = "测试库句柄的 MaxOpenConns"
# S13–S15 的红因：成功判据被摘 ⇒ 官方回业务错也当成功放行，腿先 Fatal 在「该上抛」这一条上，
# 根本走不到台账断言 ⇒ 期望字面量沿用 R_NO_LAND 就是 BROKEN（succ-1 实测：三格红名全对、
# 输出里没有"失败原因没落账"）。
R_NOT_RAISED = "官方回业务错时"
R_SENT_STATUS = "want sent"
R_SEND_TIME = "SendTime 必须是本次解析出的发送时间"
# ③"成功行不该带失败原因"这条只有 S20/S21 能单独开火：三枚首次发送成功腿的夹具里台账行本来
# 就是干净的，③开火只能是成功路径自己写了错误码；只有重发腿带着上一轮的 code/msg 进格子。
R_SUCCESS_CLEAN = "成功行不该带失败原因"


def cells():
    """每一格：(code, 破坏的是哪句承诺, 编辑列表, 包, 过滤器, 期望红名, 期望红因, 期望 CONNECT 主机)

    judge=gate 的格子期望"用例全绿 + 静态门红且点名文件"；judge=race 的格子由 race 相单独跑。
    """
    return [
        # ---------------- S 族：短信三家的接口域注入与失败补码 ----------------
        ("S1", "aliyun 的 apiURL 改回写死官方域", SMS_GO,
         [("\tapiURL := s.aliyunAPIURL\n", "\tapiURL := smsAliyunAPIURL\n")],
         PKG_SVC, F_SMS,
         {L_SENDSMS, L_RESEND, L_DRAFT, L_SUCC_ALIYUN, L_SUCC_RESEND}, R_NO_LAND, {ALIYUN_HOST}),
        ("S2", "tencent 的 apiURL 改回写死官方域", SMS_GO,
         [("\tapiURL := s.tencentAPIURL\n", "\tapiURL := smsTencentAPIURL\n")],
         PKG_SVC, F_SMS,
         {L_TENCENT, L_SUCC_TENCENT}, R_NO_LAND, {TENCENT_HOST}),
        ("S3", "huawei 的 apiURL 改回写死官方域", SMS_GO,
         [("\tapiURL := s.huaweiAPIURL\n", "\tapiURL := smsHuaweiAPIURL\n")],
         PKG_SVC, F_SMS,
         {L_HUAWEI, L_SUCC_HUAWEI}, R_NO_LAND, {HUAWEI_HOST}),
        # S4/S5 的 CONNECT 期望在 full-1 里订正过一次：串字段不是"没有出站"，而是
        # **改道到另一家的官方网关**（没被测试覆写的那个字段就是官方域）⇒ 出站面恰恰是这格的失效形状。
        ("S4", "tencent 那条腿读成 aliyun 的字段（装配写错对象）", SMS_GO,
         [("\tapiURL := s.tencentAPIURL\n", "\tapiURL := s.aliyunAPIURL\n")],
         PKG_SVC, F_SMS,
         {L_TENCENT, L_SUCC_TENCENT}, R_NO_LAND, {ALIYUN_HOST}),
        ("S5", "huawei 那条腿读成 tencent 的字段（同上，另一家）", SMS_GO,
         [("\tapiURL := s.huaweiAPIURL\n", "\tapiURL := s.tencentAPIURL\n")],
         PKG_SVC, F_SMS,
         {L_HUAWEI, L_SUCC_HUAWEI}, R_NO_LAND, {TENCENT_HOST}),
        ("S6", "摘掉 errMsg 的空白补码", SMS_GO,
         [("\t\tif errMsg == \"\" {\n\t\t\terrMsg = err.Error()\n\t\t}\n", "")],
         PKG_SVC, F_SMS,
         {L_TRANSPORT, L_UNKNOWN}, R_TRANSPORT_MSG, set()),
        ("S7", "摘掉 errCode 的空白补码", SMS_GO,
         [("\t\tif errCode == \"\" {\n\t\t\terrCode = smsErrCodeUnspecified\n\t\t}\n", "")],
         PKG_SVC, F_SMS,
         {L_TRANSPORT, L_UNKNOWN}, R_TRANSPORT_CODE, set()),
        ("S8", "default 分支立刻 return（绕过补码）", SMS_GO,
         [("\t\terr = fmt.Errorf(\"unknown sms provider: %s\", provider)\n",
           "\t\treturn time.Time{}, \"\", \"\", fmt.Errorf(\"unknown sms provider: %s\", provider)\n")],
         PKG_SVC, F_SMS,
         {L_UNKNOWN}, R_UNKNOWN_CODE, set()),
        ("S9", "NewSmsService 不填 aliyun 默认域", SMS_GO,
         [("\t\taliyunAPIURL:  smsAliyunAPIURL,\n", "")],
         PKG_SVC, F_SMS,
         {L_DEFAULTS}, R_DEFAULTS_ALIYUN, set()),
        ("S10", "NewSmsService 不填 tencent 默认域", SMS_GO,
         [("\t\ttencentAPIURL: smsTencentAPIURL,\n", "")],
         PKG_SVC, F_SMS,
         {L_DEFAULTS}, R_DEFAULTS_TENCENT, set()),
        ("S11", "NewSmsService 不填 huawei 默认域", SMS_GO,
         [("\t\thuaweiAPIURL:  smsHuaweiAPIURL,\n", "")],
         PKG_SVC, F_SMS,
         {L_DEFAULTS}, R_DEFAULTS_HUAWEI, set()),
        ("S12", "NewSmsService 把 huawei 的域填给 aliyun（三家写串）", SMS_GO,
         [("\t\taliyunAPIURL:  smsAliyunAPIURL,\n", "\t\taliyunAPIURL:  smsHuaweiAPIURL,\n")],
         PKG_SVC, F_SMS,
         {L_DEFAULTS}, R_DEFAULTS_ALIYUN, set()),

        # -------- S 族后半：三家「受理成功」那条分支（S13–S15 判据方向，S16–S17 台账落账）--------
        # 判据写成 `if false` 而不是删掉整块：删块会连带改到 return 的形状，红因就说不清是
        # "判据没了"还是"分支没了"（口径：BROKEN 修变异不修期望，变异要单变量）。
        ("S13", "阿里云成功判据失效（业务错被当成功）", SMS_GO,
         [('\tif result.Code != "OK" {\n', "\tif false {\n")],
         PKG_SVC, F_SMS,
         {L_SENDSMS, L_RESEND, L_DRAFT}, R_NOT_RAISED, set()),
        ("S14", "腾讯云成功判据失效（Error 子对象被无视）", SMS_GO,
         [('\tif result.Response.Error.Code != "" {\n', "\tif false {\n")],
         PKG_SVC, F_SMS,
         {L_TENCENT}, R_NOT_RAISED, set()),
        ("S15", "华为云成功判据失效（小写 code 的 000000 不再被要求）", SMS_GO,
         [('\tif result.Code != "000000" {\n', "\tif false {\n")],
         PKG_SVC, F_SMS,
         {L_HUAWEI}, R_NOT_RAISED, set()),
        # S16–S19 这四格**必须按站点拆开**：`record.SendTime = &sentTime` / `record.Status = "sent"`
        # 这两句在 sms.go 里各出现两次（首次发送 :296-297、重发 :587-588，逐字相同），
        # 单行锚点会命中 2 次 ⇒ PATCH-BROKEN；而若图省事只注其中一处（另一处仍在），
        # 那就是"一处符号多处消费只拆一刀"的假全杀。锚点因此带上 `send sms failed` /
        # `resend sms failed` 这两行只在一侧存在的错误包装语句做定位。
        ("S16", "首次发送受理后状态不翻成 sent", SMS_GO,
         [('''\t\treturn fmt.Errorf("send sms failed: %w", err)
\t}

\trecord.SendTime = &sentTime
\trecord.Status = "sent"
''', '''\t\treturn fmt.Errorf("send sms failed: %w", err)
\t}

\trecord.SendTime = &sentTime
\trecord.Status = "sending"
''')],
         PKG_SVC, F_SMS,
         {L_SUCC_ALIYUN, L_SUCC_TENCENT, L_SUCC_HUAWEI}, R_SENT_STATUS, set()),
        ("S17", "首次发送受理成功但发送时间不落账（句子包进 if false 以留住 sentTime 的使用点）", SMS_GO,
         [('''\t\treturn fmt.Errorf("send sms failed: %w", err)
\t}

\trecord.SendTime = &sentTime
''', '''\t\treturn fmt.Errorf("send sms failed: %w", err)
\t}

\tif false {
\t\trecord.SendTime = &sentTime
	}
''')],
         PKG_SVC, F_SMS,
         {L_SUCC_ALIYUN, L_SUCC_TENCENT, L_SUCC_HUAWEI}, R_SEND_TIME, set()),
        ("S18", "重发受理后状态不翻成 sent（首次发送那侧照旧）", SMS_GO,
         [('''\t\treturn fmt.Errorf("resend sms failed: %w", err)
\t}

\trecord.SendTime = &sentTime
\trecord.Status = "sent"
''', '''\t\treturn fmt.Errorf("resend sms failed: %w", err)
\t}

\trecord.SendTime = &sentTime
\trecord.Status = "sending"
''')],
         PKG_SVC, F_SMS,
         {L_SUCC_RESEND}, R_SENT_STATUS, set()),
        ("S19", "重发受理成功但发送时间不落账（同上，首次发送那侧照旧）", SMS_GO,
         [('''\t\treturn fmt.Errorf("resend sms failed: %w", err)
\t}

\trecord.SendTime = &sentTime
''', '''\t\treturn fmt.Errorf("resend sms failed: %w", err)
\t}

\tif false {
\t\trecord.SendTime = &sentTime
	}
''')],
         PKG_SVC, F_SMS,
         {L_SUCC_RESEND}, R_SEND_TIME, set()),
        # S20/S21 各摘一行清空：两行都在 sms.go 里唯一出现，且失败路径随后会重新赋 errCode/errMsg，
        # 所以只有"带着一轮失败原因进重发、这次被受理"的那条腿能认出少摘的那一行——归属靠测试名。
        ("S20", "重发进入时不清上一轮错误码", SMS_GO,
         [('\trecord.ErrorCode = ""\n', "")],
         PKG_SVC, F_SMS,
         {L_SUCC_RESEND}, R_SUCCESS_CLEAN, set()),
        ("S21", "重发进入时不清上一轮错误文案", SMS_GO,
         [('\trecord.ErrorMsg = ""\n', "")],
         PKG_SVC, F_SMS,
         {L_SUCC_RESEND}, R_SUCCESS_CLEAN, set()),


        # ---------------- P 族：测试库句柄的连接池上界 ----------------
        ("P1", "摘掉 SetMaxOpenConns（上界回到 database/sql 的默认＝不限）", TESTDB_GO,
         [("\t\tsqlDB.SetMaxOpenConns(testDBMaxOpenConns)\n", "")],
         PKG_UTIL, F_POOL,
         {L_POOL}, R_POOL, set()),
        ("P2", "上界常量从 32 改成 200（越过 CI 的 100 名额）", TESTDB_GO,
         [("\ttestDBMaxOpenConns = 32\n", "\ttestDBMaxOpenConns = 200\n")],
         PKG_UTIL, F_POOL,
         {L_POOL}, R_POOL, set()),
        ("P3", "把 idle 上界当成 open 上界传（两行赋值串位）", TESTDB_GO,
         [("\t\tsqlDB.SetMaxOpenConns(testDBMaxOpenConns)\n",
           "\t\tsqlDB.SetMaxOpenConns(testDBMaxIdleConns)\n")],
         PKG_UTIL, F_POOL,
         {L_POOL}, R_POOL, set()),

        # ---------------- T 族：TG 建号的两条外部腿 ----------------
        ("T1", "getMe 绕过注入点直连官方", FEISHU_GO,
         [("\t\tif uname, gerr := tgGetBotUsernameFn(acc.BotToken); gerr == nil && uname != \"\" {",
           "\t\tif uname, gerr := tgbot.GetBotUsername(acc.BotToken); gerr == nil && uname != \"\" {")],
         PKG_SVC, F_TG,
         {L_TG}, R_TG_GETME, {TG_HOST}),
        ("T2", "setWebhook 绕过注入点直连官方", FEISHU_GO,
         [("\t\tsetWebhookFn := tgSetWebhookFn\n", ""),
          ("\t\t\tif err := setWebhookFn(acc.BotToken, acc.WebhookURL, acc.WebhookSecret); err != nil {",
           "\t\t\tif err := tgbot.SetWebhook(acc.BotToken, acc.WebhookURL, acc.WebhookSecret); err != nil {")],
         PKG_SVC, F_TG,
         {L_TG}, R_TG_SETHOOK, {TG_HOST}),
        ("T3", "快照挪进协程体（行为不变，只剩静态门在守）", FEISHU_GO,
         [("\t\tsetWebhookFn := tgSetWebhookFn\n", ""),
          ("\t\t\tif err := setWebhookFn(acc.BotToken",
           "\t\t\tif err := tgSetWebhookFn(acc.BotToken")],
         PKG_SVC, F_TG, set(), None, set(), "gate", "internal/service/feishu.go"),

        # ---------------- F 族：飞书媒体替身与快照 ----------------
        ("F1", "四类媒体少派发一类（计数腿要认出 3/4）", FMEDIA_GO,
         [("\tif mediaKey != \"\" {\n\t\ts.persistFeishuMediaAsync(",
           "\tif mediaKey != \"\" && hub.MsgType != model.MsgTypeAudio {\n\t\ts.persistFeishuMediaAsync(")],
         PKG_SVC, F_F17,
         {L_F17}, R_F17_TOKEN, set()),
        # F2 必须四处一起改：只摘快照行、只把一条腿改道 ⇒ 另两条腿引用的是**已不存在的局部变量**
        # ⇒ `undefined:` 编译红（full-1 实测：rc=1 而红名是空的，看着像"行为变了"其实是没编过）。
        ("F2", "三条腿的快照挪进协程体（行为不变）", FMEDIA_GO,
         [("\ttenantTokenFn, mediaFetchFn, mediaStoreFn := feishuTenantTokenFn, feishuMediaFetchFn, feishuMediaStoreFn\n", ""),
          ("\t\ttenantToken, tkerr := tenantTokenFn(gctx, integration, acc)",
           "\t\ttenantToken, tkerr := feishuTenantTokenFn(gctx, integration, acc)"),
          ("\t\trc, contentType, derr := mediaFetchFn(gctx, tenantToken, messageID, fileKey, resType)",
           "\t\trc, contentType, derr := feishuMediaFetchFn(gctx, tenantToken, messageID, fileKey, resType)"),
          ("\t\tpublicURL, serr := mediaStoreFn(gctx, \"feishu\", fileKey, data, contentType, fileName)",
           "\t\tpublicURL, serr := feishuMediaStoreFn(gctx, \"feishu\", fileKey, data, contentType, fileName)")],
         PKG_SVC, F_F17, set(), None, set(), "gate", "internal/service/webhook_channel_feishu.go"),
    ]


def race_cells():
    """race 族：A/B 两相同一条命令，唯一区别是那把锁在不在。"""
    return [
        ("Q-off", "摘掉 qsScripts 的写锁（4 个 writer 协程并发写同一字段）", QUOTE_TEST,
         [("\ts.mu.Lock()\n\ts.gotID, s.gotOneID = id, oneID\n\ts.mu.Unlock()\n",
           "\ts.gotID, s.gotOneID = id, oneID\n")]),
    ]


# ---------------------------------------------------------------- primitives

def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(p: Path) -> str:
    return p.read_text(encoding="utf-8")


class Recorder(threading.Thread):
    """只记录不放行的 CONNECT 代理：记录 host:port，回 502，不建隧道。"""

    def __init__(self, logpath: Path):
        super().__init__(daemon=True)
        self.logpath = logpath
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self.sock.bind(("127.0.0.1", 0))
        self.sock.listen(64)
        self.port = self.sock.getsockname()[1]
        self.stop = False

    def run(self):
        while not self.stop:
            try:
                conn, _ = self.sock.accept()
            except OSError:
                return
            threading.Thread(target=self.handle, args=(conn,), daemon=True).start()

    def handle(self, conn: socket.socket):
        try:
            conn.settimeout(5)
            buf = b""
            while b"\r\n\r\n" not in buf:
                chunk = conn.recv(2048)
                if not chunk:
                    return
                buf += chunk
            line = buf.split(b"\r\n")[0].decode(errors="replace")
            host = line.split(" ")[1] if len(line.split(" ")) > 1 else line
            with self.logpath.open("a", encoding="utf-8") as fh:
                fh.write(f"{time.strftime('%H:%M:%S')} CONNECT {host}\n")
            conn.sendall(b"HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
        except Exception:
            pass
        finally:
            try:
                conn.close()
            except OSError:
                pass

    def lines(self) -> list[str]:
        if not self.logpath.exists():
            return []
        return self.logpath.read_text(encoding="utf-8").splitlines()

    def settle(self, quiet_s: float = 2.0, max_s: float = 14.0) -> list[str]:
        """等日志静默再取增量：上一条用例留下的 fire-and-forget 协程可能此刻才 CONNECT。"""
        last = len(self.lines())
        waited = 0.0
        while waited < max_s:
            time.sleep(quiet_s)
            waited += quiet_s
            now = len(self.lines())
            if now == last:
                break
            last = now
        return self.lines()


def lane_overlays() -> tuple[list[str], list[str]]:
    r = subprocess.run(["git", "-C", str(ROOT), "status", "--porcelain", "-uall", "--"] + LANE_PATHS,
                       capture_output=True, text=True, timeout=300)
    if r.returncode != 0:
        raise SystemExit("git status 失败，拿不到脏文件清单：" + r.stderr[-200:])
    mods, dels = [], []
    for line in r.stdout.splitlines():
        st = line[:2]
        p = line[3:].split(" -> ")[-1].strip().strip('"')
        dels.append(p) if "D" in st else mods.append(p)
    if not mods:
        raise SystemExit("脏文件清单为空——克隆里跑的是 HEAD，测不到本批改动（宁可停机也别假绿）")
    return mods, dels


def prepare(dst: Path) -> Path:
    clone = dst / "clone"
    if clone.exists():
        raise SystemExit(f"{clone} 已存在（换 --clone 目录或先删）")
    r = subprocess.run(["git", "clone", "--shared", "--no-checkout", str(ROOT), str(clone)],
                       capture_output=True, text=True, timeout=900)
    if r.returncode != 0:
        raise SystemExit("克隆失败：" + (r.stdout + r.stderr)[-400:])
    b = subprocess.run(["git", "checkout", "-f", "master"], cwd=clone,
                       capture_output=True, text=True, timeout=900)
    if b.returncode != 0:
        raise SystemExit("checkout 失败：" + (b.stdout + b.stderr)[-400:])
    mods, dels = lane_overlays()
    for rel in mods:
        src = ROOT / rel
        if not src.exists():
            raise SystemExit(f"覆盖源缺失：{src}")
        tgt = clone / rel
        tgt.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, tgt)
    for rel in dels:
        (clone / rel).unlink(missing_ok=True)
    print(f"覆盖 {len(mods)} 个脏文件、同步 {len(dels)} 个删除进克隆")
    for env in (ROOT / US / ".env", ROOT / ".env"):
        if env.exists():
            shutil.copy2(env, clone / US / ".env")
            break
    return clone


def db_password() -> str:
    if os.environ.get("POSTGRES_TEST_PASSWORD"):
        return os.environ["POSTGRES_TEST_PASSWORD"]
    for env in (ROOT / US / ".env", ROOT / ".env"):
        if env.exists():
            seen = {}
            for line in read(env).splitlines():
                if line.startswith("POSTGRES_PASSWORD="):
                    seen.setdefault("v", line.split("=", 1)[1].strip())
            if seen:
                return seen["v"]
    return ""


def test_env(clone: Path, port: int, race: bool) -> dict:
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOCACHE", os.environ.get("R45_MUT_GOCACHE", "/tmp/gocache-r45mut"))
    env.setdefault("POSTGRES_TEST_PORT", "8232")
    env.setdefault("POSTGRES_TEST_USER", "admin")
    if not env.get("POSTGRES_TEST_PASSWORD"):
        pw = db_password()
        if not pw:
            raise SystemExit("ENV-BROKEN：拿不到 POSTGRES_TEST_PASSWORD（.env 不在位、环境也没给）")
        env["POSTGRES_TEST_PASSWORD"] = pw
    # 只设 HTTPS_PROXY：官方域全是 https，注入用的 httptest 是 http ⇒ 桩不会被打进代理。
    env["HTTPS_PROXY"] = f"http://127.0.0.1:{port}"
    env["https_proxy"] = env["HTTPS_PROXY"]
    env["NO_PROXY"] = "127.0.0.1,localhost"
    env["no_proxy"] = env["NO_PROXY"]
    if race:
        env.setdefault("CGO_ENABLED", "1")
    return env


def run_go(clone: Path, pkg: str, filt: str, env: dict, race: bool = False,
           count: int = 1, timeout: int = 2400) -> dict:
    argv = ["go", "test", pkg, "-run", filt, "-count", str(count)]
    if race:
        argv.append("-race")
    argv += ["-v", "-timeout", "20m"]
    p = subprocess.run(argv, cwd=clone / US, capture_output=True, text=True, timeout=timeout, env=env)
    out = ANSI.sub("", p.stdout + p.stderr)
    top = lambda kind: len(re.findall(rf"^--- {kind}: ", out, re.M))
    return {"rc": p.returncode, "out": out,
            "settled": top("PASS") + top("FAIL") + top("SKIP"),
            "skipped": top("SKIP"),
            "red": sorted({m.split("/")[0] for m in re.findall(r"^--- FAIL: (\S+)", out, re.M)}),
            "panicked": bool(re.search(r"^panic: |^fatal error: ", out, re.M)),
            "buildfailed": "[build failed]" in out or "undefined:" in out
                           or "declared and not used" in out or "not enough arguments" in out,
            "races": len(re.findall(r"^WARNING: DATA RACE", out, re.M))}


def run_gate(clone: Path) -> tuple[int, str]:
    p = subprocess.run(["python3", "scripts/check-async-global-read.py"],
                       cwd=clone, capture_output=True, text=True, timeout=900)
    return p.returncode, ANSI.sub("", p.stdout + p.stderr)


def causes(out: str) -> list[str]:
    keep = [l.strip()[:220] for l in out.splitlines()
            if re.match(r"^\s{2,}\S+\.go:\d+:", l) or "panic:" in l or "DATA RACE" in l]
    return keep[:10]


def conn_hosts(lines: list[str]) -> dict[str, int]:
    c = {}
    for l in lines:
        m = re.search(r"CONNECT (\S+)", l)
        if m:
            c[m.group(1).rsplit(":", 1)[0]] = c.get(m.group(1).rsplit(":", 1)[0], 0) + 1
    return c


def apply_edits(path: Path, original: str, edits: list[tuple[str, str]]) -> tuple[bool, str]:
    """所有编辑在内存里叠完再一次写盘。锚点命中数 != 1 ⇒ 不落盘。"""
    text = original
    for old, new in edits:
        if text.count(old) != 1:
            return False, f"锚点命中 {text.count(old)} != 1：{old[:60]!r}"
        text = text.replace(old, new, 1)
    path.write_text(text)
    return True, ""


# ---------------------------------------------------------------- main

def _test_files() -> list[Path]:
    global _TEST_FILES
    if _TEST_FILES is None:
        _TEST_FILES = sorted((ROOT / US / "internal").rglob("*_test.go"))
    return _TEST_FILES


_TEST_FILES: list[Path] | None = None


def _literal_exists(lit: str) -> bool:
    """红因字面量必须真的写在某条用例里（full-1 的 P1 就是把 Go 的 getter 名当成了用例话术）。"""
    return any(lit in p.read_text() for p in _test_files())


def _leg_defined(leg: str) -> bool:
    return any(f"func {leg}(" in p.read_text() for p in _test_files())


def preflight() -> int:
    """静态自检：不放刀、不编译、不跑用例，只把"会在真跑时才暴露的写法错"提前判掉。

    为什么要有这一档：full-1 里 P1 的假定性红（`R_POOL` 写成 Go 的 getter 名而不是用例话术）、
    S16/S17 的双命中锚点、S13–S17 的元组写成裸字符串列表，三处都是"跑一趟 20 分钟才知道"的错。
    这一档只做磁盘能核的事：锚点命中数、红因字面量在不在用例里、期望红名在不在过滤器名单里、
    名单里的腿在不在 `_test.go` 里、gate 格点名的文件存不存在。它**不**替代真跑。
    """
    bad: list[str] = []
    legsets = {F_SMS: set(SMS_LEGS), F_POOL: {L_POOL}, F_TG: {L_TG}, F_F17: {L_F17}}

    texts: dict[str, str] = {}

    def src(rel: str) -> str:
        if rel not in texts:
            p = ROOT / rel
            if not p.exists():
                bad.append(f"注码目标文件不存在：{rel}")
                texts[rel] = ""
            else:
                texts[rel] = p.read_text()
        return texts[rel]

    def check_anchors(code: str, rel: str, edits: list[tuple[str, str]]) -> None:
        text = src(rel)
        for old, new in edits:
            n = text.count(old)
            if n != 1:
                bad.append(f"{code}: 锚点命中 {n} != 1 :: {old[:70]!r}")
                break
            text = text.replace(old, new, 1)

    for code, _desc, rel, edits in race_cells():
        check_anchors(code, rel, edits)

    for spec in cells():
        code, _desc, rel, edits = spec[0], spec[1], spec[2], spec[3]
        check_anchors(code, rel, edits)
        filt = spec[5]
        if filt not in legsets:
            bad.append(f"{code}: 过滤器 {filt!r} 不在已知名单里")
        else:
            extra = set(spec[6]) - legsets[filt]
            if extra:
                bad.append(f"{code}: 期望红名 {sorted(extra)} 不在 {filt} 的名单里 ⇒ 永远不会红")
        if spec[7] and not _literal_exists(spec[7]):
            bad.append(f"{code}: 红因字面量 {spec[7]!r} 在用例里查无出处")
        # gate 格点名的路径是用例所在包的**相对 user-server** 写法（门的输出用它自己的相对路径）
        if len(spec) > 10 and spec[10] and not (ROOT / US / spec[10]).exists():
            bad.append(f"{code}: gate 格点名的文件不存在：{US}/{spec[10]}")

    for filt, legs in legsets.items():
        for leg in sorted(legs):
            if not _leg_defined(leg):
                bad.append(f"名单里的腿 {leg}（{filt}）在任何 *_test.go 里没有 func 定义")

    print(f"preflight：{len(cells())} 个行为格 + {len(race_cells())} 个 race 格，"
          f"{len(bad)} 处静态问题")
    for b in bad:
        print("  - " + b)
    return 1 if bad else 0


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    ap.add_argument("--only", default="", help="逗号分隔格名；终态会写明本趟是子集")
    ap.add_argument("--skip-race", action="store_true")
    ap.add_argument("--race-count", type=int, default=10)
    ap.add_argument("--logs", default=DEFAULT_LOGS)
    ap.add_argument("--tag", default=time.strftime("%Y%m%d-%H%M%S"))
    ap.add_argument("--check", action="store_true",
                    help="只做静态自检（锚点命中数/红因字面量/红名名单），不放刀不跑用例")
    args = ap.parse_args()

    if args.check:
        return preflight()

    only = {c.strip() for c in args.only.split(",") if c.strip()}
    allcodes = {c[0] for c in cells()} | {c[0] for c in race_cells()}
    if only and not only <= allcodes:
        raise SystemExit(f"--only 里有不存在的格：{sorted(only - allcodes)}")

    st = os.statvfs("/tmp")
    free = st.f_bavail * st.f_frsize
    if free < 20 * 1024 ** 3:
        print(f"ENV-BROKEN：/tmp 只剩 {free // 1024**2} MB，-race 编译会把盘写满（盘满的红全是假红）")
        return 4

    logs = ROOT / args.logs / args.tag
    logs.mkdir(parents=True, exist_ok=True)
    from battlog import tee_to  # 判定行与逐格产物同处一地（LOGDIR/00-run.log）
    tee_to(logs / "00-run.log")
    tmp = Path(args.clone or tempfile.mkdtemp(prefix="r30mut-"))
    tmp.mkdir(parents=True, exist_ok=True)

    head = subprocess.run(["git", "-C", str(ROOT), "rev-parse", "--short", "HEAD"],
                          capture_output=True, text=True).stdout.strip()
    load = subprocess.run(["uptime"], capture_output=True, text=True).stdout.strip().split("users,")[-1].strip()
    print(f"私有作业目录：{tmp}\n逐格日志目录：{logs}")
    print(f"取证基线：HEAD={head} 工作树={ROOT} load={load} 日志 tag={args.tag}")

    clone = prepare(tmp)
    rec = Recorder(logs / "connect.log")
    rec.start()
    print(f"CONNECT 记录代理：127.0.0.1:{rec.port}（只记不走，回 502）")
    env = test_env(clone, rec.port, race=False)
    race_env = test_env(clone, rec.port, race=True)

    mutated = sorted({c[2] for c in cells()} | {c[2] for c in race_cells()})
    files = {rel: clone / rel for rel in mutated}
    originals = {rel: read(p) for rel, p in files.items()}
    base = {rel: md5_bytes(p) for rel, p in files.items()}
    print("本轮基线字节（注码前 == 还原后 才算干净）：")
    for rel in mutated:
        print(f"  {base[rel]}  {rel}")

    problems: list[str] = []

    # -------- 控制组：现测名单 + 必须零真实出站 --------
    controls: dict[str, int] = {}
    control_egress: dict[str, int] = {}
    for label, pkg, filt in (("SMS", PKG_SVC, F_SMS), ("POOL", PKG_UTIL, F_POOL),
                             ("TG", PKG_SVC, F_TG), ("F17", PKG_SVC, F_F17)):
        rec.settle()
        mark = len(rec.lines())
        c = run_go(clone, pkg, filt, env)
        hosts = conn_hosts(rec.lines()[mark:])
        for k, v in hosts.items():
            control_egress[k] = control_egress.get(k, 0) + v
        (logs / f"control_{label}.log").write_text(scrub(c["out"]))
        print(f"[控制组 {label}] rc={c['rc']} settled={c['settled']} skip={c['skipped']} "
              f"红名={c['red'] or '—'} CONNECT={hosts or '无'}")
        if c["rc"] != 0 or c["settled"] == 0 or c["skipped"] or c["red"]:
            print("\n".join(causes(c["out"])))
            print("ENV-BROKEN：控制组不干净——后面所有红/绿都不可信，停机")
            return 4
        controls[pkg + filt] = c["settled"]
    if control_egress:
        problems.append(f"CONTROL 控制组里出现真实出站 CONNECT {control_egress}：本批的「零真实出站」没修干净")
        print(f"  !! 控制组出现 CONNECT {control_egress}（真出站回归）")

    # -------- race 族 A 相：装锁的那份字节，必须 0 朵 --------
    if not args.skip_race and (not only or any(c[0] in only for c in race_cells())):
        rec.settle()
        r = run_go(clone, PKG_SVC, f"^({L_QUOTE})$", race_env, race=True, count=args.race_count)
        (logs / "race_A_locked.log").write_text(scrub(r["out"]))
        print(f"[race A 相] 带锁 {args.race_count} 趟：rc={r['rc']} settled={r['settled']} "
              f"红名={r['red'] or '—'} DATA RACE 块={r['races']}")
        if r["races"]:
            problems.append(f"Q-on race A 相带锁仍撞出 {r['races']} 朵竞争：那把锁没起作用（先修产码/夹具再谈电池）")
        # settled 必须等于本相实际跑了几趟（`-count=N` ⇒ 同一条用例落 N 行 `--- PASS`）：
        # 过滤器没匹配到用例时 go test 照样退 0、"0 朵竞争"看着像"锁有效"，那是一条没跑过
        # 任何东西的判据（口径：三档全零输出要先跑"必非空"那档；界要按本格真实趟数算，full-1 实测）。
        if r["settled"] != args.race_count:
            problems.append(f"Q-on race A 相 settled={r['settled']} != {args.race_count}（-count 趟数）："
                            f"过滤器 {L_QUOTE} 没跑满，「0 朵竞争」不成立")
        if r["rc"] != 0 or r["red"]:
            problems.append("Q-on race A 相未全绿：竞争之外的红，先归因再放行 B 相")
            print("\n".join(causes(r["out"])))
        race_control_ok = (not r["races"] and r["rc"] == 0 and not r["red"]
                           and r["settled"] == args.race_count)
    else:
        race_control_ok = True

    # -------- 逐格注码 --------
    def run_cell_tests(code, desc, rel, edits, pkg, filt, expect_red, expect_reason, expect_conn,
                       judge, gate_file):
        src = originals[rel]
        if judge == "gate":
            src2 = src
            for old, new in edits:
                if src2.count(old) != 1:
                    return "PATCH-BROKEN", f"锚点命中 {src2.count(old)} != 1", []
                src2 = src2.replace(old, new, 1)
            files[rel].write_text(src2)
            try:
                grc, gout = run_gate(clone)
                c = run_go(clone, pkg, filt, env)
            finally:
                files[rel].write_text(src)
                if md5_bytes(files[rel]) != base[rel]:
                    raise SystemExit(f"{code} 还原后 md5 不一致，停机")
            (logs / f"{code}.log").write_text(scrub(gout + "\n===== 用例 =====\n" + c["out"]))
            gate_named = gate_file in gout
            if grc == 0:
                return "SURVIVED=门没牙", f"gate rc=0（应红且点名 {gate_file}）", []
            if not gate_named:
                return "BROKEN=门红而不点名", f"gate rc={grc} 但输出里没有 {gate_file}", causes(gout)
            if c["rc"] != 0 or c["red"]:
                return "BROKEN=行为变了", f"静态格却把用例跑红了（红名={c['red']}）⇒ 变异不是行为不变的", causes(c["out"])
            if c["settled"] != controls[pkg + filt]:
                return "BROKEN=没跑完", f"settled={c['settled']} != 控制组 {controls[pkg + filt]}", causes(c["out"])
            return "杀掉", f"gate rc={grc} 点名 {gate_file}，用例 {c['settled']} 条全绿（行为不变）", []

        mark = len(rec.settle())
        ok, why = apply_edits(files[rel], src, edits)
        if not ok:
            return "PATCH-BROKEN", why, []
        try:
            c = run_go(clone, pkg, filt, env)
        finally:
            files[rel].write_text(src)
            if md5_bytes(files[rel]) != base[rel]:
                raise SystemExit(f"{code} 还原后 md5 不一致，停机")
        hosts = conn_hosts(rec.settle()[mark:])
        (logs / f"{code}.log").write_text(scrub(c["out"] + "\n===== CONNECT =====\n" +
                                          "\n".join(rec.lines()[mark:])))
        if c["buildfailed"]:
            return "BUILD-BROKEN", "编译坏（修变异不修期望）", causes(c["out"])
        if c["panicked"] or c["settled"] != controls[pkg + filt]:
            return "RAN-BROKEN", f"settled={c['settled']} != 控制组 {controls[pkg + filt]} panic={c['panicked']}", causes(c["out"])
        extra = set(hosts) - expect_conn
        if extra:
            return "CONN-MISMATCH", f"意外的真实出站 {sorted(extra)}", []
        if not set(hosts) >= expect_conn:
            return "CONN-BROKEN", f"期望打到 {sorted(expect_conn)}，实际 {hosts or '无'} ⇒ 红不是「真打回官方域」带来的", []
        if not c["red"]:
            return "SURVIVED=洞", "没有任何用例被打红 ⇒ 这条承诺只有注释在守", []
        if set(c["red"]) != expect_red:
            return "BROKEN=红集合不符", f"期望={sorted(expect_red)} 实际={c['red']}", causes(c["out"])
        if expect_reason and expect_reason not in c["out"]:
            return "BROKEN=红因而非预期", f"红名对了但输出里没有 {expect_reason!r} ⇒ 判据分支没开火", causes(c["out"])
        return "杀掉", (f"红={','.join(c['red'])}"
                      + (f" conn={{{', '.join(f'{k}:{v}' for k, v in sorted(hosts.items()))}}}" if hosts else "")), []

    ran = 0
    for spec in cells():
        code, desc, rel, edits, pkg, filt, expect_red, expect_reason, expect_conn = spec[:9]
        judge = spec[9] if len(spec) > 9 else "test"
        gate_file = spec[10] if len(spec) > 10 else ""
        if only and code not in only:
            continue
        ran += 1
        status, detail, red = run_cell_tests(code, desc, rel, edits, pkg, filt, expect_red,
                                            expect_reason, expect_conn, judge, gate_file)
        print(f"{code:<5} {desc[:46]:<48} {status:<18} {detail}")
        for r in red:
            print(f"      红因: {r}")
        if status != "杀掉":
            problems.append(f"{code} {status}：{desc}（{detail}）")

    # -------- race 族 B 相：摘锁 --------
    if not args.skip_race and (not only or any(c[0] in only for c in race_cells())):
        for code, desc, rel, edits in race_cells():
            if only and code not in only:
                continue
            ran += 1
            if not race_control_ok:
                print(f"{code:<5} {desc[:46]:<48} SKIPPED=控制组不成立")
                problems.append(f"{code} 未跑：A 相（带锁）不成立，B 相读数无从对比")
                continue
            src = originals[rel]
            ok, why = apply_edits(files[rel], src, edits)
            if not ok:
                print(f"{code:<5} {desc[:46]:<48} PATCH-BROKEN {why}")
                problems.append(f"{code} PATCH-BROKEN：{why}")
                continue
            try:
                mark = len(rec.lines())
                r = run_go(clone, PKG_SVC, f"^({L_QUOTE})$", race_env, race=True, count=args.race_count)
            finally:
                files[rel].write_text(src)
                if md5_bytes(files[rel]) != base[rel]:
                    raise SystemExit(f"{code} 还原后 md5 不一致，停机")
            hosts = conn_hosts(rec.lines()[mark:])
            (logs / f"race_B_{code}.log").write_text(scrub(r["out"]))
            attributed = re.findall(rf"^--- FAIL: {L_QUOTE}", r["out"], re.M)
            inlawfile = QUOTE_TEST.split("/")[-1] in r["out"]
            if r["buildfailed"]:
                status, detail = "BUILD-BROKEN", "摘锁后编译坏（sync 未使用之类）"
            elif hosts:
                status, detail = "CONN-MISMATCH", f"摘锁格不该有出站：{hosts}"
            elif not r["races"]:
                status, detail = "窗口不足", (f"{args.race_count} 趟没撞中竞争 ⇒ 判据不可判，"
                                            f"不是「锁没用」也不是「已杀」")
            elif not (attributed and inlawfile):
                status, detail = "BROKEN=归属不明", f"有 {r['races']} 朵但没归到 {L_QUOTE}+{QUOTE_TEST.split('/')[-1]}"
            else:
                status, detail = "杀掉", f"{r['races']} 朵竞争块，全部归本腿/本家文件，rc={r['rc']}"
            print(f"{code:<5} {desc[:46]:<48} {status:<18} {detail}")
            if status != "杀掉":
                problems.append(f"{code} {status}：{desc}（{detail}）")
                for x in causes(r["out"]):
                    print(f"      红因: {x}")

    for rel in mutated:
        if md5_bytes(files[rel]) != base[rel]:
            print(f"RESTORE-BROKEN {rel} 与基线不等：停机，先手动核对")
            return 5

    scope = f"{ran} 格" + (f"（--only {','.join(sorted(only))}，非全族）" if only else "（全族）")
    if args.skip_race:
        scope += "，race 族本趟未跑"
    print(f"\n===== 判定：{scope} =====")
    if problems:
        print(f"{len(problems)} 格未杀/BROKEN/环境不成立：")
        for p in problems:
            print("  - " + p)
    else:
        print("逐格被杀，无存活，无 CONN/RAN/PATCH/BUILD 异常")
    print(f"（BROKEN 的原始输出在 {logs}/<格>.log，逐条读后再谈定性；"
          f"CONNECT 总表在 {logs}/connect.log）")
    if not args.keep:
        shutil.rmtree(tmp, ignore_errors=True)
    else:
        print(f"保留作业目录：{tmp}")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
