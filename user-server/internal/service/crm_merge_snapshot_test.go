package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// testUndoApplier 在事务接缝内执行快照逆操作：恢复次客户与主对象字段、事件归属与 OneID 关联。
// 许可连通边只追加逆边不删除事实，确保拒收/撤销同意/营销许可不因拆回放宽。
type testUndoApplier struct{}

func (testUndoApplier) Apply(ctx context.Context, snap *MergeImpactSnapshot) error {
	secondary := &model.Customer{
		ID: snap.Secondary.ID, UnifiedID: snap.Secondary.UnifiedID, Name: snap.Secondary.Name,
		Phone: snap.Secondary.Phone, Email: snap.Secondary.Email,
		SourceAccountID: snap.Secondary.SourceAccountID, Industry: snap.Secondary.Industry,
		Tags: "[]", ChurnRisk: "low",
	}
	// 合并对次客户是软删：PK 行仍在，必须 unscoped 复位 deleted_at 并写回原字段，
	// 直接 Create 会撞 customers_pkey（历史合并留下的软删行未被物理清除）。
	// 测试替身用全局测试库句柄；生产逆操作实现必须由 Router 注入并使用同一事务接缝。
	restored := db.GetDB().WithContext(ctx).Unscoped().Model(&model.Customer{}).
		Where("id = ?", secondary.ID).
		Updates(map[string]any{
			"deleted_at": nil, "unified_id": secondary.UnifiedID, "name": secondary.Name,
			"phone": secondary.Phone, "email": secondary.Email,
			"source_account_id": secondary.SourceAccountID, "industry": secondary.Industry,
			"phone_hash": PhoneHash(secondary.Phone), "tags": "[]", "churn_risk": "low",
		})
	if restored.Error != nil || restored.RowsAffected == 0 {
		return fmt.Errorf("恢复次客户失败: err=%v rows=%d", restored.Error, restored.RowsAffected)
	}
	primary, err := repository.NewCustomerRepository().GetByID(ctx, snap.Primary.ID)
	if err != nil || primary == nil {
		return fmt.Errorf("读取主客户失败: %w", err)
	}
	primary.Name = snap.Primary.Name
	primary.Phone = snap.Primary.Phone
	if primary.Phone != "" {
		primary.PhoneHash = PhoneHash(primary.Phone)
	}
	primary.Email = snap.Primary.Email
	primary.UnifiedID = snap.Primary.UnifiedID
	if err := repository.NewCustomerRepository().Update(ctx, primary); err != nil {
		return fmt.Errorf("恢复主客户字段失败: %w", err)
	}
	if _, err := repository.NewCustomerEventRepository().ReassignCustomerID(ctx, snap.Primary.ID, snap.Secondary.ID); err != nil {
		return fmt.Errorf("逆迁移事件失败: %w", err)
	}
	return nil
}

func mergeSnapshotCustomers(t *testing.T, ctx context.Context, prefix string, primaryPhone, secondaryPhone string) (*model.Customer, *model.Customer) {
	t.Helper()
	primary := &model.Customer{ID: prefix + "-p", Name: prefix + "主", Phone: primaryPhone, UnifiedID: prefix + "-one-p", SourceAccountID: "acct-a", Tags: "[]", ChurnRisk: "low"}
	secondary := &model.Customer{ID: prefix + "-s", Name: prefix + "次", Phone: secondaryPhone, Email: prefix + "@example.test", UnifiedID: prefix + "-one-s", SourceAccountID: "acct-a", Tags: "[]", ChurnRisk: "low"}
	for _, c := range []*model.Customer{primary, secondary} {
		if err := repository.NewCustomerRepository().Create(ctx, c); err != nil {
			t.Fatalf("创建合并夹具失败: %v", err)
		}
	}
	return primary, secondary
}

