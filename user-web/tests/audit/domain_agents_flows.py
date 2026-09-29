#!/usr/bin/env python3
"""L2 智能体 + 营销流程域写测试：真实 CRUD + DB 核对 + 清理。"""
import sys, time
sys.path.insert(0, "/Users/xiaofang/Documents/www/go/hivemtk/hivemtk/user-web/tests/audit")
from testkit import api, db_count, log

def test_agents():
    log("===== 智能体 ai-agents =====")
    code = "e2e_%d" % int(time.time())
    body = {
        "agent_code": code, "name": "E2E测试智能体", "description": "集成测试",
        "agent_type": "sales", "persona": "你是测试销售", "system_prompt": "简洁回复",
        "greeting": "您好", "llm_model": "gpt-4o-mini", "temperature": 0.7,
        "max_tokens": 800, "enable_rag": True, "enable_script_match": True,
        "rag_top_k": 3, "max_ai_consecutive": 5,
    }
    s, j = api("POST", "/api/ai-agents", body)
    log("create:", s, str(j)[:150])
    if s not in (200, 201):
        log("FAIL create"); return
    aid = j.get("data", {}).get("id") or j.get("data", {}).get("agent_id")
    log("created id=", aid)
    # DB 核对
    cnt = db_count(f"SELECT 1 FROM ai_agents WHERE id={aid}")
    log("DB ai_agents 存在:", cnt)
    # 列表能查到
    s2, j2 = api("GET", "/api/ai-agents?page_size=200")
    items = (j2.get("data", {}) or {}).get("list") or (j2.get("data", {}) or {}).get("items") or []
    hit = [a for a in items if str(a.get("id"))==str(aid)]
    log("列表包含新建:", len(hit))
    # 更新
    s3, j3 = api("PUT", f"/api/ai-agents/{aid}", {"name": "E2E测试智能体_改"})
    log("update:", s3, str(j3)[:100])
    # 删除 + DB 核对
    s4, j4 = api("DELETE", f"/api/ai-agents/{aid}")
    log("delete:", s4)
    cnt2 = db_count(f"SELECT 1 FROM ai_agents WHERE id={aid}")
    log("DB 删除后存在:", cnt2)
    ok = cnt>=1 and len(hit)>=1 and cnt2==0
    log("AGENTS RESULT:", "PASS" if ok else "FAIL")

def test_flows():
    log("===== 营销流程 marketing-flows =====")
    body = {
        "name": "E2E测试流程", "description": "集成测试",
        "trigger_type": "user_follow", "trigger_config": {"event": "user_follow"},
        "flow_data": {"nodes": [{"id":"n1","type":"trigger","name":"T","config":{}},
                                 {"id":"n2","type":"action","name":"A","config":{}}]},
    }
    s, j = api("POST", "/api/marketing-flows", body)
    log("create:", s, str(j)[:150])
    if s not in (200, 201):
        log("FAIL create flow"); return
    fid = j.get("data", {}).get("id")
    log("created id=", fid)
    cnt = db_count(f"SELECT 1 FROM marketing_flows WHERE id={fid}")
    log("DB marketing_flows 存在:", cnt)
    s2, j2 = api("GET", "/api/marketing-flows?page_size=200")
    items = (j2.get("data", {}) or {}).get("list") or (j2.get("data", {}) or {}).get("items") or []
    hit = [f for f in items if str(f.get("id"))==str(fid)]
    log("列表包含新建:", len(hit))
    # 激活
    s3, j3 = api("POST", f"/api/marketing-flows/{fid}/activate")
    log("activate:", s3, str(j3)[:100])
    s4, j4 = api("DELETE", f"/api/marketing-flows/{fid}")
    log("delete:", s4)
    cnt2 = db_count(f"SELECT 1 FROM marketing_flows WHERE id={fid}")
    log("DB 删除后存在:", cnt2)
    ok = cnt>=1 and len(hit)>=1 and cnt2==0
    log("FLOWS RESULT:", "PASS" if ok else "FAIL")

if __name__ == "__main__":
    test_agents()
    test_flows()
