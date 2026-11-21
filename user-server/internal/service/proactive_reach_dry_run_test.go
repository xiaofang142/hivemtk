package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// 预检三类入口均不得触达发送器、消耗发送额度或抢占客户冷却窗。
func TestProactiveReachDryRun_NoSideEffects(t *testing.T) {
	tests := []struct {
		name      string
		phone     string
		email     string
		customer  bool
		channel   string
		recipient string
	}{
		{name: "phone", phone: "13900002101", channel: "sms", recipient: "13900002101"},
		{name: "email", email: "preview@example.com", channel: "email", recipient: "preview@example.com"},
		{name: "phone_before_email", phone: "13900002102", email: "unused@example.com", channel: "sms", recipient: "13900002102"},
		{name: "customer", customer: true, channel: "sms", recipient: "13900002103"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := installTestCompliancePolicy(t, `{"enabled":true,"regions":{"US":{"daily_limit":1}}}`)
			spy := &reachSendSpy{}
			svc, db := newSpyReachService(t, spy)
			ctx := context.Background()
			req := &ProactiveReachRequest{Phone: tt.phone, Email: tt.email, Content: "预检", Region: "US", DryRun: true}
			if tt.customer {
				req.OneID = "fa21-preview-" + gateNonce
				if err := db.Create(&model.Customer{ID: req.OneID, UnifiedID: req.OneID, Phone: tt.recipient}).Error; err != nil {
					t.Fatalf("创建客户失败: %v", err)
				}
			}
			gate := &stubReachGate{allowed: true}
			svc.SetPreSendApprovalChecker(gate)
			var smsRegistryCalls int
			registry := svc.smsRegistry
			svc.SetSMSRegistry(func() (func(context.Context, string, string, string, map[string]string) (string, error), error) {
				smsRegistryCalls++
				return registry()
			})

			preview, err := svc.ReachByCustomer(ctx, req)
			if err != nil {
				t.Fatalf("预检应成功: %v", err)
			}
			if preview.Status != "dry_run" || preview.MessageID != "dry_run" {
				t.Errorf("预检不得记录为发送成功: %+v", preview)
			}
			if preview.Channel != tt.channel || preview.RecipientID != tt.recipient {
				t.Errorf("预检选路错误: %+v", preview)
			}
			if spy.total() != 0 || smsRegistryCalls != 0 {
				t.Errorf("预检不得解析发送器或外发: spy=%+v registry=%d", *spy, smsRegistryCalls)
			}
			if gate.calls() != 0 {
				t.Errorf("预检不得执行外发审批钩子: %d", gate.calls())
			}
			input := ComplianceInput{Channel: tt.channel, RecipientID: tt.recipient, Region: "US"}
			if dec := policy.Decide(ctx, input); !dec.Allowed {
				t.Errorf("预检不得消耗发送额度: %+v", dec)
			}

			// 同一请求随即正式发送成功，证明预检未抢占冷却窗或额度。
			req.DryRun = false
			sent, err := svc.ReachByCustomer(ctx, req)
			if err != nil {
				t.Fatalf("预检后的正式发送应成功: %v", err)
			}
			if sent.Status != "sent" || spy.total() != 1 || gate.calls() != 1 {
				t.Errorf("正式发送应且只应经过一次闸门并发送一次: resp=%+v spy=%+v gate=%d", sent, *spy, gate.calls())
			}
			if preview.Channel != sent.Channel || preview.RecipientID != sent.RecipientID || preview.AccountID != sent.AccountID {
				t.Errorf("预检和正式发送应使用相同选路: preview=%+v sent=%+v", preview, sent)
			}
			if dec := policy.Decide(ctx, input); dec.Allowed || dec.Reason != ComplianceReasonDailyLimit {
				t.Errorf("正式发送成功才应累计日额度: %+v", dec)
			}
		})
	}
}

