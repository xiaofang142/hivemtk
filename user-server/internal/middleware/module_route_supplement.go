package middleware

// ModuleRouteSupplement 手写补充的路径模式（**不在生成件里**）
//
// 生成件（module_route_patterns.go）覆盖「人工审过的能力面 → 模块」。
// 这里补的是**那张表天然覆盖不到**的几类：
//
//  1. 没有菜单页面的能力——没有菜单就没有菜单键，
//     能力面表里自然也不会有它。
//  2. 不属于任何页面、纯控制面的接口（模块同步、订单查询）。
//
// 口径：**只加"必须拦"的，不加"不该拦"的**。
// 特别是下面刻意**没有**的三类，它们是买模块的入口，拦了就永远买不了：
//   /api/module-market/*   市场列表与下单
//   /api/modules/my        我的模块
//   /api/llm/*（除 wallet/plans）  商户自带 Key 的模型管理
//
// 维护约定：往这张表加条目时，请在同一处写一句"为什么生成件覆盖不到"，
// 否则三个月后没人分得清它为什么不生成。

var ModuleRouteSupplement = []ModuleRouteRule{
	// ---- llm-cloud：平台模型算力（计量型收费模块）----
	// 为什么不生成：llm-cloud 没有独立菜单页，入口是 LLM 页面的「平台套餐」Tab
	// 与顶部余额条，能力面表按"菜单页消费的口"收录，收不到它。
	// 下单 /api/llm/recharge 刻意**不进本表**：它是购买入口，拦了就死锁。
	{Pattern: "/api/llm/wallet", ModuleKey: "llm-cloud"},
	{Pattern: "/api/llm/wallet/*", ModuleKey: "llm-cloud"},
	{Pattern: "/api/llm/plans", ModuleKey: "llm-cloud"},

	// ---- platform-link：模块下发与我的模块 ----
	// 为什么不生成：纯控制面，无菜单页消费。
	{Pattern: "/api/modules/my", ModuleKey: "platform-link"},
	{Pattern: "/api/modules/sync", ModuleKey: "platform-link"},
	{Pattern: "/api/modules/sync-logs", ModuleKey: "platform-link"},
	{Pattern: "/api/manage/modules/*", ModuleKey: "platform-link"},

	// ---- browser-automation：浏览器自动化双腿（¥99 买断）----
	// 为什么不生成：能力面表未收录 /api/browser-automation 与 /api/egolite 的形状
	// （生成口径的历史缺口），但模块归属早已登记（module_bindings browser-automation
	// 10 节点，含 ego 五页）。不补路径闸 = 多租户模式下双腿免费——买断模块必须拦。
	// 免 JWT 的通道不在本闸射程（GuardModulePath 只挂 auth 组）：
	//   /api/browser/host-ws、/api/egolite/runner-ws、/api/egolite/assets/pull
	//   —— 准入由各自的 bh_/eg_ token 门负责，与模块闸互补。
	{Pattern: "/api/browser-automation", ModuleKey: "browser-automation"},
	{Pattern: "/api/browser-automation/*", ModuleKey: "browser-automation"},
	{Pattern: "/api/egolite", ModuleKey: "browser-automation"},
	{Pattern: "/api/egolite/*", ModuleKey: "browser-automation"},
}

// ModuleRouteAll 生成件 + 补充件（匹配时按此顺序，补充件在前）
//
// 补充件在前是刻意的：它是人工判定"必须拦"的，语义比生成件的推导结果强。
// 两者不会真冲突（生成件里没有这批路径），先放谁都不改变结果 —— 但顺序写死
// 是为了让"以后真冲突了"时的行为可预期，而不是取决于 map 遍历顺序。
func ModuleRouteAll() []ModuleRouteRule {
	out := make([]ModuleRouteRule, 0, len(ModuleRouteSupplement)+len(ModuleRouteRules))
	out = append(out, ModuleRouteSupplement...)
	out = append(out, ModuleRouteRules...)
	return out
}
