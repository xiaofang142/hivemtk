package model

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// de4396bc 的完整业务叶子集合：排序后的 key+Path 指纹同时覆盖删除、改名和改 URL。
func TestMenuNavigation_AllLeafKeysAndPathsPreserved(t *testing.T) {
	var leaves []string
	var paths []string
	var walk func([]SystemMenuNode)
	walk = func(nodes []SystemMenuNode) {
		for _, n := range nodes {
			if n.Path != "" {
				paths = append(paths, n.Key+"\t"+n.Path)
			}
			if len(n.Children) == 0 && n.Path != "" {
				leaves = append(leaves, n.Key+"\t"+n.Path)
			}
			walk(n.Children)
		}
	}
	walk(SystemMenuTree)
	sort.Strings(leaves)
	got := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(leaves, "\n"))))
	// 2026-11-26：线索发现独立顶级模块，新增 5 个深链叶子 + 1 个带页容器，
	// 原有 206/212 个 key/Path 全部保留（仅改父分组），此处上调计数与指纹。
	const want = "c64f72c68445c9707f50f8263b6e2e905e62a6270806d38639793a86a26ea9f1"
	if len(leaves) != 211 || got != want {
		t.Fatalf("211 个业务叶子的 key/Path 必须完整保留，实际 %d 个 / %s；清单:\n%s", len(leaves), got, strings.Join(leaves, "\n"))
	}
	sort.Strings(paths)
	got = fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(paths, "\n"))))
	const wantPaths = "9c76592c240f7703781f1db9a21178698820d08f9f430202aa00b5c7a505e19b"
	if len(paths) != 217 || got != wantPaths {
		t.Fatalf("含带页目录的 217 个 key/Path 必须完整保留，实际 %d 个 / %s", len(paths), got)
	}
}

// 原角色上界逐 key 固定；渠道归组只允许新增 admin 专用的 channels 容器键。
// T02（2026-11-26）：线索智能体族新增 leadConfigGroup / leadEvidenceGroup 两个
// 纯布局容器（无 Path、不带任何新业务叶子），上界计数与指纹相应 +1/+2；
// 所有原有 key 的角色上界未变，见上方逐 key 断言与叶子路径指纹。
func TestMenuNavigation_AllRoleCeilingsPreserved(t *testing.T) {
	cases := []struct {
		role   string
		count  int
		digest string
	}{
		{SystemRoleCodeAdmin, 258, "a55b387bdcecf341cdff06bbb78449faa41b50bee020b230faba840e52d8c6a7"},
		{SystemRoleCodeCustomerService, 62, "5beb004f7dcb0a3dd681d31c7171cbd46a64d7ae70d61700462bc6a5a2644cad"},
		{SystemRoleCodeStaff, 67, "7536303eadb161bc3b3abfd1130ed120db31b3845bb59ab2f8613b0769804131"},
	}
	for _, tc := range cases {
		var keys []string
		eligible := EligibleMenuKeys(tc.role)
		_, hasChannels := eligible["channels"]
		if hasChannels != (tc.role == SystemRoleCodeAdmin) {
			t.Errorf("channels 容器必须仅对 admin 开放，role=%s eligible=%v", tc.role, hasChannels)
		}
		for key := range eligible {
			if tc.role == SystemRoleCodeAdmin && key == "channels" {
				continue // 独立渠道入口不改变任何原有业务键的角色上界。
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(keys, "\n"))))
		if len(keys) != tc.count || digest != tc.digest {
			t.Errorf("%s 的完整上界必须与重组前相同，实际 %d 个 / %s", tc.role, len(keys), digest)
		}
	}
}

