package service

import (
	"strings"

	agentcontext "hivemtk-user/internal/aiagent/context"
)

// validatedLLMSessionRef 在把 canonical key 交给 dispatcher 前做同 scope 绑定校验。
// 校验失败时返回空串：下游会按无状态请求处理，绝不让跨 scope 的 key 进入续接链。
func validatedLLMSessionRef(view *agentcontext.AgentConversationContext, ref string) string {
	key := strings.TrimSpace(ref)
	if key == "" || view == nil {
		return ""
	}
	identity, err := agentcontext.DeriveScopeIdentity(view.Scope)
	if err != nil {
		return ""
	}
	binding := agentcontext.SessionBinding{
		ScopeHash:       identity.ScopeHash,
		BusLLMSessionID: identity.BusLLMSessionID,
	}
	if err := agentcontext.ValidateSessionBinding(binding, view.Scope, key); err != nil {
		return ""
	}
	return key
}
