package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

// successVendorCall 三家共用的成功腿骨架：装对应家的桩与配置、发一条、把台账那一行读回来。
//
// 桩回包按各家**官方成功形状**给（阿里云 Code=OK／腾讯 Response 里不带 Error／华为 code=000000
// 且键是小写），因为这正是本文件此前六条腿的共同盲区——它们全回业务错，于是
// `dispatchToProvider` 的 `err == nil` 那一路（以及 SendSms 里 `Status="sent"` + `SendTime=&sentTime`）
// 一次都没被执行过。
func successVendorCall(t *testing.T, provider, respBody, phone, content string) *model.SmsRecord {
	t.Helper()
	service, database := newSmsVendorStub(t, provider, respBody)

	switch provider {
	case "aliyun":
		database.Create(&model.SmsAliyunConfig{
			AccessKeyID: "test-key-id", AccessKeySecret: "test-key-secret", SignName: "test-sign",
		})
	case "tencent":
		database.Create(&model.SmsTencentConfig{
			SecretID: "test-secret-id", SecretKey: "test-secret-key",
			AppID: "test-app-id", SignName: "test-sign",
		})
	case "huawei":
		database.Create(&model.SmsHuaweiConfig{
			AppKey: "test-app-key", AppSecret: "test-app-secret",
			Sender: "test-sender", Signature: "test-signature",
		})
	}
	database.Create(&model.SmsConfig{
		DefaultProvider: provider, RateLimit: 100, DailyLimit: 10000, RetryTimes: 3,
	})

	if err := service.SendSms(context.Background(),
		&dto.SmsSendRequest{Phone: phone, Content: content}); err != nil {
		t.Fatalf("官方回「已受理」时 SendSms 不该上抛：%v", err)
	}

	rec := &model.SmsRecord{}
	if firstErr := database.Where("phone = ?", phone).First(rec).Error; firstErr != nil {
		t.Fatalf("发送后台账应有记录: %v", firstErr)
	}
	return rec
}

// assertSentLedger 成功行的台账形状：状态 sent、SendTime 有值、且**不带**失败原因。
//
// 三条断言各钉一处被删掉就会变样的写法：
// ① `record.Status = "sent"`（漏了 → 行永远停在 "sending"，看板上是一条既不失败也不成功的悬空行）；
// ② `record.SendTime = &sentTime`（漏了/给了零值 → 送达时间这一列整片空白，重发与统计都失去基准）；
// ③ 成功路径**不**写 ErrorCode/ErrorMsg（两家成功时产码交回的是硬编码 "OK"，若有人把它顺手落账，
//
//	"有 code 就是失败行"的查询口径会把成功行算进失败率）。
func assertSentLedger(t *testing.T, rec *model.SmsRecord, provider string) {
	t.Helper()
	if rec.Provider != provider {
		t.Errorf("台账 provider = %q, want %q（说明没走这家那条成功分支）", rec.Provider, provider)
	}
	if rec.Status != "sent" {
		t.Errorf("官方受理后状态 = %q, want sent（\"sending\" 悬空＝success 分支没走完）", rec.Status)
	}
	if rec.SendTime == nil || rec.SendTime.IsZero() {
		t.Error("受理后 SendTime 必须是本次解析出的发送时间，留空＝送达基准没了")
	}
	if rec.ErrorCode != "" || rec.ErrorMsg != "" {
		t.Errorf("成功行不该带失败原因：code=%q msg=%q", rec.ErrorCode, rec.ErrorMsg)
	}
	if rec.Status == "sent" && rec.SendTime != nil && time.Since(*rec.SendTime) > time.Hour {
		t.Errorf("SendTime = %v，与当下相差超过 1 小时（不是本次发送时间）", *rec.SendTime)
	}
}

