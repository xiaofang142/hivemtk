package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
)

// TestWebhookIngestAckMatrix 闸 10（G-03）：webhook 入站回执必须与 channelgw
// IngestAck 的状态语义一致，且由单一收口生成；限流/队列满属于可重试，验签失败/
// 解析失败属于明确拒绝，重复落库属于 duplicate。
func TestWebhookIngestAckMatrix(t *testing.T) {
	cases := []struct {
		name   string
		result ReceiveResult
		want   string
	}{
		{"accepted", ReceiveResult{Accepted: true, EventID: "e1"}, WebhookIngestAckAccepted},
		{"duplicate", ReceiveResult{Accepted: true, Duplicate: true, EventID: "e2"}, WebhookIngestAckDuplicate},
		{"rate limited", ReceiveResult{Reason: "rate limited", RateLimit: true, EventID: "e3"}, WebhookIngestAckRetry},
		{"queue full", ReceiveResult{Reason: "queue full", QueueFull: true, EventID: "e4"}, WebhookIngestAckRetry},
		{"verify failed", ReceiveResult{Reason: "signature mismatch", VerifyFail: true, EventID: "e5"}, WebhookIngestAckRejected},
		{"persist error", ReceiveResult{Reason: "persist error", EventID: "e6"}, WebhookIngestAckRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.result
			r.Ack.fillFromResult(&r)
			if r.Ack.Status != tc.want {
				t.Errorf("ack status=%q，期望 %q", r.Ack.Status, tc.want)
			}
			if r.Ack.EventID != r.EventID {
				t.Errorf("ack event_id=%q，期望 %q", r.Ack.EventID, r.EventID)
			}
		})
	}
}

type stubIngressPipeline struct {
	called int
	err    error
}

func (p *stubIngressPipeline) HandleIngressMessage(_ context.Context, _ *model.MessageEvent) error {
	p.called++
	return p.err
}

// TestWebhookIngressPipelineInjection 闸 11（G-03）：webhook 各渠道 handler 必须
// 经可注入的 IngressPipeline，便于把 channelgw pipeline 适配器接到同一条入站链上。
func TestWebhookIngressPipelineInjection(t *testing.T) {
	s := &WebhookService{}
	stub := &stubIngressPipeline{}
	s.SetIngressPipeline(stub)
	err := s.ingressHandler(context.Background()).HandleIngressMessage(context.Background(), &model.MessageEvent{})
	if err != nil {
		t.Fatalf("pipeline 调用失败: %v", err)
	}
	if stub.called != 1 {
		t.Fatalf("pipeline 调用次数=%d，期望 1", stub.called)
	}

	stub.err = errors.New("synthetic pipeline failure")
	if err := s.ingressHandler(context.Background()).HandleIngressMessage(context.Background(), &model.MessageEvent{}); err == nil {
		t.Error("pipeline 错误必须向上传播（门禁/落库失败不能被静默吞掉）")
	}
}
