package tooluse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"
)

// WhitelistSeedFileEnv 白名单 seed 文件路径（D-2）。
//
// 文件形如：
//
//	{"global":["tool_a"],"agents":{"agent_x":["tool_b"]}}
//
// 未设置 / 文件不存在 ⇒ 静默跳过，不改任何默认策略（尤其不动 defaultAllow）。
const WhitelistSeedFileEnv = "TOOL_PERMISSION_WHITELIST_SEED_FILE"

// WhitelistSeed 是启动期白名单种子。global 走全局白名单，agents 按 AgentID 覆盖式设置。
type WhitelistSeed struct {
	Global []string            `json:"global"`
	Agents map[string][]string `json:"agents"`
}

// WhitelistSeedResult 描述一次 seed 加载实际应用了多少条。
type WhitelistSeedResult struct {
	Loaded      bool
	GlobalTools int
	Agents      int
	AgentTools  int
}

// ApplyWhitelistSeedFile 读取并应用 seed 文件。
//
// 语义按"运营资产"而非"用户输入"设计：
//   - 空路径 / 文件不存在 ⇒ (Loaded=false, nil)，静默跳过（不是错误）；
//   - 读失败 / JSON 非法 ⇒ 返回 error 且**一条都不应用**，调用方保持启动默认；
//   - 应用只调 AddGlobalWhitelist / SetAgentWhitelist，绝不碰 defaultAllow ——
//     种子是"补授权"，不是"改默认策略"。
//
// 如何从 would_deny 报告生成这个文件（二选一里的"文档说明"）：
//
//	curl -s -H "Authorization: Bearer $TOKEN" https://<host>/api/agent/tools/risk \
//	  | jq '{global: [.data.counts[]? | select(.would_deny > 0) | .tool_name], agents: {}}' \
//	  > whitelist-seed.json
//
// 生成结果只是**待评审草稿**：报告按工具聚合，不按 Agent 拆分，所以 global 里是
// "所有出现过 would_deny 的工具"。把确实应当无条件放行的留在 global，其余按 Agent
// 挪进 agents（逐 Agent 归属可从 tool_risk_decision 日志的 agent_id+tool_name 汇总：
//
//	... | jq -r 'select(.event=="tool_risk_decision" and .would_deny==true) | [.agent_id,.tool_name] | @tsv'
//
// 一次 seed 里没出现的 Agent 不会被配置，仍按 defaultAllow 走。
func ApplyWhitelistSeedFile(checker *WhitelistPermissionChecker, path string) (WhitelistSeedResult, error) {
	var res WhitelistSeedResult
	path = strings.TrimSpace(path)
	if checker == nil || path == "" {
		return res, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return res, nil
		}
		return res, fmt.Errorf("read whitelist seed %q: %w", path, err)
	}
	var seed WhitelistSeed
	if err := json.Unmarshal(raw, &seed); err != nil {
		return res, fmt.Errorf("parse whitelist seed %q: %w", path, err)
	}

	seed.Global = nonEmptyTools(seed.Global)
	checker.AddGlobalWhitelist(seed.Global)
	res.GlobalTools = len(seed.Global)
	for agentID, tools := range seed.Agents {
		agentID = strings.TrimSpace(agentID)
		if agentID == "" {
			continue
		}
		tools = nonEmptyTools(tools)
		checker.SetAgentWhitelist(agentID, tools)
		res.Agents++
		res.AgentTools += len(tools)
	}
	res.Loaded = true
	return res, nil
}

// ApplyWhitelistSeedFromEnv 是启动装配用的入口：路径取自 WhitelistSeedFileEnv。
func ApplyWhitelistSeedFromEnv(checker *WhitelistPermissionChecker) (WhitelistSeedResult, error) {
	return ApplyWhitelistSeedFile(checker, os.Getenv(WhitelistSeedFileEnv))
}

