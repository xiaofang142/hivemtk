package service

// brain_reliability.go — 产品级常量与预算（BRAIN_PRODUCT_SPEC P0-4/P1-1）。

// brainMaxActionFails Brain 轮内连续动作失败上限（超限强制终止，防 0 进展死循环）。
// 对标 browser-use max_failures → 强制只留 done 工具终止。
const brainMaxActionFails = 5

// brainTokenBudgetProvider 是 session 级 token 预算的取值口。
// SaaS 配置面改造（B11，2026-11-17）：BRAIN_TOKEN_BUDGET 环境变量退役，改由参数中心
// misc.brain_token_budget 经 provider 注入（装配点 internal/app）。默认 200k：
// 约等于 40 轮 × 每轮 4k 输出 + 快照输入的保守上限。
var brainTokenBudgetProvider = func() int { return 0 }

// SetBrainTokenBudgetProvider 注入预算取值口。传 nil 是空操作而不是复位。
func SetBrainTokenBudgetProvider(fn func() int) {
	if fn != nil {
		brainTokenBudgetProvider = fn
	}
}

func brainTokenBudget() int {
	if n := brainTokenBudgetProvider(); n > 0 {
		return n
	}
	return 200_000
}
