// agent_kb_binding_repo_tenant_test.go 智能体↔知识库绑定仓储的租户契约。
//
// 同一 (agent_id, kb_id) 在不同租户可以各存一行：唯一键已由 tenancyidx.Normalize
// 复合化为 (tenant_id, agent_id, kb_id)。这里钉死「创建自动落本租户、列表/存在性检查
// 只看见本租户」。
package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const (
	akbTenantA = uint(93)
	akbTenantB = uint(94)
)

func akbSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db := testutil.NewTestDB(t, &model.AIAgent{}, &model.KnowledgeBase{}, &model.AgentKBBinding{})
	if db == nil {
		t.Fatal("测试库不可达：租户遮挡判不了（不 Skip，跳过等于没锁）")
	}
	return db
}

// akbSeedAgent 预置属于 tenantID 的智能体（写路径归属闸的正/反例夹具）。
func akbSeedAgent(t *testing.T, db *gorm.DB, tenantID, agentID uint) *model.AIAgent {
	t.Helper()
	agent := &model.AIAgent{
		TenantID:  tenantID,
		ID:        agentID,
		AgentCode: fmt.Sprintf("AKB-TENANT-%d-%d", tenantID, agentID),
		Name:      fmt.Sprintf("租户 %d 智能体 %d", tenantID, agentID),
		Persona:   "租户契约夹具",
	}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(agent).Error; err != nil {
		t.Fatalf("预置租户 %d 的智能体 %d 失败: %v", tenantID, agentID, err)
	}
	return agent
}

// akbSeedKB 预置属于 tenantID 的知识库（供绑定事务 resolveKBType 校验通过）。
func akbSeedKB(t *testing.T, db *gorm.DB, tenantID uint, code string) *model.KnowledgeBase {
	t.Helper()
	kb := &model.KnowledgeBase{
		TenantID:  tenantID,
		KBCode:    code,
		Type:      model.KnowledgeBaseTypeFAQ,
		Name:      "租户库 " + code,
		OwnerType: model.KnowledgeBaseOwnerPrivate,
	}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(kb).Error; err != nil {
		t.Fatalf("预置租户 %d 的知识库 %s 失败: %v", tenantID, code, err)
	}
	return kb
}

func akbSeed(t *testing.T, db *gorm.DB, tenantID, agentID, kbID uint) *model.AgentKBBinding {
	t.Helper()
	enabled := true
	row := &model.AgentKBBinding{
		TenantID: tenantID,
		AgentID:  agentID,
		KBID:     kbID,
		KBType:   model.KnowledgeBaseTypeFAQ,
		Role:     model.AgentKBBindingRolePrimary,
		Enabled:  &enabled,
	}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(row).Error; err != nil {
		t.Fatalf("预置租户 %d 的绑定失败: %v", tenantID, err)
	}
	return row
}

func TestAgentKBBindingRepository_CreateFillsTenant(t *testing.T) {
	db := akbSetupDB(t)
	repo := NewAgentKBBindingRepository(db)
	ctxA := tenant.WithTenantID(context.Background(), akbTenantA)

	row := &model.AgentKBBinding{
		AgentID: 1,
		KBID:    100,
		KBType:  model.KnowledgeBaseTypeFAQ,
		Role:    model.AgentKBBindingRolePrimary,
	}
	enabled := true
	row.Enabled = &enabled
	if err := repo.Create(ctxA, row); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if row.TenantID != akbTenantA {
		t.Errorf("期望 tenant_id=%d，实际 %d", akbTenantA, row.TenantID)
	}
}

func TestAgentKBBindingRepository_TenantScopedReads(t *testing.T) {
	db := akbSetupDB(t)
	akbSeed(t, db, akbTenantA, 1, 100)
	akbSeed(t, db, akbTenantB, 1, 200)

	repo := NewAgentKBBindingRepository(db)
	ctxA := tenant.WithTenantID(context.Background(), akbTenantA)

	rows, err := repo.ListByAgent(ctxA, 1, "")
	if err != nil {
		t.Fatalf("ListByAgent 失败: %v", err)
	}
	if len(rows) != 1 || rows[0].KBID != 100 {
		t.Fatalf("期望仅本租户 kb=100 的绑定，实际: %+v", rows)
	}

	exists, err := repo.CheckExists(ctxA, 1, 100)
	if err != nil {
		t.Fatalf("CheckExists 本租户失败: %v", err)
	}
	if !exists {
		t.Fatal("本租户绑定应存在")
	}

	exists, err = repo.CheckExists(ctxA, 1, 200)
	if err != nil {
		t.Fatalf("CheckExists 跨租户失败: %v", err)
	}
	if exists {
		t.Fatal("另一租户的绑定不应被本租户看见")
	}
}

