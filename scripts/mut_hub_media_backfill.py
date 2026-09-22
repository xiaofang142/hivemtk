#!/usr/bin/env python3
"""批23（入站媒体回填下沉 repository）变异电池：逐刀验"回填"这件事的每一面都有腿。

为什么这一批要有常驻电池（而不是"改完跑绿就算"）：
- 起因是架构门的一条红（`dingtalk_media.go:191,204` service 直接 `s.db.WithContext`）。
  把它下沉成 `MessageHubRepository.SetInboundMediaURLs` 之后，"门绿了"只证明**形状**合规，
  不证明**语义**没在搬家过程中丢掉——搬家最容易丢的恰好是原注释里写明的三条承诺：
  ① 一次写全（多图不能互相覆盖）② 只碰请求账号那一行 ③ 保住 Extra 里的既有键。
- 更要紧的是：这次搬家顺手把一处**归因错误**改对了。原实现把"读库真失败"和"没有这一行"
  合并成同一条日志（`找不到 hub 行`），正是本泳道批19d 点过的病（把内部读失败说成实体不存在）。
  新签名 `(found bool, err error)` 把两者分开 ⇒ 这个"分开"本身是新承诺，必须有腿，
  且必须证明腿有牙（R4/R5 两刀：合起来是一种读数 ⇒ 各自应有一条腿红）。
- R1 这一刀还留了一条**判据之外的收获**：铺序改了才测得到。最初的用例把"不该被碰的行"
  铺在第二位，于是"丢掉 account 过滤"的变异会命中第一位（正是请求的那一行）、用例照绿——
  一条看起来在测跨账号串写的腿其实什么都不测。现在把该行铺在**第一**位（id 更小），
  任何丢了 `account_id` 的查询都会先撞上它，串写才看得见（口径：电池口径 23）。

二次审核收口（本轮）在八刀之上加了十刀，每刀都对应 B1 复验出来的一条具体失效形状：
- R8/S2/S4：回填的会话键。`message_hub` 唯一键是 (platform, msg_id, conversation_id)，
  而 (platform, account_id, msg_id) **不是**唯一键 ⇒ 三键 + First 会把媒体写进 id 更小的
  另一会话行（实测过：改之前的用例就把 URL 写进了 `conv_other`）。少一维、漏传、
  和调用点把两个同为 string 的键传反，是三格不同的刀，编译器只拦最后一种的一半。
- R9：装配漏 DB 句柄时报「没这一行」还是报「我做不了」。
- S5/S6/S7：两个失败分支各写一次 `continue`——只测一个，另一个被改成 `return` 无人报警；
  S7 钉的是"失败也不占位"（否则 media_url 是一个永远打不开的 downloadCode）。
- S8：同一下载码出现多次 ⇒ 同一文件转存三次、Extra.media_urls 三条一样的 URL。
- S9/S10：预签名链接的查询串与 accessToken 泄进错误串（错误串会随 warn 日志长期留存）。
  这两刀各自只红一条腿 ⇒ 两道脱敏是两条独立承诺，少一道就是另一种泄露。

口径（沿用批16/17/18/19x/20b/20c/20d/20f/22 电池）：
- 两个控制组（repository 5 腿 / service 4 腿）都必须 rc==0、settled==期望、skip==0、无红名；
- **每格都跑两个包**：仓库侧的一刀会同时红到 service 侧（service 用得到仓库方法），
  所以判据取两包红名的**并集恰好等于期望**——只跑一侧会把"连带面"记成"没有连带"；
- 每格断言两包各自 `settled == 该包控制组 settled`（一条用例 panic 会带走整个二进制）；
  会 panic 的那格（R6）把崩溃用 `recover` 关在本腿里，所以分母仍然可判；
- BUILD FAILED / panic / 红而没点名 一律 BROKEN，不计入杀掉；红必须读红因；
- 锚点命中恰好一次、只在私有 `--shared` 克隆里注码（共享工作树与并行会话同树），还原后逐文件比 md5；
- **逐格原始输出落到仓里的持久目录**（默认 `docs/superpowers/specs/ledger/logs/R22/`），
  不再只留 `/tmp`：本轮已经实测过一次 `/tmp` 里被引用为证据的克隆三分钟后就没了。

用法：python3 scripts/mut_hub_media_backfill.py [--keep] [--clone DIR] [--logs DIR] [--cells R1,S1]
"""
from __future__ import annotations

