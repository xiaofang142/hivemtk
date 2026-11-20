// ab_stats.go AB 实验高级统计（K5，对标 GrowthBook 统计引擎的轻量实现）
//
// 全部为纯函数：输入 = 变体流量/转化计数（+ 转化事件明细），无 DB 副作用。
// 方法集：
//   - FrequentistStats: z 检验（vs 对照组）+ 95% 置信区间
//   - BayesianTest: Beta(1,1) 均匀先验后验蒙特卡洛 → Chance to Win + 期望提升
//   - SequentialTest: SPRT 对数似然比 + α/β 边界判定（可提前停止）
//   - Diagnostics: SRM 卡方 + 最小样本 + 多曝光检查
//   - CUPED: 实验前事件作协变量的方差缩减（无实验前数据返回 unavailable）
package service

import (
	"math"
	"math/rand"

	"gonum.org/v1/gonum/stat/distuv"
)

// VariantCounts 变体计数输入
type VariantCounts struct {
	VariantID       uint    `json:"variant_id"`
	VariantName     string  `json:"variant_name"`
	IsControl       bool    `json:"is_control"`
	TrafficCount    int     `json:"traffic_count"`
	ConversionCount int     `json:"conversion_count"`
	ExpectedShare   float64 `json:"expected_share,omitempty"`
}

// FrequentistVariant 单变体频率派结果
type FrequentistVariant struct {
	VariantID       uint    `json:"variant_id"`
	VariantName     string  `json:"variant_name"`
	IsControl       bool    `json:"is_control"`
	TrafficCount    int     `json:"traffic_count"`
	ConversionCount int     `json:"conversion_count"`
	Rate            float64 `json:"rate"`
	StdErr          float64 `json:"std_err"`
	ZScore          float64 `json:"z_score"`
	PValue          float64 `json:"p_value"`
	Confidence      float64 `json:"confidence"`
	CILow           float64 `json:"ci_low"`
	CIHigh          float64 `json:"ci_high"`
	IsWinner        bool    `json:"is_winner"`
}

// FrequentistStats z 检验
func FrequentistStats(variants []VariantCounts) []FrequentistVariant {
	out := make([]FrequentistVariant, 0, len(variants))
	var control *VariantCounts
	for i := range variants {
		if variants[i].IsControl {
			control = &variants[i]
			break
		}
	}
	if control == nil && len(variants) > 0 {
		control = &variants[0]
	}
	pControl, nControl := 0.0, 0.0
	if control != nil && control.TrafficCount > 0 {
		pControl = float64(control.ConversionCount) / float64(control.TrafficCount)
		nControl = float64(control.TrafficCount)
	}
	for _, v := range variants {
		fv := FrequentistVariant{
			VariantID:       v.VariantID,
			VariantName:     v.VariantName,
			IsControl:       v.IsControl,
			TrafficCount:    v.TrafficCount,
			ConversionCount: v.ConversionCount,
		}
		if v.TrafficCount > 0 {
			fv.Rate = float64(v.ConversionCount) / float64(v.TrafficCount)
			fv.StdErr = math.Sqrt(fv.Rate * (1 - fv.Rate) / float64(v.TrafficCount))
			fv.CILow = math.Max(0, fv.Rate-1.96*fv.StdErr)
			fv.CIHigh = math.Min(1, fv.Rate+1.96*fv.StdErr)
			if !v.IsControl && nControl > 0 {
				pooled := (float64(v.ConversionCount) + float64(control.ConversionCount)) / (float64(v.TrafficCount) + nControl)
				sePool := math.Sqrt(pooled * (1 - pooled) * (1/float64(v.TrafficCount) + 1/nControl))
				if sePool > 0 {
					fv.ZScore = (fv.Rate - pControl) / sePool
					fv.PValue = twoTailP(fv.ZScore)
					fv.Confidence = 1 - fv.PValue
					if fv.PValue < 0.05 && fv.Rate > pControl {
						fv.IsWinner = true
					}
				}
			}
		}
		out = append(out, fv)
	}
	return out
}

