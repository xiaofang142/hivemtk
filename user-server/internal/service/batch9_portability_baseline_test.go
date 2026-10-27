// batch9_portability_baseline_test.go 钉住携号转网流水的「基线来自库」这一条。
//
// DetectAndRecordPortability 的判据是「新 carrier ≠ 上一次 carrier」。上一次
// carrier 有两个来源：进程内缓存，以及 `sms_number_portability_logs` 里最近一条
// 流水。原实现先把本号码写进缓存、再去回库补基线 —— 而 loadCarrierCache 只填空缺
// （`if _, ok := s.carrierCache[r.Phone]; !ok`），预写把库里的基线挡在外面，回库
// 之后读回来的就是自己刚写进去的值，于是 `original == newCarrier` 恒成立：
//
//	本进程第一次见到某个号码时，无论它在库里是什么 carrier，都不会记下转网流水。
//
// 这个用例造的就是这个场景：库里已有基线 unicom，回执推来 telecom。
package service

import (
	"context"
	smssvc "hivemtk-user/internal/service/sms"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func TestSmsDeliveryTracker_DetectAndRecordPortability_BaselineFromDB(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SmsNumberPortabilityRecord{})
	repo := repository.NewSmsDeliveryRepository(db)
	ctx := context.Background()

	const phone = "13800138000"
	if err := repo.CreatePortability(ctx, &model.SmsNumberPortabilityRecord{
		Phone:           phone,
		OriginalCarrier: model.SmsCarrierMobile,
		CurrentCarrier:  model.SmsCarrierUnicom,
		DetectedAt:      time.Now().Add(-time.Hour),
		Source:          "webhook",
		CreatedAt:       time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("准备基线失败: %v", err)
	}

	svc := smssvc.NewSmsDeliveryTrackerService(nil, nil)
	if err := svc.DetectAndRecordPortability(ctx, phone, "telecom"); err != nil {
		t.Fatalf("DetectAndRecordPortability 返回错误: %v", err)
	}

	var rows []model.SmsNumberPortabilityRecord
	if err := db.Where("phone = ?", phone).Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("读取流水失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("期望「库基线 unicom → 回执 telecom」记下 1 条新流水（共 2 行），实际 %d 行：回库基线被自己刚写进缓存的值挡住了", len(rows))
	}
	last := rows[1]
	if last.OriginalCarrier != model.SmsCarrierUnicom || last.CurrentCarrier != model.SmsCarrierTelecom {
		t.Fatalf("流水应为 unicom→telecom，实际 %s→%s", last.OriginalCarrier, last.CurrentCarrier)
	}
}

// TestSmsDeliveryTracker_DetectAndRecordPortability_NoBaselineNoRecord
// 没有基线（缓存与库都没有）时不写流水：一行 OriginalCarrier 为空的「转网」事件
// 只会在报表里混进没有前值的噪音。
func TestSmsDeliveryTracker_DetectAndRecordPortability_NoBaselineNoRecord(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SmsNumberPortabilityRecord{})

	svc := smssvc.NewSmsDeliveryTrackerService(nil, nil)
	if err := svc.DetectAndRecordPortability(context.Background(), "13900139000", "telecom"); err != nil {
		t.Fatalf("DetectAndRecordPortability 返回错误: %v", err)
	}

	var n int64
	if err := db.Model(&model.SmsNumberPortabilityRecord{}).Count(&n).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("没有基线时不应写流水，实际写了 %d 行", n)
	}
}
