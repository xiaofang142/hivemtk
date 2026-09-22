#!/bin/bash
# =============================================================================
# scripts/post_deploy_check.sh —— 知识库分组（G0–G4）部署后一键验收
#
# 用法：在**仓库根目录**执行 `bash scripts/post_deploy_check.sh`
#   前置：user-server 已在跑、compose 数据层已起、仓库根有 ./.env
#   退出码：0＝五项全过；1＝任一项不过（红因印在屏幕上）
#
# 逐档口径与每条判据的实测出处见 docs/operations/KNOWLEDGE_GROUP_DEPLOY.md
# （§1.2 端口、§2.4 索引名单、§6.1 kb_code 唯一约束、§6.3 观测读数）。
# 本批（2026-09-22）落仓前把 1–5 步在开发实例上真跑过：1–4 步 rc=0；
# 第 5 步跑到完、rc=1，唯一一条红是并发共用 8232 测试库的既有偶发红
# （TestOrderDraftSweepWorker_EndToEndFlipsAndPurgesRealRows，隔离复跑绿），口径与证据见文档 §7.1。
#
# 为什么它落在仓内而不在文档里抄：这几步的写法每一格都有一个"看着对、实际吞红"的
# 形态（见下面各步的注释），手抄一遍就会抄掉其中一格。
# =============================================================================
set -euo pipefail

BASE="${BASE:-http://127.0.0.1:8204}"
[ -f ./.env ] || { echo "❌ 仓库根没有 ./.env —— PG 口令与发布端口都从它读（离开仓库根执行也会撞这条）"; exit 1; }
set -a; . ./.env; set +a       # PG 口令与端口都要这里的值（.env 里的实际键名见下一行）

# 端口：.env 里 USER_POSTGRES_HOST_PORT=8232 已把 compose 的 8202 默认覆盖掉（口径见文档 §1.2），
# 所以这里先读它、再退 DB_PORT、最后才是 8232；写在 . 之前会被 .env 覆盖，等于白设。
PGPORT="${PGPORT:-${USER_POSTGRES_HOST_PORT:-${DB_PORT:-8232}}}"

# admin 口令：必须跟 seed 写入时用的是同一条链，顺序照抄不许自己排 ——
# scripts/bootstrap.sh:63 是 SEED_PASSWORD > ADMIN_PASSWORD > 公开默认值（下一行再把结果回灌给
# ADMIN_PASSWORD），Go 侧同一条链写在 user-server/cmd/seed/seed_users.go:35 的注释里。
# 反过来排（先 ADMIN 再 SEED）的写法，在两个变量都设过值的实例上会挑中一个库里没有的那个口令 ⇒ 必 401。
# 最后一档不写字面量，从 seed_users.go:32 的常量行抽 —— 那个值本来就是随仓库公开的，
# 这里再抄一份只会多出一个会过期的副本（同 scripts/check-admin-default-credential.sh 的口径）。
# 旧版这里排过第三档 HIVEMTK_ADMIN_PASS，本机实测证伪：它只有 scripts/api_verify_full.py 一处读取点，
# 拿它登录实返 401 UNAUTHORIZED_2001 ⇒ 排在链里只会把"口令不对"变成"静默用错口令再撞 401"。
ADMIN_PASSWORD="${SEED_PASSWORD:-${ADMIN_PASSWORD:-}}"
if [ -z "$ADMIN_PASSWORD" ]; then
  ADMIN_PASSWORD=$(sed -nE 's/.*seedPasswordDefault[[:space:]]*=[[:space:]]*"([^"]*)".*/\1/p' \
    user-server/cmd/seed/seed_users.go | head -1)
  [ -n "$ADMIN_PASSWORD" ] || { echo "❌ 抽不到 seed 常量（seed_users.go 改名或常量改名了就同步这里）"; exit 1; }
  echo "⚠️ 走的是仓库公开的演示口令：能用它登进去 = 这台实例还没换过超管口令"
  echo "   对外实例请按 scripts/rotate-admin-password.sh + docs/operations/secret_rotation.md 处理"
fi

echo "=== 部署后验收（PG 口 ${PGPORT} / API ${BASE}）==="

# 1. 健康检查：/health 探 DB+Redis，任一不可用即 503 ⇒ curl -f 会红
echo "[1/5] 健康检查..."
curl -sf "$BASE/health" >/dev/null || { echo "❌ 健康检查失败：$BASE/health 不是 200（先核 user-server 进程与 DB/Redis）" && exit 1; }

# 2. 表结构验证
#    这三格取值都是 `VAR=$(...)`：命令本身失败（psql 连不上 / curl 连不上）在 set -e 下会
#    直接静默退出，屏幕上没有红因 ⇒ 每格都挂一个"取值失败"分支，把"环境不通"与"判据不过"分开。
echo "[2/5] 数据库表验证..."
TABLES=$(PGPASSWORD="$POSTGRES_PASSWORD" psql -h 127.0.0.1 -p "$PGPORT" -U admin -d user_db -tAc "SELECT count(*) FROM information_schema.tables WHERE table_name IN ('knowledge_bases', 'agent_kb_bindings');") \
  || { echo "❌ psql 取数失败：连不上 127.0.0.1:${PGPORT} 或口令不对（先核 ${PGPORT} 上有没有 PG 在听）"; exit 1; }
[ "$TABLES" -eq 2 ] || { echo "❌ 缺少表 (期望 2, 实际 $TABLES)" && exit 1; }

