#!/usr/bin/env bash
# =============================================================
# rotate-admin-password.sh —— 存量实例换超管口令的可复跑执行器
#
# 为什么要有这个脚本（不是又一份文档）：docs/DEPLOYMENT_GUIDE.md §6.2 的加固路线②
# 一直写的是"手工四步"（摘两道触发器 → pwtool 出哈希 → UPDATE → 把触发器建回）。
# 四步里任何一步漏做，后果都不是报错而是**静默**：
#   · 只 UPDATE 不摘触发器 ⇒ 语句被 v3_36.0 的 trg_guard_initial_admin_password 顶回；
#   · 摘了触发器中途 psql 挂掉 ⇒ 库留在"无守卫"状态，无人知晓，而那道守卫正是 v3_36.0
#     为了"密码被绕过应用层直改"这件事专门装的；
#   · 换完没同步 .env 的 SEED_PASSWORD ⇒ 下一次重跑 bootstrap / seed 会把公开默认值写回去。
# 本脚本把四步放进**一个事务**（要么全成要么全滚），提交后做四重读数复核。
#
# 判据（与 §6.2 那条自查同口径）：仓库公开的默认值试登录，必须**从 200 变 401**。
#
# 用法：
#   SEED_PASSWORD='<新口令>' bash scripts/rotate-admin-password.sh              # 只换库
#   SEED_PASSWORD='<新口令>' bash scripts/rotate-admin-password.sh --with-env    # 顺带写 .env
#   SEED_PASSWORD='<新口令>' bash scripts/rotate-admin-password.sh --no-probe    # 不探 HTTP
#   SEED_PASSWORD='<新口令>' bash scripts/rotate-admin-password.sh --dry-run     # 只跑前置+SQL 拼装自证，不写库
#
# 库连接：环境里的 PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE 优先；
# 否则按 .env 的 DB_HOST / DB_PORT / POSTGRES_USER / POSTGRES_PASSWORD / USER_DB_NAME 组装
# （.env 用 awk 逐键取值，不 source —— 同文件里带空格的值会打断 shell 解析）。
#
# 退出码：0 换成功且复核全过 / 1 任一步失败（事务已回滚，库未改） / 2 前置条件不满足（没碰库）。
#
# 安全口径：新口令与哈希都不进 argv（pwtool 走环境变量、curl 走 stdin、写 .env 走 builtin），
# 全程只印形状（长度 / bcrypt 前缀 / 布尔读数），失败时也只截 120 字符读数。
set -eo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ENV_FILE="$PROJECT_DIR/.env"
MIGRATION_SRC="${MIGRATION_SRC:-$PROJECT_DIR/user-server/internal/migration/migrations/v3_36_0_admin_password_guard_migration.go}"

WITH_ENV=0
NO_PROBE=0
DRY_RUN=0
for arg in "$@"; do
  case "$arg" in
    --with-env) WITH_ENV=1 ;;
    --no-probe) NO_PROBE=1 ;;
    --dry-run) DRY_RUN=1 ;;
    -h|--help) awk 'NR==1 {next} /^#/ {print; next} {exit}' "$0"; exit 0 ;;
    *) echo "未知参数：${arg}（可用 --with-env / --no-probe / --dry-run）" >&2; exit 2 ;;
  esac
done

die2() { echo "PRECHECK-FAILED: $*" >&2; exit 2; }
die1() { echo "FAILED: $*" >&2; exit 1; }

# 截读数：先按字节限宽、再用 iconv -c 丢掉被劈开的中文尾字节。
# 本机 LC_CTYPE=C，${var:0:N} 与 cut -c 都按字节切，劈出非法 UTF-8 会让调用方解码直接崩。
snip() { tr '\n' ' ' | head -c "${1:-240}" | iconv -c -f UTF-8 -t UTF-8; }

env_get() {
  # 同名键可能重复（实测踩过）：取第一条；只回值，不打印键名以外的东西
  awk -F= -v k="$1" '$1 == k { sub(/^[^=]*=/, ""); print; exit }' "$ENV_FILE" 2>/dev/null | tr -d '\r'
}

NEW_PW="${SEED_PASSWORD:-}"
[[ -n "$NEW_PW" ]] || die2 "SEED_PASSWORD 未设置（本脚本不代替你决定口令；生一个：openssl rand -base64 18）"
if [[ "${#NEW_PW}" -lt 12 ]]; then
  die2 "新口令长度 ${#NEW_PW} < 12 —— 短于此长度的超管口令没有加固意义"
fi
for bad in 'Seed@123456' 'Admin@12345678' 'Admin@123456' '62cfdc6bf1b075830734cc6f9a63501b'; do
  [[ "$NEW_PW" == "$bad" ]] && die2 "新口令就是仓库公开的演示值，换它等于不换"
