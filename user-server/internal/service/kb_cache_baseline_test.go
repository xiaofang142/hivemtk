// kb_cache_baseline_test.go T-P2-05 的 AC③ 对照用例：**本文件刻意不引用任何本卡新增的符号**
// （不出现 KBAnswerVersionFor / Version / Canary* / UpdateVersionCanary），
// 因此它可以原样放进"挂载前"的树里跑一遍。两边跑出的结果必须逐条相同 ——
// 这才是"未开灰度时检索结果与今天一致"的实证形态，而不是"我推断没改"。
//
// 它断的是生产路径今天真实使用的缓存键形状：kb_id = knowledge_bases 主键十进制串、
// prompt_version = "v1"、写入后 Tier1 精确命中读回同一条答案。
package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	ragcache "hivemtk-user/internal/aiagent/rag/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestKBCacheBaseline_TodayKeyShapeRoundTrips(t *testing.T) {
	database := testutil.NewTestDB(t, &model.KnowledgeBase{}, &ragcache.RAGAnswerCache{})
	ctx := context.Background()

	// 只用两棵树都有的列建一行 KB，避免依赖本卡新增字段。
	code := "kb-cache-baseline-r21"
	if err := database.Exec(`
		INSERT INTO knowledge_bases (kb_code, type, name, owner_type, enabled, created_at, updated_at)
		VALUES (?, 'faq', '对照库', 'shared', true, ?, ?)`, code, time.Now(), time.Now()).Error; err != nil {
		t.Fatalf("插入对照 KB 失败: %v", err)
	}
	var kbID uint
	if err := database.Raw(`SELECT id FROM knowledge_bases WHERE kb_code = ?`, code).
		Scan(&kbID).Error; err != nil || kbID == 0 {
		t.Fatalf("回读对照 KB 失败: id=%d err=%v", kbID, err)
	}
	kbIDStr := strconv.FormatUint(uint64(kbID), 10)

	cacheSvc := ragcache.NewFAQAnswerCacheService(
		ragcache.NewPGAnswerCacheStore(database), ragcache.NewPGKBMetaReader(database), 0)
	// 与生产写入完全同参：PromptVersion 就是这个常量串。
	if err := cacheSvc.Store(ctx, ragcache.StoreRequest{
		KBID: kbIDStr, PromptVersion: "v1", QueryVector: kbCacheBaselineVec(),
		Answer: "对照答案：满三百减三十。", FromKnowledgeBase: true,
	}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	res, err := cacheSvc.Lookup(ctx, ragcache.LookupRequest{
		KBID: kbIDStr, PromptVersion: "v1", QueryVector: kbCacheBaselineVec(),
	})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if res == nil || res.Tier != ragcache.TierExact || res.Answer != "对照答案：满三百减三十。" {
		t.Fatalf("对照读回不符: %+v", res)
	}

	type row struct {
		KBID          string `gorm:"column:kb_id"`
		PromptVersion string `gorm:"column:prompt_version"`
	}
	var rows []row
	if err := database.Raw(`SELECT kb_id, prompt_version FROM rag_answer_cache WHERE kb_id = ?`, kbIDStr).
		Scan(&rows).Error; err != nil {
		t.Fatalf("回读缓存行: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("期望恰好 1 行缓存，got %d 行: %+v", len(rows), rows)
	}
	if rows[0].PromptVersion != "v1" || rows[0].KBID != kbIDStr {
		t.Errorf("缓存键形状漂移：期望 (kb_id=%s, prompt_version=v1)，got %+v", kbIDStr, rows[0])
	}
}

// kbCacheBaselineVec 与 kb_canary_test.go 里同构，但独立命名：本文件要能在没有那个文件的
// 树里编译，所以一个辅助符号都不共用。
func kbCacheBaselineVec() []float32 {
	v := make([]float32, 1024)
	for i := range v {
		v[i] = 1 + float32(i)*1e-6
	}
	return v
}
