package egolite

// params_contract_test.go —— S-11：参数契约在 Start 入口的接线（真 PG，CI 跑）。

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

const paramsSchemaFixture = `{"type":"object","required":["keyword"],
	"properties":{"keyword":{"type":"string","minLength":2},"limit":{"type":"integer","minimum":1,"maximum":20}}}`

type paramsEnv struct {
	svc       *EgoExecutionService
	flowRepo  EgoFlowRepository
	assetRepo EgoAssetRepository
	catalog   *EgoAssetCatalog
}

// newParamsEnv 一套真 PG 环境：执行域 + 资产目录（参数契约两条通道都要用）
func newParamsEnv(t *testing.T) *paramsEnv {
	t.Helper()
	db := testutil.NewTestDB(t,
		&model.EgoliteAsset{}, &model.EgoliteFlow{}, &model.EgoliteExecution{},
		&model.EgoEventLog{}, &model.EgoliteWriteClaim{},
	)
	if db == nil {
		t.Fatal("测试库不可用")
	}
	assetRepo := NewEgoAssetRepositoryWithDB(db)
	flowRepo := NewEgoFlowRepositoryWithDB(db)
	svc := NewEgoExecutionService(NewEgoExecutionRepositoryWithDB(db), NewEgoEventRepositoryWithDB(db),
		flowRepo, assetRepo, NewEgoWriteClaimRepositoryWithDB(db), NewEgoRunnerRegistry())
	return &paramsEnv{svc: svc, flowRepo: flowRepo, assetRepo: assetRepo, catalog: NewEgoAssetCatalog(assetRepo, flowRepo)}
}

// market 通道：资产登记的 params_schema 就是判据，缺必填 ⇒ 拒且不落执行行
func TestStartMarketRejectsParamsAgainstAssetSchema(t *testing.T) {
	ctx := context.Background()
	env := newParamsEnv(t)
	if _, err := env.catalog.PublishFromPack(ctx, 1, PackPublishRequest{
		AssetCode: "param-asset", Name: "param asset",
		Domains: []string{"a.com"}, Script: packScript,
		ParamsSchema: []byte(paramsSchemaFixture),
	}); err != nil {
		t.Fatal(err)
	}

	_, err := env.svc.Start(ctx, 9, StartRequest{Origin: "market", AssetID: "param-asset", Params: []byte(`{}`)})
	if !errors.Is(err, ErrParamsInvalid) {
		t.Fatalf("缺必填参数应 ErrParamsInvalid，got %v", err)
	}

	_, err = env.svc.Start(ctx, 9, StartRequest{Origin: "market", AssetID: "param-asset", Params: []byte(`{"keyword":"x"}`)})
	if !errors.Is(err, ErrParamsInvalid) {
		t.Fatalf("minLength 不符应 ErrParamsInvalid，got %v", err)
	}

	// 合规参数放行（runner 离线 ⇒ 落 created，但执行行必须建出来）
	e, err := env.svc.Start(ctx, 9, StartRequest{Origin: "market", AssetID: "param-asset", Params: []byte(`{"keyword":"蜂巢","limit":5}`)})
	if err != nil {
		t.Fatalf("合规参数应放行，got %v", err)
	}
	if e == nil || e.State != model.EgoExecCreated {
		t.Fatalf("执行行应落 created，got %+v", e)
	}
}

// draft 通道（含 DryRun）：以流程自身登记的 params_schema 为准
func TestStartDraftRejectsParamsAgainstFlowSchema(t *testing.T) {
	ctx := context.Background()
	env := newParamsEnv(t)
	f := &model.EgoliteFlow{
		UserID: 4, Name: "flow-params", Status: model.EgoFlowDraft,
		Script: "console.log(1)", ScanPassed: true,
		ParamsSchema: []byte(paramsSchemaFixture),
	}
	if err := env.flowRepo.Create(ctx, f); err != nil {
		t.Fatal(err)
	}

	_, err := env.svc.Start(ctx, 4, StartRequest{Origin: "draft", FlowID: f.ID, Params: []byte(`{"limit":0}`)})
	if !errors.Is(err, ErrParamsInvalid) {
		t.Fatalf("缺必填 + 越界应 ErrParamsInvalid，got %v", err)
	}

	e, err := env.svc.Start(ctx, 4, StartRequest{Origin: "draft", FlowID: f.ID, Params: []byte(`{"keyword":"ok"}`)})
	if err != nil {
		t.Fatalf("合规参数应放行，got %v", err)
	}
	if e == nil || e.UserID != 4 {
		t.Fatalf("执行行归属应正确，got %+v", e)
	}
}

// 无契约的资产/流程保持原行为（不能因为引入校验就把老资产卡死）
func TestStartWithoutSchemaKeepsLegacyBehavior(t *testing.T) {
	ctx := context.Background()
	env := newParamsEnv(t)
	if _, err := env.catalog.PublishFromPack(ctx, 1, PackPublishRequest{
		AssetCode: "no-schema", Name: "no schema",
		Domains: []string{"a.com"}, Script: packScript,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.Start(ctx, 3, StartRequest{Origin: "market", AssetID: "no-schema", Params: []byte(`{"free":"form"}`)}); err != nil {
		t.Fatalf("无 params_schema 时不应校验参数，got %v", err)
	}
}
