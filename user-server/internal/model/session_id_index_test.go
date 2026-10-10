package model

import (
	"testing"

	"hivemtk-user/internal/pkg/testutil"
)

// TestSessionIDColumnsHaveIndexOnFreshSchema 钉住"全新装库也要给 session_id 建索引"。
//
// 为什么要有这一格：按会话维度查这几张表是常态读路径，缺索引只会变慢、不会报错，
// 因此结构体标签里的 `index` 被顺手抹掉时，用例全绿、线上无感，只有升级到一定量级才暴露。
// 而 AutoMigrate 只加不减：已经跑过旧版本的库里索引还在，读那种库看不出任何异常——
// 能暴露这件事的只有"从零建一遍库"这条路径，也就是这里。
func TestSessionIDColumnsHaveIndexOnFreshSchema(t *testing.T) {
	database := testutil.NewTestDB(t, &RagMessage{}, &KnowledgeFeedback{}, &SOPStateMemory{}, &WebVitalRecord{})
	for _, tbl := range []string{"rag_messages", "knowledge_feedbacks", "sop_state_memories", "web_vital_records"} {
		var defs []string
		if err := database.Raw(
			"select indexdef from pg_indexes where tablename = ? and indexdef ilike ?",
			tbl, "%session_id%",
		).Scan(&defs).Error; err != nil {
			t.Fatalf("读 %s 的索引清单失败：%v", tbl, err)
		}
		if len(defs) == 0 {
			t.Errorf("%s.session_id 在全新库里没有任何索引：AutoMigrate 会照标签建库，"+
				"请检查该模型 SessionID 字段的 gorm 标签是否还带 index", tbl)
			continue
		}
		t.Logf("%s.session_id 索引：%v", tbl, defs)
	}
}
