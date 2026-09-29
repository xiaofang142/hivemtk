// kb_release_test.go T-P9-02：发布制的存储层（带真库）。
//
// 本文件只测**只有 PostgreSQL 才证得出来**的那几件事，桩层（service/kb_release_test.go）
// 一条都不重复：
//
//   - 锁与幂等：LockRelease 的建行走 ON CONFLICT，SetGoverned 重复调用不重复留痕
//     （否则审计里会出现"启用了两次"这种查不出来源的行）；
//   - Publish 的五条内建判据 + **整体原子性**：任何一步失败后，库里既没有半套语料、
//     也没有半个指针、更没有那半套语料的留痕（apply_failed 这个态被删掉的全部依据）；
//   - 版本戳真的写进了 knowledge_chunks：闸门的判据就是这几列，写错一位数就是整卡失效，
//     而它在 service 的桩上永远看不出来；
//   - AC②（回滚是指针回拨）：回滚前后把整张语料表读回来逐字段比对，
//     断言的是"**一个字节都没动**"，不是"看起来还能用"；
//   - recalled_version 那道发布禁令：跨号发布会把刚撤下的内容静默放回来，
//     这条禁令是它唯一的拦阻，必须能红。
//
// 闸门本身（谓词形状、档位解析）在 internal/pkg/kbrelease 的用例里；这里只测
// "带真行的时候它数得对不对"。
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/kbrelease"
	"hivemtk-user/internal/pkg/testutil"
)

// ---------------------------------------------------------------------------
// 夹具
// ---------------------------------------------------------------------------

func kbRepoDB(t *testing.T) (*KBReleaseRepository, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t,
		&model.KBRelease{}, &model.KBChangeRequest{}, &model.KBChangeAuditLog{},
		&model.KnowledgeChunk{},
	)
	return NewKBReleaseRepository(db), db
}

// kbRepoProduct 每个用例一个独立库号：本文件里多处按 product_id 计数，
// 共号会让"这次发布写了几条"这种断言依赖上一个用例留下的行。
var kbRepoSeq int

func kbRepoProduct(t *testing.T) string {
	t.Helper()
	kbRepoSeq++
	return fmt.Sprintf("kbrepo_p%d", kbRepoSeq)
}

// kbRepoGovern 把一个库推进发布制（Publish 的第一条判据要求 governed=true）。
func kbRepoGovern(t *testing.T, r *KBReleaseRepository, product, actor string) {
	t.Helper()
	if _, err := r.SetGoverned(context.Background(), product, true, actor); err != nil {
		t.Fatalf("启用发布制失败: %v", err)
	}
}

// kbRepoChange 落一条待处理变更行，并返回可直接进 PublishIntent 的那一格。
//
// 变更行**必须真落库**：Publish 的状态回写是按 id 找行，缺行就是 0 行，
// 而"0 行要不要停下"正是本文件的一格判据（TestKBReleaseRepo_PublishRolledBackWhen...）。
func kbRepoChange(t *testing.T, db *gorm.DB, id, product, op, content string, target uint64) PublishChange {
	t.Helper()
	ch := &model.KBChangeRequest{
		ID: id, ProductID: product, Op: op, Content: content, Reason: "理由 " + id,
		Status: model.KBChangeStatusPending, ApprovalID: "apr_" + id, RequestedBy: "op-1",
		TargetChunkID: target, DocumentID: kbRepoDocOf(t, db, target),
	}
	if op == model.KBChangeOpAdd {
		ch.DocumentID = kbRepoSeqDoc()
	}
	if err := db.Create(ch).Error; err != nil {
		t.Fatalf("变更行落库失败: %v", err)
	}
	return PublishChange{
		ID: id, Op: op, ProductID: product, DocumentID: ch.DocumentID,
		Content: content, TargetChunkID: target,
	}
}

// kbRepoEnsureRow 只建一行、不开治理（测"未启用"这一态时用）。
//
// 走 LockRelease 就必须带事务：锁的存活期就是事务的存活期，脱离事务的 FOR UPDATE
// 在 autocommit 下锁立刻释放 —— 夹具自己先把它用错，等于给"锁其实没生效"留了后门。
func kbRepoEnsureRow(t *testing.T, r *KBReleaseRepository, product string) {
	t.Helper()
	err := r.db.Transaction(func(tx *gorm.DB) error {
		_, err := r.LockRelease(context.Background(), tx, product, "op-1")
		return err
	})
	if err != nil {
		t.Fatalf("建行失败: %v", err)
	}
}

// kbRepoDocOf 目标分段所属文档（revise/retire 时变更行沿用它，与真实提交链路一致）。
func kbRepoDocOf(t *testing.T, db *gorm.DB, chunkID uint64) uint64 {
	t.Helper()
	if chunkID == 0 {
		return 0
	}
	var doc uint64
	if err := db.Table("knowledge_chunks").Select("document_id").
		Where("id = ?", chunkID).Scan(&doc).Error; err != nil {
		t.Fatalf("读目标分段文档失败: %v", err)
	}
	return doc
}

// kbRepoSeqDoc 给 add 造一个不会撞号的 document_id（本夹具不做 FK，纯避重）。
func kbRepoSeqDoc() uint64 { return uint64(900000 + kbRepoSeq*100) }

// kbRepoSeed 直接落一条语料行（模拟"上一版已经在服"的存量）。
func kbRepoSeed(t *testing.T, db *gorm.DB, product string, doc uint64, idx int, content string, kbVersion, retired int) uint64 {
	t.Helper()
	row := &model.KnowledgeChunk{
		DocumentID: doc, ProductID: product, ChunkIndex: idx, Content: content,
		CharCount: len([]rune(content)), Weight: 1, SourceLanguage: "zh",
		EmbeddingSource: "tei", KBVersion: kbVersion, RetiredVersion: retired,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("语料行落库失败: %v", err)
	}
	return row.ID
}

// kbChunkSnapshot 读回某库全部语料行的**形状**（含已退役的），用于"一个字节都没动"类断言。
type kbChunkSnapshot struct {
	ID         uint64 `gorm:"column:id"`
	Doc        uint64 `gorm:"column:document_id"`
	Idx        int    `gorm:"column:chunk_index"`
	Content    string `gorm:"column:content"`
	KBVersion  int    `gorm:"column:kb_version"`
	Retired    int    `gorm:"column:retired_version"`
	ChangeName string `gorm:"column:change_id"`
}

func kbRepoSnapshot(t *testing.T, db *gorm.DB, product string) []kbChunkSnapshot {
	t.Helper()
	var rows []kbChunkSnapshot
	err := db.Table("knowledge_chunks").
		Select("id, document_id, chunk_index, content, kb_version, retired_version, change_id").
		Where("product_id = ?", product).
		Order("id ASC").
		Scan(&rows).Error
	if err != nil {
		t.Fatalf("语料快照读取失败: %v", err)
	}
	return rows
}

func kbRepoRelease(t *testing.T, r *KBReleaseRepository, product string) *model.KBRelease {
	t.Helper()
	rel, err := r.GetRelease(context.Background(), product)
	if err != nil {
		t.Fatalf("读发布指针失败: %v", err)
	}
	return rel
}

func kbRepoAuditCount(t *testing.T, db *gorm.DB, subject, action string) int {
	t.Helper()
	var n int64
	err := db.Table("kb_change_audit_logs").
		Where("subject_key = ? AND action = ?", subject, action).Count(&n).Error
	if err != nil {
		t.Fatalf("审计计数失败: %v", err)
	}
	return int(n)
}