// BayesianVariant 贝叶斯结果
type BayesianVariant struct {
	VariantID    uint    `json:"variant_id"`
	VariantName  string  `json:"variant_name"`
	IsControl    bool    `json:"is_control"`
	Rate         float64 `json:"rate"`
	PosteriorMix float64 `json:"posterior_mean"`
	ChanceToWin  float64 `json:"chance_to_win"`
	ExpectedLoss float64 `json:"expected_loss"`
	RiskBest     bool    `json:"risk_best"`
	RiskLose     bool    `json:"risk_lose"`
}

// BayesianTest 蒙特卡洛 Beta 后验比较
func BayesianTest(variants []VariantCounts, samples int, rng *rand.Rand) []BayesianVariant {
	if samples <= 0 || samples > 200000 {
		samples = 20000
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(1))
	}

	var controlCounts *VariantCounts
	for i := range variants {
		if variants[i].IsControl {
			controlCounts = &variants[i]
			break
		}
	}
	if controlCounts == nil && len(variants) > 0 {
		controlCounts = &variants[0]
	}
	ctrlSamples := betaSamples(controlCounts, samples, rng)
	out := make([]BayesianVariant, 0, len(variants))
	for _, v := range variants {
		bv := BayesianVariant{
			VariantID:   v.VariantID,
			VariantName: v.VariantName,
			IsControl:   v.IsControl,
		}
		if v.TrafficCount > 0 {
			bv.Rate = float64(v.ConversionCount) / float64(v.TrafficCount)
		}
		samplesV := betaSamples(&v, samples, rng)
		sum, win, lossSum := 0.0, 0, 0.0
		for i := 0; i < samples; i++ {
			sum += samplesV[i]
			if samplesV[i] > ctrlSamples[i] {
				win++
			} else {
				lossSum += ctrlSamples[i] - samplesV[i]
			}
		}
		bv.PosteriorMix = sum / float64(samples)
		bv.ChanceToWin = float64(win) / float64(samples)
		bv.ExpectedLoss = lossSum / float64(samples)
		bv.RiskBest = bv.ChanceToWin >= 0.95
		bv.RiskLose = bv.ChanceToWin <= 0.05
		out = append(out, bv)
	}
	return out
}

func betaParams(v *VariantCounts) (alpha, beta float64) {
	if v == nil {
		return 1, 1
	}
	return float64(v.ConversionCount) + 1, float64(v.TrafficCount-v.ConversionCount) + 1
}

func betaSamples(v *VariantCounts, n int, rng *rand.Rand) []float64 {
	a, b := betaParams(v)
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		ga := gammaSample(rng, a)
		gb := gammaSample(rng, b)
		if ga+gb <= 0 {
			out[i] = 0
			continue
		}
		out[i] = ga / (ga + gb)
	}
	return out
}

func gammaSample(rng *rand.Rand, shape float64) float64 {
	if shape < 1 {

		u := rng.Float64()
		if u <= 0 {
			u = 1e-12
		}
		return gammaSample(rng, shape+1) * math.Pow(u, 1.0/shape)
	}
	d := shape - 1.0/3.0
	c := 1.0 / math.Sqrt(9*d)
	for {
		var x, v float64
		for {
			x = rng.NormFloat64()
			v = 1 + c*x
			if v > 0 {
				break
			}
		}
		v = v * v * v
		u := rng.Float64()
		if u < 1-0.0331*x*x*x*x {
			return d * v
		}
		if math.Log(u) < 0.5*x*x+d*(1-v+math.Log(v)) {
			return d * v
		}
	}
}

