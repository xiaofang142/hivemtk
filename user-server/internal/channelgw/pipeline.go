package channelgw

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/service"
)

// IngressPipeline 渠道入站管道：所有传输（HTTP / WebSocket / webhook 适配器）
// 收敛后的唯一业务入口。
//
// 语义与 InboxIngressService 严格对齐：
//   - IngestBatch：批量入站（按 conversation 分组 + msg_id 去重 + 时序锚点 + 合并 AI 触发）
//   - PersistHistory：历史/回填消息仅落库（不触发 AI）
//   - ClaimOutbound：下发队列权威认领（pending→inflight 原子翻转；超时惰性回收；
//     返回行携带认领租约 claimed_at/push_attempts，供调用方回 ack 时做栅栏断言）
//   - AckOutbound：下发确认（inflight/pending→delivered，幂等；legacy 语义不变）
//   - AckOutboundDetailed：v2 详细 ack（CH-37b 刀2 经 pipeline 暴露，委托既有实现：
//     items 逐项 delivered/failed + conversation scope + per-item error，返回
//     acked/failed/duplicate/not_found/not_in_scope 逐项分类；fence 非 nil 时启用 claim 栅栏）
//
// 抽象为接口的目的：
//  1. 传输层（ws.go / bridge HTTP handler）与 service 层解耦，便于单测注入 fake
//  2. 未来 webhook 类渠道（telegram/whatsapp）迁移到同一网关时无需改动传输层
//
// MD-03 公共契约收窄：签名只出现本地契约类型（pipeline_contract.go）与 ORM 实体，
// 调用者实现/消费本接口不需要 import 根 Service；service 返回类型在下方实现体内
// 一次性映射，唯一权威实现仍是 InboxIngressService。
type IngressPipeline interface {
	IngestBatch(ctx context.Context, events []*model.MessageEvent) (*IngestBatchResult, error)
	PersistHistory(ctx context.Context, ev *model.MessageEvent, direction string) error
	ClaimOutbound(ctx context.Context, channel, accountID string, limit int) ([]*model.MessageHub, error)
	AckOutbound(ctx context.Context, channel, accountID string, msgIDs []string) (int, error)
	AckOutboundDetailed(ctx context.Context, channel, accountID string, msgIDs []string, conversationID, terminalStatus string, perItem map[string]OutboundAckInput, fence AckOutboundClaimFence) (*AckOutboundResult, error)
}

type ingressPipeline struct {
	ingress *service.InboxIngressService
}

// NewPipeline 构造包装 InboxIngressService 的入站管道。ingress 为 nil 时所有方法返回错误。
func NewPipeline(ingress *service.InboxIngressService) IngressPipeline {
	return &ingressPipeline{ingress: ingress}
}

var errPipelineNotConfigured = errors.New("channelgw: ingress pipeline not configured")

func (p *ingressPipeline) IngestBatch(ctx context.Context, events []*model.MessageEvent) (*IngestBatchResult, error) {
	if p.ingress == nil {
		return nil, errPipelineNotConfigured
	}
	res, err := p.ingress.HandleIngressBatch(ctx, events)
	if err != nil {
		return nil, err
	}
	return toIngestBatchResult(res), nil
}

func (p *ingressPipeline) PersistHistory(ctx context.Context, ev *model.MessageEvent, direction string) error {
	if p.ingress == nil {
		return errPipelineNotConfigured
	}
	return p.ingress.PersistBridgeHistory(ctx, ev, direction)
}

func (p *ingressPipeline) ClaimOutbound(ctx context.Context, channel, accountID string, limit int) ([]*model.MessageHub, error) {
	if p.ingress == nil {
		return nil, errPipelineNotConfigured
	}
	return p.ingress.ClaimPendingOutbound(ctx, channel, accountID, limit)
}

func (p *ingressPipeline) AckOutbound(ctx context.Context, channel, accountID string, msgIDs []string) (int, error) {
	if p.ingress == nil {
		return 0, errPipelineNotConfigured
	}
	return p.ingress.AckOutboundDelivered(ctx, channel, accountID, msgIDs)
}

// AckOutboundDetailed v2 详细 ack 委托（CH-37b 刀2）：不改 service 层语义，只加 pipeline 暴露。
func (p *ingressPipeline) AckOutboundDetailed(ctx context.Context, channel, accountID string, msgIDs []string, conversationID, terminalStatus string, perItem map[string]OutboundAckInput, fence AckOutboundClaimFence) (*AckOutboundResult, error) {
	if p.ingress == nil {
		return nil, errPipelineNotConfigured
	}
	res, err := p.ingress.AckOutboundDeliveredDetailedFenced(ctx, channel, accountID, msgIDs, conversationID, terminalStatus, toOutboundAckInputs(perItem), service.AckOutboundClaimFence(fence))
	if err != nil {
		return nil, err
	}
	return toAckOutboundResult(res), nil
}
