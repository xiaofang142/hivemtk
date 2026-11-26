package bridge

import (
	"testing"
)

// TestBuildIngestAcks 锁定统一网关 ACK 契约：SageX Android 客户端的
// AckValidator 只接受 acks[{event_id,status}]，status 必须是
// accepted/duplicate/retry/rejected 之一；缺 acks 会被判 ACK_INVALID 并暂停上报。
func TestBuildIngestAcks(t *testing.T) {
	results := []*HTTPIngestResult{
		{EventID: "ok-1", Accepted: true},
		{EventID: "dup-1", Accepted: true, Duplicate: true},
		{EventID: "retry-1", Accepted: false, Reason: "INGRESS_FAILED"},
	}
	acks := buildIngestAcks(results)
	if len(acks) != len(results) {
		t.Fatalf("acks 数量=%d, want %d", len(acks), len(results))
	}
	want := []struct{ id, status string }{
		{"ok-1", "accepted"},
		{"dup-1", "duplicate"},
		{"retry-1", "retry"},
	}
	allowed := map[string]bool{"accepted": true, "duplicate": true, "retry": true, "rejected": true}
	for i, w := range want {
		if acks[i].EventID != w.id || acks[i].Status != w.status {
			t.Errorf("acks[%d]={%s,%s}, want {%s,%s}", i, acks[i].EventID, acks[i].Status, w.id, w.status)
		}
		if !allowed[acks[i].Status] {
			t.Errorf("acks[%d].status=%q 不在 SageX AckValidator 允许集合", i, acks[i].Status)
		}
	}
}

// TestIngestResponseCarriesUnifiedACK 锁定响应用例：ingested 之外必须同时
// 携带 schema_version 与 acks，浏览器扩展读 ingested、SageX App 读 acks，互不破坏。
func TestIngestResponseCarriesUnifiedACK(t *testing.T) {
	resp := &HTTPIngestResponse{
		OK:            true,
		Ingested:      []*HTTPIngestResult{{EventID: "e-1", Accepted: true}},
		SchemaVersion: "hivemtk.channel.v1",
		Acks:          buildIngestAcks([]*HTTPIngestResult{{EventID: "e-1", Accepted: true}}),
	}
	if resp.SchemaVersion != "hivemtk.channel.v1" {
		t.Fatalf("schema_version=%q", resp.SchemaVersion)
	}
	if len(resp.Acks) != 1 || resp.Acks[0].Status != "accepted" || resp.Acks[0].EventID != "e-1" {
		t.Fatalf("acks 不符合契约: %+v", resp.Acks)
	}
	if len(resp.Ingested) != 1 || !resp.Ingested[0].Accepted {
		t.Fatalf("ingested 被破坏（浏览器扩展依赖）: %+v", resp.Ingested)
	}
}

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