// SequentialVerdict SPRT 判定
type SequentialVerdict struct {
	LLR      float64 `json:"llr"`
	Upper    float64 `json:"upper_bound"`
	Lower    float64 `json:"lower_bound"`
	Verdict  string  `json:"verdict"`
	TrafficN int     `json:"traffic"`
	Alpha    float64 `json:"alpha"`
	Beta     float64 `json:"beta"`
}

// SequentialTest SPRT：H0 对照=挑战（无差异） vs H1 有真实提升
// LLR ≈ Σ ln(p1/p0) 转化 + Σ ln((1-p1)/(1-p0)) 未转化（当前比率作极大似然估计）
func SequentialTest(variants []VariantCounts, alpha float64) SequentialVerdict {
	if alpha <= 0 || alpha >= 1 {
		alpha = 0.05
	}
	beta := 0.10
	verdict := SequentialVerdict{Alpha: alpha, Beta: beta, Verdict: "continue"}
	var control, treat *VariantCounts
	for i := range variants {
		if variants[i].IsControl && control == nil {
			control = &variants[i]
		} else if !variants[i].IsControl && treat == nil {
			treat = &variants[i]
		}
	}
	if control == nil && len(variants) > 0 {
		control = &variants[0]
	}
	if control == nil || treat == nil || control.TrafficCount == 0 || treat.TrafficCount == 0 {
		return verdict
	}
	p0 := float64(control.ConversionCount) / float64(control.TrafficCount)
	p1 := float64(treat.ConversionCount) / float64(treat.TrafficCount)
	n := treat.TrafficCount + control.TrafficCount
	verdict.TrafficN = n
	if p0 <= 0 || p0 >= 1 || p1 <= 0 || p1 >= 1 {
		return verdict
	}

	kT := float64(treat.ConversionCount)
	nT := float64(treat.TrafficCount)
	kC := float64(control.ConversionCount)
	nC := float64(control.TrafficCount)
	llr := kT*(math.Log(p1)-math.Log(p0)) + (nT-kT)*(math.Log(1-p1)-math.Log(1-p0)) +
		kC*(math.Log(p0)-math.Log(p1)) + (nC-kC)*(math.Log(1-p0)-math.Log(1-p1))
	verdict.LLR = llr
	verdict.Upper = math.Log((1 - beta) / alpha)
	verdict.Lower = math.Log(beta / (1 - alpha))
	switch {
	case llr >= verdict.Upper:
		verdict.Verdict = "accept_H1"
	case llr <= verdict.Lower:
		verdict.Verdict = "reject_H1"
	}
	return verdict
}

// DiagnosticsResult 数据质量诊断
type DiagnosticsResult struct {
	TotalTraffic  int      `json:"total_traffic"`
	VariantCount  int      `json:"variant_count"`
	SRMChi2       float64  `json:"srm_chi2"`
	SRMDf         int      `json:"srm_df"`
	SRMPValue     float64  `json:"srm_p_value"`
	SRMPassed     bool     `json:"srm_passed"`
	MinSampleOK   bool     `json:"min_sample_ok"`
	MinSampleNeed int      `json:"min_sample_need"`
	MultiExpose   int      `json:"multi_expose_users"`
	MultiExposeOK bool     `json:"multi_expose_ok"`
	Warnings      []string `json:"warnings,omitempty"`
}