// 修复预检不得放松正式发送：三种目标仍依次受退订、审批与合规约束。
func TestProactiveReachDryRun_RealSendKeepsGates(t *testing.T) {
	for _, target := range []string{"phone", "email", "customer"} {
		for _, rejection := range []string{"dnc", "approval", "compliance"} {
			t.Run(target+"/"+rejection, func(t *testing.T) {
				raw := ""
				if rejection == "compliance" {
					raw = `{"enabled":true,"regions":{"US":{"denied_channels":["sms","email"]}}}`
				}
				installTestCompliancePolicy(t, raw)
				spy := &reachSendSpy{}
				svc, db := newSpyReachService(t, spy)
				ctx := context.Background()
				req := &ProactiveReachRequest{Content: "正式发送", Region: "US"}
				dncKey := "13900002108"
				switch target {
				case "phone":
					req.Phone = dncKey
				case "email":
					req.Email = "Guard@Example.com"
					dncKey = "email:" + NormalizeEmail(req.Email)
				case "customer":
					req.OneID = "fa21-gates-" + rejection + "-" + gateNonce
					if err := db.Create(&model.Customer{ID: req.OneID, UnifiedID: req.OneID, Phone: dncKey}).Error; err != nil {
						t.Fatalf("创建客户失败: %v", err)
					}
					dncKey = req.OneID
				}
				gate := &stubReachGate{allowed: rejection != "approval", reason: "not_approved"}
				svc.SetPreSendApprovalChecker(gate)
				want := ErrReachApprovalDenied
				wantGateCalls := 1
				switch rejection {
				case "dnc":
					dnc := NewDoNotContactService(repository.NewCustomerDoNotContactRepository(db))
					if err := dnc.Block(ctx, dncKey, model.DoNotContactChannelAll, "manual"); err != nil {
						t.Fatalf("写退订记录失败: %v", err)
					}
					svc.SetDoNotContact(dnc)
					want, wantGateCalls = ErrDoNotContact, 0
				case "compliance":
					want = ErrReachComplianceDenied
				}
				if resp, err := svc.ReachByCustomer(ctx, req); !errors.Is(err, want) || resp != nil {
					t.Errorf("正式发送必须被对应闸口拒绝: want=%v resp=%+v err=%v", want, resp, err)
				}
				if spy.total() != 0 || gate.calls() != wantGateCalls {
					t.Errorf("拒绝后零外发且审批顺序不变: spy=%+v gate=%d want=%d", *spy, gate.calls(), wantGateCalls)
				}
			})
		}
	}
}

// 需要渠道账号时，预检必须复用正式选路，拒绝虚构账号或跳过账号查询。
func TestProactiveReachDryRun_UsesActualAccount(t *testing.T) {
	for _, account := range []string{"42", ""} {
		t.Run(fmt.Sprintf("account=%s", account), func(t *testing.T) {
			installTestCompliancePolicy(t, "")
			spy := &reachSendSpy{}
			svc, db := newSpyReachService(t, spy)
			svc.accountLookup = &mockAccountLookup{supported: map[string]bool{"telegram": true}, results: map[string]string{"telegram": account}}
			oneID := "fa21-account-" + account + "-" + gateNonce
			if err := db.Create(&model.Customer{ID: oneID, UnifiedID: oneID, TelegramChatID: 778899}).Error; err != nil {
				t.Fatalf("创建客户失败: %v", err)
			}
			req := &ProactiveReachRequest{OneID: oneID, Content: "预检", DryRun: true}
			preview, err := svc.ReachByCustomer(context.Background(), req)
			if spy.total() != 0 {
				t.Fatalf("预检不得外发: %+v", *spy)
			}
			if account == "" {
				if !errors.Is(err, ErrNoActiveAccount) || preview != nil {
					t.Errorf("无账号预检必须返回与正式选路相同的错误: resp=%+v err=%v", preview, err)
				}
				req.DryRun = false
				if _, err := svc.ReachByCustomer(context.Background(), req); !errors.Is(err, ErrNoActiveAccount) {
					t.Errorf("正式选路同样应拒绝无账号: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("有真实账号预检应成功: %v", err)
			}
			if preview.AccountID != account || preview.Channel != "telegram" || preview.RecipientID != "778899" {
				t.Errorf("预检应给出真实账号与身份: %+v", preview)
			}
			req.DryRun = false
			sent, err := svc.ReachByCustomer(context.Background(), req)
			if err != nil || sent == nil {
				t.Fatalf("正式发送应成功: resp=%+v err=%v", sent, err)
			}
			if sent.AccountID != preview.AccountID || sent.Channel != preview.Channel || sent.RecipientID != preview.RecipientID || spy.total() != 1 {
				t.Errorf("预检和正式发送选路必须一致: preview=%+v sent=%+v spy=%+v", preview, sent, *spy)
			}
		})
	}
}