// CRM-07 正例：合并后审计包含结构化快照（主次对象、字段前后值、规则版本、操作者、并发版本、关联归属）。
func TestMergeSnapshot_PersistedWithMerge(t *testing.T) {
	setupMultiEntityDedupeDB(t)
	ctx := tenant.WithTenantID(context.Background(), 1)
	p, s := mergeSnapshotCustomers(t, ctx, "msnap", "13811110001", "")
	if err := repository.NewCustomerEventRepository().Record(ctx, &model.CustomerEvent{CustomerID: s.ID, EventType: model.EventTypeSignup}); err != nil {
		t.Fatalf("创建事件夹具失败: %v", err)
	}
	svc := NewCustomerDedupeService()
	ctx = WithOperator(ctx, Operator{UserID: 42, Username: "合并操作员"})
	_, err := svc.Merge(ctx, CustomerDataScope{AllAccounts: true, UserID: 42}, p.ID, s.ID, "合并操作员", "同手机号", "crm/manual")
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	audits, err := svc.ListMergeAudit(ctx, CustomerDataScope{AllAccounts: true, UserID: 42}, p.ID)
	if err != nil || len(audits) == 0 {
		t.Fatalf("读取合并审计失败: audits=%d err=%v", len(audits), err)
	}
	var snap MergeImpactSnapshot
	if err := json.Unmarshal([]byte(audits[0].OriginalValues), &snap); err != nil {
		t.Fatalf("审计快照不是结构化 JSON: %v", err)
	}
	if !snap.Recoverable || snap.SchemaVersion < mergeImpactSchemaVersion {
		t.Fatalf("新快照必须标记可恢复: %+v", snap)
	}
	if snap.Primary.ID != p.ID || snap.Secondary.ID != s.ID {
		t.Fatalf("快照必须保留主次对象: %+v", snap)
	}
	if snap.Operator.UserID != 42 || snap.Operator.Username != "合并操作员" || snap.ConcurrencyVersion == "" {
		t.Fatalf("快照必须保留操作者与并发版本: %+v", snap)
	}
	if snap.Relations.Events != 1 {
		t.Fatalf("快照必须保留关联归属计数: %+v", snap.Relations)
	}
	foundField := false
	for _, change := range snap.FieldChanges {
		if change.Field == "email" && change.After == s.Email {
			foundField = true
		}
	}
	if !foundField {
		t.Fatalf("快照必须保留字段前后值: %+v", snap.FieldChanges)
	}
	if len(snap.Uncaptured) == 0 {
		t.Fatalf("未覆盖的表必须显式列出，不能假报完整: %+v", snap)
	}
}

// CRM-07 正例：快照写失败与合并失败同事务回滚。
func TestMergeSnapshot_RollbackOnSnapshotFailure(t *testing.T) {
	database := setupMultiEntityDedupeDB(t)
	ctx := tenant.WithTenantID(context.Background(), 1)
	p, s := mergeSnapshotCustomers(t, ctx, "msnap-rb", "13811110002", "")
	svc := NewCustomerDedupeService()
	// 注入合并审计写失败：合并动作与快照都不得留存。
	if err := database.Callback().Create().Before("gorm:create").Register("crm07:audit-fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "customer_merges" {
			tx.AddError(fmt.Errorf("受控快照写入失败"))
		}
	}); err != nil {
		t.Fatalf("注册故障回调失败: %v", err)
	}
	defer database.Callback().Create().Remove("crm07:audit-fail")
	_, err := svc.Merge(ctx, CustomerDataScope{AllAccounts: true, UserID: 42}, p.ID, s.ID, "op", "故障注入", "crm/manual")
	if err == nil {
		t.Fatalf("快照写入失败必须拒绝合并")
	}
	var count int64
	if err := database.Model(&model.CustomerMerge{}).Where("primary_id = ?", p.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("快照失败后审计必须回滚: count=%d err=%v", count, err)
	}
	var secondary model.Customer
	if err := database.Unscoped().Where("id = ?", s.ID).First(&secondary).Error; err != nil || !secondary.DeletedAt.Valid {
		t.Fatalf("合并必须随快照一起回滚，次客户不应被软删: err=%v deleted=%v", err, secondary.DeletedAt)
	}
}

// CRM-07 负例：存量无完整快照明确标记不可自动恢复，不伪造。
func TestMergeSnapshot_LegacyMarkedUnrecoverable(t *testing.T) {
	setupMultiEntityDedupeDB(t)
	ctx := tenant.WithTenantID(context.Background(), 1)
	legacy := &model.CustomerMerge{MergeID: "cm_legacy", PrimaryID: "legacy-p", SecondaryID: "legacy-s",
		OriginalValues: `{"id":"legacy-s","name":"旧合并"}`, Action: "manual"}
	if err := repository.NewCustomerMergeRepository().Create(ctx, legacy); err != nil {
		t.Fatalf("创建存量审计失败: %v", err)
	}
	svc := NewCustomerDedupeService()
	result, snap, err := svc.PreviewUndo(ctx, CustomerDataScope{AllAccounts: true, UserID: 7}, legacy.MergeID)
	if err != nil {
		t.Fatalf("预览存量合并失败: %v", err)
	}
	if snap.Recoverable || !strings.Contains(result.Hint, "不可自动恢复") {
		t.Fatalf("存量无完整快照必须显式标记不可恢复: result=%+v snap=%+v", result, snap)
	}
}

