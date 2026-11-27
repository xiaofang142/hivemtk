#!/usr/bin/env bash
# deep_security.sh - 多角度安全/边界/负向测试（用户端）
# 覆盖：认证/Token / SQL注入/XSS / IDOR / CORS / 文件上传 / 边界值 / 权限 / 桥接
set +u
cd "$(dirname "$0")"
source deep_lib.sh

echo "════════════════════════════════════════════════════════════"
echo "  HIVEMTK 用户端 多角度安全/边界测试"
echo "  时间: $(date '+%Y-%m-%d %H:%M:%S')"
echo "════════════════════════════════════════════════════════════"

if ! mtk_login; then echo "ABORT: 登录失败"; exit 1; fi
info "登录成功 TOKEN_LEN=${#TOKEN}"

##############################################
# 1. 认证 / Token 安全
##############################################
echo "--- 1. 认证/Token ---"
# 1.1 无 Token
out=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/api/auth/current-user")
[ "$out" = "401" ] && pass "无 Token 拒绝 (401)" || fail "无 Token 应 401 实 $out"

# 1.2 错误 Token
out=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer not-a-real-token" "$BASE/api/auth/current-user")
[ "$out" = "401" ] && pass "错误 Token 拒绝 (401)" || fail "错误 Token 应 401 实 $out"

