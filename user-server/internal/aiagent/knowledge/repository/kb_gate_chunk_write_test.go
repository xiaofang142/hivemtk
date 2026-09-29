// kb_gate_chunk_write_test.go T-P9-02 写侧闸门（版本打戳 + 就地改写拦截 + 批量删除出声）。
//
// 读路径那一半量的是"未发布的内容检不到"，这一半量的是它的前因与两个绕过口：
//
//  1. **打戳**（Create / BatchCreate）。漏一次打戳不是"少一个装饰字段"，而是那行带着
//     kb_version=0 落库 ⇒ 永不受闸门管 ⇒ AC① 从写入侧被绕过，且无声。所以这里既断言
//     "受管库的新行进了桶"，也断言"分配失败时写入方拿到错误、库里一行都不多" ——
//     后者是刻意不"放行算了"：拿不准归属时静默写 0 就是一次放行。
//  2. **就地改写 / 单条物理删除**（Update / Delete）。这两条会把未审批正文当场推进线上
//     （或无声撤下在服内容），闸门 + governed 下必须拒，且拒之后库里那行**没被改过**。
//  3. **批量删除**（DeleteByDocumentID / DeleteByProductID）。这是导入重切的通路，
//     拦它等于把"导入也走发布制"一起关掉，所以只出声不改判 —— 那一句告警是本卡
//     唯一一个"运维看得到、代码不拦"的判据，它必须真的出声，且只在有在服行时出声。
//
// 两道锁的独立性在每一组里都各测一档：旗子 off 时哪怕 governed=true 也不许拦、不许打戳，
// 旗子 on 时未受管的库不许被波及（否则关旗子不再是"即刻回到今天"）。
package repository

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"hivemtk-user/internal/aiagent/knowledge/model"
	coremodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/kbrelease"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

const (
	kbRepoGov   = "kbrepo_gov"   // governed=true, effective=1, allocated=3, draft=0
	kbRepoUngov = "kbrepo_ungov" // 有行但 governed=false
)

// kbRepoSetup 建 knowledge_chunks + kb_releases 并按需灌一行在服分段。
//
// kb_releases 走 AutoMigrate（列集由 model.KBRelease 自己给），knowledge_chunks 同理 ——
// 本包不像 rag/retrieval 那样有手写 DDL，所以没有"测试表落后于模型"这条债。
func kbRepoSetup(t *testing.T, withReleaseRow bool) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t, &model.KnowledgeChunk{})
	if err := database.Exec(`DROP TABLE IF EXISTS kb_releases`).Error; err != nil {
		t.Fatalf("清理 kb_releases 失败：%v", err)
	}
	if err := database.AutoMigrate(&coremodel.KBRelease{}); err != nil {
		t.Fatalf("建 kb_releases 失败：%v", err)
	}
	if !withReleaseRow {
		return database
	}
	if err := database.Exec(`INSERT INTO kb_releases
		(product_id, governed, effective_version, previous_version, recalled_version, draft_version, allocated_version, changed_by)
		VALUES (?, true, 1, 0, 0, 0, 3, 'tester'), (?, false, 0, 0, 0, 0, 9, 'tester')`,
		kbRepoGov, kbRepoUngov).Error; err != nil {
		t.Fatalf("灌入 release 行失败：%v", err)
	}
	return database
}

// kbRepoInsertInForce 直接插一条"在服"分段（不走被测的 Create，夹具与出口分家）。
func kbRepoInsertInForce(t *testing.T, db *gorm.DB, docID uint64, productID string) uint64 {
	t.Helper()
	if err := db.Exec(`INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, kb_version, retired_version)
		VALUES (?, ?, 0, '在服正文', 1, 0)`, docID, productID).Error; err != nil {
		t.Fatalf("夹具插入在服分段失败：%v", err)
	}
	var id uint64
	if err := db.Raw(`SELECT id FROM knowledge_chunks WHERE document_id = ?`, docID).Scan(&id).Error; err != nil {
		t.Fatalf("回读夹具分段 id 失败：%v", err)
	}
	if id == 0 {
		t.Fatal("夹具分段没落库（前置不成立）")
	}
	return id
}

