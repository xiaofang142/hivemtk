// 能力面查表：把"这条接口属于哪个菜单能力"从 middleware 的静态仅超管表里独立出来。
//
// 为什么要多这一层：侧栏给谁看（menu.go 的 Roles + 库里的 role_menu_permissions）
// 和接口谁能调（route_policy.go 的 adminOnlyRoutes）是两份**各写各的**授权。
// 两轮实测都撞上过它的对侧：
//   - 菜单藏着、接口放行 ⇒ 任何登录角色直接调 API 就能读到只该超管看的数据；
//   - 菜单给了、接口拦人 ⇒ 页面能打开但满屏 403，症状和"没有菜单"几乎一样。
//
// 本表把后者判掉：一条口的档位由**它所属能力的菜单授予**决定。管理员在角色菜单
// 界面上勾一个节点，这个节点的接口同时开；取消勾选，接口同时关。两处不再是两处。
//
// 数据件 menu_capability_routes.go 由 scripts/menu-route-role-audit.py --emit-go 生成，
// 入选口径写在那份文件的头注释里；这里只放匹配与查询。
package model

import "strings"

// capabilityIndex 是 MenuCapabilities 的一次预处理：按首段分桶，避免每个请求
// 把整表比一遍（表已 100+ 条，且它在每条 auth 路由的中间件里跑）。
type capabilityIndex struct {
	byFirstSegment map[string][]CapabilityOwner
}

// CapabilityOwner 一条能力规则的可读视图（pattern 原文 + 归属菜单 key）。
type CapabilityOwner struct {
	Pattern  string
	MenuKeys []string
}

var capabilityCompiled = buildCapabilityIndex()

func buildCapabilityIndex() capabilityIndex {
	idx := capabilityIndex{byFirstSegment: map[string][]CapabilityOwner{}}
	for _, c := range AllMenuCapabilities() {
		segs := splitPathSegments(c.Pattern)
		if len(segs) == 0 {
			continue
		}
		idx.byFirstSegment[segs[0]] = append(idx.byFirstSegment[segs[0]],
			CapabilityOwner(c))
	}
	return idx
}

func splitPathSegments(p string) []string {
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

// matchCapabilityPattern 段级匹配：`*` 吃恰好一段，段数必须相等。
//
// 与 middleware.route_policy 里的 matchSegments 同形。这里刻意不 import middleware
// （model 被 middleware 自己 import，反向依赖会成环），两处的等价性由
// middleware.TestCapabilityMatcherEquivalenceWithRoutePolicy 逐例钉住——形状等价靠测试保证，
// 不靠"看起来一样"。
func matchCapabilityPattern(pattern, path string) bool {
	ps := splitPathSegments(pattern)
	rs := splitPathSegments(path)
	if len(ps) != len(rs) {
		return false
	}
	for i, seg := range ps {
		if seg == "*" {
			continue
		}
		if seg != rs[i] {
			return false
		}
	}
	return true
}

// CapabilityOwnersFor 返回 path 命中的所有能力规则（未命中为 nil）。
//
// 一条口可以同时属于多个能力（同一族的形状被两个 admin-only 页面共用），
// 调用方按"被授予其中任意一个即放行"判定。
func CapabilityOwnersFor(path string) []CapabilityOwner {
	segs := splitPathSegments(path)
	if len(segs) == 0 {
		return nil
	}
	cands := capabilityCompiled.byFirstSegment[segs[0]]
	if len(cands) == 0 {
		return nil
	}
	var out []CapabilityOwner
	for _, c := range cands {
		if matchCapabilityPattern(c.Pattern, path) {
			out = append(out, c)
		}
	}
	return out
}

// CapabilityPatterns 返回生成件的 (pattern, menuKeys) 视图，供常驻门逐条核对
// "这一行有没有命中真实路由"（写错路径的死行会静默不生效）。
func CapabilityPatterns() [][2][]string {
	all := AllMenuCapabilities()
	out := make([][2][]string, 0, len(all))
	for _, c := range all {
		out = append(out, [2][]string{{c.Pattern}, c.MenuKeys})
	}
	return out
}

// MatchCapabilityPattern 导出段匹配供跨包等价门核对（判"两份实现是否分叉"必须能同时
// 调用两处；只有内部实现时，middleware 包里那份无从对账）。
func MatchCapabilityPattern(pattern, path string) bool {
	return matchCapabilityPattern(pattern, path)
}
