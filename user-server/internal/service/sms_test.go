package service

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupSmsServiceTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.SmsConfig{},
		&model.SmsAliyunConfig{},
		&model.SmsTencentConfig{},
		&model.SmsHuaweiConfig{},
		&model.SmsRecord{},
		&model.SmsDraft{},
		&model.SmsJob{},
		&model.SmsJobDetail{},
	)
	db.SetTestDB(database)

	smsNightRestrictedFn = func(time.Time) bool { return false }
	t.Cleanup(func() { smsNightRestrictedFn = isSMSNightRestricted })
	return database
}

func newTestSmsRepository(database *gorm.DB) repository.SmsRepository {
	return repository.NewSmsRepository()
}

// TestNewSmsService 测试创建短信服务
func TestNewSmsService(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)

	service := NewSmsService(repo)
	if service == nil {
		t.Error("Expected non-nil service")
	}
}

// TestSmsService_GetConfig_Default 测试获取默认配置
func TestSmsService_GetConfig_Default(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	config, err := service.GetConfig(context.Background())
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	if config == nil {
		t.Fatal("Expected non-nil config")
	}

	if config.DefaultProvider != "aliyun" {
		t.Errorf("Expected default provider 'aliyun', got %s", config.DefaultProvider)
	}

	if config.RateLimit != 100 {
		t.Errorf("Expected rate limit 100, got %d", config.RateLimit)
	}

	if config.DailyLimit != 10000 {
		t.Errorf("Expected daily limit 10000, got %d", config.DailyLimit)
	}
}

// TestSmsService_SaveConfig 测试保存配置
func TestSmsService_SaveConfig(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	req := &dto.SmsConfigRequest{
		DefaultProvider: "tencent",
		RateLimit:       200,
		DailyLimit:      20000,
		RetryTimes:      2,
		Aliyun: dto.SmsAliyunConfig{
			AccessKeyId:     "test-aliyun-key-id",
			AccessKeySecret: "test-aliyun-key-secret",
			SignName:        "阿里云测试签名",
		},
		Tencent: dto.SmsTencentConfig{
			SecretId:  "test-tencent-secret-id",
			SecretKey: "test-tencent-secret-key",
			AppId:     "123456",
			SignName:  "腾讯云测试签名",
		},
		Huawei: dto.SmsHuaweiConfig{
			AppKey:    "test-huawei-app-key",
			AppSecret: "test-huawei-app-secret",
			Sender:    "10690",
			Signature: "华为云测试签名",
		},
	}

	err := service.SaveConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	config, err := service.GetConfig(context.Background())
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	if config.DefaultProvider != "tencent" {
		t.Errorf("Expected default provider 'tencent', got %s", config.DefaultProvider)
	}

	if config.RateLimit != 200 {
		t.Errorf("Expected rate limit 200, got %d", config.RateLimit)
	}

	if config.Aliyun.AccessKeyId != "test-aliyun-key-id" {
		t.Errorf("Expected aliyun key id 'test-aliyun-key-id', got %s", config.Aliyun.AccessKeyId)
	}
}

// newSmsVendorStub 起一个"官方网关的回包形状"的本地服务，并把 provider 那一条腿的接口域指过去。
//
// 为什么必须有它：修复前三家网关的 URL 写死在 sendAliyun/sendTencent/sendHuawei 里，
// 于是"发送失败如何落账"这条判据的成败取决于**官方站点是否可达、回什么**：
//   - 有外网时官方回 `InvalidAccessKeyId.NotFound`，ErrorMsg 由官方文案填上 ⇒ 绿；
//   - 出站被拦（本机把 HTTPS_PROXY 指到一次性 CONNECT 记录代理做的取证格）时
//     `client.PostForm` 三次重试全失败，`resp == nil` 那条 return 出来的 errCode/errMsg 都是空串
//     ⇒ 红（`sms_test.go:212 Expected ErrorMsg to be set`），同一条用例单跑 12 次 CONNECT 里
//     本用例占 3 次（`/tmp/r44_measure/sms_proxy.log`、`/tmp/r44_measure/rec_sms.log`）。
//
// 三家都要有腿，不只 aliyun：注入点是一份产码三个字段，只装其中一条腿时另外两个字段
// 被改回常量（或写错赋值对象）不会有任何用例红 —— 普查里看不到它们，是因为今天没有
// 任何测试把 DefaultProvider 设成 tencent／huawei，不是因为它们打不出去。
//
// 夹具顺手把 ErrorMsg 清空：修复前它带着 "发送失败"，于是任何"没走到落账那一步"的早退
// （例如夜间禁发窗口开着）也能让"ErrorMsg 非空"这条断言自证为绿。
func newSmsVendorStub(t *testing.T, provider, respBody string) (svc *smsService, database *gorm.DB) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	database = setupSmsServiceTestDB(t)
	s, ok := NewSmsService(newTestSmsRepository(database)).(*smsService)
	if !ok {
		t.Fatalf("NewSmsService 返回的类型不是 *smsService：%T", s)
	}
	switch provider {
	case "aliyun":
		s.aliyunAPIURL = srv.URL + "/"
	case "tencent":
		s.tencentAPIURL = srv.URL + "/"
	case "huawei":
		s.huaweiAPIURL = srv.URL + "/"
	default:
		t.Fatalf("newSmsVendorStub 不认的 provider=%q（三家之外没有可注入的腿）", provider)
	}
	return s, database
}

