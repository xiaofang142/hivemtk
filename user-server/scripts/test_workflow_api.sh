#!/bin/bash
#
# 工作流编排模块 API 测试脚本
# 使用 curl 验证主要 API 端点的可用性
#
# 前提条件:
#   1. user-server 服务已启动（默认端口 8080）
#   2. 已获取有效的 JWT Token
#
# 用法:
#   chmod +x test_workflow_api.sh
#   ./test_workflow_api.sh [base_url] [jwt_token]
#

set -e

BASE_URL="${1:-http://localhost:8080}"
JWT_TOKEN="${2:-}"  # 仅从命令行参数/环境读取

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_success() { echo -e "${GREEN}[PASS]${NC} $1"; }
log_fail()    { echo -e "${RED}[FAIL]${NC} $1"; }
log_info()    { echo -e "${YELLOW}[INFO]${NC} $1"; }

if [ -z "$JWT_TOKEN" ]; then
    log_info "未提供 JWT Token，部分需要认证的接口将返回 401"
    log_info "使用: $0 <base_url> <jwt_token>"
fi

AUTH_HEADER=""
if [ -n "$JWT_TOKEN" ]; then
    AUTH_HEADER="-H 'Authorization: Bearer $JWT_TOKEN'"
fi

echo ""
echo "=========================================="
echo " 工作流编排模块 API 测试"
echo " Base URL: $BASE_URL"
echo "=========================================="
echo ""

PASSED=0
FAILED=0

# -- 测试 1: 创建工作流版本 --
log_info "测试 1: 创建工作流版本"
CREATE_RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE_URL/api/workflows/versions" \
    -H "Content-Type: application/json" \
    $AUTH_HEADER \
    -d '{
        "workflow_id": "wf-test-001",
        "name": "Test Workflow v1",
        "description": "Automated test",
        "definition": {
            "nodes": [
                {"id": "n1", "type": "trigger", "name": "Webhook"},
                {"id": "n2", "type": "action", "name": "SendEmail"}
            ],
            "edges": [
                {"from": "n1", "to": "n2"}
            ]
        },
        "created_by": "test-script"
    }')
HTTP_CODE=$(echo "$CREATE_RESP" | tail -n1)
BODY=$(echo "$CREATE_RESP" | sed '$d')
if [ "$HTTP_CODE" = "200" ]; then
    log_success "创建版本成功 (HTTP $HTTP_CODE)"
    WF_VERSION_ID=$(echo "$BODY" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['data']['id'])" 2>/dev/null || echo "")
    log_info "   创建的版本 ID: $WF_VERSION_ID"
    PASSED=$((PASSED + 1))
else
    log_fail "创建版本失败 (HTTP $HTTP_CODE)"
    log_info "   响应: $BODY"
    FAILED=$((FAILED + 1))
fi

# -- 测试 2: 查询版本详情 --
log_info "测试 2: 查询版本详情"
if [ -n "$WF_VERSION_ID" ]; then
    GET_RESP=$(curl -s -w "\n%{http_code}" -X GET "$BASE_URL/api/workflows/versions/$WF_VERSION_ID" \
        $AUTH_HEADER)
    HTTP_CODE=$(echo "$GET_RESP" | tail -n1)
    if [ "$HTTP_CODE" = "200" ]; then
        log_success "查询版本详情成功 (HTTP $HTTP_CODE)"
        PASSED=$((PASSED + 1))
    else
        log_fail "查询版本详情失败 (HTTP $HTTP_CODE)"
        FAILED=$((FAILED + 1))
    fi
else
    log_info "   跳过（版本 ID 不可用）"
fi

# -- 测试 3: 列出版本 --
log_info "测试 3: 列出工作流版本"
LIST_RESP=$(curl -s -w "\n%{http_code}" -X GET "$BASE_URL/api/workflows/versions?workflow_id=wf-test-001" \
    $AUTH_HEADER)
HTTP_CODE=$(echo "$LIST_RESP" | tail -n1)
if [ "$HTTP_CODE" = "200" ]; then
    log_success "列出版本成功 (HTTP $HTTP_CODE)"
    PASSED=$((PASSED + 1))
