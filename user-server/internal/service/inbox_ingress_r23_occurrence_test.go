package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// 本文件收 §8.3-17：桥接上报把「内容哈希」当消息身份，于是同一会话里客户把同一句话
// 说第二遍必然是同一个 msg_id —— 第二条既不入库也不回（客户侧表现＝「说了没回」）。
//
// 修法是把身份分层，而不是把闸门拆掉：上报方（扩展）看得见两个 DOM 节点，它按出现次数
// 给第二条及以后的同文本气泡发 `mh:<hash>#<n>`（n≥1）这种「我确认这是独立的一条」形状的
// event_id。服务端对这一形状**只**放弃内容维度的嗅探（Redis 内容窗口 + 钩子2.5 内容哈希），
// msg_id 精确判等（钩子2）照旧 —— 所以真重投（同一帧重发）仍然只会有一行。
//
// 首条的 event_id 一字不改（仍是裸 `mh:<hash>`），存量行与升级期的重报判定都不受影响。

// r23OccurrenceHash 取扩展会给出的首条 event_id（与扩展 types.js contentHash 严格同源）。
func r23OccurrenceHash(platform, conv, content string) string {
	return ContentHashMsgID(platform, conv, content)
}

// r23SeedInbound 用真实落库路径预置「首帧已入库」的那一行（msg_id 就是裸内容哈希）。
// 走 PersistBridgeHistory 而不是 db.Create：行形状（status/sent_at/列默认值）由生产代码决定，
// 夹具自己拼结构体等于把「这行到底长什么样」写成测试的假设。
func r23SeedInbound(t *testing.T, svc *InboxIngressService, nonce, platform, conv, content, msgID string) {
	t.Helper()
	if err := svc.PersistBridgeHistory(context.Background(), r23Event(nonce, msgID, conv, content), "inbound"); err != nil {
		t.Fatalf("预置 inbound 失败: %v", err)
	}
}

// TestIngress_OccurrenceSuffixSkipsContentWindowDedup §8.3-17 的服务端半边（入口层）：
// 带发生次数后缀的第二条不得再被「同发送者+同内容+窗口内」那把 Redis 键拦下。
func TestIngress_OccurrenceSuffixSkipsContentWindowDedup(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	nonce := fmt.Sprintf("occur-%d", time.Now().UnixNano())
	content := "好的 " + nonce
	conv := "conv-r23-occur"
	platform := string(model.ChannelXHS)
	base := r23OccurrenceHash(platform, conv, content)

	first, err := svc.interceptInbound(ctx, r23Event(nonce, base, conv, content))
	if err != nil {
		t.Fatalf("首条 interceptInbound: %v", err)
	}
	if first.Blocked {
		t.Fatalf("首条不该被拦：%s", first.Reason)
	}
	second, err := svc.interceptInbound(ctx, r23Event(nonce, base+"#1", conv, content))
	if err != nil {
		t.Fatalf("第二条 interceptInbound: %v", err)
	}
	if second.Blocked {
		t.Errorf("§8.3-17 未达成：上报方已按出现次数给出独立身份，第二条仍被内容窗口去重拦下 ⇒ blocked=%v dup=%v reason=%q"+
			"（同一会话里连发两遍同一句话是两条真实消息）", second.Blocked, second.IsDup, second.Reason)
	}
}

// TestIngress_PlainContentHashStillContentDeduped 同一枚刀的反向半边：裸内容哈希（无后缀）
// 仍是「内容就是唯一身份」的形态，第二条必须继续被内容窗口拦住 —— 否则就是把上游重投的
// 最后一层兜底也摘掉了。
func TestIngress_PlainContentHashStillContentDeduped(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	nonce := fmt.Sprintf("plain-%d", time.Now().UnixNano())
	content := "在吗 " + nonce
	conv := "conv-r23-plain"
	platform := string(model.ChannelXHS)
	base := r23OccurrenceHash(platform, conv, content)

	if d, err := svc.interceptInbound(ctx, r23Event(nonce, base, conv, content)); err != nil || d.Blocked {
		t.Fatalf("首条应放行: %+v err=%v", d, err)
	}
	// 同一条的二次上报（DOM 抖动 / _sentKeys 过期后重报）：身份没变，仍按内容窗口拦。
	d, err := svc.interceptInbound(ctx, r23Event(nonce, base, conv, content))
	if err != nil {
		t.Fatalf("第二条 interceptInbound: %v", err)
	}
	if !d.Blocked || !d.IsDup {
		t.Errorf("裸内容哈希的同内容第二条必须仍判重复，got blocked=%v dup=%v", d.Blocked, d.IsDup)
	}
}

