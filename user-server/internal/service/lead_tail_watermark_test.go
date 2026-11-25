package service

import (
	"testing"
	"time"
)

// LM-09 正例：尾触发保留最新水位，最大等待有确定结果；旧水位后到不覆盖新水位。
func TestLeadTailWatermark_AdvanceAndLateArrival(t *testing.T) {
	st := NewLeadTailWatermark()
	st.NoteEnqueued("a", 1, time.Unix(100, 0))
	st.NoteEnqueued("a", 2, time.Unix(105, 0))
	if st.Watermark("a") != 2 || st.Pending("a") != 2 {
		t.Fatalf("水位/待处理计数错误: %+v", st)
	}
	if !st.ShouldFlush("a", time.Unix(115, 0), 10*time.Second) {
		t.Fatal("达到最大等待应有确定尾判结果")
	}
	st.NoteJudged("a", 1, time.Unix(120, 0))
	st.NoteJudged("a", 2, time.Unix(121, 0))
	st.NoteJudged("a", 1, time.Unix(122, 0)) // 迟到旧任务
	if st.Pending("a") != 0 || st.Watermark("a") != 2 {
		t.Fatalf("重复/迟到判定不得改变新水位: %+v", st)
	}
}

// LM-09 反例：仅入队不算完成；未入队不生成待处理；队列满时仍保留水位请求。
func TestLeadTailWatermark_EnqueueIsNotCompletion(t *testing.T) {
	st := NewLeadTailWatermark()
	if st.Pending("x") != 0 || st.Watermark("x") != 0 {
		t.Fatal("空状态应无待处理与水位")
	}
	st.NoteEnqueued("x", 7, time.Unix(10, 0))
	if !st.HasUnfinished("x") {
		t.Fatal("仅入队必须保持未完成状态，不能像 BridgeLeadWorker 那样即返回完成")
	}
	st.NoteQueueFull("x", 8, time.Unix(11, 0))
	if st.Pending("x") != 2 {
		t.Fatalf("队列满不得丢请求，应保留待处理: %d", st.Pending("x"))
	}
}