// CRM-08 正例：冲突检查后事务撤销成功，恢复次客户与主字段，且不扩大许可。
func TestUndoMerge_SuccessAndConsentNotWidened(t *testing.T) {
	setupMultiEntityDedupeDB(t)
	ctx := tenant.WithTenantID(context.Background(), 1)
	p, s := mergeSnapshotCustomers(t, ctx, "undo-ok", "13822220001", "")
	svc := NewCustomerDedupeService()
	ctx = WithOperator(ctx, Operator{UserID: 42, Username: "撤销员"})
	if _, err := svc.Merge(ctx, CustomerDataScope{AllAccounts: true, UserID: 42}, p.ID, s.ID, "op", "合并", "crm/manual"); err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	// 次身份营销拒收事实在合并前存在；撤销后仍须阻断，不能因拆回放宽。
	// 逆操作只恢复对象归属，不改写 contact_consent_events / preferences 的事实行，
	// 也不删除合并时追加的连通边（见 testUndoApplier.Apply）。

	svc.SetMergeUndoApplier(testUndoApplier{})
	audits, err := svc.ListMergeAudit(ctx, CustomerDataScope{AllAccounts: true, UserID: 42}, p.ID)
	if err != nil || len(audits) == 0 {
		t.Fatalf("读取合并审计失败: %v", err)
	}
	mergeID := audits[0].MergeID
	result, err := svc.UndoMerge(ctx, CustomerDataScope{AllAccounts: true, UserID: 42}, mergeID, "误合并")
	if err != nil || !result.Undone {
		t.Fatalf("撤销应成功: result=%+v err=%v", result, err)
	}
	restored, err := repository.NewCustomerRepository().GetByID(ctx, s.ID)
	if err != nil || restored == nil || restored.Name != s.Name {
		t.Fatalf("次客户未恢复: restored=%+v err=%v", restored, err)
	}
	// 重复撤销幂等：不重复恢复、不报错。
	again, err := svc.UndoMerge(ctx, CustomerDataScope{AllAccounts: true, UserID: 42}, mergeID, "重复请求")
	if err != nil || !again.Undone || again.Conflict != "已撤销" {
		t.Fatalf("重复撤销应幂等: result=%+v err=%v", again, err)
	}
}

// CRM-08 反例：合并后主客户有新写入（版本变化/链式合并）显式拒绝，并给人工线索。
func TestUndoMerge_ConflictRejectsAndHints(t *testing.T) {
	setupMultiEntityDedupeDB(t)
	ctx := tenant.WithTenantID(context.Background(), 1)
	p, s := mergeSnapshotCustomers(t, ctx, "undo-conf", "13822220002", "")
	svc := NewCustomerDedupeService()
	if _, err := svc.Merge(ctx, CustomerDataScope{AllAccounts: true, UserID: 42}, p.ID, s.ID, "op", "合并", "crm/manual"); err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	audits, _ := svc.ListMergeAudit(ctx, CustomerDataScope{AllAccounts: true, UserID: 42}, p.ID)
	mergeID := audits[0].MergeID
	// 模拟合并后新写入：更新主客户名称，改变 updated_at。
	primary, _ := repository.NewCustomerRepository().GetByID(ctx, p.ID)
	primary.Name = "新写入的名称"
	if err := repository.NewCustomerRepository().Update(ctx, primary); err != nil {
		t.Fatalf("制造版本冲突失败: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	svc.SetMergeUndoApplier(testUndoApplier{})
	result, err := svc.UndoMerge(ctx, CustomerDataScope{AllAccounts: true, UserID: 42}, mergeID, "尝试撤销")
	if err != nil {
		t.Fatalf("冲突应显式拒绝而非报错: %v", err)
	}
	if result.Undone || result.Conflict == "" || result.Hint == "" {
		t.Fatalf("版本冲突必须拒绝并给人工线索: %+v", result)
	}
	restored, _ := repository.NewCustomerRepository().GetByID(ctx, s.ID)
	if restored != nil {
		t.Fatalf("冲突拒绝后不得恢复次客户: %+v", restored)
	}
}

// CRM-08 反例：越权用户不能撤销，且不得读取快照影响。
func TestUndoMerge_ForbiddenScope(t *testing.T) {
	setupMultiEntityDedupeDB(t)
	ctx := tenant.WithTenantID(context.Background(), 1)
	p, s := mergeSnapshotCustomers(t, ctx, "undo-scope", "13822220003", "")
	svc := NewCustomerDedupeService()
	if _, err := svc.Merge(ctx, CustomerDataScope{AllAccounts: true, UserID: 42}, p.ID, s.ID, "op", "合并", "crm/manual"); err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	audits, _ := svc.ListMergeAudit(ctx, CustomerDataScope{AllAccounts: true, UserID: 42}, p.ID)
	_, err := svc.UndoMerge(ctx, CustomerDataScope{UserID: 9, RestrictedAccounts: true, AccountIDs: []string{"acct-other"}}, audits[0].MergeID, "越权撤销")
	if err != ErrCRMCustomerForbidden {
		t.Fatalf("越权撤销必须拒绝: err=%v", err)
	}
}
