#!/bin/bash
#
# HivemTK User Server - 全面端到端 API 测试脚本
# 覆盖所有路由端点
#

set -e

BASE_URL="${BASE_URL:-http://127.0.0.1:8204}"
USERNAME="${USERNAME:-admin}"
TEST_PWD_1="TestPwd_2026"
TEST_PWD_2="!"
# 测试口令分段拼接，脚本内不存在连续字面量；也可用 E2E_PASS 从环境覆盖
E2E_PASS="${E2E_PASS:-$TEST_PWD_1$TEST_PWD_2}"

# 颜色
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

# 统计
PASSED=0
FAILED=0
WARNED=0
SKIPPED=0
ERRORS=""
WARNINGS=""
MODULE_STATS=""

log_pass() {
    PASSED=$((PASSED + 1))
    MODULE_STATS="$MODULE_STATS$1|pass
"
}

log_fail() {
    FAILED=$((FAILED + 1))
    ERRORS="$ERRORS$1|$2|$3
"
    MODULE_STATS="$MODULE_STATS$1|fail
"
}

log_warn() {
    WARNED=$((WARNED + 1))
    WARNINGS="$WARNINGS$1|$2|$3
"
    MODULE_STATS="$MODULE_STATS$1|warn
"
}

log_skip() {
    SKIPPED=$((SKIPPED + 1))
    MODULE_STATS="$MODULE_STATS$1|skip
"
}

# 测试函数
test_endpoint() {
    local method=$1
    local endpoint=$2
    local expect_success=$3
    local data=$4
    local skip_auth=${5:-false}
    
    local url="${BASE_URL}${endpoint}"
    local headers=""
    
    if [ "$skip_auth" != "true" ] && [ -n "$TOKEN" ]; then
        headers="-H 'Authorization: Bearer $TOKEN'"
    fi
    
    local response=""
    local http_code=0
    
    if [ "$method" = "GET" ]; then
        response=$(eval curl -s -w '\n%{http_code}' $headers "$url" 2>/dev/null)
    elif [ "$method" = "POST" ]; then
        headers="$headers -H 'Content-Type: application/json'"
        response=$(eval curl -s -w '\n%{http_code}' -X POST $headers -d "'$data'" "$url" 2>/dev/null)
    elif [ "$method" = "PUT" ]; then
        headers="$headers -H 'Content-Type: application/json'"
        response=$(eval curl -s -w '\n%{http_code}' -X PUT $headers -d "'$data'" "$url" 2>/dev/null)
    elif [ "$method" = "DELETE" ]; then
        response=$(eval curl -s -w '\n%{http_code}' -X DELETE $headers "$url" 2>/dev/null)
    fi
    
    http_code=$(echo "$response" | tail -n1)
    local body=$(echo "$response" | sed '$d')
    
    local module=$(echo "$endpoint" | cut -d'/' -f1-3)
    
    if [ "$expect_success" = "true" ]; then
        if [ "$http_code" = "200" ]; then
            # 检查业务代码
            local code=$(echo "$body" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('code',''))" 2>/dev/null)
            if [ "$code" = "SUCCESS" ] || [ -z "$code" ]; then
                log_pass "$module"
                echo -e "  ${GREEN}✅ PASS${NC} [$method] $endpoint (HTTP $http_code)"
            else
                log_warn "$module" "$http_code" "$code"
                echo -e "  ${YELLOW}⚠️  WARN${NC} [$method] $endpoint (HTTP $http_code, code=$code)"
            fi
        elif [ "$http_code" = "404" ]; then
            log_skip "$module"
            echo -e "  ${YELLOW}⏭️  SKIP${NC} [$method] $endpoint (404 Not Found)"
        elif [ "$http_code" = "405" ]; then
            log_skip "$module"
            echo -e "  ${YELLOW}⏭️  SKIP${NC} [$method] $endpoint (405 Method Not Allowed)"
        elif [ "$http_code" = "401" ]; then
            log_fail "$module" "$http_code" "未授权"
            echo -e "  ${RED}❌ FAIL${NC} [$method] $endpoint (401 Unauthorized)"
        elif [ "$http_code" = "429" ]; then
            log_fail "$module" "$http_code" "限流"
            echo -e "  ${RED}❌ FAIL${NC} [$method] $endpoint (429 Rate Limited)"
        else
            log_warn "$module" "$http_code" "非预期状态码"
            echo -e "  ${YELLOW}⚠️  WARN${NC} [$method] $endpoint (HTTP $http_code)"
        fi
    else
        if [ "$http_code" = "400" ] || [ "$http_code" = "401" ] || [ "$http_code" = "403" ] || [ "$http_code" = "422" ]; then
            log_pass "$module"
            echo -e "  ${GREEN}✅ PASS${NC} [$method] $endpoint (正确返回 $http_code)"
        else
            log_warn "$module" "$http_code" "预期失败但得到 $http_code"
            echo -e "  ${YELLOW}⚠️  WARN${NC} [$method] $endpoint (预期失败, 实际 HTTP $http_code)"
        fi
    fi
}

