package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// TestAssetStoreFlow 集成：创建→提审→发布→评分→举报→suspend→用量归因，
// 以及 purchase 门槛 / 提审限额 / 幂等举报 / 唯一键累加。
func TestAssetStoreFlow(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.AssetListing{}, &model.AssetVersion{}, &model.AssetReport{},
		&model.AssetRating{}, &model.ExtensionUsageMonthly{}, &model.LocalAsset{},
	)
	svc := NewAssetStoreService()
	ctx := context.Background()

	l, err := svc.CreateListing(ctx, 1, 100, "ext_demo_001", "extension", "演示扩展", "通用", "用于冒烟", nil, nil)
	if err != nil {
		t.Fatalf("CreateListing: %v", err)
	}
	// 无政策声明提审被拒
	if err := svc.SubmitListing(ctx, 1, l.ID); err == nil {
		t.Fatal("无政策声明应拒绝提审")
	}
	// 补政策声明后提审成功（产生版本行）
	if err := db.Model(l).Update("policy_declaration", []byte(`{"collect":"none"}`)).Error; err != nil {
		t.Fatalf("补声明: %v", err)
	}
	if err := svc.SubmitListing(ctx, 1, l.ID); err != nil {
		t.Fatalf("SubmitListing: %v", err)
	}
	var vCnt int64
	db.Model(&model.AssetVersion{}).Where("listing_id = ?", l.ID).Count(&vCnt)
	if vCnt != 1 {
		t.Fatalf("应产生 1 条版本, got %d", vCnt)
	}

	// 评分 purchase 门槛：无 purchase 记录应拒绝
	if err := svc.Rate(ctx, 1, l.ID, 9, 999, 5, "好"); err == nil {
		t.Fatal("无购买记录应拒绝评分")
	}
	// 造购买记录后评分成功，listing 冗余列重算
	paid := int64(555)
	if err := db.Create(&model.LocalAsset{
		AssetID: "pa_1", AssetType: "extension", Industry: "通用", Name: "x", Version: "1",
		Source: model.AssetSourcePurchased, PurchaseID: &paid, TenantID: 1,
	}).Error; err != nil {
		t.Fatalf("造购买: %v", err)
	}
	if err := svc.Rate(ctx, 1, l.ID, 9, paid, 5, "好"); err != nil {
		t.Fatalf("Rate: %v", err)
	}
	var l2 model.AssetListing
	db.First(&l2, l.ID)
	if l2.RatingCount != 1 || l2.RatingAvg != 5 {
		t.Fatalf("评分冗余列未重算: avg=%v cnt=%d", l2.RatingAvg, l2.RatingCount)
	}

	// 举报幂等：第二次同类型 open 举报被拒
	if _, err := svc.Report(ctx, 1, l.ID, 42, model.AssetReportMalicious, "含恶意代码"); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if _, err := svc.Report(ctx, 1, l.ID, 42, model.AssetReportMalicious, "再来一条"); err == nil {
		t.Fatal("同 user+listing+category 未结单应去重")
	}
	// 处理举报前先走完审核链路把 listing 推到 published
	if err := svc.TransitionListing(ctx, 1, l.ID, model.AssetListingAutoCheck, 1, ""); err != nil {
		t.Fatalf("auto_check: %v", err)
	}
	if err := svc.TransitionListing(ctx, 1, l.ID, model.AssetListingManualReview, 1, ""); err != nil {
		t.Fatalf("manual_review: %v", err)
	}
	if err := svc.TransitionListing(ctx, 1, l.ID, model.AssetListingPublished, 1, ""); err != nil {
		t.Fatalf("publish: %v", err)
	}
	var rep model.AssetReport
	db.Where("listing_id = ?", l.ID).First(&rep)
	// 举报确认触发 suspend
	if err := svc.HandleReport(ctx, 1, rep.ID, 1, true, "确认违规"); err != nil {
		t.Fatalf("HandleReport: %v", err)
	}
	var l3 model.AssetListing
	db.First(&l3, l.ID)
	if l3.Status != model.AssetListingSuspended {
		t.Fatalf("确认举报应触发 suspended, got %s", l3.Status)
	}

	// 用量归因：同 (listing,key,period) 累加；key 缺失落匿名桶
	if err := svc.ReportUsage(ctx, 1, l.ID, "extkey_a", "tz3rmdj", 3); err != nil {
		t.Fatalf("ReportUsage: %v", err)
	}
	if err := svc.ReportUsage(ctx, 1, l.ID, "extkey_a", "tz3rmdj", 2); err != nil {
		t.Fatalf("ReportUsage 累加: %v", err)
	}
	if err := svc.ReportUsage(ctx, 1, l.ID, "", "", 7); err != nil {
		t.Fatalf("匿名桶: %v", err)
	}
	rows, _, err := svc.Attribution(ctx, "", 1, 50)
	if err != nil {
		t.Fatalf("Attribution: %v", err)
	}
	got := map[string]int64{}
	for _, r := range rows {
		got[r.ExtensionKey] = r.UsageCount
	}
	if got["extkey_a"] != 5 || got["<anonymous>"] != 7 {
		t.Fatalf("归因结果不符: %+v", got)
	}
}

