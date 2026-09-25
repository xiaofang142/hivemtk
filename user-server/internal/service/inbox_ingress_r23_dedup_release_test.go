package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// 本文件收 §8.3 第 9 行的第三个子句：「去重记录要与副作用同事务」。
//
// 入口那把内容窗口键是 SetNX **先占坑**、落库**后执行**的。占而不放会造出一个比原缺陷更难的
// 形态：第一次上报因为任何原因（DB 抖动、字段超长、连接被打断）落库失败 ⇒ HTTP 非 2xx ⇒
// 扩展按重投队列原样再发一次 ⇒ 第二次被自己留下的窗口键判成 duplicate ⇒ 只留痕、不触发 AI。
// 表现还是「客户说了没回」，而这次库里连一条失败的痕迹都没有（第一行根本没写进去）。
//
// 夹具让落库失败的办法是不给生产代码开门缝：往 Extra 里塞一个 json.Marshal 编码不了的值
// （函数），message_hub.extra 列的 JSONMap.Value() 在 INSERT 时才会报错 —— 判定失败点确实
// 落在「占坑之后、写库那一步」，而不是被前面的校验提前挡下（那样这条腿什么也没测）。

// r23PoisonedEvent 造一条「内容窗口会占坑、但落库必失败」的事件。
func r23PoisonedEvent(nonce, eventID, conv, content string) *model.MessageEvent {
	evt := r23Event(nonce, eventID, conv, content)
	evt.Extra["persist_failure_probe"] = func() {}
	return evt
}

// TestHandleIngress_PersistFailureReleasesContentWindow 单条路径：落库失败必须把窗口键放回去，
// 重投的第二次（同内容、新 event_id，此时内容窗口是唯一那张网）要照常入库并排队 AI。
func TestHandleIngress_PersistFailureReleasesContentWindow(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	nonce := fmt.Sprintf("release-%d", time.Now().UnixNano())
	content := "帮我查下快递 " + nonce
	conv := "conv-r23-release-single"

	first, err := svc.HandleIngressMessage(ctx, r23PoisonedEvent(nonce, "evt-r23-release-bad", conv, content))
	if err == nil {
		t.Fatalf("夹具没能让落库失败（第一条竟然写进去了）⇒ 这条腿什么也没测： %+v", first)
	}
	if !strings.Contains(err.Error(), "持久化消息失败") {
		t.Fatalf("失败点不在落库那一步，夹具走的是别的分支：%v", err)
	}

	second, err := svc.HandleIngressMessage(ctx, r23Event(nonce, "evt-r23-release-ok", conv, content))
	if err != nil {
		t.Fatalf("重投第二次 HandleIngressMessage: %v", err)
	}
	if !second.QueuedForAI {
		t.Errorf("§8.3-9(c) 未达成：第一条落库失败后窗口键没被释放，重投的第二次被判重复——"+
			"只留痕不回 AI（Reason=%q）", second.Reason)
	}
	var count int64
	db.Model(&model.MessageHub{}).Where("msg_id = ?", "evt-r23-release-ok").Count(&count)
	if count != 1 {
		t.Errorf("重投的第二次必须入库，实际行数=%d", count)
	}
}

// TestHandleIngressBatch_PersistFailureReleasesContentWindow 批次路径有自己的一份 persist 分支，
// 同一条规则必须两边都成立（漏一边＝批里失败的那条把后面所有重投都毒掉）。
func TestHandleIngressBatch_PersistFailureReleasesContentWindow(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	nonce := fmt.Sprintf("brelease-%d", time.Now().UnixNano())
	content := "帮我查下快递 " + nonce
	conv := "conv-r23-release-batch"

	bad, err := svc.HandleIngressBatch(ctx, []*model.MessageEvent{
		r23PoisonedEvent(nonce, "evt-r23-brelease-bad", conv, content),
	})
	if err != nil {
		t.Fatalf("批次不该整批报错（每条失败折进各自的 PerEvent.Reason）：%v", err)
	}
	// 批次路径有自己的一份语义：某一条落库失败不向上抛 error，只写进那一条的 Reason 并置
	// accepted=false —— HTTP 侧据此回 accepted=false（且 IsDuplicateReason 按前缀白名单判重，
	// 「batch handle error:」不命中），扩展不确认、按重投队列再发一次。前提必须读这里，
	// 读返回值会永远等不到 error，夹具走没走到落库那一步就成了看不见的东西。
	if len(bad.PerEvent) != 1 {
		t.Fatalf("PerEvent 条数 %d ≠ 上报条数 1", len(bad.PerEvent))
	}
	if bad.PerEvent[0].Accepted {
		t.Fatalf("夹具没能让批次落库失败（第一条竟然 accepted）⇒ 这条腿什么也没测")
	}
	if !strings.Contains(bad.PerEvent[0].Reason, "持久化消息失败") {
		t.Fatalf("批次里的失败点不在落库那一步：Reason=%q", bad.PerEvent[0].Reason)
	}

	batch, err := svc.HandleIngressBatch(ctx, []*model.MessageEvent{
		r23Event(nonce, "evt-r23-brelease-ok", conv, content),
	})
	if err != nil {
		t.Fatalf("重投第二次 HandleIngressBatch: %v", err)
	}
	per := batch.PerEvent[0]
	if !per.QueuedForAI {
		t.Errorf("§8.3-9(c) 批次半边未达成：批里失败的那条把窗口键留了下来，重投第二次被判重复（Reason=%q）",
			per.Reason)
	}
}

// TestHandleIngress_PersistSuccessKeepsContentWindow 同一枚刀的反向半边：不许把补偿写成
// 「每次都删」——落库成功的那条必须留下窗口键，否则上游连发两帧同 event_id 的重投就只剩
// msg_id 精确判等一层（那是 §6-1 明确不许独扛的一层）。
func TestHandleIngress_PersistSuccessKeepsContentWindow(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	nonce := fmt.Sprintf("keep-%d", time.Now().UnixNano())
	content := "这个有货吗 " + nonce
	conv := "conv-r23-release-keep"

	if res, err := svc.HandleIngressMessage(ctx, r23Event(nonce, "evt-r23-keep-1", conv, content)); err != nil || !res.QueuedForAI {
		t.Fatalf("首条应入库并排队 AI: %+v err=%v", res, err)
	}
	// 同内容、不同 event_id 的第二帧：内容窗口必须还拦得住（说明占的坑没被误删）。
	second, err := svc.HandleIngressMessage(ctx, r23Event(nonce, "evt-r23-keep-2", conv, content))
	if err != nil {
		t.Fatalf("第二条 HandleIngressMessage: %v", err)
	}
	if second.QueuedForAI {
		t.Errorf("落库成功的那条必须把窗口键留在缓存里，否则同内容重投会回两遍 AI：got %+v", second)
	}
}
