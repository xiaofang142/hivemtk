package reflux

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// faRefluxSvc 构造带内存 FAQ fake 的 RefluxService + 测试库。
func faRefluxSvc(t *testing.T) (*RefluxService, *faFAQStore, *gorm.DB, context.Context) {
	database := testutil.NewTestDB(t,
		&model.UnknownQuestion{}, &model.UnknownQuestionCluster{}, &model.UnknownQuestionReviewLog{})
	db.SetTestDB(database)
	repo := repository.NewRefluxRepository(database)
	svc := NewRefluxService(repo)
	store := &faFAQStore{items: map[uint]bool{}}
	svc.SetFAQCreate(func(ctx context.Context, req FAQCreateReq) (uint, error) {
		store.next++
		store.items[store.next] = true
		return store.next, nil
	})
	svc.SetFAQDelete(func(ctx context.Context, faqID uint) error {
		delete(store.items, faqID)
		store.deleted = append(store.deleted, faqID)
		return nil
	})
	return svc, store, database, context.Background()
}

type faFAQStore struct {
	items   map[uint]bool
	next    uint
	deleted []uint
}

func faUQ(t *testing.T, database *gorm.DB, tenant uint, hash string, status string) *model.UnknownQuestion {
	q := &model.UnknownQuestion{
		TenantID: tenant, QuestionHash: hash, Question: "q-" + hash,
		Source: "web", Status: status,
	}
	if err := database.Create(q).Error; err != nil {
		t.Fatalf("create uq: %v", err)
	}
	return q
}

// ① 已采纳后合并拒：非 pending_review 状态不得 merge。
func TestFA_MergeNonPendingRejected(t *testing.T) {
	svc, _, database, ctx := faRefluxSvc(t)
	src := faUQ(t, database, 1, "fa-merge-nonpending-src", model.UQStatusAdopted)
	tgt := faUQ(t, database, 1, "fa-merge-nonpending-tgt", model.UQStatusPendingReview)
	_, err := svc.ReviewQuestion(ctx, src.ID, &AdoptRequest{Action: ActionMerge, MergedTargetID: tgt.ID}, "op")
	if !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("已采纳后合并应 ErrIllegalTransition, got %v", err)
	}
}

// ② 自合并拒。
func TestFA_MergeSelfRejected(t *testing.T) {
	svc, _, database, ctx := faRefluxSvc(t)
	uq := faUQ(t, database, 1, "fa-merge-self", model.UQStatusPendingReview)
	_, err := svc.ReviewQuestion(ctx, uq.ID, &AdoptRequest{Action: ActionMerge, MergedTargetID: uq.ID}, "op")
	if err == nil {
		t.Fatal("自合并应被拒")
	}
}

// ③ 跨租户目标拒。
func TestFA_MergeCrossTenantRejected(t *testing.T) {
	svc, _, database, ctx := faRefluxSvc(t)
	src := faUQ(t, database, 1, "fa-merge-cross-src", model.UQStatusPendingReview)
	tgt := faUQ(t, database, 2, "fa-merge-cross-tgt", model.UQStatusPendingReview)
	_, err := svc.ReviewQuestion(ctx, src.ID, &AdoptRequest{Action: ActionMerge, MergedTargetID: tgt.ID}, "op")
	if err == nil {
		t.Fatal("跨租户合并目标应被拒")
	}
}

// ④ 双审核只一个成功：首个 adopt 成功后，第二个（读到旧 status）CAS 失败。
func TestFA_DoubleAdoptOnlyOneWins(t *testing.T) {
	svc, _, database, ctx := faRefluxSvc(t)
	uq := faUQ(t, database, 1, "fa-double-adopt", model.UQStatusPendingReview)
	// 手动模拟两个审核者都读到了 pending_review 快照。
	first, err := svc.ReviewQuestion(ctx, uq.ID, &AdoptRequest{Action: ActionAdopt, Answer: "a"}, "op1")
	if err != nil || first == nil {
		t.Fatalf("首次采纳应成功: %v", err)
	}
	// 此时库内已是 adopted，第二个审核者用旧快照再提交 → CAS 失败。
	_, err2 := svc.adoptSingle(ctx, uq, &AdoptRequest{Action: ActionAdopt, Answer: "a"}, "op2")
	if !errors.Is(err2, ErrIllegalTransition) {
		t.Fatalf("第二次采纳（旧状态）应 ErrIllegalTransition, got %v", err2)
	}
}

// ⑤ FAQ 成功后状态写失败 → FAQ 补偿删除，无孤稿。
// 通过把库中 status 预改为 adopted（使 UpdateQuestionCAS 的 pending_review 条件失配）来注入 CAS 失败。
func TestFA_OrphanFAQCompensated(t *testing.T) {
	svc, store, database, ctx := faRefluxSvc(t)
	uq := faUQ(t, database, 1, "fa-orphan-faq", model.UQStatusPendingReview)
	// 先篡改库中状态为 adopted，使随后 adoptSingle 读到的快照与 DB 不一致 → CAS 0 行。
	stale := *uq
	stale.Status = model.UQStatusAdopted
	if err := svc.repo.UpdateQuestion(ctx, uq.ID, map[string]any{"status": model.UQStatusAdopted}); err != nil {
		t.Fatalf("预改状态: %v", err)
	}
	// adoptSingle 使用传入的 uq（仍是 pending_review 快照）→ FAQ 写成功后 CAS 失败。
	_, err := svc.adoptSingle(ctx, uq, &AdoptRequest{Action: ActionAdopt, Answer: "a"}, "op")
	if err == nil {
		t.Fatal("CAS 失配应报错")
	}
	if len(store.items) != 0 || len(store.deleted) == 0 {
		t.Fatalf("孤立 FAQ 应被补偿删除, items=%v deleted=%v", store.items, store.deleted)
	}
}

// ⑥ 正常采纳幂等语义：重复以相同旧状态提交仅一个生效 FAQ。
func TestFA_AdoptProducesSingleFAQ(t *testing.T) {
	svc, store, database, ctx := faRefluxSvc(t)
	uq := faUQ(t, database, 1, "fa-single-faq", model.UQStatusPendingReview)
	if _, err := svc.ReviewQuestion(ctx, uq.ID, &AdoptRequest{Action: ActionAdopt, Answer: "a"}, "op"); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if len(store.items) != 1 {
		t.Fatalf("应恰 1 份生效 FAQ, got %d (%s)", len(store.items), fmt.Sprint(store.items))
	}
	// 复查库中问题状态。
	var got model.UnknownQuestion
	if err := database.First(&got, uq.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Status != model.UQStatusAdopted {
		t.Fatalf("状态应为 adopted, got %s", got.Status)
	}
}
