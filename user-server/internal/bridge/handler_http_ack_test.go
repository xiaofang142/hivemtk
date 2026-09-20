package bridge

import (
	"testing"
)

// TestIsIngestDuplicate_ReasonKeywords 验证上报 ack 闭环的原因判定：
// 只有服务端**明确写出的重复结论**才标 Duplicate（前端据此停发该 event_id）。
// 批15 把这张表从"关键词云"换成"与产出方逐字对齐的结论短语"：
// 正向取自 internal/service/inbox_ingress*.go 真实写入的 result.Reason（含中间件包装形态），
// 原先几个"看着像重复"的假设文案挪进反向表——其中 "skip due to cooldown" 冷却跳过根本
// 不是重复（下一条还得允许上报），而反向表最后两条才是本批判收紧的真正动机：
// 落库失败会被包进 reason（`inbox_ingress.go:856` → PG 唯一键原文自带 "duplicate key…"），
// 旧口径判成 duplicate ⇒ 前端 `accepted || duplicate` 停发 ⇒ 一条从没存进去的消息永久消失。
func TestIsIngestDuplicate_ReasonKeywords(t *testing.T) {
	positive := []string{
		"msg_id already exists",
		"msg_id already exists in DB; idempotent skip",
		"msg_id exists with different direction; DB direction preserved",
		"content_hash already exists in DB (platform outbound echo); idempotent skip",
		"intercepted by middleware: self-echo(platform msg_id exact match) (self_echo=true dup=false)",
		"intercepted by middleware: duplicate(channel+sender+content) within window (self_echo=false dup=true)",
		"duplicate delivery in 5min",
	}
	for _, r := range positive {
		if !isIngestDuplicate(r) {
			t.Errorf("应判定为重复: %q", r)
		}
	}

	negative := []string{
		"",
		"ai queued for processing",
		"accepted new message",
		"human locked",
		"queued for ai",
		"session is human-locked; bypass AI routing",
		"sender_type=system; persisted only",
		"intercepted by dedup middleware",
		"platform echo detected",
		"skip due to cooldown",
		"already exists in db",
		`batch handle error: 持久化消息失败: duplicate key value violates unique constraint "idx_message_hub_msg_id" (SQLSTATE 23505)`,
		"handle failed for text: 这条评论 duplicate 会被 skip 掉的",
	}
	for _, r := range negative {
		if isIngestDuplicate(r) {
			t.Errorf("不应判定为重复: %q", r)
		}
	}
}
