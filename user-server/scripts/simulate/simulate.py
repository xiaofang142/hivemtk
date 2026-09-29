#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
模拟真实用户提问压测器（hivemtk user-server web_embed 渠道）

特性：
  - 真实昵称：从 names.json 的 姓+名+后缀 随机组合，贴近真实访客
  - 提问库可随时扩充：直接编辑 questions.json 往 questions 数组追加即可，无需改代码
  - 合理限速：全局并发上限 + 每条随机间隔，避免打垮真实服务（实测 AI 回复 ~47s）
  - 多端口/多方案：支持多个 app_key（不同渠道入口）轮询；--base-url 可切本地/公网
  - 统计：成功/失败/平均耗时/分类分布/AI 回复平均长度

用法：
  python3 simulate.py --count 200
  python3 simulate.py --count 50 --concurrency 2 --min-gap 3 --max-gap 8
  python3 simulate.py --base-url http://localhost:8204 --app-key ak_xxx --count 200
  python3 simulate.py --app-key ak_a --app-key ak_b   # 多渠道轮询

依赖：仅 Python 标准库（Python 3.8+）
"""

import argparse
import json
import os
import random
import string
import sys
import threading
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed

HERE = os.path.dirname(os.path.abspath(__file__))
INTERACTIONS_FILE = os.path.join(HERE, "interactions.jsonl")

# 降级判据只有一份：ai_quality.DEGRADED_MARKERS（与本文件同目录，靠 sys.path[0] 解析）。
# 这里原先另写了一条内联副本（只认 "AI 服务暂时不可用"/"请稍后再试"），
# 于是补任何一条兜底文案只会让其中一边报警，降级率永远统计不全。
from ai_quality import is_degraded


# ---------------------------------------------------------------------------
# 配置
# ---------------------------------------------------------------------------
def default_base_url():
    return "http://localhost:8204"


# ---------------------------------------------------------------------------
# 交互内容落库（供质量监控读取真实 AI 回答内容）
# ---------------------------------------------------------------------------
def append_interaction(rec):
    """把单条交互（含真实 AI 回答内容）追加到 interactions.jsonl，供 ai_quality.py 读取评估。

    rec 字段：ts/round/idx/question/cat/app_key/visitor/session_id/latency/
              ai_text/ai_len/degraded/empty/error
    """
    try:
        with open(INTERACTIONS_FILE, "a", encoding="utf-8") as f:
            f.write(json.dumps(rec, ensure_ascii=False) + "\n")
    except Exception:
        pass


def load_json(name):
    path = os.path.join(HERE, name)
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


# ---------------------------------------------------------------------------
# 昵称生成（真实感）
# ---------------------------------------------------------------------------
def gen_visitor_name(names):
    sur = random.choice(names["surnames"])
    giv = random.choice(names["given_names"])
    suf = random.choice(names["suffixes"])
    return sur + giv + suf


def gen_visitor_id():
    return "sim_" + "".join(random.choices(string.ascii_lowercase + string.digits, k=12))


# ---------------------------------------------------------------------------
# HTTP 客户端
# ---------------------------------------------------------------------------
def post_json(url, headers, payload, timeout):
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(url, data=data, headers=headers, method="POST")
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        body = resp.read().decode("utf-8")
    return json.loads(body)


def open_session(base, app_key, visitor_id, visitor_name, timeout):
    url = base.rstrip("/") + "/api/chat/public/sessions"
    headers = {
        "Content-Type": "application/json",
        "X-Chat-App-Key": app_key,
    }
    payload = {
        "channel_id": "",  # 留空，由 AppKeyResolve 解析
        "visitor_id": visitor_id,
        "visitor_name": visitor_name,
    }
    resp = post_json(url, headers, payload, timeout)
    if resp.get("code") != "SUCCESS":
        raise RuntimeError("open session failed: %s" % resp.get("message"))
    return resp["data"]["session"]["session_id"]


def send_message(base, app_key, session_id, visitor_id, content, timeout):
    url = base.rstrip("/") + "/api/chat/public/sessions/%s/messages" % session_id
    headers = {
        "Content-Type": "application/json",
        "X-Chat-App-Key": app_key,
        "X-Chat-Visitor-Id": visitor_id,
    }
    payload = {"content": content}
    resp = post_json(url, headers, payload, timeout)
    if resp.get("code") != "SUCCESS":
        raise RuntimeError("send message failed: %s" % resp.get("message"))
    ai = resp.get("data", {}).get("ai_response") or {}
    return ai.get("content", "")


# ---------------------------------------------------------------------------
# 单条任务
# ---------------------------------------------------------------------------
class Stats:
    def __init__(self):
        self.lock = threading.Lock()
        self.done = 0
        self.ok = 0
        self.fail = 0
        self.degraded = 0
        self.total_latency = 0.0
        self.total_ai_len = 0
        self.cat_ok = {}
        self.cat_fail = {}
        self.errors = []

    def record(self, ok, latency, ai_len, cat, err):
        with self.lock:
            self.done += 1
            if ok:
                self.ok += 1
                self.total_latency += latency
                self.total_ai_len += ai_len
                self.cat_ok[cat] = self.cat_ok.get(cat, 0) + 1
                if err == "AI_DEGRADED":
                    self.degraded += 1
            else:
                self.fail += 1
                self.cat_fail[cat] = self.cat_fail.get(cat, 0) + 1
                if len(self.errors) < 20:
                    self.errors.append(err)


def run_one(round_idx, idx, args, questions, names, app_keys, stats, print_lock):
    q = questions[idx % len(questions)]
    content = q["q"]
    cat = q.get("cat", "?")
    app_key = app_keys[idx % len(app_keys)]
    visitor_id = gen_visitor_id()
    visitor_name = gen_visitor_name(names)
    session_id = ""
    ai_text = ""
    degraded = False
    empty = False
    err = ""

    t0 = time.time()
    try:
        sid = open_session(args.base_url, app_key, visitor_id, visitor_name, args.timeout)
        session_id = sid
        ai_text = send_message(args.base_url, app_key, sid, visitor_id, content, args.timeout)
        latency = time.time() - t0
        stripped = ai_text.strip()
        ok = len(stripped) > 0
        empty = not ok
        # 识别 AI 栈过载/不可用兜底（HTTP 成功但非真实应答）
        degraded = is_degraded(stripped)
        err = "" if ok else "empty ai_response"
        if ok and degraded:
            err = "AI_DEGRADED"  # 链路通但 AI 栈未真实应答
        with print_lock:
            print("[%d] %s(%s) Q:%-30s -> AI %d字  %.1fs" % (
                idx + 1, visitor_name, cat, content[:28], len(ai_text), latency))
        stats.record(ok, latency, len(ai_text), cat, err)
    except Exception as e:
        latency = time.time() - t0
        err = "%s: %s" % (type(e).__name__, e)
        with print_lock:
            print("[%d] %s(%s) Q:%-30s FAIL %s" % (idx + 1, visitor_name, cat, content[:28], err))
        stats.record(False, latency, 0, cat, err)

    # 落库真实 AI 回答内容（供质量监控读取）
    append_interaction({
        "ts": time.strftime("%Y-%m-%d %H:%M:%S"),
        "epoch": int(t0),
        "round": round_idx,
        "idx": idx,
        "question": content,
        "cat": cat,
        "app_key": app_key,
        "visitor": visitor_name,
        "session_id": session_id,
        "latency": round(latency, 2),
        "ai_text": ai_text,
        "ai_len": len(ai_text),
        "degraded": degraded,
        "empty": empty,
        "error": err,
    })


# ---------------------------------------------------------------------------
# 限速调度：全局并发 + 随机间隔
# ---------------------------------------------------------------------------
def run_round(round_idx, args, questions, names, app_keys, print_lock, logf):
    """跑一轮（args.round_size 条，但不超题库），返回 Stats。守护/单次共用。"""
    count = min(args.round_size, len(questions))
    stats = Stats()
    idxs = list(range(count))
    sem = threading.Semaphore(args.concurrency)

    def worker(i):
        sem.acquire()
        try:
            run_one(round_idx, i, args, questions, names, app_keys, stats, print_lock)
        finally:
            sem.release()

    with ThreadPoolExecutor(max_workers=args.concurrency) as ex:
        futures = []
        for i in idxs:
            if i > 0:
                time.sleep(random.uniform(args.min_gap, args.max_gap))
            futures.append(ex.submit(worker, i))
        for f in as_completed(futures):
            pass

    s = stats
    avg_lat = (s.total_latency / s.ok) if s.ok else 0
    avg_ai = (s.total_ai_len / s.ok) if s.ok else 0
    rate = (100.0 * s.ok / s.done) if s.done else 0
    ts = time.strftime("%Y-%m-%d %H:%M:%S")
    summary = ("[%s] ROUND %d  done=%d ok=%d degraded=%d fail=%d rate=%.1f%% "
               "avg_lat=%.1fs avg_ai=%.0f字" % (
                   ts, round_idx, s.done, s.ok, s.degraded, s.fail, rate, avg_lat, avg_ai))
    with print_lock:
        print("\n" + "-" * 64)
        print(summary)
        print("  分类OK: %s" % s.cat_ok)
        if s.cat_fail:
            print("  分类FAIL: %s" % s.cat_fail)
        if s.errors:
            print("  错误样例:")
            for e in s.errors[:5]:
                print("    - %s" % e)
        print("-" * 64)
    if logf:
        logf.write(summary + "\n")
        logf.flush()
    return s


def main():
    parser = argparse.ArgumentParser(description="模拟真实用户提问压测器")
    parser.add_argument("--base-url", default=default_base_url(), help="服务基址，默认 http://localhost:8204")
    parser.add_argument("--app-key", action="append", dest="app_keys", default=None,
                        help="渠道 AppKey（可多次指定以轮询多渠道）；默认从环境变量 SIM_APP_KEY 读取")
    parser.add_argument("--count", type=int, default=200, help="提问总条数，默认 200（非守护模式）；配合 --sequential 时可设 0=跑完整个题库")
    parser.add_argument("--sequential", action="store_true",
                        help="顺序遍历模式：按题库顺序逐条测试（不随机循环），便于覆盖全部数据；--count 0 表示跑完整个题库")
    parser.add_argument("--batch", type=int, default=0,
                        help="顺序模式本次最多跑几条（断点续跑），0=不限（跑完整个题库）。配合 automation 分批安全执行")
    parser.add_argument("--seq-progress", default=".seq_progress", help="顺序模式断点进度文件，默认 .seq_progress")
    parser.add_argument("--concurrency", type=int, default=1, help="最大并发数，默认 1（RAG/LLM 栈响应慢且易过载，建议 1~2）")
    parser.add_argument("--min-gap", type=float, default=30.0, help="每条任务最小间隔秒，默认 30.0（已降频，适配脆弱 RAG/LLM 栈）")
    parser.add_argument("--max-gap", type=float, default=45.0, help="每条任务最大间隔秒，默认 45.0（已降频）")
    parser.add_argument("--timeout", type=int, default=120, help="单条 HTTP 超时秒，默认 120（AI 真实回复常 40~60s）")
    parser.add_argument("--questions", default="questions.json", help="提问库文件")
    parser.add_argument("--names", default="names.json", help="昵称素材文件")
    parser.add_argument("--seed", type=int, default=None, help="随机种子，可复现")
    parser.add_argument("--shuffle", action="store_true", help="打乱提问顺序")
    parser.add_argument("--daemon", action="store_true",
                        help="守护进程模式：无限循环跑，每轮 --round-size 条，轮间休息 --round-gap 秒，日志追加到 --log-file")
    parser.add_argument("--round-size", type=int, default=20, help="守护模式每轮提问数，默认 20")
    parser.add_argument("--round-gap", type=float, default=60.0, help="守护模式轮间休息秒，默认 60")
    parser.add_argument("--max-rounds", type=int, default=0, help="守护模式最大轮数，0=无限，默认 0")
    parser.add_argument("--log-file", default="simulate.log", help="守护模式日志文件路径，默认 simulate.log（追加）")
    args = parser.parse_args()

    if args.seed is not None:
        random.seed(args.seed)

    app_keys = args.app_keys or []
    if not app_keys:
        env = os.environ.get("SIM_APP_KEY")
        if env:
            app_keys = [env]
    if not app_keys:
        print("错误：未指定 --app-key 且无环境变量 SIM_APP_KEY。", file=sys.stderr)
        print("可用 active 渠道 app_key 例如：ak_sN52ZmTUqkXsTtPak4Sfp3jD", file=sys.stderr)
        sys.exit(2)

    qdata = load_json(args.questions)
    questions = qdata["questions"]
    if args.shuffle:
        questions = questions[:]
        random.shuffle(questions)
    names = load_json(args.names)

    print_lock = threading.Lock()

    if args.daemon:
        logf = open(args.log_file, "a", encoding="utf-8")
        print("=" * 64)
        print("守护进程模式 启动")
        print("  目标服务 : %s" % args.base_url)
        print("  渠道数   : %d (%s)" % (len(app_keys), ", ".join(app_keys)))
        print("  题库     : %d 条" % len(questions))
        print("  每轮题数 : %d  轮间休息: %.0fs  最大轮数: %s" % (
            args.round_size, args.round_gap, "无限" if args.max_rounds == 0 else str(args.max_rounds)))
        print("  并发/间隔: %d / %.1f~%.1f s  超时: %d s" % (
            args.concurrency, args.min_gap, args.max_gap, args.timeout))
        print("  日志文件 : %s" % os.path.abspath(args.log_file))
        print("  按 Ctrl+C 停止" % ())
        print("=" * 64)
        logf.write("=== daemon start %s base=%s keys=%d round_size=%d ===\n" % (
            time.strftime("%Y-%m-%d %H:%M:%S"), args.base_url, len(app_keys), args.round_size))
        round_idx = 0
        try:
            while True:
                round_idx += 1
                if args.max_rounds and round_idx > args.max_rounds:
                    break
                with print_lock:
                    print("\n>>> 第 %d 轮开始 @ %s" % (round_idx, time.strftime("%H:%M:%S")))
                run_round(round_idx, args, questions, names, app_keys, print_lock, logf)
                if args.max_rounds and round_idx >= args.max_rounds:
                    break
                with print_lock:
                    print("    轮间休息 %.0fs ..." % args.round_gap)
                time.sleep(args.round_gap)
        except KeyboardInterrupt:
            with print_lock:
                print("\n收到中断，守护进程退出。")
        finally:
            logf.write("=== daemon stop %s rounds=%d ===\n" % (
                time.strftime("%Y-%m-%d %H:%M:%S"), round_idx))
            logf.close()
        return

    # 非守护：一次性跑 args.count 条
    # 顺序遍历模式：覆盖整个题库（按 idx 顺序逐条，不随机循环）+ 断点续跑
    if args.sequential:
        prog_path = os.path.join(HERE, args.seq_progress)
        start = 0
        if os.path.exists(prog_path):
            try:
                with open(prog_path, "r", encoding="utf-8") as pf:
                    start = int(pf.read().strip() or "0")
            except Exception:
                start = 0
        if start >= len(questions):
            # 已跑完一轮，重置从头再覆盖（保留全量覆盖能力）
            start = 0
            with open(prog_path, "w", encoding="utf-8") as pf:
                pf.write("0")
        # 本批范围：[start, end)
        end = len(questions)
        if args.count and args.count > 0:
            end = min(end, start + args.count)
        if args.batch and args.batch > 0:
            end = min(end, start + args.batch)
        run_slice = questions[start:end]
        mode_desc = "顺序遍历 [idx %d~%d) 共 %d 条（断点续跑）" % (start, end, len(run_slice))
        # 顺序模式：本批一次性跑完切片（不受 round_size 截断）
        args.round_size = len(run_slice)
    else:
        start = 0
        run_slice = questions[:args.count] if args.count and args.count > 0 else questions
        mode_desc = "随机循环取样 %d 条 (题库 %d 条)" % (len(run_slice), len(questions))

    print("=" * 64)
    print("模拟真实用户提问 开始")
    print("  目标服务 : %s" % args.base_url)
    print("  渠道数   : %d (%s)" % (len(app_keys), ", ".join(app_keys)))
    print("  模式     : %s" % ("顺序遍历(断点续跑)" if args.sequential else "随机循环"))
    print("  本批范围 : %s" % mode_desc)
    print("  并发上限 : %d" % args.concurrency)
    print("  间隔     : %.1f~%.1f s" % (args.min_gap, args.max_gap))
    print("  超时     : %d s" % args.timeout)
    print("=" * 64)

    logf = open(os.path.join(HERE, "simulate.log"), "a", encoding="utf-8")
    logf.write("=== single run %s base=%s mode=%s start=%d end=%d ===\n" % (
        time.strftime("%Y-%m-%d %H:%M:%S"), args.base_url,
        "sequential" if args.sequential else "random", start, start + len(run_slice)))
    s = run_round(1, args, run_slice, names, app_keys, print_lock, logf)
    logf.close()

    # 顺序模式：更新断点（仅当本批确实跑完才推进）
    if args.sequential:
        new_start = start + len(run_slice)
        if new_start >= len(questions):
            new_start = 0  # 全量覆盖完成，下一轮从头
        with open(os.path.join(HERE, args.seq_progress), "w", encoding="utf-8") as pf:
            pf.write(str(new_start))
    # 单次模式补充完整汇总
    print("\n" + "=" * 64)
    print("汇总报告")
    print("  总任务   : %d" % s.done)
    print("  成功     : %d" % s.ok)
    print("  其中降级 : %d  (AI 栈不可用兜底，链路通但非真应答)" % s.degraded)
    print("  失败     : %d" % s.fail)
    print("  成功率   : %.1f%%" % (100.0 * s.ok / s.done if s.done else 0))
    print("  平均耗时 : %.1fs (仅成功)" % (s.total_latency / s.ok if s.ok else 0))
    print("  AI均长   : %.0f 字" % (s.total_ai_len / s.ok if s.ok else 0))
    print("  分类 OK  : %s" % s.cat_ok)
    if s.cat_fail:
        print("  分类 FAIL: %s" % s.cat_fail)
    if s.errors:
        print("  错误样例 :")
        for e in s.errors[:10]:
            print("    - %s" % e)
    print("=" * 64)


if __name__ == "__main__":
    main()
