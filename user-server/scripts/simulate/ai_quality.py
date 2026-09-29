#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
AI 回答内容与质量监控器（hivemtk 模拟压测）

读取 simulate.py 落库的 interactions.jsonl（每条真实 AI 回答内容），
对回答做内容级质量评估并持续监控：

  - 空回答 / 降级兜底（"AI 服务暂时不可用"、"…无法处理您的请求…" 等固定兜底句式）检测
  - 截断检测（长回答未以句末标点结束，疑似被 max_tokens 截断）
  - 重复/啰嗦检测（高频 4-gram 重复、字符重复、词汇多样性过低）
  - 切题度检测（按问题分类匹配关键词，判断是否答非所问）
  - 综合质量分（0~100）与 优/中/差 分档
  - 采样展示真实回答内容（好的样本 + 最差样本），供人工复核

用法：
  python3 ai_quality.py --once                # 单次评估最近 50 条，输出报告
  python3 ai_quality.py --once --last 200     # 评估最近 200 条
  python3 ai_quality.py --once --since 60     # 评估最近 60 分钟内的回答
  python3 ai_quality.py --daemon --interval 300   # 每 300s 评估一次，写 ai_quality.log
  python3 ai_quality.py --daemon --last 30 --interval 600

依赖：仅 Python 标准库（Python 3.8+）。
与 simulate.py 配合：先让 simulate.py --daemon / monitor.py --supervisor 产生
interactions.jsonl，再本脚本读取评估。也可独立单独跑。
"""

import argparse
import json
import os
import time

HERE = os.path.dirname(os.path.abspath(__file__))
INTERACTIONS_FILE = os.path.join(HERE, "interactions.jsonl")
QUALITY_LOG = os.path.join(HERE, "ai_quality.log")

# 句末标点（用于截断检测）；~ ～ 是客服澄清反问常见的句末语气词
TERMINAL_PUNCT = set("。！？.!?…）)】”’\"'~～")
# 降级/兜底特征串（链路通但非真实应答）
# 仅保留「明确表明 AI 栈不可用」的兜底串；客服正常的「暂时无法确定/需要补充信息」
# 是澄清话术而非兜底，不纳入（避免把反问误判为降级）。
# 逐条都有真实产出方（改文案要同步这里，也同步 bridge-e2e-sim.sh 的 case 判据）：
#   AI 服务暂时不可用     -> internal/service/sales_engine_agentloop.go
#   暂时无法处理您的请求   -> 同上 emptyReplyFallback
#   当前服务暂时繁忙      -> internal/aiagent/llm/provider_failover.go
#   当前客服系统繁忙/系统暂时有点忙 -> internal/aiagent/llm/fallback_tree.go（通用池）
#   系统暂不可用          -> internal/aiagent/agent/runtime/runtime.go
# 刻意不收"网络异常/服务繁忙/系统繁忙/请稍后再试"这类通用词：正常应答里也会合法出现
# （答客户"服务器高峰期网络异常"会被整批记成降级），而上面每条模板都含自己的独有前缀。
# 已知下界：fallback_tree.scenarioFallbackTemplates 里的礼貌挂起话术只会在 LLM 失败路径出现，
# 但不含"栈不可用"字样，按上面口径不计降级 ⇒ 本报告里的"降级率"是下界。
DEGRADED_MARKERS = [
    "AI 服务暂时不可用",
    "暂时无法处理您的请求",
    "当前服务暂时繁忙",
    "当前客服系统繁忙",
    "系统暂时有点忙",
    "系统暂不可用",
]
# 各分类切题关键词（命中其一即认为切题；chat 类默认切题）
CAT_KEYWORDS = {
    "product": ["功能", "特点", "参数", "材质", "规格", "支持", "适用", "产品",
                "型号", "升级", "配置", "性能"],
    "price": ["元", "价格", "优惠", "活动", "折扣", "多少钱", "券", "满减",
              "立减", "特价", "促销", "价", "划算"],
    "buy": ["下单", "购买", "拍下", "订单", "付款", "支付", "怎么买", "加购",
            "结算", "买"],
    "logistics": ["发货", "物流", "快递", "配送", "邮费", "包邮", "顺丰", "到货",
                  "收货", "送达", "几天", "运输", "寄"],
    "aftersale": ["售后", "质保", "保修", "维修", "客服", "三包", "服务"],
    "return": ["退", "换", "七天", "无理由", "寄回", "退款"],
    "feature": ["怎么", "如何", "设置", "操作", "使用", "步骤", "教程", "开关",
                "功能"],
    "cooperation": ["合作", "代理", "加盟", "招商", "商务", "批发", "分销", "联系"],
    "complaint": ["投诉", "差评", "失望", "不满", "问题", "建议", "反馈"],
    "account": ["账号", "登录", "注册", "密码", "账户", "绑定", "验证码"],
    "chat": [],  # 闲聊默认切题
}


# ---------------------------------------------------------------------------
# 单条质量分析
# ---------------------------------------------------------------------------
def is_degraded(text):
    return any(m in text for m in DEGRADED_MARKERS)


def _longest_repeat(text):
    """最长重复子串长度（O(n^2)，文本短，足够快）。"""
    for L in range(min(len(text), 40), 3, -1):
        seen = set()
        for i in range(len(text) - L + 1):
            s = text[i:i + L]
            if s in seen:
                return L
            seen.add(s)
    return 0


def is_repetitive(text):
    """真·重复检测：
      - 连续相同字符 >8（如"啊啊啊…"纯噪声）；
      - 或存在「长且占比高」的重复子串（模型卡壳循环复述整句）。
    普通要点列表里"模块"等关键词、或两行恰好同字的模板 token（如"5 个模块"）
    长度短、占比低，不构成重复，不误判。"""
    if len(text) >= 8:
        run = 1
        for i in range(1, len(text)):
            a, b = text[i - 1], text[i]
            # 只统计字母/汉字的连续重复（忽略 -、*、空格、标点等排版符号）
            if a == b and (a.isalnum() or "\u4e00" <= a <= "\u9fff"):
                run += 1
                if run > 8:
                    return True
            else:
                run = 1
    lr = _longest_repeat(text)
    if lr < 12 or lr < 0.10 * len(text):
        return False
    # 排除纯排版符号构成的重复（markdown 表格分隔行 "|----|----|" 在长表里
    # 必然重复出现，属正常排版而非模型卡壳循环）
    return _has_semantic_repeat(text, lr)


def _has_semantic_repeat(text, lr):
    """在最长重复子串长度 lr 下，判断重复是否含「有意义的语义内容」
    （即不全由 - | * 空格 等排版符号组成）。纯排版重复不算啰嗦。"""
    seen = set()
    for i in range(len(text) - lr + 1):
        s = text[i:i + lr]
        if s in seen:
            # 去掉排版符号后剩余内容应有实际语义字符
            stripped = [c for c in s if c not in "-|:* \t"]
            if any(c.isalnum() or "\u4e00" <= c <= "\u9fff" for c in stripped):
                return True
            return False
        seen.add(s)
    return False


def is_low_diversity(text):
    """词汇多样性过低（退化啰嗦）。仅对较长文本判定。"""
    if len(text) < 30:
        return False
    uniq = len(set(text))
    return (uniq / len(text)) < 0.25


def is_truncated(text):
    """疑似被截断：长回答未以句末标点/闭合括号/表情符结束。

    以 emoji（如 😊🙏✨ 等，多为代理对，码点 > 0xFFFF）收尾的回答是完整
    礼貌收尾，非截断，不误判。"""
    if len(text) < 40:
        return False
    last = text[-1]
    if last in TERMINAL_PUNCT:
        return False
    # 表情符（代理对）收尾视为完整
    if ord(last) > 0xFFFF:
        return False
    return True


def is_off_topic(text, cat):
    kws = CAT_KEYWORDS.get(cat, [])
    if not kws:
        return False  # chat 等默认切题
    return not any(k in text for k in kws)


def analyze(rec):
    """返回单条质量评估结果 dict（含真实内容裁剪）。"""
    text = (rec.get("ai_text") or "").strip()
    cat = rec.get("cat", "?")
    length = len(text)
    err = rec.get("error") or ""
    degraded = bool(rec.get("degraded")) or is_degraded(text)
    # HTTP 失败（502/超时等）：链路未返回真实回答，等同失败
    http_fail = ("HTTP" in err) or ("Error" in err) or ("Timeout" in err)
    empty = (length < 5) or http_fail

    flags = []
    score = 100
    if degraded:
        flags.append("degraded")
        score = 0
    if http_fail:
        flags.append("http_fail")
        score = 0
    if length < 5 and not http_fail:
        flags.append("empty")
        score = 0
    if score > 0:
        if length < 15:
            flags.append("too_short")
            score -= 20
        if is_truncated(text):
            flags.append("truncated")
            score -= 25
        if is_repetitive(text):
            flags.append("repetitive")
            score -= 30
        if is_low_diversity(text):
            flags.append("low_diversity")
            score -= 15
        if is_off_topic(text, cat):
            flags.append("off_topic")
            score -= 15
        score = max(0, min(100, score))

    if score >= 80:
        band = "good"
    elif score >= 50:
        band = "fair"
    else:
        band = "poor"

    return {
        "ts": rec.get("ts", ""),
        "cat": cat,
        "length": length,
        "degraded": degraded,
        "empty": empty,
        "flags": flags,
        "score": score,
        "band": band,
        "question": rec.get("question", ""),
        "ai_text": text,
    }


# ---------------------------------------------------------------------------
# 读取交互记录
# ---------------------------------------------------------------------------
def load_interactions(last_n=None, since_minutes=None):
    if not os.path.exists(INTERACTIONS_FILE):
        return []
    recs = []
    with open(INTERACTIONS_FILE, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                recs.append(json.loads(line))
            except Exception:
                continue
    if since_minutes is not None:
        cutoff = time.time() - since_minutes * 60
        recs = [r for r in recs if r.get("epoch", 0) >= cutoff]
    if last_n is not None:
        recs = recs[-last_n:]
    return recs


# ---------------------------------------------------------------------------
# 窗口聚合 + 报告
# ---------------------------------------------------------------------------
def evaluate(recent):
    if not recent:
        return None
    results = [analyze(r) for r in recent]
    total = len(results)
    empties = [x for x in results if x["empty"]]
    degraded = [x for x in results if x["degraded"] and not x["empty"]]
    real = [x for x in results if not x["empty"] and not x["degraded"]]
    http_fails = [x for x in results if "http_fail" in x["flags"]]
    poor = [x for x in real if x["band"] == "poor"]
    fair = [x for x in real if x["band"] == "fair"]
    good = [x for x in real if x["band"] == "good"]
    avg_score = (sum(x["score"] for x in real) / len(real)) if real else 0.0

    # 分类均分
    cat_scores = {}
    for x in real:
        cat_scores.setdefault(x["cat"], []).append(x["score"])
    cat_avg = {c: round(sum(v) / len(v), 1) for c, v in cat_scores.items()}

    # 最差样本（real 中分最低，取 3）+ 好样本（>=85，取 2）
    worst = sorted(real, key=lambda x: x["score"])[:3]
    best = sorted([x for x in real if x["score"] >= 85],
                  key=lambda x: -x["score"])[:2]

    rep = {
        "ts": time.strftime("%Y-%m-%d %H:%M:%S"),
        "total": total,
        "empty": len(empties),
        "http_fail": len(http_fails),
        "degraded": len(degraded),
        "real": len(real),
        "good": len(good),
        "fair": len(fair),
        "poor": len(poor),
        "avg_score": round(avg_score, 1),
        "cat_avg": cat_avg,
        "worst": worst,
        "best": best,
        "results": results,
    }
    return rep


def _fmt_sample(x, n):
    snippet = x["ai_text"]
    if len(snippet) > 220:
        snippet = snippet[:220] + "…"
    return ("    [%d] (%s) 分=%d 标志=%s\n"
            "        Q: %s\n"
            "        A: %s" % (n, x["cat"], x["score"], ",".join(x["flags"]) or "-",
                               x["question"], snippet))


def render_report(rep):
    if rep is None:
        return "[%s] AI 质量监控：暂无交互记录（interactions.jsonl 为空，先跑 simulate）" % (
            time.strftime("%Y-%m-%d %H:%M:%S"))
    lines = []
    lines.append("=" * 70)
    lines.append("[%s] AI 回答内容与质量监控报告" % rep["ts"])
    lines.append("-" * 70)
    lines.append("样本总数 : %d  (空=%d  HTTP失败=%d  降级=%d  真实=%d)" % (
        rep["total"], rep["empty"], rep["http_fail"], rep["degraded"], rep["real"]))
    if rep["real"]:
        lines.append("质量分档 : 优=%d  中=%d  差=%d   平均质量分=%s" % (
            rep["good"], rep["fair"], rep["poor"], rep["avg_score"]))
        lines.append("质量优良率: %.1f%%  (分>=80)" % (
            100.0 * rep["good"] / rep["real"]))
        if rep["cat_avg"]:
            lines.append("分类均分 : " + "  ".join(
                "%s=%.0f" % (c, v) for c, v in sorted(
                    rep["cat_avg"].items(), key=lambda kv: kv[1])))
    else:
        lines.append("质量分档 : 无真实回答（全部空/降级）")
    lines.append("-" * 70)
    if rep["worst"]:
        lines.append("最差样本（供人工复核 AI 实际回答内容）:")
        for i, x in enumerate(rep["worst"], 1):
            lines.append(_fmt_sample(x, i))
    if rep["best"]:
        lines.append("优质样本:")
        for i, x in enumerate(rep["best"], 1):
            lines.append(_fmt_sample(x, i + 100))
    # 结论
    lines.append("-" * 70)
    lines.append("评估结论 : %s" % assess(rep))
    lines.append("=" * 70)
    return "\n".join(lines)


def assess(rep):
    issues = []
    if rep["total"] == 0:
        return "无样本"
    if rep["real"] == 0:
        if rep["degraded"]:
            return "全部降级：RAG/LLM 栈未真实应答，需排查 8207/8208/8209 推理栈"
        return "全部空回答：链路异常或 AI 返回空，需排查会话/写库链路"
    if rep["empty"]:
        issues.append("空回答 %d 条" % rep["empty"])
    if rep["http_fail"]:
        issues.append("HTTP失败 %d 条(502/超时，多因推理栈瞬时抖动，看门狗已自愈)" % rep["http_fail"])
    if rep["degraded"]:
        issues.append("降级兜底 %d 条（推理栈过载/未应答）" % rep["degraded"])
    if rep["poor"]:
        issues.append("低质回答 %d 条(分<50，含截断/重复/答非所问)" % rep["poor"])
    if rep["avg_score"] < 60:
        issues.append("平均质量分偏低(%.1f)" % rep["avg_score"])
    if not issues:
        return "内容健康：AI 真实应答、质量分达标、无显著降级/低质。"
    return "需关注: " + "; ".join(issues)


# ---------------------------------------------------------------------------
# 对外入口（供 monitor.py supervisor 调用）
# ---------------------------------------------------------------------------
def evaluate_recent(last_n=50, since_minutes=None, write=True):
    """评估最近窗口，返回报告文本；write=True 时追加到 ai_quality.log。"""
    recs = load_interactions(last_n=last_n, since_minutes=since_minutes)
    rep = evaluate(recs)
    text = render_report(rep)
    if write:
        try:
            with open(QUALITY_LOG, "a", encoding="utf-8") as f:
                f.write(text + "\n\n")
        except Exception:
            pass
    return text


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------
def main():
    p = argparse.ArgumentParser(description="AI 回答内容与质量监控器")
    p.add_argument("--once", action="store_true", help="单次评估并退出")
    p.add_argument("--daemon", action="store_true", help="守护进程：周期评估")
    p.add_argument("--last", type=int, default=50, help="评估最近 N 条，默认 50")
    p.add_argument("--since", type=int, default=None,
                   help="仅评估最近 N 分钟内的回答（与 --last 取交集）")
    p.add_argument("--interval", type=int, default=300,
                   help="守护模式采样间隔秒，默认 300")
    args = p.parse_args()

    if args.once:
        print(evaluate_recent(last_n=args.last, since_minutes=args.since, write=True))
        return

    print("AI 质量监控守护启动，每 %ds 评估最近 %d 条（Ctrl+C 停止）" % (
        args.interval, args.last))
    try:
        while True:
            print(evaluate_recent(last_n=args.last, since_minutes=args.since,
                                  write=True))
            time.sleep(args.interval)
    except KeyboardInterrupt:
        print("\nAI 质量监控退出。")


if __name__ == "__main__":
    main()
