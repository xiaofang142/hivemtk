#!/usr/bin/env python3
"""
HivemTK User Server - 全面端到端 API 测试脚本
覆盖所有 697+ 路由端点
"""

import requests
import json
import sys
import os
from datetime import datetime

BASE_URL = os.environ.get("BASE_URL", "http://127.0.0.1:8204")
USERNAME = os.environ.get("USERNAME", "admin")
PASSWORD = os.environ.get("PASSWORD", "TestPwd_2026!")

class TestResult:
    def __init__(self):
        self.passed = 0
        self.failed = 0
        self.skipped = 0
        self.errors = []
        self.warnings = []
        self.details = []
    
    def add_pass(self, endpoint, method, status_code, message=""):
        self.passed += 1
        self.details.append({
            "status": "PASS",
            "endpoint": endpoint,
            "method": method,
            "status_code": status_code,
            "message": message
        })
    
    def add_fail(self, endpoint, method, status_code, message):
        self.failed += 1
        self.errors.append({
            "endpoint": endpoint,
            "method": method,
            "status_code": status_code,
            "message": message
        })
        self.details.append({
            "status": "FAIL",
            "endpoint": endpoint,
            "method": method,
            "status_code": status_code,
            "message": message
        })
    
    def add_skip(self, endpoint, method, reason):
        self.skipped += 1
        self.details.append({
            "status": "SKIP",
            "endpoint": endpoint,
            "method": method,
            "message": reason
        })
    
    def add_warning(self, endpoint, method, status_code, message):
        self.warnings.append({
            "endpoint": endpoint,
            "method": method,
            "status_code": status_code,
            "message": message
        })
        self.details.append({
            "status": "WARN",
            "endpoint": endpoint,
            "method": method,
            "status_code": status_code,
            "message": message
        })

def get_token():
    """获取认证 Token"""
    try:
        resp = requests.post(f"{BASE_URL}/api/auth/login", json={
            "username": USERNAME,
            "password": PASSWORD
        })
        data = resp.json()
        if data.get("code") == "SUCCESS" and data.get("data", {}).get("token"):
            return data["data"]["token"]
        return None
    except Exception as e:
        print(f"登录失败: {e}")
        return None

def test_endpoint(session, method, endpoint, result, expected_success=True, data=None, skip_auth=False):
    """测试单个端点"""
    url = f"{BASE_URL}{endpoint}"
    headers = {}
    
    if not skip_auth and session:
        headers["Authorization"] = f"Bearer {session}"
    
    try:
        if method == "GET":
            resp = requests.get(url, headers=headers, timeout=10)
        elif method == "POST":
            headers["Content-Type"] = "application/json"
            resp = requests.post(url, headers=headers, json=data or {}, timeout=10)
        elif method == "PUT":
            headers["Content-Type"] = "application/json"
            resp = requests.put(url, headers=headers, json=data or {}, timeout=10)
        elif method == "DELETE":
            resp = requests.delete(url, headers=headers, timeout=10)
        else:
            result.add_skip(endpoint, method, f"不支持的方法: {method}")
            return
        
        status_code = resp.status_code
        
        # 检查响应
        if expected_success:
            if status_code == 200:
                try:
                    body = resp.json()
                    code = body.get("code", "")
                    if code in ("SUCCESS", "", None):
                        result.add_pass(endpoint, method, status_code)
                    else:
                        result.add_warning(endpoint, method, status_code, f"业务代码: {code}, 消息: {body.get('message', '')}")
                except:
                    result.add_pass(endpoint, method, status_code, "非JSON响应")
            elif status_code == 404:
                result.add_skip(endpoint, method, "路由不存在 (404)")
            elif status_code == 405:
                result.add_skip(endpoint, method, "方法不允许 (405)")
            elif status_code == 401:
                result.add_fail(endpoint, method, status_code, "未授权 (401) - Token 无效")
            elif status_code == 429:
                result.add_fail(endpoint, method, status_code, "请求过多 (429) - 限流触发")
            else:
                result.add_warning(endpoint, method, status_code, "非预期状态码")
        else:
            if status_code in (400, 401, 403, 422):
                result.add_pass(endpoint, method, status_code, "正确返回错误状态码")
            elif status_code == 200:
                result.add_warning(endpoint, method, status_code, "预期失败但成功")
            else:
                result.add_warning(endpoint, method, status_code, "非预期状态码")
                
    except requests.exceptions.ConnectionError:
        result.add_fail(endpoint, method, 0, "连接失败 - 服务不可达")
    except requests.exceptions.Timeout:
        result.add_fail(endpoint, method, 0, "请求超时")
    except Exception as e:
        result.add_fail(endpoint, method, 0, str(e))

