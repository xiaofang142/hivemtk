package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
)

type crmAuditRepoStub struct {
	logs      []*model.OperationLog
	createErr error
}

func (r *crmAuditRepoStub) Create(_ context.Context, log *model.OperationLog) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.logs = append(r.logs, log)
	return nil
}

// TestCRMAuditRecordStoresFieldDiff 业务审计必须写真实对象/来源/actor 和字段前后差异，敏感值脱敏。
func TestCRMAuditRecordStoresFieldDiff(t *testing.T) {
	repo := &crmAuditRepoStub{}
	svc := NewCRMBusinessAuditServiceWithRepo(repo)
	_, err := svc.Record(context.Background(), CRMBusinessAuditInput{
		ActorID: 7, Actor: "张三", Action: "update", Module: "crm", Resource: "customer",
		ResourceID: "c-1", BusinessEvent: "evt-1", Source: "profile360",
		Before: map[string]any{"phone": "13812345678", "name": "旧名"},
		After:  map[string]any{"phone": "13812345678", "name": "新名"},
	})
	if err != nil {
		t.Fatalf("审计写入失败: %v", err)
	}
	if len(repo.logs) != 1 {
		t.Fatalf("应写 1 条 OperationLog，实际 %d", len(repo.logs))
	}
	log := repo.logs[0]
	if log.UserID != 7 || log.Username != "张三" || log.ResourceID != "c-1" || log.Action != "update" {
		t.Fatalf("对象/actor 不正确：%+v", log)
	}
	if strings.Contains(log.Detail, "13812345678") {
		t.Fatalf("审计明细泄漏敏感值：%s", log.Detail)
	}
	if !strings.Contains(log.OldValue, "旧名") || !strings.Contains(log.NewValue, "新名") {
		t.Fatalf("字段前后差异丢失：old=%s new=%s", log.OldValue, log.NewValue)
	}
	if !strings.Contains(log.Detail, "evt-1") || !strings.Contains(log.Detail, "profile360") {
		t.Fatalf("业务事件/来源未关联：%s", log.Detail)
	}
}

// TestCRMAuditRecordWriteFailureVisible 审计写失败必须显式返回（不再静默成功）。
func TestCRMAuditRecordWriteFailureVisible(t *testing.T) {
	svc := NewCRMBusinessAuditServiceWithRepo(&crmAuditRepoStub{createErr: errors.New("db down")})
	_, err := svc.Record(context.Background(), CRMBusinessAuditInput{Action: "delete", Module: "crm", Resource: "customer", ResourceID: "c-2"})
	if err == nil || !strings.Contains(err.Error(), "审计") {
		t.Fatalf("审计写失败必须显式返回，实际 err=%v", err)
	}
}

// TestCRMAuditRecordRejectAndNoop 拒绝/无变更不写业务，但仍有审计结论；敏感字段策略可注入。
func TestCRMAuditRecordRejectAndNoop(t *testing.T) {
	repo := &crmAuditRepoStub{}
	svc := NewCRMBusinessAuditServiceWithRepo(repo)
	svc.Mask = func(s string) string { return "[masked-by-policy]" }
	_, err := svc.Record(context.Background(), CRMBusinessAuditInput{
		ActorID: 9, Action: "reject", Module: "crm", Resource: "customer", ResourceID: "c-3",
		After: map[string]any{"email": "a@b.com"},
	})
	if err != nil || len(repo.logs) != 1 {
		t.Fatalf("拒绝也应留审计：err=%v logs=%d", err, len(repo.logs))
	}
	if strings.Contains(repo.logs[0].NewValue, "a@b.com") {
		t.Fatalf("自定义脱敏策略未生效：%s", repo.logs[0].NewValue)
	}
}