// kbRepoAuditFor 读回某对象某动作的全部留痕行（走仓储自己的读通路，含排序与夹逼）。
//
// 只数条数的话，"改前/改后两格写反"与"applied 数取自别的变量"都能绿着过去，
// 而运营查"谁在什么时候把哪个号换成了哪个号"要的正是这三格数字。
func kbRepoAuditFor(t *testing.T, r *KBReleaseRepository, subject, action string) []model.KBChangeAuditLog {
	t.Helper()
	rows, err := r.ListAuditBySubject(context.Background(), subject, 200)
	if err != nil {
		t.Fatalf("留痕读取失败: %v", err)
	}
	out := make([]model.KBChangeAuditLog, 0, len(rows))
	for _, row := range rows {
		if row.Action == action {
			out = append(out, row)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// 锁与幂等
// ---------------------------------------------------------------------------

// TestKBReleaseRepo_LockReleaseEnsuresRow 锁一扇同时管两件事：建行幂等、且不越权开治理。
//
// 拿 effective_version>0 当"已治理"是本卡最容易顺手写错的一处：一个刚被查到状态的库
// 此刻 effective 就是 0，如果建行时顺手 governed=true，那么"运营点开看一眼"就把这个库
// 推进了发布制（此后所有导入都被挡在待发布桶里）。
func TestKBReleaseRepo_LockReleaseEnsuresRow(t *testing.T) {
	ctx := context.Background()
	r, db := kbRepoDB(t)
	product := kbRepoProduct(t)

	if _, err := r.GetRelease(ctx, product); !errors.Is(err, ErrKBReleaseNotFound) {
		t.Errorf("没有行时必须报 ErrKBReleaseNotFound（与\"读不出来\"分开），实得 %v", err)
	}

	var first, second *model.KBRelease
	err := db.Transaction(func(tx *gorm.DB) error {
		var e error
		first, e = r.LockRelease(ctx, tx, product, "op-1")
		if e != nil {
			return e
		}
		second, e = r.LockRelease(ctx, tx, product, "op-1")
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == 0 || second.ID != first.ID {
		t.Fatalf("两次锁行必须落在同一行：%+v / %+v", first, second)
	}
	if first.Governed {
		t.Error("LockRelease 建的那一行必须 governed=false：查一次状态不等于开启治理")
	}
	if first.AllocatedVersion != 0 || first.DraftVersion != 0 || first.EffectiveVersion != 0 {
		t.Errorf("新建行三个号都该是 0：%+v", first)
	}
	// 锁必须在事务里：脱离事务的 FOR UPDATE 语句在 autocommit 下锁立刻释放，
	// 而代码看上去"加了锁"，只有并发才会露馅 —— 所以这里判"拒绝"而不是"退化成无锁读"。
	if _, err := r.LockRelease(ctx, nil, product, "op-1"); err == nil {
		t.Error("tx=nil 必须报错：无事务的锁等于没锁")
	}
	// created_at/updated_at 由那句裸 SQL 写死：漏一列就是 NULL，而 model 上是非指针的
	// time.Time —— 读回来那一下直接报错，报的还不是"发布制坏了"这种可归因的错。
	var stamped int64
	if err := db.Table("kb_releases").
		Where("product_id = ? AND (created_at IS NULL OR updated_at IS NULL)", product).
		Count(&stamped).Error; err != nil {
		t.Fatal(err)
	}
	if stamped != 0 {
		t.Errorf("kb_releases 出现 NULL 时间戳行 %d 条：裸 SQL 建行必须自己写这两列", stamped)
	}
}

// TestKBReleaseRepo_SetGovernedIdempotent 重复启用不该再记一条审计。
//
// 留痕里的 old/new 必须取"改前改后"两个真值：由目标值反推旧值（!governed）在一次
// 判断改动后就会把留痕写反，而写反的审计比没有审计更糟 —— 它会让人相信翻转过。
func TestKBReleaseRepo_SetGovernedIdempotent(t *testing.T) {
	ctx := context.Background()
	r, db := kbRepoDB(t)
	product := kbRepoProduct(t)

	var anchor *model.KBRelease
	for i := 0; i < 3; i++ {
		rel, err := r.SetGoverned(ctx, product, true, "op-1")
		if err != nil {
			t.Fatalf("第 %d 次启用失败: %v", i+1, err)
		}
		if !rel.Governed {
			t.Fatalf("第 %d 次启用后读回 governed=false", i+1)
		}
		if i == 0 {
			anchor = rel
			continue
		}
		// 幂等的那两次不许再碰这一行：changed_by/updated_at 记的是"最后一次真改了它的人与时刻"，
		// 空转一次写会把这两个读数改成一次什么都没改的点击。
		if rel.ChangedBy != anchor.ChangedBy || !rel.UpdatedAt.Equal(anchor.UpdatedAt) {
			t.Errorf("第 %d 次重复启用不该改写行：changed_by %q→%q，updated_at %v→%v",
				i+1, anchor.ChangedBy, rel.ChangedBy, anchor.UpdatedAt, rel.UpdatedAt)
		}
	}
	if n := kbRepoAuditCount(t, db, "release:"+product, model.KBAuditGoverned); n != 1 {
		t.Errorf("三次启用只该留一条痕，实得 %d 条", n)
	}
	if _, err := r.SetGoverned(ctx, product, false, "op-2"); err != nil {
		t.Fatal(err)
	}
	var rows []model.KBChangeAuditLog
	if err := db.Where("subject_key = ? AND action = ?", "release:"+product, model.KBAuditGoverned).
		Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	// 条数不成立时后面的下标没有意义：判成 Errorf 会让下面那两行按下标取值时越界 panic，
	// 一个 panic 带走整个测试二进制，本包其余用例连"红"都记不上。
	if len(rows) != 2 {
		t.Fatalf("启停各该留一条痕，实得 %d 条：%+v", len(rows), rows)
	}
	if rows[0].OldValue != "false" || rows[0].NewValue != "true" ||
		rows[1].OldValue != "true" || rows[1].NewValue != "false" {
		t.Errorf("留痕的改前改后写反或缺项：%+v", rows)
	}
	if rows[1].Actor != "op-2" {
		t.Errorf("停用是谁按的要留得下来：%+v", rows[1])
	}
	// 停用只翻这一列：已生效的版本号必须原样留着，重新开启时才不需要重建任何状态。
	rel := kbRepoRelease(t, r, product)
	if rel.EffectiveVersion != 0 || rel.Governed {
		t.Errorf("停用后指针形状不对：%+v", rel)
	}

	// 只启用的库进列表：运营看这张表判断"哪些库被管着"，未启用的空行是噪音。
	if _, err := r.SetGoverned(ctx, kbRepoProduct(t), true, "op-1"); err != nil {
		t.Fatal(err)
	}
	list, err := r.ListReleases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("ListReleases 只该给出治理中的库，实得 %d 条：%+v", len(list), list)
	}
}

// ---------------------------------------------------------------------------
// Publish：五条内建判据 + 原子性
// ---------------------------------------------------------------------------

// TestKBReleaseRepo_PublishJudgments 把 Publish 里"该拒绝"的判据一格一格判到。
//
// 每条都附"放过的代价"，因为这几条的失效方向全是**放行**（多发布出去内容），
// 而多放的那部分没有任何一次审批批准过它。
func TestKBReleaseRepo_PublishJudgments(t *testing.T) {
	ctx := context.Background()

	t.Run("没进发布制的库拒绝发布", func(t *testing.T) {
		r, _ := kbRepoDB(t)
		product := kbRepoProduct(t)
		// 只建行、不启用治理（LockRelease 走一次即建行）。
		kbRepoEnsureRow(t, r, product)
		_, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-1"})
		if !errors.Is(err, ErrKBReleaseNotGoverned) {
			t.Errorf("实得 %v", err)
		}
		if !strings.Contains(err.Error(), product) {
			t.Errorf("报错要点名是哪个库：%q", err.Error())
		}
	})

	t.Run("既没变更又没待发布桶", func(t *testing.T) {
		r, _ := kbRepoDB(t)
		product := kbRepoProduct(t)
		kbRepoGovern(t, r, product, "op-1")
		if _, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-1"}); !errors.Is(err, ErrKBReleaseNothingToPublish) {
			t.Errorf("实得 %v", err)
		}
		rel := kbRepoRelease(t, r, product)
		if rel.EffectiveVersion != 0 || rel.PreviousVersion != 0 || rel.AllocatedVersion != 0 {
			t.Errorf("空移指针会留下一次不存在的发布：%+v", rel)
		}
	})

	t.Run("桶号不高于在服版本就停下", func(t *testing.T) {
		r, db := kbRepoDB(t)
		product := kbRepoProduct(t)
		kbRepoGovern(t, r, product, "op-1")
		// 账目被外部改过的形态（正常走不到）：宁可拒绝，也不能把新内容打在"已经在服"的号上。
		if err := db.Table("kb_releases").Where("product_id = ?", product).
			UpdateColumns(map[string]any{"effective_version": 5, "draft_version": 3, "allocated_version": 5}).Error; err != nil {
			t.Fatal(err)
		}
		_, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-1"})
		if err == nil || !strings.Contains(err.Error(), "不高于在服版本") {
			t.Errorf("实得 %v", err)
		}
	})

	t.Run("跨号发布被禁令挡住", func(t *testing.T) {
		r, db := kbRepoDB(t)
		product := kbRepoProduct(t)
		kbRepoGovern(t, r, product, "op-1")
		if err := db.Table("kb_releases").Where("product_id = ?", product).
			UpdateColumns(map[string]any{"effective_version": 3, "recalled_version": 5}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-1"}); !errors.Is(err, ErrKBReleaseRecallBan) {
			t.Errorf("禁令没开火：跨过一次被回滚的版本会静默复活那段语料，实得 %v", err)
		}
	})

	t.Run("缺归属或操作者", func(t *testing.T) {
		r, _ := kbRepoDB(t)
		if _, err := r.Publish(ctx, PublishIntent{Actor: "op"}); err == nil {
			t.Error("缺 product_id 要报错")
		}
		if _, err := r.Publish(ctx, PublishIntent{ProductID: kbRepoProduct(t)}); err == nil {
			t.Error("缺 actor 要报错：发布必须归人")
		}
	})

	t.Run("未装配底座", func(t *testing.T) {
		var nilRepo *KBReleaseRepository
		if _, err := nilRepo.Publish(ctx, PublishIntent{ProductID: "p", Actor: "a"}); err == nil {
			t.Error("nil 接收者要报错而不是 panic")
		}
		if nilRepo.Available() {
			t.Error("nil 接收者的 Available 该答 false")
		}
	})
}

// TestKBReleaseRepo_PublishRejectsBadChanges 变更格自身的约束在落库前判：
// add 缺内容/缺文档、未知动作、revise/retire 的目标与当前语料对不上。
//
// 这一组全都在事务**内部**判，所以判红的同一句就把前面已经写下的语料一起回滚 ——
// 这就是本卡不留 apply_failed 这个态的全部依据，下一格专门测它。
func TestKBReleaseRepo_PublishRejectsBadChanges(t *testing.T) {
	ctx := context.Background()
	r, db := kbRepoDB(t)
	product := kbRepoProduct(t)
	kbRepoGovern(t, r, product, "op-1")
	doc := kbRepoSeqDoc()
	keep := kbRepoSeed(t, db, product, doc, 0, "在服正文", 0, 0)
	other := kbRepoProduct(t)
	otherDoc := kbRepoSeqDoc() + 1
	retired := kbRepoSeed(t, db, other, otherDoc, 0, "别库正文", 0, 0)

	cases := []struct {
		name     string
		change   PublishChange
		wantErr  string
		sentinel error
	}{
		{"add 无内容", PublishChange{ID: "kbc_a1", Op: model.KBChangeOpAdd, ProductID: product, DocumentID: doc}, "必须有内容", nil},
		{"add 无文档", PublishChange{ID: "kbc_a2", Op: model.KBChangeOpAdd, ProductID: product, Content: "正文"}, "必须指定 document_id", nil},
		{"未知动作", PublishChange{ID: "kbc_a3", Op: "sideways", ProductID: product, Content: "正文"}, "未知变更动作", nil},
		{"revise 目标不存在", PublishChange{ID: "kbc_a4", Op: model.KBChangeOpRevise, ProductID: product, Content: "新正文", TargetChunkID: keep + 9999}, "不存在", ErrKBReleaseTargetConflict},
		{"retire 目标不存在", PublishChange{ID: "kbc_a5", Op: model.KBChangeOpRetire, ProductID: product, TargetChunkID: keep + 9999}, "不存在", ErrKBReleaseTargetConflict},
		{"retire 目标属于别库", PublishChange{ID: "kbc_a6", Op: model.KBChangeOpRetire, ProductID: product, TargetChunkID: retired}, "与本变更的库", ErrKBReleaseTargetConflict},
		{"revise 目标属于别库", PublishChange{ID: "kbc_a7", Op: model.KBChangeOpRevise, ProductID: product, Content: "新正文", TargetChunkID: retired}, "与本变更的库", ErrKBReleaseTargetConflict},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-1", Changes: []PublishChange{c.change}})
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("要的错误 %q 没出现，实得 %v", c.wantErr, err)
			}
			if c.sentinel != nil && !errors.Is(err, c.sentinel) {
				t.Errorf("目标类冲突要用 sentinel 判到（控制层据此回 409）：%v", err)
			}
			rel := kbRepoRelease(t, r, product)
			if rel.EffectiveVersion != 0 {
				t.Errorf("判据拒了却仍移了指针：%+v", rel)
			}
		})
	}

	// 已退役的分段不能再修订：第二次"下线同一条"同样是 0 行，必须报错而不是静默放过。
	if err := db.Table("knowledge_chunks").Where("id = ?", keep).
		Update("retired_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	_, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-1", Changes: []PublishChange{
		{ID: "kbc_a8", Op: model.KBChangeOpRevise, ProductID: product, Content: "新正文", TargetChunkID: keep},
	}})
	if err == nil || !strings.Contains(err.Error(), "不能再次修订") {
		t.Errorf("已退役目标要拒，实得 %v", err)
	}
	if !errors.Is(err, ErrKBReleaseTargetConflict) {
		t.Errorf("目标冲突的 sentinel 身份丢了：%v", err)
	}
}

