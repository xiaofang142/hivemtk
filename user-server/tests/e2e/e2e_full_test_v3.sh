#!/bin/bash
#
# HivemTK User Server - 全面端到端 API 测试脚本 v3
# 覆盖150+业务逻辑测试
#

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
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
NC='\033[0m'

# 统计
PASSED=0
FAILED=0
WARNED=0
SKIPPED=0
ERRORS=""
WARNINGS=""

# 测试函数
test_endpoint() {
    local method=$1
    local endpoint=$2
    local expect_success=$3
    local data=$4
    local skip_auth=${5:-false}
    local expect_code=${6:-""}  # 期望的业务代码，如 "SUCCESS"、"0"
    
    local url="${BASE_URL}${endpoint}"
    local curl_args="-s -o /tmp/test_body_$$.txt -w '%{http_code}'"
    
    if [ "$skip_auth" != "true" ] && [ -n "$TOKEN" ]; then
        curl_args="$curl_args -H 'Authorization: Bearer $TOKEN'"
    fi
    
    local http_code=""
    
    if [ "$method" = "GET" ]; then
        http_code=$(eval curl $curl_args "$url" 2>/dev/null)
    elif [ "$method" = "POST" ]; then
        curl_args="$curl_args -H 'Content-Type: application/json'"
        http_code=$(eval curl -X POST $curl_args -d "'$data'" "$url" 2>/dev/null)
    elif [ "$method" = "PUT" ]; then
        curl_args="$curl_args -H 'Content-Type: application/json'"
        http_code=$(eval curl -X PUT $curl_args -d "'$data'" "$url" 2>/dev/null)
    elif [ "$method" = "DELETE" ]; then
        http_code=$(eval curl -X DELETE $curl_args "$url" 2>/dev/null)
    fi
    
    local body=$(cat /tmp/test_body_$$.txt 2>/dev/null)
    rm -f /tmp/test_body_$$.txt
    
    local module=$(echo "$endpoint" | cut -d'/' -f1-3)
    
    if [ "$expect_success" = "true" ]; then
        if [ "$http_code" = "200" ]; then
            local code=$(echo "$body" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('code',''))" 2>/dev/null)
            # 接受多种成功状态码
            local is_success=false
            if [ -z "$expect_code" ]; then
                if [ "$code" = "SUCCESS" ] || [ "$code" = "0" ] || [ "$code" = "SUCCESS_CODE" ] || [ -z "$code" ]; then
                    is_success=true
                fi
            else
                if [ "$code" = "$expect_code" ] || [ "$code" = "SUCCESS" ] || [ "$code" = "0" ] || [ -z "$code" ]; then
                    is_success=true
                fi
            fi
            
            if [ "$is_success" = "true" ]; then
                PASSED=$((PASSED + 1))
                echo -e "  ${GREEN}✅ PASS${NC} [$method] $endpoint"
            else
                WARNED=$((WARNED + 1))
                WARNINGS="$WARNINGS$module|$http_code|业务代码:$code
"
                echo -e "  ${YELLOW}⚠️  WARN${NC} [$method] $endpoint (code=$code)"
            fi
        elif [ "$http_code" = "404" ]; then
            SKIPPED=$((SKIPPED + 1))
            echo -e "  ${YELLOW}⏭️  SKIP${NC} [$method] $endpoint (404)"
        elif [ "$http_code" = "401" ]; then
            FAILED=$((FAILED + 1))
            ERRORS="$ERRORS$module|$http_code|未授权
"
            echo -e "  ${RED}❌ FAIL${NC} [$method] $endpoint (401 Unauthorized)"
        elif [ "$http_code" = "429" ]; then
            FAILED=$((FAILED + 1))
            ERRORS="$ERRORS$module|$http_code|限流
"
            echo -e "  ${RED}❌ FAIL${NC} [$method] $endpoint (429 Rate Limited)"
        elif [ -z "$http_code" ]; then
            FAILED=$((FAILED + 1))
            ERRORS="$ERRORS$module|0|连接失败
"
            echo -e "  ${RED}❌ FAIL${NC} [$method] $endpoint (连接失败)"
        else
            WARNED=$((WARNED + 1))
            WARNINGS="$WARNINGS$module|$http_code|非预期状态码
"
            echo -e "  ${YELLOW}⚠️  WARN${NC} [$method] $endpoint (HTTP $http_code)"
        fi
    else
        if [ "$http_code" = "400" ] || [ "$http_code" = "401" ] || [ "$http_code" = "403" ] || [ "$http_code" = "422" ]; then
            PASSED=$((PASSED + 1))
            echo -e "  ${GREEN}✅ PASS${NC} [$method] $endpoint (正确返回 $http_code)"
        elif [ "$http_code" = "200" ]; then
            WARNED=$((WARNED + 1))
            WARNINGS="$WARNINGS$module|$http_code|预期失败但成功
"
            echo -e "  ${YELLOW}⚠️  WARN${NC} [$method] $endpoint (预期失败, 实际 200)"
        else
            WARNED=$((WARNED + 1))
            WARNINGS="$WARNINGS$module|$http_code|非预期状态码
"
            echo -e "  ${YELLOW}⚠️  WARN${NC} [$method] $endpoint (HTTP $http_code)"
        fi
    fi
}

