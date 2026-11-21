// agent_kb_binding_tx_test.go 绑定域三条写事务（覆盖 / 全量替换 / 批量覆盖）的原子性契约。
//
// 为什么钉在这里而不是 service：这三个动作的共同形状是"先删后建"，一旦不是在**同一事务**
// 里完成，中途失败就会把已有绑定删掉却没有替代品——用户看到的是"点了保存，挂载全没了"。
// 事务边界此前在 service 里，而且同时存在"有 db 走事务 / 无 db 退化成裸写"两条路径，
// 只有当调用方恰好传了 db 才有原子性。现在边界下沉到仓储，这份用例锁的就是：
// 任何一条失败路径，库里都保持调用前的样子。
package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func akbTxSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, &model.AIAgent{}, &model.KnowledgeBase{}, &model.AgentKBBinding{})
	if db == nil {
		t.Fatal("测试库不可达：事务原子性判不了（不 Skip，跳过等于没锁）")
	}
	return db
}

// akbTxSeedAgent 预置智能体行：写事务入口的 resolveAgentExists 闸要求被绑定的
// 智能体真实存在（含租户归属），所以每个写路径用例都要先 seed 自己的 agent。
func akbTxSeedAgent(t *testing.T, db *gorm.DB, agentID uint) *model.AIAgent {
	t.Helper()
	agent := &model.AIAgent{
		ID:        agentID,
		AgentCode: fmt.Sprintf("AKB-TX-%d", agentID),
		Name:      fmt.Sprintf("绑定事务智能体 %d", agentID),
		Persona:   "测试夹具",
	}
	if err := db.Create(agent).Error; err != nil {
		t.Fatalf("预置智能体 %d 失败: %v", agentID, err)
	}
	return agent
}

func akbTxSeedKB(t *testing.T, db *gorm.DB, code, kbType string) *model.KnowledgeBase {
	t.Helper()
	kb := &model.KnowledgeBase{
		KBCode:    code,
		Type:      kbType,
		Name:      "库 " + code,
		OwnerType: model.KnowledgeBaseOwnerShared,
	}
	if err := NewKnowledgeBaseRepository(db).Create(context.Background(), kb); err != nil {
		t.Fatalf("预置知识库 %s 失败: %v", code, err)
	}
	return kb
}

func akbTxRows(t *testing.T, db *gorm.DB, agentID uint) []model.AgentKBBinding {
	t.Helper()
	rows, err := NewAgentKBBindingRepository(db).ListByAgentAll(context.Background(), agentID)
	if err != nil {
		t.Fatalf("回查 agent=%d 的绑定失败: %v", agentID, err)
	}
	return rows
}

// TestAgentKBBindingRepository_ReplaceRollsBackOnMissingKB 全量替换里引用到不存在的
// 知识库时必须整体回滚：调用前的绑定要原样还在，而不是"删完了才发现建不出来"。
func TestAgentKBBindingRepository_ReplaceRollsBackOnMissingKB(t *testing.T) {
	db := akbTxSetupDB(t)
	repo := NewAgentKBBindingRepository(db)
	ctx := context.Background()
	akbTxSeedAgent(t, db, 7)

	kb := akbTxSeedKB(t, db, "KB-TX-1", model.KnowledgeBaseTypeFAQ)
	old := &model.AgentKBBinding{
		AgentID: 7, KBID: kb.ID, KBType: model.KnowledgeBaseTypeFAQ,
		Role: model.AgentKBBindingRolePrimary, Enabled: boolPtr(true),
	}
	if err := repo.Create(ctx, old); err != nil {
		t.Fatalf("预置既有绑定失败: %v", err)
	}

	err := repo.ReplaceByAgent(ctx, 7, []uint{kb.ID, 999999})
	if !errors.Is(err, ErrKnowledgeBaseNotFound) {
		t.Fatalf("引用不存在知识库应返回 ErrKnowledgeBaseNotFound，实际: %v", err)
	}

	rows := akbTxRows(t, db, 7)
	if len(rows) != 1 || rows[0].KBID != kb.ID {
		t.Fatalf("替换失败后既有绑定应原样保留，实际: %+v", rows)
	}
}

