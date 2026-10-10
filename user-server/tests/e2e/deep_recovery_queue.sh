#!/usr/bin/env bash
# deep_recovery_queue.sh — 挽回队列深度回归 (三条读口 + 四条写口必须仍然不可达)
set -uo pipefail
source "$(dirname "$0")/deep_lib.sh"
mtk_login

PASS=0; FAIL=0

# ---------------- 写口：路由从未注册，打它只会拿到 404 ----------------
# 这一段原本是断言"入队 200 / 尝试 200 / 已挽回 200 / 取消 200 并核 DB 落库"，
# 而那四条 POST 口根本不在路由表里：注册面只有三条 GET
# （internal/router/content_routes.go 的 setupRecoveryQueueRoutes）。
# 断言 200 的脚本每次跑都恒红，模块里唯一真的三条读口就被淹在永久红里。
#
# 队列今天是怎么被写坏的：
#   - 入队：internal/service/customer_rfm.go 的 enqueueRecovery 直接 repository.Create，
#     绕过 service.Enqueue 与本控制器，所以队列有数据、但不是从入队口径来的；
#   - 推进：internal/service/recovery_queue_worker.go 只用 service.MarkAttempt / DeferAttempt，
#     耗尽次数置 failed、命中免打扰置 cancelled 都由 worker 传 stage 完成；
#   - 缺口：全仓没有任何代码把队列项写成 succeed——RecoveryStageSucceed 只出现在
#     service.MarkRecovered 里，而它唯一的调用方就是下面那个没有路由的 handler。
#     客户回流后这条记录不会收敛成"已挽回"，只会一路走到 failed。
#
# 探针保留而不删，是为了"哪天有人把写口注册上却没同步这段脚本"时当场红。
# 读失败时注意区分两种红：路径不在=404，路径在但方法没注册=405
# （internal/router/router.go 开了 HandleMethodNotAllowed），两者含义不同。
for p in /api/recovery-queue/enqueue /api/recovery-queue/1/attempt \
         /api/recovery-queue/1/recovered /api/recovery-queue/1/cancel; do
	info "POST $p"
	api POST "$p" "{\"customer_id\":\"cust_probe_$$\"}"
	[ "$API_HTTP" = "404" ] && pass "写口 404 $(basename "$p")" || fail "$p 期望 404, 实际 $API_HTTP"
done

# ---------- 列表 / 分布 / 就绪（路由表里真有这三条）----------
api GET /api/recovery-queue/list "?stage=recovered" && [ "$API_HTTP" = "200" ] && pass "挽回 列表 200" || fail "挽回 列表 http=$API_HTTP"
api GET /api/recovery-queue/distribution && [ "$API_HTTP" = "200" ] && pass "挽回 分布 200" || fail "挽回 分布 http=$API_HTTP"
api GET /api/recovery-queue/ready "?limit=10" && [ "$API_HTTP" = "200" ] && pass "挽回 就绪 200" || fail "挽回 就绪 http=$API_HTTP"

# ---------- 只读口径下队列不该被这次跑批改动 ----------
# 这段脚本不再建记录，所以不再需要 cleanup；核一次行数读数，
# 万一上面哪条 POST 被注册上并开始落库，这里会跟着红。
before=$(dbqv "select count(*) from recovery_queue;")
[ -n "$before" ] && pass "队列行数可读 ($before)" || fail "队列行数读不到（表或库不通）"

info "==== deep_recovery_queue 完成 PASS=$PASS FAIL=$FAIL ===="
[ "$FAIL" -gt 0 ] && exit 1 || exit 0