// TestSmsService_SendSms 首次发送：官方回业务错 ⇒ 台账落一条 failed 且原因来自官方。
func TestSmsService_SendSms(t *testing.T) {
	service, database := newSmsVendorStub(t, "aliyun", `{"Code":"isv.SMS_SIGNATURE_ILLEGAL","Message":"签名不符合规范"}`)

	database.Create(&model.SmsConfig{
		DefaultProvider: "aliyun",
		RateLimit:       100,
		DailyLimit:      10000,
		RetryTimes:      3,
	})
	database.Create(&model.SmsAliyunConfig{
		AccessKeyID:     "test-key-id",
		AccessKeySecret: "test-key-secret",
		SignName:        "test-sign",
	})

	req := &dto.SmsSendRequest{
		Phone:   "13812345678",
		Content: "【测试签名】您的验证码是 12356",
	}

	err := service.SendSms(context.Background(), req)
	if err == nil {
		t.Fatal("官方回业务错时 SendSms 应上抛，之前只是 t.Log 一下就放行")
	}

	var rec model.SmsRecord
	if firstErr := database.Where("phone = ?", req.Phone).First(&rec).Error; firstErr != nil {
		t.Fatalf("发送后台账应有记录: %v", firstErr)
	}
	if rec.Status != "failed" {
		t.Errorf("官方回业务错后状态 = %q, want failed", rec.Status)
	}
	if rec.ErrorCode != "isv.SMS_SIGNATURE_ILLEGAL" || !strings.Contains(rec.ErrorMsg, "签名不符合规范") {
		t.Errorf("失败原因没落账：code=%q msg=%q", rec.ErrorCode, rec.ErrorMsg)
	}
}

// TestSmsService_SendSms_TencentOfficialError 腾讯腿：签名串按官方形状算，请求发往注入的域，
// 官方回的业务错误码原样落账。
//
// 这条腿钉的是 sms.go 里 `apiURL := s.tencentAPIURL` 那一句。把该字段改回写死的官方域，
// 本用例就向 sms.tencentcloudapi.com 发一次真实请求，桩里那句中文原因取不到 ⇒ 最后一条断言红
// （常驻电池 `scripts/mut_egress_pool_r30.py` 的 S2 格；读数见 docs/architecture/CHANNEL_INTEGRATION_AUDIT_2026-09.md §23.19）。
func TestSmsService_SendSms_TencentOfficialError(t *testing.T) {
	service, database := newSmsVendorStub(t, "tencent",
		`{"Response":{"Error":{"Code":"FailedSend.OperationLimitReached","Message":"到达日发送上限"}}}`)

	database.Create(&model.SmsConfig{
		DefaultProvider: "tencent",
		RateLimit:       100,
		DailyLimit:      10000,
		RetryTimes:      3,
	})
	database.Create(&model.SmsTencentConfig{
		SecretID:  "test-secret-id",
		SecretKey: "test-secret-key",
		AppID:     "1234567",
		SignName:  "测试签名",
	})

	req := &dto.SmsSendRequest{
		Phone:   "13812345681",
		Content: "您的验证码是 12357",
	}

	if err := service.SendSms(context.Background(), req); err == nil {
		t.Fatal("官方回业务错时 SendSms 应上抛")
	}

	var rec model.SmsRecord
	if firstErr := database.Where("phone = ?", req.Phone).First(&rec).Error; firstErr != nil {
		t.Fatalf("发送后台账应有记录: %v", firstErr)
	}
	if rec.Provider != "tencent" {
		t.Errorf("台账记录的 provider = %q, want tencent（说明没走 sendTencent 那条腿）", rec.Provider)
	}
	if rec.Status != "failed" {
		t.Errorf("官方回业务错后状态 = %q, want failed", rec.Status)
	}
	if rec.ErrorCode != "FailedSend.OperationLimitReached" || !strings.Contains(rec.ErrorMsg, "到达日发送上限") {
		t.Errorf("失败原因没落账：code=%q msg=%q", rec.ErrorCode, rec.ErrorMsg)
	}
}