// TestKBReleaseRepo_PublishRolledBackWhenChangeNotPending 变更行在"读待办 → 进事务"之间
// 被人撤回时，整次发布必须回滚。
//
// 这是本层唯一一处"service 的结论会过期"的窗口：Publish 只锁 kb_releases 那一行，
// 不锁变更行，所以状态回写的 CAS 命中 0 行时必须停下。放过的表现是"正文已经进语料、
// 变更行却写着 withdrawn"，而那次撤回在审计里查无此事。
func TestKBReleaseRepo_PublishRolledBackWhenChangeNotPending(t *testing.T) {
	ctx := context.Background()
	r, db := kbRepoDB(t)
	product := kbRepoProduct(t)
	kbRepoGovern(t, r, product, "op-1")

	ok := kbRepoChange(t, db, "kbc_live", product, model.KBChangeOpAdd, "第一条正文", 0)
	gone := kbRepoChange(t, db, "kbc_gone", product, model.KBChangeOpAdd, "被撤回的正文", 0)
	if affected, err := r.WithdrawChange(ctx, "kbc_gone"); err != nil || !affected {
		t.Fatalf("夹具撤回没生效: %v %v", affected, err)
	}

	before := kbRepoSnapshot(t, db, product)
	_, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-1", Changes: []PublishChange{ok, gone}})
	if !errors.Is(err, ErrKBReleaseChangeNotPending) {
		t.Fatalf("要的是发布回滚，实得 %v", err)
	}
	if !strings.Contains(err.Error(), "kbc_gone") {
		t.Errorf("报错要点名是哪条变更被撤回：%q", err.Error())
	}
	// 整次发布没发生：语料一条没多、指针没动、那次发布的留痕也没留下。
	if after := kbRepoSnapshot(t, db, product); len(after) != len(before) {
		t.Errorf("回滚不彻底：语料从 %d 条变成 %d 条", len(before), len(after))
	}
	rel := kbRepoRelease(t, r, product)
	if rel.EffectiveVersion != 0 || rel.DraftVersion != 0 || rel.AllocatedVersion != 0 {
		t.Errorf("指针不该动：%+v", rel)
	}
	if n := kbRepoAuditCount(t, db, "release:"+product, model.KBAuditPublished); n != 0 {
		t.Errorf("留痕里不该有一次没发生的发布，实得 %d 条", n)
	}
	if got, err := r.GetChange(ctx, "kbc_live"); err != nil || got.Status != model.KBChangeStatusPending {
		t.Errorf("被回滚的那条变更要回到待处理：status=%v err=%v", got, err)
	}
}

// ---------------------------------------------------------------------------
// Publish：正常路径写进语料的形状
// ---------------------------------------------------------------------------

