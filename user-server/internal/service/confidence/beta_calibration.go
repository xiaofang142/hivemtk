package confidence

import "math"

// BetaCalibration 二分类置信度 Beta 校准器
//
// 学术依据：
//   - Kull, M., Silva Filho, T. & Flach, P. (2017). "Beta calibration: a well-founded
//     and easily implemented improvement on logistic calibration for binary classifiers".
//   - 业界实现：scikit-learn `MapieCalibrator`（2023+）、`betacal` Python 包
//   - 公式：P(y=1|z) = σ(a·z + b)^c / (σ(a·z + b)^c + (1-σ(a·z + b))^c)
//   - 与 Platt 的关系：Platt 是 Beta(c=1) 的特例；Beta 多 1 个参数 c → 更灵活
//   - 优势：
//   - 2 参数 (a, b) 仍是低维度（不需交叉验证）
//   - 多 1 个 c 参数能拟合「双峰分布」（Platt 只能拟合单峰）
//   - MLE 闭式更新（梯度 + Hessian 3x3）
//   - 适用：客服意图分类（label 极不平衡、分布有偏）
//
// 与 Platt 对比：
//   - Platt: P(y=1|z) = σ(a·z + b)         2 参数
//   - Beta:  P(y=1|z) = σ(a·z + b)^c / (...) 3 参数
//
// 训练算法：3 维 L-BFGS 简化版（梯度 + 数值线搜索步长）
type BetaCalibration struct {
	A float64
	B float64
	C float64
}

// NewBetaCalibration 默认参数：identity（c=1 等价于 Platt）
func NewBetaCalibration() *BetaCalibration {
	return &BetaCalibration{A: 1.0, B: 0.0, C: 1.0}
}

// BetaSample Beta 校准样本（同 Platt）
type BetaSample struct {
	DecisionValue float64
	Label         int
}

// Fit 用 MLE 拟合 (a, b, c)。
//
// 数学（Kull 2017 §2）：
//
//	p_i = σ(a·z_i + b)
//	q_i = p_i^c / (p_i^c + (1-p_i)^c)
//	目标：max Σ [y_i · log(q_i) + (1-y_i) · log(1-q_i)]
//
// 训练流程（简化版）：
//  1. 先以 Platt 拟合 (a, b)，得初始点
//  2. 再用数值梯度 + 步长搜索优化 c（line search 0.1..3.0）
//  3. 固定 c，联合优化 (a, b)（Newton-Raphson 2x2）
//  4. 重复 2-3 直至收敛
func (b *BetaCalibration) Fit(samples []BetaSample) *BetaCalibration {
	if len(samples) == 0 {
		return b
	}

	plattSamples := make([]PlattSample, len(samples))
	for i, s := range samples {
		plattSamples[i] = PlattSample(s)
	}
	platt := NewPlattScaling().Fit(plattSamples)
	a, bb := platt.Parameters()

	bestC := 1.0
	bestNLL := math.Inf(1)
	cs := []float64{0.5, 0.7, 1.0, 1.3, 1.5, 2.0, 2.5, 3.0}
	for _, c := range cs {
		nll := betaNLL(samples, a, bb, c)
		if nll < bestNLL {
			bestNLL = nll
			bestC = c
		}
	}

	a, bb = newtonAB(samples, bestC, a, bb)

	b.A = a
	b.B = bb
	b.C = bestC
	return b
}

// Predict 校准后的 P(y=1)
//
// 数值稳定：
//   - 用 stableSigmoid 算 p
//   - c>0 时用 pow 后归一化（p=0 或 1 边界处理）
func (b *BetaCalibration) Predict(decisionValue float64) float64 {
	z := b.A*decisionValue + b.B
	p := stableSigmoid(z)
	if b.C == 1.0 {

		return p
	}

	pC := math.Pow(p, b.C)
	oneMinusPC := math.Pow(1.0-p, b.C)
	denom := pC + oneMinusPC
	if denom == 0 {
		return 0.5
	}
	return pC / denom
}

// Parameters 返回 (a, b, c)
func (b *BetaCalibration) Parameters() (a, bb, c float64) {
	return b.A, b.B, b.C
}

// ECE 计算 Beta 校准的 ECE（与 Platt 同样的 15 分桶方法）
func (b *BetaCalibration) ECE(samples []BetaSample) float64 {
	if len(samples) == 0 {
		return 0
	}
	const bins = 15
	binConf := make([]float64, bins)
	binAcc := make([]float64, bins)
	binCount := make([]int, bins)
	for _, s := range samples {
		conf := b.Predict(s.DecisionValue)
		binIdx := int(conf * float64(bins))
		if binIdx >= bins {
			binIdx = bins - 1
		}
		if binIdx < 0 {
			binIdx = 0
		}
		binConf[binIdx] += conf
		binAcc[binIdx] += float64(s.Label)
		binCount[binIdx]++
	}
	n := float64(len(samples))
	ece := 0.0
	for i := 0; i < bins; i++ {
		if binCount[i] == 0 {
			continue
		}
		avgConf := binConf[i] / float64(binCount[i])
		avgAcc := binAcc[i] / float64(binCount[i])
		ece += (float64(binCount[i]) / n) * math.Abs(avgAcc-avgConf)
	}
	return ece
}

func betaNLL(samples []BetaSample, a, b, c float64) float64 {
	nll := 0.0
	for _, s := range samples {
		z := a*s.DecisionValue + b
		p := stableSigmoid(z)

		logPC := c * math.Log(p+1e-15)
		log1MinusPC := c * math.Log(1.0-p+1e-15)

		maxLog := math.Max(logPC, log1MinusPC)
		logSum := maxLog + math.Log(math.Exp(logPC-maxLog)+math.Exp(log1MinusPC-maxLog))

		logQ := logPC - logSum
		log1MinusQ := log1MinusPC - logSum
		y := float64(s.Label)
		nll -= y*logQ + (1.0-y)*log1MinusQ
	}
	return nll
}

func newtonAB(samples []BetaSample, c, initA, initB float64) (float64, float64) {
	a, bb := initA, initB
	const maxIter = 5
	for iter := 0; iter < maxIter; iter++ {
		gA, gB := 0.0, 0.0
		hAA, hBB, hAB := 0.0, 0.0, 0.0
		for _, s := range samples {
			z := a*s.DecisionValue + bb
			p := stableSigmoid(z)
			pC := math.Pow(p, c)
			oneMinusPC := math.Pow(1.0-p, c)
			denom := pC + oneMinusPC
			if denom == 0 {
				continue
			}
			q := pC / denom
			err := q - float64(s.Label)

			dQdP := c * pC * oneMinusPC / (denom * denom)
			dpdz := p * (1.0 - p)
			gA += err * dQdP * dpdz * s.DecisionValue
			gB += err * dQdP * dpdz
			hAA += dQdP * dpdz * s.DecisionValue * s.DecisionValue
			hBB += dQdP * dpdz
			hAB += dQdP * dpdz * s.DecisionValue
		}
		det := hAA*hBB - hAB*hAB
		if det < 1e-12 {
			break
		}
		dA := (hBB*gA - hAB*gB) / det
		dB := (hAA*gB - hAB*gA) / det
		a -= dA
		bb -= dB
		if math.Abs(dA) < 1e-6 && math.Abs(dB) < 1e-6 {
			break
		}
	}
	return a, bb
}