func kbRepoDraftAndAllocated(t *testing.T, db *gorm.DB, productID string) (int, int) {
	t.Helper()
	var cur struct {
		Draft     int `gorm:"column:draft_version"`
		Allocated int `gorm:"column:allocated_version"`
	}
	if err := db.Table("kb_releases").Select("draft_version, allocated_version").
		Where("product_id = ?", productID).Scan(&cur).Error; err != nil {
		t.Fatalf("读 kb_releases 失败：%v", err)
	}
	return cur.Draft, cur.Allocated
}

func kbRepoChunkCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Table("knowledge_chunks").Count(&n).Error; err != nil {
		t.Fatalf("数分段失败：%v", err)
	}
	return n
}

// TestKbRepo_WriteStampingRoutesToDraftBucket 新写入进待发布桶，且只在闸门 shadow|on 时进。
func TestKbRepo_WriteStampingRoutesToDraftBucket(t *testing.T) {
	repo := func(db *gorm.DB) *KnowledgeChunkRepository { return NewKnowledgeChunkRepository(db) }

	for _, mode := range []string{"shadow", "on"} {
		t.Run(mode+" 受管库：首条分配、次条复用同一桶", func(t *testing.T) {
			t.Setenv(kbrelease.FlagEnv, mode)
			db := kbRepoSetup(t, true)
			ctx := context.Background()

			first := &model.KnowledgeChunk{DocumentID: 1, ProductID: kbRepoGov, Content: "第一段"}
			if err := repo(db).Create(ctx, first); err != nil {
				t.Fatalf("Create 失败：%v", err)
			}
			if first.KBVersion != 4 {
				t.Errorf("首条该拿到 allocated(3)+1=4，实际 %d", first.KBVersion)
			}
			var stored model.KnowledgeChunk
			if err := db.First(&stored, first.ID).Error; err != nil {
				t.Fatalf("回读失败：%v", err)
			}
			if stored.KBVersion != 4 {
				t.Errorf("库里那行 kb_version=%d，期望 4（内存里改了、库里没落 = 本卡最怕的形态）", stored.KBVersion)
			}
			draft, alloc := kbRepoDraftAndAllocated(t, db, kbRepoGov)
			if draft != 4 || alloc != 4 {
				t.Errorf("桶位推进不对：draft=%d allocated=%d，期望 4/4", draft, alloc)
			}

			second := &model.KnowledgeChunk{DocumentID: 2, ProductID: kbRepoGov, Content: "第二段"}
			if err := repo(db).Create(ctx, second); err != nil {
				t.Fatalf("第二条 Create 失败：%v", err)
			}
			if second.KBVersion != 4 {
				t.Errorf("同库第二条应复用桶 4，实际 %d（一批导入的内容必须整体上线）", second.KBVersion)
			}
			draft, alloc = kbRepoDraftAndAllocated(t, db, kbRepoGov)
			if draft != 4 || alloc != 4 {
				t.Errorf("第二条不该再往前挪一格：draft=%d allocated=%d", draft, alloc)
			}
		})
	}

	t.Run("off 受管库：一行都不碰、一次分配都不做", func(t *testing.T) {
		t.Setenv(kbrelease.FlagEnv, "off")
		db := kbRepoSetup(t, true)
		chunk := &model.KnowledgeChunk{DocumentID: 1, ProductID: kbRepoGov, Content: "正文"}
		if err := repo(db).Create(context.Background(), chunk); err != nil {
			t.Fatalf("Create 失败：%v", err)
		}
		if chunk.KBVersion != 0 {
			t.Errorf("off 档不该打戳，实际 kb_version=%d", chunk.KBVersion)
		}
		draft, alloc := kbRepoDraftAndAllocated(t, db, kbRepoGov)
		if draft != 0 || alloc != 3 {
			t.Errorf("off 档不该动桶的高水位：draft=%d allocated=%d，期望 0/3", draft, alloc)
		}
	})

	t.Run("on 未受管库：版本 0，且不新建 release 行", func(t *testing.T) {
		t.Setenv(kbrelease.FlagEnv, "on")
		db := kbRepoSetup(t, true)
		chunk := &model.KnowledgeChunk{DocumentID: 1, ProductID: "kbrepo_brandnew", Content: "正文"}
		if err := repo(db).Create(context.Background(), chunk); err != nil {
			t.Fatalf("Create 失败：%v", err)
		}
		if chunk.KBVersion != 0 {
			t.Errorf("没有 release 行的库该拿 0，实际 %d", chunk.KBVersion)
		}
		var n int64
		if err := db.Table("kb_releases").Where("product_id = ?", "kbrepo_brandnew").Count(&n).Error; err != nil {
			t.Fatalf("数 release 行失败：%v", err)
		}
		if n != 0 {
			t.Errorf("给一个没进发布制的库凭空建 release 行 = 把它标成受管过（n=%d）", n)
		}
	})

	t.Run("on 未 governance 的既有库：版本 0，高水位不动", func(t *testing.T) {
		t.Setenv(kbrelease.FlagEnv, "on")
		db := kbRepoSetup(t, true)
		chunk := &model.KnowledgeChunk{DocumentID: 1, ProductID: kbRepoUngov, Content: "正文"}
		if err := repo(db).Create(context.Background(), chunk); err != nil {
			t.Fatalf("Create 失败：%v", err)
		}
		if chunk.KBVersion != 0 {
			t.Errorf("governed=false 的库该拿 0，实际 %d", chunk.KBVersion)
		}
		draft, alloc := kbRepoDraftAndAllocated(t, db, kbRepoUngov)
		if draft != 0 || alloc != 9 {
			t.Errorf("未受管库的高水位被动了：draft=%d allocated=%d", draft, alloc)
		}
	})

	t.Run("on 空归属：跨库重建这类调用方不给库号 ⇒ 版本 0", func(t *testing.T) {
		t.Setenv(kbrelease.FlagEnv, "on")
		db := kbRepoSetup(t, true)
		chunk := &model.KnowledgeChunk{DocumentID: 1, ProductID: "", Content: "正文"}
		if err := repo(db).Create(context.Background(), chunk); err != nil {
			t.Fatalf("Create 失败：%v", err)
		}
		if chunk.KBVersion != 0 {
			t.Errorf("product_id 为空时该拿 0（无法归属就不参与闸门），实际 %d", chunk.KBVersion)
		}
	})

	t.Run("on BatchCreate 跨库：各归各的桶", func(t *testing.T) {
		t.Setenv(kbrelease.FlagEnv, "on")
		db := kbRepoSetup(t, true)
		chunks := []model.KnowledgeChunk{
			{DocumentID: 1, ProductID: kbRepoGov, Content: "受管 A"},
			{DocumentID: 2, ProductID: kbRepoUngov, Content: "未受管"},
			{DocumentID: 3, ProductID: "", Content: "无归属"},
			{DocumentID: 4, ProductID: kbRepoGov, Content: "受管 B"},
		}
		if err := repo(db).BatchCreate(context.Background(), chunks); err != nil {
			t.Fatalf("BatchCreate 失败：%v", err)
		}
		// 断言落库后的值而不是入参切片：BatchCreate 的入参是值切片，
		// 只改副本的实现（StampForWrite 的指针形状写错就是这样）会在这里现形。
		got := map[string]int{}
		var rows []model.KnowledgeChunk
		if err := db.Find(&rows).Error; err != nil {
			t.Fatalf("回读失败：%v", err)
		}
		for _, r := range rows {
			got[r.ProductID] = r.KBVersion
		}
		if got[kbRepoGov] != 4 {
			t.Errorf("受管库的批量行该进桶 4，实际 %d", got[kbRepoGov])
		}
		if got[kbRepoUngov] != 0 || got[""] != 0 {
			t.Errorf("未受管/无归属的行不该被 stamp 波及：%v", got)
		}
	})

	// 分配失败**不许**退化成"写 0 放行"：拿掉 kb_releases 之后 EnsureDraftStamp 的
	// 点查必报错，此时 Create 必须整体失败且库里一行都不多。
	t.Run("on 账目读不动：Create 报错且不落库", func(t *testing.T) {
		t.Setenv(kbrelease.FlagEnv, "on")
		db := kbRepoSetup(t, true)
		if err := db.Exec(`DROP TABLE kb_releases`).Error; err != nil {
			t.Fatalf("撤掉 kb_releases 失败：%v", err)
		}
		before := kbRepoChunkCount(t, db)
		err := repo(db).Create(context.Background(), &model.KnowledgeChunk{
			DocumentID: 1, ProductID: kbRepoGov, Content: "正文",
		})
		if err == nil {
			t.Fatal("归属账读不动时 Create 仍成功了（会静默写 kb_version=0 ⇒ 闸门绕过口）")
		}
		if after := kbRepoChunkCount(t, db); after != before {
			t.Errorf("失败的写入仍落了库：%d → %d", before, after)
		}
	})
}

