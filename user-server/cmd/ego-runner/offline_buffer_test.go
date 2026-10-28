package main

import "testing"

// 断线缓冲（DF-4⑤）：conn 为空时事件行进有界队列不丢审计；溢出丢最老并如实计数；
// drainPending 取走即清零（flushPending 用它做补传源）。
func TestOfflineBufferEnqueueOverflowAndDrain(t *testing.T) {
	r := &runner{} // conn=nil：send 全部走缓冲路径
	for i := 0; i < egoPendingCap+10; i++ {
		r.send(eventLine{Seq: i + 1, Type: "action", Execution: "ego-1-1"})
	}

	evs, dropped := r.drainPending()
	if dropped != 10 {
		t.Fatalf("溢出丢弃数 = %d, want 10", dropped)
	}
	if len(evs) != egoPendingCap {
		t.Fatalf("缓冲长度 = %d, want %d", len(evs), egoPendingCap)
	}
	if evs[0].Seq != 11 {
		t.Fatalf("最老 10 条应被丢弃：首条 seq = %d, want 11", evs[0].Seq)
	}
	if last := evs[len(evs)-1].Seq; last != egoPendingCap+10 {
		t.Fatalf("最新一条应在缓冲尾：尾条 seq = %d, want %d", last, egoPendingCap+10)
	}

	// 取走即清零：flushPending 不会重复补传
	if evs2, d2 := r.drainPending(); len(evs2) != 0 || d2 != 0 {
		t.Fatalf("drain 后应清零：len=%d dropped=%d", len(evs2), d2)
	}
}
