#!/usr/bin/env python3
"""L2 系统用户管理域写测试：users CRUD + DB 核对 + 清理。"""
import sys, time
sys.path.insert(0, "/Users/xiaofang/Documents/www/go/hivemtk/hivemtk/user-web/tests/audit")
from testkit import api, db_count, log

def test_users():
    log("===== 系统用户管理 system/users =====")
    uname = "e2e_user_%d" % int(time.time())
    body = {"username": uname, "password": "E2epass123", "email": f"e2e_{int(time.time())}@test.com",
            "name": "E2E测试账号", "role": "staff"}
    s, j = api("POST", "/api/system/users", body)
    log("create:", s)
    if s != 200:
        log("FAIL create"); return
    uid = (j.get("data",{}) or {}).get("id")
    log("uid=", uid)
    cnt = db_count(f"SELECT 1 FROM system_users WHERE id={uid}")
    s2,j2 = api("GET", "/api/system/users")
    items = (j2.get("data",{}) or {}).get("list") or (j2.get("data",{}) or {}).get("items") or []
    hit = [d for d in items if str((d.get("id") or d.get("ID")))==str(uid)]
    s3 = api("PUT", f"/api/system/users/{uid}", {"name":"E2E测试账号_改"})[0]
    # 重置密码
    s4 = api("PUT", f"/api/system/permissions/{uid}/password", {"password":"Newpass123"})[0]
    s5 = api("DELETE", f"/api/system/users/{uid}")[0]
    cnt2 = db_count(f"SELECT 1 FROM system_users WHERE id={uid}")
    ok = cnt>=1 and len(hit)>=1 and s3==200 and s4==200 and s5==200 and cnt2==0
    log("USERS: DBcr=%s list=%s update=%s pwreset=%s delete=%s DBafter=%s => %s"%(cnt,len(hit),s3,s4,s5,cnt2,"PASS" if ok else "FAIL"))
    if cnt2>0:
        api("DELETE", f"/api/system/users/{uid}")

if __name__ == "__main__":
    test_users()
