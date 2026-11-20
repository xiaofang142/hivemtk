package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/delivery"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

type deliveryPolicyFunc func(context.Context, delivery.Intent) (bool, error)

func (f deliveryPolicyFunc) AuthorizeDelivery(ctx context.Context, i delivery.Intent) (bool, error) {
	return f(ctx, i)
}

// 受控外部信号：模拟按键原子去重的对端，不代表第三方生产送达。
type deliverySignal struct {
	mu                sync.Mutex
	receipts          map[string]delivery.Receipt
	sends, lookups    int
	state             delivery.State
	lost, lookupError bool
	onSend            func()
}

func (s *deliverySignal) Send(_ context.Context, key string, _ delivery.Intent) (delivery.Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sends++
	r, ok := s.receipts[key]
	if !ok {
		r = delivery.Receipt{State: s.state, ID: "signal-" + key}
		s.receipts[key] = r
	}
	if s.onSend != nil {
		s.onSend()
	}
	if s.lost {
		return delivery.Receipt{}, errors.New("private lost receipt")
	}
	return r, nil
}
func (s *deliverySignal) Lookup(_ context.Context, key string, _ delivery.Intent) (delivery.Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lookups++
	if s.lookupError {
		return delivery.Receipt{}, errors.New("private lookup failure")
	}
	if r, ok := s.receipts[key]; ok {
		return r, nil
	}
	return delivery.Receipt{State: delivery.Absent}, nil
}
func deliveryDBFixture(t *testing.T) (*gorm.DB, context.Context, delivery.Intent, *deliverySignal, delivery.Policy) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.InferenceDelivery{})
	ctx := tenant.WithTenantID(context.Background(), 773)
	i := delivery.Intent{MessageID: "message-1", Channel: "local", AccountID: "account-1", CustomerID: "customer-1", SessionID: "session-1", AgentCode: "default", Kind: "reply", Content: "经过最终复核的回复"}
	s := &deliverySignal{receipts: map[string]delivery.Receipt{}, state: delivery.Confirmed}
	return db, ctx, i, s, deliveryPolicyFunc(func(context.Context, delivery.Intent) (bool, error) { return true, nil })
}
func deliveryServiceFor(db *gorm.DB, sender delivery.Sender, policy delivery.Policy) InferenceDeliveryService {
	return NewInferenceDeliveryService(repository.NewInferenceDeliveryRepository(db), sender, policy)
}

func TestInferenceDeliveryPostgresIdempotencyAndRestart(t *testing.T) {
	for _, kind := range []string{"reply", "handoff"} {
		t.Run(kind, func(t *testing.T) {
			db, ctx, i, signal, policy := deliveryDBFixture(t)
			i.Kind = kind
			first, err := deliveryServiceFor(db, signal, policy).Submit(ctx, i)
			if err != nil || first.State != delivery.Confirmed || first.AcceptedCount != 1 || first.ConfirmedCount != 1 || !first.NewConfirmed || first.SendAttempts != 1 {
				t.Fatalf("无可靠确认：%+v %v", first, err)
			}
			// 新 service/repository 实例只依赖 PG 行恢复；不复用原进程内状态。
			restarted := deliveryServiceFor(db, signal, policy)
			for n := 0; n < 3; n++ {
				r, err := restarted.Submit(ctx, i)
				if err != nil || r.Key != first.Key || r.State != delivery.Confirmed || r.NewConfirmed || r.NewAccepted {
					t.Fatalf("重启重复计数：%+v %v", r, err)
				}
			}
			if signal.sends != 1 || signal.lookups != 1 {
				t.Fatalf("重复外发：send=%d lookup=%d", signal.sends, signal.lookups)
			}
			changed := i
			changed.Content = "篡改正文"
			if _, err := restarted.Submit(ctx, changed); !errors.Is(err, delivery.ErrConflict) {
				t.Fatal("同键不同正文被覆盖")
			}
			if _, err := restarted.Get(tenant.WithTenantID(context.Background(), 774), first.Key); !errors.Is(err, delivery.ErrNotFound) {
				t.Fatal("跨租户可读")
			}
			if _, err := restarted.Retry(context.Background(), first.Key); !errors.Is(err, delivery.ErrTenant) {
				t.Fatal("缺租户未关闭")
			}
		})
	}
}