// TestKBReleaseRepo_PublishAddStampsAndAdvances add 的三件产物一起判：
// 语料行带对版本戳与变更号、同文档两条不会拿到同一个分段号、变更行被指认到新行。
func TestKBReleaseRepo_PublishAddStampsAndAdvances(t *testing.T) {
	ctx := context.Background()
	r, db := kbRepoDB(t)
	product := kbRepoProduct(t)
	kbRepoGovern(t, r, product, "op-1")
	doc := kbRepoSeqDoc()

	c1 := kbRepoChange(t, db, "kbc_1", product, model.KBChangeOpAdd, "正文一", 0)
	c1.DocumentID = doc
	c2 := kbRepoChange(t, db, "kbc_2", product, model.KBChangeOpAdd, "正文二", 0)
	c2.DocumentID = doc

	res, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-1", Changes: []PublishChange{c1, c2}})
	if err != nil {
		t.Fatal(err)
	}
	if res.FromVersion != 0 || res.ToVersion != 1 {
		t.Errorf("首次发布 v0→v1，实得 %+v", res)
	}
	if len(res.Applied) != 2 {
		t.Fatalf("Applied=%+v", res.Applied)
	}

	rows := kbRepoSnapshot(t, db, product)
	if len(rows) != 2 {
		t.Fatalf("语料应有两条：%+v", rows)
	}
	// 同一文档连续两条必须拿到不同分段号：(document_id,chunk_index) 上没有唯一约束兜着，
	// 同号的话重建与展示都按号取第一条，第二条永远检索不到。
	if rows[0].Idx == rows[1].Idx || rows[0].Doc != doc || rows[1].Doc != doc {
		t.Errorf("分段号/归属错：%+v", rows)
	}
	for i, row := range rows {
		if row.KBVersion != 1 || row.Retired != 0 {
			t.Errorf("第 %d 条版本戳错：kb=%d retired=%d（闸门就按这两列判）", i+1, row.KBVersion, row.Retired)
		}
		if row.ChangeName != res.Applied[i].ChangeID {
			t.Errorf("第 %d 条的 change_id 指认错：%s vs %+v", i+1, row.ChangeName, res.Applied[i])
		}
		if res.Applied[i].AppliedChunkID != row.ID {
			t.Errorf("回给 service 补向量的新行号错：%+v vs %+v", res.Applied[i], row)
		}
	}
	for _, id := range []string{"kbc_1", "kbc_2"} {
		got, err := r.GetChange(ctx, id)
		if err != nil || got == nil {
			t.Fatalf("读回 %s: %v", id, err)
		}
		if got.Status != model.KBChangeStatusApplied || got.ReleaseVersion != 1 {
			t.Errorf("%s 状态回写错：%+v", id, got)
		}
		if got.AppliedChunkID == 0 {
			t.Errorf("%s 没记下落到哪条分段（回滚时要靠它指认）", id)
		}
		if n := kbRepoAuditCount(t, db, "change:"+id, model.KBAuditPublished); n != 1 {
			t.Errorf("%s 的发布留痕 %d 条", id, n)
		}
	}
	rel := kbRepoRelease(t, r, product)
	if rel.EffectiveVersion != 1 || rel.AllocatedVersion != 1 || rel.DraftVersion != 0 || rel.PreviousVersion != 0 {
		t.Errorf("发布后指针形状错：%+v", rel)
	}
	if n := kbRepoAuditCount(t, db, "release:"+product, model.KBAuditPublished); n != 1 {
		t.Errorf("发布留痕 %d 条", n)
	}
	// 留痕的**内容**：改前读了哪两格、改后写了哪三格（一次发布移动的是四个号，
	// 而审计上说得清的只有这一句话，写反了就没法复盘"当时在服是第几版"）。
	if traces := kbRepoAuditFor(t, r, "release:"+product, model.KBAuditPublished); len(traces) != 1 {
		t.Fatalf("发布留痕读回 %d 条（期望 1）", len(traces))
	} else {
		tr := traces[0]
		if tr.OldValue != "effective=0 draft=0" {
			t.Errorf("首次发布的改前读数=%q，期望 effective=0 draft=0", tr.OldValue)
		}
		if tr.NewValue != "effective=1 previous=0 applied=2" {
			t.Errorf("首次发布的改后读数=%q，期望 effective=1 previous=0 applied=2", tr.NewValue)
		}
		if tr.Actor != "op-1" {
			t.Errorf("留痕操作者=%q，期望 op-1（这次发布的按的人）", tr.Actor)
		}
	}

	// 第二次发布拿的是高水位的下一格：号与时间顺序永远同向，回滚不会复用旧号。
	//
	// 这一批故意发三条：两条 add 量不到"记了号却不往后推"这个坏法 —— 第二条拿到的号来自
	// 查询本身，只有第三条才依赖上一条把本次事务里的计数器推上去（同号的两条分段
	// 在 (document_id,chunk_index) 上没有唯一约束兜着，第二条从此检索不到）。
	c3 := kbRepoChange(t, db, "kbc_3", product, model.KBChangeOpAdd, "正文三", 0)
	c3.DocumentID = doc
	c3b := kbRepoChange(t, db, "kbc_3b", product, model.KBChangeOpAdd, "正文三乙", 0)
	c3b.DocumentID = doc
	c3c := kbRepoChange(t, db, "kbc_3c", product, model.KBChangeOpAdd, "正文三丙", 0)
	c3c.DocumentID = doc
	res2, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-2",
		Changes: []PublishChange{c3, c3b, c3c}})
	if err != nil {
		t.Fatal(err)
	}
	if res2.FromVersion != 1 || res2.ToVersion != 2 {
		t.Errorf("第二次 v1→v2，实得 %+v", res2)
	}
	var seqs []int
	if err := db.Table("knowledge_chunks").
		Where("change_id IN ?", []string{"kbc_3", "kbc_3b", "kbc_3c"}).
		Order("chunk_index ASC").Pluck("chunk_index", &seqs).Error; err != nil {
		t.Fatal(err)
	}
	if len(seqs) != 3 || seqs[0] == seqs[1] || seqs[1] == seqs[2] {
		t.Errorf("同批三条 add 的分段号要互不相同（拿到同号的那条从此检索不到）：%v", seqs)
	}
	rel = kbRepoRelease(t, r, product)
	if rel.EffectiveVersion != 2 || rel.PreviousVersion != 1 {
		t.Errorf("第二次发布后指针错：%+v", rel)
	}
}

// TestKBReleaseRepo_PublishPublishesDraftBucket 导入侧预先占下的待发布桶，
// 一次"零变更"的发布也要能把它整体上线（这是"导入也走发布制"那格扩面的落点）。
func TestKBReleaseRepo_PublishPublishesDraftBucket(t *testing.T) {
	ctx := context.Background()
	r, db := kbRepoDB(t)
	product := kbRepoProduct(t)
	kbRepoGovern(t, r, product, "op-1")
	doc := kbRepoSeqDoc()

	bucket, err := kbrelease.EnsureDraftStamp(ctx, db, product)
	if err != nil || bucket != 1 {
		t.Fatalf("导入侧打桶：bucket=%d err=%v", bucket, err)
	}
	imported := kbRepoSeed(t, db, product, doc, 0, "导入的正文", bucket, 0)
	t.Setenv(kbrelease.FlagEnv, "on")
	if inForce, _, err := r.CountChunksInForce(ctx, product); err != nil || inForce != 0 {
		t.Fatalf("未发布前它不该在服：inForce=%d err=%v", inForce, err)
	}
	// 导入的内容带着待发布号落库：此刻它在 on 档不可见（可见性那格另测）。
	if _, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-1"}); err != nil {
		t.Fatalf("有待发布桶就不算\"没内容可发\": %v", err)
	}
	rel := kbRepoRelease(t, r, product)
	if rel.EffectiveVersion != bucket || rel.DraftVersion != 0 {
		t.Errorf("桶没被整体上线：%+v", rel)
	}
	if ids := kbVisibleIDs(t, db, product); len(ids) != 1 || ids[0] != imported {
		t.Errorf("发布后要恰好放出那条导入内容（on 档、按谓词读）：%v vs %d", ids, imported)
	}
	// 桶号复用：同一批导入的多条 chunk 落同一个桶，一次发布整体上线。
	again, err := kbrelease.EnsureDraftStamp(ctx, db, product)
	if err != nil {
		t.Fatal(err)
	}
	if again != 2 {
		t.Errorf("发布后新批次要拿高水位的下一格，实得 %d", again)
	}
}

