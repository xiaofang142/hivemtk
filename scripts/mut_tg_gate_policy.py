#!/usr/bin/env python3
"""社群门控三项口径（ADR-016）的牙齿电池：逐刀把定下的判据改坏，必须有人红。

用例绿只证明"写下的断言成立"，不证明"断言看得见这一格的失效"。本卡最坏的一类失效恰好是
**删掉它没有任何用例会红**——而被删掉的判据全都在"对真人动手"的那条路上：补偿集合的
"提示从未送达"谓词没了，就是线上那 219 条重复入群播报；到期宽限没了，就是一个窗口没点链接
的人被当场移出；租约判据没了，就是同一套库上每个实例各自每分钟广播一遍。

跑法（只在 `--shared` 影子树里动手，活树一刀不碰；影子树必须是本泳道全量文件的现取副本）：

    python3 scripts/mut_tg_gate_policy.py --check           # 只验锚点＋现数影子树文件名单
    R67_LOGDIR=docs/superpowers/specs/ledger/logs/TGGATE/$(date +%Y%m%d-%H%M%S) \
        python3 scripts/mut_tg_gate_policy.py run

判定行与逐格产物同处一地：驱动一开跑就 `battlog.tee_to()` 把 stdout 与 stderr 一起镜像进本轮
目录的 `00-run.log`（预检轮叫 `00-check.log`），所以命令行**不要**再 `| tee`——两层 tee 抢同一个
文件会把先写的那半覆盖掉。`R67_LOGDIR` 不给就用进程启动时刻自己开一个轮次目录；给了就要每一格
都落进那一份，否则读数会分在两个"同一轮"里（本卡踩过：cell 日志与 driver.out 各在一个时间戳下）。

身份行（`基线字节`）由 `battlog.identity()` 现读：它报的是**影子克隆那一笔 HEAD**＋`overlay=` 现场
数出的"`LANE_FILES` 名单里有几份还没入库"。这一行必须在 `tee_to()` 之后发（早于 tee 只进终端，
`scripts/check-battery-identity.py` 的 A2/A4 就是拦这个）。

影子树不是自己长出来的：它是 `git clone --shared` 出来的基线＋本泳道现取的改动。
同步只按本文件里的 `LANE_FILES` 名单逐份 `cp` 并比 md5（`--check` 会现数现印每份的 md5），
不是把 `git status` 里的全部 `.go` 搬过来——共享工作树上别的泳道的未提交改动也在那份名单里，
带进影子树就等于拿别人树上的代码判自己的牙（这一族失效表现为"绿得莫名其妙"，最难归因）。
DB/Redis 凭证由 `R65_RUNNER` 那个包装脚本现取仓库根 `.env`（未跟踪文件，影子树里没有），
它只 export 环境、不落盘也不打印取值。

每刀的原始 go test 输出、控制组读数、`98-tally.log`（逐刀判定行）与 `00-run.log`（驱动自己的
判定行，含身份行）落在 `docs/superpowers/specs/ledger/logs/TGGATE/<运行时间戳>/`
（轮次目录不复用，重跑不覆盖上一轮）。

三条自己的对账：
  - **控制组现测**：放刀前先跑一次全绿，用例名单与数量从这次运行里取，不写死常量
    （共享树上别人加一格用例就会把写死的分母带歪）；
  - **每刀断言 `PASS+FAIL+SKIP == 控制组数`**：少一格就是装架坏了（一条用例 panic 会带走
    整个测试二进制，那种"红"要先证明红在被注码的那条腿上，见 `KILLED-PANIC`）；
  - **复原比 md5**：每刀跑完立刻用内存快照写回并复核，末尾再逐文件对账一次，
    残骸按 `RESTORE-LEFT` 计入退出码（下一轮的基线会被它读成已改坏的码）。
"""
import hashlib
import os
import re
import subprocess
import sys
import time

from battlog import identity, tee_to

