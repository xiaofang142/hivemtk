#!/usr/bin/env python3
"""L2 调优域写测试：confidence/policies upsert + prompt/candidates/:id/status。真实写+DB核对。"""
import sys, time
sys.path.insert(0, "/Users/xiaofang/Documents/www/go/hivemtk/hivemtk/user-web/tests/audit")
from testkit import api, db_count, log, db_rows

def test_policies():
    log("===== 调优 confidence/policies upsert =====")
    pid = "e2e_policy_%d" % int(time.time())
    body = {
        "policy_id": pid, "intent_type": "sales", "base_threshold": 0.75,
        "customer_level_weight": 0.1, "timeslot_weight": 0.1, "agent_availability_weight": 0.1,
        "band_handoff_upper": 0.9, "band_fallback_upper": 0.4, "band_review_upper": 0.6,
        "review_sla_seconds": 300, "version": 1,
    }
    s, j = api("PUT", "/api/admin/tuning/confidence/policies", body)
    log("upsert:", s)
    if s != 200:
        log("FAIL policies"); return
    # DB 核对（threshold_policies 表）
    cnt = db_count(f"SELECT 1 FROM threshold_policies WHERE policy_id='{pid}'")
    log("DB threshold_policies:", cnt)
    # 列表能查到
    s2, j2 = api("GET", "/api/admin/tuning/confidence/policies")
    items = (j2.get("data",{}) or {}).get("list") or (j2.get("data",{}) or {}).get("items") or []
    hit = [d for d in items if (d.get("policy_id") or d.get("PolicyID"))==pid]
    log("列表包含:", len(hit))
    # 清理：再次 upsert 不改表，需直接清（无 delete 端点）-> 标记
    ok = cnt>=1 and len(hit)>=1
    log("POLICIES RESULT:", "PASS" if ok else "FAIL")
    if cnt>=1:
        # 清理测试数据
        import subprocess, os
        pw = os.environ.get("PGPASSWORD","")
        cmd=["psql","-h","localhost","-p","8232","-U","admin","-d","user_db","-c",
             f"DELETE FROM threshold_policies WHERE policy_id='{pid}';"]
        env=dict(os.environ); env["PGPASSWORD"]=pw
        subprocess.run(cmd, capture_output=True, env=env)

def test_prompt_candidate_status():
    log("===== 调优 prompt/candidates/:id/status =====")
    # 取一个候选 id
    s, j = api("GET", "/api/admin/tuning/prompt/candidates?page=1&limit=10")
    items = (j.get("data",{}) or {}).get("list") or (j.get("data",{}) or {}).get("items") or []
    if not items:
        log("无候选，跳过"); return
    cid = items[0].get("id") or items[0].get("candidate_id")
    old_status = items[0].get("status")
    s2, j2 = api("PUT", f"/api/admin/tuning/prompt/candidates/{cid}/status?status=approved")
    log("update status:", s2, str(j2)[:100])
    # 核对 DB
    rows = db_rows(f"SELECT status FROM prompt_candidates WHERE id='{cid}'")
    new_status = rows[0][0] if rows else None
    log("DB status:", new_status, "期望 approved")
    # 还原
    if old_status:
        api("PUT", f"/api/admin/tuning/prompt/candidates/{cid}/status?status={old_status}")
    ok = s2==200 and new_status=="approved"
    log("CANDIDATE RESULT:", "PASS" if ok else "FAIL")

if __name__ == "__main__":
    test_policies()
    test_prompt_candidate_status()
