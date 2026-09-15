package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"hivemtk-user/internal/identity"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func setupAuditTestDB(t *testing.T) {
	t.Helper()
	// 同 oneid_merge_test.go：MergeCustomers 事务会改写下列全部表，
	// 缺任意一张即报 relation does not exist 并回滚整笔合并。
	database := testutil.NewTestDB(t,
		&model.Customer{},
		&model.CustomerSession{},
		&model.CustomerEvent{},
		&model.OperationLog{},
		&model.CustomerChannel{},
		&model.CustomerDoNotContact{},
		&model.CSATSurvey{},
		&model.Clue{},
		&model.ScriptExposureLog{},
	)
	db.SetTestDB(database)
}

// TestIdentifyOrCreate_ConcurrentNoSplit 回归：高并发双上报同标识时，
// 必须收敛为全局唯一建档（unified_id 唯一索引冲突回查兜底），不得分裂建档。
func TestIdentifyOrCreate_ConcurrentNoSplit(t *testing.T) {
	setupMergeTestDB(t)
	svc := NewCustomerIdentityService()
	const phone = "13800001234"
	const n = 30

	results := make([]*model.Customer, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c, err := svc.IdentifyOrCreate(context.Background(), identity.Identifiers{Phone: phone})
			if err != nil {
				t.Errorf("IdentifyOrCreate[%d]: %v", idx, err)
				return
			}
			results[idx] = c
		}(i)
	}
	wg.Wait()

	firstID := results[0].ID
	for i := 1; i < n; i++ {
		if results[i] == nil || results[i].ID != firstID {
			t.Fatalf("并发建档分裂：期望统一 ID %v，第 %d 个返回 %+v", firstID, i, results[i])
		}
	}

	var cnt int64
	if err := db.GetDB().WithContext(context.Background()).Model(&model.Customer{}).
		Where("phone = ?", phone).Count(&cnt).Error; err != nil {
		t.Fatalf("count customers: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("并发建档后客户数量为 %d，期望 1（存在分裂建档风险）", cnt)
	}

	var uidCnt int64
	uniqID := identity.UnifiedIDFromPhone(phone)
	if err := db.GetDB().WithContext(context.Background()).Model(&model.Customer{}).
		Where("unified_id = ?", uniqID).Count(&uidCnt).Error; err != nil {
		t.Fatalf("count unified_id: %v", err)
	}
	if uidCnt != 1 {
		t.Fatalf("并发建档后 unified_id=%s 出现 %d 条，唯一索引约束可能被绕过", uniqID, uidCnt)
	}
}