# 影子克隆的根（`git -C` 的对象）与 `LANE_FILES` 里那些文件**相对该根**的路径：身份行数
# "覆盖进克隆的来树字节"要用克隆自己的 git 读，不能拿活树的 git 数——两边未入库的份数是两回事。
SHADOW_US = os.environ.get("R65_SHADOW", "/tmp/r65-shadow/hivemtk/user-server")
SHADOW_ROOT = os.path.dirname(SHADOW_US)
RUNNER = os.environ.get("R65_RUNNER", "/tmp/r65.sh")
# 仓库根＝本脚本的上两级（`<ws>/hivemtk`）。活树的取证落点必须拼在这棵树的根上：
# 影子克隆在 `/tmp`，把产物写进它里面就等于没写（下一轮读不到，且 `check-battery-identity`
# 的 A4 只认仓库树里的轮次目录）。
REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LIVE_US = os.path.join(REPO_ROOT, "user-server")
LOGROOT = os.path.join(REPO_ROOT, "docs/superpowers/specs/ledger/logs/TGGATE")
LOGDIR = os.environ.get("R67_LOGDIR") or os.path.join(LOGROOT, time.strftime("%Y%m%d-%H%M%S"))
os.makedirs(LOGDIR, exist_ok=True)

TESTS = [
    # service
    "TestSweepOnceRunsOnlyForLeaseHolder",
    "TestGateSweeperTickLoopTakesAndReleasesLease",
    "TestStartGateSweeperIdempotentAndStopSafe",
    "TestGateSweeperSelfHealsAfterTickPanic",
    "TestSweeperPanicRestartSkippedDuringShutdown",
    "TestRecoverStalledNeverRebroadcastsDeliveredWelcome",
    "TestRecoverStalledResendsOnlyWhenNeverDelivered",
    "TestRecoverStalledCountsFailedAttemptsAndStopsAtCap",
    "TestRecoverStalledSkipsRowStillInsideInRequestWindow",
    "TestRecoverStalledResendsAfterInRequestWindowPasses",
    "TestRecoverStalledResendsImmediatelyAfterRequestPathHandedOff",
    "TestRecoverStalledStillPicksRowsWithoutUpdatedAt",
    "TestHandleNewMembersRecordsDeliveryOutcome",
    "TestHandleNewMembersReJoinResetsWelcomeState",
    "TestHandleNewMembersFailedSendStaysUndelivered",
    "TestSweepExpiredLeavesNeverNotifiedMember",
    "TestSweepExpiredGivesOneTTLGrace",
    "TestSweepExpiredRemovesAfterGraceAndKeepsRejoinOpen",
    "TestSweepExpiredDeclinesJoinRequest",
    "TestSweepExpiredKeepsRetryWhenBanFails",
    "TestSweepExpiredSkipsAuthorizedMember",
    "TestSweepExpiredFilter",
    # repository
    "TestCronJobLease_HoldAndRelease",
    "TestCronJobLease_RejectsEmptyArgs",
    "TestTelegramMember_ListStalledRestricted",
    "TestTelegramMember_ListStalledRestrictedIdleWindow",
    "TestTelegramMember_UpsertResetsWelcomeState",
    "TestTelegramMember_WelcomeStateGuards",
    "TestTelegramMember_ClaimStalledResend",
    # cmd/api
    "TestGateSweeperPairedOnShutdown",
]
PACKAGES = ["./internal/service/", "./internal/repository/", "./cmd/api/"]
# 重名必须当场炸：`-run` 的交替式重复一项不影响 go test 跑几格，却让控制组的分母（len(TESTS)）
# 比真跑数大 ⇒ `ran != len(TESTS)` 把全绿的控制组判成 NOT-GREEN，红因读起来像"缺用例"。
if len(TESTS) != len(set(TESTS)):
    _dup = [t for t in TESTS if TESTS.count(t) > 1]
    sys.exit("TESTS 有重名（去重后再跑）: %s" % sorted(set(_dup)))
