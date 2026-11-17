package service

import "context"

// 手动低层缓存测试保留原全局门槛；生产编排使用代理实际门槛。
func (o *SmartCSOrchestrator) lookupFAQAnswerCache(ctx context.Context, kbID, promptVersion string, vec []float32, result *HandleResult) (*HandleResult, bool) {
	return o.lookupFAQAnswerCacheAtThreshold(ctx, kbID, promptVersion, vec, result, o.confidenceThreshold)
}