else
    log_fail "列出版本失败 (HTTP $HTTP_CODE)"
    FAILED=$((FAILED + 1))
fi

# -- 测试 4: 更新版本 --
log_info "测试 4: 更新工作流版本"
if [ -n "$WF_VERSION_ID" ]; then
    UPDATE_RESP=$(curl -s -w "\n%{http_code}" -X PUT "$BASE_URL/api/workflows/versions/$WF_VERSION_ID" \
        -H "Content-Type: application/json" \
        $AUTH_HEADER \
        -d '{
            "name": "Updated Workflow v1",
            "definition": {
                "nodes": [
                    {"id": "n1", "type": "trigger", "name": "Webhook"},
                    {"id": "n2", "type": "action", "name": "SendEmail"},
                    {"id": "n3", "type": "condition", "name": "CheckReply"}
                ]
            },
            "changelog": "Added condition node"
        }')
    HTTP_CODE=$(echo "$UPDATE_RESP" | tail -n1)
    if [ "$HTTP_CODE" = "200" ]; then
        log_success "更新版本成功 (HTTP $HTTP_CODE)"
        PASSED=$((PASSED + 1))
    else
        log_fail "更新版本失败 (HTTP $HTTP_CODE)"
        FAILED=$((FAILED + 1))
    fi
else
    log_info "   跳过（版本 ID 不可用）"
fi

# -- 测试 5: 发布版本 --
log_info "测试 5: 发布工作流版本"
if [ -n "$WF_VERSION_ID" ]; then
    PUBLISH_RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE_URL/api/workflows/versions/$WF_VERSION_ID/publish" \
        $AUTH_HEADER)
    HTTP_CODE=$(echo "$PUBLISH_RESP" | tail -n1)
    if [ "$HTTP_CODE" = "200" ]; then
        log_success "发布版本成功 (HTTP $HTTP_CODE)"
        PASSED=$((PASSED + 1))
    else
        log_fail "发布版本失败 (HTTP $HTTP_CODE)"
        FAILED=$((FAILED + 1))
    fi
else
    log_info "   跳过（版本 ID 不可用）"
fi

# -- 测试 6: 执行工作流 --
log_info "测试 6: 执行工作流"
EXEC_RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE_URL/api/workflows/execute" \
    -H "Content-Type: application/json" \
    $AUTH_HEADER \
    -d '{
        "workflow_id": "wf-test-001",
        "trigger_payload": {
            "user_id": "u001",
            "message": "Hello, test"
        }
    }')
HTTP_CODE=$(echo "$EXEC_RESP" | tail -n1)
BODY=$(echo "$EXEC_RESP" | sed '$d')
if [ "$HTTP_CODE" = "200" ]; then
    log_success "执行工作流成功 (HTTP $HTTP_CODE)"
    WF_EXEC_ID=$(echo "$BODY" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['data']['id'])" 2>/dev/null || echo "")
    log_info "   执行 ID: $WF_EXEC_ID"
    PASSED=$((PASSED + 1))
else
    log_fail "执行工作流失败 (HTTP $HTTP_CODE)"
    log_info "   响应: $BODY"
    FAILED=$((FAILED + 1))
fi

# -- 测试 7: 查询执行详情 --
log_info "测试 7: 查询执行详情"
if [ -n "$WF_EXEC_ID" ]; then
    GET_EXEC_RESP=$(curl -s -w "\n%{http_code}" -X GET "$BASE_URL/api/workflows/executions/$WF_EXEC_ID" \
        $AUTH_HEADER)
    HTTP_CODE=$(echo "$GET_EXEC_RESP" | tail -n1)
    if [ "$HTTP_CODE" = "200" ]; then
        log_success "查询执行详情成功 (HTTP $HTTP_CODE)"
        PASSED=$((PASSED + 1))
    else
        log_fail "查询执行详情失败 (HTTP $HTTP_CODE)"
        FAILED=$((FAILED + 1))
    fi
else
    log_info "   跳过（执行 ID 不可用）"
fi

# -- 测试 8: 列出执行实例 --
log_info "测试 8: 列出执行实例"
LIST_EXEC_RESP=$(curl -s -w "\n%{http_code}" -X GET "$BASE_URL/api/workflows/executions?workflow_id=wf-test-001&page=1&page_size=20" \
    $AUTH_HEADER)