// TestSmsService_SendSms_HuaweiOfficialError 华为腿：Basic + WSSE 头照常签，请求发往注入的域。
//
// 华为的回包键是小写的 code／message（与另两家的首字母大写不同形），所以这条腿同时钉住
// "解码形状没串到别家"这一件事：把 sms.go 里 `apiURL := s.huaweiAPIURL` 改回写死的官方域，
// 本用例的最后一条断言红（同一电池的 S3 格）。
func TestSmsService_SendSms_HuaweiOfficialError(t *testing.T) {
	service, database := newSmsVendorStub(t, "huawei", `{"code":"000007","message":"appKey不存在"}`)

	database.Create(&model.SmsConfig{
		DefaultProvider: "huawei",
		RateLimit:       100,
		DailyLimit:      10000,
		RetryTimes:      3,
	})
	database.Create(&model.SmsHuaweiConfig{
		AppKey:    "test-app-key",
		AppSecret: "test-app-secret",
		Sender:    "test-sender",
		Signature: "test-signature",
	})

	req := &dto.SmsSendRequest{
		Phone:   "13812345682",
		Content: "您的验证码是 12358",
	}

	if err := service.SendSms(context.Background(), req); err == nil {
		t.Fatal("官方回业务错时 SendSms 应上抛")
	}

	var rec model.SmsRecord
	if firstErr := database.Where("phone = ?", req.Phone).First(&rec).Error; firstErr != nil {
		t.Fatalf("发送后台账应有记录: %v", firstErr)
	}
	if rec.Provider != "huawei" {
		t.Errorf("台账记录的 provider = %q, want huawei（说明没走 sendHuawei 那条腿）", rec.Provider)
	}
	if rec.Status != "failed" {
		t.Errorf("官方回业务错后状态 = %q, want failed", rec.Status)
	}
	if rec.ErrorCode != "000007" || !strings.Contains(rec.ErrorMsg, "appKey不存在") {
		t.Errorf("失败原因没落账：code=%q msg=%q", rec.ErrorCode, rec.ErrorMsg)
	}
}

// TestSmsService_ResendSms 重发：官方回业务错 ⇒ 状态回到 failed、原因由官方给。
func TestSmsService_ResendSms(t *testing.T) {
	service, database := newSmsVendorStub(t, "aliyun", `{"Code":"isv.ACCOUNT_NOT_EXISTS","Message":"账号不存在"}`)

	smsConfig := &model.SmsConfig{
		DefaultProvider: "aliyun",
		RateLimit:       100,
		DailyLimit:      10000,
		RetryTimes:      3,
	}
	database.Create(smsConfig)

	aliyunConfig := &model.SmsAliyunConfig{
		AccessKeyID:     "test-access-key-id",
		AccessKeySecret: "test-access-key-secret",
		SignName:        "测试签名",
	}
	database.Create(aliyunConfig)

	record := &model.SmsRecord{
		Phone:     "13812345678",
		Content:   "【测试签名】您的验证码是 123456",
		Provider:  "aliyun",
		Status:    "failed",
		ErrorCode: "FAILED",
		ErrorMsg:  "",
	}
	database.Create(record)

	err := service.ResendSms(context.Background(), record.ID)
	if err == nil {
		t.Fatal("官方回业务错时 ResendSms 应上抛")
	}

	var updatedRecord model.SmsRecord
	database.First(&updatedRecord, record.ID)
	if updatedRecord.Status != "failed" {
		t.Errorf("Expected status 'failed' after API error, got %s", updatedRecord.Status)
	}
	if updatedRecord.ErrorCode != "isv.ACCOUNT_NOT_EXISTS" || !strings.Contains(updatedRecord.ErrorMsg, "账号不存在") {
		t.Errorf("失败原因没落账：code=%q msg=%q", updatedRecord.ErrorCode, updatedRecord.ErrorMsg)
	}
}

// TestSmsService_ResendSms_TransportFailureRecordsReason 传输层失败（连不上网关）也要把原因落到台账上。
//
// 这条腿钉的是 sendAliyun 里 `resp == nil` 那个 return：它把 errCode/errMsg 交回空串，
// 于是 ResendSms 写回的是"状态 failed、原因一片空白"。修复前该形状只能靠"官方站点恰好不可达"
// 复现（本机把 HTTPS_PROXY 指向一次性 CONNECT 记录代理那格实测：`sms_test.go:212 Expected ErrorMsg
// to be set` 而 Status 那条照绿），所以它既是一条用例的假红源，也是生产台账的观测盲区。
func TestSmsService_ResendSms_TransportFailureRecordsReason(t *testing.T) {
	// 只 listen 不开 accept 循环的地址：连接立刻被拒，等价于"网关连不上"而不需要外网。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	service, database := newSmsVendorStub(t, "aliyun", "")
	service.aliyunAPIURL = "http://" + addr + "/"

	database.Create(&model.SmsConfig{DefaultProvider: "aliyun", RateLimit: 100, DailyLimit: 10000, RetryTimes: 3})
	database.Create(&model.SmsAliyunConfig{
		AccessKeyID: "test-access-key-id", AccessKeySecret: "test-access-key-secret", SignName: "测试签名",
	})
	record := &model.SmsRecord{
		Phone: "13812345679", Content: "【测试签名】验证码", Provider: "aliyun", Status: "failed",
	}
	database.Create(record)

	if rerr := service.ResendSms(context.Background(), record.ID); rerr == nil {
		t.Fatal("连不上网关时 ResendSms 应上抛")
	}

	var updated model.SmsRecord
	database.First(&updated, record.ID)
	if updated.Status != "failed" {
		t.Errorf("状态 = %q, want failed", updated.Status)
	}
	if updated.ErrorMsg == "" {
		t.Error("传输层失败也必须留下原因：台账上\"failed 但原因空白\"等于没记")
	}
	if updated.ErrorCode == "" {
		t.Error("传输层失败也要留下错误码（供重试归类与看板聚合）")
	}
}