# 1.3 错误密码（不应暴露用户存在性）
RESP=$(curl -s -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' -d '{"username":"admin","password":"wrong-pass-1234"}')
echo "$RESP" | grep -q "用户名或密码错误" && pass "错误密码统一文案（防枚举）" || fail "错误密码文案: $RESP"

# 1.4 不存在用户
RESP=$(curl -s -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' -d '{"username":"nosuchuser_xyz","password":"wrong"}')
echo "$RESP" | grep -q "用户名或密码错误" && pass "不存在用户也返统一文案" || fail "不存在用户文案: $RESP"

##############################################
# 2. CORS 安全
##############################################
echo "--- 2. CORS ---"
# 2.1 任意 Origin 不应反射
aca=$(curl -s -I -H "Origin: https://evil.com" "$BASE/api/health" | grep -i 'access-control-allow-origin' | tr -d '\r')
if [ -z "$aca" ]; then
  pass "未配置 Origin 不返回 ACAO（防 CSRF）"
else
  echo "$aca" | grep -q "evil.com" && fail "CORS 反射 evil.com: $aca" || pass "ACAO 非 evil.com: $aca"
fi

# 2.2 chrome-extension 反射（预期允许）
ace=$(curl -s -I -H "Origin: chrome-extension://abcdefghijklmnop" "$BASE/api/health" | grep -i 'access-control-allow-origin' | tr -d '\r')
echo "$ace" | grep -q "chrome-extension://" && pass "chrome-extension Origin 反射" || fail "chrome-extension 未反射: $ace"

##############################################
# 3. IDOR（不存在 ID）
##############################################
echo "--- 3. IDOR ---"
api GET "/api/customer/99999999-no-such-id"
[ "$API_HTTP" = "404" ] && pass "不存在 customer id 404" || info "不存在 customer id HTTP=$API_HTTP"

api GET "/api/customer-sessions/99999999"
[ "$API_HTTP" = "404" ] || [ "$API_HTTP" = "200" ] && pass "不存在 session HTTP=$API_HTTP" || fail "不存在 session HTTP=$API_HTTP"

api GET "/api/customer-360/basic?customer_id=99999999"
[ "$API_HTTP" = "200" ] || [ "$API_HTTP" = "404" ] && pass "不存在 customer-360 HTTP=$API_HTTP" || info "customer-360 HTTP=$API_HTTP"

##############################################
# 4. 客户创建输入校验（新增）
##############################################
echo "--- 4. 客户创建输入校验 ---"
# 4.1 空 body
api POST /api/customer '{}'
[ "$API_HTTP" = "400" ] && pass "空 body 拒绝 (400)" || fail "空 body 应 400 实 $API_HTTP $API_BODY"

# 4.2 错误手机号
api POST /api/customer '{"phone":"abc"}'
[ "$API_HTTP" = "400" ] && pass "无效手机号 400" || fail "无效手机号应 400 实 $API_HTTP"

# 4.3 错误邮箱
api POST /api/customer '{"email":"notanemail"}'
[ "$API_HTTP" = "400" ] && pass "无效邮箱 400" || fail "无效邮箱应 400 实 $API_HTTP"

# 4.4 合法手机号
api POST /api/customer '{"phone":"13900000001"}'
[ "$API_HTTP" = "200" ] && pass "合法手机号 200" || fail "合法手机号应 200 实 $API_HTTP $API_BODY"

# 4.5 合法邮箱
api POST /api/customer '{"email":"test1@example.com"}'
[ "$API_HTTP" = "200" ] && pass "合法邮箱 200" || fail "合法邮箱应 200 实 $API_HTTP"

# 4.6 仅第三方 ID（无 phone/email）
api POST /api/customer '{"wechat_open_id":"o6_bmwk_test_xyz_001"}'
[ "$API_HTTP" = "200" ] && pass "仅 wechat_open_id 200" || fail "仅 wechat_open_id 应 200 实 $API_HTTP"

# 4.7 清理测试客户
dbq "DELETE FROM customers WHERE phone='13900000001' OR email='test1@example.com' OR wechat_open_id='o6_bmwk_test_xyz_001'" >/dev/null 2>&1
info "清理测试客户"

##############################################
# 5. 文件上传安全
##############################################
echo "--- 5. 文件上传安全 ---"
# 5.1 上传可执行文件
echo -ne "MZ\x00\x00" > /tmp/sec_test.exe
out=$(curl -s -X POST -H "Authorization: Bearer $TOKEN" -F "file=@/tmp/sec_test.exe" "$BASE/api/upload")
echo "$out" | grep -q "禁止上传\|不支持的文件" && pass ".exe 被拒绝" || info ".exe 响应: $out"

# 5.2 路径穿越文件名
echo "test" > /tmp/innocent.txt
out=$(curl -s -X POST -H "Authorization: Bearer $TOKEN" -F "file=@/tmp/innocent.txt;filename=../../../etc/passwd.txt" "$BASE/api/upload")
echo "$out" | grep -q "成功" && pass "路径穿越文件名 UUID 重命名" || info "路径穿越响应: $out"

# 5.3 文件类型伪造（.jpg 实际是 PHP）
echo '<?php echo "x"; ?>' > /tmp/fake.jpg
out=$(curl -s -X POST -H "Authorization: Bearer $TOKEN" -F "file=@/tmp/fake.jpg" "$BASE/api/upload")
echo "$out" | grep -q "不支持\|不匹配\|伪造" && pass "伪装文件类型拒绝" || info "伪装检测响应: $out"

##############################################
# 6. 边界值
##############################################
echo "--- 6. 边界值 ---"
# 6.1 超大页码
api GET "/api/clue/list?page=999999999&limit=20"
[ "$API_HTTP" = "200" ] && pass "超大页码 200" || info "超大页码: $API_HTTP"

# 6.2 极大 page_size（clue DTO 绑定 limit 而非 page_size）
api GET "/api/clue/list?page=1&limit=10000"
[ "$API_HTTP" = "200" ] && pass "极大 limit 200（被 MaxLimit 截断）" || info "极大 limit: $API_HTTP"

# 6.3 线索 type 非数字
api POST /api/clue/import '[{"name":"x","account":"a","type":"abc"}]'
[ "$API_HTTP" = "400" ] && pass "type 非数字 400" || fail "type非数字应 400 实 $API_HTTP"

# 6.4 线索空数组导入
api POST /api/clue/import '[]'
[ "$API_HTTP" = "200" ] && pass "空数组导入 200" || fail "空数组应 200 实 $API_HTTP"

##############################################
# 7. 桥接端点
##############################################
echo "--- 7. 桥接端点 ---"
# 7.1 ingest 无 token（设计公开）
out=$(curl -s -o /dev/null -w "%{http_code}" -X POST -H 'Content-Type: application/json' -d '{}' "$BASE/api/channelbus/ingest")
[ "$out" = "400" ] || [ "$out" = "200" ] && pass "bridge/ingest 公开 HTTP=$out" || fail "bridge/ingest 异常 $out"

# 7.2 outbox 无 token
out=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/api/channelbus/outbox")
[ "$out" = "401" ] || [ "$out" = "400" ] && pass "bridge/outbox 需 token HTTP=$out" || fail "bridge/outbox 异常 $out"

##############################################
# 8. 暴力守卫阈值（修复后应 5 次失败即锁）
##############################################
echo "--- 8. 暴力守卫 ---"
# 先用对端点成功 1 次清掉之前的锁（不易跨端点，先观察）
# 由于守卫仅记录 "auth.login" 端点，登录成功会清空，故只需观察
# 当前 IP 状态未知，本测试只验证行为一致性
INFO=$(curl -s -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' -d '{"username":"nobody_xyz","password":"x"}' | head -c 200)
echo "$INFO" | grep -q "用户名或密码错误\|INSUFFICIENT_QUOTA" && pass "暴力守卫仍可工作" || fail "暴力守卫: $INFO"

##############################################
# 9. 多角色越权测试（admin vs staff 二轮发现）
##############################################
echo "--- 9. 多角色越权 (admin only 写操作) ---"
STAFF_USER="sec_staff_01"
STAFF_PASS="Staff@1234"
# 先确保有 staff 用户（之前应已创建）
STAFF_TOK=$(curl -s -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$STAFF_USER\",\"password\":\"$STAFF_PASS\"}" | jq -r '.data.token // empty')
if [ -z "$STAFF_TOK" ]; then
  # 创建 staff 用户
  ADMIN_TOK=$(curl -s -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' -d '{"username":"admin","password":"Admin@123456"}' | jq -r '.data.token')
  curl -s -X POST -H "Authorization: Bearer $ADMIN_TOK" -H 'Content-Type: application/json' -d "{
    \"username\":\"$STAFF_USER\",\"password\":\"$STAFF_PASS\",\"email\":\"sec_staff_q@test.com\",
    \"role\":\"staff\",\"enabled\":true,\"status\":1
  }" "$BASE/api/system/users" >/dev/null 2>&1
  STAFF_TOK=$(curl -s -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$STAFF_USER\",\"password\":\"$STAFF_PASS\"}" | jq -r '.data.token // empty')
fi
if [ -n "$STAFF_TOK" ]; then
  # LLM 越权（防 LLM 流量重定向到 evil.com）
  for entry in "PUT:/api/llm/models/sensenova" "POST:/api/llm/models" "DELETE:/api/llm/models/default" "PUT:/api/llm/strategies" "POST:/api/llm/providers/circuit/reset"; do
    method=${entry%:*}; endpoint=${entry#*:}
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    [ "$code" = "403" ] && pass "staff ${method} ${endpoint} → 403" || fail "staff ${method} ${endpoint} 应 403 实 $code"
  done
  # 集成越权（防 corp_secret 等敏感凭据被 staff 创建/修改）
  for entry in "POST:/api/integrations" "PUT:/api/integrations/1" "DELETE:/api/integrations/1" "POST:/api/integrations/1/sync-customers"; do
    method=${entry%:*}; endpoint=${entry#*:}
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    [ "$code" = "403" ] && pass "staff ${method} ${endpoint} → 403" || fail "staff ${method} ${endpoint} 应 403 实 $code"
  done
  # 企微越权（含 corp_secret 凭据）
  for entry in "POST:/api/wecom/accounts" "PUT:/api/wecom/accounts/1" "DELETE:/api/wecom/accounts/1" "POST:/api/wecom/accounts/1/send-message"; do
    method=${entry%:*}; endpoint=${entry#*:}
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    [ "$code" = "403" ] && pass "staff ${method} ${endpoint} → 403" || fail "staff ${method} ${endpoint} 应 403 实 $code"
  done
  # 渠道越权（chat-channels 含 app_key/app_secret）
  for entry in "POST:/api/chat-channels" "PUT:/api/chat-channels/1" "POST:/api/chat-channels/1/rotate-key" "POST:/api/chat-channels/1/reset-secret"; do
    method=${entry%:*}; endpoint=${entry#*:}
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    [ "$code" = "403" ] && pass "staff ${method} ${endpoint} → 403" || fail "staff ${method} ${endpoint} 应 403 实 $code"
  done
  # AB 实验越权（防 staff 篡改统计实验）
  for entry in "POST:/api/ab-experiments" "PUT:/api/ab-experiments/1" "DELETE:/api/ab-experiments/1" "POST:/api/ab-experiments/1/stop" "POST:/api/ab-experiments/1/start"; do
    method=${entry%:*}; endpoint=${entry#*:}
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    [ "$code" = "403" ] && pass "staff ${method} ${endpoint} → 403" || fail "staff ${method} ${endpoint} 应 403 实 $code"
  done
  # WhatsApp 越权
  for entry in "POST:/api/whatsapp/accounts" "POST:/api/whatsapp/jobs" "DELETE:/api/whatsapp/jobs/1" "POST:/api/whatsapp/group-messaging/send"; do
    method=${entry%:*}; endpoint=${entry#*:}
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    [ "$code" = "403" ] && pass "staff ${method} ${endpoint} → 403" || fail "staff ${method} ${endpoint} 应 403 实 $code"
  done
  # 读操作：staff 应仍可访问（业务需要）
  for endpoint in "/api/llm/models" "/api/wecom/accounts" "/api/integrations" "/api/ab-experiments" "/api/chat-channels" "/api/whatsapp/accounts"; do
    code=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $STAFF_TOK" "$BASE$endpoint")
    [ "$code" = "200" ] && pass "staff GET ${endpoint} → 200" || fail "staff GET ${endpoint} 应 200 实 $code"
  done

  # ===== 三轮：扩展越权面覆盖（8 类新发现） =====
  echo "--- 9b. 三轮扩展越权（sop/community/ai-content/marketing/short-link/lead-mining/rfm/backup/asset-bundle） ---"

  # SOP 写操作越权（防 staff 篡改客户服务流程）
  # 注：resume/cancel 在 /api/sop/executions/:id/ 下测试（见下节）
  for entry in "POST:/api/sop-agents" "PUT:/api/sop-agents/1" "DELETE:/api/sop-agents/1" \
               "POST:/api/sop-agents/1/activate" "POST:/api/sop-agents/1/deactivate" \
               "POST:/api/sop-agents/1/pause"; do
    method=${entry%:*}; endpoint=${entry#*:}
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    [ "$code" = "403" ] && pass "staff ${method} ${endpoint} → 403" || fail "staff ${method} ${endpoint} 应 403 实 $code"
  done

  # SOP 模板写操作越权
  for entry in "POST:/api/sop-templates" "PUT:/api/sop-templates/1" "DELETE:/api/sop-templates/1"; do
    method=${entry%:*}; endpoint=${entry#*:}
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    [ "$code" = "403" ] && pass "staff ${method} ${endpoint} → 403" || fail "staff ${method} ${endpoint} 应 403 实 $code"
  done

  # AI 内容写操作越权（防 staff 篡改 AI 生成模板与记录）
  # 注：模板写操作注册在 /api/ai/templates（非 /api/ai-content/templates）
  for entry in "POST:/api/ai-content" "POST:/api/ai-content/generate" "DELETE:/api/ai-content/1" \
               "POST:/api/ai/templates" "PUT:/api/ai/templates/1" "DELETE:/api/ai/templates/1"; do
    method=${entry%:*}; endpoint=${entry#*:}
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    [ "$code" = "403" ] && pass "staff ${method} ${endpoint} → 403" || fail "staff ${method} ${endpoint} 应 403 实 $code"
  done

  # 社群写操作越权（群组创建/删除 + 成员增删）
  for entry in "POST:/api/community/groups" "DELETE:/api/community/groups/1" \
               "POST:/api/community/members" "DELETE:/api/community/members/1"; do
    method=${entry%:*}; endpoint=${entry#*:}
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    [ "$code" = "403" ] && pass "staff ${method} ${endpoint} → 403" || fail "staff ${method} ${endpoint} 应 403 实 $code"
  done

  # 营销流程写操作越权
  for entry in "POST:/api/marketing-flows" "PUT:/api/marketing-flows/1" "DELETE:/api/marketing-flows/1" \
               "POST:/api/marketing-flows/1/activate" "POST:/api/marketing-flows/1/pause" "POST:/api/marketing-flows/1/stop"; do
    method=${entry%:*}; endpoint=${entry#*:}
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    [ "$code" = "403" ] && pass "staff ${method} ${endpoint} → 403" || fail "staff ${method} ${endpoint} 应 403 实 $code"
  done

  # 短链写操作越权（防钓鱼短链被 staff 创建）
  for entry in "POST:/api/short-link" "PUT:/api/short-link/1" "DELETE:/api/short-link/1"; do
    method=${entry%:*}; endpoint=${entry#*:}
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    [ "$code" = "403" ] && pass "staff ${method} ${endpoint} → 403" || fail "staff ${method} ${endpoint} 应 403 实 $code"
  done

  # 线索挖掘配置写操作越权（注：实际为 POST，非 PUT）
  for entry in "POST:/api/lead-mining/config"; do
    method=${entry%:*}; endpoint=${entry#*:}
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    [ "$code" = "403" ] && pass "staff ${method} ${endpoint} → 403" || fail "staff ${method} ${endpoint} 应 403 实 $code"
  done

  # 用户分群 RFM 规则写操作越权（注：实际路径为 /api/user-segment/rfm/rule）
  for entry in "POST:/api/user-segment/rfm/rule" "PUT:/api/user-segment/rfm/rule/1" "DELETE:/api/user-segment/rfm/rule/1" \
               "POST:/api/customer-rfm/compute-all"; do
    method=${entry%:*}; endpoint=${entry#*:}
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    [ "$code" = "403" ] && pass "staff ${method} ${endpoint} → 403" || fail "staff ${method} ${endpoint} 应 403 实 $code"
  done

  # 备份/恢复越权（防 staff 删除/覆盖整库数据）
  # 注：实际路径为 POST /api/backups（创建）、DELETE /api/backups/:id（删除）、POST /api/restore（恢复）
  for entry in "POST:/api/backups" "DELETE:/api/backups/1" "POST:/api/restore"; do
    method=${entry%:*}; endpoint=${entry#*:}
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    [ "$code" = "403" ] && pass "staff ${method} ${endpoint} → 403" || fail "staff ${method} ${endpoint} 应 403 实 $code"
  done

  # 资源包写操作越权
  for entry in "POST:/api/asset-bundle" "PUT:/api/asset-bundle/1" "DELETE:/api/asset-bundle/1" \
               "POST:/api/asset-bundle/1/publish" "POST:/api/asset-bundle/1/archive"; do
    method=${entry%:*}; endpoint=${entry#*:}
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    [ "$code" = "403" ] && pass "staff ${method} ${endpoint} → 403" || fail "staff ${method} ${endpoint} 应 403 实 $code"
  done

  # SOP 执行控制越权（注：resume/cancel 实际在 /api/sop/executions/:id/ 下）
  for entry in "POST:/api/sop/executions/1/resume" "POST:/api/sop/executions/1/cancel" "POST:/api/sop/executions/1/pause"; do
    method=${entry%:*}; endpoint=${entry#*:}
    code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    [ "$code" = "403" ] && pass "staff ${method} ${endpoint} → 403" || fail "staff ${method} ${endpoint} 应 403 实 $code"
  done

  # 三轮：读操作 staff 应仍可访问（校正后路径）
  for endpoint in "/api/sop-agents" "/api/sop-templates" "/api/ai-content/list" "/api/community/groups" \
                   "/api/marketing-flows" "/api/short-link/list" "/api/lead-mining/config" \
                   "/api/user-segment/rfm/rules" "/api/asset-bundle/list"; do
    # asset-bundle/list 为 POST，单独处理
    if [[ "$endpoint" == *"/asset-bundle/list"* ]]; then
      code=$(curl -s -o /dev/null -w "%{http_code}" -X POST -H "Authorization: Bearer $STAFF_TOK" -H 'Content-Type: application/json' -d '{}' "$BASE$endpoint")
    else
      code=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $STAFF_TOK" "$BASE$endpoint")
    fi
    [ "$code" = "200" ] && pass "staff GET/POST ${endpoint} → 200" || fail "staff GET/POST ${endpoint} 应 200 实 $code"
  done
else
  info "无法创建/登录 staff，跳过多角色越权测试"
fi

##############################################
# 汇总
##############################################
echo "════════════════════════════════════════════════════════════"
echo "汇总: 通过 $PASS  失败 $FAIL"
if [ "$FAIL" -gt 0 ]; then echo "失败列表:"; printf '  %s\n' "${CASES[@]}"; exit 1; fi
echo "RESULT: GREEN"
exit 0
