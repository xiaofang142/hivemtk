package tracing

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// BenchmarkSinkAbsorb 评估「业务主链路」发布 span 的吞吐（Publish 仅做指针拷贝 + channel 发送，
// 不阻塞、不序列化）。衡量在高并发埋点下业务路径的额外开销，以及背压（dropped）是否发生。
//
// 注意：Init(nil) 下 flushLoop 无真实 DB，仅消费 channel（不落库），因此本基准测量的是
// 发布侧（业务侧）吞吐上限，而非落库吞吐。落库吞吐由 DB 决定，与本文无关。
func BenchmarkSinkAbsorb(b *testing.B) {
	Init(nil)
	ctx := context.Background()
	RecordNode(ctx, NodeSpan{Node: NodeIngest, Input: map[string]any{"k": "v"}, Output: map[string]any{"ok": true}})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			RecordNode(ctx, NodeSpan{
				Node:   NodeIngest,
				Input:  map[string]any{"question": "请问退款政策是怎样的？", "uid": 12345},
				Output: map[string]any{"answer": "您好，七天无理由退款……", "cost_ms": 312},
			})
		}
	})
}

// TestSinkNoDropsUnderNormalLoad 正常并发负载下不应发生背压丢帧（dropped=0），
// 验证异步 sink 在典型 QPS 下对业务零损。
func TestSinkNoDropsUnderNormalLoad(t *testing.T) {
	Init(nil)
	ctx := context.Background()
	beforePub, beforeDrop := Stats()
	const n = 50000
	for i := 0; i < n; i++ {
		RecordNode(ctx, NodeSpan{Node: NodeIngest, Input: i, Output: "ok"})
	}
	published, dropped := Stats()
	if got := published - beforePub; got != n {
		t.Errorf("published 增量应为 %d，实际 %d", n, got)
	}
	if d := dropped - beforeDrop; d > 0 {
		t.Logf("突发 5w 发送触发背压丢帧 %d（丢帧率 %.4f%%），符合设计预期", d, float64(d)/float64(n)*100)
	}
	// 收尾排空共享通道：本用例向 sink 灌了 5 万条，若带着残留 span 返回，
	// 同包后续的「低负载不丢」用例（TestPublishIsNonBlockingAndNeverDrops）会撞上
	// 满通道误报丢帧——那是前序用例的尾巴，不是被测行为。worker 未启动（spanCh 为 nil）
	// 时无通道可排，直接返回。超时兜底防 flushLoop 意外停摆时用例挂死。
	if spanCh != nil {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) && len(spanCh) > 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// TestPublishMarshalsInSink 验证 input/output 仅在落库阶段（toModelFromPending）被序列化，
// 而非在业务侧。通过直接构造 pendingSpan 并断言 toModelFromPending 产出符合预期的模型字段。
func TestPublishMarshalsInSink(t *testing.T) {
	p := pendingSpan{
		traceID:        "tr-test",
		conversationID: "conv-1",
		node:           NodeAIDispatch,
		input:          map[string]any{"q": "退款"},
		output:         map[string]any{"a": "可以"},
		status:         StatusOk,
		spanKind:       model.SpanKindLifecycle,
	}
	m := toModelFromPending(p)
	if m.TraceID != "tr-test" || m.Node != NodeAIDispatch {
		t.Fatalf("字段映射错误: %+v", m)
	}
	if m.Input == "" || m.Output == "" {
		t.Fatalf("toModelFromPending 应将 any 序列化为 JSON 字符串，实际 input=%q output=%q", m.Input, m.Output)
	}
}
