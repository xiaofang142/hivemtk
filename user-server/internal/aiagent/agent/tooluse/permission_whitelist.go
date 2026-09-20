package tooluse

import (
	"context"
	"fmt"
	"os"
	"sync"
)

// WhitelistPermissionChecker 基于 agent_id 维度的工具白名单权限检查器
type WhitelistPermissionChecker struct {
	mu sync.RWMutex

	agentWhitelist map[string]map[string]bool

	globalWhitelist map[string]bool

	defaultAllow bool
}

// NewWhitelistPermissionChecker 创建白名单权限检查器
//
// 默认策略由环境变量 TOOL_PERMISSION_DEFAULT_DENY 控制：
//   - 未设置或 != "true"：默认放行（向后兼容，未启用白名单时放行所有工具）
//   - 设置为 "true"：默认拒绝（未配置白名单的 Agent 一律拒绝，纵深防御）
//
// 无论默认如何，Check 的优先级保证：Agent 已配置白名单时其白名单为权威（"*" 通配也不绕过，
// 防覆盖式逃逸）；全局白名单始终放行；仅当 Agent 未配置白名单时才回退到 defaultAllow。
func NewWhitelistPermissionChecker() *WhitelistPermissionChecker {
	return &WhitelistPermissionChecker{
		agentWhitelist:  make(map[string]map[string]bool),
		globalWhitelist: make(map[string]bool),
		defaultAllow:    os.Getenv("TOOL_PERMISSION_DEFAULT_DENY") != "true",
	}
}

// Check 实现 PermissionChecker 接口
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
// 与 Check 的差别是这条查询存在的理由：Check 在白名单未命中时还会看全局白名单、
// tc.Permissions 里的 "*" 和 defaultAllow，任何一级命中都返回 nil（放行）。
// 分级判定（RiskVerdict）要的是"这个 Agent 被逐条授权过这个工具吗"，
// 兜底出来的"放行"不是授权。因此这里只查 agentWhitelist 这一张表。
//
// 注意 ["*"] 也返回 false：Agent 白名单里写 "*" 是授权层的通配写法，
// 不构成"reach.sms.send 已被逐条授权"。是否为通配由 AgentWildcard 单独观测。
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
//
// 单独可判是必要的：未配置与"配了但没包含这个工具"在阻断时是两个后果 ——
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