// TestSmsService_SendSms_UnknownProviderRecordsReason 配错供应商时，台账同样不能是"failed 但原因空白"。
//
// dispatchToProvider 的 default 分支是补码逻辑的第 4 个消费者（另三个是三家的传输层/解析层失败）。
// 三家都有腿可打，default 没有：它唯一的入口是 `SmsConfig.DefaultProvider` 落一个词表外的串。
// 少这条腿时，"把 default 改成立刻 return（绕过后面的补码）"在任何现有用例下都绿。
func TestSmsService_SendSms_UnknownProviderRecordsReason(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	service, ok := NewSmsService(newTestSmsRepository(database)).(*smsService)
	if !ok {
		t.Fatalf("NewSmsService 返回的类型不是 *smsService：%T", service)
	}

	database.Create(&model.SmsConfig{
		DefaultProvider: "not_a_vendor",
		RateLimit:       100,
		DailyLimit:      10000,
		RetryTimes:      3,
	})

	req := &dto.SmsSendRequest{Phone: "13812345684", Content: "【测试签名】验证码"}
	if err := service.SendSms(context.Background(), req); err == nil {
		t.Fatal("provider 不在词表里时 SendSms 应上抛")
	}

	var rec model.SmsRecord
	if firstErr := database.Where("phone = ?", req.Phone).First(&rec).Error; firstErr != nil {
		t.Fatalf("拦截前已建的台账行应仍在: %v", firstErr)
	}
	if rec.Status != "failed" {
		t.Errorf("状态 = %q, want failed", rec.Status)
	}
	// 字面量而不是 smsErrCodeUnspecified：与常量同源时，"占位码被改成空串"这一格永远绿。
	if rec.ErrorCode != "unspecified_error" {
		t.Errorf("占位错误码 = %q, want unspecified_error", rec.ErrorCode)
	}
	if !strings.Contains(rec.ErrorMsg, "unknown sms provider") {
		t.Errorf("失败原因没落账：msg=%q", rec.ErrorMsg)
	}
}

// TestNewSmsServiceDefaultsToOfficialGatewayURLs 钉住注入点的生产那一半：
// NewSmsService 建出来的实例必须指向三家官方域。
//
// 为什么单独立一条腿：其余 SMS 腿都经 newSmsVendorStub 把字段改写成 httptest 地址，
// 于是"构造函数忘了填那三行""把 tencent 的默认值填成 huawei 的域"这类失效，在现有腿下全都看不出来。
//
// 期望值写官方域字面量而不是 smsAliyunAPIURL 等常量（同 testdb_pool_test.go 的口径）：
// 与被测常量同源时，"常量值被改了"这一格永远绿。代价是改官方域时要两处同步——有意为之的一次对话。
func TestNewSmsServiceDefaultsToOfficialGatewayURLs(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	service, ok := NewSmsService(newTestSmsRepository(database)).(*smsService)
	if !ok {
		t.Fatalf("NewSmsService 返回的类型不是 *smsService：%T", service)
	}

	if got := service.aliyunAPIURL; got != "https://dysmsapi.aliyuncs.com/" {
		t.Errorf("aliyun 默认接口域 = %q, want https://dysmsapi.aliyuncs.com/", got)
	}
	if got := service.tencentAPIURL; got != "https://sms.tencentcloudapi.com/" {
		t.Errorf("tencent 默认接口域 = %q, want https://sms.tencentcloudapi.com/", got)
	}
	if got := service.huaweiAPIURL; got != "https://smsapi.cn-north-4.boe-business.huaweicloud.com:443/sms/batchSendSms/v1" {
		t.Errorf("huawei 默认接口域 = %q, want https://smsapi.cn-north-4.boe-business.huaweicloud.com:443/sms/batchSendSms/v1", got)
	}
}

// TestSmsService_ResendSms_NotFailed 测试重发非失败状态的短信
func TestSmsService_ResendSms_NotFailed(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	record := &model.SmsRecord{
		Phone:    "13812345678",
		Content:  "【测试签名】您的验证码是 123456",
		Provider: "aliyun",
		Status:   "sent",
	}
	database.Create(record)

	err := service.ResendSms(context.Background(), record.ID)
	if err == nil {
		t.Error("Expected error for resending non-failed SMS")
	}
	if err.Error() != "只有失败的短信可以重发" {
		t.Errorf("Expected '只有失败的短信可以重发', got %s", err.Error())
	}
}

// TestSmsService_CreateDraft 测试创建草稿
func TestSmsService_CreateDraft(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	req := &dto.SmsDraftCreateRequest{
		Title:   "测试草稿",
		Content: "【测试签名】这是一条测试短信",
	}

	err := service.CreateDraft(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateDraft failed: %v", err)
	}

	var count int64
	database.Model(&model.SmsDraft{}).Where("title = ?", req.Title).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 draft, got %d", count)
	}
}

