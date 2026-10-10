package app

import (
	"context"
	"time"

	"hivemtk-user/internal/service"
	"hivemtk-user/internal/service/confidence"
	"hivemtk-user/internal/service/humanize"
)

// WireConfidenceConfigParams 注入 confidence 组的参数读取口。
//
// 集中在装配层而不是就地接线的原因同 WireMiscConfigParams：这批点位横跨
// internal/service（fewshot / 意图指标 / 意图重试冷却）与 internal/service/confidence
// （否决规则）、internal/service/humanize（拟人度阈值），后两者是 service 的**下层包**，
// 不能反向 import service 读参数中心（会成环），就地接线只能各造函数变量。
//
// 刻意不接 ctx 参数：provider 是延迟读取的，捕获装配时的 ctx 会让它随请求
// 结束一起失效。统一用 context.Background()。
//
// 返回接线的键名列表，供测试逐条核对——返回空数组等于什么都没接，
// 而"没接"在参数中心页面上看不出来。
func WireConfidenceConfigParams() []string {
	cp := service.GlobalConfigParam()
	bg := context.Background()
	wired := make([]string, 0, 5)

	humanize.SetDefaultThresholdProvider(func() float64 {
		return cp.GetFloat(bg, "confidence", "humanize_default_threshold", humanize.DefaultThreshold)
	})
	wired = append(wired, "confidence.humanize_default_threshold")

	service.SetFewShotMinCosProvider(func() float64 {
		return cp.GetFloat(bg, "confidence", "intent_fewshot_min_cos", service.FewShotMinCosDefault)
	})
	wired = append(wired, "confidence.intent_fewshot_min_cos")

	service.SetWeakTruthMinConfidenceProvider(func() float64 {
		return cp.GetFloat(bg, "confidence", "weak_truth_min_confidence", service.WeakTruthMinConfidence)
	})
	wired = append(wired, "confidence.weak_truth_min_confidence")

	service.SetEmbRetryCooldownProvider(func() time.Duration {
		return cp.GetDuration(bg, "confidence", "emb_retry_cooldown", service.EmbRetryCooldownDefault)
	})
	wired = append(wired, "confidence.emb_retry_cooldown")

	confidence.SetVetoLowRAGThresholdProvider(func() float64 {
		return cp.GetFloat(bg, "confidence", "veto_low_rag_threshold", confidence.DefaultVetoLowRAGThreshold)
	})
	wired = append(wired, "confidence.veto_low_rag_threshold")

	return wired
}
