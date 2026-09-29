package feedbackloop

import (
	"math"
	"testing"

	"hivemtk-user/internal/dto"
)

// TestComputeReward_Collection 回款金额缩放（T-P8-02）：
//
//	reward = weight × log10(1+累计 settled)，封顶 collectionRewardCap。
//	1 万给 ≈2.0（与 conversion 同量级），v<=0 给 0（冲销不倒扣）。
func TestComputeReward_Collection(t *testing.T) {
	c := &FeedbackCollector{config: DefaultFeedbackCollectorConfig()}
	weight := c.lookupWeight(dto.FBSignalCollection)
	if !approxEqualF64(weight, 0.5) {
		t.Fatalf("lookupWeight(collection) = %v want 0.5（缩放系数）", weight)
	}
	cases := []struct {
		name  string
		value float64
		want  float64
	}{
		{"一万与conversion同量级", 10000, 0.5 * math.Log10(10001)},
		{"一百", 100, 0.5 * math.Log10(101)},
		{"零", 0, 0},
		{"负数不倒扣", -500, 0},
		{"巨额封顶", 1e12, collectionRewardCap},
	}
	for _, tc := range cases {
		if got := c.computeReward(dto.FBSignalCollection, tc.value, weight); !approxEqualF64(got, tc.want) {
			t.Errorf("%s: reward = %v want %v", tc.name, got, tc.want)
		}
	}
}

// TestComputeReward_CollectionLost 丢单负样本（T-P8-02）：flat -1.0，走 default 分支。
func TestComputeReward_CollectionLost(t *testing.T) {
	c := &FeedbackCollector{config: DefaultFeedbackCollectorConfig()}
	weight := c.lookupWeight(dto.FBSignalCollectionLost)
	if !approxEqualF64(weight, -1.0) {
		t.Fatalf("lookupWeight(collection_lost) = %v want -1.0", weight)
	}
	if got := c.computeReward(dto.FBSignalCollectionLost, 0, weight); !approxEqualF64(got, -1.0) {
		t.Errorf("collection_lost reward = %v want -1.0", got)
	}
}
