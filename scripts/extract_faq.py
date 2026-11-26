#!/usr/bin/env python3
"""
extract_faq.py - 从电商客服数据集提取高频问答对作为 FAQ 种子

设计依据: 2026-07-31 AI 智能体性能优化 (T24)
- Layer1 FAQ 库是企业级交付的核心数据资产
- 从已有 E_commerce_Customer_Service 数据集提取 Top 50 高频问答
- 分类: logistics / pricing / aftersales / product / general
- 输出 faq_seed.json, 供 importfaq 工具导入

B-023: 增加 simhash 近似去重
- 同义问题 (海明距离 <= 3) 视为重复, 保留 hit_count 最高的
- 纯 Python 实现, 不引入第三方依赖 (零部署成本)
- 测试: scripts/extract_faq_test.py 验证 5 组同义 -> 输出 <= 5 条

使用:
    python3 scripts/extract_faq.py
    python3 scripts/extract_faq.py --input <custom.jsonl> --output faq.json --top 100
"""
import argparse
import json
import re
import sys
from collections import Counter, defaultdict
from pathlib import Path

# 默认路径 (基于脚本位置；输入数据在工作区根，输出在产品仓 scripts/)
SCRIPT_DIR = Path(__file__).resolve().parent
WORKSPACE_ROOT = SCRIPT_DIR.parent.parent
DEFAULT_INPUT = WORKSPACE_ROOT / "E_commerce_Customer_Service/test_clean_v2.jsonl"
DEFAULT_OUTPUT = SCRIPT_DIR / "faq_seed.json"
DEFAULT_TOP_N = 50

# simhash 指纹位数
# 中文短问句的 token 数通常 < 15, 64-bit 会因"每 token 贡献 bit 翻转数翻倍"导致
# 微小变体距离过大 (>10), 无法通过 3-bit 阈值。
# 32-bit 在保留 5-10 token 的文本上, 1-2 字变体的海明距离通常在 1-3 之间, 满足阈值。
SIMHASH_BITS = 32
# 海明距离阈值, 距离 <= 3 视为同义
SIMHASH_HAMMING_THRESHOLD = 3


def classify_intent(question: str) -> str:
    """根据关键词分类意图"""
    q = question.lower()
    if any(k in q for k in ["邮", "快递", "发货", "韵达", "邮政", "顺丰", "ems", "中通", "圆通", "申通", "物流", "到货"]):
        return "logistics"
    if any(k in q for k in ["价格", "多少钱", "优惠", "折扣", "便宜", "贵", "价"]):
        return "pricing"
    if any(k in q for k in ["退", "换", "退款", "退货", "换货", "售后"]):
        return "aftersales"
    if any(k in q for k in ["尺码", "尺寸", "颜色", "规格", "材质", "质量", "好不好", "怎么样"]):
        return "product"
    if any(k in q for k in ["活动", "促销", "买", "下单", "订单", "支付"]):
        return "order"
    return "general"


def normalize_text(s: str) -> str:
    """标准化文本 (去空格/标点, 转小写) 用于去重"""
    return re.sub(r"\s+", "", s).strip().lower()


# =============================================================================
# simhash 实现 (纯 Python, 零依赖)
# =============================================================================
#
# 算法:
#   1. 中文按字符 bigram 切词 + 英文按词切, 形成 token 列表
#   2. 每个 token 用 FNV-1a 64-bit hash 作为特征
#   3. 64 个 bit 累加: hash 的 bit=1 加权 +1, bit=0 加权 -1
#   4. 累加和 > 0 -> 指纹 bit=1, 否则 bit=0
#   5. 两个指纹的"海明距离"=不同 bit 数
#   6. 海明距离 <= 3 视为同义 (经验值, 对短问句/微小变体鲁棒)


def _fnv1a_64(token: str) -> int:
    """FNV-1a 64-bit hash (纯 Python, 不依赖 hashlib)"""
    h = 0xcbf29ce484222325
    for ch in token:
        h ^= ord(ch)
        h = (h * 0x100000001b3) & 0xFFFFFFFFFFFFFFFF
    return h


def _tokenize_for_simhash(text: str) -> list:
    """simhash 切词: 中文 (unigram + bigram) + 英文 word

    B-023: 同时使用 unigram (单字) 和 bigram (双字) 是关键,
    否则 simhash 对"加一个字"型变体距离很大 (阈值 3 无法通过)。
    unigram 提供字符级信号, bigram 提供词组级信号。
    """
    text = normalize_text(text)
    if not text:
        return []
    tokens = []
    # 中文 unigram (单字, 鲁棒)
    for ch in text:
        if _is_cjk(ch):
            tokens.append(ch)
    # 中文 bigram (双字, 增强词组信号)
    for i in range(len(text) - 1):
        if _is_cjk(text[i]) and _is_cjk(text[i + 1]):
            tokens.append(text[i:i + 2])
    # 英文按非字母切
    for w in re.split(r"[^a-z0-9]+", text):
        if len(w) >= 2:
            tokens.append(w)
    return tokens


def _is_cjk(ch: str) -> bool:
    return "\u4e00" <= ch <= "\u9fff"


def simhash64(text: str) -> int:
    """计算 64-bit simhash 指纹 (B-023)"""
    tokens = _tokenize_for_simhash(text)
    if not tokens:
        return 0
    # 64 个 bit 累加器
    v = [0] * SIMHASH_BITS
    for tok in tokens:
        h = _fnv1a_64(tok)
        for i in range(SIMHASH_BITS):
            if (h >> i) & 1:
                v[i] += 1
            else:
                v[i] -= 1
    # 累加和 > 0 -> bit=1
    fingerprint = 0
    for i in range(SIMHASH_BITS):
        if v[i] > 0:
            fingerprint |= 1 << i
    return fingerprint