// TestKbRepo_GuardDirectWrite 就地改写与单条物理删除的两道锁。
func TestKbRepo_GuardDirectWrite(t *testing.T) {
	ctx := context.Background()

	t.Run("on 受管库：Update 被拒且正文没变", func(t *testing.T) {
		t.Setenv(kbrelease.FlagEnv, "on")
		db := kbRepoSetup(t, true)
		id := kbRepoInsertInForce(t, db, 1, kbRepoGov)
		err := NewKnowledgeChunkRepository(db).Update(ctx, &model.KnowledgeChunk{
			ID: id, ProductID: kbRepoGov, Content: "未审批的正文",
		})
		if !errors.Is(err, kbrelease.ErrGovernedDirectWrite) {
			t.Fatalf("应回 ErrGovernedDirectWrite，实际 %v", err)
		}
		var stored model.KnowledgeChunk
		if err := db.First(&stored, id).Error; err != nil {
			t.Fatalf("回读失败：%v", err)
		}
		if stored.Content != "在服正文" {
			t.Errorf("被拒的 Update 仍改了正文：%q", stored.Content)
		}
	})

	// 上面那一格的镜像：shadow 档同一批夹具必须**照常改下去**。只测"on 拦得住"测不出
	// "影子期连写都拦了"，而后者才是这一档真正的失效形态（见下面四格循环的说明）。
	t.Run("shadow 受管库：Update 照常生效", func(t *testing.T) {
		t.Setenv(kbrelease.FlagEnv, "shadow")
		db := kbRepoSetup(t, true)
		id := kbRepoInsertInForce(t, db, 1, kbRepoGov)
		if err := NewKnowledgeChunkRepository(db).Update(ctx, &model.KnowledgeChunk{
			ID: id, ProductID: kbRepoGov, Content: "影子期正文",
		}); err != nil {
			t.Fatalf("shadow 档不该拦就地改写：%v", err)
		}
		var stored model.KnowledgeChunk
		if err := db.First(&stored, id).Error; err != nil {
			t.Fatalf("回读失败：%v", err)
		}
		if stored.Content != "影子期正文" {
			t.Errorf("shadow 档的 Update 没落到库里：%q", stored.Content)
		}
	})

	// Delete 只给 id：这一路要按 id 回查归属再判，回查写错的表现是"删得掉受管库的在服行"。
	t.Run("on 受管库：Delete 被拒且行还在", func(t *testing.T) {
		t.Setenv(kbrelease.FlagEnv, "on")
		db := kbRepoSetup(t, true)
		id := kbRepoInsertInForce(t, db, 1, kbRepoGov)
		err := NewKnowledgeChunkRepository(db).Delete(ctx, id)
		if !errors.Is(err, kbrelease.ErrGovernedDirectWrite) {
			t.Fatalf("应回 ErrGovernedDirectWrite，实际 %v", err)
		}
		var n int64
		if err := db.Table("knowledge_chunks").Where("id = ?", id).Count(&n).Error; err != nil {
			t.Fatalf("数行失败：%v", err)
		}
		if n != 1 {
			t.Errorf("被拒的 Delete 仍删掉了在服行（n=%d）", n)
		}
	})

	// 反向四格：四道门各关掉一道，Delete 都该放行。缺任何一格，"闸门只影响受管库"
	// 这句就只是注释。
	//
	// shadow 那一格量的是**影子期不许变成冻结期**：这一档的全部承诺是"只在日志里报
	// 会被隐藏几条"，写侧若跟着拦，运维在决定要不要转 on 的那几周里连一条文案都改不动，
	// 而这一档换来的证据也就没了。把 !GateOn() 写成 !ShadowsOrOn() 恰是这个失效形态。
	for _, c := range []struct {
		name    string
		mode    string
		product string
	}{
		{"off 档即使受管也放行", "off", kbRepoGov},
		{"shadow 档即使受管也放行", "shadow", kbRepoGov},
		{"on 档未受管放行", "on", kbRepoUngov},
		{"on 档没有 release 行放行", "on", "kbrepo_none"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(kbrelease.FlagEnv, c.mode)
			db := kbRepoSetup(t, true)
			id := kbRepoInsertInForce(t, db, 1, c.product)
			if err := NewKnowledgeChunkRepository(db).Delete(ctx, id); err != nil {
				t.Fatalf("不该拦的写被拦了：%v", err)
			}
			var n int64
			if err := db.Table("knowledge_chunks").Where("id = ?", id).Count(&n).Error; err != nil {
				t.Fatalf("数行失败：%v", err)
			}
			if n != 0 {
				t.Errorf("放行的 Delete 没真删掉（n=%d）", n)
			}
		})
	}

	t.Run("on 受管库：不存在的 id 不报错也不查账", func(t *testing.T) {
		t.Setenv(kbrelease.FlagEnv, "on")
		db := kbRepoSetup(t, true)
		// guard 里"行本来就不在 ⇒ 不代答"那一路：让紧随其后的删除给出"删不到"。
		if err := NewKnowledgeChunkRepository(db).Delete(ctx, 999999); err != nil {
			t.Errorf("删不存在的 id 不该报错：%v", err)
		}
	})
}

