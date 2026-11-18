package service

import (
	"context"
	"strconv"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
)

// inbox_ingress_route.go 路由决策接线（specs/024 T304）。
//
// 只做三件事：调引擎、记录命中的规则 id、把 discard 的处置写进审计。
// 引擎本身是纯逻辑（channel_route_engine.go），读规则走仓储的租户条件。

// resolveRoute 返回命中规则（nil = 无规则/未装配，调用方按原路径处理）。
//
// 引擎报错时**放行**而不是丢弃：路由表读不出来属于基础设施故障，
// 让客户消息因为规则表挂了而消失，比多触发一次 AI 更糟（后者看得见、可纠正）。
func (s *InboxIngressService) resolveRoute(ctx context.Context, event *model.MessageEvent, result *InboxIngressResult) *RouteDecision {
	if s == nil || s.routeEngine == nil {
		return nil
	}
	// 租户来源有两处：事件载荷里带的 tenant_id（webhook 解析器写入）、
	// 或请求 ctx 上绑定的租户（管理端/内部调用）。两者都不存在时不读任何规则。
	// 这里补齐而不是让引擎自己去猜：规则表的 tenant_id 是字符串，而 ctx 是 uint。
	if _, ok := event.Extra["tenant_id"]; !ok {
		if tid, has := tenant.FromContext(ctx); has && tid != 0 {
			event.Extra["tenant_id"] = strconv.FormatUint(uint64(tid), 10)
		}
	}
	decision, err := s.routeEngine.Resolve(ctx, event)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("channel", event.Channel).
			Str("event_id", event.EventID).Msg("[Inbox] 路由解析失败，按无规则放行")
		return nil
	}
	if decision == nil {
		return nil
	}
	if result != nil {
		result.RouteRuleID = decision.RuleID
	}
	return decision
}

// recordRouteDiscard 把「被规则丢弃」写成一行平台事件审计。
//
// 为什么不写 operation_logs：那张表语义是「后台人操作」，而这是系统决策；
// 租户键统一走 tenantEventKey（uint / bigint 列），与其余业务表同口径。
func (s *InboxIngressService) recordRouteDiscard(ctx context.Context, event *model.MessageEvent, decision *RouteDecision) {
	if s == nil || s.auditRepo == nil || event == nil || decision == nil {
		return
	}
	accountID := extraString(event, "account_id")
	row := &model.ChannelEventAudit{
		TenantID:       tenantEventKey(ctx, event),
		Channel:        event.Channel,
		AccountID:      accountID,
		ChannelMsgID:   event.EventID,
		EventType:      event.EventType,
		SenderID:       event.SenderID,
		SenderName:     event.SenderName,
		ConversationID: event.ConversationID,
		RouteRuleID:    decision.RuleID,
		Payload: model.JSONMap{
			"decision": "discard",
			"target":   decision.Target,
			"content":  event.Content,
		},
	}
	if peerID, ok := event.Extra["peer_id"]; ok {
		row.Payload["peer_id"] = peerID
	}
	if _, err := s.auditRepo.Record(ctx, row); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("channel", event.Channel).
			Uint("rule_id", decision.RuleID).Msg("[Inbox] 路由丢弃审计写入失败")
	}
}