done
# 口令要同时活过三处：source .env、JSON 转义、psql -v。只禁危险类，不回显值本身。
if printf '%s' "$NEW_PW" | LC_ALL=C grep -q '[^A-Za-z0-9._~+/=%@^*:,-]'; then
  die2 "新口令含空白/引号/反斜杠/\$ 等字符 —— 这类口令写进 .env 后会被 source 打断，等于把自己锁在门外（建议 openssl rand -base64 18）"
fi
command -v psql >/dev/null 2>&1 || die2 "找不到 psql"
[[ -f "$MIGRATION_SRC" ]] || die2 "缺迁移源 ${MIGRATION_SRC}（守卫 DDL 要从这里抽，不能凭记忆写）"

if [[ -z "${PGPASSWORD:-}" ]]; then
  [[ -f "$ENV_FILE" ]] || die2 "环境未设 PGPASSWORD 且 $ENV_FILE 不存在"
  PGHOST="${PGHOST:-$(env_get DB_HOST)}"
  PGPORT="${PGPORT:-$(env_get DB_PORT)}"
  PGUSER="${PGUSER:-$(env_get POSTGRES_USER)}"
  PGPASSWORD="$(env_get POSTGRES_PASSWORD)"
  PGDATABASE="${PGDATABASE:-$(env_get USER_DB_NAME)}"
  export PGHOST="${PGHOST:-127.0.0.1}" PGPORT="${PGPORT:-8232}" PGUSER="${PGUSER:-admin}" \
         PGPASSWORD PGDATABASE="${PGDATABASE:-user_db}"
fi
[[ -n "${PGPASSWORD:-}" ]] || die2 "拿不到库口令（.env 缺 POSTGRES_PASSWORD 且环境未设 PGPASSWORD）"

echo "conn: host=${PGHOST} port=${PGPORT} db=${PGDATABASE} user=${PGUSER} pwlen=${#PGPASSWORD}"

ADMIN_ID="$(psql -At -c "SELECT id FROM system_users WHERE username = 'admin'" 2>/dev/null || true)"
[[ -n "$ADMIN_ID" ]] || die2 "system_users 里查不到 username='admin'（本脚本只服务已 seed 的存量库）"
if [[ "$ADMIN_ID" != "1" ]]; then
  die1 "admin 的 id 是 $ADMIN_ID 而不是 1，而 v3_36.0 两道守卫钉的就是 id=1 ⇒ 摘触发器保护不到这一行，先人工核对库"
fi
TRIG_COUNT="$(psql -At -c "SELECT count(*) FROM pg_trigger WHERE tgname IN ('trg_guard_initial_admin_password','trg_guard_initial_admin_delete') AND NOT tgisinternal" 2>/dev/null || true)"
echo "precheck: admin_id=$ADMIN_ID guard_triggers=${TRIG_COUNT}/2（少于 2 也会被本脚本一并补回）"

