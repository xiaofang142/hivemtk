package service

import (
	"os"
	"strconv"
)

// brain_reliability.go — 产品级常量与预算（BRAIN_PRODUCT_SPEC P0-4/P1-1）。

// brainMaxActionFails Brain 轮内连续动作失败上限（超限强制终止，防 0 进展死循环）。
// 对标 browser-use max_failures → 强制只留 done 工具终止。
const brainMaxActionFails = 5

// brainTokenBudget session 级 token 预算（plan+judge 全计入，超限熔断）。
// 默认 200k：约等于 40 轮 × 每轮 4k 输出 + 快照输入的保守上限；env BRAIN_TOKEN_BUDGET 可调。
func brainTokenBudget() int {
	if v := os.Getenv("BRAIN_TOKEN_BUDGET"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 200_000
}
