package service

// sms_unsubscribed_send_test.go —— 退订号码的发送结果必须是"没发"，不能是"发成功了"（T-P5-04 第 4 片）。
//
// 为什么这一条属于本卡：LTC-29 的送达率与"按档发送数"都从外发结果统计，而
// `SendSms` 在退订分支上 `return nil` —— 调用方（`smsLikeAdapter.Send`、
// `BindProactiveReachSenders` 的短信闭包）拿到 nil 就返回 `"sms_out", nil`，
// 于是 SOP 节点记 Completed、烧掉 `reach_sent:` 幂等键，挽回队列记成功，
// 统计里则多出一条"已发送"。一条都没发出去的外发被三处记成发生过的成功，
// 这比没有数据更糟：它是有方向错的数。
//
// 判据的口径与整条 LTC 外发链一致：**退订 = 命中 DNC**，不是"渠道故障"。
// 所以这里返回 ErrDoNotContact 而不是一个新造的第三类错误 ——
// 队列侧与节点侧早已按这个哨兵分好处置（cancelled / skipped，且**不烧幂等键**），
// 造新哨兵等于要求那两处各加一个分支，漏一个就变成"退订的人被重试三次"。
//
// 本文件锁两个入口：`SendSms`（原来命中退订就 `return nil`）与 `ResendSms`
// （原来**根本没问过退订名单**，直连渠道）。后者是修前一条时顺手查出来的：
// 同一个合规判据写在 `SmsUnsubscribeService` 的注释里（"发送前必须调用"），
// 却只接在了一个出口上。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// setupSmsUnsubscribedSendDB 建两张表：退订名单（判据的输入）与发送记录（判据的输出）。
// 发送记录必须一起建，否则"没落库"这一支是靠 panic 而不是靠数出来的 0 变绿的。
// 三份渠道配置也一起建：没退订那一格要走到"凭据缺失"这一步，而不是停在缺表。
func setupSmsUnsubscribedSendDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.SmsUnsubscribe{},
		&model.SmsRecord{},
		&model.SmsConfig{},
		&model.SmsAliyunConfig{},
		&model.SmsTencentConfig{},
		&model.SmsHuaweiConfig{},
	)
	db.SetTestDB(database)
	smsNightRestrictedFn = func(time.Time) bool { return false }
	t.Cleanup(func() { smsNightRestrictedFn = isSMSNightRestricted })
	return database
}

// setUnsubscribed 直接落一行退订记录。
//
// 不走 UnsubscribePhone：那条路还会去同步全局 DNC 标志位（要碰 customers 表），
// 而本测试要断的是"发送侧读得到退订名单"，不是那条同步链。
func setUnsubscribed(t *testing.T, database *gorm.DB, phone string) {
	t.Helper()
	if err := database.Create(&model.SmsUnsubscribe{
		Phone: phone, Reason: "用户回复关键词 TD", KeywordMatched: "TD", UnsubscribedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("预置退订记录失败：%v", err)
	}
}

func newSmsSendSvc(database *gorm.DB) *smsService {
	svc := NewSmsService(repository.NewSmsRepository()).(*smsService)
	svc.SetSmsUnsubscribe(NewSmsUnsubscribeService(repository.NewSmsUnsubscribeRepository(database)))
	return svc
}

// 退订号码：返回 DNC 哨兵，且库里一行发送记录都没有。
func TestSmsSendUnsubscribedPhoneIsNotReportedAsSent(t *testing.T) {
	database := setupSmsUnsubscribedSendDB(t)
	const phone = "13900001111"
	setUnsubscribed(t, database, phone)

	err := newSmsSendSvc(database).SendSms(context.Background(),
		&dto.SmsSendRequest{Phone: phone, Content: "您好，这是外联内容"})
	if err == nil {
		t.Fatal("退订号码的 SendSms 返回了 nil ⇒ 调用方会把「一条都没发」记成「已发送」")
	}
	if !errors.Is(err, ErrDoNotContact) {
		t.Errorf("必须命中 DNC 哨兵（队列与节点按它分处置），实得 %v", err)
	}
	var n int64
	if err := database.Model(&model.SmsRecord{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("退订号码不得留下任何发送记录，实得 %d 行", n)
	}
	// 错误串会被写进队列台账与 HTTP 响应 ⇒ 不许带收件人。
	if strings.Contains(err.Error(), phone) {
		t.Errorf("错误串里不许出现手机号（会落进 last_result）：%q", err.Error())
	}
}

// 反向一格：没退订的号码不能也被判成 DNC。
//
// 少了这一格，"永远返回 DNC"这种变异能同时骗过上一条（它只断言命中哨兵）。
// 这里让流程正常往下走到配置/渠道分支，只要错不在 DNC 这一档就算通过。
func TestSmsSendSubscribedPhoneIsNotMarkedDoNotContact(t *testing.T) {
	database := setupSmsUnsubscribedSendDB(t)

	err := newSmsSendSvc(database).SendSms(context.Background(),
		&dto.SmsSendRequest{Phone: "13900002222", Content: "您好，这是外联内容"})
	if errors.Is(err, ErrDoNotContact) {
		t.Errorf("号码没退订却报 DNC ⇒ 整个渠道被这句锁死：%v", err)
	}
}

// 重发是同一条判据的第二个入口：人工在管理台点"重发"时，号码可能早已回复过 TD。
//
// 这一格先立在这里，因为 `ResendSms` 走的是 `dispatchToProvider`，**根本没问过退订名单**：
// 合规要求写在自己的服务注释里（"发送前必须调用，命中则跳过发送"），漏的正是这条支路。
func TestSmsResendToUnsubscribedPhoneIsBlocked(t *testing.T) {
	database := setupSmsUnsubscribedSendDB(t)
	const phone = "13900003333"
	setUnsubscribed(t, database, phone)
	rec := &model.SmsRecord{Phone: phone, Content: "您好，这是外联内容", Provider: "aliyun", Status: "failed", ErrorMsg: "网关超时"}
	if err := database.Create(rec).Error; err != nil {
		t.Fatal(err)
	}

	err := newSmsSendSvc(database).ResendSms(context.Background(), rec.ID)
	if !errors.Is(err, ErrDoNotContact) {
		t.Fatalf("退订号码的重发必须命中 DNC，实得 %v", err)
	}
	var back model.SmsRecord
	if err := database.First(&back, rec.ID).Error; err != nil {
		t.Fatal(err)
	}
	// 拦下时不能先把行改成 sending：那等于台账上留下一条"正在发"的假象。
	if back.Status != "failed" {
		t.Errorf("被拦下的重发不该改动原记录状态，实得 %q（err=%s）", back.Status, back.ErrorMsg)
	}
}

func TestSmsResendToSubscribedPhoneIsNotMarkedDoNotContact(t *testing.T) {
	database := setupSmsUnsubscribedSendDB(t)
	rec := &model.SmsRecord{Phone: "13900004444", Content: "您好", Provider: "aliyun", Status: "failed"}
	if err := database.Create(rec).Error; err != nil {
		t.Fatal(err)
	}

	err := newSmsSendSvc(database).ResendSms(context.Background(), rec.ID)
	if errors.Is(err, ErrDoNotContact) {
		t.Errorf("号码没退订却报 DNC：%v", err)
	}
}
