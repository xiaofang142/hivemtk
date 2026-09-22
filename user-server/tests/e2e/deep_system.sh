#!/usr/bin/env bash
# deep_system.sh - 系统/应用配置/监控追踪 深度测试 (仅只读与安全写, 不触发重启/迁移)
set +u
cd "$(dirname "$0")"
source ./deep_lib.sh

echo "================ 系统/配置/监控 深度测试 ================ "
mtk_login || { echo "LOGIN_FAIL"; exit 1; }
U="$(date +%sN | tail -c 7)"

# ---------------- system ----------------
info "GET /api/system/info"
api GET "/api/system/info"
[ "$API_HTTP" = "200" ] && pass "system/info 200" || fail "system/info http=$API_HTTP"
info "GET /api/system/init-status"
api GET "/api/system/init-status"
[ "$API_HTTP" = "200" ] && pass "system/init-status 200" || info "system/init-status http=$API_HTTP (info)"

# ---------------- 授权端点必须不存在 ----------------
# 开源版无授权流程：/api/license/* 曾恒真返回 "licensed: true"，2026-09 已连 handler 删除。
for p in /api/license/status /api/license/features; do
	info "GET $p"
	api GET "$p"
	[ "$API_HTTP" = "404" ] && pass "$(basename "$p") 404 (端点已移除)" || fail "$p 期望 404, 实际 $API_HTTP"
done

# ---------------- app-config ----------------
info "GET /api/app-config"
api GET "/api/app-config"
[ "$API_HTTP" = "200" ] && pass "app-config 200" || fail "app-config http=$API_HTTP"
info "PUT /api/app-config"
api PUT "/api/app-config" "{\"site_name\":\"深度测试站$U\"}"
[ "$API_HTTP" = "200" ] && pass "app-config 更新 200" || info "app-config 更新 http=$API_HTTP (info)"

# ---------------- monitor ----------------
info "GET /api/monitor/trace-eval"
api GET "/api/monitor/trace-eval"
[ "$API_HTTP" = "200" ] && pass "monitor/trace-eval 200" || info "monitor/trace-eval http=$API_HTTP (info)"
info "GET /api/monitor/knowledge-weights"
api GET "/api/monitor/knowledge-weights"
[ "$API_HTTP" = "200" ] && pass "monitor/knowledge-weights 200" || info "monitor/knowledge-weights http=$API_HTTP (info)"

# ---------------- health (根路径, 无 /api 前缀) ----------------
info "GET /health"
api GET "/health"
[ "$API_HTTP" = "200" ] && pass "health 200" || fail "health http=$API_HTTP"
info "GET /healthz"
api GET "/healthz"
[ "$API_HTTP" = "200" ] && pass "healthz 200" || fail "healthz http=$API_HTTP"
info "GET /readyz"
api GET "/readyz"
[ "$API_HTTP" = "200" ] && pass "readyz 200" || fail "readyz http=$API_HTTP"

echo "通过: $PASS  失败: $FAIL"
[ "$FAIL" -eq 0 ] && echo "RESULT: GREEN" || echo "RESULT: RED"
exit $FAIL
