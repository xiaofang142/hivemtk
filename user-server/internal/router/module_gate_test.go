package router

import (
	"net/http"
	"testing"

	"hivemtk-user/internal/model"
)

// TestAdminAlsoSeesModuleGate 超管**也**受模块闸约束。
//
// 这是商业模式的落地点，值得单独钉死：admin 是「本系统内权限最高」，
// 不是「买了所有模块」。如果哪天有人为了「让超管少点几下」把闸在 admin 上短路，
// 整套收费会从那一刻起失效，而界面上看不出任何差别 —— 所有菜单都在、页面都能开，
// 只是没人付过钱。
func TestAdminAlsoSeesModuleGate(t *testing.T) {
	defer model.SetModuleEnabledFilter(nil)

	// 只开基础模块
	opened := map[string]bool{}
	for _, k := range model.BaseModuleKeys() {
		opened[k] = true
	}
	model.SetModuleEnabledFilter(func(mk string) bool {
		if mk == "" {
			return true
		}
		return opened[mk]
	})

	tree := model.FilterMenuTreeByModule(model.SystemMenuTree, model.GetModuleEnabledFilter())

	if hasKey(tree, "geoTools") {
		t.Error("未开通的 geo 模块菜单不应出现在树中（对 admin 也一样）")
	}
	if !hasKey(tree, "systemUser") {
		t.Error("基础模块菜单不应被过滤掉")
	}

	// 开通 geo 后应立刻出现 —— 证明这是"闸"而不是"硬编码的清单"
	opened[model.ModuleGEO] = true
	tree2 := model.FilterMenuTreeByModule(model.SystemMenuTree, model.GetModuleEnabledFilter())
	if !hasKey(tree2, "geoTools") {
		t.Error("已开通的 geo 模块菜单应出现在树中")
	}
}

func hasKey(nodes []model.SystemMenuNode, key string) bool {
	for _, n := range nodes {
		if n.Key == key {
			return true
		}
		if hasKey(n.Children, key) {
			return true
		}
	}
	return false
}

var _ = http.StatusOK
