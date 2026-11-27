package bridge

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/service"
)

type ackDispositionRecorder struct {
	calls    int
	platform string
	account  string
	inputs   []service.OutboxAckDispositionInput
	err      error
}

func (r *ackDispositionRecorder) Apply(_ context.Context, platform, account string, inputs []service.OutboxAckDispositionInput) (int, error) {
	r.calls++
	r.platform, r.account = platform, account
	r.inputs = append([]service.OutboxAckDispositionInput(nil), inputs...)
	if r.err != nil {
		return 0, r.err
	}
	return len(inputs), nil
}

func TestApplyAckDispositions_V1DeliveredMappedWithScope(t *testing.T) {
	rec := &ackDispositionRecorder{}
	h := NewBridgeIngestHandler(nil)
	h.SetOutboxAckDisposition(rec)

	h.applyAckDispositions(context.Background(), "douyin", "42",
		&BridgeOutboxAckRequest{MsgIDs: []string{"douyin-out-ack-1"}},
		&service.AckOutboundResult{AckedItemsCount: 1})

	if rec.calls != 1 || rec.platform != "douyin" || rec.account != "42" {
		t.Fatalf("ACK 必须带权威 scope 反查处置: calls=%d platform=%q account=%q", rec.calls, rec.platform, rec.account)
	}
	if len(rec.inputs) != 1 || rec.inputs[0].MsgID != "douyin-out-ack-1" || rec.inputs[0].Status != model.BridgeAckStatusDelivered {
		t.Fatalf("v1 缺省状态按 delivered 处理: %+v", rec.inputs)
	}
}

func TestApplyAckDispositions_V2OnlyDeliveredForwarded(t *testing.T) {
	rec := &ackDispositionRecorder{}
	h := NewBridgeIngestHandler(nil)
	h.SetOutboxAckDisposition(rec)

	h.applyAckDispositions(context.Background(), "douyin", "42", &BridgeOutboxAckRequest{
		Items: []BridgeOutboxAckItem{
			{MsgID: "out-delivered", ConversationID: "conv-1", Status: model.BridgeAckStatusDelivered},
			{MsgID: "out-failed", ConversationID: "conv-1", Status: model.BridgeAckStatusFailed},
			{MsgID: "out-unknown", ConversationID: "conv-1", Status: model.BridgeAckStatusUnknown},
			{MsgID: "out-default", ConversationID: "conv-2"},
		},
	}, &service.AckOutboundResult{})

	if len(rec.inputs) != 2 {
		t.Fatalf("只把真实 delivered ACK 交给处置台账: %+v", rec.inputs)
	}
	if rec.inputs[0].MsgID != "out-delivered" || rec.inputs[0].ConversationID != "conv-1" {
		t.Fatalf("v2 必须携带会话作用域: %+v", rec.inputs[0])
	}
	if rec.inputs[1].MsgID != "out-default" || rec.inputs[1].Status != model.BridgeAckStatusDelivered {
		t.Fatalf("v2 缺省状态按 delivered 处理: %+v", rec.inputs[1])
	}
}

func TestApplyAckDispositions_UnwiredAndFailedAckAreNoOp(t *testing.T) {
	h := NewBridgeIngestHandler(nil)
	// 未注入时不得 panic，也不得改变 ACK 返回。
	h.applyAckDispositions(context.Background(), "douyin", "42", &BridgeOutboxAckRequest{
		MsgIDs: []string{"out-1"}, Status: model.BridgeAckStatusDelivered,
	}, &service.AckOutboundResult{})

	rec := &ackDispositionRecorder{err: errors.New("scoped store unavailable")}
	h.SetOutboxAckDisposition(rec)
	// 处置推进失败只记日志，ACK 结果不被改写（本 helper 不返回错误）。
	h.applyAckDispositions(context.Background(), "douyin", "42", &BridgeOutboxAckRequest{
		MsgIDs: []string{"out-1"}, Status: model.BridgeAckStatusDelivered,
	}, &service.AckOutboundResult{})
	if rec.calls != 1 {
		t.Fatalf("失败也要留下调用记录: %d", rec.calls)
	}
}
