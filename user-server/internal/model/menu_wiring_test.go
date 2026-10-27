package model

import (
	"sort"
	"testing"
)

// 这份用例把 Wave 3/4 里「页面/接口都通了，但侧栏没有入口」的接线尾巴钉住。
//
// 为什么单独立一份而不是塞进 module_bindings_test.go：那边守的是
// 「每个菜单节点都有模块归属」，这边守的是「这两个具体入口存在、路径对、角色对」。
// 前者是全局不变量，后者是两条会随交付单消失的验收断言——混在一起，
// 将来删掉某个功能时不容易判断该删哪一条。
//
// 已知历史：2026-10-24 交付 007（可观测看板）与 013（模板/技能市场）时，
// 两条菜单行都只写进了「建议接线点」，页面 URL 能直接打开但侧栏点不到。
// 007 的菜单行与 adminOnly 已在同日收口；013 的菜单行是本轮补的。

// menuNodeWithParent 在注册表里按 key 找节点并回传父节点（根节点父节点为 nil）。
func menuNodeWithParent(key string) (node *SystemMenuNode, parent *SystemMenuNode, hits int) {
	var walk func(ns []SystemMenuNode, p *SystemMenuNode)
	walk = func(ns []SystemMenuNode, p *SystemMenuNode) {
		for i := range ns {
			n := &ns[i]
			if n.Key == key {
				hits++
				if node == nil {
					node, parent = n, p
				}
			}
			walk(n.Children, n)
		}
	}
	walk(SystemMenuTree, nil)
	return node, parent, hits
}

func hasRole(roles []string, want string) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}

// TestMenuWiring_ObservabilityDashboardRow 007 的看板入口必须存在且仍是 admin-only。
//
// 三件事一起断，因为少任何一件都会「看起来有入口但实际用不了/不该看的人看得到」：
//   - 行在：侧栏有入口；
//   - path 对：点进去不是 NotFound（前端路由 /observability-dashboard）；
//   - 角色只含 admin：接口侧 RoutePolicyGuard 已按仅超管收口。
func TestMenuWiring_ObservabilityDashboardRow(t *testing.T) {
	node, _, hits := menuNodeWithParent("observabilityDashboard")
	if hits != 1 {
		t.Fatalf("observabilityDashboard 菜单行应恰好 1 条，实际 %d 条（0=侧栏没有入口，>1=重复登记）", hits)
	}
	if node.Path != "/observability-dashboard" {
		t.Errorf("observabilityDashboard 的 path 是 %q，应为 %q（与前端路由模块对不上会点进 NotFound）",
			node.Path, "/observability-dashboard")
	}
	if len(node.Roles) != 1 || !hasRole(node.Roles, "admin") {
		t.Errorf("observabilityDashboard 的角色是 %v，应恰为 [admin]：看板回的是整租户成本口径，"+
			"接口侧已进仅超管表，菜单若对非 admin 可见就是给人一个必然 403 的入口", node.Roles)
	}
	if owner := OwnerOfMenuKey("observabilityDashboard"); owner != ModuleCore {
		t.Errorf("observabilityDashboard 归属模块是 %q，应为 %q：它是系统基座的只读运维读数，"+
			"不能被某个付费模块的闸关掉", owner, ModuleCore)
	}
}

// TestMenuWiring_TemplateMarketRow 013 的模板/技能市场入口必须存在且路径/角色符合交付建议。
//
// 013 的后端接口是公开橱窗（/api/template-market/* 进了匿名白名单），但侧栏入口
// 按该交付 §4 的建议只给 admin/manager —— 页面是「管理员挑现成能力」的配置面，
// 不是坐席的日常工作台。断言锁定的是这条交付决定，不是接口鉴权档（接口仍公开）。
func TestMenuWiring_TemplateMarketRow(t *testing.T) {
	node, parent, hits := menuNodeWithParent("templateMarket")
	if hits != 1 {
		t.Fatalf("templateMarket 菜单行应恰好 1 条，实际 %d 条（0=侧栏没有入口，>1=重复登记）", hits)
	}
	if node.Path != "/template-market" {
		t.Errorf("templateMarket 的 path 是 %q，应为 %q（前端路由模块 src/router/modules/templateMarket.js 的路径）",
			node.Path, "/template-market")
	}
	if parent == nil {
		t.Fatalf("templateMarket 没有父分组：它应挂在 system 顶级的 templateMarketGroup 下")
	}
	if parent.Key != "templateMarketGroup" {
		t.Errorf("templateMarket 的父分组是 %q，应为 templateMarketGroup", parent.Key)
	}
	if len(parent.Roles) != 2 || !hasRole(parent.Roles, "admin") || !hasRole(parent.Roles, "manager") {
		t.Errorf("templateMarketGroup 的角色是 %v，应恰为 [admin manager]（specs/013/delivery.md §4）", parent.Roles)
	}
	for _, key := range []string{"templateMarketGroup", "templateMarket"} {
		if owner := OwnerOfMenuKey(key); owner != ModulePlatformLink {
			t.Errorf("%s 归属模块是 %q，应为 %q：目录是浏览档、不绑付费权益，"+
				"被付费闸关掉会让用户看不到「有哪些现成能力」", key, owner, ModulePlatformLink)
		}
	}
}