// 渠道入口与原八个业务入口必须直接来自后端注册表。
func TestMenuNavigation_AuthoritativeGroups(t *testing.T) {
	want := []string{"workspace", "customer", "leadDiscovery", "aiAgent", "reach", "knowledge", "geoTools", "analytics", "system"}
	got := make([]string, 0, len(SystemMenuTree))
	for _, n := range SystemMenuTree {
		got = append(got, n.Key)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("后端一级业务分组应为 %v，实际 %v", want, got)
	}
	parents := map[string]string{
		"channelManage": "channels", "channelAccount": "channels", "channelIdentifyCode": "channels",
		"inbox": "workspace", "approvalTask": "workspace", "badCase": "workspace",
		"salesCockpit": "customer", "orderDraft": "customer", "salesWorkbench": "customer", "followups": "customer",
		"agentSalesIntel": "customer", "commerceOperations": "customer",
		"customerSession": "agentPassive", "agentWorkbench": "aiAgent", "automationCenter": "agentWorkbench",
		// T02：线索智能体族的三个功能层——工作台直达，配置/判定归档各归一个子目录。
		"leadMining": "leadDiscovery", "leadMiningAgents": "leadConfigGroup",
		"leadGovernance": "leadConfigGroup", "leadNotifyRoutes": "leadConfigGroup",
		"leadEvidence": "leadEvidenceGroup", "clueList": "leadEvidenceGroup",
		"clueStatistics": "leadEvidenceGroup", "leadArchive": "leadEvidenceGroup",
		"leadScopedList": "leadEvidenceGroup",
		"agentActive": "reach", "systemMaterialLibrary": "reach",
		"faqKb": "knowledge", "knowledgeBase": "knowledge", "sopTemplate": "knowledge", "sopTemplateMarket": "knowledge",
		"sopAgent": "agentSupport", "aiToolManagement": "agentSupport",
		"confidencePanel": "agentPassive", "humanizePanel": "agentPassive", "feedbackLoopPanel": "agentPassive",
		"businessAccountAuth": "teamGroup", "wecomAccount": "permissionGroup", "agentChannel": "permissionGroup",
		"messageHub": "siteConfig", "messageHubDashboard": "siteConfig", "browserAutomation": "system",
		"operationLog": "siteConfig", "securityAudit": "siteConfig", "geoDecisionReport": "geoProd",
	}
	for key, wantParent := range parents {
		node, parent, hits := menuNodeWithParent(key)
		if hits != 1 || node == nil || parent == nil || parent.Key != wantParent {
			t.Errorf("%s 应唯一挂在 %s 下，实际节点=%v 父节点=%v 命中=%d", key, wantParent, node, parent, hits)
		}
	}
	channels, _, hits := menuNodeWithParent("channels")
	if hits != 1 || channels == nil || !reflect.DeepEqual(channels.Roles, []string{SystemRoleCodeAdmin}) || len(channels.Children) != 3 {
		t.Fatalf("渠道容器必须唯一、仅 admin 可见且恰好包含三个既有渠道入口，实际 %+v（命中=%d）", channels, hits)
	}
	node, _, _ := menuNodeWithParent("workspace")
	if len(node.Children) != 3 {
		t.Errorf("工作台只保留收件箱、待办和 Bad Case，实际 %d 项", len(node.Children))
	}
}

// 名称按实际页面内容表达，业务 URL 与授权 key 保持稳定。
func TestMenuNavigation_BusinessLabelsAndPaths(t *testing.T) {
	want := map[string][2]string{
		"customer": {"客户与销售", ""}, "aiAgent": {"客服智能体", ""}, "leadDiscovery": {"线索智能体", ""}, "system": {"系统管理", ""},
		"geoTools": {"GEO 增长", "/geo-tools/keyword-mining"}, "permissionGroup": {"渠道与集成", ""},
		"geoBrandConfig":    {"AI 爬虫访问文件", "/geo-tools/brand-config"},
		"geoConfig":         {"品牌与站点资料", "/geo-tools/config"},
		"geoDecisionReport": {"信源补位任务", "/geo/decision-report"},
		"emailSmtp":         {"发件账号", "/email/smtp"}, "emailInfo": {"邮箱服务商参考", "/email/info"},
		"automationCenter": {"客服自动化", "/system/automation-hub"},
		"geoCrawlerStats":  {"UA 探测记录", "/geo-tools/crawler-stats"},
		"teamGroup":        {"团队与权限", ""}, "siteConfig": {"系统配置与运维", ""},
	}
	for key, expected := range want {
		node, _, hits := menuNodeWithParent(key)
		if hits != 1 || node == nil {
			t.Errorf("%s 应有唯一菜单节点，实际 %d", key, hits)
			continue
		}
		if node.Title != expected[0] || node.Path != expected[1] {
			t.Errorf("%s 应为标题 %q / 路径 %q，实际 %q / %q", key, expected[0], expected[1], node.Title, node.Path)
		}
	}
}

