package service

import "testing"

// 分阶段计数必须各用各的分母：消息数≠分析任务数≠候选数≠有效需求数≠投递数。
func TestLeadPipelineMetrics_StageDenominators(t *testing.T) {
	m := LeadPipelineMetrics{
		MessagesIngest:      100,
		TasksAnalyzed:       80,
		NoSignal:            20,
		Failed:              5,
		Candidates:          30,
		ValidLeads:          18,
		EventsSubmitted:     12,
		RecipientDeliveries: 24,
	}
	if m.AnalysisRateBPS() != 8000 {
		t.Fatalf("分析率应按入库消息为分母 80/100=8000bps，实际 %d", m.AnalysisRateBPS())
	}
	if m.ValidRateBPS() != 2250 {
		t.Fatalf("有效率应按已分析任务为分母 18/80=2250bps，实际 %d", m.ValidRateBPS())
	}
	if m.DeliveryPerEventBPS() != 20000 {
		t.Fatalf("每事件投递应按已提交事件为分母 24/12=20000bps，实际 %d", m.DeliveryPerEventBPS())
	}
}

// 零分母必须显式不可判断，不能写 0%。
func TestLeadPipelineMetrics_ZeroDenominator(t *testing.T) {
	m := LeadPipelineMetrics{}
	if m.AnalysisRateBPS() != -1 || m.ValidRateBPS() != -1 || m.DeliveryPerEventBPS() != -1 {
		t.Fatal("零分母应返回 -1 表示不可判断，不得写 0")
	}
}