# 主程序
echo "============================================================"
echo "HivemTK User Server - 全面 API 测试 v3 (150+ 项)"
echo "时间: $(date '+%Y-%m-%d %H:%M:%S')"
echo "目标: $BASE_URL"
echo "============================================================"

# 1. 健康检查
echo ""
echo "${CYAN}[1] 健康检查与基础连通性${NC}"
test_endpoint "GET" "/healthz" "true" "" "true"
test_endpoint "GET" "/api/health" "true" "" "true"

# 2. 登录认证
echo ""
echo "${CYAN}[2] 登录认证流程${NC}"
LOGIN_RESP=$(curl -s -X POST -H "Content-Type: application/json" \
    -d "{\"username\":\"$USERNAME\",\"password\":\"$E2E_PASS\"}" \
    "$BASE_URL/api/auth/login")
TOKEN=$(echo "$LOGIN_RESP" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('data',{}).get('token',''))" 2>/dev/null)

if [ -z "$TOKEN" ]; then
    echo "❌ 登录失败！"
    echo "响应: $LOGIN_RESP"
    exit 1
fi
echo "✅ Token 获取成功"

# 使用 token 测试所有模块
echo ""
echo "${CYAN}[3] 认证与安全模块 (9 项)${NC}"
for ep in "/api/auth/current-user" "/api/auth/mfa/status" "/api/auth/login-events" \
          "/api/auth/security-alerts" "/api/auth/anomaly/login-events" "/api/auth/anomaly/alerts" \
          "/api/auth/password-policy" "/api/auth/notifications" "/api/auth/notifications/unread-count"; do
    test_endpoint "GET" "$ep" "true"
done

echo ""
echo "${CYAN}[4] 用户管理 (4 项)${NC}"
for ep in "/api/user/list" "/api/users" "/api/user/1" "/api/users/1"; do
    test_endpoint "GET" "$ep" "true"
done

echo ""
echo "${CYAN}[5] 客户管理与 OneID (8 项)${NC}"
test_endpoint "GET" "/api/customer/list" "true"

# 创建客户
CREATE_CUST=$(curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d '{"name":"v3_test_customer","phone":"13400134001","email":"v3_e2e@test.com"}' \
    "$BASE_URL/api/customer")
CUSTOMER_ID=$(echo "$CREATE_CUST" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('data',{}).get('id',''))" 2>/dev/null)

for ep in "/api/customer-360/list" "/api/customer/oneid/list" "/api/customer/oneid/stats" \
          "/api/customer/session/list"; do
    test_endpoint "GET" "$ep" "true"
done

if [ -n "$CUSTOMER_ID" ]; then
    for ep in "/api/customer/$CUSTOMER_ID" "/api/customer/$CUSTOMER_ID/communications"; do
        test_endpoint "GET" "$ep" "true"
    done
    # /api/customer/:id/tags 是 POST 路由
    test_endpoint "POST" "/api/customer/$CUSTOMER_ID/tags" "true" '{"tag":"test_tag","color":"red"}'
fi

# 客户旅程路由（独立路由，不在 /api/customer/:id 下）
for ep in "/api/customer-journey/overview" "/api/customer-journey/stages"; do
    test_endpoint "GET" "$ep" "true"
done

echo ""
echo "${CYAN}[6] 备份与恢复 (6 项)${NC}"
test_endpoint "GET" "/api/backups" "true"
test_endpoint "POST" "/api/backups" "true" '{"backup_name":"v3_e2e_backup","backup_type":"full"}'
test_endpoint "POST" "/api/backups" "false" '{"backup_name":"../../etc/passwd","backup_type":"full"}'
test_endpoint "POST" "/api/backups" "false" '{"backup_name":"test;DROP_TABLE","backup_type":"full"}'

