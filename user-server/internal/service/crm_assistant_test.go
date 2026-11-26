package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ---- CRM-22 fakes ----

type assistantFakeExecutor struct {
	evidence map[string][]CRMAssistantEvidence
	err      error
}

func (f *assistantFakeExecutor) Execute(_ context.Context, _ CRMAssistantScope, call CRMAssistantToolCall) ([]CRMAssistantEvidence, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.evidence[call.Name], nil
}

type assistantFakeModel struct {
	text string
	refs []string
	err  error
}

func (f *assistantFakeModel) Answer(_ context.Context, _ string, _ []CRMAssistantEvidence) (string, []string, error) {
	return f.text, f.refs, f.err
}

type assistantAuditSpy struct {
	records []CRMAssistantToolRecord
}

func (s *assistantAuditSpy) RecordToolCall(_ context.Context, _ CRMAssistantScope, r CRMAssistantToolRecord) {
	s.records = append(s.records, r)
}

func assistantScope() CRMAssistantScope {
	return CRMAssistantScope{
		TenantID: 1, EmployeeID: 7, SessionID: "s-1",
		CustomerDataScope: CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acc-a"}, Industries: []string{"教育"}},
	}
}

// CRM-22：只读白名单服务端执行，写/外发工具一律拒绝，未授权账号/行业拒绝。
func TestCRMAssistant_ToolWhitelistAndScope(t *testing.T) {
	svc := NewCRMAssistantService()
	svc.SetExecutor(&assistantFakeExecutor{evidence: map[string][]CRMAssistantEvidence{
		"crm.query_customer": {{Ref: "ev-1", Source: "customer", AccountID: "acc-a"}},
		"send_message":       {{Ref: "ev-x", Source: "external"}},
	}})
	svc.SetModel(&assistantFakeModel{text: "结论", refs: []string{"ev-1"}})
	spy := &assistantAuditSpy{}
	svc.SetAudit(spy)

	ans, err := svc.Ask(context.Background(), assistantScope(), "问题", []CRMAssistantToolCall{
		{Name: "crm.query_customer", AccountID: "acc-a"},
		{Name: "send_message", AccountID: "acc-a"},
		{Name: "crm.query_customer", AccountID: "acc-b"},
		{Name: "crm.query_contact", Industry: "医疗"},
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if ans.Status != "ok" || ans.Text != "结论" || len(ans.Evidence) != 1 {
		t.Fatalf("仅白名单内且授权的工具应产出证据与结论: %+v", ans)
	}
	if len(ans.ToolCalls) != 4 {
		t.Fatalf("所有调用都应留痕: %+v", ans.ToolCalls)
	}
	if ans.ToolCalls[1].Allowed || ans.ToolCalls[2].Allowed || ans.ToolCalls[3].Allowed {
		t.Fatalf("写工具/越权账号/越权行业必须拒绝: %+v", ans.ToolCalls)
	}
	if len(spy.records) != 4 {
		t.Fatalf("审计必须记录每次调用: %d", len(spy.records))
	}
}

// CRM-22：伪造/越权引用不能借模型输出通过；无证据时显式降级为空结论。
func TestCRMAssistant_NoFabricatedEvidence(t *testing.T) {
	svc := NewCRMAssistantService()
	svc.SetExecutor(&assistantFakeExecutor{evidence: map[string][]CRMAssistantEvidence{
		"crm.query_customer": {{Ref: "ev-1", Source: "customer"}},
	}})
	svc.SetModel(&assistantFakeModel{text: "编造结论", refs: []string{"ev-fake"}})

	ans, err := svc.Ask(context.Background(), assistantScope(), "问题", []CRMAssistantToolCall{
		{Name: "crm.query_customer", AccountID: "acc-a"},
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if ans.Status != "degraded_no_evidence" || ans.Text != "" {
		t.Fatalf("伪造引用必须降级且不产出结论: %+v", ans)
	}
}

// CRM-22：模型不可用时显式降级；跨员工会话直接拒绝。
func TestCRMAssistant_ModelUnavailableAndSessionOwnership(t *testing.T) {
	svc := NewCRMAssistantService()
	svc.SetModel(&assistantFakeModel{err: errors.New("llm down")})

	ans, err := svc.Ask(context.Background(), assistantScope(), "问题", nil)
	if err != nil || ans.Status != "model_unavailable" || ans.Text != "" {
		t.Fatalf("模型失败必须显式降级: ans=%+v err=%v", ans, err)
	}

	cross := assistantScope()
	cross.EmployeeID = 8
	if _, err := svc.Ask(context.Background(), cross, "问题", nil); !errors.Is(err, ErrAssistantForbidden) {
		t.Fatalf("跨员工会话必须拒绝，实际 err=%v", err)
	}
}

// ---- CRM-23 fakes ----

type actionAuditSpy struct {
	actions []string
}

func (s *actionAuditSpy) Record(_ context.Context, _, action, _ string) {
	s.actions = append(s.actions, action)
}

type actionExecutorSpy struct {
	receipt string
	calls   int
	err     error
}

func (s *actionExecutorSpy) Execute(_ context.Context, _ CRMAssistantScope, _ *CRMActionApprovalDraft) (string, error) {
	s.calls++
	if s.err != nil {
		return "", s.err
	}
	return s.receipt, nil
}

func newDraftService() (*CRMActionApprovalService, *actionExecutorSpy, *actionAuditSpy) {
	svc := NewCRMActionApprovalService(NewInMemoryCRMActionApprovalDraftStore())
	exec := &actionExecutorSpy{receipt: "opp-1"}
	audit := &actionAuditSpy{}
	svc.SetExecutor(exec)
	svc.SetAudit(audit)
	return svc, exec, audit
}

func draftFixture() (CRMAssistantScope, string, []string) {
	return assistantScope(), `{"name":"商机A"}`, []string{"ev-1"}
}

// CRM-23：建议→审批→执行闭环，未审批不执行，重复执行幂等且只建一次业务对象。
func TestCRMActionApprovalDraft_ApprovalLoop(t *testing.T) {
	svc, exec, audit := newDraftService()
	scope, payload, evidence := draftFixture()

	_, err := svc.Execute(context.Background(), scope, "missing")
	if !errors.Is(err, ErrAssistantDraftNotFound) {
		t.Fatalf("不存在的草稿执行必须失败: %v", err)
	}

	d, err := svc.CreateDraft(context.Background(), scope, "opportunity", payload, evidence, "v-1")
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if _, err := svc.Execute(context.Background(), scope, d.ID); err == nil {
		t.Fatal("未审批的草稿禁止执行")
	}
	approved, err := svc.Approve(context.Background(), scope, d.ID)
	if err != nil || approved.Status != CRMActionApprovalDraftApproved {
		t.Fatalf("Approve: draft=%+v err=%v", approved, err)
	}
	// 重复审批幂等
	if _, err := svc.Approve(context.Background(), scope, d.ID); err != nil {
		t.Fatalf("重复审批应幂等: %v", err)
	}
	done, err := svc.Execute(context.Background(), scope, d.ID)
	if err != nil || done.Status != CRMActionApprovalDraftExecuted || done.ReceiptID != "opp-1" {
		t.Fatalf("Execute: draft=%+v err=%v", done, err)
	}
	again, err := svc.Execute(context.Background(), scope, d.ID)
	if err != nil || again.ReceiptID != "opp-1" || exec.calls != 1 {
		t.Fatalf("重复执行必须幂等且不重复建单: calls=%d err=%v", exec.calls, err)
	}
	if len(audit.actions) < 2 {
		t.Fatalf("审批与执行都要审计: %+v", audit.actions)
	}
}

// CRM-23：拒绝/过期/对象版本失效均不执行；撤权（跨员工）拒绝。
func TestCRMActionApprovalDraft_RejectExpireAndVersion(t *testing.T) {
	svc, exec, _ := newDraftService()
	scope, payload, evidence := draftFixture()

	rejected, err := svc.CreateDraft(context.Background(), scope, "followup", payload, evidence, "v-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reject(context.Background(), scope, rejected.ID, "不需要"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if _, err := svc.Execute(context.Background(), scope, rejected.ID); err == nil || exec.calls != 0 {
		t.Fatal("被拒绝的草稿禁止执行")
	}

	expiring, err := svc.CreateDraft(context.Background(), scope, "followup", payload, evidence, "v-1")
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return time.Now().Add(48 * time.Hour) }
	if _, err := svc.Execute(context.Background(), scope, expiring.ID); !errors.Is(err, ErrAssistantVersionConflict) {
		t.Fatalf("过期草稿必须拒绝: %v", err)
	}
	svc.now = time.Now

	missingEvidence, err := svc.CreateDraft(context.Background(), scope, "followup", payload, nil, "v-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(context.Background(), scope, missingEvidence.ID); !errors.Is(err, ErrAssistantForbidden) {
		t.Fatalf("无证据/版本草稿禁止审批: %v", err)
	}

	cross := scope
	cross.EmployeeID = 8
	if _, err := svc.Approve(context.Background(), cross, rejected.ID); !errors.Is(err, ErrAssistantForbidden) {
		t.Fatalf("撤权/跨员工审批必须拒绝: %v", err)
	}
}

// CRM-23：执行前对象版本变化（CAS 冲突）不得重复执行。
func TestCRMActionApprovalDraft_VersionConflict(t *testing.T) {
	svc, exec, _ := newDraftService()
	scope, payload, evidence := draftFixture()
	d, err := svc.CreateDraft(context.Background(), scope, "opportunity", payload, evidence, "v-1")
	if err != nil {
		t.Fatal(err)
	}
	approved, err := svc.Approve(context.Background(), scope, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 调用方持有审批前的陈旧版本：执行时必须 CAS 冲突，不得覆盖并发改动。
	staleVersion := d.Version
	if _, err := svc.Execute(context.Background(), scope, d.ID, staleVersion); !errors.Is(err, ErrAssistantVersionConflict) {
		t.Fatalf("陈旧版本执行必须冲突: %v", err)
	}
	if approved.Status != CRMActionApprovalDraftApproved {
		t.Fatalf("审批状态异常: %+v", approved)
	}
	if exec.calls != 0 {
		t.Fatalf("冲突时不得调用业务执行出口: calls=%d", exec.calls)
	}
}