import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
US = "user-server"
REPO_REL = f"{US}/internal/repository/message_hub_inbox_media.go"
MEDIA_REL = f"{US}/internal/service/dingtalk_media.go"
APP_REL = f"{US}/internal/service/dingtalk_app.go"
LANE_PATHS = [f"{US}/internal"]
DEFAULT_LOGS = "docs/superpowers/specs/ledger/logs/R22"
ANSI = re.compile(r"\x1b\[[0-9;]*m")

PKG_REPO = "./internal/repository/"
PKG_SVC = "./internal/service/"
R_REPO = "TestSetInboundMediaURLs"
# service 侧过滤器要同时罩住两个家族：M01（下载转存回填）与 batchJ（两条 HTTP 腿的报错形状）。
# 只跑 M01 会把"日志脱敏"那一格的判据留在门外——batchJ 的用例才是它们唯一的腿。
R_SVC = "TestM01_DingTalk|TestBatchJ_DingTalk"

L_MULTI = "TestSetInboundMediaURLsWritesFirstAndAll"
L_SCOPE = "TestSetInboundMediaURLsScopesByAccount"
L_CONV = "TestSetInboundMediaURLsScopesByConversation"
L_NILH = "TestSetInboundMediaURLsNilHandleIsErrorNotNotFound"
L_MISS = "TestSetInboundMediaURLsMissingRowIsNotFoundNotError"
L_READERR = "TestSetInboundMediaURLsReadFailureIsNotReportedAsNotFound"
L_EMPTY = "TestSetInboundMediaURLsEmptyURLsWriteNothing"
S_STORE = "TestM01_DingTalkMediaIsStoredAndBackfilled"
S_RICH = "TestM01_DingTalkRichTextStoresEveryPicture"
S_CANCEL = "TestM01_DingTalkMediaSurvivesRequestCancel"
S_PF = "TestM01_DingTalkPartialTransferFailureKeepsSuccessfulPicture"
S_PS = "TestM01_DingTalkPartialStoreFailureKeepsSuccessfulPicture"
S_AF = "TestM01_DingTalkAllTransfersFailedWriteNoMedia"
S_DP = "TestM01_DingTalkSameDownloadCodeTransfersOnce"
S_URL = "TestBatchJ_DingTalkPresignedURLQueryNeverReachesError"
S_TOK = "TestBatchJ_DingTalkAccessTokenNeverReachesParseError"
S_ALL = {S_STORE, S_RICH, S_CANCEL}
# 回填整条断掉（键传错 / 装配漏线）时，红的不止上面三条：下面三条同样以"成功回填"为前置
# （S_PF/S_PS 断言首张成功图要落进 media_url，S_DP 断言去重后的那张要落进去）。
# 只有 S_AF 例外——它断言的是"什么都没写"，回填坏掉时它反而更绿，所以它不在名单里。
S_BROKEN_BACKFILL = S_ALL | {S_PF, S_PS, S_DP}


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(p: Path) -> str:
    return p.read_text(encoding="utf-8")


WHERE4 = ('\t\tWhere("platform = ? AND account_id = ? AND conversation_id = ? AND msg_id = ?",\n'
          '\t\t\tplatform, accountID, conversationID, msgID).')