RUN_RE = "|".join(TESTS)

SVC = "internal/service/telegram_gate.go"
REPO = "internal/repository/telegram_gate.go"
LEASE = "internal/repository/cron_job_lease.go"
MAIN = "cmd/api/main.go"

# 已登记的纵深防御：这一格的判据在数据路径上被上一道谓词先拦下，单线程用例合成不出
# 需要并发才能观测的窗口，所以"没有用例红"是这套设计的正确读数，不是漏网：
#   M18 —— SweepExpired 的 fresh.Authorized 复查：ListExpired 只认 authorized=false 的行，
#          要红得合成出"列表读到写之间另一进程放了他"的窗口。
#   M02 —— RecoverStalled 的补发上限预检：同一行在下一笔就被 ClaimStalledResend 的
#          welcome_resends < ? 拦住（M22 正杀得了它），删掉预检只是多一次注定失败的认领，
#          群里的播报条数不变。预检仍要留着：它省掉一次 DB 往返，也让"Bot 不在群里"
#          这种持续故障在日志里点名，而不是藏在认领的 false 里。
DECLARED_SURVIVORS = {"M18", "M02"}

# 本泳道在影子树里应当存在的文件全集（份数由 `--check` 现数并逐份打印 md5，不写死在这里）。
# `--check` 逐份现数：缺一份就退出，
# 因为"影子树里根本没有这一格用例"会让 `-run` 命中 0 却照样退 0——那是假绿，不是通过。
LANE_FILES = [
    "cmd/api/main.go",
    "cmd/api/startup_order_test.go",
    "internal/model/cron_job_lease.go",
    "internal/model/telegram_gate.go",
    "internal/pkg/db/migrate.go",
    "internal/repository/cron_job_lease.go",
    "internal/repository/cron_job_lease_test.go",
    "internal/repository/telegram_gate.go",
    "internal/repository/telegram_gate_repo_test.go",
    "internal/repository/telegram_gate_stalled_idle_test.go",
    "internal/service/telegram_gate.go",
    "internal/service/telegram_gate_in_flight_test.go",
    "internal/service/telegram_gate_lease_test.go",
    "internal/service/telegram_gate_recover_stalled_test.go",
    "internal/service/telegram_gate_sweep_test.go",
    "internal/service/telegram_gate_test.go",
]

