package service

// marketing_flow_send_gate_test.go FA-20：发送型动作的流程状态栅栏与邮件 fail-closed。
//
// 本文件钉住 content 层的两段闸口接缝：
//  1. 流程在执行中途被暂停/停用后，在途执行的发送动作必须零外发（ensureFlowActiveAtSend）；
//  2. 邮件动作不再直连 SMTP——未注册发送器 = fail-closed，注册后也只见到解析好的配置。
// 判定链本体（DNC/审批/合规）在 internal/service/marketing_flow_send_gate_test.go 钉住。

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/content/model"
)

// 流程定义：触发器 → send_sms 动作（复用图测试的合法最小图）。
func sendGateFlowDef(phone, content string) string {
	def := model.FlowDefinition{Nodes: []model.FlowNode{
		{ID: "start", Type: "trigger", NextNodes: []string{"send"}},
		{ID: "send", Type: "action", Config: map[string]any{"action_type": "send_sms", "phone": phone, "content": content}},
	}}
	raw, err := json.Marshal(def)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// 在途执行遇到已暂停/停用的流程：发送动作零外发，执行以 failed 收口并带可诊断原因。
func TestMarketingFlowSendGate_PausedFlowStopsSendsMidFlight(t *testing.T) {
	for _, status := range []model.FlowStatus{model.FlowStatusPaused, model.FlowStatusInactive} {
		t.Run(string(status), func(t *testing.T) {
			svc := setupMarketingFlowService(t)
			var mu sync.Mutex
			var sent []string
			old := smsSenderFunc
			SetSmsSender(func(_ context.Context, _, content string) error {
				mu.Lock()
				defer mu.Unlock()
				sent = append(sent, content)
				return nil
			})
			t.Cleanup(func() { SetSmsSender(old) })

			flow := &model.MarketingFlow{Name: "send-gate-" + string(status), Status: status, FlowData: sendGateFlowDef("13900001900", "hi")}
			if err := svc.flowRepo.Create(context.Background(), flow); err != nil {
				t.Fatalf("创建流程失败：%v", err)
			}
			execution := &model.FlowExecution{FlowID: flow.ID, UserID: "send-gate-spy", Status: "running", StartedAt: time.Now()}
			if err := svc.executionRepo.Create(context.Background(), execution); err != nil {
				t.Fatalf("创建执行失败：%v", err)
			}

			svc.executeFlow(context.Background(), execution, flow, map[string]any{})

			if len(sent) != 0 {
				t.Errorf("流程 %s 时在途执行必须零外发，实际发送 %v", status, sent)
			}
			if execution.Status != "failed" {
				t.Errorf("发送被栅栏拒绝后执行不得记 completed，实际 %s（%s）", execution.Status, execution.ErrorMessage)
			}
			if !strings.Contains(execution.ErrorMessage, "暂停或停用") && !strings.Contains(execution.ErrorMessage, "状态失败") {
				t.Errorf("错误应可诊断（暂停/停用或读态失败），实际 %q", execution.ErrorMessage)
			}
		})
	}
}

// 对照组：active 流程的在途执行照常发送恰好一次（栅栏不误伤合法路径）。
func TestMarketingFlowSendGate_ActiveFlowStillSends(t *testing.T) {
	svc := setupMarketingFlowService(t)
	var mu sync.Mutex
	var sent []string
	old := smsSenderFunc
	SetSmsSender(func(_ context.Context, _, content string) error {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, content)
		return nil
	})
	t.Cleanup(func() { SetSmsSender(old) })

	flow := &model.MarketingFlow{Name: "send-gate-active", Status: model.FlowStatusActive, FlowData: sendGateFlowDef("13900001901", "hi")}
	if err := svc.flowRepo.Create(context.Background(), flow); err != nil {
		t.Fatalf("创建流程失败：%v", err)
	}
	execution := &model.FlowExecution{FlowID: flow.ID, UserID: "send-gate-spy", Status: "running", StartedAt: time.Now()}
	if err := svc.executionRepo.Create(context.Background(), execution); err != nil {
		t.Fatalf("创建执行失败：%v", err)
	}

	svc.executeFlow(context.Background(), execution, flow, map[string]any{})

	if execution.Status != "completed" {
		t.Fatalf("active 流程应正常完成，实际 %s（%s）", execution.Status, execution.ErrorMessage)
	}
	if len(sent) != 1 || sent[0] != "hi" {
		t.Errorf("active 流程应恰好发送一次，实际 %v", sent)
	}
}

// 邮件动作未注册发送器 = fail-closed：不允许任何绕过闸口的直连 SMTP 通道。
func TestMarketingFlowSendGate_EmailFailsClosedWithoutSender(t *testing.T) {
	svc := setupMarketingFlowService(t)
	old := emailSenderFunc
	SetEmailSender(nil)
	t.Cleanup(func() { SetEmailSender(old) })

	_, err := svc.sendActionSendEmail(context.Background(), map[string]any{
		"smtp_host": "smtp.example.com", "smtp_user": "u", "smtp_pass": "p",
		"to": "a@b.com", "body": "hi",
	}, "user", map[string]any{})
	if err == nil {
		t.Fatal("未注册邮件发送器必须拒发")
	}
	if !strings.Contains(err.Error(), "统一发送闸口") {
		t.Errorf("错误应指向统一闸口，实际 %q", err)
	}
}

// 注册后的邮件发送器收到的是解析好的配置（传输细节不在动作层）。
func TestMarketingFlowSendGate_EmailPassesConfigToRegisteredSender(t *testing.T) {
	svc := setupMarketingFlowService(t)
	var got MarketingFlowEmailConfig
	old := emailSenderFunc
	SetEmailSender(func(_ context.Context, cfg MarketingFlowEmailConfig) error {
		got = cfg
		return nil
	})
	t.Cleanup(func() { SetEmailSender(old) })

	resp, err := svc.sendActionSendEmail(context.Background(), map[string]any{
		"smtp_host": "smtp.example.com", "smtp_port": 465, "smtp_user": "u@example.com", "smtp_pass": "p",
		"to": "a@b.com", "subject": "s", "body": "hi", "is_html": false,
	}, "user", map[string]any{})
	if err != nil {
		t.Fatalf("注册发送器后应正常发送：%v", err)
	}
	if resp["sent"] != true || resp["to"] != "a@b.com" {
		t.Errorf("返回口径不得变化，实际 %v", resp)
	}
	if got.Host != "smtp.example.com" || got.Port != 465 || got.To != "a@b.com" || got.From != "u@example.com" || got.IsHTML {
		t.Errorf("传输配置解析不符预期：%+v", got)
	}
}
