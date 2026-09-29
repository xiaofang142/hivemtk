#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
守护进程监控与评估器（hivemtk 模拟压测）

功能：
  - 间隔读取 simulate.log 的 ROUND 汇总行，统计累计题数/成功率/AI 降级率/平均耗时趋势
  - 探活目标服务 /api/health（或 /api/chat/public/sessions 探测）
  - 查 PostgreSQL 实际落库数据：visitor session 数、session_messages 数、最近增量
  - 输出评估报告到控制台 + monitor_report.log

用法：
  python3 monitor.py                      # 默认每 300s 采样一次，无限循环
  python3 monitor.py --interval 120       # 每 120s 采样
  python3 monitor.py --once               # 只采样一次并退出
  python3 monitor.py --db-pass xxx        # 指定 PG 密码（否则从 ../.env 读）

依赖：仅标准库 + 系统 psql（用于查库）。Python 3.8+。
"""

import argparse
import json
import os
import re
import subprocess
import time

HERE = os.path.dirname(os.path.abspath(__file__))
LOG_FILE = os.path.join(HERE, "simulate.log")
REPORT_FILE = os.path.join(HERE, "monitor_report.log")


# ---------------------------------------------------------------------------
# 日志解析
# ---------------------------------------------------------------------------
ROUND_RE = re.compile(
    r"ROUND (\d+)  done=(\d+) ok=(\d+) degraded=(\d+) fail=(\d+) "
    r"rate=([\d.]+)% avg_lat=([\d.]+)s avg_ai=([\d.]+)字"
)


def parse_log():
    """返回最近一次累计统计 + 历史轮次列表。"""
    rounds = []
    if not os.path.exists(LOG_FILE):
        return rounds, None
    with open(LOG_FILE, encoding="utf-8") as f:
        for line in f:
            m = ROUND_RE.search(line)
            if m:
                rounds.append({
                    "round": int(m.group(1)),
                    "done": int(m.group(2)),
                    "ok": int(m.group(3)),
                    "degraded": int(m.group(4)),
                    "fail": int(m.group(5)),
                    "rate": float(m.group(6)),
                    "avg_lat": float(m.group(7)),
                    "avg_ai": float(m.group(8)),
                })
    return rounds, (rounds[-1] if rounds else None)


# ---------------------------------------------------------------------------
# 服务探活
# ---------------------------------------------------------------------------
def probe_service(base_url):
    """返回 (http_code, latency_s)。"""
    t0 = time.time()
    try:
        cmd = ["curl", "-s", "-m", "8", "-o", "/dev/null", "-w", "%{http_code}",
               base_url.rstrip("/") + "/api/health"]
        out = subprocess.run(cmd, capture_output=True, text=True, timeout=12)
        code = out.stdout.strip()
        return (code, time.time() - t0)
    except Exception as e:
        return ("ERR:%s" % e, time.time() - t0)


# ---------------------------------------------------------------------------
# 数据库采样
# ---------------------------------------------------------------------------
def get_pg_password():
    env_path = os.path.join(HERE, "..", "..", ".env")
    try:
        with open(env_path, encoding="utf-8") as f:
            for line in f:
                if line.startswith("POSTGRES_PASSWORD="):
                    return line.split("=", 1)[1].strip()
    except Exception:
        pass
    return os.environ.get("PGPASSWORD", "")


def db_counts(pw):
    """返回 (sessions, messages) 计数。"""
    sql = ("SELECT (SELECT count(*) FROM customer_sessions), "
           "(SELECT count(*) FROM session_messages);")
    try:
        out = subprocess.run(
            ["psql", "-h", "localhost", "-p", "8232", "-U", "admin", "-d", "user_db",
             "-tAc", sql],
            capture_output=True, text=True, timeout=20,
            env={**os.environ, "PGPASSWORD": pw})
        if out.returncode != 0:
            return (None, None, out.stderr.strip()[:200])
        parts = out.stdout.strip().split("|")
        if len(parts) == 2:
            return (int(parts[0]), int(parts[1]), "")
        return (None, None, "unexpected:%s" % out.stdout.strip()[:120])
    except Exception as e:
        return (None, None, str(e))


def db_recent_messages(pw, minutes=10):
    """最近 N 分钟新增 session_messages 数。"""
    sql = ("SELECT count(*) FROM session_messages "
           "WHERE created_at > now() - interval '%d minutes';" % minutes)
    try:
        out = subprocess.run(
            ["psql", "-h", "localhost", "-p", "8232", "-U", "admin", "-d", "user_db",
             "-tAc", sql],
            capture_output=True, text=True, timeout=20,
            env={**os.environ, "PGPASSWORD": pw})
        if out.returncode != 0:
            return None
        return int(out.stdout.strip())
    except Exception:
        return None


# ---------------------------------------------------------------------------
# 评估输出
# ---------------------------------------------------------------------------
def evaluate(rounds, last, code, latency, counts, recent):
    ts = time.strftime("%Y-%m-%d %H:%M:%S")
    lines = []
    lines.append("=" * 64)
    lines.append("[%s] 监控评估报告" % ts)
    lines.append("-" * 64)
    # 服务探活
    lines.append("服务探活 : HTTP %s  (%.2fs)" % (code, latency))
    # 日志累计
    if last:
        total_done = sum(r["done"] for r in rounds)
        total_ok = sum(r["ok"] for r in rounds)
        total_deg = sum(r["degraded"] for r in rounds)
        total_fail = sum(r["fail"] for r in rounds)
        lines.append("累计轮次 : %d" % len(rounds))
        lines.append("累计题数 : %d  ok=%d  降级=%d  失败=%d" % (
            total_done, total_ok, total_deg, total_fail))
        lines.append("累计成功率: %.1f%%   累计降级率: %.1f%%" % (
            100.0 * total_ok / total_done if total_done else 0,
            100.0 * total_deg / total_done if total_done else 0))
        # 最近 3 轮趋势
        lines.append("近 3 轮成功率: " + " ".join(
            "R%d=%.0f%%" % (r["round"], r["rate"]) for r in rounds[-3:]))
        lines.append("近 3 轮耗时  : " + " ".join(
            "R%d=%.0fs" % (r["round"], r["avg_lat"]) for r in rounds[-3:]))
        lines.append("近 3 轮AI字  : " + " ".join(
            "R%d=%.0f" % (r["round"], r["avg_ai"]) for r in rounds[-3:]))
    else:
        lines.append("日志     : 暂无 ROUND 汇总（守护进程可能未启动或尚未完成一轮）")
    # 数据库
    if counts[0] is not None:
        lines.append("DB落库   : customer_sessions=%d  session_messages=%d" % (
            counts[0], counts[1]))
    else:
        lines.append("DB落库   : 查询失败 %s" % (counts[2] or ""))
    if recent is not None:
        lines.append("近10分钟 : 新增 session_messages=%d" % recent)
    # 评估结论
    lines.append("-" * 64)
    verdict = assess(code, last, counts, recent)
    lines.append("评估结论 : %s" % verdict)
    lines.append("=" * 64)
    text = "\n".join(lines)
    print(text)
    with open(REPORT_FILE, "a", encoding="utf-8") as f:
        f.write(text + "\n\n")
    return text


def assess(code, last, counts, recent):
    issues = []
    if str(code) not in ("200", "000") and not str(code).startswith("ERR"):
        issues.append("服务探活异常(HTTP %s)" % code)
    if last and last["fail"] > 0 and last["rate"] < 80:
        issues.append("最近一轮成功率偏低(%.1f%%)" % last["rate"])
    if last and last["ok"] > 0 and last["degraded"] >= last["ok"]:
        issues.append("AI 降级率 100%（链路通但 RAG/LLM 栈未真实应答，需排查推理栈）")
    elif last and last["ok"] > 0 and last["degraded"] > last["ok"] * 0.5:
        issues.append("AI 降级占比过高（RAG/LLM 栈可能过载，需降速或扩容）")
    if counts[0] is None:
        issues.append("数据库不可达")
    if recent is not None and recent == 0 and last and last["ok"] > 0:
        issues.append("日志成功但未落库（链路/写库异常）")
    if not issues:
        if last:
            return "运行健康：服务可达、成功率达标、AI 真实应答、数据正常落库。"
        return "运行健康：服务可达、数据正常落库（暂无压测轮次汇总）。"
    return "需关注: " + "; ".join(issues)


def _load_json(name):
    with open(os.path.join(HERE, name), encoding="utf-8") as f:
        return json.load(f)


def run_supervisor(args, pw, app_keys, questions, names):
    """
    单进程守护+监控：内置跑模拟轮次并周期评估。
    每轮 round_size 条 -> 评估 -> 休息 round_gap -> 继续。
    """
    import simulate  # 复用 run_round
    print_lock = __import__("threading").Lock()
    logf = open(os.path.join(HERE, "simulate.log"), "a", encoding="utf-8")
    logf.write("=== supervisor start %s base=%s keys=%d round_size=%d ===\n" % (
        time.strftime("%Y-%m-%d %H:%M:%S"), args.base_url, len(app_keys), args.round_size))
    print("守护+监控 启动：单进程内联模式（Ctrl+C 停止）")
    print("  每轮 %d 题，轮间休息 %.0fs，每 %d 轮评估一次" % (
        args.round_size, args.round_gap, args.eval_every))
    try:
        ridx = 0
        while True:
            ridx += 1
            simulate.run_round(ridx, args, questions, names, app_keys, print_lock, logf)
            # 周期评估（每 eval_every 轮，或首轮后）
            if ridx % args.eval_every == 0:
                rounds, last = parse_log()
                code, lat = probe_service(args.base_url)
                counts = db_counts(pw)
                recent = db_recent_messages(pw)
                evaluate(rounds, last, code, lat, counts, recent)
                # AI 回答内容+质量评估（读取 interactions.jsonl）
                try:
                    import ai_quality
                    print(ai_quality.evaluate_recent(
                        last_n=args.round_size * args.eval_every,
                        since_minutes=None, write=True))
                except Exception as qe:
                    print("  [warn] AI 质量评估跳过: %s" % qe)
            print("    轮间休息 %.0fs ..." % args.round_gap)
            left = args.round_gap
            while left > 0:
                s = min(30.0, left)
                time.sleep(s)
                left -= s
                if left > 0:
                    print("    ... 心跳 @ %s (剩余 %.0fs)" % (
                        time.strftime("%H:%M:%S"), left))
    except KeyboardInterrupt:
        print("\n收到中断，守护+监控退出。")
    finally:
        logf.write("=== supervisor stop %s ===\n" % time.strftime("%Y-%m-%d %H:%M:%S"))
        logf.close()


def main():
    p = argparse.ArgumentParser(description="模拟压测守护进程监控评估器")
    p.add_argument("--base-url", default="http://localhost:8204")
    p.add_argument("--interval", type=int, default=300, help="采样间隔秒，默认 300（--watchdog 用）")
    p.add_argument("--once", action="store_true", help="只采样一次退出")
    p.add_argument("--db-pass", default=None, help="PG 密码，默认从 ../.env 读")
    p.add_argument("--watchdog", action="store_true",
                   help="看门狗模式（兼容保留）：单进程内联守护+周期评估")
    p.add_argument("--supervisor", action="store_true",
                   help="单进程守护+监控：内置跑模拟轮次并周期评估")
    p.add_argument("--app-key", action="append", dest="app_keys", default=None,
                   help="渠道 AppKey（同 simulate.py）")
    p.add_argument("--round-size", type=int, default=20, help="每轮题数，默认 20")
    p.add_argument("--round-gap", type=float, default=90.0, help="轮间休息秒，默认 90")
    p.add_argument("--concurrency", type=int, default=1, help="并发，默认 1")
    p.add_argument("--min-gap", type=float, default=8.0, help="每条最小间隔，默认 8")
    p.add_argument("--max-gap", type=float, default=15.0, help="每条最大间隔，默认 15")
    p.add_argument("--timeout", type=int, default=120, help="单条超时，默认 120")
    p.add_argument("--eval-every", type=int, default=1, help="每几轮评估一次，默认 1")
    args = p.parse_args()

    pw = args.db_pass or get_pg_password()

    if args.supervisor or args.watchdog:
        app_keys = args.app_keys or []
        if not app_keys:
            env = os.environ.get("SIM_APP_KEY")
            app_keys = [env] if env else ["ak_sN52ZmTUqkXsTtPak4Sfp3jD"]
        qdata = _load_json("questions.json")
        questions = qdata["questions"]
        names = _load_json("names.json")
        run_supervisor(args, pw, app_keys, questions, names)
        return

    def sample():
        rounds, last = parse_log()
        code, lat = probe_service(args.base_url)
        counts = db_counts(pw)
        recent = db_recent_messages(pw)
        evaluate(rounds, last, code, lat, counts, recent)

    if args.once:
        sample()
        return

    print("监控评估器启动，每 %ds 采样一次（Ctrl+C 停止）" % args.interval)
    try:
        while True:
            sample()
            time.sleep(args.interval)
    except KeyboardInterrupt:
        print("\n监控退出。")


if __name__ == "__main__":
    main()
