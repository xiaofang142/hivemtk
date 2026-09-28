package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/dto"
)

func TestT07_ExtractConfidenceNilAgg(t *testing.T) {
	o := NewSmartCSOrchestrator(nil, nil, nil)
	resp := &SalesResponse{Reply: "您好", Polished: true, Audited: true}
	got, dec := o.extractConfidence(context.Background(), resp, "s1", "在吗")

	if got < 0.74 || got > 0.76 {
		t.Errorf("nil agg 应回退启发式 0.75, got %v", got)
	}
	// T-P8-03 加的第二个返回值：走启发式时**没有**决策（没有 signal_id 可指），
	// 坏例那侧必须能区分"决策给的 0.75"与"启发式猜的 0.75"，不许凭空带一个假 signal_id。
	if dec != nil {
		t.Errorf("nil agg 走启发式路径不该有决策，得到 %+v", dec)
	}
}

// 启发式保留：Intent 透传分支不变
func TestT07_FallbackPassthroughIntent(t *testing.T) {
	o := NewSmartCSOrchestrator(nil, nil, nil)
	resp := &SalesResponse{Intent: &dto.RecognizeResult{IntentType: "price_inquiry", Confidence: 0.83}}
	if got := o.fallbackConfidence(resp); got != 0.83 {
		t.Errorf("Intent.Confidence>0 应透传 0.83, got %v", got)
	}
}
