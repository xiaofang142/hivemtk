package service

// marketing_flow_send_gate_test.go FA-20：营销流程外发动作的统一判定链矩阵。
//
// 判据钉住：撤回/暂停（content 层栅栏，见 content/service 侧测试）之外，判定链对
// DNC、审批拒绝、静默时段、日上限配额全部「拒绝 ⇒ 真实传输零调用」；放行路径
// 恰好调用一次传输。全部使用内存替身，不连真实渠道、不外发。

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	contentsvc "hivemtk-user/internal/content/service"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

type fakeMarketingFlowDNCRepo struct {
	blocked map[string]bool
}

func (r *fakeMarketingFlowDNCRepo) Create(_ context.Context, _ *model.CustomerDoNotContact) (bool, error) {
	return true, nil
}

func (r *fakeMarketingFlowDNCRepo) ExistsByOneIDAndChannels(_ context.Context, oneID string, _ []string) (bool, error) {
	return r.blocked[oneID], nil
}

func (r *fakeMarketingFlowDNCRepo) DeleteByOneIDAndChannel(_ context.Context, _ string, _ string) error {
	return nil
}

func (r *fakeMarketingFlowDNCRepo) ListByOneID(_ context.Context, _ string) ([]*model.CustomerDoNotContact, error) {
	return nil, nil
}

type fakeMarketingFlowApproval struct {
	allowed bool
	reason  string
}

func (f *fakeMarketingFlowApproval) CheckReachPreSend(_ context.Context, _ ReachSubject) (bool, string) {
	return f.allowed, f.reason
}

// installMarketingFlowGate 装一份可控判定链：退订名单 + 审批钩子，并替换真实传输为计数替身。
func installMarketingFlowGate(t *testing.T, blocked map[string]bool, checker ReachPreSendChecker) (*[]string, *[]string) {
	t.Helper()
	reach := NewProactiveReachService(
		repository.NewProactiveReachRepository(nil),
		&fakeMarketingFlowDNCRepo{blocked: blocked},
		nil,
	)
	reach.SetPreSendApprovalChecker(checker)
	AttachMarketingFlowReachGate(reach)
	t.Cleanup(func() { AttachMarketingFlowReachGate(nil) })

	var smsCalls, emailCalls []string
	var mu sync.Mutex
	oldSms, oldEmail := marketingFlowSmsTransport, marketingFlowEmailTransport
	marketingFlowSmsTransport = func(_ context.Context, phone, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		smsCalls = append(smsCalls, phone)
		return nil
	}
	marketingFlowEmailTransport = func(_ context.Context, cfg contentsvc.MarketingFlowEmailConfig) error {
		mu.Lock()
		defer mu.Unlock()
		emailCalls = append(emailCalls, cfg.To)
		return nil
	}
	t.Cleanup(func() { marketingFlowSmsTransport, marketingFlowEmailTransport = oldSms, oldEmail })
	return &smsCalls, &emailCalls
}

func TestMarketingFlowSendGate_NotAttachedFailsClosed(t *testing.T) {
	smsCalls, emailCalls := installMarketingFlowGate(t, nil, nil)
	AttachMarketingFlowReachGate(nil) // 显式未装配

	err := gatedMarketingFlowSms(context.Background(), "13900002000", "hi")
	if err == nil || !strings.Contains(err.Error(), "未装配") {
		t.Fatalf("未装配判定链必须 fail-closed 拒发，实际 %v", err)
	}
	err = gatedMarketingFlowEmail(context.Background(), contentsvc.MarketingFlowEmailConfig{To: "a@b.com", Body: "hi"})
	if err == nil || !strings.Contains(err.Error(), "未装配") {
		t.Fatalf("邮件未装配判定链同样必须拒发，实际 %v", err)
	}
	if len(*smsCalls)+len(*emailCalls) != 0 {
		t.Errorf("拒发不得触达真实传输，实际 sms=%d email=%d", len(*smsCalls), len(*emailCalls))
	}
}