# (cell, file, old, new, expect_fail, what_the_branch_guards)
CELLS = [
    ("M01", REPO,
     '"join_status = ? AND authorized = ? AND welcome_sent_at IS NULL AND (expires_at IS NULL OR expires_at > ?)"+',
     '"join_status = ? AND authorized = ? AND (expires_at IS NULL OR expires_at > ?)"+',
     {"TestTelegramMember_ListStalledRestricted"},
     "补偿集合只取「提示从未送达」——线上 219 条重播的入口（服务侧那枚用例改由认领谓词守，见 M20）"),
    ("M02", SVC,
     "if m.WelcomeResends >= tgGateWelcomeResendMax {",
     "if m.WelcomeResends >= tgGateWelcomeResendMax && false {",
     {"TestRecoverStalledResendsOnlyWhenNeverDelivered", "TestRecoverStalledCountsFailedAttemptsAndStopsAtCap"},
     "补发次数上限（发不出去就别刷屏）"),
    ("M03", SVC,
     '\t\t\t\ttgGateWelcomeResendMax, m.ChatID, m.UserID)\n\t\t\tcontinue',
     '\t\t\t\ttgGateWelcomeResendMax, m.ChatID, m.UserID)\n\t\t\ts.retimeGateWindow(ctx, m, ttl)\n\t\t\tcontinue',
     {"TestRecoverStalledCountsFailedAttemptsAndStopsAtCap"},
     "到上限不再把 expires_at 顶回去（否则 TTL 对这人永不成熟）"),
    ("M04", SVC,
     "if fresh.WelcomeSentAt == nil {",
     "if fresh.WelcomeSentAt == nil && false {",
     {"TestSweepExpiredLeavesNeverNotifiedMember"},
     "决策一第 1 层：没被告知的人不罚"),
    ("M05", SVC,
     "if fresh.ExpiresAt != nil && now.Sub(*fresh.ExpiresAt) < grace {",
     "if fresh.ExpiresAt != nil && grace < 0 {",
     {"TestSweepExpiredGivesOneTTLGrace"},
     "决策一第 2 层：到期再宽限一个 TTL"),
    ("M06", SVC,
     '"[TG-Gate] 踢出超时成员失败 chat=%s user=%d: %v", fresh.ChatID, userID, err)\n\t\t\t\tcontinue // TG',
     '"[TG-Gate] 踢出超时成员失败 chat=%s user=%d: %v", fresh.ChatID, userID, err)\n\t\t\t\t_ = err // TG',
     {"TestSweepExpiredKeepsRetryWhenBanFails"},
     "TG 侧没动成功就不许把台账落成 kicked"),
    ("M07", REPO,
     '\t\tWhere("id = ? AND authorized = ?", memberID, false).\n\t\tUpdates(map[string]any{"join_status": model.TGMemberKicked,',
     '\t\tWhere("id = ?", memberID).\n\t\tUpdates(map[string]any{"join_status": model.TGMemberKicked,',
     {"TestTelegramMember_WelcomeStateGuards"},
     "窄更新的 authorized=false 守卫（read→write 窗口里刚过审的人不被回滚）"),
    ("M08", SVC,
     's.bumpWelcomeResend(ctx, accountID, chatIDStr, strconv.FormatInt(m.ID, 10))\n\t\t\tcontinue\n\t\t}\n\t\ts.markWelcomeSent(',
     's.bumpWelcomeResend(ctx, accountID, chatIDStr, strconv.FormatInt(m.ID, 10))\n\t\t}\n\t\ts.markWelcomeSent(',
     {"TestHandleNewMembersFailedSendStaysUndelivered"},
     "发送失败不得登记成送达（否则补偿循环与免罚判据同时关掉）"),
    ("M09", SVC,
     "\tif !held {\n\t\treturn 0, false, nil\n\t}",
     "\tif !held && false {\n\t\treturn 0, false, nil\n\t}",
     {"TestSweepOnceRunsOnlyForLeaseHolder"},
     "决策三：抢不到租约的一轮零 TG 调用"),
    ("M10", SVC,
     "\tif r.svc != nil && r.svc.leaseRepo != nil {\n\t\tif err := r.svc.leaseRepo.Release(",
     "\tif r.svc != nil && false && r.svc.leaseRepo != nil {\n\t\tif err := r.svc.leaseRepo.Release(",
     {"TestGateSweeperTickLoopTakesAndReleasesLease"},
     "停机交回租约（否则下一任最多等陈旧窗口才接手）"),
    ("M11", LEASE,
     "\t\t         OR heartbeat_at < ?)`,",
     "\t\t         OR (heartbeat_at < ? AND 1 = 0))`,",
     {"TestCronJobLease_HoldAndRelease"},
     "陈旧心跳可被接管（僵尸持有者不锁死全场）"),
    ("M12", SVC,
     "\tif gateSweeper != nil {\n\t\treturn // 幂等",
     "\tif gateSweeper != nil && false {\n\t\treturn // 幂等",
     {"TestStartGateSweeperIdempotentAndStopSafe"},
     "重复启动不换实例（两台同扫＝双份播报）"),
    ("M13", LEASE,
     '\tif jobName == "" || workerID == "" {\n\t\treturn false, ErrInvalidCronLeaseArgs\n\t}',
     '\tif (jobName == "" || workerID == "") && false {\n\t\treturn false, ErrInvalidCronLeaseArgs\n\t}',
     {"TestCronJobLease_RejectsEmptyArgs"},
     "空参数拒绝（空 job_name 会与他人租约互相顶掉）"),
    ("M14", MAIN,
     "defer service.StopGateSweeper(context.Background())",
     "service.StopGateSweeper(context.Background())",
     {"TestGateSweeperPairedOnShutdown"},
     "关停钩子真的挂在 defer 上（形状判据）"),
    ("M15", SVC,
     'logger.Errorf("[TG-Gate] TTL 清扫协程 panic 重启: %v", rec)\n\t\tr.restartAfterPanic(ctx)',
     'logger.Errorf("[TG-Gate] TTL 清扫协程 panic 重启: %v", rec)',
     {"TestGateSweeperSelfHealsAfterTickPanic"},
     "tick 里 panic 后自愈复活（静默失效）"),
    ("M16", SVC,
     "\tif ctx.Err() != nil {\n\t\treturn\n\t}\n\tgateSweeperMu.Lock()",
     "\tif ctx.Err() != nil && false {\n\t\treturn\n\t}\n\tgateSweeperMu.Lock()",
     {"TestSweeperPanicRestartSkippedDuringShutdown"},
     "关停窗口里 panic 不许复活（进程已停却还在踢人）"),
    ("M17", REPO,
     '"verify_token", "expires_at", "welcome_sent_at", "welcome_resends", "updated_at"',
     '"verify_token", "expires_at", "updated_at"',
     {"TestTelegramMember_UpsertResetsWelcomeState", "TestHandleNewMembersReJoinResetsWelcomeState"},
     "重新入群清空上一段送达状态"),
    ("M18", SVC,
     "\t\tif fresh.Authorized {\n\t\t\tcontinue // 已过审",
     "\t\tif fresh.Authorized && false {\n\t\t\tcontinue // 已过审",
     {"TestSweepExpiredSkipsAuthorizedMember"},
     "已过审成员不再被清扫动手"),
    ("M19", REPO,
     '"authorized = ? AND join_status IN ? AND expires_at IS NOT NULL AND expires_at < ?",',
     '"(authorized = ? OR 1 = 1) AND join_status IN ? AND expires_at IS NOT NULL AND expires_at < ?",',
     {"TestSweepExpiredFilter"},
     "过期集合只认未授权的人"),
    # 认领（ClaimStalledResend）是「补发上限」与「同一行一轮只发一条」的落库执行点：
    # 三条谓词各拦一种失效，逐格拆刀，不并成一枚"删掉整个 WHERE"的粗变异。
    ("M20", REPO,
     '"id = ? AND welcome_sent_at IS NULL AND welcome_resends = ? AND welcome_resends < ?", memberID, expectedResends, maxResends)',
     '"id = ? AND welcome_resends = ? AND welcome_resends < ?", memberID, expectedResends, maxResends)',
     {"TestTelegramMember_ClaimStalledResend"},
     "已送达的行不再认领"),
    ("M21", REPO,
     '"id = ? AND welcome_sent_at IS NULL AND welcome_resends = ? AND welcome_resends < ?", memberID, expectedResends, maxResends)',
     '"id = ? AND welcome_sent_at IS NULL AND welcome_resends < ?", memberID, maxResends)',
     {"TestTelegramMember_ClaimStalledResend"},
     "乐观锁：拿着过期期望值的第二个认领者必须失败"),
    ("M22", REPO,
     '"id = ? AND welcome_sent_at IS NULL AND welcome_resends = ? AND welcome_resends < ?", memberID, expectedResends, maxResends)',
     '"id = ? AND welcome_sent_at IS NULL AND welcome_resends = ?", memberID, expectedResends)',
     {"TestTelegramMember_ClaimStalledResend"},
     "到上限的行不再认领（发不出去就别刷屏）"),
    # 安静窗口（"请求内路径已经不在这一行上"）的三个 OR 分支各自拦一种行，逐格拆刀：
    # 并成一枚"删掉整个窗口谓词"只测得出"有没有这句"，测不出"这三条各管谁"。
    ("M23", REPO,
     '" AND (welcome_resends > 0 OR updated_at IS NULL OR updated_at <= ?)",',
     '" AND (updated_at IS NULL OR updated_at <= ?)",',
     {"TestTelegramMember_ListStalledRestrictedIdleWindow",
      "TestRecoverStalledResendsImmediatelyAfterRequestPathHandedOff"},
     "请求内路径计过失败并交棒的行当轮就补（延迟不等于取消）"),
    ("M24", REPO,
     '" AND (welcome_resends > 0 OR updated_at IS NULL OR updated_at <= ?)",',
     '" AND (welcome_resends > 0 OR updated_at <= ?)",',
     {"TestTelegramMember_ListStalledRestrictedIdleWindow",
      "TestRecoverStalledStillPicksRowsWithoutUpdatedAt"},
     "没有写入时刻的行（手工/迁移写入）视为早已安静，照补"),
    ("M25", REPO,
     '" AND (welcome_resends > 0 OR updated_at IS NULL OR updated_at <= ?)",',
     '" AND (welcome_resends > 0 OR updated_at IS NULL OR updated_at >= ?)",',
     {"TestTelegramMember_ListStalledRestrictedIdleWindow",
      "TestRecoverStalledSkipsRowStillInsideInRequestWindow",
      "TestRecoverStalledResendsAfterInRequestWindowPasses"},
     "安静窗口方向：握着的那行不碰、跨过窗口的那行才捞（反了就是把刹车装成正油门）"),
    ("M26", SVC,
     "s.memberRepo.ListStalledRestricted(ctx, now, now.Add(-tgGateInFlightQuiet), limit)",
     "s.memberRepo.ListStalledRestricted(ctx, now, now, limit)",
     {"TestRecoverStalledSkipsRowStillInsideInRequestWindow"},
     "调用点传进来的窗口长度（传成 now 等于窗口为零，抢请求内路径那一行）"),
]


