#!/usr/bin/env python3
"""L2 剩余清晰域写测试：email draft / sms draft / customer-sessions。真实写+DB核对+清理。"""
import sys, time
sys.path.insert(0, "/Users/xiaofang/Documents/www/go/hivemtk/hivemtk/user-web/tests/audit")
from testkit import api, db_count, log

def alive(tbl, where):
    return db_count(f"SELECT 1 FROM {tbl} WHERE {where} AND deleted_at IS NULL")

def test_email_draft():
    log("===== 邮件草稿 email/drafts =====")
    subj = "E2E测试邮件草稿_%d" % int(time.time())
    s, j = api("POST", "/api/email/drafts", {"subject": subj, "content": "集成测试内容"})
    log("create:", s)
    if s not in (200,201):
        log("FAIL email create"); return
    did = (j.get("data",{}) or {}).get("id")
    cnt = alive("email_drafts", f"id='{did}'")
    s2,j2 = api("GET", "/api/email/drafts")
    items = (j2.get("data",{}) or {}).get("list") or (j2.get("data",{}) or {}).get("items") or []
    hit = [d for d in items if str((d.get("id") or d.get("ID")))==str(did)]
    s3 = api("PUT", f"/api/email/drafts/{did}", {"id":did,"subject":subj+"_改","content":"改"})[0]
    s4 = api("DELETE", f"/api/email/drafts/{did}")[0]
    cnt2 = alive("email_drafts", f"id='{did}'")
    ok = cnt>=1 and len(hit)>=1 and s3==200 and s4==200 and cnt2==0
    log("EMAIL: DBcr=%s list=%s update=%s delete=%s DBafter=%s => %s"%(cnt,len(hit),s3,s4,cnt2,"PASS" if ok else "FAIL"))
    if cnt2>0:
        api("DELETE", f"/api/email/drafts/{did}")  # 兜底清理

def test_sms_draft():
    log("===== 短信草稿 sms/drafts =====")
    title = "E2E测试短信草稿_%d" % int(time.time())
    s, j = api("POST", "/api/sms/drafts", {"title":title,"content":"集成测试短信内容"})
    log("create:", s)
    if s not in (200,201):
        log("FAIL sms create"); return
    s2,j2 = api("GET", "/api/sms/drafts?page=1&limit=50")
    items = (j2.get("data",{}) or {}).get("list") or (j2.get("data",{}) or {}).get("items") or []
    hit = [d for d in items if (d.get("title") or d.get("Title"))==title]
    did = hit[0].get("id") if hit else None
    cnt = alive("sms_drafts", f"id={did}") if did is not None else -1
    s3 = api("DELETE", f"/api/sms/drafts/{did}")[0] if did is not None else 0
    cnt2 = alive("sms_drafts", f"id={did}") if did is not None else -1
    ok = (did is not None) and cnt>=1 and len(hit)>=1 and s3==200 and cnt2==0
    log("SMS: id=%s DBcr=%s list=%s delete=%s DBafter=%s => %s"%(did,cnt,len(hit),s3,cnt2,"PASS" if ok else "FAIL"))
    if cnt2>0:
        api("DELETE", f"/api/sms/drafts/{did}")

def test_customer_sessions():
    log("===== 客户会话 customer-sessions =====")
    uid = "e2e_user_%d"%int(time.time())
    s, j = api("POST", "/api/customer-sessions", {"platform":"web_embed","account_id":"e2e_test_acct","user_id":uid})
    log("create:", s)
    if s not in (200,201):
        log("FAIL session create"); return
    data = j.get("data", {}) or {}
    sid = data.get("session_id")            # 字符串，用于 messages 端点
    nid = data.get("id")                     # 数字，用于 close 端点
    log("sid=", sid, "nid=", nid)
    cnt = db_count(f"SELECT 1 FROM customer_sessions WHERE session_id='{sid}'")
    s2,j2 = api("POST", f"/api/customer-sessions/{sid}/messages", {"content":"你好，E2E测试消息","sender_type":"customer"})
    log("send msg:", s2)
    time.sleep(3)
    cnt_msg = db_count(f"SELECT 1 FROM session_messages WHERE session_id='{sid}'")
    log("session_messages:", cnt_msg)
    s3 = api("POST", f"/api/customer-sessions/{nid}/close", {"reason":"e2e"})[0]
    log("close:", s3)
    cnt2 = db_count(f"SELECT 1 FROM customer_sessions WHERE session_id='{sid}' AND status='closed'")
    log("closed after:", cnt2)
    ok = cnt>=1 and cnt_msg>=1 and s3==200 and cnt2>=1
    log("SESSION: DBcr=%s msg=%s close=%s => %s"%(cnt,cnt_msg,s3,"PASS" if ok else "FAIL"))

if __name__ == "__main__":
    test_email_draft()
    test_sms_draft()
    test_customer_sessions()