// TestMergeCustomers_AuditLog 回归：合并成功后必须写入一条不可变操作审计，
// 记录操作人与被合并方关键标识，便于事后追溯。
func TestMergeCustomers_AuditLog(t *testing.T) {
	setupAuditTestDB(t)
	ctx := context.Background()
	repo := repository.NewCustomerRepository()

	primary := &model.Customer{Phone: "13800138099", Email: "p99@example.com", Tags: "[]", ChurnRisk: "low"}
	secondary := &model.Customer{Phone: "13900139099", Tags: "[]", ChurnRisk: "low"}
	if err := repo.Create(ctx, primary); err != nil {
		t.Fatalf("create primary: %v", err)
	}
	if err := repo.Create(ctx, secondary); err != nil {
		t.Fatalf("create secondary: %v", err)
	}

	op := Operator{UserID: 42, Username: "auditor"}
	svc := NewCustomerService()
	if err := svc.MergeCustomers(WithOperator(ctx, op), primary.ID, secondary.ID); err != nil {
		t.Fatalf("MergeCustomers: %v", err)
	}

	var logs []model.OperationLog
	if err := db.GetDB().WithContext(ctx).
		Where("action = ? AND resource = ? AND resource_id = ?", "merge", "customer", fmt.Sprintf("%v", primary.ID)).
		Find(&logs).Error; err != nil {
		t.Fatalf("query audit log: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("期望 1 条合并审计记录，实际 %d", len(logs))
	}
	if logs[0].UserID != 42 || logs[0].Username != "auditor" {
		t.Fatalf("审计操作人记录错误: UserID=%d Username=%s", logs[0].UserID, logs[0].Username)
	}
	if !strings.Contains(logs[0].Detail, secondary.ID) {
		t.Fatalf("审计 Detail 未包含被合并方 ID: %s", logs[0].Detail)
	}
}

// TestMergeByIdentity_UsesInjectedSvc 回归：MergeByIdentity 经反模式修复后，
// 必须复用 DI 注入的 CustomerService（s.customerSvc）完成合并，
// 而非以字面量绕过 NewCustomerService() 自建 service；审计须记录真实操作人。
func TestMergeByIdentity_UsesInjectedSvc(t *testing.T) {
	setupAuditTestDB(t)
	ctx := context.Background()
	repo := repository.NewCustomerRepository()

	primary := &model.Customer{Phone: "13700137001", Email: "", Tags: "[]", ChurnRisk: "low"}
	secondary := &model.Customer{Phone: "", Email: "dup_merge@example.com", Tags: "[]", ChurnRisk: "low"}
	if err := repo.Create(ctx, primary); err != nil {
		t.Fatalf("create primary: %v", err)
	}
	if err := repo.Create(ctx, secondary); err != nil {
		t.Fatalf("create secondary: %v", err)
	}

	op := Operator{UserID: 7, Username: "mergeop"}
	svc := NewCustomerIdentityService()
	merged, err := svc.MergeByIdentity(WithOperator(ctx, op), identity.Identifiers{Phone: "13700137001", Email: "dup_merge@example.com"})
	if err != nil {
		t.Fatalf("MergeByIdentity: %v", err)
	}
	if merged == nil || merged.Email != "dup_merge@example.com" {
		t.Fatalf("MergeByIdentity 返回主客户异常: %+v", merged)
	}
	if merged.Phone != primary.Phone && merged.Phone != secondary.Phone {
		t.Fatalf("合并后主客户 phone 既不是 primary 也不是 secondary: %+v", merged)
	}

	pAlive, _ := repo.GetByID(ctx, primary.ID)
	sAlive, _ := repo.GetByID(ctx, secondary.ID)
	if (pAlive == nil) == (sAlive == nil) {
		t.Fatalf("合并后两个原始客户应一存一删，实际 primary=%v secondary=%v", pAlive, sAlive)
	}

	var logs []model.OperationLog
	if err := db.GetDB().WithContext(ctx).
		Where("action = ? AND resource = ? AND resource_id = ?", "merge", "customer", fmt.Sprintf("%v", merged.ID)).
		Find(&logs).Error; err != nil {
		t.Fatalf("query audit log: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("期望 1 条合并审计记录，实际 %d", len(logs))
	}
	if logs[0].UserID != 7 || logs[0].Username != "mergeop" {
		t.Fatalf("审计操作人记录错误: UserID=%d Username=%s", logs[0].UserID, logs[0].Username)
	}
}

// TestMergeByIdentity_SingleEmailHealsSplit 决策2 回归：单一标识历史分裂场景。
// 两条客户拥有相同 email（unified_id 分别取各自 phone，不违反唯一索引），
// 旧实现 MergeByIdentity 因 GetByEmail 用 First 只返回一条 → 永远不合并；
// 新实现改用 FindByIdentityAll（多条）后，传入单一 email 应能自愈合并。
func TestMergeByIdentity_SingleEmailHealsSplit(t *testing.T) {
	setupAuditTestDB(t)
	ctx := context.Background()
	repo := repository.NewCustomerRepository()

	a := &model.Customer{Phone: "13700138001", Email: "split@example.com", Tags: "[]", ChurnRisk: "low"}
	b := &model.Customer{Phone: "13700138002", Email: "split@example.com", Tags: "[]", ChurnRisk: "low"}
	if err := repo.Create(ctx, a); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := repo.Create(ctx, b); err != nil {
		t.Fatalf("create b: %v", err)
	}

	svc := NewCustomerIdentityService()
	merged, err := svc.MergeByIdentity(ctx, identity.Identifiers{Email: "split@example.com"})
	if err != nil {
		t.Fatalf("MergeByIdentity: %v", err)
	}
	if merged == nil || merged.Email != "split@example.com" {
		t.Fatalf("合并后主客户异常: %+v", merged)
	}
	aAlive, _ := repo.GetByID(ctx, a.ID)
	bAlive, _ := repo.GetByID(ctx, b.ID)
	if (aAlive == nil) == (bAlive == nil) {
		t.Fatalf("单一 email 历史分裂未自愈合并，两原始客户应一存一删: a=%v b=%v", aAlive, bAlive)
	}
}
