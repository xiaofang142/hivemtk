#!/usr/bin/env python3
"""
P1-A 知识库绑定 seed: 按智能体业务线分配 FAQ / SOP 模板 ID
依据 2026-07-31 P1-A: 头脑风暴二次论证 - 各 AI 智能体可绑定自己的 FAQ / SOP 范围
"""
import json
import os
import sys
import psycopg2

# 口令仅从环境注入（本文件不落任何明文字面量）：本机 `set -a && . .env && set +a`
_DB_PW = (os.environ.get("POSTGRES_PASSWORD")
          or os.environ.get("HIVEMTK_DB_PASSWORD")
          or os.environ.get("PGPASSWORD"))
if not _DB_PW:
    sys.exit("❌ 缺少数据库口令：请导出 POSTGRES_PASSWORD（或 HIVEMTK_DB_PASSWORD / PGPASSWORD）")

DB = {
    "host": "127.0.0.1",
    "port": 8232,
    "database": "user_db",
    "user": "admin",
    "password": _DB_PW,
}

# 各智能体绑定的 FAQ category 范围 + SOP (intent, stage) 范围
AGENT_BINDING = {
    # 全渠道客服 - 综合
    "seed-cs-passive-01": {
        "name": "全渠道客服智能体",
        "faq_categories": ["logistics", "aftersales", "general"],
        "sop_intents": ["logistics", "aftersales", "general"],
    },
    # 售后专项 - 售后为主
    "seed-cs-passive-02": {
        "name": "售后专项智能体",
        "faq_categories": ["aftersales", "logistics"],
        "sop_intents": ["aftersales", "logistics"],
    },
    # 金融合规 - 支付/价格
    "seed-cs-passive-03": {
        "name": "金融合规客服智能体",
        "faq_categories": ["payment", "pricing"],
        "sop_intents": ["pricing"],
    },
    # 金牌销售 - 产品+定价+促销
    "seed-sales-passive-01": {
        "name": "金牌销售智能体",
        "faq_categories": ["product", "pricing", "promotion"],
        "sop_intents": ["product", "pricing", "promotion"],
    },
    # 高客单价销售
    "seed-sales-passive-02": {
        "name": "高客单价销售智能体",
        "faq_categories": ["product", "pricing"],
        "sop_intents": ["product", "pricing"],
    },
    # 教育行业销售
    "seed-sales-passive-03": {
        "name": "教育行业销售智能体",
        "faq_categories": ["product", "pricing", "membership"],
        "sop_intents": ["product", "pricing"],
    },
    # 客户回访
    "seed-cs-active-01": {
        "name": "客户回访智能体",
        "faq_categories": ["membership", "general", "aftersales"],
        "sop_intents": ["general"],
    },
    # 主动营销
    "seed-sales-active-01": {
        "name": "主动营销智能体",
        "faq_categories": ["promotion", "membership"],
        "sop_intents": ["promotion"],
    },
    # 全栈销售客服 - 全部
    "seed-hybrid-passive-01": {
        "name": "全栈销售客服混合智能体",
        "faq_categories": ["all"],
        "sop_intents": ["all"],
    },
    # 私域复购唤醒
    "seed-hybrid-active-01": {
        "name": "私域复购唤醒智能体",
        "faq_categories": ["membership", "promotion", "aftersales"],
        "sop_intents": ["promotion", "general"],
    },
    # hivemtk 产品服务
    "seed-hivemtk-product-service": {
        "name": "hivemtk 产品服务智能体",
        "faq_categories": ["product", "general"],
        "sop_intents": ["product", "general"],
    },
}


def main():
    conn = psycopg2.connect(**DB)
    cur = conn.cursor()

    # 1) 读取所有 FAQ + SOP
    cur.execute("SELECT id, category FROM faq_entries WHERE enabled = true")
    faq_rows = cur.fetchall()
    faq_by_cat = {}
    for fid, cat in faq_rows:
        faq_by_cat.setdefault(cat, []).append(fid)
    print(f"FAQ total={len(faq_rows)}, by category: {[(k, len(v)) for k, v in faq_by_cat.items()]}")

    cur.execute("SELECT id, intent FROM sop_templates WHERE enabled = true")
    sop_rows = cur.fetchall()
    sop_by_intent = {}
    for sid, intent in sop_rows:
        sop_by_intent.setdefault(intent, []).append(sid)
    print(f"SOP total={len(sop_rows)}, by intent: {[(k, len(v)) for k, v in sop_by_intent.items()]}")

    # 2) 遍历每个智能体，写入绑定
    summary = []
    for code, spec in AGENT_BINDING.items():
        if spec["faq_categories"] == ["all"]:
            faq_ids = [str(fid) for fid, _ in faq_rows]
        else:
            faq_ids = []
            for cat in spec["faq_categories"]:
                faq_ids.extend([str(fid) for fid in faq_by_cat.get(cat, [])])

        if spec["sop_intents"] == ["all"]:
            sop_ids = [str(sid) for sid, _ in sop_rows]
        else:
            sop_ids = []
            for intent in spec["sop_intents"]:
                sop_ids.extend([str(sid) for sid in sop_by_intent.get(intent, [])])

        # 写入数据库 (text[] 数组)
        cur.execute(
            "UPDATE ai_agents SET faq_entry_ids = %s::text[], sop_template_ids = %s::text[] WHERE agent_code = %s RETURNING id",
            (faq_ids, sop_ids, code),
        )
        row = cur.fetchone()
        if not row:
            print(f"⚠️  agent {code} not found, skip")
            continue
        agent_id = row[0]
        summary.append({
            "agent_id": agent_id,
            "agent_code": code,
            "name": spec["name"],
            "faq_count": len(faq_ids),
            "sop_count": len(sop_ids),
        })
        print(f"✓ {code:35s} ({spec['name'][:24]:24s}) FAQ={len(faq_ids):3d} SOP={len(sop_ids):3d}")

    conn.commit()
    conn.close()
    print(f"\n=== 共绑定 {len(summary)} 个智能体 ===")
    for s in summary:
        print(f"  [{s['agent_id']:3d}] {s['agent_code']:35s} FAQ={s['faq_count']:3d} SOP={s['sop_count']:3d}")


if __name__ == "__main__":
    main()