// 这是重组前真实注册表的全部非 admin 叶子上界；迁移目录不能静默放宽或收窄权限。
func TestMenuNavigation_LeafRoleCeilingsPreserved(t *testing.T) {
	wantByRole := map[string]string{
		SystemRoleCodeCustomerService: `agentStatus aiAgentList aiSuggestion approvalTask assetMarket badCase clueList clueStatistics conversionFunnel csatDashboard customReport customer360 customerConsents customerEvent customerSession dashboardScreen dialogueMemory douyinStats emailGuide emailInfo faqKb inbox intentRecognition knowledgeBase kuaishouStats leadMining llmRouting myAssets quickReply ragOverview scriptTemplate scriptTemplateAbStats sessionTag smsDrafts smsJobs smsList syncLog tiktokStats xianyuStats xiaohongshuStats`,
		SystemRoleCodeStaff:           `abExperiment aiAgentList aiProductivity approvalTask assetMarket badCase churnPrediction clueList clueStatistics cohortPath confidencePanel conversionFunnel customReport customer360 customerConsents customerEvent customerJourney customerSession dashboardScreen dialogueMemory douyinStats emailGuide emailInfo faqKb feedbackLoopPanel humanizePanel inbox intentRecognition knowledgeBase kuaishouStats leadMining llmRouting llmUsageReport myAssets ragOverview scriptTemplate scriptTemplateAbStats smsDrafts smsJobs smsList syncLog tagSegmentation tiktokStats userSegment xianyuStats xiaohongshuStats`,
	}
	for role, expected := range wantByRole {
		t.Run(role, func(t *testing.T) {
			eligible := EligibleMenuKeys(role)
			got := []string{}
			var walk func([]SystemMenuNode)
			walk = func(nodes []SystemMenuNode) {
				for _, n := range nodes {
					if n.Path != "" && len(n.Children) == 0 {
						if _, ok := eligible[n.Key]; ok {
							got = append(got, n.Key)
						}
					}
					walk(n.Children)
				}
			}
			walk(SystemMenuTree)
			sort.Strings(got)
			want := strings.Fields(expected)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s 的业务叶子上界发生变化：应为 %v，实际 %v", role, want, got)
			}
		})
	}
	for _, key := range []string{"automationCenter", "systemMaterialLibrary", "messageHub", "messageHubDashboard"} {
		node, _, _ := menuNodeWithParent(key)
		if node == nil || !reflect.DeepEqual(node.Roles, []string{SystemRoleCodeAdmin}) {
			t.Errorf("%s 必须显式保留 admin 限制，实际 %v", key, node)
		}
	}
}

// 基础会话/知识能力不能因位于付费智能体目录下而要求购买智能体或 AI 销冠。
func TestMenuNavigation_BaseModulesRemainReachable(t *testing.T) {
	tree := FilterMenuTreeByModule(SystemMenuTree, func(key string) bool {
		return OwnerModuleCategory(key) == ModuleCategoryBase
	})
	keys := map[string]bool{}
	for _, key := range FlattenMenuKeys(tree) {
		keys[key] = true
	}
	for _, key := range []string{"inbox", "customerSession", "agentStatus", "quickReply", "faqKb", "scriptTemplate", "llmRouting", "automationCenter", "systemMaterialLibrary"} {
		if !keys[key] {
			t.Errorf("只开基础模块时 %s 应可达，目录不得增加购买依赖", key)
		}
	}
	for _, key := range []string{"aiAgentList", "knowledgeBase", "sopTemplate", "reachPipeline", "commerceOperations", "geoTools", "browserAutomation"} {
		if keys[key] {
			t.Errorf("未开通所属付费模块时 %s 不应可达", key)
		}
	}
}

// 带默认页面的目录也只控制自身页面，跨模块后代由各自模块裁剪。
func TestMenuNavigation_ModuleGateDoesNotInheritDirectoryOwner(t *testing.T) {
	tree := []SystemMenuNode{{Key: "aiAgent", Path: "/aiAgent/list", Children: []SystemMenuNode{
		{Key: "aiAgentList", Path: "/aiAgent/list"},
		{Key: "customerSession", Path: "/customerSession/list"},
	}}}
	got := FilterMenuTreeByModule(tree, func(key string) bool { return key == ModuleInbox })
	if len(got) != 1 || got[0].Path != "" || len(got[0].Children) != 1 || got[0].Children[0].Key != "customerSession" {
		t.Fatalf("关闭目录默认页所属模块时应仅保留已开通会话子页并清空默认路径，实际 %v", got)
	}
	if tree[0].Path == "" || len(tree[0].Children) != 2 {
		t.Fatal("模块过滤不得修改权威注册表")
	}
}