def md5p(path):
    with open(path, "rb") as fh:
        return hashlib.md5(fh.read()).hexdigest()


def readb(path):
    with open(path, "rb") as fh:
        return fh.read()


def writeb(path, data):
    with open(path, "wb") as fh:
        fh.write(data)


TOP = re.compile(r"^--- (PASS|FAIL|SKIP):\s+(\S+)")


def run_suite(tag):
    env = dict(os.environ)
    env["DEVELOPER_DIR"] = "/Library/Developer/CommandLineTools"
    cmd = ["bash", RUNNER, "go", "test", "-v", "-count=1", "-run", RUN_RE] + PACKAGES
    t0 = time.time()
    p = subprocess.run(cmd, capture_output=True, text=True, env=env, cwd=SHADOW_US)
    out = p.stdout + p.stderr
    log = os.path.join(LOGDIR, tag + ".log")
    with open(log, "w") as fh:
        fh.write("$ %s\n[rc=%d] [%.1fs]\n\n" % (" ".join(cmd), p.returncode, time.time() - t0, ))
        fh.write(out)
    passed, failed, skipped = {}, {}, {}
    for line in out.splitlines():
        m = TOP.match(line)
        if not m:
            continue
        kind, name = m.group(1), m.group(2)
        if name in TESTS:
            {"PASS": passed, "FAIL": failed, "SKIP": skipped}[kind][name] = True
    return {"rc": p.returncode, "pass": set(passed), "fail": set(failed), "skip": set(skipped),
            "out": out, "log": log}


