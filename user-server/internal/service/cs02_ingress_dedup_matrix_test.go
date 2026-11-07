// cs02_ingress_dedup_matrix_test.go CS-02 入站去重矩阵的补格用例（真实 PG 8217 受控变量）。
//
// CS-01/既有电池已钉住矩阵的大多数格（同 event_id 单条/批次/人工锁重放、occurrence
// 后缀、平台 ID、self-echo 各 reason、跨会话不互抑、落库失败释放、history 仅留存）。
// 本文件只补两类缺口，每格按「实际识别 reason + 行数 + 触发次数」三件套断言：
//
//	① 不同 event_id 同内容（窗口重复）：既有用例只断 queued=false + 留痕行存在，
//	   没断 reason 的实际识别值与 AI 触发次数——补齐。
//	② 「不删除他调用占的键」：落库失败释放只许退回**本次自己占到**的那把键
//	   （IngressDecision.DedupKey 的口径）。被判重复的那次没有占新坑，它走完整
//	   入口（判重+留痕落库）之后，第一占坑者的键必须原样存活——否则第三帧会被
//	   误放行、同一句话得到第二次 AI 回复。
//
// cs02- 前缀专用键隔离惯例沿用 cs01（内容窗口键在进程级缓存里，nonce 每测独立）。
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

// TestCS02_DupWindow_ReasonRowAndTriggerCount 不同 event_id 同内容（内容窗口重复）三件套：
//
//	首帧：accepted+queued、AI 触发 1 次、库内 1 行；
//	次帧（新 event_id 同内容同会话）：accepted=true（幂等回执）、queued_for_ai=false，
//	reason 必须识别为 "duplicate(channel+sender+content) within window"（IsDup 分支的
//	实际识别值，不是 self-echo 也不是 msg_id 幂等），库内留痕 1 行（§8.3-18 不销毁记录），
//	AI 触发仍 1 次。
func TestCS02_DupWindow_ReasonRowAndTriggerCount(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)
	ctx := context.Background()

	nonce := fmt.Sprintf("cs02dup-%d", time.Now().UnixNano())
	content := "CS02窗口重复三件套专用：查退款进度 " + nonce
	conv := "conv-cs02-dup-window"

	first, err := svc.HandleIngressMessage(ctx, r23Event(nonce, "evt-cs02-dup-1", conv, content))
	if err != nil {
		t.Fatalf("首帧 HandleIngressMessage: %v", err)
	}
	if !first.Accepted || !first.QueuedForAI {
		t.Fatalf("首帧应 accepted+queued: %+v", first)
	}
	if tr.called != 1 {
		t.Fatalf("首帧 AI 触发应 1 次，实际 %d", tr.called)
	}

	second, err := svc.HandleIngressMessage(ctx, r23Event(nonce, "evt-cs02-dup-2", conv, content))
	if err != nil {
		t.Fatalf("次帧 HandleIngressMessage: %v", err)
	}
	if !second.Accepted {
		t.Errorf("窗口重复帧也应是 accepted=true（留痕回执）: %+v", second)
	}
	if second.QueuedForAI {
		t.Errorf("窗口重复帧不得再触发 AI: %+v", second)
	}
	if !strings.Contains(second.Reason, "duplicate(channel+sender+content) within window") {
		t.Errorf("reason 必须是内容窗口分支的实际识别值，实际 %q", second.Reason)
	}
	if strings.Contains(second.Reason, "self-echo") || strings.Contains(second.Reason, "idempotent skip") {
		t.Errorf("reason 识别错分支（内容窗口≠回声≠msg_id 幂等）: %q", second.Reason)
	}

	var rows int64
	db.Model(&model.MessageHub{}).Where("conversation_id = ?", conv).Count(&rows)
	if rows != 2 {
		t.Errorf("重复留痕后库内应恰 2 行（首帧 1 + 留痕 1），实际 %d", rows)
	}
	var traceRow int64
	db.Model(&model.MessageHub{}).Where("msg_id = ? AND direction = ?", "evt-cs02-dup-2", "inbound").Count(&traceRow)
	if traceRow != 1 {
		t.Errorf("被判重复的帧必须以 inbound 留痕 1 行（§8.3-18），实际 %d", traceRow)
	}
	if tr.called != 1 {
		t.Errorf("窗口重复后 AI 触发应仍为 1 次，实际 %d", tr.called)
	}
}