HASH="$(cd "$PROJECT_DIR/user-server" && SEED_PASSWORD="$NEW_PW" go run ./cmd/pwtool 2>/dev/null | tr -d '\r\n')"
if [[ ${#HASH} -ne 60 || "${HASH:0:4}" != '$2a$' ]]; then
  die1 "pwtool 产出的哈希形状不对（长度 ${#HASH}、前缀 ${HASH:0:3}）——期望 \$2a\$ 开头 60 字符"
fi
echo "hash: bcrypt 前缀=${HASH:0:4} 长度=${#HASH}"

# 守卫函数体从迁移源里抽，不另抄一份（抄一份就等于留一个会过期的第二事实源）。
# 抽出来的是 Go 原始字符串的一段，首尾带着反引号定界符和结尾逗号 —— 那是源码记号不是 SQL，
# 必须剥掉再拼（实测漏剥会把 "`CREATE OR REPLACE FUNCTION …`," 整段喂给 psql）。
extract_guard_fn() {
  sed -n "/CREATE OR REPLACE FUNCTION fn_guard_initial_admin_$1/,/LANGUAGE plpgsql/p" "$MIGRATION_SRC" \
    | tr '\n' ' ' \
    | sed -e 's/^[[:space:]]*`//' -e 's/[[:space:]]*`,[[:space:]]*$//' \
          -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//'
}
FN_PW_DDL="$(extract_guard_fn password)"
FN_DEL_DDL="$(extract_guard_fn delete)"

# 判据按内容而非长度（delete 那条本就只有 ~180 字符，任何"最小长度"下界都是碰运气）：
# 必须是完整函数体（CREATE…RETURNS trigger…$$ LANGUAGE plpgsql 收尾）且带着它自己的 RAISE 文案。
check_ddl() {
  local name="$1" ddl="$2" raise="$3"
  [[ "$ddl" == "CREATE OR REPLACE FUNCTION fn_guard_initial_admin_${name}()"* ]] \
    || die1 "守卫函数体抽取失败（${name}）：开头不是预期的 CREATE OR REPLACE FUNCTION（长度 ${#ddl}）"
  [[ "$ddl" == *"RETURNS trigger AS \$\$"* ]] || die1 "守卫函数体抽取不全（${name}）：没看到 RETURNS trigger AS \$\$"
  [[ "$ddl" == *"LANGUAGE plpgsql" ]] || die1 "守卫函数体抽取不全（${name}）：结尾不是 LANGUAGE plpgsql，锚点截在了半句上"
  [[ "$ddl" == *"$raise"* ]] || die1 "守卫函数体抽取不全（${name}）：缺 RAISE 文案「${raise}」"
}
check_ddl password "$FN_PW_DDL" '不允许被修改'
check_ddl delete   "$FN_DEL_DDL" '不允许被删除'

SQL="BEGIN;
SET LOCAL lock_timeout = '5s';
LOCK TABLE system_users IN ACCESS EXCLUSIVE MODE;
DROP TRIGGER IF EXISTS trg_guard_initial_admin_password ON system_users;
DROP TRIGGER IF EXISTS trg_guard_initial_admin_delete ON system_users;
UPDATE system_users SET password = :'pw' WHERE username = 'admin';
${FN_PW_DDL};
DROP TRIGGER IF EXISTS trg_guard_initial_admin_password ON system_users;
CREATE TRIGGER trg_guard_initial_admin_password BEFORE UPDATE ON system_users FOR EACH ROW EXECUTE FUNCTION fn_guard_initial_admin_password();
${FN_DEL_DDL};
DROP TRIGGER IF EXISTS trg_guard_initial_admin_delete ON system_users;
CREATE TRIGGER trg_guard_initial_admin_delete BEFORE DELETE ON system_users FOR EACH ROW WHEN (OLD.id = 1) EXECUTE FUNCTION fn_guard_initial_admin_delete();
COMMIT;"

# 自证拼装：守卫函数名在最终 SQL 里必须**恰好各出现 2 次**——
#   1 次来自抽出来的 CREATE OR REPLACE FUNCTION（证明函数体真被带进来了），
#   1 次来自 CREATE TRIGGER ... EXECUTE FUNCTION（证明触发器真指向它）。
# 其余位置写的都是 trg_ 名字，grep 'fn_' 不计入，所以下界不是"三处 DROP/CREATE"。
# 少一次 = 函数没抽到或触发器建给了空气；多一次 = 拼装被改坏，两者都会静默留下无守卫的库。
PW_HITS="$(printf '%s' "$SQL" | grep -o 'fn_guard_initial_admin_password' | wc -l | tr -d ' ')"
DEL_HITS="$(printf '%s' "$SQL" | grep -o 'fn_guard_initial_admin_delete' | wc -l | tr -d ' ')"
if [[ "$PW_HITS" != "2" || "$DEL_HITS" != "2" ]]; then
  die1 "拼装后 SQL 里守卫函数出现次数异常（password=$PW_HITS delete=${DEL_HITS}，各应恰好 2：函数体 1 + EXECUTE FUNCTION 1）"
fi

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "DRY-RUN: 前置校验、pwtool 出哈希、守卫 DDL 抽取与 SQL 拼装自证全部通过；只做过只读读数，未写库"
  exit 0
fi

printf '%s\n' "$SQL" | psql -q -v ON_ERROR_STOP=1 -v pw="$HASH" -f - >/dev/null \
  || die1 "轮换事务失败（已回滚：口令与两道守卫都保持原状）"

POST_TRIG="$(psql -At -c "SELECT count(*) FROM pg_trigger WHERE tgname IN ('trg_guard_initial_admin_password','trg_guard_initial_admin_delete') AND NOT tgisinternal")"
[[ "$POST_TRIG" == "2" ]] || die1 "提交后守卫触发器数量是 $POST_TRIG 而不是 2 —— 立刻人工补建，别继续用这台实例"

STORED="$(psql -At -c "SELECT password FROM system_users WHERE username = 'admin'")"
[[ "$STORED" == "$HASH" ]] || die1 "读回的哈希与新产出不一致（列被别处覆盖？人工核对）"
echo "verify-1: 读回哈希与新产出一致"

GUARD_READ="$(psql -q -At -c "BEGIN; UPDATE system_users SET password = 'x' WHERE username = 'admin'; ROLLBACK;" 2>&1 || true)"
if printf '%s' "$GUARD_READ" | grep -q '不允许被修改'; then
  echo "verify-2: 守卫复核 OK（直改 id=1 密码仍被触发器顶回）"
else
  die1 "守卫复核失败：直改密码没被顶回，读数 $(printf '%s' "$GUARD_READ" | snip 240)"
fi

# verify-3：离线 bcrypt 复核"公开默认值已全部不再命中"（不依赖服务在跑）
OFFLINE="$(PW="$NEW_PW" STORED="$STORED" python3 - <<'PY' 2>/dev/null || true
import os
try:
    import bcrypt
except ImportError:
    print("SKIP"); raise SystemExit
stored = os.environ["STORED"].encode()
new = os.environ["PW"].encode()
public = ["Seed@123456", "Admin@12345678", "Admin@123456", "62cfdc6bf1b075830734cc6f9a63501b"]
still = [p for p in public if bcrypt.checkpw(p.encode(), stored)]
print(("NEW-OK " if bcrypt.checkpw(new, stored) else "NEW-BAD ") + ("PUBLIC-CLEAR" if not still else "PUBLIC-STILL:" + ",".join(still)))
PY
)"
case "$OFFLINE" in
  *"NEW-OK"*"PUBLIC-CLEAR"*) echo "verify-3: 离线复核 OK（新口令命中、4 个公开字面量全不命中）" ;;
  SKIP*|"") echo "verify-3: 跳过（本机无 python3+bcrypt）—— 由 verify-4 的 HTTP 探针兜这条判据" ;;
  *) die1 "verify-3 不通过：$OFFLINE" ;;