// TestKBReleaseRepo_ReviseAndRetireShape 修订是"插新行 + 退役老行"，下线只退役。
//
// 新行沿用老行的分段号：替换要落在原位，检索结果的排序与上下文邻接才不变。
func TestKBReleaseRepo_ReviseAndRetireShape(t *testing.T) {
	ctx := context.Background()
	r, db := kbRepoDB(t)
	product := kbRepoProduct(t)
	kbRepoGovern(t, r, product, "op-1")
	doc := kbRepoSeqDoc()
	old := kbRepoSeed(t, db, product, doc, 7, "老正文", 0, 0)
	toRetire := kbRepoSeed(t, db, product, doc, 8, "要下线的正文", 0, 0)

	c1 := kbRepoChange(t, db, "kbc_rev", product, model.KBChangeOpRevise, "新正文", old)
	c2 := kbRepoChange(t, db, "kbc_ret", product, model.KBChangeOpRetire, "", toRetire)
	res, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-1", Changes: []PublishChange{c1, c2}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied[1].AppliedChunkID != 0 {
		t.Errorf("retire 不该回新行号：%+v", res.Applied[1])
	}

	// 逐行核三件事：老行在第 1 版退役、新行在第 1 版生效且号位不变、被下线那条只退役不插新行。
	var oldRow, retiredRow *kbChunkSnapshot
	all := kbRepoSnapshot(t, db, product)
	for i := range all {
		switch all[i].ID {
		case old:
			oldRow = &all[i]
		case toRetire:
			retiredRow = &all[i]
		}
	}
	if oldRow == nil || oldRow.Retired != 1 || oldRow.Content != "老正文" {
		t.Errorf("被替换的老行形状错（内容必须原样留着，回滚靠它）：%+v", oldRow)
	}
	if retiredRow == nil || retiredRow.Retired != 1 {
		t.Errorf("被下线的那条要退役：%+v", retiredRow)
	}
	var inserted kbChunkSnapshot
	if err := db.Table("knowledge_chunks").
		Select("id, document_id, chunk_index, content, kb_version, retired_version, change_id").
		Where("change_id = ?", "kbc_rev").Scan(&inserted).Error; err != nil {
		t.Fatal(err)
	}
	if inserted.ID == 0 || inserted.Idx != 7 || inserted.KBVersion != 1 || inserted.Retired != 0 {
		t.Errorf("修订新行要落在原位并带生效号：%+v", inserted)
	}
	if inserted.Doc != doc {
		t.Errorf("文档归属要从被替换那行继承：%+v", inserted)
	}
	// 上面那条腿用的是"变更自带归属、老行又正好在这个文档下"的夹具，两条通路读出来的结果
	// 相同 ⇒ 删掉继承分支它照样绿。这一条把归属摘掉、老行挪到另一个文档，才量得到继承本身。
	doc2 := kbRepoSeqDoc()
	src2 := kbRepoSeed(t, db, product, doc2, 2, "另一篇文档的老正文", 0, 0)
	c4 := kbRepoChange(t, db, "kbc_rev2", product, model.KBChangeOpRevise, "改后正文", src2)
	if _, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-1",
		Changes: []PublishChange{{ID: c4.ID, Op: c4.Op, ProductID: c4.ProductID,
			Content: c4.Content, TargetChunkID: c4.TargetChunkID}}}); err != nil {
		t.Fatalf("变更不带归属的修订要能发: %v", err)
	}
	var inherited kbChunkSnapshot
	if err := db.Table("knowledge_chunks").
		Select("id, document_id, chunk_index, content, kb_version, retired_version, change_id").
		Where("change_id = ?", "kbc_rev2").Scan(&inherited).Error; err != nil {
		t.Fatal(err)
	}
	if inherited.ID == 0 || inherited.Doc != doc2 || inherited.Idx != 2 {
		t.Errorf("不带归属的修订要落到被替换那行的文档与号位上：%+v", inherited)
	}
	// 已退役的分段再被"下线"一次必须报错：静默放过等于"同一条被下线两次"，第二条变更
	// 会在下一次发布里凭空消失，而它的审批记录写着"已批准"。
	again := kbRepoChange(t, db, "kbc_ret2", product, model.KBChangeOpRetire, "", toRetire)
	if _, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-1",
		Changes: []PublishChange{again}}); err == nil ||
		!strings.Contains(err.Error(), "已被退役") {
		t.Errorf("第二次下线同一条要报错，实得 %v", err)
	}
	if got, err := r.GetChange(ctx, "kbc_ret2"); err != nil || got.Status != model.KBChangeStatusPending {
		t.Errorf("失败的那条变更要留在待处理态（这次发布没发生）：%+v %v", got, err)
	}
}

// TestKBReleaseRepo_RollbackTouchesNoCorpus AC② 的正证：回滚前后整张语料表逐字段相同。
//
// 断言打在"字节没动"上而不是"检索还能用"上：后者在实现悄悄改成"回滚时把新行删掉"
// 之后仍然会绿一段时间，而那时回滚窗口已经永久失去了依据。
func TestKBReleaseRepo_RollbackTouchesNoCorpus(t *testing.T) {
	ctx := context.Background()
	r, db := kbRepoDB(t)
	product := kbRepoProduct(t)
	kbRepoGovern(t, r, product, "op-1")
	doc := kbRepoSeqDoc()

	c1 := kbRepoChange(t, db, "kbc_r1", product, model.KBChangeOpAdd, "第一版正文", 0)
	c1.DocumentID = doc
	if _, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-1", Changes: []PublishChange{c1}}); err != nil {
		t.Fatal(err)
	}
	c2 := kbRepoChange(t, db, "kbc_r2", product, model.KBChangeOpAdd, "第二版正文", 0)
	c2.DocumentID = doc + 1
	if _, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-1", Changes: []PublishChange{c2}}); err != nil {
		t.Fatal(err)
	}
	frozen := kbRepoSnapshot(t, db, product)
	if len(frozen) != 2 {
		t.Fatalf("夹具该有两条语料：%+v", frozen)
	}

	rb, err := r.Rollback(ctx, product, "op-2")
	if err != nil {
		t.Fatal(err)
	}
	if rb.FromVersion != 2 || rb.ToVersion != 1 || rb.Recalled != 2 {
		t.Errorf("回滚读数错：%+v", rb)
	}
	if after := kbRepoSnapshot(t, db, product); !kbSameChunks(frozen, after) {
		t.Errorf("AC② 破了：回滚改动了语料行\n改前 %+v\n改后 %+v", frozen, after)
	}
	rel := kbRepoRelease(t, r, product)
	if rel.EffectiveVersion != 1 || rel.PreviousVersion != 2 || rel.RecalledVersion != 2 {
		t.Errorf("回滚后指针错：%+v", rel)
	}
	// applied 是终态：那条改动确实生效过一次，这是历史事实，"当前是否在服"由指针回答。
	got, err := r.GetChange(ctx, "kbc_r2")
	if err != nil || got.Status != model.KBChangeStatusApplied || got.ReleaseVersion != 2 {
		t.Errorf("回滚不许改写变更历史：%+v err=%v", got, err)
	}
	if n := kbRepoAuditCount(t, db, "release:"+product, model.KBAuditRollback); n != 1 {
		t.Errorf("回滚留痕 %d 条", n)
	}

	// 连点两下不能穿到更老的历史：previous 在回滚后已是"刚在服的号"，回滚判据随即拒。
	if _, err := r.Rollback(ctx, product, "op-2"); !errors.Is(err, ErrKBReleaseRollbackUnavailable) {
		t.Errorf("第二次回滚要拒，实得 %v", err)
	}
	// 禁令在位时发布也要拒（否则跨号静默复活）。
	if _, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-2"}); !errors.Is(err, ErrKBReleaseRecallBan) {
		t.Errorf("被回滚的版本未处置时禁止发布，实得 %v", err)
	}

	// Restore 是精确逆操作：放回那一版、禁令解除、previous 又变成"刚离开的那个号"。
	rs, err := r.Restore(ctx, product, "op-3")
	if err != nil {
		t.Fatal(err)
	}
	if rs.FromVersion != 1 || rs.ToVersion != 2 || rs.Recalled != 0 {
		t.Errorf("放回读数错：%+v", rs)
	}
	if after := kbRepoSnapshot(t, db, product); !kbSameChunks(frozen, after) {
		t.Errorf("放回也不许动语料：%+v", after)
	}
	rel = kbRepoRelease(t, r, product)
	if rel.EffectiveVersion != 2 || rel.RecalledVersion != 0 || rel.PreviousVersion != 1 {
		t.Errorf("放回后指针错（previous 该是刚离开的那个号）：%+v", rel)
	}
	if n := kbRepoAuditCount(t, db, "release:"+product, model.KBAuditRestored); n != 1 {
		t.Errorf("放回留痕 %d 条", n)
	}
	// 禁令解除后才能再发一次（这里攒一格新桶，免得撞上 NothingToPublish）。
	c4 := kbRepoChange(t, db, "kbc_r4", product, model.KBChangeOpAdd, "第三版正文", 0)
	c4.DocumentID = doc + 2
	res, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-3", Changes: []PublishChange{c4}})
	if err != nil {
		t.Fatalf("禁令解除后发布该放行：%v", err)
	}
	if res.FromVersion != 2 || res.ToVersion != 3 {
		t.Errorf("再发布要接在高水位的下一格：%+v", res)
	}
}

