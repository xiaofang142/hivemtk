package channelgw

import "testing"

// 批15（同行调研维度3/4 落地）：IsDuplicateReason 的决定权太大——命中即让传输层回
// Duplicate=true，而扩展侧 `_markConfirmedFromResponse` 的条件是 `accepted || duplicate`
// （user-web/bridge/src/core/uplink.js:65），所以一次误判 = 那条消息**永久不再上报**。
// 因此判定只能认「服务端明确给出的重复结论短语」，不能认任意子串：
// 生产路径 `inbox_ingress.go:856` 会把落库错误整段包进 reason
// （`batch handle error: 持久化消息失败: <DB 原文>`），而 PG 的唯一键冲突原文里天然带
// "duplicate key value violates unique constraint" —— 用子串嗅探时，一次「没存进去」
// 会被判成「已经存过了」，客户端从此闭嘴。这条不是推演：克隆树里喂真文案跑出过 true。

func TestIsDuplicateReason_只认重复结论短语不认任意子串(t *testing.T) {
	wantTrue := []string{
		// 与生产/夹具里真实产出的重复结论逐字一致（改这些文案要同步改这里）
		"msg_id already exists in DB; idempotent skip",
		"content_hash already exists in DB (platform outbound echo); idempotent skip",
		"intercepted by middleware: self-echo(platform msg_id exact match) (self_echo=true dup=false)",
		"intercepted by middleware: duplicate(channel+sender+content) within window (self_echo=false dup=true)",
		"msg_id already exists (mock): evt-dup",
		// 同一条幂等跳过的方向冲突分支：旧实现漏判（不含任何关键字），
		// 客户端于是每轮巡逻都重报同一条，服务端每次都再走一遍钩子2
		"msg_id exists with different direction; DB direction preserved",
	}
	for _, s := range wantTrue {
		if !IsDuplicateReason(s) {
			t.Errorf("应判重复但判成非重复: %q", s)
		}
	}

	wantFalse := []string{
		"",
		"trigger AI customer service",
		"session is human-locked; bypass AI routing",
		"sender_type=system; persisted only (系统消息不触发 AI)",
		"batch: 3 messages merged, 1 AI trigger",
		// ↓ 本批立批原因：落库失败被包进 reason，文案里天然带 duplicate
		`batch handle error: 持久化消息失败: duplicate key value violates unique constraint "idx_message_hub_msg_id" (SQLSTATE 23505)`,
		"ingest error: dial tcp 127.0.0.1:5432: connect: connection refused",
		// ↓ 任何把用户正文带进 reason 的形态：正文里出现关键词不等于消息重复
		"handle failed for text: 这条评论 duplicate 会被 skip 掉的",
	}
	for _, s := range wantFalse {
		if IsDuplicateReason(s) {
			t.Errorf("不应判重复（判了就是让客户端永久停止重发一条没落库的消息）: %q", s)
		}
	}
}