# 3. 索引验证（下界取 10 的依据＝本机活库实测 12 条，两表各 6，全名单见文档 §2.4）
echo "[3/5] 索引验证..."
INDEXES=$(PGPASSWORD="$POSTGRES_PASSWORD" psql -h 127.0.0.1 -p "$PGPORT" -U admin -d user_db -tAc "SELECT count(*) FROM pg_indexes WHERE tablename IN ('knowledge_bases', 'agent_kb_bindings');") \
  || { echo "❌ psql 取数失败：连不上 127.0.0.1:${PGPORT} 或口令不对"; exit 1; }
[ "$INDEXES" -ge 10 ] || { echo "❌ 索引数过少 (期望 ≥10, 实际 $INDEXES)" && exit 1; }

# 4. API 端到端：/api/* 整组挂 JWTAuthMiddleware，不带 token 直接 401。
#    kb_code 上压着 uq_knowledge_bases_kb_code，而它是**非 partial** 索引（不带 WHERE deleted_at IS NULL）
#    ⇒ 用例名必须带一次性尾巴；结尾那条 DELETE 是软删（模型带 gorm.DeletedAt，索引
#    idx_knowledge_bases_deleted_at 就是它的），只把行从列表里摘掉，**腾不出这个 kb_code**，
#    同一个 code 第二次建必撞唯一约束（本机实测：报 500 + INTERNAL_ERROR_6002，见文档 §6.1）。
#    登录这一格旧写法是 `curl -sf ... | jq -r`：口令不对时 -f 让 curl 退 22、jq 拿到空输入，
#    整条管道在 set -e 下静默死掉，屏幕上只有"[4/5] API 端到端..."一行，没有红因。
#    这里改成先收状态码再判：401 要看得见，且必须提示防爆破闸（/api/auth/login 挂
#    BruteForceGuard("auth.login")，15 分钟窗口内 5 次失败锁 127.0.0.1 半小时 ⇒ 别连试）。
echo "[4/5] API 端到端..."
LOGIN=$(curl -s -w '\n%{http_code}' -X POST "$BASE/api/auth/login" -H "Content-Type: application/json" \
  -d "{\"username\":\"admin\",\"password\":\"${ADMIN_PASSWORD}\"}") \
  || { echo "❌ 登录请求没发出去：${BASE} 连不上（进程没起／端口不对，与口令无关）"; exit 1; }
LOGIN_CODE=$(printf '%s' "$LOGIN" | tail -n1)
LOGIN_BODY=$(printf '%s' "$LOGIN" | sed '$d')
if [ "$LOGIN_CODE" != "200" ]; then
  echo "❌ 登录失败：HTTP ${LOGIN_CODE}；响应 $(printf '%s' "$LOGIN_BODY" | head -c 200)"
  echo "   口令错不是唯一可能：防爆破锁定期内同样是这里。本脚本不做重试，"
  echo "   请按红因核库里 system_users（认证表是 system_users，不是 user）与上面那条链路档位；"
  echo "   连续失败会触发锁，改一次试一次，别循环。"
  exit 1
fi
TOKEN=$(printf '%s' "$LOGIN_BODY" | jq -r '.data.token')
[ "$TOKEN" != "null" ] && [ -n "$TOKEN" ] || { echo "❌ 200 但响应里没有 .data.token（接口契约变了，同步这里）"; exit 1; }
KBCODE="KB-DEPLOY-VERIFY-$(date +%s)"
RESP=$(curl -s -w '\n%{http_code}' -X POST "$BASE/api/knowledge-bases" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"kb_code\":\"$KBCODE\",\"type\":\"faq\",\"name\":\"verify\",\"owner_type\":\"shared\"}") \
  || { echo "❌ 创建请求没发出去：${BASE} 连不上（登录刚成功过 ⇒ 多半是这一分钟里进程重启了）"; exit 1; }
RESP_CODE=$(printf '%s' "$RESP" | tail -n1)
RESP_BODY=$(printf '%s' "$RESP" | sed '$d')
if [ "$RESP_CODE" != "200" ] && [ "$RESP_CODE" != "201" ]; then
  echo "❌ 创建失败：HTTP ${RESP_CODE}；响应 $(printf '%s' "$RESP_BODY" | head -c 300)"
  exit 1
fi
KBID=$(printf '%s' "$RESP_BODY" | jq -r '.data.id')
[ "$KBID" != "null" ] && [ -n "$KBID" ] || { echo "❌ 建成功了但响应里没有 .data.id，无法清理"; exit 1; }
# 清掉自己造的这条（软删，同上：目的是不污染列表，不是让 kb_code 能被复用）
curl -sf -o /dev/null -X DELETE "$BASE/api/knowledge-bases/${KBID}" \
  -H "Authorization: Bearer $TOKEN" || echo "⚠️ 验收行清理失败（不影响结论，去库里手工清 id=${KBID}）"

# 5. 测试套件：必须 cd 进 user-server；service 包单跑就要 450–880s（本机实测口径），
#    旧写法的 -timeout 120s 永远跑不完，而结尾的 `2>&1 | tail -3` 会把 go test 的退出码
#    换成 tail 的 ⇒ 整套测试红了脚本照样打印"全部验收通过"。这里既不截断也不吞码。
#    POSTGRES_TEST_PORT 是 user-server/internal/pkg/testutil/testdb.go:68 的真实读取点。
#    预算：整批在本机要留 ≥45 分钟；磁盘不够时 go build cache 写不下去会以"编译失败"的形态混进来，
#    先看 df 再判定红因（本批落仓时本机剩 1.5Gi，这一步没能跑完，见文件头说明）。
echo "[5/5] 测试套件..."
( cd user-server
  POSTGRES_TEST_PORT="${POSTGRES_TEST_PORT:-$PGPORT}" \
    go test -p 1 -count=1 -timeout 2500s ./internal/repository ./internal/service ./test/... )

echo "✅ 全部验收通过"