func TestInferenceDeliveryPostgresMissingAndRejected(t *testing.T) {
	for _, mode := range []string{"missing_sender", "missing_policy", "policy_error", "denied", "external_rejected", "lookup_error"} {
		t.Run(mode, func(t *testing.T) {
			db, ctx, i, signal, policy := deliveryDBFixture(t)
			var sender delivery.Sender = signal
			switch mode {
			case "missing_sender":
				sender = nil
			case "missing_policy":
				policy = nil
			case "policy_error":
				policy = deliveryPolicyFunc(func(context.Context, delivery.Intent) (bool, error) { return false, errors.New("private policy") })
			case "denied":
				policy = deliveryPolicyFunc(func(context.Context, delivery.Intent) (bool, error) { return false, nil })
			case "external_rejected":
				signal.state = delivery.Rejected
			case "lookup_error":
				signal.lookupError = true
			}
			svc := deliveryServiceFor(db, sender, policy)
			r, err := svc.Submit(ctx, i)
			want := delivery.Blocked
			if mode == "denied" || mode == "external_rejected" {
				want = delivery.Rejected
			}
			if mode == "lookup_error" {
				want = delivery.Unknown
			}
			if err != nil || r.State != want || r.Key == "" || r.AcceptedCount != 0 || r.ConfirmedCount != 0 {
				t.Fatalf("拒绝/缺配置伪成功：%+v %v", r, err)
			}
			if mode != "external_rejected" && signal.sends != 0 {
				t.Fatal("授权或查询未通过仍发送")
			}
			loaded, err := deliveryServiceFor(db, nil, nil).Get(ctx, r.Key)
			if err != nil || loaded.State != want {
				t.Fatalf("失败状态不可查询：%+v %v", loaded, err)
			}
			if want == delivery.Blocked || mode == "lookup_error" {
				signal.lookupError = false
				retried, err := deliveryServiceFor(db, signal, deliveryPolicyFunc(func(context.Context, delivery.Intent) (bool, error) { return true, nil })).Retry(ctx, r.Key)
				if err != nil || retried.State != delivery.Confirmed || signal.sends != 1 {
					t.Fatalf("重新授权后不能恢复：%+v %v", retried, err)
				}
			}
		})
	}
}

func TestInferenceDeliveryPostgresLostReceiptAndAcceptance(t *testing.T) {
	for _, mode := range []string{"lost_confirmed", "accepted_then_confirmed", "accepted_missing_receipt", "invalid_receipt", "changed_receipt", "denied_after_unknown"} {
		t.Run(mode, func(t *testing.T) {
			db, ctx, i, signal, policy := deliveryDBFixture(t)
			if mode == "lost_confirmed" || mode == "denied_after_unknown" {
				signal.lost = true
			} else {
				signal.state = delivery.Accepted
			}
			first, err := deliveryServiceFor(db, signal, policy).Submit(ctx, i)
			if err != nil {
				t.Fatal(err)
			}
			if signal.lost {
				if first.State != delivery.Unknown || first.AcceptedCount != 0 {
					t.Fatal("丢回执伪造受理")
				}
			} else if first.State != delivery.Accepted || first.AcceptedCount != 1 || first.ConfirmedCount != 0 {
				t.Fatal("仅受理伪造确认")
			}
			signal.mu.Lock()
			signal.lost = false
			switch mode {
			case "accepted_missing_receipt":
				delete(signal.receipts, first.Key)
			case "invalid_receipt":
				signal.receipts[first.Key] = delivery.Receipt{State: delivery.Confirmed}
			case "changed_receipt":
				signal.receipts[first.Key] = delivery.Receipt{State: delivery.Confirmed, ID: "different-receipt"}
			case "denied_after_unknown":
				policy = deliveryPolicyFunc(func(context.Context, delivery.Intent) (bool, error) { return false, nil })
			default:
				r := signal.receipts[first.Key]
				r.State = delivery.Confirmed
				signal.receipts[first.Key] = r
			}
			signal.mu.Unlock()
			r, err := deliveryServiceFor(db, signal, policy).Retry(ctx, first.Key)
			want := delivery.Unknown
			if mode == "lost_confirmed" || mode == "accepted_then_confirmed" {
				want = delivery.Confirmed
			}
			if err != nil || r.State != want || signal.sends != 1 || r.SendAttempts != 1 {
				t.Fatalf("恢复盲重发或错误确认：%+v sends=%d %v", r, signal.sends, err)
			}
			if want == delivery.Confirmed && (!r.NewConfirmed || r.AcceptedCount != 1 || r.ConfirmedCount != 1) {
				t.Fatal("确认计数错误")
			}
		})
	}
}

