// tool_risk_coverage_test.go T-P3-05 AC①：生产工具集的分级覆盖率。
//
// 为什么在这里枚举 Build* 而不是读全局注册中心：全局注册中心是 sync.Once 单例，
// 测试进程里一旦装配过就再也换不掉（见 tool_executor_wiring.go 的 SetGlobal* 只有
// 路由有测试钩子）。而分级声明就写在 Build* 返回的这批工具上，枚举它们既能拿到
// 完整集合又不需要 DB。装配链本身的风险（包装层洗掉声明）由
// internal/app 的 permissionGuardedTool 转发测试兜住。
//
// 数量断言用运行时求出的总数 + "每个都必须已声明"，不写死 42/45 这类数字：
// 上一个卡的注释里"40→42"就烂在那儿没人改（tool_executor_wiring.go:137）。
package tooluse

import (
	"sort"
	"testing"
)

// allProductionTools 按与 registerAllAgentToolsViaProviders 相同的构成列出生产工具集。
func allProductionTools() []Tool {
	tools := make([]Tool, 0, 48)
	tools = append(tools, BuildReachTools(ReachToolDeps{})...)
	tools = append(tools, BuildPrivateMessageTools(PrivateMessageToolDeps{})...)
	tools = append(tools, BuildCustomerTools(CustomerToolDeps{})...)
	tools = append(tools, BuildKnowledgeTools(KnowledgeToolDeps{})...)
	tools = append(tools, BuildBusinessTools(BusinessToolDeps{})...)
	tools = append(tools, BuildCardTools()...)
	tools = append(tools, BuildBrowserTools(BrowserToolDeps{})...)
	return tools
}

func TestToolRisk_EveryProductionToolDeclaresALevel(t *testing.T) {
	tools := allProductionTools()
	if len(tools) == 0 {
		t.Fatal("生产工具集枚举为空：Build* 改名了，这个覆盖率断言已经不再覆盖任何东西")
	}

	var undeclared, illegal []string
	byLevel := map[ToolRiskLevel]int{}
	for _, tool := range tools {
		level, declared := EffectiveRisk(tool)
		if !declared {
			// 区分两种"未声明"：完全没实现 / 实现了但写了非法字面量。
			// 后者更糟（作者以为分级了），必须单独点名。
			if _, ok := tool.(RiskDeclared); ok {
				illegal = append(illegal, tool.Name())
			} else {
				undeclared = append(undeclared, tool.Name())
			}
			continue
		}
		byLevel[level]++
	}
	sort.Strings(undeclared)
	sort.Strings(illegal)

	if len(undeclared) > 0 || len(illegal) > 0 {
		t.Errorf("%d 个生产工具没有可用分级（未声明=%d 非法字面量=%d），它们会被兜底成 high_write：\n  未声明: %v\n  非法: %v",
			len(undeclared)+len(illegal), len(undeclared), len(illegal), undeclared, illegal)
	}

	// 三档都必须有人：某档为空说明分级退化成两档（或整个维度没在被使用）。
	for _, l := range KnownRiskLevels {
		if byLevel[l] == 0 {
			t.Errorf("生产工具集里没有任何 %s 工具：分级已退化为 %d 档", l, len(KnownRiskLevels)-1)
		}
	}
	sum := byLevel[RiskReadonly] + byLevel[RiskLowWrite] + byLevel[RiskHighWrite]
	if sum != len(tools) {
		t.Errorf("分档计数之和=%d，工具总数=%d：有工具没被计入", sum, len(tools))
	}
	t.Logf("生产工具集 %d 个：readonly=%d low_write=%d high_write=%d",
		len(tools), byLevel[RiskReadonly], byLevel[RiskLowWrite], byLevel[RiskHighWrite])
}

func TestToolRisk_NamesAreUnique(t *testing.T) {
	// 重名会让 registry.Register 静默覆盖，覆盖率断言随之虚高。
	seen := map[string]bool{}
	for _, tool := range allProductionTools() {
		if seen[tool.Name()] {
			t.Errorf("工具名重复：%s", tool.Name())
		}
		seen[tool.Name()] = true
	}
}