// TestIngress_NonOccurrenceHashLikeIDSuffixStillContentDeduped 锁「只有 mh:<hash>#n 这一形状」：
// 别的 id 带 #1 后缀（渠道官方 id 恰好含 # 的形态）不得获得豁免。
func TestIngress_NonOccurrenceHashLikeIDSuffixStillContentDeduped(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	nonce := fmt.Sprintf("shape-%d", time.Now().UnixNano())
	content := "发货了吗 " + nonce
	conv := "conv-r23-shape"

	if d, err := svc.interceptInbound(ctx, r23Event(nonce, "wamid.ABC#1", conv, content)); err != nil || d.Blocked {
		t.Fatalf("首条应放行: %+v err=%v", d, err)
	}
	d, err := svc.interceptInbound(ctx, r23Event(nonce, "wamid.DEF#2", conv, content))
	if err != nil {
		t.Fatalf("第二条 interceptInbound: %v", err)
	}
	if !d.Blocked {
		t.Errorf("非 mh:<hash>#n 形状的后缀不该被当成独立身份豁免，got %+v", d)
	}
}

// TestPersistBridgeHistory_OccurrenceSuffixPersistsSecondRow §8.3-17 的落库半边：
// 钩子2.5 的内容哈希嗅探不得吞掉带发生次数后缀的第二条。
func TestPersistBridgeHistory_OccurrenceSuffixPersistsSecondRow(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const platform = "xiaohongshu"
	nonce := fmt.Sprintf("h25-%d", time.Now().UnixNano())
	content := "好的 " + nonce
	conv := "conv-r23-h25"
	base := r23OccurrenceHash(platform, conv, content)

	r23SeedInbound(t, svc, nonce, platform, conv, content, base)

	if err := svc.PersistBridgeHistory(ctx, r23Event(nonce, base+"#1", conv, content), "inbound"); err != nil {
		t.Fatalf("PersistBridgeHistory: %v", err)
	}
	var count int64
	if err := db.Model(&model.MessageHub{}).
		Where("platform = ? AND conversation_id = ? AND md5(content) = md5(?)", platform, conv, content).
		Count(&count).Error; err != nil {
		t.Fatalf("统计同内容行数: %v", err)
	}
	if count != 2 {
		t.Errorf("§8.3-17 未达成：同会话同文本的第二条被钩子2.5 的内容哈希嗅探吞了，实际行数=%d"+
			"（上报方已声明这是独立的一条，内容维度不再有权判重复）", count)
	}
}

// TestPersistBridgeHistory_OccurrenceSuffixStillExactIdempotent 同一枚刀的反向半边：
// 同一条 `mh:<hash>#1` 帧重发两次，只许有一行 —— 豁免的是内容嗅探，不是 msg_id 精确判等。
func TestPersistBridgeHistory_OccurrenceSuffixStillExactIdempotent(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const platform = "xiaohongshu"
	nonce := fmt.Sprintf("exact-%d", time.Now().UnixNano())
	content := "麻烦催下发货 " + nonce
	conv := "conv-r23-exact"
	base := r23OccurrenceHash(platform, conv, content)
	second := base + "#1"

	if err := svc.PersistBridgeHistory(ctx, r23Event(nonce, second, conv, content), "inbound"); err != nil {
		t.Fatalf("首帧 PersistBridgeHistory: %v", err)
	}
	if err := svc.PersistBridgeHistory(ctx, r23Event(nonce, second, conv, content), "inbound"); err != nil {
		t.Fatalf("重发帧 PersistBridgeHistory: %v", err)
	}
	var count int64
	db.Model(&model.MessageHub{}).Where("msg_id = ?", second).Count(&count)
	if count != 1 {
		t.Errorf("同一帧重发必须仍是 1 行（钩子2 精确判等不许被放宽），实际 msg_id=%s 行数=%d", second, count)
	}
}

// TestPersistBridgeHistory_ForeignIDStillCaughtByContentHash 锁豁免面：升级期/虚拟列表重渲染
// 换出「与内容哈希完全不同」的 id 时，钩子2.5 仍是那层兜底，必须继续命中。
func TestPersistBridgeHistory_ForeignIDStillCaughtByContentHash(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const platform = "xiaohongshu"
	nonce := fmt.Sprintf("foreign-%d", time.Now().UnixNano())
	content := "这个多少钱 " + nonce
	conv := "conv-r23-foreign"
	base := r23OccurrenceHash(platform, conv, content)

	r23SeedInbound(t, svc, nonce, platform, conv, content, base)

	if err := svc.PersistBridgeHistory(ctx, r23Event(nonce, "dom:7fk2lq", conv, content), "inbound"); err != nil {
		t.Fatalf("PersistBridgeHistory: %v", err)
	}
	var count int64
	db.Model(&model.MessageHub{}).
		Where("platform = ? AND conversation_id = ? AND md5(content) = md5(?)", platform, conv, content).
		Count(&count)
	if count != 1 {
		t.Errorf("非发生次数形状的外来 id 仍要被内容哈希兜住，实际行数=%d（放宽过头＝回环防护消失）", count)
	}
}