def cuts():
    """十八刀。(code, desc, rel, old, new, 期望红名并集)

    期望是先推演后跑的：每格只应红在"这一刀破坏了哪条承诺"所对应的那几条腿上，
    多一条是连带面（要读红因定性），少一条是判据没牙。

    本轮（二次审核收口）在批23 的八刀上加了八刀，都对应 B1 复验出来的具体失效形状：
    R8 会话键、R9 手工接线、S2 会话键没传下来、S4 调用点两键传反、S5/S6 两个失败分支各自
    被改成整批放弃、S7 失败也占位、S8 下载码重复、S9/S10 两类凭证值泄进日志。
    """
    return [
        ("R1", "回填不锁 account_id（跨账号串写）", REPO_REL,
         WHERE4,
         '\t\tWhere("platform = ? AND conversation_id = ? AND msg_id = ?",\n'
         '\t\t\tplatform, conversationID, msgID).',
         {L_SCOPE}),
        ("R8", "回填不锁 conversation_id（串到同 msg_id 的另一会话）", REPO_REL,
         WHERE4,
         '\t\tWhere("platform = ? AND account_id = ? AND msg_id = ?",\n'
         '\t\t\tplatform, accountID, msgID).',
         {L_CONV}),
        ("R9", "把手工接线失败（无 DB 句柄）说成「没这一行」", REPO_REL,
         '\t\treturn false, errors.New("message hub media backfill: repository has no DB handle")',
         '\t\treturn false, nil',
         {L_NILH}),
        ("R2", "Extra 整列替换（抹掉入站时落的既有键）", REPO_REL,
         '\textra := mergeHubExtra(hub.Extra, map[string]any{"media_urls": urls})',
         '\textra := model.JSONMap{"media_urls": urls}',
         {L_MULTI, S_STORE}),
        ("R3", "media_url 写末位而不是首图", REPO_REL,
         'map[string]any{"media_url": urls[0], "extra": extra}',
         'map[string]any{"media_url": urls[len(urls)-1], "extra": extra}',
         {L_MULTI, S_RICH}),
        ("R4", "「没有这一行」也当错误返回（和读失败并成一类）", REPO_REL,
         "\t\tif errors.Is(err, gorm.ErrRecordNotFound) {\n\t\t\treturn false, nil\n\t\t}",
         "\t\tif errors.Is(err, gorm.ErrRecordNotFound) && false {\n\t\t\treturn false, nil\n\t\t}",
         {L_MISS}),
        ("R5", "读库真失败被吞成「没找到」（批19d 那一类归因错）", REPO_REL,
         "\t\treturn false, err\n\t}",
         "\t\treturn false, nil\n\t}",
         {L_READERR}),
        ("R6", "摘掉空 urls 守卫（写侧 urls[0] 越界）", REPO_REL,
         'if platform == "" || accountID == "" || conversationID == "" || msgID == "" || len(urls) == 0 {',
         'if platform == "" || accountID == "" || conversationID == "" || msgID == "" || len(urls) == 0 && false {',
         {L_EMPTY}),
        ("S1", "service 传错 platform（回填永远找不到行）", MEDIA_REL,
         's.hubRepo.SetInboundMediaURLs(ctx, "dingtalk", accID, conversationID, hubMsgID, urls)',
         's.hubRepo.SetInboundMediaURLs(ctx, "dingtalk_wrong", accID, conversationID, hubMsgID, urls)',
         S_BROKEN_BACKFILL),
        ("S2", "service 漏传会话键（四键退化成三键）", MEDIA_REL,
         's.hubRepo.SetInboundMediaURLs(ctx, "dingtalk", accID, conversationID, hubMsgID, urls)',
         's.hubRepo.SetInboundMediaURLs(ctx, "dingtalk", accID, "", hubMsgID, urls)',
         S_BROKEN_BACKFILL),
        ("S4", "调用点把 msg_id 与会话键传反（同为 string，编译器不拦）", APP_REL,
         '\t\ts.persistDingTalkMediaAsync(ctx, accountID, event.EventID, event.ConversationID,',
         '\t\ts.persistDingTalkMediaAsync(ctx, accountID, event.ConversationID, event.EventID,',
         S_BROKEN_BACKFILL),
        ("S3", "构造函数不注入 hubRepo（装配漏一行）", APP_REL,
         "\t\thubRepo:    repository.NewMessageHubRepositoryWithDB(db),",
         "\t\thubRepo:    nil,",
         S_BROKEN_BACKFILL),
        ("S5", "第一张下载失败即放弃整批", MEDIA_REL,
         '\t\t\t\tMsg("[DingTalk] 媒体下载失败（占位符保留）")\n\t\t\t\tcontinue',
         '\t\t\t\tMsg("[DingTalk] 媒体下载失败（占位符保留）")\n\t\t\t\treturn',
         {S_PF, S_AF}),
        ("S6", "第一张转存失败即放弃整批", MEDIA_REL,
         '\t\t\t\tlogger.Ctx(gctx).Warn().Err(serr).Str("media_code", code).Msg("[DingTalk] 媒体转存失败")\n\t\t\t\tcontinue',
         '\t\t\t\tlogger.Ctx(gctx).Warn().Err(serr).Str("media_code", code).Msg("[DingTalk] 媒体转存失败")\n\t\t\t\treturn',
         {S_PS}),
        ("S7", "下载失败也把 downloadCode 占位写进结果", MEDIA_REL,
         '\t\t\t\tMsg("[DingTalk] 媒体下载失败（占位符保留）")\n\t\t\t\tcontinue',
         '\t\t\t\tMsg("[DingTalk] 媒体下载失败（占位符保留）")\n\t\t\t\turls = append(urls, code)\n\t\t\t\tcontinue',
         {S_PF, S_AF}),
        ("S8", "下载码不去重（同一张图转存三次）", APP_REL,
         '\tseen := make(map[string]bool, len(c.RichText)+1)\n'
         '\tappendCode := func(code string) {\n'
         '\t\tif code == "" || seen[code] {\n'
         '\t\t\treturn\n'
         '\t\t}\n'
         '\t\tseen[code] = true\n'
         '\t\tcodes = append(codes, code)\n'
         '\t}',
         '\tappendCode := func(code string) {\n'
         '\t\tif code == "" {\n'
         '\t\t\treturn\n'
         '\t\t}\n'
         '\t\tcodes = append(codes, code)\n'
         '\t}',
         {S_DP}),
        ("S9", "预签名链接的查询串泄进错误（日志长期留存）", MEDIA_REL,
         '\ts := dtQueryRe.ReplaceAllString(string(b), "?<redacted>")',
         '\ts := string(b)',
         {S_URL}),
        ("S10", "accessToken 明文泄进错误（日志长期留存）", MEDIA_REL,
         '\ts = dtSecretValueRe.ReplaceAllStringFunc(s, func(m string) string {\n'
         '\t\tg := dtSecretValueRe.FindStringSubmatch(m)\n'
         '\t\tif g[2] == "" {\n'
         '\t\t\treturn m\n'
         '\t\t}\n'
         '\t\treturn g[1] + "<redacted>" + g[3]\n'
         '\t})',
         '',
         {S_TOK}),
    ]