// TestSmsService_GetDraftByID 测试根据 ID 获取草稿
func TestSmsService_GetDraftByID(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	draft := &model.SmsDraft{
		Title:   "测试草稿",
		Content: "【测试签名】这是一条测试短信",
	}
	database.Create(draft)

	retrievedDraft, err := service.GetDraftByID(context.Background(), draft.ID)
	if err != nil {
		t.Fatalf("GetDraftByID failed: %v", err)
	}

	if retrievedDraft.Title != "测试草稿" {
		t.Errorf("Expected title '测试草稿', got %s", retrievedDraft.Title)
	}
}

// TestSmsService_GetDraftByID_NotFound 测试获取不存在的草稿
func TestSmsService_GetDraftByID_NotFound(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	_, err := service.GetDraftByID(context.Background(), 99999)
	if err == nil {
		t.Error("Expected error for non-existent draft")
	}
}

// TestSmsService_UpdateDraft 测试更新草稿
func TestSmsService_UpdateDraft(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	draft := &model.SmsDraft{
		Title:   "旧标题",
		Content: "旧内容",
	}
	database.Create(draft)

	updateReq := &dto.SmsDraftUpdateRequest{
		Title:   "新标题",
		Content: "新内容",
	}

	err := service.UpdateDraft(context.Background(), draft.ID, updateReq)
	if err != nil {
		t.Fatalf("UpdateDraft failed: %v", err)
	}

	var updatedDraft model.SmsDraft
	database.First(&updatedDraft, draft.ID)
	if updatedDraft.Title != "新标题" {
		t.Errorf("Expected title '新标题', got %s", updatedDraft.Title)
	}
	if updatedDraft.Content != "新内容" {
		t.Errorf("Expected content '新内容', got %s", updatedDraft.Content)
	}
}

// TestSmsService_DeleteDraft 测试删除草稿
func TestSmsService_DeleteDraft(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	draft := &model.SmsDraft{
		Title:   "待删除草稿",
		Content: "这是待删除的内容",
	}
	database.Create(draft)

	err := service.DeleteDraft(context.Background(), draft.ID)
	if err != nil {
		t.Fatalf("DeleteDraft failed: %v", err)
	}

	var count int64
	database.Unscoped().Model(&model.SmsDraft{}).Where("id = ?", draft.ID).Unscoped().Count(&count)
	if count != 1 {
		database.Model(&model.SmsDraft{}).Where("id = ?", draft.ID).Count(&count)
		if count != 0 {
			t.Errorf("Expected draft to be soft-deleted, got count %d", count)
		}
	}

	var deletedAt *time.Time
	database.Unscoped().Model(&model.SmsDraft{}).Where("id = ?", draft.ID).Select("deleted_at").Scan(&deletedAt)
	if deletedAt == nil {
		t.Error("Expected draft to be soft-deleted (deleted_at should be set)")
	}
}

// TestSmsService_SendDraft 发草稿：官方回业务错 ⇒ 台账留一行 failed，且原因由官方给。
//
// 改前它写的是"注: 真实 API 会因测试凭据失败,这里只验证数据库创建了记录"，判据只有 `count == 1`：
// 一句 t.Logf 把真实错误吃掉，于是这条用例的绿要依赖 dysmsapi.aliyuncs.com **恰好可达且恰好回错**
// （整包出站普查实测它一个跑 3 次 CONNECT＝sendAliyun 的三次重试）。桩打上后次数＝0，且官方给的原因成为断言。
func TestSmsService_SendDraft(t *testing.T) {
	service, database := newSmsVendorStub(t, "aliyun", `{"Code":"isv.SMS_SIGNATURE_ILLEGAL","Message":"签名不符合规范"}`)

	database.Create(&model.SmsConfig{
		DefaultProvider: "aliyun",
		RateLimit:       100,
		DailyLimit:      10000,
		RetryTimes:      3,
	})
	database.Create(&model.SmsAliyunConfig{
		AccessKeyID:     "test-key-id",
		AccessKeySecret: "test-key-secret",
		SignName:        "test-sign",
	})

	draft := &model.SmsDraft{
		Title:   "发送草稿",
		Content: "【测试签名】这是草稿发送的内容",
	}
	database.Create(draft)

	phone := "13812345678"
	err := service.SendDraft(context.Background(), draft.ID, phone)
	if err == nil {
		t.Fatal("官方回业务错时 SendDraft 应上抛")
	}

	var count int64
	database.Model(&model.SmsRecord{}).Where("phone = ? AND content = ?", phone, draft.Content).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 SMS record, got %d", count)
	}

	var rec model.SmsRecord
	if firstErr := database.Where("phone = ?", phone).First(&rec).Error; firstErr != nil {
		t.Fatalf("发送后台账应有记录: %v", firstErr)
	}
	if rec.Status != "failed" {
		t.Errorf("官方回业务错后状态 = %q, want failed", rec.Status)
	}
	if rec.ErrorCode != "isv.SMS_SIGNATURE_ILLEGAL" || !strings.Contains(rec.ErrorMsg, "签名不符合规范") {
		t.Errorf("失败原因没落账：code=%q msg=%q", rec.ErrorCode, rec.ErrorMsg)
	}
}