HTTP_CODE=$(echo "$LIST_EXEC_RESP" | tail -n1)
if [ "$HTTP_CODE" = "200" ]; then
    log_success "列出执行实例成功 (HTTP $HTTP_CODE)"
    PASSED=$((PASSED + 1))
else
    log_fail "列出执行实例失败 (HTTP $HTTP_CODE)"
    FAILED=$((FAILED + 1))
fi

# -- 测试 9: 获取节点执行明细 --
log_info "测试 9: 获取节点执行明细"
if [ -n "$WF_EXEC_ID" ]; then
    NODES_RESP=$(curl -s -w "\n%{http_code}" -X GET "$BASE_URL/api/workflows/executions/$WF_EXEC_ID/nodes" \
        $AUTH_HEADER)
    HTTP_CODE=$(echo "$NODES_RESP" | tail -n1)
    if [ "$HTTP_CODE" = "200" ]; then
        log_success "获取节点执行明细成功 (HTTP $HTTP_CODE)"
        PASSED=$((PASSED + 1))
    else
        log_fail "获取节点执行明细失败 (HTTP $HTTP_CODE)"
        FAILED=$((FAILED + 1))
    fi
else
    log_info "   跳过（执行 ID 不可用）"
fi

# -- 测试 10: 归档版本 --
log_info "测试 10: 归档工作流版本"
if [ -n "$WF_VERSION_ID" ]; then
    ARCHIVE_RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE_URL/api/workflows/versions/$WF_VERSION_ID/archive" \
        $AUTH_HEADER)
    HTTP_CODE=$(echo "$ARCHIVE_RESP" | tail -n1)
    if [ "$HTTP_CODE" = "200" ]; then
        log_success "归档版本成功 (HTTP $HTTP_CODE)"
        PASSED=$((PASSED + 1))
    else
        log_fail "归档版本失败 (HTTP $HTTP_CODE)"
        FAILED=$((FAILED + 1))
    fi
else
    log_info "   跳过（版本 ID 不可用）"
fi

# -- 测试 11: 删除版本 --
log_info "测试 11: 删除工作流版本"
if [ -n "$WF_VERSION_ID" ]; then
    DELETE_RESP=$(curl -s -w "\n%{http_code}" -X DELETE "$BASE_URL/api/workflows/versions/$WF_VERSION_ID" \
        $AUTH_HEADER)
    HTTP_CODE=$(echo "$DELETE_RESP" | tail -n1)
    if [ "$HTTP_CODE" = "200" ]; then
        log_success "删除版本成功 (HTTP $HTTP_CODE)"
        PASSED=$((PASSED + 1))
    else
        log_fail "删除版本失败 (HTTP $HTTP_CODE)"
        FAILED=$((FAILED + 1))
    fi
else
    log_info "   跳过（版本 ID 不可用）"
fi

# -- 测试 12: 无效 ID 查询 --
log_info "测试 12: 查询不存在的版本 (应返回 404)"
INVALID_RESP=$(curl -s -w "\n%{http_code}" -X GET "$BASE_URL/api/workflows/versions/99999" \
    $AUTH_HEADER)
HTTP_CODE=$(echo "$INVALID_RESP" | tail -n1)
if [ "$HTTP_CODE" = "404" ]; then
    log_success "查询不存在版本返回 404 (HTTP $HTTP_CODE)"
    PASSED=$((PASSED + 1))
else
    log_fail "查询不存在版本期望 404，得到 $HTTP_CODE"
    FAILED=$((FAILED + 1))
fi

echo ""
echo "=========================================="
echo " 测试结果汇总"
echo "=========================================="
echo -e " ${GREEN}通过: $PASSED${NC}"
echo -e " ${RED}失败: $FAILED${NC}"
TOTAL=$((PASSED + FAILED))
echo -e " 总计: $TOTAL"
echo ""

if [ "$FAILED" -gt 0 ]; then
    echo -e "${RED}部分测试失败，请检查日志${NC}"
    exit 1
else
    echo -e "${GREEN}所有测试通过！${NC}"
    exit 0
fi