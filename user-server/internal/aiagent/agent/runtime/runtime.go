package agent_runtime

import (
	"context"
	"time"
)

func (r *defaultAgentRuntime) HandleCustomerMessage(ctx context.Context, payload CustomerMessagePayload) (*SalesResponse, error) {
	if r.stopped {
		return nil, ErrRuntimeStopped
	}

	agentCtx, err := r.LoadAgentContext(ctx, payload.ChannelType, payload.AccountID)
	if err != nil {
		return nil, err
	}

	req := &SalesRequest{
		Channel:    payload.ChannelType,
		AccountID:  payload.AccountID,
		CustomerID: payload.CustomerID,
		Content:    payload.Content,
		TraceID:    payload.TraceID,
		Raw:        payload.Raw,
	}

	switch agentCtx.AgentType {
	case "sales":
		if r.salesSales == nil {
			return r.fallbackResponse(payload, agentCtx), nil
		}
		return r.salesSales.HandleWithAgent(ctx, agentCtx, req)
	case "customer_service":
		if r.csBridge == nil {
			return r.fallbackResponse(payload, agentCtx), nil
		}
		return r.csBridge.HandleIncomingWithAgent(ctx, agentCtx, req)
	case "hybrid":
		if r.salesSales != nil {
			resp, err := r.salesSales.HandleWithAgent(ctx, agentCtx, req)
			if err == nil && !resp.HandoffToHuman {
				return resp, nil
			}
		}
		if r.csBridge != nil {
			return r.csBridge.HandleIncomingWithAgent(ctx, agentCtx, req)
		}
		return r.fallbackResponse(payload, agentCtx), nil
	default:
		return r.fallbackResponse(payload, agentCtx), nil
	}
}

func (r *defaultAgentRuntime) LoadAgentContext(ctx context.Context, channelType, accountID string) (*AgentContext, error) {
	if r.loader == nil {
		return &AgentContext{
			AgentID:   0,
			AgentCode: "default",
			Name:      "默认智能体",
			AgentType: "sales",
			LoadedAt:  time.Now(),
			Channel:   channelType,
			AccountID: accountID,
		}, nil
	}

	key := cacheKey{Channel: channelType, AccountID: accountID}

	r.mu.RLock()
	if cached, ok := r.cache[key]; ok {
		if time.Since(cached.cachedAt) < r.cacheTTL {
			r.mu.RUnlock()
			return cached.ctx, nil
		}
	}
	r.mu.RUnlock()

	agentCtx, err := r.loader.LoadByChannelAccount(ctx, channelType, accountID)
	if err != nil {
		return nil, err
	}
	agentCtx.LoadedAt = time.Now()
	agentCtx.Channel = channelType
	agentCtx.AccountID = accountID

	r.mu.Lock()
	r.cache[key] = &cachedContext{ctx: agentCtx, cachedAt: time.Now()}
	r.mu.Unlock()

	return agentCtx, nil
}

func (r *defaultAgentRuntime) RefreshCache(ctx context.Context, agentID uint) error {
	if r.loader != nil {
		if err := r.loader.Invalidate(ctx, agentID); err != nil {
			return err
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for k, c := range r.cache {
		if c.ctx.AgentID == agentID {
			delete(r.cache, k)
		}
	}
	return nil
}

func (r *defaultAgentRuntime) Stop(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = true
	r.cache = nil
	return nil
}

func (r *defaultAgentRuntime) fallbackResponse(payload CustomerMessagePayload, agentCtx *AgentContext) *SalesResponse {
	return &SalesResponse{
		// 「请稍后再试」是压测脚本识别降级回复的锚；「转人工」必须是 service 层
		// 转人工关键词表认识的词（裸「人工」不是），否则客户照做也转不出去。
		ReplyContent: "系统暂不可用，请稍后再试，或回复「转人工」由人工客服为您服务。",
		ReplyType:    "text",
		Confidence:   0,
		AgentID:      agentCtx.AgentID,
		AgentCode:    agentCtx.AgentCode,
		Channel:      payload.ChannelType,
		CustomerID:   payload.CustomerID,
		TraceID:      payload.TraceID,
		StopReason:   "no_engine_bridge",
		Duration:     0,
	}
}

// ErrRuntimeStopped 运行时已停止
var ErrRuntimeStopped = &RuntimeError{Code: "stopped", Message: "agent runtime stopped"}

// RuntimeError 运行时错误
type RuntimeError struct {
	Code    string
	Message string
}

func (e *RuntimeError) Error() string {
	return e.Code + ": " + e.Message
}
