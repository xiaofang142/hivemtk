package model

// RateValue 单个率：分子 / 分母 / 比率三件套一起返。
//
// 三件套一起返不是冗余：AC① 要求"与 DB 手工 SQL 一致"，前端只拿到比率的话，
// 对账的人还得猜分子分母是怎么切的；分子分母摆出来，手工 SQL 按同一口径复算即可比对。
type RateValue struct {
	// Rate 比率（0~1），分母为 0 时为 0。
	Rate float64 `json:"rate"`
	// Numerator 分子。
	Numerator float64 `json:"numerator"`
	// Denominator 分母。
	Denominator float64 `json:"denominator"`
}

// LtcRates LTC 三率（T-P8-05，LTC-29/AC4）。
//
// 口径（与 T-P8-06 SLO 文档同一版，改这里必须同步改文档）：
//   - Closure 闭环完成率 = (won + lost) / 全部商机行。北极星指标，看板首位。
//     cancelled（误建作废）同样计入分母：它进过漏斗，没走完就是没闭环。
//   - Collection 回款率 = Σpayments(confirmed) / Σbills(排除 voided)。
//     作废的应收不该收，分母剔除；冲销掉的回款不算数，分子只认 confirmed。
//   - Overdue 逾期率 = open 且 expected_close_at < now 的商机 / open 商机。
//     没定关单日的（NULL）不算逾期。
type LtcRates struct {
	// Closure 闭环完成率（北极星）。
	Closure RateValue `json:"closure"`
	// Collection 回款率。
	Collection RateValue `json:"collection"`
	// Overdue 逾期率。
	Overdue RateValue `json:"overdue"`
}