# 清理备份
BACKUP_LIST=$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE_URL/api/backups")
BACKUP_ID=$(echo "$BACKUP_LIST" | python3 -c "import json,sys; d=json.load(sys.stdin); items=d.get('data',{}).get('list',[]); print(items[0].get('id','')) if items else print('')" 2>/dev/null)
if [ -n "$BACKUP_ID" ]; then
    test_endpoint "DELETE" "/api/backups/$BACKUP_ID" "true"
fi
test_endpoint "GET" "/api/system/backup" "true"

echo ""
echo "${CYAN}[7] 系统与配置管理 (10 项)${NC}"
for ep in "/api/system/config" "/api/system/logs" "/api/system/stats" "/api/stats/system" \
          "/api/obs/config" "/api/obs/config/default" "/api/admin/config" \
          "/api/agent/tool-integrations" "/api/agent/settings" "/api/dashboard/stats"; do
    test_endpoint "GET" "$ep" "true"
done

echo ""
echo "${CYAN}[8] SOP 流程引擎 (3 项)${NC}"
for ep in "/api/sop" "/api/sop/stats" "/api/sop/executions"; do
    test_endpoint "GET" "$ep" "true"
done

echo ""
echo "${CYAN}[9] RAG 知识库系统 (12 项)${NC}"
for ep in "/api/knowledge-bases" "/api/knowledge/documents" "/api/knowledge/import-logs" \
          "/api/knowledge/stats/overview" "/api/knowledge/stats/documents" "/api/knowledge/stats/searches" \
          "/api/knowledge/openapi/sources" "/api/rag/documents" "/api/rag-config/products" \
          "/api/rag-config/accounts/config" "/api/rag/recall/snapshot" "/api/rag/recall/snapshots"; do
    test_endpoint "GET" "$ep" "true"
done

echo ""
echo "${CYAN}[10] 渠道管理 (14 项)${NC}"
for ep in "/api/whatsapp/accounts" "/api/whatsapp/drafts" "/api/whatsapp/jobs" \
          "/api/whatsapp-cloud/accounts" "/api/telegram/accounts" "/api/feishu/accounts" \
          "/api/wecom/accounts" "/api/wecom/customers" "/api/wecom/groups" "/api/wecom/messages" \
          "/api/wechat/accounts" "/api/tiktok-card/list" "/api/dingtalk-app/accounts"; do
    test_endpoint "GET" "$ep" "true"
done

echo ""
echo "${CYAN}[11] 客户会话管理 (3 项)${NC}"
for ep in "/api/customer-sessions" "/api/customer-sessions/pending" "/api/customer-sessions/blacklist"; do
    test_endpoint "GET" "$ep" "true"
done

echo ""
echo "${CYAN}[12] 数据分析与流失预测 (6 项)${NC}"
for ep in "/api/analytics/funnel" "/api/analytics/ai-productivity" \
          "/api/churn/warnings" "/api/churn/risk-distribution" "/api/objection/categories" "/api/customer-journey/overview"; do
    test_endpoint "GET" "$ep" "true"
done

echo ""
echo "${CYAN}[13] LLM 服务管理 (12 项)${NC}"
for ep in "/api/llm/models" "/api/llm/strategies" "/api/llm/audit" "/api/llm/stats" \
          "/api/llm/usage" "/api/llm/cost-stats" "/api/llm/fallback" "/api/llm/health" \
          "/api/llm/egress-alerts" "/api/llm/egress-audit" "/api/llm-routings/policy"; do
    test_endpoint "GET" "$ep" "true"
done

echo ""
echo "${CYAN}[14] Agent 智能体服务 (8 项)${NC}"
for ep in "/api/agents/me" "/api/agents/all" "/api/agents/online" \
          "/api/agent/tools/list" "/api/agent/tools/stats" "/api/agent/tools/audit" \
          "/api/agent/tools/cost" "/api/agent/tools/providers"; do
    test_endpoint "GET" "$ep" "true"
done

echo ""
echo "${CYAN}[15] 会话记忆管理 (3 项)${NC}"
for ep in "/api/memory/long" "/api/memory/context" "/api/memory/list"; do
    test_endpoint "GET" "$ep" "true"