// TestKbRepo_BatchDeleteWarnsOnlyInForceRows 批量删除的告警：只在真有在服行时出声。
//
// 这条通路**不拦**（拦它等于关掉导入），所以它的全部防线就是这一句告警。
// 断言按整行匹配并数行数：正文里带换行的分段名不会把一行劈成两行、
// 也不会把"有告警"与"有别的 kb-release 日志"拼成一条绿。
func TestKbRepo_BatchDeleteWarnsOnlyInForceRows(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name     string
		mode     string
		document uint64
		wantHits int
	}{
		{"on 且有在服行 ⇒ 出声", "on", 10, 1},
		{"on 且全未发布 ⇒ 不出声", "on", 11, 0},
		{"off 档 ⇒ 不出声也不查账", "off", 10, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(kbrelease.FlagEnv, tc.mode)
			db := kbRepoSetup(t, true)
			kbRepoInsertInForce(t, db, 10, kbRepoGov)
			if err := db.Exec(`INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, kb_version, retired_version)
				VALUES (11, ?, 0, '未发布正文', 2, 0)`, kbRepoGov).Error; err != nil {
				t.Fatalf("夹具插入未发布分段失败：%v", err)
			}
			// 夹具控制（不重复写一遍可见性判据，只钉版本号与库上那个 1 的关系）：
			// doc 10 的版本号**等于** effective ⇒ 在服；doc 11 的版本号**大于**它 ⇒ 未发布。
			// 两格互为反向控制：若 COUNT 恒为 0，(a) 红；若 COUNT 不看版本号，(b) 红。
			var v10, v11, eff int
			if err := db.Raw(`SELECT kb_version FROM knowledge_chunks WHERE document_id = 10`).Scan(&v10).Error; err != nil {
				t.Fatalf("回读 doc 10 版本号失败：%v", err)
			}
			if err := db.Raw(`SELECT kb_version FROM knowledge_chunks WHERE document_id = 11`).Scan(&v11).Error; err != nil {
				t.Fatalf("回读 doc 11 版本号失败：%v", err)
			}
			if err := db.Raw(`SELECT effective_version FROM kb_releases WHERE product_id = ?`, kbRepoGov).Scan(&eff).Error; err != nil {
				t.Fatalf("回读生效版本失败：%v", err)
			}
			if v10 != eff || v11 <= eff {
				t.Fatalf("夹具版本号不成立：doc10=%d doc11=%d effective=%d（要 10 在服、11 未发布）", v10, v11, eff)
			}

			logged := kbRepoCaptureLogs(t, func() {
				err := NewKnowledgeChunkRepository(db).DeleteByDocumentID(ctx, tc.document)
				if err != nil {
					t.Fatalf("DeleteByDocumentID 失败：%v", err)
				}
			})

			var hits int
			for _, line := range strings.Split(logged, "\n") {
				if strings.Contains(line, `"message":"[kb-release] 即将物理删除 document_id=`) {
					hits++
				}
			}
			if hits != tc.wantHits {
				t.Errorf("告警行数 %d，期望 %d；捕获内容：\n%s", hits, tc.wantHits, logged)
			}
			if tc.wantHits == 1 && !strings.Contains(logged, " 1 条在服分段") {
				t.Errorf("告警句该报出在服行数 1：%s", logged)
			}
			// 出声不改判：这一批行**必须**真的被删掉了。
			var left int64
			if err := db.Table("knowledge_chunks").Where("document_id = ?", tc.document).Count(&left).Error; err != nil {
				t.Fatalf("数剩余行失败：%v", err)
			}
			if left != 0 {
				t.Errorf("告警通路仍要把行删掉（剩 %d 行）", left)
			}
		})
	}

	// 读失败上抛：删到一半才发现归属账读不动时，宁可这一次删除失败。
	t.Run("on 且 kb_releases 读不动 ⇒ 报错且一行都不删", func(t *testing.T) {
		t.Setenv(kbrelease.FlagEnv, "on")
		db := kbRepoSetup(t, true)
		kbRepoInsertInForce(t, db, 10, kbRepoGov)
		if err := db.Exec(`DROP TABLE kb_releases`).Error; err != nil {
			t.Fatalf("撤表失败：%v", err)
		}
		if err := NewKnowledgeChunkRepository(db).DeleteByDocumentID(ctx, 10); err == nil {
			t.Error("归属账读不动时删除仍成功了（应当上抛错误）")
		}
		var left int64
		if err := db.Table("knowledge_chunks").Where("document_id = ?", 10).Count(&left).Error; err != nil {
			t.Fatalf("数剩余行失败：%v", err)
		}
		if left != 1 {
			t.Errorf("报错的删除仍动了行（剩 %d 行，期望 1）", left)
		}
	})
}

// kbRepoCaptureLogs 抓一段闭包写出的日志（json 档 + 整行匹配，口径同 internal/app 那两处）。
func kbRepoCaptureLogs(t *testing.T, fn func()) string {
	t.Helper()
	oldOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("建管道失败：%v", err)
	}
	os.Stdout = w
	logger.InitLogger(logger.LoggingConfig{Level: "info", Format: "json", Output: "stdout"})
	defer func() {
		os.Stdout = oldOut
		logger.InitLogger(logger.DefaultConfig())
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("关闭写端失败：%v", err)
	}
	os.Stdout = oldOut
	captured, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("读取日志失败：%v", readErr)
	}
	return string(captured)
}