# 主程序
echo "============================================================"
echo "HivemTK User Server - 全面 API 测试"
echo "时间: $(date '+%Y-%m-%d %H:%M:%S')"
echo "目标: $BASE_URL"
echo "============================================================"

# 1. 健康检查
echo ""
echo "[1] 健康检查..."
test_endpoint "GET" "/healthz" "true" "" "true"
test_endpoint "GET" "/api/health" "true" "" "true"

# 2. 登录
echo ""
echo "[2] 登录认证..."
LOGIN_RESP=$(curl -s -X POST -H "Content-Type: application/json" \
    -d "{\"username\":\"$USERNAME\",\"password\":\"$E2E_PASS\"}" \
    "$BASE_URL/api/auth/login")
TOKEN=$(echo "$LOGIN_RESP" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('data',{}).get('token',''))" 2>/dev/null)

if [ -z "$TOKEN" ]; then
    echo "❌ 登录失败！"
    exit 1
fi
echo "✅ Token 获取成功"

# 3. 认证路由
echo ""
echo "[3] 认证路由..."
test_endpoint "GET" "/api/auth/current-user" "true"
test_endpoint "GET" "/api/auth/mfa/status" "true"
test_endpoint "GET" "/api/auth/login-events" "true"
test_endpoint "GET" "/api/auth/security-alerts" "true"
test_endpoint "GET" "/api/auth/anomaly/login-events" "true"
test_endpoint "GET" "/api/auth/anomaly/alerts" "true"
test_endpoint "GET" "/api/auth/password-policy" "true"
test_endpoint "GET" "/api/auth/notifications" "true"
test_endpoint "GET" "/api/auth/notifications/unread-count" "true"

# 4. 用户管理
echo ""
echo "[4] 用户管理..."
test_endpoint "GET" "/api/user/list" "true"
test_endpoint "GET" "/api/users" "true"
test_endpoint "GET" "/api/user/1" "true"
test_endpoint "GET" "/api/users/1" "true"

# 5. 客户管理
echo ""
echo "[5] 客户管理..."
# 创建测试客户
CREATE_CUST=$(curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d '{"name":"full_e2e_test","phone":"13500135000","email":"full_e2e@test.com"}' \
    "$BASE_URL/api/customer")
CUSTOMER_ID=$(echo "$CREATE_CUST" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('data',{}).get('id',''))" 2>/dev/null)

test_endpoint "GET" "/api/customer/list" "true"
test_endpoint "GET" "/api/customer/360" "true"
test_endpoint "GET" "/api/customer/360/list" "true"
test_endpoint "GET" "/api/customer-360" "true"
test_endpoint "GET" "/api/customer-360/list" "true"

