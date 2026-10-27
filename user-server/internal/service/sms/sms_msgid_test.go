package sms

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

// C-14 送达率链的单号 join 键：外发侧把运营商消息号落进台账、回执侧按它回写终态。
// 此前 BizId/SerialNo 在渠道回包里解析后被丢弃，sms_records 一行也接不到送达回执。

// TestSendSmsStoresProviderMsgID 三家渠道成功腿：官方回包里的消息号必须落进台账 MsgID。
// 漏了不红不报——发送照旧成功，只是送达回执从此永远对不上外发行，送达率只能靠
// 送达表自数自（没回执的行从分母里消失）。
func TestSendSmsStoresProviderMsgID(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		respBody string
		want     string
	}{
		{"aliyun BizId", "aliyun",
			`{"Code":"OK","Message":"已受理","RequestId":"r1","BizId":"biz-msg-1001"}`, "biz-msg-1001"},
		{"tencent SerialNo", "tencent",
			`{"Response":{"RequestId":"r2","SendStatusSet":[{"SerialNo":"tc-serial-2002","Code":"Ok"}]}}`, "tc-serial-2002"},
		{"huawei smsId", "huawei",
			`{"code":"000000","message":"成功","result":[{"smsId":"hw-sms-3003","status":"000000"}]}`, "hw-sms-3003"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			phones := []string{"13812345701", "13812345702", "13812345703"}
			rec := successVendorCall(t, tc.provider, tc.respBody,
				phones[i], "您的验证码是 12345")
			if rec.MsgID != tc.want {
				t.Fatalf("台账 MsgID = %q，期望 %q——送达回执按它回连，落空即整条送达率链断", rec.MsgID, tc.want)
			}
		})
	}
}

// TestSendSmsNoMsgIdStillSends 渠道成功但没带消息号（旧网关/兜底形状）：发送仍成功、
// MsgID 落空串，不许因为取不到单号把成功腿判成失败。
func TestSendSmsNoMsgIdStillSends(t *testing.T) {
	rec := successVendorCall(t, "tencent",
		`{"Response":{"RequestId":"r3"}}`,
		"13812345704", "您的验证码是 12399")
	if rec.Status != "sent" {
		t.Fatalf("无消息号不是发送失败：status = %q，期望 sent", rec.Status)
	}
	if rec.MsgID != "" {
		t.Fatalf("回包没给单号，MsgID 应为空串，实际 %q", rec.MsgID)
	}
}

// TestDeliveryReceiptMarksLedgerByMsgID 回执侧闭环：送达回执按 MsgID 把台账行 sent→delivered；
// 随后同单号的失败回执（迟到/重复）不得回撬已落终态的行。
func TestDeliveryReceiptMarksLedgerByMsgID(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.SmsRecord{},
		&model.SmsDeliveryStatus{},
	)
	db.SetTestDB(database)

	tracker := NewSmsDeliveryTrackerService(nil, nil)

	rec := &model.SmsRecord{
		Phone: "13812345800", Content: "您的验证码是 12400",
		Provider: "aliyun", Status: "sent", MsgID: "biz-msg-4004",
	}
	if err := database.Create(rec).Error; err != nil {
		t.Fatalf("预置台账行: %v", err)
	}

	if err := tracker.RecordFromProvider(context.Background(), &ProviderDeliveryReport{
		MessageID: "biz-msg-4004", Phone: "13812345800", Provider: "aliyun",
		Status: "DELIVERED", DeliveredAt: "2026-10-25T12:00:00+08:00",
	}); err != nil {
		t.Fatalf("送达回执不该失败: %v", err)
	}

	got := &model.SmsRecord{}
	if err := database.First(got, rec.ID).Error; err != nil {
		t.Fatalf("回读台账行: %v", err)
	}
	if got.Status != model.SmsStatusDelivered {
		t.Fatalf("回执后台账 status = %q，期望 delivered（回执没回写 = 台账永远停在 sent）", got.Status)
	}

	// 同单号迟到失败回执：不得把已 delivered 的行改回 failed。
	if err := tracker.RecordFromProvider(context.Background(), &ProviderDeliveryReport{
		MessageID: "biz-msg-4004", Phone: "13812345800", Provider: "aliyun",
		Status: "FAILED", ErrorCode: "ERR_4001", ErrorMsg: "号码无效",
	}); err != nil {
		t.Fatalf("迟到失败回执不该上抛（送达表仍要收下这条事实）: %v", err)
	}
	got2 := &model.SmsRecord{}
	if err := database.First(got2, rec.ID).Error; err != nil {
		t.Fatalf("二次回读台账行: %v", err)
	}
	if got2.Status != model.SmsStatusDelivered {
		t.Fatalf("迟到失败回执回撬了终态：status = %q，仍应为 delivered", got2.Status)
	}
}

// TestDeliveryReceiptFailedMarksLedger 失败回执：台账行 sent→failed 且带渠道错误码，
// 台账页面/导出由此能看到失败原因，而不必去送达表对单号。
func TestDeliveryReceiptFailedMarksLedger(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.SmsRecord{},
		&model.SmsDeliveryStatus{},
	)
	db.SetTestDB(database)

	tracker := NewSmsDeliveryTrackerService(nil, nil)

	rec := &model.SmsRecord{
		Phone: "13812345801", Content: "您的验证码是 12411",
		Provider: "tencent", Status: "sent", MsgID: "tc-serial-5005",
	}
	if err := database.Create(rec).Error; err != nil {
		t.Fatalf("预置台账行: %v", err)
	}

	if err := tracker.RecordFromProvider(context.Background(), &ProviderDeliveryReport{
		MessageID: "tc-serial-5005", Phone: "13812345801", Provider: "tencent",
		Status: "FAILED", ErrorCode: "ERR_4002", ErrorMsg: "黑名单",
	}); err != nil {
		t.Fatalf("失败回执不该失败: %v", err)
	}

	got := &model.SmsRecord{}
	if err := database.First(got, rec.ID).Error; err != nil {
		t.Fatalf("回读台账行: %v", err)
	}
	if got.Status != model.SmsStatusFailed {
		t.Fatalf("失败回执后台账 status = %q，期望 failed", got.Status)
	}
	if got.ErrorCode != "ERR_4002" {
		t.Fatalf("台账 ErrorCode = %q，期望渠道错误码 ERR_4002", got.ErrorCode)
	}
}
