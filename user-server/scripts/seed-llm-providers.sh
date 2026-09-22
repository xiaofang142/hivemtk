#!/usr/bin/env bash
# =============================================================
# GEO 国产主流 LLM 渠道一键接入脚本（v3）
#
# 用途：向任意环境的 HiveMtk 用户端写入 4 个国产厂商 provider，
#       使「LLM 路由」页与 GEO 内容生成/验证立即可用。
#
# 用法：
#   # 本地（默认 http://127.0.0.1:8204）：
#   ./scripts/seed-llm-providers.sh
#
#   # 远程主机（换成你自己的部署地址，需可达且已开放对应端口）：
#   BASE=https://<你的 user-server 地址> \
#   USER=admin PASS='你的密码' \
#   DS_KEY=sk-xxx QW_KEY=sk-xxx DB_KEY=xxx ER_KEY=bce-v3/xxx \
#   ./scripts/seed-llm-providers.sh
#
# 密钥来源建议：从 .env.geo.local 读取（gitignored）
# =============================================================
set -euo pipefail

BASE="${BASE:-http://127.0.0.1:8204}"
USER="${USER:-admin}"
PASS="${PASS:-Admin@2026geo}"

DS_KEY="${DS_KEY:-${DEEPSEEK_API_KEY:-}}"
QW_KEY="${QW_KEY:-${QWEN_API_KEY:-}}"
DB_KEY="${DB_KEY:-${DOUBAO_API_KEY:-}}"
ER_KEY="${ER_KEY:-${ERNIE_API_KEY:-}}"

for v in "DS_KEY|$DS_KEY" "QW_KEY|$QW_KEY" "DB_KEY|$DB_KEY" "ER_KEY|$ER_KEY"; do
  name="${v%%|*}"; val="${v#*|}"
  [ -z "$val" ] && { echo "❌ 缺少 ${name}（环境变量未设置）"; exit 1; }
done

# ---- 1) 登录换 token ----
LOGIN=$(curl -sf -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$USER\",\"password\":\"$PASS\"}")
TOKEN=$(echo "$LOGIN" | python3 -c "import json,sys;d=json.load(sys.stdin);print((d.get('data') or {}).get('token') or '')")
[ -z "$TOKEN" ] && { echo "❌ 登录失败: $LOGIN"; exit 1; }
echo "== 登录成功 =="

upsert() { # name display model base key quality cost
  local payload=$(python3 - "$1" "$2" "$3" "$4" "$5" "$6" "$7" <<'PY'
import json,sys
name,display,model,base,key,q,c=sys.argv[1:8]
print(json.dumps({"name":name,"display_name":display,"model":model,"base_url":base,
 "api_key":key,"api_type":"openai","enabled":True,
 "quality_score":float(q),"cost_per_1k":float(c),"max_rpm":60}))
PY
)
  # 先尝试更新，404/不存在则创建
  CODE=$(curl -s -o /dev/null -w "%{http_code}" -X PUT \
    -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
    "$BASE/api/llm/models/$1" -d "$payload")
  if [ "$CODE" != "200" ]; then
    curl -s -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
      "$BASE/api/llm/models" -d "$payload" > /dev/null
  fi
  echo "  ✓ $1 ($2)"
}

echo "== 写入 4 个国产主流渠道 =="
upsert deepseek "DeepSeek"        deepseek-chat         "https://api.deepseek.com"                            "$DS_KEY" 0.88 0.001
upsert qwen     "通义千问(Qwen)"   qwen-plus             "https://dashscope.aliyuncs.com/compatible-mode/v1"   "$QW_KEY" 0.85 0.002
upsert doubao   "豆包(Doubao)"     doubao-pro-32k        "https://ark.cn-beijing.volces.com/api/v3"            "$DB_KEY" 0.84 0.002
upsert ernie    "文心一言(ERNIE)"  ernie-4.0-8k-latest   "https://qianfan.baidubce.com/v2"                     "$ER_KEY" 0.86 0.004

# ---- 2) 场景路由切到 deepseek 主选 + 门禁适配 ----
CUR=$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE/api/llm/scene-routing")
PAYLOAD=$(python3 - <<PY
import json,sys
rows=json.loads('''$CUR''')["data"]
fb=["qwen","doubao","ernie"]
for r in rows:
    r["provider"]="deepseek"; r["fallbacks"]=fb; r["min_quality"]=min(r.get("min_quality",0.9),0.7)
print(json.dumps({"routes":rows,"operator":"geo-seed","commit_msg":"国产渠道主选路由(质量门禁适配DB评分)"}))
PY
)
curl -s -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  "$BASE/api/llm/scene-routing" -d "$PAYLOAD" > /dev/null
echo "== 场景路由已切换（primary=deepseek, 门禁≤0.7） =="

# ---- 3) 连通性验证 ----
echo "== 连通性测试 =="
for p in deepseek qwen doubao ernie; do
  R=$(curl -s -X POST -H "Authorization: Bearer $TOKEN" "$BASE/api/llm/models/$p/test" | head -c 120)
  echo "  $p => $R"
done

echo ""
echo "✅ 完成。刷新前端 https://$BASE/#/llmRouting/list 即可看到 4 个国产渠道"
echo "   （本地环境请打开 http://127.0.0.1:5173/#/llmRouting/list 或对应端口）"