// TestSmsService_SendDraft_NotFound 测试发送不存在的草稿
func TestSmsService_SendDraft_NotFound(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	err := service.SendDraft(context.Background(), 99999, "13812345678")
	if err == nil {
		t.Error("Expected error for non-existent draft")
	}
}

// TestSmsService_GetDraftList 测试获取草稿列表
func TestSmsService_GetDraftList(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	for i := 0; i < 5; i++ {
		draft := &model.SmsDraft{
			Title:   "草稿" + string(rune('0'+i)),
			Content: "内容" + string(rune('0'+i)),
		}
		database.Create(draft)
	}

	req := &dto.SmsDraftListRequest{
		Page:  1,
		Limit: 10,
	}

	drafts, total, err := service.GetDraftList(context.Background(), req)
	if err != nil {
		t.Fatalf("GetDraftList failed: %v", err)
	}

	if total != 5 {
		t.Errorf("Expected total 5, got %d", total)
	}
	if len(drafts) != 5 {
		t.Errorf("Expected 5 drafts, got %d", len(drafts))
	}
}

// TestSmsService_GetDraftList_WithTitleFilter 测试带标题过滤的草稿列表
func TestSmsService_GetDraftList_WithTitleFilter(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	database.Create(&model.SmsDraft{Title: "测试草稿 1", Content: "内容 1"})
	database.Create(&model.SmsDraft{Title: "测试草稿 2", Content: "内容 2"})
	database.Create(&model.SmsDraft{Title: "其他草稿", Content: "其他内容"})

	req := &dto.SmsDraftListRequest{
		Page:  1,
		Limit: 10,
		Title: "测试",
	}

	drafts, total, err := service.GetDraftList(context.Background(), req)
	if err != nil {
		t.Fatalf("GetDraftList failed: %v", err)
	}

	if total != 2 {
		t.Errorf("Expected total 2, got %d", total)
	}

	if len(drafts) == 0 {
		t.Error("Expected non-empty drafts list")
	}
}

// TestSmsService_CreateJob 测试创建任务
func TestSmsService_CreateJob(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	req := &dto.SmsJobCreateRequest{
		Name:      "测试任务",
		PhoneList: []string{"13812345678", "13812345679", "13812345680"},
		Content:   "【测试签名】这是一条群发短信",
	}

	err := service.CreateJob(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	var job model.SmsJob
	database.First(&job)
	if job.Name != "测试任务" {
		t.Errorf("Expected job name '测试任务', got %s", job.Name)
	}
	if job.Total != 3 {
		t.Errorf("Expected total 3, got %d", job.Total)
	}

	var detailCount int64
	database.Model(&model.SmsJobDetail{}).Count(&detailCount)
	if detailCount != 3 {
		t.Errorf("Expected 3 job details, got %d", detailCount)
	}
}

// TestSmsService_CreateJob_WithScheduleTime 测试创建定时任务
func TestSmsService_CreateJob_WithScheduleTime(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	scheduleTime := time.Now().Add(24 * time.Hour)

	req := &dto.SmsJobCreateRequest{
		Name:         "定时任务",
		PhoneList:    []string{"13812345678"},
		Content:      "【测试签名】定时发送",
		ScheduleTime: &scheduleTime,
	}

	err := service.CreateJob(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	var job model.SmsJob
	database.First(&job)
	if job.Status != "pending" {
		t.Errorf("Expected status 'pending', got %s", job.Status)
	}
}

// TestSmsService_GetJobByID 测试根据 ID 获取任务
func TestSmsService_GetJobByID(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	job := &model.SmsJob{
		Name:   "测试任务",
		Total:  10,
		Status: "pending",
	}
	database.Create(job)

	retrievedJob, err := service.GetJobByID(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("GetJobByID failed: %v", err)
	}

	if retrievedJob.Name != "测试任务" {
		t.Errorf("Expected job name '测试任务', got %s", retrievedJob.Name)
	}
}

// TestSmsService_GetJobByID_NotFound 测试获取不存在的任务
func TestSmsService_GetJobByID_NotFound(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	_, err := service.GetJobByID(context.Background(), 99999)
	if err == nil {
		t.Error("Expected error for non-existent job")
	}
}

// TestSmsService_GetJobList 测试获取任务列表
func TestSmsService_GetJobList(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	database.Create(&model.SmsJob{Name: "任务 1", Status: "pending", Total: 5})
	database.Create(&model.SmsJob{Name: "任务 2", Status: "running", Total: 10})
	database.Create(&model.SmsJob{Name: "任务 3", Status: "completed", Total: 15})

	req := &dto.SmsJobListRequest{
		Page:  1,
		Limit: 10,
	}

	jobs, total, err := service.GetJobList(context.Background(), req)
	if err != nil {
		t.Fatalf("GetJobList failed: %v", err)
	}

	if total != 3 {
		t.Errorf("Expected total 3, got %d", total)
	}
	if len(jobs) != 3 {
		t.Errorf("Expected 3 jobs, got %d", len(jobs))
	}
}

// TestSmsService_GetJobList_WithStatusFilter 测试带状态过滤的任务列表
func TestSmsService_GetJobList_WithStatusFilter(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	database.Create(&model.SmsJob{Name: "任务 1", Status: "pending", Total: 5})
	database.Create(&model.SmsJob{Name: "任务 2", Status: "running", Total: 10})
	database.Create(&model.SmsJob{Name: "任务 3", Status: "completed", Total: 15})

	req := &dto.SmsJobListRequest{
		Page:   1,
		Limit:  10,
		Status: "running",
	}

	jobs, total, err := service.GetJobList(context.Background(), req)
	if err != nil {
		t.Fatalf("GetJobList failed: %v", err)
	}

	if total != 1 {
		t.Errorf("Expected total 1, got %d", total)
	}

	if len(jobs) == 0 {
		t.Error("Expected non-empty jobs list")
	}
}

// TestSmsService_PauseJob 测试暂停任务
func TestSmsService_PauseJob(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	job := &model.SmsJob{
		Name:   "运行中任务",
		Status: "running",
	}
	database.Create(job)

	err := service.PauseJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("PauseJob failed: %v", err)
	}

	var updatedJob model.SmsJob
	database.First(&updatedJob, job.ID)
	if updatedJob.Status != "paused" {
		t.Errorf("Expected status 'paused', got %s", updatedJob.Status)
	}
}

// TestSmsService_PauseJob_InvalidStatus 测试暂停非运行中的任务
func TestSmsService_PauseJob_InvalidStatus(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	job := &model.SmsJob{
		Name:   "已完成任务",
		Status: "completed",
	}
	database.Create(job)

	err := service.PauseJob(context.Background(), job.ID)
	if err == nil {
		t.Error("Expected error for pausing non-running job")
	}
	if err.Error() != "只能暂停运行中的任务" {
		t.Errorf("Expected '只能暂停运行中的任务', got %s", err.Error())
	}
}

// TestSmsService_ResumeJob 测试继续任务
func TestSmsService_ResumeJob(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	job := &model.SmsJob{
		Name:   "暂停任务",
		Status: "paused",
	}
	database.Create(job)

	err := service.ResumeJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("ResumeJob failed: %v", err)
	}

	var updatedJob model.SmsJob
	database.First(&updatedJob, job.ID)
	if updatedJob.Status != "running" {
		t.Errorf("Expected status 'running', got %s", updatedJob.Status)
	}
}