// TestMenuWiring_RegistryKeysAndPathsUnique 菜单注册表的形状自检：key 与叶子 path 都不得重复。
//
// 重复 key 会让角色授予/能力面归属指向不确定的那个节点；重复叶子 path 会让两个菜单项
// 指向同一页，后注册的静默顶掉先注册的（前端 menuRegistry_routes.test.js 也断了一次，
// 这里在 Go 侧留一份：菜单是后端下发的，后端自己先要有这条不变量）。
//
// 只对**叶子**查 path 重复：分组节点可以自带 Path 指向默认子页，与那个子项同 path 是
// 有意设计（browserAutomation / geoTools 都是这个形状），按全部节点查会把设计读成缺陷。
func TestMenuWiring_RegistryKeysAndPathsUnique(t *testing.T) {
	all := FlattenMenuKeys(SystemMenuTree)
	if len(all) < 200 {
		t.Fatalf("菜单注册表只有 %d 个节点，远低于历史规模（200+）：树可能被截断，后面的唯一性断言会真空通过", len(all))
	}

	keySeen := map[string]int{}
	pathSeen := map[string]string{}
	pathNodes, leafCount := 0, 0
	var walk func([]SystemMenuNode)
	walk = func(ns []SystemMenuNode) {
		for i := range ns {
			n := &ns[i]
			keySeen[n.Key]++
			if n.Path != "" {
				pathNodes++
				if len(n.Children) > 0 {
					walk(n.Children)
					continue
				}
				leafCount++
				if prev, ok := pathSeen[n.Path]; ok {
					t.Errorf("叶子菜单 path %q 被 %s 与 %s 重复登记（前者会被后者静默顶掉）",
						n.Path, prev, n.Key)
				} else {
					pathSeen[n.Path] = n.Key
				}
			}
			walk(n.Children)
		}
	}
	walk(SystemMenuTree)

	dups := make([]string, 0, 4)
	for k, n := range keySeen {
		if n > 1 {
			dups = append(dups, k)
		}
	}
	sort.Strings(dups)
	if len(dups) > 0 {
		t.Errorf("菜单 key 重复 %d 个：%v（角色授予/能力面归属会指向不确定的节点）", len(dups), dups)
	}
	t.Logf("菜单注册表 %d 个节点（带 Path %d 个，其中叶子 %d 个、叶子 path 去重后 %d 个），key 全部唯一",
		len(all), pathNodes, leafCount, len(pathSeen))
}

// TestMenuWiring_EgoLiteLegRows ego-lite 腿的 5 个入口必须都在 browserAutomation 下、
// 路径与前端路由模块一致、归属同一付费模块（不新增模块条目）。
//
// 为什么单独立一条：本轮（2026-10-27 收口轮）的验收点就是"页面/接口/菜单三者同时存在"，
// 而历史两次事故（007、013）都是"接口通了、侧栏没有入口"。这里把 5 条一次钉住，
// 其中 egoAssets / egoAssetAdmin 两条是同轮新增，另 3 条此前只有菜单行、语言包缺标题
// （menuRegistry_routes.test.js 当场红了）——路径写错的代价是点进去 NotFound。
func TestMenuWiring_EgoLiteLegRows(t *testing.T) {
	want := map[string]string{
		"egoFlows":      "/browser-automation/ego/flows",
		"egoExecutions": "/browser-automation/ego/executions",
		"egoRunner":     "/browser-automation/ego/runner",
		"egoAssets":     "/browser-automation/ego/assets",
		"egoAssetAdmin": "/browser-automation/ego/assets/admin",
	}
	for key, path := range want {
		node, parent, hits := menuNodeWithParent(key)
		if hits != 1 {
			t.Errorf("%s 菜单行应恰好 1 条，实际 %d 条", key, hits)
			continue
		}
		if node.Path != path {
			t.Errorf("%s 的 path 是 %q，应为 %q（src/router/modules/browserAutomation.js）", key, node.Path, path)
		}
		if parent == nil || parent.Key != "browserAutomation" {
			t.Errorf("%s 的父分组不是 browserAutomation：ego 腿属于同一个付费能力的第二种执行腿", key)
		}
		if owner := OwnerOfMenuKey(key); owner != ModuleBrowserAutomation {
			t.Errorf("%s 归属模块是 %q，应为 %q：ego 腿不新增收费模块条目", key, owner, ModuleBrowserAutomation)
		}
	}
	// 发布台只给 admin（用户端橱窗是 egoAssets，发布即上架、不能开放给坐席）
	node, _, _ := menuNodeWithParent("egoAssetAdmin")
	if node == nil || len(node.Roles) != 1 || !hasRole(node.Roles, "admin") {
		t.Errorf("egoAssetAdmin 的角色应恰为 [admin]，实际 %v", node.Roles)
	}
	node, _, _ = menuNodeWithParent("egoAssets")
	if node == nil || len(node.Roles) != 0 {
		t.Errorf("egoAssets 不应限制角色（继承父节点的 admin/manager），实际 %v", node.Roles)
	}
}