def redcause(res, names):
    lines = res["out"].splitlines()
    picked = []
    for i, ln in enumerate(lines):
        if ln.startswith("--- FAIL: ") and any(n in ln.split() for n in names):
            picked.append(ln)
            for j in range(i + 1, min(i + 7, len(lines))):
                if lines[j].startswith("--- ") or lines[j].startswith("=== "):
                    break
                picked.append(lines[j])
    return "\n".join(picked)


def lane_parity():
    """影子克隆里的本泳道文件必须与活树逐字相同，否则读数测的不是要交付的那份字节。

    这一格是身份行的兑现处：`基线字节` 那行说"本轮读克隆 HEAD＋来树覆盖字节"，只有两边
    md5 逐份相同才成立；否则跑的是克隆里**上一批**的旧字节（改过用例却没同步，就是这种
    失效的形状），全绿也只是旧判据的绿。
    """
    out = []
    for f in LANE_FILES:
        live, shadow = os.path.join(LIVE_US, f), os.path.join(SHADOW_US, f)
        if not os.path.isfile(live):
            out.append("%s 活树缺件" % f)
        elif not os.path.isfile(shadow):
            out.append("%s 影子树缺件" % f)
        elif md5p(live) != md5p(shadow):
            out.append("%s md5 不一致（影子树是旧字节）" % f)
    return out