func TestMarketingFlowSendGate_DeniedApprovalSendsNothing(t *testing.T) {
	smsCalls, emailCalls := installMarketingFlowGate(t, map[string]bool{}, &fakeMarketingFlowApproval{allowed: false, reason: "not approved"})

	err := gatedMarketingFlowSms(context.Background(), "13900002001", "hi")
	if !errors.Is(err, ErrReachApprovalDenied) {
		t.Fatalf("审批拒绝应返回 ErrReachApprovalDenied，实际 %v", err)
	}
	err = gatedMarketingFlowEmail(context.Background(), contentsvc.MarketingFlowEmailConfig{To: "b@b.com", Body: "hi"})
	if !errors.Is(err, ErrReachApprovalDenied) {
		t.Fatalf("邮件审批拒绝同样应返回 ErrReachApprovalDenied，实际 %v", err)
	}
	if len(*smsCalls)+len(*emailCalls) != 0 {
		t.Errorf("审批拒绝必须零外发，实际 sms=%d email=%d", len(*smsCalls), len(*emailCalls))
	}
}

func TestMarketingFlowSendGate_DNCSendsNothing(t *testing.T) {
	smsCalls, emailCalls := installMarketingFlowGate(t, map[string]bool{"13900002002": true, "email:c@c.com": true}, nil)

	err := gatedMarketingFlowSms(context.Background(), "13900002002", "hi")
	if !errors.Is(err, ErrDoNotContact) {
		t.Fatalf("退订短信应返回 ErrDoNotContact，实际 %v", err)
	}
	err = gatedMarketingFlowEmail(context.Background(), contentsvc.MarketingFlowEmailConfig{To: "C@C.com", Body: "hi"})
	if !errors.Is(err, ErrDoNotContact) {
		t.Fatalf("退订邮箱应返回 ErrDoNotContact（大小写归一），实际 %v", err)
	}
	if len(*smsCalls)+len(*emailCalls) != 0 {
		t.Errorf("退订命中必须零外发，实际 sms=%d email=%d", len(*smsCalls), len(*emailCalls))
	}
}

func TestMarketingFlowSendGate_QuietHoursSendsNothing(t *testing.T) {
	installTestCompliancePolicy(t, `{"enabled":true,"default_region":"US","regions":{"US":{
		"quiet_hours_start":20,"quiet_hours_end":10}}}`)
	smsCalls, _ := installMarketingFlowGate(t, map[string]bool{}, nil)

	err := gatedMarketingFlowSms(context.Background(), "13900002003", "hi")
	if !errors.Is(err, ErrReachComplianceDenied) || !strings.Contains(err.Error(), ComplianceReasonQuietHours) {
		t.Fatalf("静默时段应拒发并带原因码，实际 %v", err)
	}
	if len(*smsCalls) != 0 {
		t.Errorf("静默时段必须零外发，实际 %d", len(*smsCalls))
	}
}

func TestMarketingFlowSendGate_DailyLimitSendsNothing(t *testing.T) {
	p := installTestCompliancePolicy(t, `{"enabled":true,"default_region":"US","regions":{"US":{
		"daily_limit":1}}}`)
	smsCalls, _ := installMarketingFlowGate(t, map[string]bool{}, nil)

	// 与判定链同构的入参先烧掉当日配额（RecipientID 兜底进计数键）。
	p.RecordSentInput(context.Background(), ComplianceInput{Channel: "sms", RecipientID: "13900002004"})

	err := gatedMarketingFlowSms(context.Background(), "13900002004", "hi")
	if !errors.Is(err, ErrReachComplianceDenied) || !strings.Contains(err.Error(), ComplianceReasonDailyLimit) {
		t.Fatalf("日上限用尽应拒发并带原因码，实际 %v", err)
	}
	if len(*smsCalls) != 0 {
		t.Errorf("配额不足必须零外发，实际 %d", len(*smsCalls))
	}
}

func TestMarketingFlowSendGate_AllowedPathCallsTransportExactlyOnce(t *testing.T) {
	installTestCompliancePolicy(t, "") // 未配置策略 = 放行（默认零行为变更）
	smsCalls, emailCalls := installMarketingFlowGate(t, map[string]bool{}, nil)

	if err := gatedMarketingFlowSms(context.Background(), "13900002005", "hi"); err != nil {
		t.Fatalf("放行路径不得报错：%v", err)
	}
	if err := gatedMarketingFlowEmail(context.Background(), contentsvc.MarketingFlowEmailConfig{To: "d@d.com", Body: "hi"}); err != nil {
		t.Fatalf("邮件放行路径不得报错：%v", err)
	}
	if len(*smsCalls) != 1 || (*smsCalls)[0] != "13900002005" {
		t.Errorf("短信应恰好传输一次，实际 %v", *smsCalls)
	}
	if len(*emailCalls) != 1 || (*emailCalls)[0] != "d@d.com" {
		t.Errorf("邮件应恰好传输一次且收到原始配置，实际 %v", *emailCalls)
	}
}