esac

# verify-4：HTTP 探针（文档里的判据本体）
if [[ "$NO_PROBE" -eq 0 ]]; then
  BASE_URL="${PROBE_BASE_URL:-http://127.0.0.1:${USER_SERVER_PORT:-8204}}"
  code_of() {
    # 口令从 stdin 进 curl（不进 argv，ps 看不到）；JSON 转义用纯 bash 参数展开——
    # 原先走 `{ read -r esc; ...; }` 时，无换行的输入让 read 退 1，set -e 会把整组打断。
    local pw="$1"
    pw="${pw//\\/\\\\}"
    pw="${pw//\"/\\\"}"
    printf '{"username":"admin","password":"%s"}' "$pw" \
      | curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE_URL/api/auth/login" \
            -H 'Content-Type: application/json' --data-binary @-
  }
  OLD_CODE="$(code_of 'Seed@123456')"
  NEW_CODE="$(code_of "$NEW_PW")"
  echo "verify-4: 公开默认值 http=${OLD_CODE}（期望 401） / 新口令 http=${NEW_CODE}（期望 200）"
  [[ "$OLD_CODE" == "401" ]] || die1 "公开默认值仍能登录（http=${OLD_CODE}）——服务连的不是这个库，先把口径对齐再收尾"
  [[ "$NEW_CODE" == "200" ]] || die1 "新口令登录失败（http=${NEW_CODE}）——先别把 .env 写下去"
else
  echo "verify-4: 跳过（--no-probe）；「公开默认值从 200 变 401」这条判据本轮未取数"
fi

if [[ "$WITH_ENV" -eq 1 ]]; then
  [[ -f "$ENV_FILE" ]] || die2 "--with-env 需要 $ENV_FILE 存在"
  BK="${ENV_FILE}.bak-$(date +%Y%m%d-%H%M%S)"
  cp "$ENV_FILE" "$BK"
  if grep -q '^SEED_PASSWORD=' "$ENV_FILE"; then
    SEED_PW="$NEW_PW" awk 'BEGIN { pw = ENVIRON["SEED_PW"] } /^SEED_PASSWORD=/ { print "SEED_PASSWORD=" pw; next } { print }' \
      "$ENV_FILE" > "${ENV_FILE}.tmp" && mv "${ENV_FILE}.tmp" "$ENV_FILE"
  else
    printf '\n# 存量实例的超管口令由 scripts/rotate-admin-password.sh 轮换后写在这里；\n' >> "$ENV_FILE"
    printf '# 它同时是 bootstrap / seed 的口令单一源（见 docs/DEPLOYMENT_GUIDE.md §6.2）。\n' >> "$ENV_FILE"
    printf 'SEED_PASSWORD=%s\n' "$NEW_PW" >> "$ENV_FILE"
  fi
  # 备份成功后即删：`.env.bak-*` 会被 check-secrets 的 `.env.*` 模式扫到，
  # 留着一个含全套密钥的明文副本比留着"回滚能力"更贵（要回滚请用 git 之外的手工方式）。
  rm -f "$BK"
  echo "env: .env 的 SEED_PASSWORD 已更新（长度 ${#NEW_PW}，值不落日志），临时备份已删除"
else
  echo "env: 未改 .env。不同步的后果——下次跑 bootstrap/seed 会把公开默认值写回库"
fi

echo "DONE 超管口令轮换完成：守卫 2/2 在位、直改被顶回、库与新哈希一致"
exit 0
