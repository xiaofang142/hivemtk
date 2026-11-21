package service

import (
	"math"
	"math/rand"
	"testing"
)

// 频率派：等样本无差异 → z≈0；明显差异 → 显著
func TestFrequentistStats_Basic(t *testing.T) {
	variants := []VariantCounts{
		{VariantID: 1, VariantName: "control", IsControl: true, TrafficCount: 1000, ConversionCount: 100},
		{VariantID: 2, VariantName: "treat", TrafficCount: 1000, ConversionCount: 100},
		{VariantID: 3, VariantName: "winner", TrafficCount: 1000, ConversionCount: 150},
	}
	res := FrequentistStats(variants)
	if len(res) != 3 {
		t.Fatalf("应返回 3 行, got %d", len(res))
	}
	if math.Abs(res[0].ZScore) > 1e-9 {
		t.Fatalf("对照自身 z 应为 0, got %f", res[0].ZScore)
	}
	if res[1].IsWinner {
		t.Fatal("同率变体不应判胜")
	}
	if !res[2].IsWinner || res[2].PValue >= 0.05 {
		t.Fatalf("150/1000 vs 100/1000 应显著胜出: %+v", res[2])
	}
}

// 贝叶斯：确定性 rng，胜率语义
func TestBayesianTest_Deterministic(t *testing.T) {
	variants := []VariantCounts{
		{VariantID: 1, IsControl: true, TrafficCount: 1000, ConversionCount: 100},
		{VariantID: 2, TrafficCount: 1000, ConversionCount: 200},
		{VariantID: 3, TrafficCount: 1000, ConversionCount: 50},
	}
	res := BayesianTest(variants, 5000, rand.New(rand.NewSource(42)))
	if res[1].ChanceToWin < 0.95 {
		t.Fatalf("2x 转化率变体胜率应>=0.95, got %f", res[1].ChanceToWin)
	}
	if !res[1].RiskBest {
		t.Fatal("高胜率应标 risk_best")
	}
	if res[2].ChanceToWin > 0.05 || !res[2].RiskLose {
		t.Fatalf("半率变体胜率应<=0.05, got %f", res[2].ChanceToWin)
	}

	res2 := BayesianTest(variants, 5000, rand.New(rand.NewSource(42)))
	if res[1].ChanceToWin != res2[1].ChanceToWin {
		t.Fatal("同 seed 蒙特卡洛应确定")
	}
}

// 卡方检验使用上尾概率，零偏差必须为1，已知临界值必须与分布一致。
func TestChiSquareSurvival_KnownProbabilities(t *testing.T) {
	cases := []struct {
		name string
		x    float64
		df   int
		want float64
	}{
		{"零偏差", 0, 1, 1},
		{"单自由度单位偏差", 1, 1, 0.31731050786291415},
		{"百分之五临界值", 3.841458820694124, 1, 0.05},
		{"双自由度", 2, 2, math.Exp(-1)},
		{"千分之一临界值", 13.815510557964274, 2, 0.001},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := chiSquareSurvival(tc.x, tc.df)
			if math.IsNaN(got) || math.Abs(got-tc.want) > 1e-10 {
				t.Fatalf("期望p=%.12g，实际%.12g（chi2=%g,df=%d）", tc.want, got, tc.x, tc.df)
			}
		})
	}
	previous := 1.0
	for _, x := range []float64{0, 0.1, 1, 2, 4, 10, 20} {
		got := chiSquareSurvival(x, 1)
		if got > previous || got < 0 || got > 1 {
			t.Fatalf("上尾概率应随chi2递减且位于[0,1]，chi2=%g，前值%g，实际%g", x, previous, got)
		}
		previous = got
	}
}

// SRM：50/50 期望但 90/10 流量 → 告警
func TestDiagnostics_SRM(t *testing.T) {
	ok := Diagnostics([]VariantCounts{
		{VariantID: 1, IsControl: true, TrafficCount: 500},
		{VariantID: 2, TrafficCount: 500},
	}, 0)
	if !ok.SRMPassed || !ok.MinSampleOK {
		t.Fatalf("均衡实验应通过: %+v", ok)
	}
	bad := Diagnostics([]VariantCounts{
		{VariantID: 1, IsControl: true, TrafficCount: 900},
		{VariantID: 2, TrafficCount: 100},
	}, 3)
	if bad.SRMPassed {
		t.Fatal("90/10 失配应 SRM 告警")
	}
	if bad.MultiExposeOK {
		t.Fatal("多曝光用户应告警")
	}
	if len(bad.Warnings) == 0 {
		t.Fatal("应有告警明细")
	}
}

// 序贯检验：无差异 continue；极强差异 accept_H1
func TestSequentialTest_Verdicts(t *testing.T) {
	even := []VariantCounts{
		{VariantID: 1, IsControl: true, TrafficCount: 1000, ConversionCount: 100},
		{VariantID: 2, TrafficCount: 1000, ConversionCount: 100},
	}
	if v := SequentialTest(even, 0.05); v.Verdict != "continue" {
		t.Fatalf("无差异应 continue, got %+v", v)
	}
	strong := []VariantCounts{
		{VariantID: 1, IsControl: true, TrafficCount: 5000, ConversionCount: 200},
		{VariantID: 2, TrafficCount: 5000, ConversionCount: 800},
	}
	if v := SequentialTest(strong, 0.05); v.Verdict != "accept_H1" {
		t.Fatalf("强差异应 accept_H1, got %+v", v)
	}
}

// CUPED：数据不足退化
func TestCUPED_InsufficientData(t *testing.T) {
	variants := []VariantCounts{
		{VariantID: 1, IsControl: true, TrafficCount: 100, ConversionCount: 10},
		{VariantID: 2, TrafficCount: 100, ConversionCount: 12},
	}
	res := CUPED(variants, []cupedUserMetric{{y: 1, x: 2}, {y: 0, x: 1}})
	if res.Available {
		t.Fatal("样本<30 应不可用")
	}
	if len(res.Variants) != 2 {
		t.Fatal("退化时仍应返回未调整结果")
	}
}
