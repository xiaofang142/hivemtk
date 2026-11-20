package agent_runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"hivemtk-user/internal/aiagent/agent/delivery"
	"hivemtk-user/internal/pkg/tenant"
)

func executionTestContext() context.Context { return tenant.WithTenantID(context.Background(), 773) }
func executionPayload() CustomerMessagePayload {
	p := testPayload()
	p.MessageID = "test-message"
	p.AccountID = "test-account"
	return p
}

type executionGatewayFunc func(context.Context, delivery.Intent) (*delivery.Record, error)

func (f executionGatewayFunc) Submit(ctx context.Context, i delivery.Intent) (*delivery.Record, error) {
	return f(ctx, i)
}
func (f executionGatewayFunc) Get(context.Context, string) (*delivery.Record, error) {
	return nil, delivery.ErrNotFound
}
func (f executionGatewayFunc) Retry(context.Context, string) (*delivery.Record, error) {
	return nil, delivery.ErrNotFound
}

func TestInferenceDeliveryRuntimeEvidenceBoundaries(t *testing.T) {
	for _, state := range []delivery.State{delivery.Intended, delivery.Blocked, delivery.Sending, delivery.Unknown, delivery.Accepted, delivery.Confirmed, delivery.Rejected} {
		t.Run(string(state), func(t *testing.T) {
			orch, pub, _ := executionFixture(t, 0)
			orch.SetDeliveryGateway(executionGatewayFunc(func(ctx context.Context, i delivery.Intent) (*delivery.Record, error) {
				key, _ := delivery.Identity(ctx, i)
				r := &delivery.Record{Key: key, Kind: i.Kind, State: state}
				if state == delivery.Accepted || state == delivery.Confirmed {
					r.AcceptedCount, r.ReceiptID = 1, "receipt"
				}
				if state == delivery.Confirmed {
					r.ConfirmedCount, r.NewConfirmed = 1, true
				}
				return r, nil
			}))
			r, err := orch.ProcessWithMode(executionTestContext(), executionPayload(), nil, ExecutionModeExecution)
			if err != nil || r.Delivery == nil || len(pub.replies) != 0 {
				t.Fatalf("账本状态被绕过：%+v %v", r, err)
			}
			switch state {
			case delivery.Confirmed:
				if r.FinalReply == "" || !r.Publishable || orch.GetStats().DirectReplyCount != 1 {
					t.Fatal("真实确认未计数")
				}
			case delivery.Rejected:
				if r.Outcome != InferenceRejected || r.Publishable || r.FinalReply != "" || orch.GetStats().RejectedCount != 1 {
					t.Fatal("确定拒绝被错误计数")
				}
			default:
				if r.Outcome != InferenceDeliveryPending || r.ExecutionStatus != ExecutionDeliveryPending || r.Publishable || r.FinalReply != "" || orch.GetStats().DirectReplyCount != 0 {
					t.Fatalf("未确认被当作成功发布：%+v", r)
				}
			}
		})
	}
}

func TestInferenceDeliveryRuntimeRejectsInvalidReceipt(t *testing.T) {
	for _, mode := range []string{"nil", "empty_key", "other_key", "other_kind", "missing_receipt", "invalid_counts", "unknown_state", "store_failed"} {
		t.Run(mode, func(t *testing.T) {
			orch, pub, _ := executionFixture(t, 0)
			cause := errors.New("private database failure")
			orch.SetDeliveryGateway(executionGatewayFunc(func(ctx context.Context, i delivery.Intent) (*delivery.Record, error) {
				key, _ := delivery.Identity(ctx, i)
				r := &delivery.Record{Key: key, Kind: i.Kind, State: delivery.Confirmed, AcceptedCount: 1, ConfirmedCount: 1, ReceiptID: "receipt", NewConfirmed: true}
				switch mode {
				case "nil":
					return nil, nil
				case "empty_key":
					r.Key = ""
				case "other_key":
					r.Key = "another-request"
				case "other_kind":
					r.Kind = "handoff"
				case "missing_receipt":
					r.ReceiptID = ""
				case "invalid_counts":
					r.AcceptedCount = 0
				case "unknown_state":
					r.State = "invalid"
				case "store_failed":
					r.State = delivery.Sending
					return r, cause
				}
				return r, nil
			}))
			r, err := orch.ProcessWithMode(executionTestContext(), executionPayload(), nil, ExecutionModeExecution)
			if err == nil || r.Outcome != InferenceFailed || r.Publishable || r.FinalReply != "" || len(pub.replies) != 0 || orch.GetStats().DirectReplyCount != 0 {
				t.Fatalf("非法确认被信任：%+v %v", r, err)
			}
			if mode == "store_failed" && (!errors.Is(err, cause) || r.Delivery == nil || r.Delivery.Key == "") {
				t.Fatal("落库故障失去可查询的意向键或内部错误链")
			}
		})
	}
}