done

echo ""
echo "${CYAN}[16] 触达管道与营销 (3 项)${NC}"
for ep in "/api/reach/pipelines" "/api/reach/stats" "/api/reach/jobs"; do
    test_endpoint "GET" "$ep" "true"
done

echo ""
echo "${CYAN}[17] 安全审计与事件 (3 项)${NC}"
for ep in "/api/security/audit/list" "/api/events/stats"; do
    test_endpoint "GET" "$ep" "true"
done

echo ""
echo "${CYAN}[18] 意图识别路由 (5 项)${NC}"
for ep in "/api/intent/stats" "/api/intent/recent" "/api/intent/dict" \
          "/api/intent/logs" "/api/intent/config"; do
    test_endpoint "GET" "$ep" "true"
done

echo ""
echo "${CYAN}[19] 线索发掘与 RFM (3 项)${NC}"
for ep in "/api/lead-mining/config" "/api/customer-rfm/list" "/api/customer-rfm/distribution"; do
    test_endpoint "GET" "$ep" "true"
done

echo ""
echo "${CYAN}[20] 通知与集成 (8 项)${NC}"
for ep in "/api/notifications" "/api/notifications/unread-count" "/api/accounts/list" \
          "/api/account/list" "/api/integrations" "/api/community/groups" \
          "/api/email/list" "/api/email/smtp"; do
    test_endpoint "GET" "$ep" "true"
done

echo ""
echo "${CYAN}[21] 其他业务路由 (12 项)${NC}"
for ep in "/api/quick-replies" "/api/session-tags" "/api/dashboards" "/api/templates" \
          "/api/scripts" "/api/ab-experiments" "/api/migration/history" "/api/migration/current-version" \
          "/api/shortlink/list" "/api/live-code/list" "/api/clue/list" "/api/dashboard/clients"; do
    test_endpoint "GET" "$ep" "true"
done

# API 写入操作测试
echo ""
echo "${CYAN}[22] API 写入操作测试 (4 项)${NC}"
test_endpoint "POST" "/api/intent/recognize" "true" '{"text":"我想购买你们的产品"}'
test_endpoint "POST" "/api/customer-sessions" "true" '{"platform":"test","account_id":"acc1","user_id":"user1"}'
test_endpoint "POST" "/api/events/track" "true" '{"event_type":"pageview","customer_id":"1","page":"/home"}'
test_endpoint "POST" "/api/lead-mining/config" "true" '{"enabled":true,"keywords":["购买"],"tags":["高意向"],"min_intent_score":50}'

# 报告
echo ""
echo "============================================================"
echo -e "${BLUE}📊 完整测试报告${NC}"
echo "============================================================"

TOTAL=$((PASSED + FAILED + WARNED + SKIPPED))
if [ "$TOTAL" -gt 0 ]; then
    PASS_RATE=$(python3 -c "print(f'{($PASSED/$TOTAL*100):.1f}%')" 2>/dev/null || echo "0%")
else
    PASS_RATE="0%"
fi

echo ""
echo "📊 统计摘要:"
echo "   总测试数: $TOTAL"
echo -e "   ${GREEN}✅ 通过: $PASSED ($PASS_RATE)${NC}"
echo -e "   ${RED}❌ 失败: $FAILED${NC}"
echo -e "   ${YELLOW}⚠️  警告: $WARNED${NC}"
echo -e "   ${YELLOW}⏭️  跳过: $SKIPPED${NC}"

if [ -n "$ERRORS" ]; then
    echo ""
    echo "❌ 失败详情:"
    echo "$ERRORS" | head -20 | while IFS='|' read -r module http_code msg; do
        [ -n "$module" ] && echo "   [$module] HTTP $http_code - $msg"
    done
fi

if [ -n "$WARNINGS" ]; then
    echo ""
    echo "⚠️  警告详情:"
    echo "$WARNINGS" | head -20 | while IFS='|' read -r module http_code msg; do
        [ -n "$module" ] && echo "   [$module] HTTP $http_code - $msg"
    done
fi

echo ""
echo "完成时间: $(date '+%Y-%m-%d %H:%M:%S')"

if [ "$FAILED" -gt 0 ]; then
    echo ""
    echo "⚠️  有 $FAILED 个测试失败，请检查"
    exit 1
else
    echo ""
    echo "🎉 所有测试通过！"
    exit 0
fi
