// rag_product_global_wiring_test.go RagProduct 全局装配桥回归（AV-04c 批次6 刀2）。
//
// NewRagProductServiceFromGlobal 收口为 kbrepo.NewRagConfigRepositoryDefault
// 默认构造（AV-09 回归修复：批次6 的根 repository 桥与 knowledge/repository
// 测试闭合成 test-only import 环，桥撤除，装配改走 kbrepo 仓储侧 Default
// 构造）后：有库时仓储绑到测试库、读写路径行为不变；service 文件不再出现
// 任何 .GetDB（AST 守卫棘轮锁定）。
package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestRagProductService_FromGlobal_ListOnTestDB(t *testing.T) {
	db := testutil.NewTestDB(t, &model.RagProduct{})
	if err := db.Create(&model.RagProduct{ID: "rp_wiring", Name: "桥接回归产品", IsActive: true}).Error; err != nil {
		t.Fatalf("seed rag product: %v", err)
	}

	svc := NewRagProductServiceFromGlobal()
	if svc == nil || svc.repo == nil {
		t.Fatal("FromGlobal 应装配仓储")
	}
	products, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(products) != 1 || products[0].ID != "rp_wiring" {
		t.Fatalf("应查到测试库里的产品: %+v", products)
	}

	stats, err := svc.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats["total"] != int64(1) || stats["active"] != int64(1) {
		t.Fatalf("统计口径不符: %+v", stats)
	}
}