// TestAgentKBBindingRepository_EnabledAndUniqueSemantics 绑定表的三条落库语义：
//   - Enabled 留 nil 时取列默认 true（Create 若用 Select("*") 会把 nil 写成 NULL 撞 NOT NULL）
//   - 显式 false 必须真正落库，不被列默认覆盖
//   - 唯一约束按租户复合：同租户重复绑定报冲突，跨租户同 (agent_id, kb_id) 各存一份
//
// TestAgentKBBindingRepository_WriteRejectsForeignAgent 写路径归属闸：租户 B 对
// 租户 A 的智能体执行绑定/替换/批量必须全部拒绝，且 A 的既有绑定原样保留。
// 回归背景：修复前三个写事务只校验 KB，B 可对任意 agent_id 播种幽灵绑定行。
func TestAgentKBBindingRepository_WriteRejectsForeignAgent(t *testing.T) {
	db := akbSetupDB(t)
	akbSeedAgent(t, db, akbTenantA, 1)
	kbA := akbSeedKB(t, db, akbTenantA, "AKB-KB-A")
	akbSeed(t, db, akbTenantA, 1, kbA.ID) // A 既有绑定 (agent 1, kbA)

	repo := NewAgentKBBindingRepository(db)
	ctxB := tenant.WithTenantID(context.Background(), akbTenantB)
	kbB := akbSeedKB(t, db, akbTenantB, "AKB-KB-B") // B 自己的库：保证失败落在 agent 闸而不是 KB 闸

	if err := repo.BindOverwrite(ctxB, 1, kbB.ID, 0); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("跨租户 agent 绑定应返回 ErrAgentNotFound，实际: %v", err)
	}
	if err := repo.ReplaceByAgent(ctxB, 1, []uint{kbB.ID}); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("跨租户 agent 替换应返回 ErrAgentNotFound，实际: %v", err)
	}
	if err := repo.BatchBindOverwrite(ctxB, []BindingTarget{{AgentID: 1, KBID: kbB.ID}}); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("跨租户 agent 批量绑定应返回 ErrAgentNotFound，实际: %v", err)
	}

	// A 的既有绑定必须原样保留（replace 的 DeleteByAgent 被租户闸挡住）。
	rows, err := repo.ListByAgentAll(tenant.WithTenantID(context.Background(), akbTenantA), 1)
	if err != nil {
		t.Fatalf("回查租户 A 绑定失败: %v", err)
	}
	if len(rows) != 1 || rows[0].KBID != kbA.ID {
		t.Fatalf("跨租户写失败后 A 的绑定应原样保留，实际: %+v", rows)
	}
}

func TestAgentKBBindingRepository_EnabledAndUniqueSemantics(t *testing.T) {
	db := akbSetupDB(t)
	repo := NewAgentKBBindingRepository(db)
	ctxA := tenant.WithTenantID(context.Background(), akbTenantA)

	nilEnabled := &model.AgentKBBinding{
		AgentID: 2, KBID: 100, KBType: model.KnowledgeBaseTypeFAQ,
		Role: model.AgentKBBindingRolePrimary,
	}
	if err := repo.Create(ctxA, nilEnabled); err != nil {
		t.Fatalf("Enabled 留 nil 的绑定创建失败: %v", err)
	}
	if nilEnabled.Enabled == nil || !*nilEnabled.Enabled {
		t.Fatal("Enabled 留 nil 时应取列默认值 true")
	}

	falseEnabled := &model.AgentKBBinding{
		AgentID: 3, KBID: 100, KBType: model.KnowledgeBaseTypeFAQ,
		Role: model.AgentKBBindingRolePrimary, Enabled: boolPtr(false),
	}
	if err := repo.Create(ctxA, falseEnabled); err != nil {
		t.Fatalf("停用绑定创建失败: %v", err)
	}
	rows, err := repo.ListByAgentAll(ctxA, 3)
	if err != nil {
		t.Fatalf("ListByAgentAll 失败: %v", err)
	}
	if len(rows) != 1 || rows[0].Enabled == nil || *rows[0].Enabled {
		t.Fatalf("显式 enabled=false 未落库: %+v", rows)
	}

	// 跨租户同 (agent_id, kb_id) 不应撞唯一索引；同租户再插一次必须冲突。
	akbSeed(t, db, akbTenantB, 2, 100)
	if err := repo.Create(ctxA, &model.AgentKBBinding{
		AgentID: 2, KBID: 100, KBType: model.KnowledgeBaseTypeFAQ,
		Role: model.AgentKBBindingRolePrimary,
	}); err == nil {
		t.Fatal("同租户重复绑定应报唯一冲突")
	}
}