def hamming_distance(a: int, b: int) -> int:
    """两个 64-bit 整数的海明距离 (不同 bit 数)"""
    x = a ^ b
    # bin(x).count('1'), 但避免 str 转换
    cnt = 0
    while x:
        cnt += x & 1
        x >>= 1
    return cnt


def _jaccard(a_tokens: list, b_tokens: list) -> float:
    """Jaccard 相似度 = |A ∩ B| / |A ∪ B|"""
    if not a_tokens or not b_tokens:
        return 0.0
    sa, sb = set(a_tokens), set(b_tokens)
    inter = len(sa & sb)
    union = len(sa | sb)
    return inter / union if union else 0.0


def dedup_by_simhash(entries: list, threshold: int = SIMHASH_HAMMING_THRESHOLD) -> list:
    """simhash 近似去重 (B-023)

    入参: entries 列表, 每条至少含 question, hit_count 字段
    行为: 同义问题视为重复组, 保留 hit_count 最高者

    判定规则 (任一满足即为重复):
      1. simhash 海明距离 <= threshold (默认 3) - 抓"1-2 字之内"的近重复
      2. token jaccard 相似度 >= 0.7 - 抓"加前缀/加字/近义改写"
    两个规则结合后, 短问句去重更鲁棒。

    返回: 去重后的列表 (顺序: 优先 hit_count 高, 然后首次出现)
    """
    if not entries:
        return []
    # 按 hit_count 降序排, 高频优先保留
    sorted_entries = sorted(entries, key=lambda e: (-int(e.get("hit_count", 0)), e.get("question", "")))
    # 缓存 token 列表 (同问题只切一次)
    token_cache: dict = {}
    def _toks(q: str) -> list:
        if q not in token_cache:
            token_cache[q] = _tokenize_for_simhash(q)
        return token_cache[q]
    kept: list = []
    kept_fps: list = []
    kept_toks: list = []
    for e in sorted_entries:
        q = e.get("question", "")
        fp = simhash64(q)
        toks = _toks(q)
        is_dup = False
        for kept_fp, kept_tok in zip(kept_fps, kept_toks):
            if hamming_distance(fp, kept_fp) <= threshold:
                is_dup = True
                break
            if _jaccard(toks, kept_tok) >= 0.7:
                is_dup = True
                break
        if not is_dup:
            kept.append(e)
            kept_fps.append(fp)
            kept_toks.append(toks)
    return kept


def extract_pairs(input_path: Path, top_n: int) -> list[dict]:
    """从 JSONL 数据集提取问答对并按频次排序"""
    if not input_path.exists():
        raise FileNotFoundError(f"输入文件不存在: {input_path}")

    pair_counter: Counter = Counter()
    answer_pool: defaultdict = defaultdict(list)
    seen_normalized: set = set()

    with input_path.open(encoding="utf-8") as f:
        for line_num, line in enumerate(f, 1):
            try:
                item = json.loads(line)
            except json.JSONDecodeError:
                continue
            messages = item.get("messages", [])
            user_q = None
            assistant_a = None
            for m in messages:
                if m.get("role") == "user" and user_q is None:
                    user_q = m.get("content", "").strip()
                elif m.get("role") == "assistant":
                    assistant_a = m.get("content", "").strip()
            if not user_q or not assistant_a:
                continue
            if len(user_q) < 3 or len(user_q) > 200:
                continue
            if len(assistant_a) < 1 or len(assistant_a) > 500:
                continue
            # 用标准化问题去重
            key = normalize_text(user_q)
            if key in seen_normalized:
                continue
            seen_normalized.add(key)
            pair_counter[(user_q, assistant_a)] += 1
            answer_pool[user_q].append(assistant_a)

    # 取 Top N (按频次降序)
    top = pair_counter.most_common(top_n)
    candidates = []
    for (q, a), count in top:
        # 关键词: 抽取名词短语 (简化版, 用字符 bigram)
        keywords = []
        # 抽取中文 2-gram 关键词
        chars = re.findall(r"[\u4e00-\u9fff]+", q)
        for word in chars:
            for i in range(len(word) - 1):
                kw = word[i:i + 2]
                if kw not in keywords:
                    keywords.append(kw)
        keywords = keywords[:5]  # 最多 5 个关键词
        candidates.append({
            "question": q,
            "answer": a,
            "hit_count": count,
            "intent": classify_intent(q),
            "category": classify_intent(q),
            "keywords": keywords,
            "confidence": 0.85,
            "enabled": True,
        })

    # simhash 近似去重 (按 hit_count 高的保留)
    out = dedup_by_simhash(candidates, SIMHASH_HAMMING_THRESHOLD)
    return out


def main() -> int:
    parser = argparse.ArgumentParser(description="从电商客服数据集提取 FAQ 种子")
    parser.add_argument("--input", type=Path, default=DEFAULT_INPUT, help="输入 JSONL 路径")
    parser.add_argument("--output", type=Path, default=DEFAULT_OUTPUT, help="输出 JSON 路径")
    parser.add_argument("--top", type=int, default=DEFAULT_TOP_N, help="Top N 高频问答")
    args = parser.parse_args()

    try:
        entries = extract_pairs(args.input, args.top)
    except FileNotFoundError as e:
        print(f"[ERROR] {e}", file=sys.stderr)
        return 1

    # 确保输出目录存在
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(entries, ensure_ascii=False, indent=2), encoding="utf-8")

    # 统计分类分布
    by_intent = Counter(e["intent"] for e in entries)
    print(f"[OK] 已提取 {len(entries)} 条 FAQ 种子 (含 simhash 去重)")
    print(f"[OK] 输出文件: {args.output}")
    print(f"[INFO] 分类分布: {dict(by_intent)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