// TestAgentKBBindingRepository_ReplaceEmptyClears 空列表等价于清空挂载。
func TestAgentKBBindingRepository_ReplaceEmptyClears(t *testing.T) {
	db := akbTxSetupDB(t)
	repo := NewAgentKBBindingRepository(db)
	ctx := context.Background()
	akbTxSeedAgent(t, db, 8)

	kb := akbTxSeedKB(t, db, "KB-TX-2", model.KnowledgeBaseTypeRAG)
	if err := repo.Create(ctx, &model.AgentKBBinding{
		AgentID: 8, KBID: kb.ID, KBType: model.KnowledgeBaseTypeRAG,
		Role: model.AgentKBBindingRolePrimary, Enabled: boolPtr(true),
	}); err != nil {
		t.Fatalf("预置绑定失败: %v", err)
	}

	if err := repo.ReplaceByAgent(ctx, 8, nil); err != nil {
		t.Fatalf("清空挂载失败: %v", err)
	}
	if rows := akbTxRows(t, db, 8); len(rows) != 0 {
		t.Fatalf("清空后仍有 %d 条绑定: %+v", len(rows), rows)
	}
}

// TestAgentKBBindingRepository_BindOverwriteReplacesAndReusesRealKBType
// 覆盖语义 = 同 (agent_id, kb_id) 只剩一行且优先级被后来的值改写；kb_type 取库的真实类型。
func TestAgentKBBindingRepository_BindOverwriteReplacesAndReusesRealKBType(t *testing.T) {
	db := akbTxSetupDB(t)
	repo := NewAgentKBBindingRepository(db)
	ctx := context.Background()
	akbTxSeedAgent(t, db, 9)

	kb := akbTxSeedKB(t, db, "KB-TX-3", model.KnowledgeBaseTypeSOP)
	if err := repo.BindOverwrite(ctx, 9, kb.ID, 3); err != nil {
		t.Fatalf("首次绑定失败: %v", err)
	}
	if err := repo.BindOverwrite(ctx, 9, kb.ID, 7); err != nil {
		t.Fatalf("覆盖绑定失败: %v", err)
	}

	rows := akbTxRows(t, db, 9)
	if len(rows) != 1 {
		t.Fatalf("覆盖语义下应只剩 1 行，实际 %d: %+v", len(rows), rows)
	}
	if rows[0].Priority != 7 {
		t.Fatalf("优先级应被覆盖为 7，实际 %d", rows[0].Priority)
	}
	if rows[0].KBType != model.KnowledgeBaseTypeSOP {
		t.Fatalf("kb_type 应取知识库真实类型 sop，实际 %q", rows[0].KBType)
	}
}

// TestAgentKBBindingRepository_BindOverwriteMissingKBWritesNothing 引用了不存在的
// 知识库：报 ErrKnowledgeBaseNotFound，且不留下任何绑定行（含不覆盖掉旧的）。
func TestAgentKBBindingRepository_BindOverwriteMissingKBWritesNothing(t *testing.T) {
	db := akbTxSetupDB(t)
	repo := NewAgentKBBindingRepository(db)
	akbTxSeedAgent(t, db, 10)

	if err := repo.BindOverwrite(context.Background(), 10, 999999, 0); !errors.Is(err, ErrKnowledgeBaseNotFound) {
		t.Fatalf("引用不存在知识库应返回 ErrKnowledgeBaseNotFound，实际: %v", err)
	}
	if rows := akbTxRows(t, db, 10); len(rows) != 0 {
		t.Fatalf("失败路径不应落库，实际 %d 行: %+v", len(rows), rows)
	}
}

// TestAgentKBBindingRepository_BatchBindOverwriteIsAllOrNothing 批量里有一条坏 kb：
// 前面已经写成功的条目必须一起回滚。
func TestAgentKBBindingRepository_BatchBindOverwriteIsAllOrNothing(t *testing.T) {
	db := akbTxSetupDB(t)
	repo := NewAgentKBBindingRepository(db)
	ctx := context.Background()
	akbTxSeedAgent(t, db, 11)

	kb := akbTxSeedKB(t, db, "KB-TX-4", model.KnowledgeBaseTypeFAQ)
	targets := []BindingTarget{
		{AgentID: 11, KBID: kb.ID, Priority: 1},
		{AgentID: 11, KBID: 999999},
	}
	if err := repo.BatchBindOverwrite(ctx, targets); !errors.Is(err, ErrKnowledgeBaseNotFound) {
		t.Fatalf("批量含不存在知识库应返回 ErrKnowledgeBaseNotFound，实际: %v", err)
	}
	if rows := akbTxRows(t, db, 11); len(rows) != 0 {
		t.Fatalf("批量失败后应整体回滚，实际留下 %d 行: %+v", len(rows), rows)
	}

	if err := repo.BatchBindOverwrite(ctx, targets[:1]); err != nil {
		t.Fatalf("合法批量绑定失败: %v", err)
	}
	if rows := akbTxRows(t, db, 11); len(rows) != 1 || rows[0].KBID != kb.ID {
		t.Fatalf("合法批量绑定应落 1 行，实际: %+v", rows)
	}
}