// TestCS02_DupDecision_DoesNotReleaseFirstCallerKey 「不删除他调用占的键」：
//
//	A 帧占坑并落库成功（键存活）→ B 帧（新 event_id 同内容）被判重复：判定本身必须
//	没有占新坑（decision.DedupKey==""，否则任何 release/补偿路径都会误删 A 的键）；
//	B 走完整入口（判重+留痕落库）之后，C 帧（又一个新 event_id 同内容）必须**仍然**
//	被判窗口重复 —— 这证明 B 的整条路径（含落库成功后的钩子）没有把 A 的键放掉。
//	若键被误放，C 会重新占坑并 queued_for_ai=true，同一句话得到第二次 AI 回复。
func TestCS02_DupDecision_DoesNotReleaseFirstCallerKey(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)
	ctx := context.Background()

	nonce := fmt.Sprintf("cs02hold-%d", time.Now().UnixNano())
	content := "CS02占坑不被他调用删除专用：发票能开吗 " + nonce
	conv := "conv-cs02-hold-key"

	first, err := svc.HandleIngressMessage(ctx, r23Event(nonce, "evt-cs02-hold-1", conv, content))
	if err != nil || !first.QueuedForAI {
		t.Fatalf("A 帧应占坑并触发 AI: %+v err=%v", first, err)
	}

	// 判定对象本身：dup 判定不占坑（DedupKey 空），谁都不许拿着它去删 A 的键。
	decision, derr := svc.interceptInbound(ctx, r23Event(nonce, "evt-cs02-hold-2", conv, content))
	if derr != nil {
		t.Fatalf("interceptInbound(B): %v", derr)
	}
	if !decision.Blocked || !decision.IsDup {
		t.Fatalf("B 帧应被判窗口重复: %+v", decision)
	}
	if decision.DedupKey != "" {
		t.Fatalf("被判重复的那次没有占新坑，DedupKey 必须为空（否则补偿路径会误删首占坑者的键）: %q", decision.DedupKey)
	}

	// B 走完整入口（判重 + 留痕落库，走的就是会被误补偿的那条路）。
	second, err := svc.HandleIngressMessage(ctx, r23Event(nonce, "evt-cs02-hold-2", conv, content))
	if err != nil {
		t.Fatalf("B 帧完整入口: %v", err)
	}
	if second.QueuedForAI || !strings.Contains(second.Reason, "duplicate(channel+sender+content) within window") {
		t.Fatalf("B 帧应判重留痕: %+v", second)
	}

	// C 帧：A 的键必须还在——B 的路径没删它。
	third, err := svc.HandleIngressMessage(ctx, r23Event(nonce, "evt-cs02-hold-3", conv, content))
	if err != nil {
		t.Fatalf("C 帧 HandleIngressMessage: %v", err)
	}
	if third.QueuedForAI {
		t.Errorf("A 的键被 B 的路径误删了（C 重新占坑并排队 AI，同一句话第二次回复）: %+v", third)
	}
	if !strings.Contains(third.Reason, "duplicate(channel+sender+content) within window") {
		t.Errorf("C 帧仍应被窗口键判重: %+v", third)
	}
	if tr.called != 1 {
		t.Errorf("AI 触发应始终 1 次，实际 %d", tr.called)
	}
	var rows int64
	db.Model(&model.MessageHub{}).Where("conversation_id = ?", conv).Count(&rows)
	if rows != 3 {
		t.Errorf("A+B+C 各留 1 行（首条 + 两条留痕），实际 %d", rows)
	}
}