// GEO 提升为顶级带页目录后，启用 GEO 即可进入，不再依赖原 analytics 祖先。
func TestMenuNavigation_GEOOnlyDoesNotRequireAnalytics(t *testing.T) {
	tree := FilterMenuTreeByModule(SystemMenuTree, func(key string) bool { return key == ModuleGEO })
	if len(tree) != 1 || tree[0].Key != "geoTools" || tree[0].Path != "/geo-tools/keyword-mining" {
		t.Fatalf("仅开 GEO 时应直接返回 GEO 顶级入口及其默认页，实际 %v", tree)
	}
	keys := FlattenMenuKeys(tree)
	if len(keys) != len(ModuleMenuBindings[ModuleGEO]) {
		t.Fatalf("仅开 GEO 时所有 GEO 节点都应可达，实际 %d，期望 %d", len(keys), len(ModuleMenuBindings[ModuleGEO]))
	}
	for _, key := range keys {
		if owner := OwnerOfMenuKey(key); owner != ModuleGEO {
			t.Errorf("仅开 GEO 时不应包含其他模块节点 %s，实际归属 %s", key, owner)
		}
	}
}

// 对每个模块的真实声明依赖闭包，逐一核对整棵树，目录位置不得创造额外购买条件。
func TestMenuNavigation_ExplicitModuleDependenciesOnly(t *testing.T) {
	for _, module := range ModuleCatalogMeta {
		t.Run(module.Key, func(t *testing.T) {
			enabled := map[string]bool{}
			for _, key := range ClosureOfModules(module.Key) {
				enabled[key] = true
			}
			filtered := FilterMenuTreeByModule(SystemMenuTree, func(key string) bool { return enabled[key] })
			visible := map[string]bool{}
			for _, key := range FlattenMenuKeys(filtered) {
				visible[key] = true
			}
			var walk func([]SystemMenuNode)
			walk = func(nodes []SystemMenuNode) {
				for _, n := range nodes {
					if n.Path != "" && len(n.Children) == 0 && visible[n.Key] != enabled[OwnerOfMenuKey(n.Key)] {
						t.Errorf("模块 %s 的闭包对 %s 的可见性错误：owner=%s visible=%t", module.Key, n.Key, OwnerOfMenuKey(n.Key), visible[n.Key])
					}
					walk(n.Children)
				}
			}
			walk(SystemMenuTree)
		})
	}
}

// 搬菜单不重定义商品：这些业务能力仍由原模块控制，基础能力也不转为付费。
func TestMenuNavigation_MovedLeafModuleOwnersPreserved(t *testing.T) {
	want := map[string]string{
		"salesCockpit": ModuleSalesCRM, "orderDraft": ModuleSalesCRM, "salesWorkbench": ModuleSalesCRM, "followups": ModuleSalesCRM,
		"objectionList": ModuleAISales, "personaList": ModuleAISales, "commerceOperations": ModuleReachSocial,
		"faqKb": ModuleKnowledge, "knowledgeBase": ModuleAISales, "sopTemplate": ModuleAISales, "sopTemplateMarket": ModuleAISales,
		"confidencePanel": ModuleAISales, "humanizePanel": ModuleAISales, "feedbackLoopPanel": ModuleAISales,
		"automationCenter": ModuleCore, "systemMaterialLibrary": ModuleCore, "businessAccountAuth": ModuleCustomer,
		"messageHub": ModuleInbox, "messageHubDashboard": ModuleInbox, "wecomAccount": ModuleIMWecom,
		"customerSession": ModuleInbox, "agentStatus": ModuleInbox, "reachPipeline": ModuleReachAuto,
		"geoDecisionReport": ModuleGEO, "browserAutomation": ModuleBrowserAutomation,
	}
	for key, expected := range want {
		if owner := OwnerOfMenuKey(key); owner != expected {
			t.Errorf("%s 应仍归属 %s，实际 %s", key, expected, owner)
		}
	}
}