// TestSmsService_ResumeJob_InvalidStatus 测试继续非暂停状态的任务
func TestSmsService_ResumeJob_InvalidStatus(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	job := &model.SmsJob{
		Name:   "已完成任务",
		Status: "completed",
	}
	database.Create(job)

	err := service.ResumeJob(context.Background(), job.ID)
	if err == nil {
		t.Error("Expected error for resuming non-paused job")
	}
	if err.Error() != "只能继续已暂停的任务" {
		t.Errorf("Expected '只能继续已暂停的任务', got %s", err.Error())
	}
}

// TestSmsService_StopJob 测试停止任务
func TestSmsService_StopJob(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	job := &model.SmsJob{
		Name:   "运行中任务",
		Status: "running",
	}
	database.Create(job)

	err := service.StopJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("StopJob failed: %v", err)
	}

	var updatedJob model.SmsJob
	database.First(&updatedJob, job.ID)
	if updatedJob.Status != "failed" {
		t.Errorf("Expected status 'failed', got %s", updatedJob.Status)
	}
}

// TestSmsService_StopJob_InvalidStatus 测试停止不可停止的任务
func TestSmsService_StopJob_InvalidStatus(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	job := &model.SmsJob{
		Name:   "已完成任务",
		Status: "completed",
	}
	database.Create(job)

	err := service.StopJob(context.Background(), job.ID)
	if err == nil {
		t.Error("Expected error for stopping completed job")
	}
	if err.Error() != "只能停止运行中、暂停或待执行的任务" {
		t.Errorf("Expected '只能停止运行中、暂停或待执行的任务', got %s", err.Error())
	}
}

// TestSmsService_DeleteJob 测试删除任务
func TestSmsService_DeleteJob(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	job := &model.SmsJob{
		Name:   "已完成任务",
		Status: "completed",
	}
	database.Create(job)

	detail := &model.SmsJobDetail{
		JobID:   job.ID,
		Phone:   "13812345678",
		Content: "测试内容",
		Status:  "sent",
	}
	database.Create(detail)

	err := service.DeleteJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("DeleteJob failed: %v", err)
	}

	var count int64
	database.Unscoped().Model(&model.SmsJob{}).Where("id = ?", job.ID).Unscoped().Count(&count)
	if count != 1 {
		database.Model(&model.SmsJob{}).Where("id = ?", job.ID).Count(&count)
		if count != 0 {
			t.Errorf("Expected job to be soft-deleted, got count %d", count)
		}
	}

	var deletedAt *time.Time
	database.Unscoped().Model(&model.SmsJob{}).Where("id = ?", job.ID).Select("deleted_at").Scan(&deletedAt)
	if deletedAt == nil {
		t.Error("Expected job to be soft-deleted (deleted_at should be set)")
	}
}