// nonEmptyTools 去掉空串（SetAgentWhitelist 遇到空切片会删除该 Agent 的配置，
// 若 list 里全是空串，这里先折成空切片，语义与"没配"一致）。
func nonEmptyTools(tools []string) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// WhitelistPermissionChecker 基于 agent_id 维度的工具白名单权限检查器
type WhitelistPermissionChecker struct {
	mu sync.RWMutex

	agentWhitelist map[string]map[string]bool

	globalWhitelist map[string]bool

	defaultAllow bool
}

// NewWhitelistPermissionChecker 创建白名单权限检查器。
//
// 默认策略由环境变量 TOOL_PERMISSION_DEFAULT_DENY 控制：
//   - 未设置或 != "true"：默认放行（向后兼容，未启用白名单时放行所有工具）
//   - 设置为 "true"：默认拒绝（未配置白名单的 Agent 一律拒绝，纵深防御）
//
// Check 的优先级：Agent 已配置白名单时其白名单为权威（"*" 通配也不绕过，
// 防覆盖式逃逸）；全局白名单始终放行；仅当 Agent 未配置白名单时才回退到 defaultAllow。
//
// D-2 前置条件（切到 default-deny 之前必须同时满足，缺一不要改默认值）：
//  1. 白名单 seed 文件已就位并通过 WhitelistSeedFileEnv 在启动期加载（global/agents
//     两档都按运营真实数据填过），否则一开 default-deny 就是全站工具调用被拒；
//  2. would_deny 统计已落库并累过一轮生产流量：/api/agent/tools/risk 的
//     observations_persisted=true，且 counts 里的 would_deny 分布在开旗前被评审过。
//
// 2026-11-17 修订（审计 P2-3）：出厂收口改走 RiskGate enforce（FF_TOOL_PERMISSION_ENFORCE=block，
// 位于审批门之后、专为"高危需逐条授权"设计的既有机器），不在本层内嵌高危门——
// 权限层在审批门之前，内嵌会卡死人工审批放行链。
func NewWhitelistPermissionChecker() *WhitelistPermissionChecker {
	return &WhitelistPermissionChecker{
		agentWhitelist:  make(map[string]map[string]bool),
		globalWhitelist: make(map[string]bool),
		defaultAllow:    os.Getenv("TOOL_PERMISSION_DEFAULT_DENY") != "true",
	}
}

// Check 实现 PermissionChecker 接口
//
// 2026-11-17 审计 P2-3 收口（部署方明确拍板，替代 D-2 的"暂保持放行"口径）：
// defaultAllow 分支不再是无条件放行——**高危工具（EffectiveRisk=high_write，即
// 出域/不可逆类：真实外发、RAG 语料改写等）默认拒绝**，只读/低写工具照常放行，
// 保证存量智能体业务不塌方；高危工具如需启用走 AI 工具管理显式授权
// （/api/tools/permission，超管面）或 TOOL_PERMISSION_WHITELIST_SEED_FILE。
// 未知工具（注册表查无）fail-closed。
func (c *WhitelistPermissionChecker) Check(ctx context.Context, toolName string, tc *ToolContext) error {
	if c == nil {
		return nil
	}
	agentID := ""
	if tc != nil {
		agentID = tc.AgentID
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if agentID != "" {
		if allowed, ok := c.agentWhitelist[agentID]; ok {
			if allowed[toolName] {
				return nil
			}

			return fmt.Errorf("%w: agent=%s tool=%s not in agent whitelist", ErrPermissionDenied, agentID, toolName)
		}
	}

	if c.globalWhitelist[toolName] {
		return nil
	}

	if tc != nil {
		for _, p := range tc.Permissions {
			if p == "*" {
				return nil
			}
		}
	}

	if c.defaultAllow {
		return nil
	}

	return fmt.Errorf("%w: agent=%s tool=%s (default deny)", ErrPermissionDenied, agentID, toolName)
}

// SetAgentWhitelist 设置指定 Agent 的工具白名单（覆盖式）
//
// 传入空 tools 切片等价于 RemoveAgentWhitelist
func (c *WhitelistPermissionChecker) SetAgentWhitelist(agentID string, tools []string) {
	if c == nil || agentID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(tools) == 0 {
		delete(c.agentWhitelist, agentID)
		return
	}
	set := make(map[string]bool, len(tools))
	for _, t := range tools {
		if t != "" {
			set[t] = true
		}
	}
	c.agentWhitelist[agentID] = set
}

// AddAgentTools 向指定 Agent 的白名单追加工具（增量式）
func (c *WhitelistPermissionChecker) AddAgentTools(agentID string, tools []string) {
	if c == nil || agentID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	set, ok := c.agentWhitelist[agentID]
	if !ok {
		set = make(map[string]bool)
		c.agentWhitelist[agentID] = set
	}
	for _, t := range tools {
		if t != "" {
			set[t] = true
		}
	}
}

// RemoveAgentWhitelist 移除指定 Agent 的白名单配置
//
// 移除后该 Agent 走 defaultAllow 策略
func (c *WhitelistPermissionChecker) RemoveAgentWhitelist(agentID string) {
	if c == nil || agentID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.agentWhitelist, agentID)
}