def lane_overlays() -> tuple[list[str], list[str]]:
    """返回 (要覆盖进克隆的脏文件, 要在克隆里删掉的文件)。

    为什么删除也要搬：`--shared` 克隆 = HEAD + 覆盖，工作树里一条 ` D` 若不搬，
    克隆就在跑一份"文件还在"的树——它绿不能代表工作树绿（本轮实测：
    `user-server/internal/middleware/license_checker.go` 在工作树被删、克隆按 HEAD 仍带着它）。
    覆盖面也不限 `.go`：本批的判据有一部分住在 md/sh 里，只搬 .go 会让复验面窄于改动面。
    """
    r = subprocess.run(["git", "-C", str(ROOT), "status", "--porcelain", "-uall", "--"] + LANE_PATHS,
                       capture_output=True, text=True, timeout=300)
    if r.returncode != 0:
        raise SystemExit("git status 失败，拿不到脏文件清单：" + r.stderr[-200:])
    mods, dels = [], []
    for line in r.stdout.splitlines():
        st = line[:2]
        p = line[3:].split(" -> ")[-1].strip().strip('"')
        if "D" in st:
            dels.append(p)
        else:
            mods.append(p)
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
    hostenv = ROOT / US / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / US / ".env")
    return clone


def test_env(clone: Path) -> dict:
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOCACHE", "/tmp/gocache-b23mut")
    env.setdefault("POSTGRES_TEST_PORT", "8232")
    envf = clone / US / ".env"
    if envf.exists() and "POSTGRES_TEST_PASSWORD" not in os.environ:
        for line in read(envf).splitlines():
            if line.startswith("POSTGRES_PASSWORD="):
                env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
                break
    return env


def run_test(clone: Path, pkg: str, filt: str, env: dict) -> dict:
    p = subprocess.run(["go", "test", pkg, "-run", filt, "-count=1", "-v"],
                       cwd=clone / US, capture_output=True, text=True, timeout=2400, env=env)
    out = ANSI.sub("", p.stdout + p.stderr)
    top = lambda kind: len(re.findall(rf"^--- {kind}: ", out, re.M))
    redtop = sorted({m.split("/")[0] for m in re.findall(r"^--- FAIL: (\S+)", out, re.M)})
    return {"rc": p.returncode, "out": out,
            "settled": top("PASS") + top("FAIL") + top("SKIP"),
            "skipped": top("SKIP"), "red": redtop,
            "panicked": bool(re.search(r"^panic: |^fatal error: ", out, re.M)),
            "buildfailed": "[build failed]" in out or "undefined:" in out or "declared and not used" in out}