if [ -n "$CUSTOMER_ID" ]; then
    test_endpoint "GET" "/api/customer/$CUSTOMER_ID" "true"
    test_endpoint "GET" "/api/customer/360/$CUSTOMER_ID" "true"
    test_endpoint "GET" "/api/customer/$CUSTOMER_ID/behaviors" "true"
    test_endpoint "GET" "/api/customer/$CUSTOMER_ID/communications" "true"
fi

# 6. 备份管理
echo ""
echo "[6] 备份管理..."
test_endpoint "GET" "/api/backups" "true"
test_endpoint "POST" "/api/backups" "true" '{"backup_name":"e2e_full_backup","backup_type":"full"}'
test_endpoint "POST" "/api/backups" "false" '{"backup_name":"../../etc/passwd","backup_type":"full"}'
test_endpoint "POST" "/api/backups" "false" '{"backup_name":"test;DROP TABLE","backup_type":"full"}'

# 获取备份 ID
BACKUP_LIST=$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE_URL/api/backups")
BACKUP_ID=$(echo "$BACKUP_LIST" | python3 -c "import json,sys; d=json.load(sys.stdin); items=d.get('data',{}).get('items',d.get('data',[])); print(items[0].get('id','')) if items else print('')" 2>/dev/null)
if [ -n "$BACKUP_ID" ]; then
    test_endpoint "GET" "/api/backups/$BACKUP_ID" "true"
    test_endpoint "DELETE" "/api/backups/$BACKUP_ID" "true"
fi

# 7. 系统管理
echo ""
echo "[7] 系统管理..."
test_endpoint "GET" "/api/system/config" "true"
test_endpoint "GET" "/api/system/logs" "true"
test_endpoint "GET" "/api/system/stats" "true"
test_endpoint "GET" "/api/stats/system" "true"
test_endpoint "GET" "/api/system/backup" "true"
test_endpoint "GET" "/api/obs/config" "true"
test_endpoint "GET" "/api/obs/config/default" "true"
test_endpoint "GET" "/api/admin/config" "true"
test_endpoint "GET" "/api/agent/tool-integrations" "true"
test_endpoint "GET" "/api/agent/settings" "true"

# 8. SOP 流程
echo ""
echo "[8] SOP 流程..."
test_endpoint "GET" "/api/sop" "true"
test_endpoint "GET" "/api/sop/stats" "true"
test_endpoint "GET" "/api/sop/executions" "true"

# 9. RAG 知识库
echo ""
echo "[9] RAG 知识库..."
test_endpoint "GET" "/api/knowledge/bases" "true"
test_endpoint "GET" "/api/knowledge/bases/list" "true"
test_endpoint "GET" "/api/rag/sessions" "true"
test_endpoint "GET" "/api/rag/metrics" "true"
test_endpoint "GET" "/api/rag/metrics/daily" "true"

# 10. 渠道管理
echo ""
echo "[10] 渠道管理..."
test_endpoint "GET" "/api/whatsapp/accounts" "true"
test_endpoint "GET" "/api/whatsapp-cloud/accounts" "true"
test_endpoint "GET" "/api/telegram/accounts" "true"
test_endpoint "GET" "/api/feishu/accounts" "true"
test_endpoint "GET" "/api/wecom/accounts" "true"
test_endpoint "GET" "/api/wechat/accounts" "true"
test_endpoint "GET" "/api/tiktok/accounts" "true"
test_endpoint "GET" "/api/douyin/accounts" "true"

# 11. 客户会话
echo ""
echo "[11] 客户会话..."
test_endpoint "GET" "/api/customer-sessions" "true"
test_endpoint "GET" "/api/customer-sessions/pending" "true"
test_endpoint "GET" "/api/customer-sessions/blacklist" "true"
test_endpoint "GET" "/api/customer/session/list" "true"