// AddGlobalWhitelist 向全局白名单追加工具
func (c *WhitelistPermissionChecker) AddGlobalWhitelist(tools []string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range tools {
		if t != "" {
			c.globalWhitelist[t] = true
		}
	}
}

// SetDefaultAllow 设置默认放行策略
//
// true: 未配置的 Agent 放行所有工具（向后兼容，默认值）
// false: 未配置的 Agent 拒绝所有工具（严格模式）
func (c *WhitelistPermissionChecker) SetDefaultAllow(allow bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.defaultAllow = allow
}

// GetDefaultAllow 返回当前默认放行策略
func (c *WhitelistPermissionChecker) GetDefaultAllow() bool {
	if c == nil {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.defaultAllow
}

// ListAgentWhitelist 返回指定 Agent 的白名单工具列表（只读快照）
func (c *WhitelistPermissionChecker) ListAgentWhitelist(agentID string) []string {
	if c == nil || agentID == "" {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	set, ok := c.agentWhitelist[agentID]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	return out
}

// AgentHasTool 查询"这个 Agent 的白名单里有没有这一个工具"，不走任何兜底。
//
// 与 Check 的差别：Check 在白名单未命中时还会看全局白名单、tc.Permissions 里的
// "*" 和 defaultAllow，任何一级命中都放行；而分级判定（RiskVerdict）要的是
// "这个 Agent 被逐条授权过这个工具吗"，兜底出来的"放行"不是授权。
//
// ["*"] 也返回 false：Agent 白名单里写 "*" 是授权层的通配写法，不构成逐条授权，
// 是否为通配由 AgentWildcard 单独观测。
func (c *WhitelistPermissionChecker) AgentHasTool(agentID, toolName string) bool {
	if c == nil || agentID == "" || toolName == "" {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	allowed, ok := c.agentWhitelist[agentID]
	return ok && allowed[toolName]
}

// AgentWhitelistConfigured 该 Agent 是否配置过白名单（配过又清空视为未配置）。
// 未配置与"配了但没包含这个工具"在阻断时是两个后果：
// 前者要给运营补配置，后者是明确的不授权决定。
func (c *WhitelistPermissionChecker) AgentWhitelistConfigured(agentID string) bool {
	if c == nil || agentID == "" {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.agentWhitelist[agentID]
	return ok
}

// ListGlobalWhitelist 返回全局白名单工具列表（只读快照）
func (c *WhitelistPermissionChecker) ListGlobalWhitelist() []string {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.globalWhitelist))
	for t := range c.globalWhitelist {
		out = append(out, t)
	}
	return out
}

// ListConfiguredAgents 返回已配置白名单的 AgentID 列表（只读快照）
func (c *WhitelistPermissionChecker) ListConfiguredAgents() []string {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.agentWhitelist))
	for id := range c.agentWhitelist {
		out = append(out, id)
	}
	return out
}
