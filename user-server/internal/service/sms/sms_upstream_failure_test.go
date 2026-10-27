package sms

// 契约：短信**渠道调用失败**必须被声明成"上游失败"，不能把渠道原文直接抛出去。
//
// 坑：上层判定"记录不存在"是**按错误文本**做的（文本含"不存在"或"not found" ⇒ 404），
// 而渠道原文常自带这些词 —— 阿里云回 "Specified access key is not found." ——
// 于是"密钥填错了"被翻译成 404「记录不存在」，
// 把操作者引向"去查记录"这个完全错误的方向（真因是改密钥）。
//
// 修法：渠道错误包成 bizerr.CodePlatformUnavail（5xxx = 显式声明"这是上游失败"），
// 上层据此判"不是记录没了"，HTTP 走 502 且消息带真因。
//
// 本文件锁**两个入口**：首次发送（SendSms）与重发（ResendSms）——
// 两者都直连渠道，只修一个就漏一个（第一次只修了 SendSms，ResendSms 仍在漏）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// stubAliyunGateway 起一个只会回"密钥找不到"的阿里云桩。
// 关键点：回包里带 "not found" —— 正是会骗过"按文本判记录不存在"的那个词。
func stubAliyunGateway(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Code":"InvalidAccessKeyId.NotFound","Message":"Specified access key is not found."}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newSmsSvcForProvider 造一个指向桩网关的 smsService。
func newSmsSvcForProvider(t *testing.T, db *gorm.DB, aliyunURL string) *smsService {
	t.Helper()
	svc := NewSmsService(newTestSmsRepository(db)).(*smsService)
	svc.aliyunAPIURL = aliyunURL
	return svc
}

// TestSmsSendChannelFailureIsUpstreamFailure 首次发送：渠道失败 ⇒ 上游失败，不是 not-found。
func TestSmsSendChannelFailureIsUpstreamFailure(t *testing.T) {
	db := setupSmsServiceTestDB(t)
	gw := stubAliyunGateway(t)
	svc := newSmsSvcForProvider(t, db, gw.URL+"/")

	// 配一个阿里云通道（否则 GetConfig 回落默认 provider=aliyun，正好也是阿里云），
	// 并给出**假的**凭证：渠道桩会回"密钥找不到"，正是要复现的那个上游错误。
	if err := db.Create(&model.SmsConfig{
		DefaultProvider: "aliyun", RateLimit: 100, DailyLimit: 10000, RetryTimes: 0,
	}).Error; err != nil {
		t.Fatalf("短信配置落库失败：%v", err)
	}
	if err := db.Create(&model.SmsAliyunConfig{
		AccessKeyID: "ak-fake", AccessKeySecret: "sk-fake", SignName: "测试签名",
	}).Error; err != nil {
		t.Fatalf("阿里云通道配置落库失败：%v", err)
	}

	err := svc.SendSms(context.Background(), &dto.SmsSendRequest{
		Phone: "13800138000", Content: "api 测试",
	})
	if err == nil {
		t.Fatal("渠道失败必须返回错误")
	}
	if !bizerr.IsUpstreamFailure(err) {
		t.Errorf("渠道失败必须声明成上游失败（bizerr 5xxx），实际 err=%v", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("错误串应保留渠道原文（便于定位），实际=%v", err)
	}
}

// TestSmsResendChannelFailureIsUpstreamFailure 重发：渠道失败 ⇒ 上游失败，不是 not-found。
//
// 这条腿是"只修一个入口就漏一个"的现场：首次发送修了、重发没修，
// 于是重发仍然 404「记录不存在」。
func TestSmsResendChannelFailureIsUpstreamFailure(t *testing.T) {
	db := setupSmsServiceTestDB(t)
	gw := stubAliyunGateway(t)
	svc := newSmsSvcForProvider(t, db, gw.URL+"/")

	if err := db.Create(&model.SmsConfig{
		DefaultProvider: "aliyun", RateLimit: 100, DailyLimit: 10000, RetryTimes: 0,
	}).Error; err != nil {
		t.Fatalf("短信配置落库失败：%v", err)
	}
	if err := db.Create(&model.SmsAliyunConfig{
		AccessKeyID: "ak-fake", AccessKeySecret: "sk-fake", SignName: "测试签名",
	}).Error; err != nil {
		t.Fatalf("阿里云通道配置落库失败：%v", err)
	}
	// 造一条 failed 的台账行（重发只允许重发 failed 的）。
	rec := &model.SmsRecord{
		Phone: "13800138000", Content: "api 测试", Provider: "aliyun", Status: "failed",
	}
	if err := db.Create(rec).Error; err != nil {
		t.Fatalf("短信台账落库失败：%v", err)
	}

	err := svc.ResendSms(context.Background(), rec.ID)
	if err == nil {
		t.Fatal("渠道失败必须返回错误")
	}
	if !bizerr.IsUpstreamFailure(err) {
		t.Errorf("重发的渠道失败也必须声明成上游失败（bizerr 5xxx），实际 err=%v", err)
	}
}