// TestSmsService_ResendSms_SuccessClearsFailureAndMarksSent 重发成功腿（同一条台账形状的第二站点）。
//
// `record.SendTime = &sentTime` / `record.Status = "sent"` 这两句在 sms.go 里**出现两次**
// （首次发送与重发各一份，逐字相同），变异格因此也必须两格（S16/S18）：改一处、另一处照旧，
// 只测其中一个入口的那条腿就会对另一半失明。
//
// 这一格还额外钉一份只有重发入口才有的承诺：进入重发时把上一轮的 `ErrorCode/ErrorMsg` 清空。
// 漏了清空 ⇒ 台账上是"状态 sent、错误码还挂着上一次失败的原因"，按"有 code 即失败"取数的
// 重试归类与看板就会把这批成功量算成失败 —— 由 `assertSentLedger` 的第 ③ 条认出，
// 变异格 S20/S21 各摘掉那两行清空里的一行。
func TestSmsService_ResendSms_SuccessClearsFailureAndMarksSent(t *testing.T) {
	service, database := newSmsVendorStub(t, "aliyun", `{"Code":"OK","Message":"已受理","RequestId":"req-2"}`)
	database.Create(&model.SmsConfig{
		DefaultProvider: "aliyun", RateLimit: 100, DailyLimit: 10000, RetryTimes: 3,
	})
	database.Create(&model.SmsAliyunConfig{
		AccessKeyID: "test-key-id", AccessKeySecret: "test-key-secret", SignName: "test-sign",
	})
	record := &model.SmsRecord{
		Phone: "13812345690", Content: "【测试签名】您的验证码是 12390",
		Provider: "aliyun", Status: "failed",
		ErrorCode: "isv.ACCOUNT_NOT_EXISTS", ErrorMsg: "账号不存在",
	}
	database.Create(record)

	if err := service.ResendSms(context.Background(), record.ID); err != nil {
		t.Fatalf("官方受理后 ResendSms 不该上抛：%v", err)
	}

	var rec model.SmsRecord
	if firstErr := database.First(&rec, record.ID).Error; firstErr != nil {
		t.Fatalf("读回台账行失败: %v", firstErr)
	}
	// 清空承诺的判据就是 assertSentLedger 的第 ③ 条，这里刻意不再重复断言一遍：
	// 归属靠**测试名**区分——三枚 SendSms 成功腿的夹具里台账行本来就是干净的，③若开火只能是
	// "成功路径自己写了错误码"；只有这一格的夹具带着上一轮的 code/msg，③开火即"清空没做"。
	assertSentLedger(t, &rec, "aliyun")
}

// TestSmsService_SendSms_AliyunSuccess 阿里云成功腿。
func TestSmsService_SendSms_AliyunSuccess(t *testing.T) {
	rec := successVendorCall(t, "aliyun",
		`{"Code":"OK","Message":"已受理","RequestId":"req-1","BizId":"biz-1"}`,
		"13812345687", "【测试签名】您的验证码是 12387")
	assertSentLedger(t, rec, "aliyun")
}

// TestSmsService_SendSms_TencentSuccess 腾讯成功腿：回包里有 Response 但**没有** Error 子对象。
//
// 这条腿顺带钉住判据的形状是 `Response.Error.Code != ""` 而不是 `Response == nil`：
// 官方成功时回的是 `{"Response":{"RequestId":"..."}}`，判据若写成"整个 Response 在不在"，
// 这一格就会把成功读成成功、但把"Error 里只有 Message 没有 Code"读成失败。
func TestSmsService_SendSms_TencentSuccess(t *testing.T) {
	rec := successVendorCall(t, "tencent",
		`{"Response":{"RequestId":"tc-req-1","SendStatusSet":[{"SerialNumber":"s1","Code":"Ok"}]}}`,
		"13812345688", "您的验证码是 12388")
	assertSentLedger(t, rec, "tencent")
}

// TestSmsService_SendSms_HuaweiSuccess 华为成功腿：小写 code，成功值是 "000000"（与业务错的
// "000007" 只差一位，正是最容易被写成 `code[0] == '0'` 那类宽松判据的地方）。
func TestSmsService_SendSms_HuaweiSuccess(t *testing.T) {
	rec := successVendorCall(t, "huawei",
		`{"code":"000000","message":"成功","time":1700000000000}`,
		"13812345689", "您的验证码是 12389")
	assertSentLedger(t, rec, "huawei")
}