// TestSubmitListingLimit 提审限额：7 天内提审数超限被拦。
func TestSubmitListingLimit(t *testing.T) {
	testutil.NewTestDB(t, &model.AssetListing{}, &model.AssetVersion{})
	svc := NewAssetStoreService()
	ctx := context.Background()
	for i := 0; i < AssetStoreSubmitLimit; i++ {
		l, err := svc.CreateListing(ctx, 1, 7, "code_"+string(rune('a'+i)), "prompt", "n", "", "", []byte(`{}`), nil)
		if err != nil {
			t.Fatalf("CreateListing %d: %v", i, err)
		}
		if err := svc.SubmitListing(ctx, 1, l.ID); err != nil {
			t.Fatalf("SubmitListing %d: %v", i, err)
		}
	}
	l, err := svc.CreateListing(ctx, 1, 7, "code_over", "prompt", "n", "", "", []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("CreateListing over: %v", err)
	}
	if err := svc.SubmitListing(ctx, 1, l.ID); err == nil {
		t.Fatal("超过 7 天限额应被拦截")
	}
}

// TestAssetStoreOnModeTenantCtx on 模式回归：服务层把请求 ctx 透传到仓储后，
// 带租户绑定可正常读写；无绑定仍必须 fail-closed（防串库）。
func TestAssetStoreOnModeTenantCtx(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	testutil.NewTestDB(t, &model.AssetListing{}, &model.AssetVersion{}, &model.AssetReport{},
		&model.AssetRating{}, &model.ExtensionUsageMonthly{}, &model.LocalAsset{})
	svc := NewAssetStoreService()

	ctx := tenant.WithTenantID(context.Background(), 7)
	l, err := svc.CreateListing(ctx, 7, 100, "ext_onmode_001", "extension", "on 模式扩展", "通用", "s", []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("on 模式带租户 ctx 创建失败: %v", err)
	}
	if err := svc.SubmitListing(ctx, 7, l.ID); err != nil {
		t.Fatalf("on 模式带租户 ctx 提审失败: %v", err)
	}
	if _, _, err := svc.ReviewQueue(ctx, "extension", 1, 20); err != nil {
		t.Fatalf("on 模式带租户 ctx 审核队列失败: %v", err)
	}

	// 无租户绑定：仓储查询必须 fail-closed，而不是静默查出别的租户数据。
	if _, _, err := svc.ReviewQueue(context.Background(), "extension", 1, 20); err == nil {
		t.Fatal("on 模式无 ctx 租户绑定必须 fail-closed")
	}
}