func TestInferenceDeliveryPostgresCrashAndStoreFailures(t *testing.T) {
	for _, mode := range []string{"put_failure", "mark_failure", "finish_failure", "crash_before_send", "crash_after_send"} {
		t.Run(mode, func(t *testing.T) {
			db, ctx, i, signal, policy := deliveryDBFixture(t)
			repo := repository.NewInferenceDeliveryRepository(db)
			key, _ := delivery.Identity(ctx, i)
			if mode == "put_failure" {
				name := "delivery_fail_create"
				if err := db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
					if tx.Statement.Table == "inference_deliveries" {
						tx.AddError(errors.New("private put failure"))
					}
				}); err != nil {
					t.Fatal(err)
				}
				r, err := deliveryServiceFor(db, signal, policy).Submit(ctx, i)
				db.Callback().Create().Remove(name)
				if err == nil || r != nil || signal.sends != 0 {
					t.Fatal("未持久化意向仍发送")
				}
				return
			}
			if mode == "crash_before_send" || mode == "crash_after_send" {
				body, _ := json.Marshal(i)
				_, err := repo.Put(ctx, &model.InferenceDelivery{TenantID: 773, Key: key, Fingerprint: delivery.Fingerprint(i), IntentJSON: string(body), Kind: i.Kind, State: string(delivery.Intended)})
				if err != nil {
					t.Fatal(err)
				}
				now := time.Now().UTC()
				if _, ok, err := repo.Claim(ctx, key, "crashed-process", now, now.Add(time.Minute)); err != nil || !ok {
					t.Fatal("未认领")
				}
				if mode == "crash_after_send" {
					if err := repo.MarkSend(ctx, key, "crashed-process"); err != nil {
						t.Fatal(err)
					}
					if _, err := signal.Send(ctx, key, i); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				name := "delivery_fail_update"
				if err := db.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
					if tx.Statement.Table != "inference_deliveries" {
						return
					}
					values, _ := tx.Statement.Dest.(map[string]any)
					if mode == "mark_failure" && values["send_attempts"] != nil || mode == "finish_failure" && values["lease_token"] == "" {
						tx.AddError(errors.New("private state write failure"))
					}
				}); err != nil {
					t.Fatal(err)
				}
				r, err := deliveryServiceFor(db, signal, policy).Submit(ctx, i)
				db.Callback().Update().Remove(name)
				if err == nil {
					t.Fatalf("持久化故障被吞：%+v %v", r, err)
				}
				if r == nil || r.Key != key || r.State != delivery.Sending || r.ConfirmedCount != 0 {
					t.Fatalf("已持久化意向的可查询证据丢失：%+v %v", r, err)
				}
				if mode == "mark_failure" && signal.sends != 0 || mode == "finish_failure" && signal.sends != 1 {
					t.Fatal("发送与持久预留顺序错误")
				}
			}
			// 未过租约时不会抢占；用真实 PG 时间字段模拟进程退出后的租约到期。
			pending, err := deliveryServiceFor(db, signal, policy).Retry(ctx, key)
			if err != nil || pending.State != delivery.Sending {
				t.Fatalf("在途状态丢失：%+v %v", pending, err)
			}
			if err := db.WithContext(tenant.OnlyTenant(ctx, 773)).Model(&model.InferenceDelivery{}).Where("tenant_id = ? AND key = ?", 773, key).Update("lease_until", time.Now().Add(-time.Second)).Error; err != nil {
				t.Fatal(err)
			}
			r, err := deliveryServiceFor(db, signal, policy).Retry(ctx, key)
			if err != nil || r.State != delivery.Confirmed || r.AcceptedCount != 1 || r.ConfirmedCount != 1 || signal.sends != 1 {
				t.Fatalf("重启恢复重复发送或假确认：%+v sends=%d %v", r, signal.sends, err)
			}
		})
	}
}

func TestInferenceDeliveryPostgresConcurrentSubmit(t *testing.T) {
	db, ctx, i, signal, policy := deliveryDBFixture(t)
	svc := deliveryServiceFor(db, signal, policy)
	// 显式并发请求只在测试内验证 PG 锁与键唯一性，不是子 agent 或 UI 自动点击。
	const count = 12
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for n := 0; n < count; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := svc.Submit(ctx, i)
			if err != nil {
				errs <- err
			} else if r.Key == "" {
				errs <- fmt.Errorf("缺持久键")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	key, _ := delivery.Identity(ctx, i)
	r, err := svc.Retry(ctx, key)
	if err != nil || r.State != delivery.Confirmed || signal.sends != 1 || r.SendAttempts != 1 {
		t.Fatalf("并发重复外发：%+v sends=%d %v", r, signal.sends, err)
	}
}
