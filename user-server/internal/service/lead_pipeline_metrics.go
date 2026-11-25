package service

// LeadPipelineMetrics 线索链路分阶段计数。
// 每阶段使用自己的分母，禁止把消息数、任务数、候选数、事件数、投递数混为一谈。
// 比率以基点（1/10000）表示，-1 表示分母为 0「不可判断」，不得写成 0%。
type LeadPipelineMetrics struct {
	MessagesIngest      int64 // 入库消息数
	TasksAnalyzed       int64 // 实际分析任务数
	NoSignal            int64 // 无业务信号
	Failed              int64 // 失败任务
	Candidates          int64 // 候选需求
	ValidLeads          int64 // 有效需求
	EventsSubmitted     int64 // 已提交业务事件
	RecipientDeliveries int64 // 每收件人投递次数
}

func rateBPS(numerator, denominator int64) int64 {
	if denominator <= 0 {
		return -1
	}
	return numerator * 10000 / denominator
}

// AnalysisRateBPS 分析率 = 已分析任务 / 入库消息。
func (m LeadPipelineMetrics) AnalysisRateBPS() int64 {
	return rateBPS(m.TasksAnalyzed, m.MessagesIngest)
}

// ValidRateBPS 有效率 = 有效需求 / 已分析任务（不是入库消息）。
func (m LeadPipelineMetrics) ValidRateBPS() int64 { return rateBPS(m.ValidLeads, m.TasksAnalyzed) }

// DeliveryPerEventBPS 每事件投递 = 投递次数 / 已提交事件。
func (m LeadPipelineMetrics) DeliveryPerEventBPS() int64 {
	return rateBPS(m.RecipientDeliveries, m.EventsSubmitted)
}