// TestKBReleaseRepo_PointerMovesRefuseCleanRows 从没发布过 / 没被回滚过的库上
// 按回滚与放回，拿到的必须是两条业务结论而不是 500。
func TestKBReleaseRepo_PointerMovesRefuseCleanRows(t *testing.T) {
	ctx := context.Background()
	r, _ := kbRepoDB(t)

	fresh := kbRepoProduct(t)
	kbRepoGovern(t, r, fresh, "op-1")
	if _, err := r.Rollback(ctx, fresh, "op-1"); !errors.Is(err, ErrKBReleaseRollbackUnavailable) {
		t.Errorf("没发布过就该答\"没有上一版\"，实得 %v", err)
	}
	if _, err := r.Restore(ctx, fresh, "op-1"); !errors.Is(err, ErrKBReleaseRestoreUnavailable) {
		t.Errorf("没回滚过就该答\"没有可放回的版本\"，实得 %v", err)
	}
	// 未启用治理的库同样只翻开关：回滚不借 governed 这道锁开后门。
	plain := kbRepoProduct(t)
	if _, err := r.Rollback(ctx, plain, "op-1"); !errors.Is(err, ErrKBReleaseRollbackUnavailable) {
		t.Errorf("实得 %v", err)
	}
	var nilRepo *KBReleaseRepository
	if _, err := nilRepo.Rollback(ctx, "p", "a"); err == nil {
		t.Error("nil 接收者的 Rollback 要报错")
	}
	if _, err := nilRepo.Restore(ctx, "p", "a"); err == nil {
		t.Error("nil 接收者的 Restore 要报错")
	}
}

// ---------------------------------------------------------------------------
// 计数与写侧闸门（带真行的那一半）
// ---------------------------------------------------------------------------

// TestKBReleaseRepo_CountChunksInForce off 档的读数必须等于总数：这条断言挡的是
// "把谓词取反再减"这类改写（它会让 inForce 与 hidden 算反，且 off 档照样绿）。
func TestKBReleaseRepo_CountChunksInForce(t *testing.T) {
	ctx := context.Background()
	r, db := kbRepoDB(t)
	product := kbRepoProduct(t)
	kbRepoGovern(t, r, product, "op-1")
	doc := kbRepoSeqDoc()

	c1 := kbRepoChange(t, db, "kbc_f1", product, model.KBChangeOpAdd, "第一版正文", 0)
	c1.DocumentID = doc
	if _, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-1", Changes: []PublishChange{c1}}); err != nil {
		t.Fatal(err)
	}
	// v2：导入侧攒的一格待发布内容整体上线（零变更发布）。
	if b, err := kbrelease.EnsureDraftStamp(ctx, db, product); err != nil || b != 2 {
		t.Fatalf("第二格桶：b=%d err=%v", b, err)
	}
	kbRepoSeed(t, db, product, doc, 1, "第二版正文", 2, 0)
	if _, err := r.Publish(ctx, PublishIntent{ProductID: product, Actor: "op-1"}); err != nil {
		t.Fatal(err)
	}
	// v2 同时把第一版那条下线：退役号<=在服号 ⇒ 不计入在服，但 total 仍算它（行还在表里，
	// 这正是回滚窗口的依据）。
	if err := db.Table("knowledge_chunks").
		Where("product_id = ? AND chunk_index = 0", product).
		Update("retired_version", 2).Error; err != nil {
		t.Fatal(err)
	}
	// v3 还压在待发布桶里：kb_version > effective ⇒ 一条都不该算进在服数。
	kbRepoSeed(t, db, product, doc, 2, "待发布正文", 3, 0)

	// off / shadow 两档都不过滤：读数必须等于全量。
	for _, env := range []string{"off", "shadow"} {
		t.Setenv(kbrelease.FlagEnv, env)
		inForce, total, err := r.CountChunksInForce(ctx, product)
		if err != nil {
			t.Fatal(err)
		}
		if total != 3 {
			t.Errorf("%s 档：total 该是表里的条数（含已退役行），实得 %d", env, total)
		}
		if inForce != 3 {
			t.Errorf("%s 档不加过滤：inForce=%d（应为全量 3）", env, inForce)
		}
	}

	t.Setenv(kbrelease.FlagEnv, "on")
	inForce, total, err := r.CountChunksInForce(ctx, product)
	if err != nil {
		t.Fatal(err)
	}
	// 在服 = kb_version<=2 且（未退役 或 退役号>2）：只有第二版那条算，
	// 第一版已退役、第三版还没上线，两条都不计入。
	if inForce != 1 || total != 3 {
		t.Errorf("on 档读数错：inForce=%d total=%d（应为在服 1 条 / 全量 3 条）", inForce, total)
	}
}

// TestKBReleaseRepo_DirectWriteBlockedWithRealRows 写侧那道锁的两把钥匙：
// 只有"旗子 on"且"该库 governed"同时成立才拒。
//
// 未开闸就拦下运营的编辑入口，等于让"读代码顺便改了写语义"发生在人身上；
// 开了闸却不拦，等于给就地改写留了个绕过审批的洞。两头都要判住。
func TestKBReleaseRepo_DirectWriteBlockedWithRealRows(t *testing.T) {
	ctx := context.Background()
	r, db := kbRepoDB(t)
	governed := kbRepoProduct(t)
	kbRepoGovern(t, r, governed, "op-1")
	plain := kbRepoProduct(t)
	missing := kbRepoProduct(t)

	for _, tc := range []struct {
		name string
		env  string
		pid  string
		want bool
	}{
		{"off 档不拦（即使已治理）", "off", governed, false},
		{"shadow 档不拦", "shadow", governed, false},
		{"on 档拦已治理的库", "on", governed, true},
		{"on 档放行未治理", "on", plain, false},
		{"on 档放行没有行的库", "on", missing, false},
		{"空库号不拦", "on", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(kbrelease.FlagEnv, tc.env)
			got, err := kbrelease.DirectWriteBlocked(ctx, db, tc.pid)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("DirectWriteBlocked=%t 期望 %t（env=%s product=%q）", got, tc.want, tc.env, tc.pid)
			}
		})
	}
}