def main():
    mode = sys.argv[1] if len(sys.argv) > 1 else "run"
    checking = mode in ("check", "--check")
    tee_to(os.path.join(LOGDIR, "00-check.log" if checking else "00-run.log"))
    print("SHADOW " + SHADOW_US)
    print("LOGDIR " + LOGDIR)
    # 身份行：这一轮读的究竟是哪一笔字节。影子克隆的 HEAD 只是基线，`LANE_FILES` 那批是**盖
    # 进去的未入库改动**，所以份数必须由 `overlay=` 现测（写成声明就成了假身份行）。
    identity(SHADOW_ROOT, label="基线字节",
             overlay=["user-server/" + f for f in LANE_FILES],
             extra="｜本轮读影子 `--shared` 克隆的 HEAD＋上面现测的那批来树覆盖字节；"
                   "活树同批文件的 md5 由 `--check` 逐份印出，两份逐字相同才算这一轮的数")
    identity(REPO_ROOT, label="来源活树",
             overlay=["user-server/" + f for f in LANE_FILES],
             extra="｜上面 overlay 名单的出处；本电池不在活树装架，只把这里读出的字节盖进影子克隆")
    diff = lane_parity()
    if diff and not checking:
        sys.exit("影子树与本泳道字节不一致（先按 LANE_FILES 重新同步再跑，"
                 "否则这一轮读的是旧字节）:\n  " + "\n  ".join(diff))
    # 快照：内存里存原始字节，复原只认这份
    snap = {}
    for f in {c[1] for c in CELLS}:
        p = os.path.join(SHADOW_US, f)
        if not os.path.isfile(p):
            sys.exit("影子树里缺 " + f + "：这一刀无处落，先按 git status 现取本泳道全量文件同步进去")
        snap[f] = readb(p)

    if checking:
        def _m(p):
            return md5p(p) if os.path.isfile(p) else "缺"
        print("LANE_FILES 现数 %d 份（同＝活树与影子树逐字相同）：" % len(LANE_FILES))
        for f in LANE_FILES:
            lv, sh = _m(os.path.join(LIVE_US, f)), _m(os.path.join(SHADOW_US, f))
            print("  %s %s  %s" % ("同" if lv == sh else "异", lv, f))
            if lv != sh:
                print("       影子 " + sh)
        bad = ["PARITY " + d for d in diff]
        for cell, f, old, new, exp, why in CELLS:
            cur = snap[f]
            n = cur.count(old.encode())
            if n != 1:
                bad.append("%s 锚点命中 %d 次（期望 1）: %s" % (cell, n, f))
            if old.encode() == new.encode():
                bad.append("%s 是空变异" % cell)
        for line in bad:
            print("CHECK-BAD " + line)
        print("parity bad=%d anchors=%d anchor bad=%d"
              % (len(diff), len(CELLS), len(bad) - len(diff)))
        return 1 if bad else 0

    ctrl = run_suite("CT00-control")
    ran = ctrl["pass"] | ctrl["fail"] | ctrl["skip"]
    print("CONTROL rc=%d pass=%d fail=%d skip=%d ran=%d/%d"
          % (ctrl["rc"], len(ctrl["pass"]), len(ctrl["fail"]), len(ctrl["skip"]), len(ran), len(TESTS)))
    if ctrl["fail"] or ctrl["skip"] or len(ran) != len(TESTS):
        print("CONTROL-NOT-GREEN fail=%s skip=%s missing=%s"
              % (sorted(ctrl["fail"]), sorted(ctrl["skip"]), sorted(set(TESTS) - ran)))
        print("红因:\n" + redcause(ctrl, ctrl["fail"]))
        return 2
    print("CONTROL_LOG " + ctrl["log"])

    tally = []
    for cell, f, old, new, exp, why in CELLS:
        path = os.path.join(SHADOW_US, f)
        mutated = snap[f].replace(old.encode(), new.encode(), 1)
        writeb(path, mutated)
        res = run_suite(cell)
        writeb(path, snap[f])
        back = md5p(path)
        ok_restore = back == hashlib.md5(snap[f]).hexdigest()
        still = mutated.count(new.encode())
        del still
        ran_names = res["pass"] | res["fail"] | res["skip"]
        missing = set(TESTS) - ran_names
        crashed = "panic:" in res["out"]
        if "[build failed]" in res["out"] or "vet:" in res["out"]:
            verdict = "BROKEN"
        elif not exp <= res["fail"]:
            verdict = "SURVIVED" if cell not in DECLARED_SURVIVORS else "DECLARED-SURVIVOR"
        elif crashed and missing:
            verdict = "KILLED-PANIC"
        elif missing:
            verdict = "TALLY-BROKEN"
        else:
            verdict = "KILLED"
        extra = sorted(res["fail"] - exp)
        tally.append((cell, verdict, sorted(exp & res["fail"]), extra, ok_restore, res["log"]))
        print("%-4s %-9s killed=%s extra=%s restored=%s ran=%d/%d"
              % (cell, verdict, ",".join(sorted(exp & res["fail"])) or "-",
                 ",".join(extra) or "-", ok_restore, len(ran_names), len(TESTS)))
        if verdict in ("SURVIVED", "BROKEN"):
            head = res["out"]
            print("   ---- 取证（前 25 行判定行）----")
            for ln in [l for l in head.splitlines() if l.startswith(("--- ", "ok ", "FAIL", "?  "))][:25]:
                print("   " + ln)

    print("\n=== 复原对账（影子树的每一刀目标文件必须逐字回到放刀前）===")
    left = 0
    for f, data in sorted(snap.items()):
        p = os.path.join(SHADOW_US, f)
        same = md5p(p) == hashlib.md5(data).hexdigest()
        if not same:
            left += 1
        print("%s %s" % ("OK  " if same else "LEFT", f))
    print("RESTORE-LEFT=%d" % left)
    kinds = [t[1] for t in tally]
    print("\nBATTERY cells=%d KILLED=%d KILLED-PANIC=%d DECLARED-SURVIVOR=%d SURVIVED=%d BROKEN=%d"
          % (len(tally), kinds.count("KILLED"), kinds.count("KILLED-PANIC"),
             kinds.count("DECLARED-SURVIVOR"), kinds.count("SURVIVED"),
             kinds.count("BROKEN") + kinds.count("TALLY-BROKEN")))
    with open(os.path.join(LOGDIR, "98-tally.log"), "w") as fh:
        for t in tally:
            fh.write("%s %s killed=%s extra=%s restored=%s log=%s\n"
                     % (t[0], t[1], ",".join(t[2]) or "-", ",".join(t[3]) or "-", t[4], t[5]))
    bad = kinds.count("SURVIVED") + kinds.count("BROKEN") + kinds.count("TALLY-BROKEN") + left
    return 0 if bad == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
