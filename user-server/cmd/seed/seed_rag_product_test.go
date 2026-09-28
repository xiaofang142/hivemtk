package main

import (
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func setupRagProductTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.RagProduct{})
}

// 种子跑完必须能查到产品行。
//
// 缺陷本体：cmd/seed 把客服智能体的 rag_product_ids 绑到 'hivemtk-platform-cs'，
// 而这个产品行的**唯一**产码是 migrations/031_platform_cs_rag_seed.sql，
// 该文件按它自己的头注释「不会被任何自动路径执行」（compose 的 initdb.d 只挂
// init-user-db.sql，Go 侧无人读 .sql）⇒ 装完种子的库里绑着一个不存在的产品，
// 后台「知识库」列表里看不到它、也调不了它的检索参数。
func TestEnsurePlatformCSRagProductCreatesRow(t *testing.T) {
	db := setupRagProductTestDB(t)
	if err := db.Exec("DELETE FROM rag_products WHERE id = ?", platformCSProductID).Error; err != nil {
		t.Fatalf("清理前置失败：%v", err)
	}

	if err := ensurePlatformCSRagProduct(db); err != nil {
		t.Fatalf("ensurePlatformCSRagProduct 返回错误：%v", err)
	}

	var got model.RagProduct
	if err := db.First(&got, "id = ?", platformCSProductID).Error; err != nil {
		t.Fatalf("种子后查不到 RAG 产品行（id=%s）：%v", platformCSProductID, err)
	}
	if got.VectorTable == "" {
		t.Errorf("产品行 vector_table 为空，检索侧按它定位向量表：%+v", got)
	}
	if !got.IsActive {
		t.Error("产品行 IsActive=false，绑定它的智能体检索会被判为未启用")
	}
}

// 重复执行只留一行，且不把运行期攒出来的计数抹回零。
//
// doc_count / chunk_count 由导入链路（知识文档入库、expand_knowledge_base*.py）维护，
// 种子只该补"注册行缺失"这一格，重跑种子不能把已灌进来的知识库统计清零。
func TestEnsurePlatformCSRagProductIdempotent(t *testing.T) {
	db := setupRagProductTestDB(t)
	if err := db.Exec("DELETE FROM rag_products WHERE id = ?", platformCSProductID).Error; err != nil {
		t.Fatalf("清理前置失败：%v", err)
	}
	for i := 0; i < 2; i++ {
		if err := ensurePlatformCSRagProduct(db); err != nil {
			t.Fatalf("第 %d 次 ensurePlatformCSRagProduct 返回错误：%v", i+1, err)
		}
	}
	if err := db.Model(&model.RagProduct{}).Where("id = ?", platformCSProductID).
		Update("doc_count", 13).Error; err != nil {
		t.Fatalf("写入 doc_count 失败：%v", err)
	}

	if err := ensurePlatformCSRagProduct(db); err != nil {
		t.Fatalf("第三次 ensurePlatformCSRagProduct 返回错误：%v", err)
	}

	var rows int64
	if err := db.Model(&model.RagProduct{}).Where("id = ?", platformCSProductID).Count(&rows).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if rows != 1 {
		t.Errorf("产品行数 = %d，期望 1（幂等 upsert 不得插重复行）", rows)
	}
	var after model.RagProduct
	if err := db.First(&after, "id = ?", platformCSProductID).Error; err != nil {
		t.Fatalf("重跑后查不到产品行：%v", err)
	}
	if after.DocCount != 13 {
		t.Errorf("重跑种子把 doc_count 改成 %d，期望保留导入链路写入的 13", after.DocCount)
	}
}