// TestKBReleaseRepo_StampForWriteRealRows 导入侧打戳在带真行时的三种走法：
// 已治理的库进待发布桶、未治理的一行都不碰、off 档连点查都不做。
//
// 这里专门测"0 不许写回去"：把 0 写进 kb_version 等于给一个未治理的库打上"待发布"，
// 闸门第一次 on 就会把它的存量挡在线外。
func TestKBReleaseRepo_StampForWriteRealRows(t *testing.T) {
	ctx := context.Background()
	r, db := kbRepoDB(t)
	governed := kbRepoProduct(t)
	kbRepoGovern(t, r, governed, "op-1")
	plain := kbRepoProduct(t)
	kbRepoEnsureRow(t, r, plain) // 有行但未治理

	t.Setenv(kbrelease.FlagEnv, "shadow")
	rows := []model.KnowledgeChunk{
		{ProductID: governed, Content: "已治理"},
		{ProductID: plain, Content: "未治理"},
		{ProductID: "", Content: "跨库全量重建没给归属"},
	}
	if err := kbrelease.StampForWrite(ctx, db, rows); err != nil {
		t.Fatal(err)
	}
	if rows[0].KBVersion != 1 {
		t.Errorf("已治理库的导入要进待发布桶，实得 %d", rows[0].KBVersion)
	}
	if rows[1].KBVersion != 0 {
		t.Errorf("未治理库要拿到 0 且**不写回**版本列，实得 %d", rows[1].KBVersion)
	}
	if rows[2].KBVersion != 0 {
		t.Errorf("无归属的写入要原样不动，实得 %d", rows[2].KBVersion)
	}

	// 已带版本号的存量行不能被打戳抹平：未治理的库拿到 0 时走的是"跳过"，不是"写 0"。
	// 写 0 等于把一条已经编过号的行退化成"升级前存量"，而版本账只有发布/回滚能动。
	legacy := []model.KnowledgeChunk{
		{ProductID: plain, Content: "未治理库里已编号的行", KBVersion: 7, RetiredVersion: 3},
	}
	if err := kbrelease.StampForWrite(ctx, db, legacy); err != nil {
		t.Fatal(err)
	}
	if legacy[0].KBVersion != 7 || legacy[0].RetiredVersion != 3 {
		t.Errorf("未治理库的已编号行要原样不动，实得 kb_version=%d retired_version=%d",
			legacy[0].KBVersion, legacy[0].RetiredVersion)
	}

	// 同批次幂等：再打一次仍拿同一个桶（一次导入的多条要整体上线）。
	again := []model.KnowledgeChunk{{ProductID: governed, Content: "第二批同桶"}}
	if err := kbrelease.StampForWrite(ctx, db, again); err != nil {
		t.Fatal(err)
	}
	if again[0].KBVersion != 1 {
		t.Errorf("同批次桶号复用上错，实得 %d", again[0].KBVersion)
	}

	// off 档一行都不碰，且不分配桶（少一次点查就是纯收益）。
	t.Setenv(kbrelease.FlagEnv, "off")
	untouched := []model.KnowledgeChunk{{ProductID: governed, Content: "关档写入"}}
	before := kbRepoRelease(t, r, governed)
	if err := kbrelease.StampForWrite(ctx, db, untouched); err != nil {
		t.Fatal(err)
	}
	if untouched[0].KBVersion != 0 {
		t.Errorf("off 档不该打戳：%+v", untouched[0])
	}
	if after := kbRepoRelease(t, r, governed); after.DraftVersion != before.DraftVersion ||
		after.AllocatedVersion != before.AllocatedVersion {
		t.Errorf("off 档不该动桶账：%+v → %+v", before, after)
	}
}

// TestKBReleaseRepo_EnsureDraftStampConcurrent 并发两路导入必须拿到同一个桶。
//
// AllocateDraftSQL 的 ON CONFLICT + CASE 就是为这一格写的；它漂了的形态不是报错，
// 而是"两批内容各占一格、只有后一格上线"，前一批在待办列表里长得像没导入过。
func TestKBReleaseRepo_EnsureDraftStampConcurrent(t *testing.T) {
	ctx := context.Background()
	r, db := kbRepoDB(t)
	product := kbRepoProduct(t)
	kbRepoGovern(t, r, product, "op-1")

	const n = 6
	var wg sync.WaitGroup
	got := make([]int, n)
	errs := make([]error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			got[i], errs[i] = kbrelease.EnsureDraftStamp(ctx, db, product)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("第 %d 路分配失败: %v", i, e)
		}
	}
	for i, v := range got {
		if v != got[0] {
			t.Fatalf("并发拿到了不同桶号：%v", got)
		}
		if v <= 0 {
			t.Errorf("第 %d 路拿到 %d：分配失败不能由 0 代答（0 意味着\"不受闸门管\"）", i, v)
		}
	}
	rel := kbRepoRelease(t, r, product)
	if rel.DraftVersion != got[0] || rel.AllocatedVersion != got[0] {
		t.Errorf("六路只该推进一格：%+v（首路 %d）", rel, got[0])
	}
}

// ---------------------------------------------------------------------------
// 待办视图与留痕
// ---------------------------------------------------------------------------