// TestSmsService_DeleteJob_InvalidStatus 测试删除不可删除的任务
func TestSmsService_DeleteJob_InvalidStatus(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	job := &model.SmsJob{
		Name:   "运行中任务",
		Status: "running",
	}
	database.Create(job)

	err := service.DeleteJob(context.Background(), job.ID)
	if err == nil {
		t.Error("Expected error for deleting running job")
	}
	if err.Error() != "只能删除已完成或失败的任务" {
		t.Errorf("Expected '只能删除已完成或失败的任务', got %s", err.Error())
	}
}

// TestSmsService_GetJobRecords 测试获取任务发送记录
func TestSmsService_GetJobRecords(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	job := &model.SmsJob{
		Name:   "测试任务",
		Status: "running",
	}
	database.Create(job)

	for i := 0; i < 5; i++ {
		detail := &model.SmsJobDetail{
			JobID:   job.ID,
			Phone:   "1381234567" + string(rune('0'+i)),
			Content: "测试内容",
			Status:  "sent",
		}
		database.Create(detail)
	}

	records, total, err := service.GetJobRecords(context.Background(), job.ID, 1, 10)
	if err != nil {
		t.Fatalf("GetJobRecords failed: %v", err)
	}

	if total != 5 {
		t.Errorf("Expected total 5, got %d", total)
	}
	if len(records) != 5 {
		t.Errorf("Expected 5 records, got %d", len(records))
	}
}

// TestSmsService_GetJobRecords_JobNotFound 测试获取不存在任务的记录
func TestSmsService_GetJobRecords_JobNotFound(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	_, _, err := service.GetJobRecords(context.Background(), 99999, 1, 10)
	if err == nil {
		t.Error("Expected error for non-existent job")
	}
}

// TestSmsService_GetSmsList 测试获取短信列表
func TestSmsService_GetSmsList(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	for i := 0; i < 5; i++ {
		record := &model.SmsRecord{
			Phone:    "1381234567" + string(rune('0'+i)),
			Content:  "测试内容" + string(rune('0'+i)),
			Provider: "aliyun",
			Status:   "sent",
		}
		database.Create(record)
	}

	req := &dto.SmsListRequest{
		Page:  1,
		Limit: 10,
	}

	records, total, err := service.GetSmsList(context.Background(), req)
	if err != nil {
		t.Fatalf("GetSmsList failed: %v", err)
	}

	if total != 5 {
		t.Errorf("Expected total 5, got %d", total)
	}
	if len(records) != 5 {
		t.Errorf("Expected 5 records, got %d", len(records))
	}
}

// TestSmsService_GetSmsByID 测试根据 ID 获取短信
func TestSmsService_GetSmsByID(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	record := &model.SmsRecord{
		Phone:    "13812345678",
		Content:  "测试内容",
		Provider: "aliyun",
		Status:   "sent",
	}
	database.Create(record)

	retrievedRecord, err := service.GetSmsByID(context.Background(), record.ID)
	if err != nil {
		t.Fatalf("GetSmsByID failed: %v", err)
	}

	if retrievedRecord.Phone != "13812345678" {
		t.Errorf("Expected phone '13812345678', got %s", retrievedRecord.Phone)
	}
}

// TestSmsService_GetSmsByID_NotFound 测试获取不存在的短信
func TestSmsService_GetSmsByID_NotFound(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	_, err := service.GetSmsByID(context.Background(), 99999)
	if err == nil {
		t.Error("Expected error for non-existent SMS")
	}
}

func TestIsSMSNightRestricted(t *testing.T) {
	cst := time.FixedZone("CST", 8*3600)
	cases := []struct {
		hour int
		want bool
	}{
		{22, true}, {23, true}, {0, true}, {5, true}, {7, true},
		{8, false}, {12, false}, {18, false}, {21, false},
	}
	for _, c := range cases {
		got := isSMSNightRestricted(time.Date(2026, 1, 1, c.hour, 30, 0, 0, cst))
		if got != c.want {
			t.Errorf("hour=%d: got %v, want %v", c.hour, got, c.want)
		}
	}
}

func TestSendSms_NightGuard(t *testing.T) {
	database := setupSmsServiceTestDB(t)
	repo := newTestSmsRepository(database)
	service := NewSmsService(repo)

	smsNightRestrictedFn = func(time.Time) bool { return true }
	t.Cleanup(func() { smsNightRestrictedFn = isSMSNightRestricted })

	err := service.SendSms(context.Background(), &dto.SmsSendRequest{
		Phone:   "13800138000",
		Content: "night test",
	})
	if err == nil || !strings.Contains(err.Error(), "夜间禁发") {
		t.Fatalf("夜间时段应拒绝发送, got err=%v", err)
	}

	var count int64
	database.Model(&model.SmsRecord{}).Count(&count)
	if count != 0 {
		t.Errorf("夜间拦截后不应产生发送记录, got %d", count)
	}
}