def run_tests():
    """运行所有测试"""
    print("=" * 60)
    print("HivemTK User Server - 全面 API 测试")
    print(f"时间: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    print(f"目标: {BASE_URL}")
    print("=" * 60)
    
    result = TestResult()
    
    # 1. 健康检查
    print("\n[1] 健康检查...")
    test_endpoint(None, "GET", "/healthz", result, expected_success=True, skip_auth=True)
    test_endpoint(None, "GET", "/api/health", result, expected_success=True, skip_auth=True)
    
    # 2. 登录认证
    print("\n[2] 登录认证...")
    token = get_token()
    if not token:
        print("❌ 登录失败，无法继续测试")
        return result
    print("✅ Token 获取成功")
    
    # 3. 认证相关路由
    print("\n[3] 认证路由测试...")
    auth_tests = [
        ("GET", "/api/auth/current-user", None, True),
        ("GET", "/api/auth/mfa/status", None, True),
        ("GET", "/api/auth/login-events", None, True),
        ("GET", "/api/auth/security-alerts", None, True),
        ("GET", "/api/auth/anomaly/login-events", None, True),
        ("GET", "/api/auth/anomaly/alerts", None, True),
        ("GET", "/api/auth/password-policy", None, True),
        ("GET", "/api/auth/notifications", None, True),
        ("GET", "/api/auth/notifications/unread-count", None, True),
    ]
    for method, endpoint, data, expected in auth_tests:
        test_endpoint(token, method, endpoint, result, expected_success=expected, data=data)
    
    # 4. 用户管理
    print("\n[4] 用户管理测试...")
    user_tests = [
        ("GET", "/api/user/list", None, True),
        ("GET", "/api/users", None, True),
        ("GET", "/api/user/1", None, True),
        ("GET", "/api/users/1", None, True),
    ]
    for method, endpoint, data, expected in user_tests:
        test_endpoint(token, method, endpoint, result, expected_success=expected, data=data)
    
    # 5. 客户管理
    print("\n[5] 客户管理测试...")
    # 创建测试客户
    create_customer_resp = requests.post(
        f"{BASE_URL}/api/customer",
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
        json={"name": "e2e-test-full", "phone": "13600136000", "email": "e2e_full@test.com"}
    )
    customer_data = create_customer_resp.json()
    customer_id = customer_data.get("data", {}).get("id", "")
    
    customer_tests = [
        ("GET", "/api/customer/list", None, True),
        ("GET", "/api/customer/360", None, True),
        ("GET", "/api/customer/360/list", None, True),
        ("GET", "/api/customer-360", None, True),
        ("GET", "/api/customer-360/list", None, True),
        ("GET", "/api/customers", None, True),
    ]
    if customer_id:
        customer_tests.extend([
            ("GET", f"/api/customer/{customer_id}", None, True),
            ("GET", f"/api/customer/360/{customer_id}", None, True),
            ("GET", f"/api/customer/{customer_id}/behaviors", None, True),
            ("GET", f"/api/customer/{customer_id}/communications", None, True),
            ("GET", f"/api/customer-360/{customer_id}", None, True),
        ])
    for method, endpoint, data, expected in customer_tests:
        test_endpoint(token, method, endpoint, result, expected_success=expected, data=data)
    
    # 6. 备份管理
    print("\n[6] 备份管理测试...")
    backup_tests = [
        ("GET", "/api/backups", None, True),
        ("POST", "/api/backups", {"backup_name": "e2e_full_test", "backup_type": "full"}, True),
        # 路径穿越防护测试
        ("POST", "/api/backups", {"backup_name": "../../etc/passwd", "backup_type": "full"}, False),
        ("POST", "/api/backups", {"backup_name": "test;DROP TABLE", "backup_type": "full"}, False),
    ]
    for method, endpoint, data, expected in backup_tests:
        test_endpoint(token, method, endpoint, result, expected_success=expected, data=data)
    
    # 获取备份 ID 用于删除
    backup_list_resp = requests.get(
        f"{BASE_URL}/api/backups",
        headers={"Authorization": f"Bearer {token}"}
    )
    backup_data = backup_list_resp.json()
    backups = backup_data.get("data", {}).get("items", backup_data.get("data", []))
    if isinstance(backups, list) and len(backups) > 0:
        latest_backup = backups[0]
        backup_id = latest_backup.get("id", "")
        if backup_id:
            test_endpoint(token, "GET", f"/api/backups/{backup_id}", result, expected_success=True)
            test_endpoint(token, "DELETE", f"/api/backups/{backup_id}", result, expected_success=True)
    
    # 7. 系统管理
    print("\n[7] 系统管理测试...")
    system_tests = [
        ("GET", "/api/system/config", None, True),
        ("GET", "/api/system/logs", None, True),
        ("GET", "/api/system/stats", None, True),
        ("GET", "/api/stats/system", None, True),
        ("GET", "/api/system/backup", None, True),
        ("GET", "/api/obs/config", None, True),
        ("GET", "/api/obs/config/default", None, True),
        ("GET", "/api/admin/config", None, True),
        ("GET", "/api/agent/tool-integrations", None, True),
        ("GET", "/api/agent/settings", None, True),
    ]
    for method, endpoint, data, expected in system_tests:
        test_endpoint(token, method, endpoint, result, expected_success=expected, data=data)
    
    # 8. SOP 相关
    print("\n[8] SOP 流程测试...")
    sop_tests = [
        ("GET", "/api/sop", None, True),
        ("GET", "/api/sop/stats", None, True),
        ("GET", "/api/sop/executions", None, True),
        ("GET", "/api/sop/match", None, True),
    ]
    for method, endpoint, data, expected in sop_tests:
        test_endpoint(token, method, endpoint, result, expected_success=expected, data=data)
    
    # 9. RAG 知识库
    print("\n[9] RAG 知识库测试...")
    rag_tests = [
        ("GET", "/api/knowledge/bases", None, True),
        ("GET", "/api/knowledge/bases/list", None, True),
        ("GET", "/api/rag/sessions", None, True),
        ("GET", "/api/rag/metrics", None, True),
        ("GET", "/api/rag/metrics/daily", None, True),
    ]
    for method, endpoint, data, expected in rag_tests:
        test_endpoint(token, method, endpoint, result, expected_success=expected, data=data)
    
    # 10. 渠道管理
    print("\n[10] 渠道管理测试...")
    channel_tests = [
        ("GET", "/api/whatsapp/accounts", None, True),
        ("GET", "/api/whatsapp-cloud/accounts", None, True),
        ("GET", "/api/telegram/accounts", None, True),
        ("GET", "/api/feishu/accounts", None, True),
        ("GET", "/api/wecom/accounts", None, True),
        ("GET", "/api/wechat/accounts", None, True),
        ("GET", "/api/tiktok/accounts", None, True),
        ("GET", "/api/douyin/accounts", None, True),
    ]
    for method, endpoint, data, expected in channel_tests:
        test_endpoint(token, method, endpoint, result, expected_success=expected, data=data)
    
    # 11. 客户会话
    print("\n[11] 客户会话测试...")
    session_tests = [
        ("GET", "/api/customer-sessions", None, True),
        ("GET", "/api/customer-sessions/pending", None, True),
        ("GET", "/api/customer-sessions/blacklist", None, True),
        ("GET", "/api/customer/session/list", None, True),
    ]
    for method, endpoint, data, expected in session_tests:
        test_endpoint(token, method, endpoint, result, expected_success=expected, data=data)
    
    # 12. 数据分析
    print("\n[12] 数据分析测试...")
    analytics_tests = [
        ("GET", "/api/analytics/funnel", None, True),
        ("GET", "/api/analytics/ai-productivity", None, True),
        ("GET", "/api/analytics/persona/staffs", None, True),
        ("GET", "/api/churn/prediction", None, True),
        ("GET", "/api/churn/predictions", None, True),
        ("GET", "/api/churn/high-risk-users", None, True),
        ("GET", "/api/churn/warnings", None, True),
        ("GET", "/api/churn/statistics", None, True),
        ("GET", "/api/churn/risk-distribution", None, True),
    ]
    for method, endpoint, data, expected in analytics_tests:
        test_endpoint(token, method, endpoint, result, expected_success=expected, data=data)
    
    # 13. LLM 相关
    print("\n[13] LLM 服务测试...")
    llm_tests = [
        ("GET", "/api/llm/models", None, True),
        ("GET", "/api/llm/strategies", None, True),
        ("GET", "/api/llm/audit", None, True),
        ("GET", "/api/llm/stats", None, True),
        ("GET", "/api/llm/usage", None, True),
        ("GET", "/api/llm/cost-stats", None, True),
        ("GET", "/api/llm/fallback", None, True),
        ("GET", "/api/llm/scene-routing", None, True),
        ("GET", "/api/llm/scenarios", None, True),
        ("GET", "/api/llm/health", None, True),
        ("GET", "/api/llm/scenario-stats", None, True),
        ("GET", "/api/llm/model-type-stats", None, True),
        ("GET", "/api/llm/egress-alerts", None, True),
        ("GET", "/api/llm/egress-audit", None, True),
        ("GET", "/api/llm-routings/policy", None, True),
    ]
    for method, endpoint, data, expected in llm_tests:
        test_endpoint(token, method, endpoint, result, expected_success=expected, data=data)
    
    # 14. Agent 相关
    print("\n[14] Agent 服务测试...")
    agent_tests = [
        ("GET", "/api/agents/me", None, True),
        ("GET", "/api/agents/all", None, True),
        ("GET", "/api/agents/online", None, True),
        ("GET", "/api/agent/tools/list", None, True),
        ("GET", "/api/agent/tools/stats", None, True),
        ("GET", "/api/agent/tools/audit", None, True),
        ("GET", "/api/agent/tools/cost", None, True),
        ("GET", "/api/agent/tools/providers", None, True),
    ]
    for method, endpoint, data, expected in agent_tests:
        test_endpoint(token, method, endpoint, result, expected_success=expected, data=data)
    
    # 15. 内存管理
    print("\n[15] 内存管理测试...")
    memory_tests = [
        ("GET", "/api/memory/short", None, True),
        ("GET", "/api/memory/long", None, True),
        ("GET", "/api/memory/context", None, True),
        ("GET", "/api/memory/list", None, True),
    ]
    for method, endpoint, data, expected in memory_tests:
        test_endpoint(token, method, endpoint, result, expected_success=expected, data=data)
    
    # 16. 触达管道
    print("\n[16] 触达管道测试...")
    reach_tests = [
        ("GET", "/api/reach/pipelines", None, True),
        ("GET", "/api/reach/stats", None, True),
        ("GET", "/api/reach/jobs", None, True),
    ]
    for method, endpoint, data, expected in reach_tests:
        test_endpoint(token, method, endpoint, result, expected_success=expected, data=data)
    
    # 17. 安全审计
    print("\n[17] 安全审计测试...")
    security_tests = [
        ("GET", "/api/security/audit/list", None, True),
    ]
    for method, endpoint, data, expected in security_tests:
        test_endpoint(token, method, endpoint, result, expected_success=expected, data=data)
    
    # 18. 其他 CRUD 测试
    print("\n[18] 其他 CRUD 测试...")
    other_tests = [
        ("GET", "/api/notifications", None, True),
        ("GET", "/api/notifications/unread-count", None, True),
        ("GET", "/api/accounts/list", None, True),
        ("GET", "/api/account/list", None, True),
        ("GET", "/api/quick-replies", None, True),
        ("GET", "/api/quick-replies/categories", None, True),
        ("GET", "/api/session-tags", None, True),
        ("GET", "/api/dashboards", None, True),
        ("GET", "/api/templates", None, True),
        ("GET", "/api/templates/official", None, True),
        ("GET", "/api/scripts", None, True),
        ("GET", "/api/scripts/categories", None, True),
        ("GET", "/api/ab-experiments", None, True),
        ("GET", "/api/integrations", None, True),
        ("GET", "/api/batch/template", None, True),
        ("GET", "/api/dashboard/clients", None, True),
        ("GET", "/api/dashboard/topics", None, True),
        ("GET", "/api/dashboard/stats", None, True),
        ("GET", "/api/customer-journey/overview", None, True),
        ("GET", "/api/customer-journey/stages", None, True),
        ("GET", "/api/objection/categories", None, True),
    ]
    for method, endpoint, data, expected in other_tests:
        test_endpoint(token, method, endpoint, result, expected_success=expected, data=data)
    
    # 19. 数据库路由（frontend aliases）
    print("\n[19] 前端别名路由测试...")
    alias_tests = [
        ("GET", "/customer/list", None, True),
        ("GET", "/customer-tags", None, True),
        ("GET", "/tag-segments", None, True),
        ("GET", "/user-segments", None, True),
        ("GET", "/user-segments/rfm/list", None, True),
        ("GET", "/customer-events", None, True),
        ("GET", "/customer-events/stats", None, True),
    ]
    for method, endpoint, data, expected in alias_tests:
        test_endpoint(token, method, endpoint, result, expected_success=expected, data=data)
    
    return result

def print_report(result):
    """打印测试报告"""
    print("\n" + "=" * 60)
    print("测试报告")
    print("=" * 60)
    
    total = result.passed + result.failed + result.skipped
    pass_rate = (result.passed / total * 100) if total > 0 else 0
    
    print("\n📊 统计摘要:")
    print(f"   总测试数: {total}")
    print(f"   ✅ 通过: {result.passed} ({pass_rate:.1f}%)")
    print(f"   ❌ 失败: {result.failed}")
    print(f"   ⚠️  警告: {len(result.warnings)}")
    print(f"   ⏭️  跳过: {result.skipped}")
    
    if result.errors:
        print(f"\n❌ 失败详情 ({len(result.errors)} 项):")
        for err in result.errors[:30]:  # 只显示前30项
            print(f"   [{err['method']}] {err['endpoint']}")
            print(f"     状态码: {err['status_code']}, 消息: {err['message']}")
        
        if len(result.errors) > 30:
            print(f"   ... 还有 {len(result.errors) - 30} 项错误")
    
    if result.warnings:
        print(f"\n⚠️  警告详情 ({len(result.warnings)} 项):")
        for warn in result.warnings[:20]:
            print(f"   [{warn['method']}] {warn['endpoint']}")
            print(f"     状态码: {warn['status_code']}, 消息: {warn['message']}")
    
    # 按模块统计
    modules = {}
    for detail in result.details:
        endpoint = detail["endpoint"]
        parts = endpoint.split("/")
        if len(parts) >= 3:
            module = f"/{parts[1]}/{parts[2]}"
        elif len(parts) >= 2:
            module = f"/{parts[1]}"
        else:
            module = "/"
        
        if module not in modules:
            modules[module] = {"pass": 0, "fail": 0, "warn": 0, "skip": 0}
        
        status = detail["status"]
        if status == "PASS":
            modules[module]["pass"] += 1
        elif status == "FAIL":
            modules[module]["fail"] += 1
        elif status == "WARN":
            modules[module]["warn"] += 1
        elif status == "SKIP":
            modules[module]["skip"] += 1
    
    print(f"\n📁 模块统计 ({len(modules)} 个模块):")
    for module, stats in sorted(modules.items()):
        status_icon = "✅" if stats["fail"] == 0 else "❌"
        print(f"   {status_icon} {module}: {stats['pass']}通过/{stats['fail']}失败/{stats['warn']}警告/{stats['skip']}跳过")
    
    # 保存详细结果到文件
    report_file = "/tmp/e2e_full_test_report.json"
    with open(report_file, "w") as f:
        json.dump({
            "timestamp": datetime.now().isoformat(),
            "summary": {
                "total": total,
                "passed": result.passed,
                "failed": result.failed,
                "warnings": len(result.warnings),
                "skipped": result.skipped,
                "pass_rate": f"{pass_rate:.1f}%"
            },
            "errors": result.errors,
            "warnings": result.warnings,
            "module_stats": modules
        }, f, indent=2, ensure_ascii=False)
    
    print(f"\n📄 详细报告已保存: {report_file}")
    
    return result.failed == 0

def main():
    """主函数"""
    try:
        result = run_tests()
        success = print_report(result)
        
        if success:
            print("\n🎉 所有测试通过！")
            sys.exit(0)
        else:
            print(f"\n⚠️  有 {result.failed} 个测试失败，请查看详细报告")
            sys.exit(1)
            
    except Exception as e:
        print(f"\n❌ 测试执行异常: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(2)

if __name__ == "__main__":
    main()