func TestKBReleaseRepo_ChangeViews(t *testing.T) {
	ctx := context.Background()
	r, db := kbRepoDB(t)
	product := kbRepoProduct(t)
	other := kbRepoProduct(t)

	for _, id := range []string{"kbc_v1", "kbc_v2"} {
		if err := db.Create(&model.KBChangeRequest{
			ID: id, ProductID: product, Op: model.KBChangeOpAdd, Content: "正文", Reason: "r",
			Status: model.KBChangeStatusPending, ApprovalID: "apr_" + id, RequestedBy: "op-1",
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.KBChangeRequest{
		ID: "kbc_v3", ProductID: other, Op: model.KBChangeOpRetire, TargetChunkID: 9, Reason: "r",
		Status: model.KBChangeStatusApplied, ApprovalID: "apr_v3", RequestedBy: "op-1",
	}).Error; err != nil {
		t.Fatal(err)
	}

	if rows, err := r.PendingChanges(ctx, product); err != nil || len(rows) != 2 {
		t.Errorf("待办只该是本库未落地的：rows=%d err=%v", len(rows), err)
	}
	rows, total, err := r.ListChanges(ctx, KBChangeFilter{ProductID: product})
	if err != nil || len(rows) != 2 || total != 2 {
		t.Errorf("按库过滤错：rows=%d total=%d err=%v", len(rows), total, err)
	}
	if _, total, err := r.ListChanges(ctx, KBChangeFilter{Status: model.KBChangeStatusApplied}); err != nil || total != 1 {
		t.Errorf("按状态过滤错：total=%d err=%v", total, err)
	}
	if _, total, err := r.ListChanges(ctx, KBChangeFilter{Op: model.KBChangeOpRetire}); err != nil || total != 1 {
		t.Errorf("按动作过滤错：total=%d err=%v", total, err)
	}
	// 分页上限要夹紧：limit 是请求参数，放开的表现是一次列表拉走整张表。
	rows, _, err = r.ListChanges(ctx, KBChangeFilter{Limit: 100000})
	if err != nil || len(rows) != 3 {
		t.Errorf("全量三行（limit 夹到 50）：rows=%d err=%v", len(rows), err)
	}
	rows, _, err = r.ListChanges(ctx, KBChangeFilter{Limit: 1})
	if err != nil || len(rows) != 1 {
		t.Errorf("limit=1 要生效：rows=%d err=%v", len(rows), err)
	}
	if got, err := r.GetChange(ctx, "kbc_nope"); err != nil || got != nil {
		t.Errorf("查无此条要是 (nil,nil)：%+v %v", got, err)
	}
}

// TestKBReleaseRepo_ClampsAreExact 分页夹逼的**边界三档**：默认值、上界内侧、上界外侧。
//
// ChangeViews 里只有 3 行，"limit 夹紧"在那种夹具上量不出来：把 200 的上界整条删掉、
// 把默认 50 改成 3、把 .Offset() 摘掉 —— 三种坏法在 3 行数据上都照样全绿。
// 这里灌 51 行（刚好越过默认值）再逐档判，"夹到 50"与"只认 50"才分得开。
// 两条读通路（变更列表 / 事件流）各自判一次：夹逼是两处各写一遍的数字，分叉的表现
// 是"变更页能翻到第二页、审计页永远只有 50 条"。
func TestKBReleaseRepo_ClampsAreExact(t *testing.T) {
	ctx := context.Background()
	r, db := kbRepoDB(t)
	product := kbRepoProduct(t)

	const seeded = 51
	for i := 0; i < seeded; i++ {
		if err := db.Create(&model.KBChangeRequest{
			ID: fmt.Sprintf("kbc_clamp_%03d", i), ProductID: product,
			Op: model.KBChangeOpAdd, Content: "正文", Reason: "r",
			Status:     model.KBChangeStatusPending,
			ApprovalID: fmt.Sprintf("apr_clamp_%03d", i), RequestedBy: "op-1",
		}).Error; err != nil {
			t.Fatalf("变更行落库失败: %v", err)
		}
	}
	for i := 0; i < seeded; i++ {
		if err := r.RecordAudit(ctx, model.KBChangeAuditLog{
			SubjectKey: "change:clamp", Action: model.KBAuditSubmitted,
			Actor: "op-1", NewValue: model.KBChangeStatusPending,
		}); err != nil {
			t.Fatalf("留痕落库失败: %v", err)
		}
	}

	for _, tc := range []struct {
		name  string
		limit int
		want  int
	}{
		{"超上界夹到默认 50", 100000, 50},
		{"上界外侧 201 夹到默认 50", 201, 50},
		{"零值回默认 50", 0, 50},
		{"负数回默认 50", -7, 50},
		{"上界 200 原样生效", 200, seeded},
		{"上界内侧 51 原样生效", 51, seeded},
		{"显式 10 原样生效", 10, 10},
	} {
		rows, total, err := r.ListChanges(ctx, KBChangeFilter{ProductID: product, Limit: tc.limit})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(rows) != tc.want {
			t.Errorf("%s：limit=%d 实得 %d 行（期望 %d）", tc.name, tc.limit, len(rows), tc.want)
		}
		if total != seeded {
			t.Errorf("%s：total 该是全量 %d，实得 %d", tc.name, seeded, total)
		}
	}

	// Offset 要真参与翻页：摘掉它的形态是"第二页拿到第一页的内容"，页面永远翻不动。
	if second, _, err := r.ListChanges(ctx, KBChangeFilter{ProductID: product, Limit: 50, Offset: 50}); err != nil || len(second) != 1 {
		t.Errorf("第二页该剩 1 行：rows=%d err=%v", len(second), err)
	}
	if over, _, err := r.ListChanges(ctx, KBChangeFilter{ProductID: product, Limit: 50, Offset: seeded}); err != nil || len(over) != 0 {
		t.Errorf("越界偏移该回空：rows=%d err=%v", len(over), err)
	}

	for _, tc := range []struct {
		limit int
		want  int
	}{
		{100000, 50},
		{201, 50},
		{0, 50},
		{51, seeded},
		{10, 10},
	} {
		traces, err := r.ListAuditBySubject(ctx, "change:clamp", tc.limit)
		if err != nil {
			t.Fatalf("留痕列表读取失败: %v", err)
		}
		if len(traces) != tc.want {
			t.Errorf("ListAuditBySubject(limit=%d) 实得 %d 行（期望 %d）", tc.limit, len(traces), tc.want)
		}
	}
}

// TestKBReleaseRepo_WithdrawCAS 撤回是"仍是 pending 才改得动"：
// 已发布的撤不掉，想撤已上线的内容要提一条新的 retire 再走一次审批。
func TestKBReleaseRepo_WithdrawCAS(t *testing.T) {
	ctx := context.Background()
	r, db := kbRepoDB(t)
	product := kbRepoProduct(t)
	if err := db.Create(&model.KBChangeRequest{
		ID: "kbc_w1", ProductID: product, Op: model.KBChangeOpAdd, Content: "正文", Reason: "r",
		Status: model.KBChangeStatusPending, ApprovalID: "apr_w1", RequestedBy: "op-1",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if ok, err := r.WithdrawChange(ctx, "kbc_missing"); err != nil || ok {
		t.Errorf("不存在的行不该报成功：%t %v", ok, err)
	}
	if ok, err := r.WithdrawChange(ctx, "kbc_w1"); err != nil || !ok {
		t.Fatalf("撤回失败: %t %v", ok, err)
	}
	if ok, err := r.WithdrawChange(ctx, "kbc_w1"); err != nil || ok {
		t.Errorf("重复撤回要 CAS 拒掉：%t %v", ok, err)
	}
	got, err := r.GetChange(ctx, "kbc_w1")
	if err != nil || got.Status != model.KBChangeStatusWithdrawn {
		t.Errorf("状态没落库：%+v %v", got, err)
	}
	if rows, _ := r.PendingChanges(ctx, product); len(rows) != 0 {
		t.Errorf("撤回后仍出现在待办里：%+v", rows)
	}
}

// TestKBReleaseRepo_AuditShape 留痕三列必填 + 事件流只增且倒序可读。
//
// 删掉这张表等于把 AC③ 一起删掉，所以这里判的是"写不全要报错"而不是"少列也能过"。
func TestKBReleaseRepo_AuditShape(t *testing.T) {
	ctx := context.Background()
	r, db := kbRepoDB(t)

	for _, bad := range []model.KBChangeAuditLog{
		{Action: model.KBAuditSubmitted, Actor: "op"},
		{SubjectKey: "change:x", Actor: "op"},
		{SubjectKey: "change:x", Action: model.KBAuditSubmitted},
	} {
		if err := r.RecordAudit(ctx, bad); err == nil {
			t.Errorf("字段不全的审计行要拒：%+v", bad)
		}
	}
	if err := r.AppendAudit(ctx, nil, model.KBChangeAuditLog{
		SubjectKey: "change:x", Action: model.KBAuditSubmitted, Actor: "op-1", NewValue: "pending",
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := r.ListAuditBySubject(ctx, "change:x", 10)
	if err != nil || len(rows) != 1 || rows[0].Actor != "op-1" {
		t.Fatalf("读不回留痕：%d %v", len(rows), err)
	}
	if _, err := r.ListAuditBySubject(ctx, "change:x", 0); err != nil {
		t.Errorf("limit=0 要夹默认值而不是报错：%v", err)
	}
	var nilRepo *KBReleaseRepository
	if err := nilRepo.RecordAudit(ctx, model.KBChangeAuditLog{
		SubjectKey: "change:x", Action: model.KBAuditSubmitted, Actor: "op",
	}); err == nil {
		t.Error("nil 仓储要报错而不是 panic")
	}
	if n := kbRepoAuditCount(t, db, "change:x", model.KBAuditSubmitted); n != 1 {
		t.Errorf("留痕条数 %d", n)
	}
}

// TestKBReleaseRepo_UnavailableEveryMethod 底座没起来时每个方法都要报错，
// 而不是 panic 或返回一个"看着像空结果"的零值。
func TestKBReleaseRepo_UnavailableEveryMethod(t *testing.T) {
	ctx := context.Background()
	var nilRepo *KBReleaseRepository
	empty := NewKBReleaseRepository(nil)

	for _, r := range []*KBReleaseRepository{nilRepo, empty} {
		if r.Available() {
			t.Error("Available 该答 false")
		}
		if _, err := r.GetRelease(ctx, "p"); err == nil {
			t.Error("GetRelease 要报错")
		}
		if _, err := r.ListReleases(ctx); err == nil {
			t.Error("ListReleases 要报错")
		}
		if _, err := r.SetGoverned(ctx, "p", true, "a"); err == nil {
			t.Error("SetGoverned 要报错")
		}
		if err := r.InsertChange(ctx, &model.KBChangeRequest{ID: "x"}); err == nil {
			t.Error("InsertChange 要报错")
		}
		if _, err := r.GetChange(ctx, "x"); err == nil {
			t.Error("GetChange 要报错")
		}
		if _, _, err := r.ListChanges(ctx, KBChangeFilter{}); err == nil {
			t.Error("ListChanges 要报错")
		}
		if _, err := r.PendingChanges(ctx, "p"); err == nil {
			t.Error("PendingChanges 要报错")
		}
		if _, err := r.WithdrawChange(ctx, "x"); err == nil {
			t.Error("WithdrawChange 要报错")
		}
		if _, err := r.Publish(ctx, PublishIntent{ProductID: "p", Actor: "a"}); err == nil {
			t.Error("Publish 要报错")
		}
		if _, err := r.Rollback(ctx, "p", "a"); err == nil {
			t.Error("Rollback 要报错")
		}
		if _, err := r.Restore(ctx, "p", "a"); err == nil {
			t.Error("Restore 要报错")
		}
		if _, _, err := r.CountChunksInForce(ctx, "p"); err == nil {
			t.Error("CountChunksInForce 要报错")
		}
		if _, err := r.ListAuditBySubject(ctx, "change:x", 10); err == nil {
			t.Error("ListAuditBySubject 要报错")
		}
	}
}

// kbVisibleIDs 用**叶子包那一份**谓词读"此刻线上真能不能检到"的集合。
//
// 不复用 CountChunksInForce：那条通路自己也在数同一件事，用它的输出验它的输出等于没验；
// 这里要的是"按召回同一个判据，具体是哪几行可见"。
func kbVisibleIDs(t *testing.T, db *gorm.DB, product string) []uint64 {
	t.Helper()
	var ids []uint64
	q := db.Table("knowledge_chunks").Select("id").Where("product_id = ?", product)
	if pred := kbrelease.VisiblePredicate(); pred != "" {
		q = q.Where(pred)
	}
	if err := q.Order("id ASC").Scan(&ids).Error; err != nil {
		t.Fatalf("可见集合读取失败: %v", err)
	}
	return ids
}

func kbSameChunks(a, b []kbChunkSnapshot) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