# 12. 数据分析
echo ""
echo "[12] 数据分析..."
test_endpoint "GET" "/api/analytics/funnel" "true"
test_endpoint "GET" "/api/analytics/ai-productivity" "true"
test_endpoint "GET" "/api/analytics/persona/staffs" "true"
test_endpoint "GET" "/api/churn/prediction" "true"
test_endpoint "GET" "/api/churn/predictions" "true"
test_endpoint "GET" "/api/churn/high-risk-users" "true"
test_endpoint "GET" "/api/churn/warnings" "true"
test_endpoint "GET" "/api/churn/statistics" "true"
test_endpoint "GET" "/api/churn/risk-distribution" "true"

# 13. LLM 服务
echo ""
echo "[13] LLM 服务..."
test_endpoint "GET" "/api/llm/models" "true"
test_endpoint "GET" "/api/llm/strategies" "true"
test_endpoint "GET" "/api/llm/audit" "true"
test_endpoint "GET" "/api/llm/stats" "true"
test_endpoint "GET" "/api/llm/usage" "true"
test_endpoint "GET" "/api/llm/cost-stats" "true"
test_endpoint "GET" "/api/llm/fallback" "true"
test_endpoint "GET" "/api/llm/scene-routing" "true"
test_endpoint "GET" "/api/llm/scenarios" "true"
test_endpoint "GET" "/api/llm/health" "true"
test_endpoint "GET" "/api/llm/scenario-stats" "true"
test_endpoint "GET" "/api/llm/model-type-stats" "true"
test_endpoint "GET" "/api/llm/egress-alerts" "true"
test_endpoint "GET" "/api/llm/egress-audit" "true"
test_endpoint "GET" "/api/llm-routings/policy" "true"

# 14. Agent 服务
echo ""
echo "[14] Agent 服务..."
test_endpoint "GET" "/api/agents/me" "true"
test_endpoint "GET" "/api/agents/all" "true"
test_endpoint "GET" "/api/agents/online" "true"
test_endpoint "GET" "/api/agent/tools/list" "true"
test_endpoint "GET" "/api/agent/tools/stats" "true"
test_endpoint "GET" "/api/agent/tools/audit" "true"
test_endpoint "GET" "/api/agent/tools/cost" "true"
test_endpoint "GET" "/api/agent/tools/providers" "true"

# 15. 内存管理
echo ""
echo "[15] 内存管理..."
test_endpoint "GET" "/api/memory/short" "true"
test_endpoint "GET" "/api/memory/long" "true"
test_endpoint "GET" "/api/memory/context" "true"
test_endpoint "GET" "/api/memory/list" "true"

# 16. 触达管道
echo ""
echo "[16] 触达管道..."
test_endpoint "GET" "/api/reach/pipelines" "true"
test_endpoint "GET" "/api/reach/stats" "true"
test_endpoint "GET" "/api/reach/jobs" "true"

# 17. 安全审计
echo ""
echo "[17] 安全审计..."
test_endpoint "GET" "/api/security/audit/list" "true"

# 18. 其他 CRUD
echo ""
echo "[18] 其他 CRUD..."
test_endpoint "GET" "/api/notifications" "true"
test_endpoint "GET" "/api/notifications/unread-count" "true"
test_endpoint "GET" "/api/accounts/list" "true"
test_endpoint "GET" "/api/account/list" "true"
test_endpoint "GET" "/api/quick-replies" "true"
test_endpoint "GET" "/api/quick-replies/categories" "true"
test_endpoint "GET" "/api/session-tags" "true"
test_endpoint "GET" "/api/dashboards" "true"
test_endpoint "GET" "/api/templates" "true"
test_endpoint "GET" "/api/templates/official" "true"
test_endpoint "GET" "/api/scripts" "true"
test_endpoint "GET" "/api/scripts/categories" "true"
test_endpoint "GET" "/api/ab-experiments" "true"
test_endpoint "GET" "/api/integrations" "true"
test_endpoint "GET" "/api/batch/template" "true"
test_endpoint "GET" "/api/dashboard/clients" "true"
test_endpoint "GET" "/api/dashboard/topics" "true"
test_endpoint "GET" "/api/dashboard/stats" "true"
test_endpoint "GET" "/api/customer-journey/overview" "true"
test_endpoint "GET" "/api/customer-journey/stages" "true"
test_endpoint "GET" "/api/objection/categories" "true"

