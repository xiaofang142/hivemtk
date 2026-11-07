package service

// 契约：知识 OpenAPI 数据源删除必须按产品归属校验，且"不存在"要能表达出来。
//
// D-28（实测踩出来的）：
//   - 重复删 / 删不存在的 id → repo 的 EnsureAffected 返回 gorm.ErrRecordNotFound，
//     控制器却统一兜底 500「操作失败，请稍后重试」，调用方分不清是删掉了还是本来就没有；
//   - DeleteSource 收了 productID 却直接按 id 删，同租户其它产品的数据源也能被删掉。
//
// 本文件锁两条腿：越权（产品不匹配）不得删成功、"不存在"必须回错；匹配时确实删掉。

import (
	"context"
	"testing"

	knowledgerepo "hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestOpenAPISourceDeleteScopedByProduct(t *testing.T) {
	db := testutil.NewTestDB(t, &model.KnowledgeOpenAPISource{})
	if db == nil {
		t.Fatal("测试库不可达：删除归属结论无法判定（不 Skip）")
	}
	repo := knowledgerepo.NewKnowledgeOpenAPIRepository(db)
	svc := &OpenAPIService{srcRepo: repo, docRepo: knowledgerepo.NewKnowledgeDocumentRepository(db)}

	seed := &model.KnowledgeOpenAPISource{
		TenantID:  5,
		ProductID: "p1",
		Name:      "src-p1",
		Type:      "rest",
		Endpoint:  "https://api.example.com/v1",
		Method:    "GET",
		AuthType:  "none",
		Enabled:   1,
	}
	if err := repo.Create(context.Background(), seed); err != nil {
		t.Fatalf("种子数据源落库失败：%v", err)
	}

	// 1) 产品不匹配（同租户别的产品）不得删掉。
	if err := svc.DeleteSource(context.Background(), "p2", int64(seed.ID)); err == nil {
		t.Fatal("按其它产品删除应报错（not found），实际成功后数据源可被跨产品删除")
	}
	still, err := repo.GetByProductAndID(context.Background(), "p1", int64(seed.ID))
	if err != nil || still == nil {
		t.Fatalf("越权删除后数据源应仍存在：%v", err)
	}

	// 2) 归属正确时删除生效。
	if err := svc.DeleteSource(context.Background(), "p1", int64(seed.ID)); err != nil {
		t.Fatalf("归属正确时删除应成功：%v", err)
	}
	if _, err := repo.GetByProductAndID(context.Background(), "p1", int64(seed.ID)); err == nil {
		t.Fatal("删除后仍能查到该数据源")
	}

	// 3) 重复删必须回错（控制器据此映射 404，而不是 500）。
	if err := svc.DeleteSource(context.Background(), "p1", int64(seed.ID)); err == nil {
		t.Fatal("删除不存在的数据源应报错，实际静默成功")
	}
}
