#!/usr/bin/env python3
"""L2 知识库域写测试：import/text 真实导入 + 核对 DB documents/chunks 落库 + 清理。"""
import sys, time
sys.path.insert(0, "/Users/xiaofang/Documents/www/go/hivemtk/hivemtk/user-web/tests/audit")
from testkit import api, db_count, log

def main():
    # 取真实产品 id
    s, j = api("GET", "/api/rag-config/products")
    log("rag-config/products:", s, str(j)[:200])
    products = (j.get("data") or {}).get("items") or (j.get("data") or {}).get("list") or []
    if s != 200 or not products:
        log("无产品，跳过导入测试"); return
    pid = str(products[0]["id"])
    log("使用 product_id =", pid, "name=", products[0].get("name"))

    before_doc = db_count("SELECT 1 FROM knowledge_documents")
    before_chunk = db_count("SELECT 1 FROM knowledge_chunks")
    log("导入前: doc=", before_doc, "chunk=", before_chunk)

    # 真实文本导入
    payload = {
        "product_id": pid,
        "title": "E2E测试文档_"+time.strftime("%H%M%S"),
        "content": "这是一个端到端测试文档。退货政策：七天内无理由退货。运费说明：满99元包邮。",
        "category": "e2e-test",
        "tags": ["e2e"],
    }
    s2, j2 = api("POST", "/api/knowledge/import/text", payload)
    log("import/text:", s2, str(j2)[:300])
    if s2 not in (200, 201):
        log("导入失败，终止"); return

    time.sleep(3)  # 等待异步落库/分块
    # 核对 DB 是否新增
    after_doc = db_count("SELECT 1 FROM knowledge_documents WHERE category='e2e-test'")
    after_chunk = db_count("SELECT 1 FROM knowledge_chunks WHERE content LIKE '%退货政策%'")
    log("导入后(e2e-test doc):", after_doc, " 含'退货政策'chunk:", after_chunk)

    # 核对列表接口能查到（按 product 拉全部，按 title 前缀匹配）
    s3, j3 = api("GET", f"/api/knowledge/documents?product_id={pid}&page_size=100")
    items = (j3.get("data",{}).get("items") or [])
    found = [d for d in items if (d.get("title") or "").startswith("E2E测试文档")]
    log("列表查到 e2e 文档数:", len(found), " 列表total=", j3.get("data",{}).get("total"))

    # 清理：删除测试文档
    if found:
        did = found[0]["id"]
        s4, j4 = api("DELETE", f"/api/knowledge/documents/{did}", {"product_id": pid})
        log("delete doc:", s4, str(j4)[:150])
        time.sleep(1)
        after_del = db_count(f"SELECT 1 FROM knowledge_documents WHERE id={did}")
        log("删除后该文档是否存在:", after_del)

    # 断言
    ok = (after_doc >= 1) and (after_chunk >= 1) and (len(found) >= 1)
    log("RESULT:", "PASS" if ok else "FAIL")

if __name__ == "__main__":
    main()