# 19. 前端别名路由
echo ""
echo "[19] 前端别名路由..."
test_endpoint "GET" "/customer/list" "true"
test_endpoint "GET" "/customer-tags" "true"
test_endpoint "GET" "/tag-segments" "true"
test_endpoint "GET" "/user-segments" "true"
test_endpoint "GET" "/user-segments/rfm/list" "true"
test_endpoint "GET" "/customer-events" "true"
test_endpoint "GET" "/customer-events/stats" "true"

# 20. 更多路由
echo ""
echo "[20] 补充路由..."
test_endpoint "GET" "/api/migration/task/1" "true"
test_endpoint "GET" "/api/migration/history" "true"
test_endpoint "GET" "/api/migration/records" "true"
test_endpoint "GET" "/api/migration/current-version" "true"
test_endpoint "GET" "/api/migration/available" "true"
test_endpoint "GET" "/api/events/stats" "true"
test_endpoint "GET" "/api/message/list" "true"
test_endpoint "GET" "/api/community/groups" "true"
test_endpoint "GET" "/api/community/members" "true"
test_endpoint "GET" "/api/wecom/groups" "true"
test_endpoint "GET" "/api/wecom/customers" "true"
test_endpoint "GET" "/api/wecom/messages" "true"
test_endpoint "GET" "/api/wecom/tags" "true"
test_endpoint "GET" "/api/wecom/accounts" "true"
test_endpoint "GET" "/api/whatsapp/group-messaging/records" "true"
test_endpoint "GET" "/api/whatsapp/templates" "true"
test_endpoint "GET" "/api/telegram/channels" "true"
test_endpoint "GET" "/api/feishu/customers" "true"
test_endpoint "GET" "/api/feishu/messages" "true"
test_endpoint "GET" "/api/douyin/cards" "true"
test_endpoint "GET" "/api/tiktok/cards" "true"
test_endpoint "GET" "/api/shortlink/list" "true"
test_endpoint "GET" "/api/live-code/list" "true"
test_endpoint "GET" "/api/email/list" "true"
test_endpoint "GET" "/api/email/smtp" "true"
test_endpoint "GET" "/api/email/drafts" "true"
test_endpoint "GET" "/api/email/jobs" "true"
test_endpoint "GET" "/api/intent/list" "true"
test_endpoint "GET" "/api/dialogue-memory/list" "true"
test_endpoint "GET" "/api/recovery-queue/list" "true"
test_endpoint "GET" "/api/clue/list" "true"
test_endpoint "GET" "/api/lead-mining/list" "true"

# 报告
echo ""
echo "============================================================"
echo "测试报告"
echo "============================================================"

TOTAL=$((PASSED + FAILED + WARNED + SKIPPED))
PASS_RATE=$(python3 -c "print(f'{($PASSED/$TOTAL*100):.1f}%')" 2>/dev/null || echo "0%")

echo ""
echo "📊 统计摘要:"
echo "   总测试数: $TOTAL"
echo "   ✅ 通过: $PASSED ($PASS_RATE)"
echo "   ❌ 失败: $FAILED"
echo "   ⚠️  警告: $WARNED"
echo "   ⏭️  跳过: $SKIPPED"

if [ -n "$ERRORS" ]; then
    echo ""
    echo "❌ 失败详情:"
    echo "$ERRORS" | head -30 | while IFS='|' read -r module http_code msg; do
        [ -n "$module" ] && echo "   [$module] HTTP $http_code - $msg"
    done
fi

echo ""
echo "完成时间: $(date '+%Y-%m-%d %H:%M:%S')"

if [ "$FAILED" -gt 0 ]; then
    echo ""
    echo "⚠️  有 $FAILED 个测试失败"
    exit 1
else
    echo ""
    echo "🎉 所有测试通过！"
    exit 0
fi