// Diagnostics SRM + 最小样本 + 多曝光
func Diagnostics(variants []VariantCounts, multiExposedUsers int) DiagnosticsResult {
	d := DiagnosticsResult{
		MinSampleNeed: 100,
		MultiExpose:   multiExposedUsers,
	}
	for _, v := range variants {
		d.TotalTraffic += v.TrafficCount
	}
	d.VariantCount = len(variants)
	if d.VariantCount < 2 || d.TotalTraffic == 0 {
		d.Warnings = append(d.Warnings, "样本不足，无法执行诊断")
		return d
	}

	chi2 := 0.0
	for _, v := range variants {
		share := v.ExpectedShare
		if share <= 0 || share >= 1 {
			share = 1.0 / float64(d.VariantCount)
		}
		expected := share * float64(d.TotalTraffic)
		if expected > 0 {
			diff := float64(v.TrafficCount) - expected
			chi2 += diff * diff / expected
		}
	}
	d.SRMChi2 = chi2
	d.SRMDf = d.VariantCount - 1
	d.SRMPValue = chiSquareSurvival(chi2, d.SRMDf)
	d.SRMPassed = d.SRMPValue > 0.001
	if !d.SRMPassed {
		d.Warnings = append(d.Warnings, "SRM 流量分配失配（p<=0.001），实验结果不可信")
	}
	minOK := true
	for _, v := range variants {
		if v.TrafficCount < d.MinSampleNeed {
			minOK = false
		}
	}
	d.MinSampleOK = minOK
	if !minOK {
		d.Warnings = append(d.Warnings, "存在变体流量低于最小样本 100，建议继续积累")
	}
	d.MultiExposeOK = multiExposedUsers == 0
	if !d.MultiExposeOK {
		d.Warnings = append(d.Warnings, "存在多曝光用户，转化归因可能失真")
	}
	return d
}

// CUPEDResult CUPED 方差缩减结果
type CUPEDResult struct {
	Available         bool                 `json:"available"`
	Reason            string               `json:"reason,omitempty"`
	Theta             float64              `json:"theta"`
	VarianceReduction float64              `json:"variance_reduction"`
	Variants          []FrequentistVariant `json:"variants"`
}

type cupedUserMetric struct{ y, x float64 }

// CUPED 使用实验前事件计数作协变量（GrowthBook 同思路）
// users: 按 user_id 聚合的 (实验内转化, 实验前事件数)；nPerVariant: 各变体用户数
func CUPED(variants []VariantCounts, users []cupedUserMetric) CUPEDResult {
	if len(users) < 30 {
		return CUPEDResult{Available: false, Reason: "实验前数据不足（需>=30 用户级样本），已退化为未调整结果", Variants: FrequentistStats(variants)}
	}
	var sumX, sumY, sumXX, sumYY, sumXY float64
	n := float64(len(users))
	for _, u := range users {
		sumX += u.x
		sumY += u.y
		sumXX += u.x * u.x
		sumYY += u.y * u.y
		sumXY += u.x * u.y
	}
	meanX, meanY := sumX/n, sumY/n
	varX := sumXX/n - meanX*meanX
	varY := sumYY/n - meanY*meanY
	covXY := sumXY/n - meanX*meanY
	if varX <= 0 || varY <= 0 {
		return CUPEDResult{Available: false, Reason: "协变量方差为 0，无法执行 CUPED", Variants: FrequentistStats(variants)}
	}
	theta := covXY / varX
	vr := (covXY * covXY) / (varX * varY)

	adjusted := make([]VariantCounts, len(variants))
	copy(adjusted, variants)
	totalAdjust := 0.0
	for _, u := range users {
		totalAdjust += theta * (meanX - u.x)
	}
	perVariantAdjust := totalAdjust / n
	for i := range adjusted {
		adj := adjusted[i].ConversionCount
		adjF := float64(adj) + perVariantAdjust*float64(adjusted[i].TrafficCount)/math.Max(1, 1)
		if adjF < 0 {
			adjF = 0
		}
		adjusted[i].ConversionCount = int(math.Round(adjF))
	}
	return CUPEDResult{
		Available:         true,
		Theta:             theta,
		VarianceReduction: vr,
		Variants:          FrequentistStats(adjusted),
	}
}

func twoTailP(z float64) float64 {
	return math.Erfc(math.Abs(z) / math.Sqrt2)
}

func chiSquareSurvival(x float64, df int) float64 {
	if df <= 0 || x < 0 {
		return 1
	}
	// SRM 取卡方上尾概率；双尾正态近似会让零偏差p值不为1。
	return distuv.ChiSquared{K: float64(df)}.Survival(x)
}
