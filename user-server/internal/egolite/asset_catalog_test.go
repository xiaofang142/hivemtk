package egolite

import (
	"context"
	"encoding/json"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/datatypes"
)

// 资产目录测试（平台端发布 → 用户端使用 → 下发执行 的自包含闭环）。
//
// 钉住四件事：
//  1. 发布闸：block 脚本一律拒发（复扫以当下规则为准，不认历史结论）；
//  2. 快照：发布是脚本快照 + sha256，同编码再发布版本自增；
//  3. 上下线：offline 从橱窗与执行两侧同时消失（同响应，不暴露状态）；
//  4. market 执行的服务端裁决：资产必须 published+scanPassed，writes 非空强制确认。

func newAssetEnv(t *testing.T) (EgoAssetRepository, EgoFlowRepository, *EgoAssetCatalog) {
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
	return assetRepo, flowRepo, NewEgoAssetCatalog(assetRepo, flowRepo)
}

const packScript = `const task = await taskSpace("search repos");
const page = task.page("p1");
await page.goto("https://github.com/search?q=ego");
console.log(JSON.stringify({seq:1, type:"extract", key:"repos", count: 3}));`

func TestPublishFromPackScanGate(t *testing.T) {
	ctx := context.Background()
	assetRepo, _, catalog := newAssetEnv(t)

	// block 级脚本：拒发，且库里不留行
	_, err := catalog.PublishFromPack(ctx, 1, PackPublishRequest{
		Name:    "evil",
		Domains: []string{"github.com"},
		Script:  `const fs = require("fs");`,
	})
	if err == nil {
		t.Fatal("block 脚本必须被拒发（扫描门是发布必经路径）")
	}
	if _, err := assetRepo.GetByCode(ctx, "evil"); err == nil {
		t.Fatal("拒发时不允许留下目录行")
	}

	// 域名缺失：拒发
	if _, err := catalog.PublishFromPack(ctx, 1, PackPublishRequest{
		Name: "no domain", Script: packScript,
	}); err == nil {
		t.Fatal("缺 domains 必须拒发")
	}

	a, err := catalog.PublishFromPack(ctx, 1, PackPublishRequest{
		AssetCode: "github-repo-search",
		Name:      "GitHub 仓库检索",
		Domains:   []string{"github.com"},
		Writes:    []string{"comment"},
		Entry:     "searchRepos.js",
		Script:    packScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != model.EgoAssetPublished || !a.ScanPassed {
		t.Fatalf("发布后应为 published 且扫描通过: %+v", a)
	}
	if a.Entry != "tools/searchrepos.js" {
		t.Fatalf("entry 必须 slug 化落到 tools/ 下，got %q", a.Entry)
	}
	if a.ScriptHash == "" || a.Version != 1 {
		t.Fatalf("快照字段缺失: %+v", a)
	}

	// 同编码再发布：版本自增（快照不可变语义）
	again, err := catalog.PublishFromPack(ctx, 1, PackPublishRequest{
		AssetCode: "github-repo-search", Name: "GitHub 仓库检索 v2",
		Domains: []string{"github.com"}, Script: packScript + "\n// v2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.Version != 2 || again.ID != a.ID {
		t.Fatalf("同编码再发布应版本自增且同一行: id=%d ver=%d", again.ID, again.Version)
	}
}

func TestPublishFromFlowRequiresSubmitted(t *testing.T) {
	ctx := context.Background()
	assetRepo, flowRepo, catalog := newAssetEnv(t)

	domains, _ := json.Marshal([]string{"github.com"})
	writes, _ := json.Marshal([]string{"submit"})
	f := &model.EgoliteFlow{
		UserID: 9, Name: "Repo Scout", DomainsJSON: datatypes.JSON(domains),
		WritesJSON: datatypes.JSON(writes), Script: packScript,
		Status: model.EgoFlowDraft, Version: 1, ScanPassed: true,
	}
	if err := flowRepo.Create(ctx, f); err != nil {
		t.Fatal(err)
	}

	// draft 不可直接发布（必须走 submit 审核）
	if _, err := catalog.PublishFromFlow(ctx, 1, f.ID); err == nil {
		t.Fatal("draft 流程不可直接发布为资产")
	}

	// 用户侧提交审核后，管理端审核发布
	if err := flowRepo.Update(ctx, f.ID, 9, map[string]any{"status": model.EgoFlowSubmitted}); err != nil {
		t.Fatal(err)
	}
	a, err := catalog.PublishFromFlow(ctx, 1, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.AssetCode != "repo-scout" || a.Source != model.EgoAssetSourceFlow || a.SourceFlowID != f.ID {
		t.Fatalf("资产编码/来源不符: %+v", a)
	}
	if a.PublisherUserID != 1 {
		t.Fatalf("发布人应为管理端操作者: %d", a.PublisherUserID)
	}
	got, err := flowRepo.GetByID(ctx, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.EgoFlowPublished {
		t.Fatalf("审核通过后 flow 应回到 published，got %s", got.Status)
	}
	if _, err := assetRepo.GetByCode(ctx, "repo-scout"); err != nil {
		t.Fatalf("目录条目未落库: %v", err)
	}
}

func TestOfflineHidesAsset(t *testing.T) {
	ctx := context.Background()
	_, _, catalog := newAssetEnv(t)

	a, err := catalog.PublishFromPack(ctx, 1, PackPublishRequest{
		AssetCode: "pack-one", Name: "pack one", Domains: []string{"a.com"}, Script: packScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := catalog.PublicList(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("橱窗应有 1 条: %v %d", err, len(list))
	}
	out, err := catalog.Offline(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != model.EgoAssetOffline {
		t.Fatalf("下线后状态应为 offline: %s", out.Status)
	}
	if list, _ = catalog.PublicList(ctx); len(list) != 0 {
		t.Fatalf("下线资产不得出现在橱窗: %d", len(list))
	}
	if _, err := catalog.Offline(ctx, 99999); err == nil {
		t.Fatal("下线不存在的资产必须报错")
	}
}

func TestMarketStartValidatesCatalog(t *testing.T) {
	ctx := context.Background()
	execRepo, _, flowRepo, assetRepo, registry, svc := newExecTestEnv(t)
	catalog := NewEgoAssetCatalog(assetRepo, flowRepo)
	_ = execRepo
	_ = registry

	// 未发布资产：拒绝
	if _, err := svc.Start(ctx, 5, StartRequest{Origin: "market", AssetID: "nope"}); err != ErrAssetNotFound {
		t.Fatalf("不存在的资产应 ErrAssetNotFound，got %v", err)
	}

	a, err := catalog.PublishFromPack(ctx, 1, PackPublishRequest{
		AssetCode: "market-one", Name: "market one",
		Domains: []string{"a.com"}, Writes: []string{"post"}, Script: packScript,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 已上架资产：entry 缺省用资产入口；writes 非空 → 强制 require
	e, err := svc.Start(ctx, 5, StartRequest{
		Origin: "market", AssetID: "market-one", ConfirmPolicy: "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.Entry != a.Entry {
		t.Fatalf("entry 应缺省取资产入口，got %q want %q", e.Entry, a.Entry)
	}
	if e.ConfirmPolicy != "require" {
		t.Fatalf("writes 非空的资产执行必须强制 confirm=require，got %q", e.ConfirmPolicy)
	}
	if e.AssetVer != "1" {
		t.Fatalf("资产版本应快照进执行行: %q", e.AssetVer)
	}

	// 引用计数（弱计数）
	got, _ := assetRepo.GetByCode(ctx, "market-one")
	if got.InstallCount != 1 {
		t.Fatalf("install_count 应 +1，got %d", got.InstallCount)
	}

	// 下线后：与不存在同响应
	if _, err := catalog.Offline(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(ctx, 6, StartRequest{Origin: "market", AssetID: "market-one"}); err != ErrAssetNotFound {
		t.Fatalf("下线资产执行应 ErrAssetNotFound，got %v", err)
	}
}

func TestMarketStartFailsClosedWithoutCatalog(t *testing.T) {
	ctx := context.Background()
	execRepo, evRepo, flowRepo, _, registry, _ := newExecTestEnv(t)
	svcNoCatalog := NewEgoExecutionService(execRepo, evRepo, flowRepo, nil, registry)
	if _, err := svcNoCatalog.Start(ctx, 5, StartRequest{Origin: "market", AssetID: "x"}); err != ErrAssetUnavailable {
		t.Fatalf("装配缺资产目录时 market 必须 fail-closed，got %v", err)
	}
}

func TestListInstallableForUserMergesCatalog(t *testing.T) {
	ctx := context.Background()
	assetRepo, flowRepo, catalog := newAssetEnv(t)

	// 目录资产 + 同名用户流程：目录资产胜出（同 slug 不出现两份）
	_, err := catalog.PublishFromPack(ctx, 1, PackPublishRequest{
		AssetCode: "shared-slug", Name: "shared slug",
		Domains: []string{"a.com"}, Script: packScript + "\n// catalog",
	})
	if err != nil {
		t.Fatal(err)
	}
	domains, _ := json.Marshal([]string{"b.com"})
	own := &model.EgoliteFlow{
		UserID: 3, Name: "shared slug", DomainsJSON: datatypes.JSON(domains),
		Script: packScript + "\n// user", Status: model.EgoFlowPublished, Version: 1, ScanPassed: true,
	}
	if err := flowRepo.Create(ctx, own); err != nil {
		t.Fatal(err)
	}
	other := &model.EgoliteFlow{
		UserID: 3, Name: "my private flow", DomainsJSON: datatypes.JSON(domains),
		Script: packScript + "\n// other", Status: model.EgoFlowPublished, Version: 1, ScanPassed: true,
	}
	if err := flowRepo.Create(ctx, other); err != nil {
		t.Fatal(err)
	}

	got, err := ListInstallableForUser(ctx, flowRepo, assetRepo, 3)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]InstallableAsset{}
	for _, a := range got {
		byID[a.ID] = a
	}
	if len(got) != 2 {
		t.Fatalf("应为 2 条（目录 1 + 自有 1，同 slug 合并），got %d: %+v", len(got), got)
	}
	if a, ok := byID["shared-slug"]; !ok || a.AssetID != "shared-slug" || a.Source != model.EgoAssetSourcePack {
		t.Fatalf("同 slug 应取目录资产: %+v", byID["shared-slug"])
	}
	if a, ok := byID["my-private-flow"]; !ok || a.FlowID != other.ID {
		t.Fatalf("自有流程应带 flow_id: %+v", byID["my-private-flow"])
	}
	// 目录资产入口直接取自资产（发布时已 slug 化），不再二次派生
	if byID["shared-slug"].Entry != "tools/shared-slug.js" {
		t.Fatalf("目录资产入口不符: %q", byID["shared-slug"].Entry)
	}
}