def causes(out: str) -> list[str]:
    keep = [l.strip()[:200] for l in out.splitlines()
            if re.match(r"^\s{2,}\S+\.go:\d+:", l) or "panic:" in l or "Error Test" in l]
    return keep[:8]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    ap.add_argument("--logs", default=DEFAULT_LOGS)
    ap.add_argument("--cells", default="", help="逗号分隔格名；修单格后复跑用，终态会写明本趟是子集")
    args = ap.parse_args()
    only = {c.strip() for c in args.cells.split(",") if c.strip()}
    unknown = only - {c[0] for c in cuts()}
    if only and unknown:
        raise SystemExit(f"--cells 里有不存在的格：{sorted(unknown)}")

    logs = ROOT / args.logs
    logs.mkdir(parents=True, exist_ok=True)
    tmp = Path(args.clone or tempfile.mkdtemp(prefix="b23mut-"))
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}\n逐格日志目录：{logs}")
    clone = prepare(tmp)
    env = test_env(clone)

    files = {rel: clone / rel for rel in (REPO_REL, MEDIA_REL, APP_REL)}
    originals = {rel: read(p) for rel, p in files.items()}
    basemd5 = {rel: md5_bytes(p) for rel, p in files.items()}

    controls = {}
    for label, pkg, filt in (("repository", PKG_REPO, R_REPO), ("service", PKG_SVC, R_SVC)):
        c = run_test(clone, pkg, filt, env)
        print(f"\n[{label}] 控制组 rc={c['rc']} settled={c['settled']} skip={c['skipped']} 红名={c['red']}")
        (logs / f"control_{label}.log").write_text(c["out"])
        if c["rc"] != 0 or c["settled"] == 0 or c["skipped"] > 0 or c["red"]:
            print(c["out"][-4000:])
            raise SystemExit(f"[{label}] 控制组不干净——后面所有红/绿都不可信")
        controls[pkg] = c["settled"]

    problems = []
    ran = 0
    for code, desc, rel, old, new, expect in cuts():
        if only and code not in only:
            continue
        ran += 1
        src = originals[rel]
        if src.count(old) != 1:
            problems.append(f"{code} 注码失效：锚点命中 {src.count(old)} != 1")
            print(f"{code:<4} {desc[:44]:<46} BROKEN=注码失效")
            continue
        files[rel].write_text(src.replace(old, new, 1))
        try:
            rs = {pkg: run_test(clone, pkg, filt, env)
                  for pkg, filt in ((PKG_REPO, R_REPO), (PKG_SVC, R_SVC))}
        finally:
            files[rel].write_text(src)
            if md5_bytes(files[rel]) != basemd5[rel]:
                raise SystemExit(f"{code} 还原后 md5 不一致，停机")

        raw = "\n".join(r["out"] for r in rs.values())
        (logs / f"{code}.log").write_text(raw)
        red = sorted(set().union(*[set(r["red"]) for r in rs.values()]))
        short = [r for r in rs.values() if r["rc"] != 0 and not r["red"]]
        if all(r["rc"] == 0 for r in rs.values()) and not red:
            v = "存活=洞"
        elif any(r["panicked"] or r["buildfailed"] for r in rs.values()) or short:
            v = "BROKEN=判不了"
        elif any(r["settled"] != controls[pkg] or r["skipped"] > 0 for pkg, r in rs.items()):
            v = "BROKEN=没跑完"
        elif set(red) != expect:
            v = "BROKEN=红集合不符"
        else:
            v = "杀掉"
        print(f"{code:<4} {desc[:44]:<46} {v:<14} settled={rs[PKG_REPO]['settled']}+"
              f"{rs[PKG_SVC]['settled']} skip={rs[PKG_REPO]['skipped']}+{rs[PKG_SVC]['skipped']} "
              f"红={','.join(red) or '—'}")
        if v != "杀掉":
            problems.append(f"{code} {v}：{desc}")
            if v == "BROKEN=红集合不符":
                print(f"     期望={sorted(expect)} 实际={red}")
            for c in causes(raw):
                print("     红因: " + c)
            if v == "存活=洞":
                print(raw[-2500:])

    scope = f"{ran}/{len(cuts())} 格" + (f"（--cells {','.join(sorted(only))}）" if only else "")
    print("\n===== 判定：" + (f"{scope}，逐格被杀，无存活" if not problems
                          else f"{scope}，{len(problems)} 格未杀/BROKEN：" + "; ".join(problems)) + " =====")
    print(f"（BROKEN=红集合不符 的红因已打进 {args.logs}/<格>.log，逐条读后再谈定性）")
    if not args.keep:
        shutil.rmtree(tmp, ignore_errors=True)
    else:
        print(f"保留作业目录：{tmp}")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
