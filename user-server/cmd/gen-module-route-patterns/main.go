package main

// gen-module-route-patterns 从既有能力面表 menu_capability_routes.go 生成模块路由模式。
//
// 为什么复用而不是另建一张表：
//   menu_capability_routes.go 已经是**人工审过 251 条**的「API 路径形状 → 菜单 key」
//   对照表，它回答的正是"这条口属于哪个页面"。模块归属是同一件事的下一跳：
//   path → menu_key → module_key。
//   另建一张表等于让同一个问题有两份人工维护的答案，迟早漂移。
//
// 生成物：middleware/module_route_patterns.go
// 用法（hivemtk/user-server 下）：
//   go run ./cmd/gen-module-route-patterns
//
// 生成件禁止手改：下一条命令会覆盖它，且 middleware 侧的测试会按本表的
// 计数与内容核对。需要新增/调整归属时，改 model/module_bindings.go 里的
// 菜单绑定后重新生成。

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"hivemtk-user/internal/model"
)

func main() {
	type rule struct {
		Pattern   string
		ModuleKey string
	}
	var rules []rule
	seen := map[string]bool{}
	unbound := map[string]int{}

	for _, c := range model.AllMenuCapabilities() {
		// 关键判据：**只有一个**不同模块时才算归属明确。
		//
		// 一条能力面可以同时归多个菜单节点（menu_capability_routes.go 里
		// 有 {opsOverview, salesCockpit} 这种），那是「两个页面的读侧共用这条口」。
		// 此时取第一个（此前的做法）会把 /api/ai/sales-cockpit 判成 core，
		// 而它真正属于 sales-crm —— 闭着眼睛取第一个看起来能跑，实际上给
		// 收费模块开了后门。歧义一律不拦（走未覆盖通道 + 采集）。
		distinct := map[string]bool{}
		for _, k := range c.MenuKeys {
			if owner := model.OwnerOfMenuKey(k); owner != "" {
				distinct[owner] = true
			}
		}
		var mk string
		if len(distinct) == 1 {
			for k := range distinct {
				mk = k
			}
		}
		if mk == "" {
			unbound[c.Pattern]++
			continue
		}
		if seen[c.Pattern] {
			continue
		}
		seen[c.Pattern] = true
		rules = append(rules, rule{Pattern: c.Pattern, ModuleKey: mk})
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Pattern < rules[j].Pattern })

	var b strings.Builder
	b.WriteString(header)
	fmt.Fprintf(&b, "\n// 本文件由 cmd/gen-module-route-patterns 生成（勿手改）。\n")
	fmt.Fprintf(&b, "// 事实源：internal/model/module_bindings.go 的菜单归属 + 本文件头所述生成命令。\n\n")
	b.WriteString("// ModuleRouteRules 生成的对照表\n")
	b.WriteString("var ModuleRouteRules = []ModuleRouteRule{\n")
	for _, r := range rules {
		fmt.Fprintf(&b, "\t{Pattern: %q, ModuleKey: %q},\n", r.Pattern, r.ModuleKey)
	}
	b.WriteString("}\n")

	out := "internal/middleware/module_route_patterns.go"
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "写入失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ 已生成 %s：%d 条路径模式\n", out, len(rules))
	if len(unbound) > 0 {
		fmt.Printf("⚠️  %d 个能力面没解析出模块（其菜单 key 未绑定），已跳过：\n", len(unbound))
		keys := make([]string, 0, len(unbound))
		for k := range unbound {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			if i >= 10 {
				fmt.Printf("   ...还有 %d 条\n", len(keys)-10)
				break
			}
			fmt.Printf("   %s\n", k)
		}
	}
}

const header = `// Package middleware 模块路由模式（功能模块化 · FR-5.3）
//
// 模块闸读这张表把「请求路径 → 模块 key」，再问 ModuleGateService 该模块是否开通。
//
// 为什么不逐条路由挂中间件：仓内 2000+ 条路由跨 37 个文件、且跨模块交叉引用
// （AI 销冠的接口要用客户中心的 service），逐条挂会漏挂且无法测试覆盖。
// 形态复刻同包既有的能力面守卫（route_policy），判据一致、读法一致。
//
// 为什么这张表是**生成件**而不是手写：它由
//   internal/model/menu_capability_routes.go（人工审过的「路径形状 → 菜单 key」
// 251 条能力面）
// 接上
//   internal/model/module_bindings.go（菜单 key → 模块）
// 两跳推导得出。另手写一张「路径 → 模块」的表，等于让"这条口属于哪"这个问题
// 出现两份人工答案，迟早漂移；而这张表的好处是：改菜单归属只需改一处。
package middleware

// ModuleRouteRule 一条路径模式 → 模块
type ModuleRouteRule struct {
	Pattern   string
	ModuleKey string
}

`