func TestInferenceDeliveryIdentityFailsBeforeTools(t *testing.T) {
	for _, missing := range []string{"tenant", "message", "account"} {
		t.Run(missing, func(t *testing.T) {
			orch, pub, events := executionFixture(t, 2)
			ctx, payload := executionTestContext(), executionPayload()
			switch missing {
			case "tenant":
				ctx = context.Background()
			case "message":
				payload.MessageID = ""
			case "account":
				payload.AccountID = ""
			}
			r, err := orch.ProcessWithMode(ctx, payload, nil, ExecutionModeExecution)
			if err == nil || r.ToolCallCount != 0 || len(*events) != 0 || len(pub.replies) != 0 || r.Publishable {
				t.Fatalf("身份未验证就产生副作用：%+v %v", r, err)
			}
		})
	}
}

// 只用于运行时边界测试的受控回执端口；持久化和恢复另由真实 PostgreSQL 验收。
type executionDeliveryFixture struct {
	mu           sync.Mutex
	records      map[string]delivery.Record
	fingerprints map[string]string
	accept       func(context.Context, delivery.Intent) error
}

func newExecutionDeliveryFixture(accept func(context.Context, delivery.Intent) error) *executionDeliveryFixture {
	return &executionDeliveryFixture{records: map[string]delivery.Record{}, fingerprints: map[string]string{}, accept: accept}
}
func (g *executionDeliveryFixture) Submit(ctx context.Context, intent delivery.Intent) (*delivery.Record, error) {
	key, err := delivery.Identity(ctx, intent)
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if prior, ok := g.records[key]; ok {
		if g.fingerprints[key] != delivery.Fingerprint(intent) {
			return nil, delivery.ErrConflict
		}
		prior.NewAccepted, prior.NewConfirmed = false, false
		return &prior, nil
	}
	if err := g.accept(ctx, intent); err != nil {
		return nil, err
	}
	r := delivery.Record{Key: key, Kind: intent.Kind, State: delivery.Confirmed, SendAttempts: 1, AcceptedCount: 1, ConfirmedCount: 1, ReceiptID: "local-receipt-" + key, NewAccepted: true, NewConfirmed: true}
	g.records[key], g.fingerprints[key] = r, delivery.Fingerprint(intent)
	return &r, nil
}
func (g *executionDeliveryFixture) Get(ctx context.Context, key string) (*delivery.Record, error) {
	if _, ok := tenant.FromContext(ctx); !ok {
		return nil, delivery.ErrTenant
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	r, ok := g.records[key]
	if !ok {
		return nil, delivery.ErrNotFound
	}
	r.NewAccepted, r.NewConfirmed = false, false
	return &r, nil
}
func (g *executionDeliveryFixture) Retry(ctx context.Context, key string) (*delivery.Record, error) {
	return g.Get(ctx, key)
}

// 旧接口真实执行两次同一消息，必须有可查询的持久受理证据且不可重复发布。
func TestInferenceDeliveryRepeatedRequestHasReliableEvidence(t *testing.T) {
	orch, pub, _ := executionFixture(t, 0)
	payload := executionPayload()
	payload.MessageID = "delivery-message-1"
	for attempt := 0; attempt < 2; attempt++ {
		result, err := orch.ProcessWithMode(executionTestContext(), payload, nil, ExecutionModeExecution)
		if err != nil {
			t.Fatal(err)
		}
		blob, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var contract struct {
			Delivery *struct {
				Key   string `json:"key"`
				State string `json:"state"`
			} `json:"delivery"`
		}
		if err := json.Unmarshal(blob, &contract); err != nil {
			t.Fatal(err)
		}
		if contract.Delivery == nil || contract.Delivery.Key == "" || contract.Delivery.State != "confirmed" {
			t.Errorf("没有可靠受理或确认的可查证据：%s", blob)
		}
	}
	if len(pub.replies) != 1 {
		t.Errorf("同一消息重复发布：实际次数=%d", len(pub.replies))
	}
	if orch.GetStats().DirectReplyCount != 1 {
		t.Fatal("重复查询被重复计为确认")
	}
}
