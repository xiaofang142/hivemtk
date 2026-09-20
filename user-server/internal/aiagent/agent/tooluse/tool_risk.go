// Package tooluse 的工具风险分级（T-P3-05 / G-3）。
//
// 这里只有**元数据与读取口径**，没有任何放行决定：本卡的任务是"先给每个工具定级，
// 并把判定记下来"，转阻断排在 P9。之所以要单独一层，是因为现存的
// WhitelistPermissionChecker 回答的是"这个 Agent 能不能调这个工具"，
// 而 G-3 要回答的是另一个问题："这个工具一旦跑起来会不会出域、能不能撤回"。
// 前者是授权，后者是后果 —— 两者叠在一起才有纵深，混成一个布尔值就两头都说不清。
package tooluse

// ToolRiskLevel 工具后果分级。
//
// 三档的判据是**副作用落在谁的系统中**，不是"看起来危不危险"：
//   - readonly   ：只读，调用前后本系统与外部系统的状态一致。
//   - low_write  ：写，但只写本系统内部状态（客户档案、任务、看板），客户看不见。
//   - high_write ：出域或不可逆 —— 消息真的发给了一个真人、RAG 语料被改写、
//     以用户已登录的浏览器身份操作了第三方平台。这类工具按 C1 的口径必须过 checkpoint。
//
// 未声明（含第三方 Provider 交来的、不认识本约定的工具）一律按 high_write 处理，
// 见 EffectiveRisk。
type ToolRiskLevel string

const (
	RiskReadonly  ToolRiskLevel = "readonly"
	RiskLowWrite  ToolRiskLevel = "low_write"
	RiskHighWrite ToolRiskLevel = "high_write"
)

// KnownRiskLevels 合法分级集合。判定、报告、校验共用这一份，避免第三处字面量漂移。
var KnownRiskLevels = []ToolRiskLevel{RiskReadonly, RiskLowWrite, RiskHighWrite}

// IsValidRiskLevel 精确比对（不做大小写折叠、不 TrimSpace）。
//
// 拼错的字面量必须被判为"非法"而不是"归一后合法"：归一会把 `Readonly` 读成只读，
// 于是"写错了"这件事在数据上完全消失。
func IsValidRiskLevel(l ToolRiskLevel) bool {
	for _, k := range KnownRiskLevels {
		if l == k {
			return true
		}
	}
	return false
}

// RiskDeclared 由声明了自身后果分级的工具实现。
//
// 刻意**不**加进 Tool 接口：加进去等于宣布"第三方工具不升级就注册不了"，
// 而这一卡的目的是观察而非设门槛。未实现者由 EffectiveRisk 兜到 high_write。
type RiskDeclared interface {
	RiskLevel() ToolRiskLevel
}

// DeclaredRisk 透出 t 自己的声明；未声明时返回空串，**不**返回兜底等级。
//
// 包装层（审批门 / DNC / 权限守卫）用它转发。转发的难点不是"把等级传下去"，
// 而是"把没等级这件事传下去"：如果包装层在无内层声明时返回 high_write，
// 报告里就分不清"这个工具真被判成高危"和"包了三层谁都没分级"。
func DeclaredRisk(t Tool) ToolRiskLevel {
	if t == nil {
		return ""
	}
	rd, ok := t.(RiskDeclared)
	if !ok {
		return ""
	}
	return rd.RiskLevel()
}

// EffectiveRisk 返回工具的实际分级，以及它是否自己声明过。
//
// declared 必须单独返回：只有"未声明"这件事在报告里可见，P9 转阻断前才知道
// 有多少工具是被兜底规则顶上去的 —— 那批工具是评审的重点，不是配好的存量。
func EffectiveRisk(t Tool) (ToolRiskLevel, bool) {
	l := DeclaredRisk(t)
	if !IsValidRiskLevel(l) {
		return RiskHighWrite, false
	}
	return l, true
}
