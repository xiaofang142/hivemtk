package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"
	smssvc "hivemtk-user/internal/service/sms"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupTestSmsDB(t *testing.T) *gorm.DB {
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
	return database
}

func setupSmsController() *SmsController {
	repo := repository.NewSmsRepository()
	svc := smssvc.NewSmsService(repo)
	return NewSmsController(svc)
}

// TestSmsController_GetConfig_Success 测试获取配置成功
func TestSmsController_GetConfig_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/config", ctrl.GetConfig)

	config := model.SmsConfig{
		DefaultProvider: "aliyun",
		RateLimit:       100,
		DailyLimit:      10000,
		RetryTimes:      3,
	}
	db.GetDB().Create(&config)

	db.GetDB().Create(&model.SmsAliyunConfig{
		AccessKeyID:     "test_ak",
		AccessKeySecret: "test_sk",
		SignName:        "测试签名",
	})
	db.GetDB().Create(&model.SmsTencentConfig{
		SecretID:  "test_sid",
		SecretKey: "test_skey",
		AppID:     "12345",
		SignName:  "腾讯签名",
	})
	db.GetDB().Create(&model.SmsHuaweiConfig{
		AppKey:    "test_ak",
		AppSecret: "test_as",
		Sender:    "10690",
		Signature: "华为签名",
	})

	req, _ := http.NewRequest("GET", "/sms/config", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code SUCCESS, got %v", response["code"])
	}
}

// TestSmsController_SaveConfig_Success 测试保存配置成功
func TestSmsController_SaveConfig_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/config", ctrl.SaveConfig)

	saveReq := dto.SmsConfigRequest{
		DefaultProvider: "aliyun",
		RateLimit:       50,
		DailyLimit:      5000,
		RetryTimes:      2,
		Aliyun: dto.SmsAliyunConfig{
			AccessKeyId:     "new_ak",
			AccessKeySecret: "new_sk",
			SignName:        "新签名",
		},
		Tencent: dto.SmsTencentConfig{
			SecretId:  "new_sid",
			SecretKey: "new_skey",
			AppId:     "54321",
			SignName:  "新腾讯签名",
		},
		Huawei: dto.SmsHuaweiConfig{
			AppKey:    "new_ak",
			AppSecret: "new_as",
			Sender:    "10691",
			Signature: "新华为签名",
		},
	}
	body, _ := json.Marshal(saveReq)

	req, _ := http.NewRequest("POST", "/sms/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSmsController_SaveConfig_InvalidJSON 测试无效 JSON 请求
func TestSmsController_SaveConfig_InvalidJSON(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/config", ctrl.SaveConfig)

	req, _ := http.NewRequest("POST", "/sms/config", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_SaveConfig_InvalidProvider 测试无效的提供商
func TestSmsController_SaveConfig_InvalidProvider(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/config", ctrl.SaveConfig)

	saveReq := dto.SmsConfigRequest{
		DefaultProvider: "invalid_provider",
	}
	body, _ := json.Marshal(saveReq)

	req, _ := http.NewRequest("POST", "/sms/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_SaveConfig_RateLimitOutOfRange 测试频率限制超出范围
func TestSmsController_SaveConfig_RateLimitOutOfRange(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/config", ctrl.SaveConfig)

	saveReq := dto.SmsConfigRequest{
		DefaultProvider: "aliyun",
		RateLimit:       2000,
	}
	body, _ := json.Marshal(saveReq)

	req, _ := http.NewRequest("POST", "/sms/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_GetSmsList_Success 测试获取短信列表成功
func TestSmsController_GetSmsList_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/list", ctrl.GetSmsList)

	now := time.Now()
	db.GetDB().Create(&model.SmsRecord{
		Phone:    "13800138000",
		Content:  "测试短信内容",
		Provider: "aliyun",
		Status:   "sent",
		SendTime: &now,
	})
	db.GetDB().Create(&model.SmsRecord{
		Phone:    "13900139000",
		Content:  "另一条短信",
		Provider: "tencent",
		Status:   "sent",
	})

	req, _ := http.NewRequest("GET", "/sms/list?page=1&limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code SUCCESS, got %v", response["code"])
	}
}

// TestSmsController_GetSmsList_WithFilters 测试带过滤条件的短信列表
func TestSmsController_GetSmsList_WithFilters(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/list", ctrl.GetSmsList)

	now := time.Now()
	db.GetDB().Create(&model.SmsRecord{
		Phone:    "13800138000",
		Content:  "测试短信",
		Provider: "aliyun",
		Status:   "sent",
		SendTime: &now,
	})
	db.GetDB().Create(&model.SmsRecord{
		Phone:    "13900139000",
		Content:  "失败短信",
		Provider: "tencent",
		Status:   "failed",
	})

	req, _ := http.NewRequest("GET", "/sms/list?status=sent&phone=13800138000", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSmsController_GetSmsList_InvalidStatus 测试无效的状态参数
// 注意：由于 DTO 使用 omitempty，无效状态会被忽略而不是返回错误
func TestSmsController_GetSmsList_InvalidStatus(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/list", ctrl.GetSmsList)

	req, _ := http.NewRequest("GET", "/sms/list?status=invalid_status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK (invalid status ignored due to omitempty), got %d", w.Code)
	}
}

// TestSmsController_GetSmsDetail_Success 测试获取短信详情成功
func TestSmsController_GetSmsDetail_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/detail/:id", ctrl.GetSmsDetail)

	now := time.Now()
	record := model.SmsRecord{
		Phone:    "13800138000",
		Content:  "测试短信详情",
		Provider: "aliyun",
		Status:   "sent",
		SendTime: &now,
	}
	db.GetDB().Create(&record)

	req, _ := http.NewRequest("GET", "/sms/detail/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code SUCCESS, got %v", response["code"])
	}
}

// TestSmsController_GetSmsDetail_InvalidID 测试无效的 ID
func TestSmsController_GetSmsDetail_InvalidID(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/detail/:id", ctrl.GetSmsDetail)

	req, _ := http.NewRequest("GET", "/sms/detail/invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_GetSmsDetail_NotFound 测试短信不存在
func TestSmsController_GetSmsDetail_NotFound(t *testing.T) {
	t.Setenv("SMS_ALLOW_NIGHT_SEND", "true")
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/detail/:id", ctrl.GetSmsDetail)

	req, _ := http.NewRequest("GET", "/sms/detail/999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status Not Found, got %d", w.Code)
	}
}

// TestSmsController_SendSms_Success 测试发送短信成功
// 注: 测试中无真实阿里云凭据, 真实 API 会失败. 这里验证: (1) 流程到达 service 层
// (2) 数据库中创建了 sms_records 记录 (3) 状态标记为 failed
func TestSmsController_SendSms_Success(t *testing.T) {
	t.Setenv("SMS_ALLOW_NIGHT_SEND", "true")
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/send", ctrl.SendSms)

	db.GetDB().Create(&model.SmsConfig{
		DefaultProvider: "aliyun",
		RateLimit:       100,
		DailyLimit:      10000,
		RetryTimes:      3,
	})
	db.GetDB().Create(&model.SmsAliyunConfig{
		AccessKeyID:     "test-key-id",
		AccessKeySecret: "test-key-secret",
		SignName:        "test-sign",
	})

	sendReq := dto.SmsSendRequest{
		Phone:   "13800138000",
		Content: "测试发送短信",
	}
	body, _ := json.Marshal(sendReq)

	req, _ := http.NewRequest("POST", "/sms/send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var record model.SmsRecord
	if err := db.GetDB().Where("phone = ?", "13800138000").First(&record).Error; err != nil {
		t.Errorf("Expected sms record to be created, got error: %v", err)
	}
	if record.Provider != "aliyun" {
		t.Errorf("Expected provider=aliyun, got %s", record.Provider)
	}
	if record.Status != "sending" && record.Status != "failed" && record.Status != "sent" {
		t.Errorf("Expected status in [sending, failed, sent], got %s", record.Status)
	}
}

// TestSmsController_SendSms_InvalidJSON 测试无效 JSON
func TestSmsController_SendSms_InvalidJSON(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/send", ctrl.SendSms)

	req, _ := http.NewRequest("POST", "/sms/send", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_SendSms_EmptyPhone 测试空手机号
func TestSmsController_SendSms_EmptyPhone(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/send", ctrl.SendSms)

	sendReq := dto.SmsSendRequest{
		Phone:   "",
		Content: "测试短信",
	}
	body, _ := json.Marshal(sendReq)

	req, _ := http.NewRequest("POST", "/sms/send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_SendSms_WrongPhoneLength 测试手机号长度错误
func TestSmsController_SendSms_WrongPhoneLength(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/send", ctrl.SendSms)

	sendReq := dto.SmsSendRequest{
		Phone:   "1380013800",
		Content: "测试短信",
	}
	body, _ := json.Marshal(sendReq)

	req, _ := http.NewRequest("POST", "/sms/send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_SendSms_EmptyContent 测试空内容
func TestSmsController_SendSms_EmptyContent(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/send", ctrl.SendSms)

	sendReq := dto.SmsSendRequest{
		Phone:   "13800138000",
		Content: "",
	}
	body, _ := json.Marshal(sendReq)

	req, _ := http.NewRequest("POST", "/sms/send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_ResendSms_Success 测试重发短信成功
// 注: 测试中无真实阿里云凭据, 真实 API 会失败. 这里验证:
// (1) 流程到达 service 层 (2) 记录状态被更新为 sending/failed
func TestSmsController_ResendSms_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/resend/:id", ctrl.ResendSms)

	db.GetDB().Create(&model.SmsConfig{
		DefaultProvider: "aliyun",
		RateLimit:       100,
		DailyLimit:      10000,
		RetryTimes:      3,
	})
	db.GetDB().Create(&model.SmsAliyunConfig{
		AccessKeyID:     "test-key-id",
		AccessKeySecret: "test-key-secret",
		SignName:        "test-sign",
	})

	record := model.SmsRecord{
		Phone:    "13800138000",
		Content:  "重发测试",
		Provider: "aliyun",
		Status:   "failed",
	}
	db.GetDB().Create(&record)

	req, _ := http.NewRequest("POST", "/sms/resend/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var updated model.SmsRecord
	if err := db.GetDB().Where("phone = ?", "13800138000").First(&updated).Error; err != nil {
		t.Errorf("Expected sms record to exist, got error: %v", err)
	}
	if updated.Status != "sending" && updated.Status != "failed" && updated.Status != "sent" {
		t.Errorf("Expected status in [sending, failed, sent], got %s", updated.Status)
	}
}

// TestSmsController_ResendSms_InvalidID 测试无效 ID
func TestSmsController_ResendSms_InvalidID(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/resend/:id", ctrl.ResendSms)

	req, _ := http.NewRequest("POST", "/sms/resend/invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_ResendSms_NotFailedStatus 测试重发非失败状态的短信
func TestSmsController_ResendSms_NotFailedStatus(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/resend/:id", ctrl.ResendSms)

	now := time.Now()
	record := model.SmsRecord{
		Phone:    "13800138000",
		Content:  "已发送短信",
		Provider: "aliyun",
		Status:   "sent",
		SendTime: &now,
	}
	db.GetDB().Create(&record)

	req, _ := http.NewRequest("POST", "/sms/resend/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSmsController_GetDraftList_Success 测试获取草稿列表成功
func TestSmsController_GetDraftList_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/draft/list", ctrl.GetDraftList)

	db.GetDB().Create(&model.SmsDraft{
		Title:   "草稿 1",
		Content: "草稿内容 1",
	})
	db.GetDB().Create(&model.SmsDraft{
		Title:   "草稿 2",
		Content: "草稿内容 2",
	})

	req, _ := http.NewRequest("GET", "/sms/draft/list?page=1&limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code SUCCESS, got %v", response["code"])
	}
}

// TestSmsController_GetDraftList_WithTitleFilter 测试带标题过滤的草稿列表
func TestSmsController_GetDraftList_WithTitleFilter(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/draft/list", ctrl.GetDraftList)

	db.GetDB().Create(&model.SmsDraft{
		Title:   "测试草稿",
		Content: "测试内容",
	})
	db.GetDB().Create(&model.SmsDraft{
		Title:   "其他草稿",
		Content: "其他内容",
	})

	req, _ := http.NewRequest("GET", "/sms/draft/list?title=测试", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSmsController_GetDraft_Success 测试获取草稿详情成功
func TestSmsController_GetDraft_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/draft/:id", ctrl.GetDraft)

	draft := model.SmsDraft{
		Title:   "测试草稿",
		Content: "测试草稿内容",
	}
	db.GetDB().Create(&draft)

	req, _ := http.NewRequest("GET", "/sms/draft/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code SUCCESS, got %v", response["code"])
	}
}

// TestSmsController_GetDraft_InvalidID 测试无效 ID
func TestSmsController_GetDraft_InvalidID(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/draft/:id", ctrl.GetDraft)

	req, _ := http.NewRequest("GET", "/sms/draft/invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_CreateDraft_Success 测试创建草稿成功
func TestSmsController_CreateDraft_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/draft", ctrl.CreateDraft)

	createReq := dto.SmsDraftCreateRequest{
		Title:   "新草稿",
		Content: "新草稿内容",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/sms/draft", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSmsController_CreateDraft_EmptyTitle 测试空标题
func TestSmsController_CreateDraft_EmptyTitle(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/draft", ctrl.CreateDraft)

	createReq := dto.SmsDraftCreateRequest{
		Title:   "",
		Content: "草稿内容",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/sms/draft", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_CreateDraft_EmptyContent 测试空内容
func TestSmsController_CreateDraft_EmptyContent(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/draft", ctrl.CreateDraft)

	createReq := dto.SmsDraftCreateRequest{
		Title:   "草稿标题",
		Content: "",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/sms/draft", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_UpdateDraft_Success 测试更新草稿成功
func TestSmsController_UpdateDraft_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.PUT("/sms/draft/:id", ctrl.UpdateDraft)

	draft := model.SmsDraft{
		Title:   "旧标题",
		Content: "旧内容",
	}
	db.GetDB().Create(&draft)

	updateReq := dto.SmsDraftUpdateRequest{
		Title:   "新标题",
		Content: "新内容",
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", "/sms/draft/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSmsController_UpdateDraft_InvalidID 测试无效 ID
func TestSmsController_UpdateDraft_InvalidID(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.PUT("/sms/draft/:id", ctrl.UpdateDraft)

	updateReq := dto.SmsDraftUpdateRequest{
		Title:   "新标题",
		Content: "新内容",
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", "/sms/draft/invalid", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_DeleteDraft_Success 测试删除草稿成功
func TestSmsController_DeleteDraft_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.DELETE("/sms/draft/:id", ctrl.DeleteDraft)

	draft := model.SmsDraft{
		Title:   "待删除草稿",
		Content: "草稿内容",
	}
	db.GetDB().Create(&draft)

	req, _ := http.NewRequest("DELETE", "/sms/draft/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSmsController_DeleteDraft_InvalidID 测试无效 ID
func TestSmsController_DeleteDraft_InvalidID(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.DELETE("/sms/draft/:id", ctrl.DeleteDraft)

	req, _ := http.NewRequest("DELETE", "/sms/draft/invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_SendDraft_Success 测试发送草稿成功 (表单形式)
// 注: 真实阿里云 API 会因测试凭据失败. 验证: 数据库已创建 sms_records
func TestSmsController_SendDraft_Form_Success(t *testing.T) {
	t.Setenv("SMS_ALLOW_NIGHT_SEND", "true")
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/draft/send/:id", ctrl.SendDraft)

	draft := model.SmsDraft{
		Title:   "发送测试",
		Content: "草稿发送内容",
	}
	db.GetDB().Create(&draft)

	db.GetDB().Create(&model.SmsConfig{
		DefaultProvider: "aliyun",
		RateLimit:       100,
		DailyLimit:      10000,
		RetryTimes:      3,
	})
	db.GetDB().Create(&model.SmsAliyunConfig{
		AccessKeyID:     "test-key-id",
		AccessKeySecret: "test-key-secret",
		SignName:        "test-sign",
	})

	req, _ := http.NewRequest("POST", "/sms/draft/send/1", bytes.NewReader([]byte("phone=13800138000")))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var record model.SmsRecord
	if err := db.GetDB().Where("phone = ?", "13800138000").First(&record).Error; err != nil {
		t.Errorf("Expected sms record to be created, got error: %v, body: %s", err, w.Body.String())
	}
	if record.Content != "草稿发送内容" {
		t.Errorf("Expected content=草稿发送内容, got %s", record.Content)
	}
}

// TestSmsController_SendDraft_JSON_Success 测试发送草稿成功 (JSON 形式)
func TestSmsController_SendDraft_JSON_Success(t *testing.T) {
	t.Setenv("SMS_ALLOW_NIGHT_SEND", "true")
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/draft/:id/send", ctrl.SendDraft)

	draft := model.SmsDraft{
		Title:   "发送测试",
		Content: "草稿发送内容",
	}
	db.GetDB().Create(&draft)

	db.GetDB().Create(&model.SmsConfig{
		DefaultProvider: "aliyun",
		RateLimit:       100,
		DailyLimit:      10000,
		RetryTimes:      3,
	})
	db.GetDB().Create(&model.SmsAliyunConfig{
		AccessKeyID:     "test-key-id",
		AccessKeySecret: "test-key-secret",
		SignName:        "test-sign",
	})

	sendReq := map[string]string{"phone": "13800138000"}
	body, _ := json.Marshal(sendReq)

	req, _ := http.NewRequest("POST", "/sms/draft/1/send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var record model.SmsRecord
	if err := db.GetDB().Where("phone = ?", "13800138000").First(&record).Error; err != nil {
		t.Errorf("Expected sms record to be created, got error: %v, body: %s", err, w.Body.String())
	}
	if record.Content != "草稿发送内容" {
		t.Errorf("Expected content=草稿发送内容, got %s", record.Content)
	}
}

// TestSmsController_SendDraft_EmptyPhone 测试空手机号
func TestSmsController_SendDraft_EmptyPhone(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/draft/send/:id", ctrl.SendDraft)

	draft := model.SmsDraft{
		Title:   "发送测试",
		Content: "草稿发送内容",
	}
	db.GetDB().Create(&draft)

	req, _ := http.NewRequest("POST", "/sms/draft/send/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_GetJobList_Success 测试获取任务列表成功
func TestSmsController_GetJobList_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/job/list", ctrl.GetJobList)

	db.GetDB().Create(&model.SmsJob{
		Name:   "任务 1",
		Total:  100,
		Sent:   50,
		Failed: 5,
		Status: "running",
	})
	db.GetDB().Create(&model.SmsJob{
		Name:   "任务 2",
		Total:  200,
		Sent:   200,
		Failed: 0,
		Status: "completed",
	})

	req, _ := http.NewRequest("GET", "/sms/job/list?page=1&limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code SUCCESS, got %v", response["code"])
	}
}

// TestSmsController_GetJobList_WithFilters 测试带过滤条件的任务列表
func TestSmsController_GetJobList_WithFilters(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/job/list", ctrl.GetJobList)

	db.GetDB().Create(&model.SmsJob{
		Name:   "运行中任务",
		Total:  100,
		Sent:   50,
		Failed: 5,
		Status: "running",
	})
	db.GetDB().Create(&model.SmsJob{
		Name:   "已完成任务",
		Total:  200,
		Sent:   200,
		Failed: 0,
		Status: "completed",
	})

	req, _ := http.NewRequest("GET", "/sms/job/list?status=running", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSmsController_GetJobList_InvalidStatus 测试无效状态参数
// 注意：由于 DTO 使用 omitempty，无效状态会被忽略而不是返回错误
func TestSmsController_GetJobList_InvalidStatus(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/job/list", ctrl.GetJobList)

	req, _ := http.NewRequest("GET", "/sms/job/list?status=invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK (invalid status ignored due to omitempty), got %d", w.Code)
	}
}

// TestSmsController_GetJob_Success 测试获取任务详情成功
func TestSmsController_GetJob_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/job/:id", ctrl.GetJob)

	job := model.SmsJob{
		Name:   "测试任务",
		Total:  100,
		Sent:   50,
		Failed: 5,
		Status: "running",
	}
	db.GetDB().Create(&job)

	req, _ := http.NewRequest("GET", "/sms/job/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code SUCCESS, got %v", response["code"])
	}
}

// TestSmsController_GetJob_InvalidID 测试无效 ID
func TestSmsController_GetJob_InvalidID(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/job/:id", ctrl.GetJob)

	req, _ := http.NewRequest("GET", "/sms/job/invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_CreateJob_Success 测试创建任务成功
func TestSmsController_CreateJob_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/job", ctrl.CreateJob)

	createReq := dto.SmsJobCreateRequest{
		Name:      "新任务",
		PhoneList: []string{"13800138000", "13900139000"},
		Content:   "任务短信内容",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/sms/job", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSmsController_CreateJob_EmptyName 测试空任务名
func TestSmsController_CreateJob_EmptyName(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/job", ctrl.CreateJob)

	createReq := dto.SmsJobCreateRequest{
		Name:      "",
		PhoneList: []string{"13800138000"},
		Content:   "任务内容",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/sms/job", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_CreateJob_EmptyPhoneList 测试空手机号列表
func TestSmsController_CreateJob_EmptyPhoneList(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/job", ctrl.CreateJob)

	createReq := dto.SmsJobCreateRequest{
		Name:      "任务名",
		PhoneList: []string{},
		Content:   "任务内容",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/sms/job", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_CreateJob_EmptyContent 测试空内容
func TestSmsController_CreateJob_EmptyContent(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/job", ctrl.CreateJob)

	createReq := dto.SmsJobCreateRequest{
		Name:      "任务名",
		PhoneList: []string{"13800138000"},
		Content:   "",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/sms/job", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_PauseJob_Success 测试暂停任务成功
func TestSmsController_PauseJob_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/job/pause/:id", ctrl.PauseJob)

	job := model.SmsJob{
		Name:   "运行中任务",
		Status: "running",
	}
	db.GetDB().Create(&job)

	req, _ := http.NewRequest("POST", "/sms/job/pause/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSmsController_PauseJob_NotRunning 测试暂停非运行中任务
func TestSmsController_PauseJob_NotRunning(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/job/pause/:id", ctrl.PauseJob)

	job := model.SmsJob{
		Name:   "已完成任务",
		Status: "completed",
	}
	db.GetDB().Create(&job)

	req, _ := http.NewRequest("POST", "/sms/job/pause/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 状态不对是**业务校验**失败，不是数据库出错 ⇒ 400（由 HandleServiceError 给出）。
	// 曾经走 HandleDBError 落成 500，把"只能暂停运行中的任务"这个真正的原因吞成
	// 一句"暂停任务失败"，客户端还会把 500 当服务端故障去重试。
	if w.Code != http.StatusBadRequest {
		t.Errorf("状态不对应回 400，实际 %d，body: %s", w.Code, w.Body.String())
	}
}

// TestSmsController_ResumeJob_Success 测试继续任务成功
func TestSmsController_ResumeJob_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/job/resume/:id", ctrl.ResumeJob)

	job := model.SmsJob{
		Name:   "暂停任务",
		Status: "paused",
	}
	db.GetDB().Create(&job)

	req, _ := http.NewRequest("POST", "/sms/job/resume/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSmsController_ResumeJob_NotPaused 测试继续非暂停任务
func TestSmsController_ResumeJob_NotPaused(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/job/resume/:id", ctrl.ResumeJob)

	job := model.SmsJob{
		Name:   "运行中任务",
		Status: "running",
	}
	db.GetDB().Create(&job)

	req, _ := http.NewRequest("POST", "/sms/job/resume/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 状态不对是**业务校验**失败 ⇒ 400（HandleServiceError），不是 500。
	if w.Code != http.StatusBadRequest {
		t.Errorf("状态不对应回 400，实际 %d，body: %s", w.Code, w.Body.String())
	}
}

// TestSmsController_StopJob_Success 测试停止任务成功
func TestSmsController_StopJob_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/job/stop/:id", ctrl.StopJob)

	job := model.SmsJob{
		Name:   "运行中任务",
		Status: "running",
	}
	db.GetDB().Create(&job)

	req, _ := http.NewRequest("POST", "/sms/job/stop/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSmsController_StopJob_AlreadyCompleted 测试停止已完成任务
func TestSmsController_StopJob_AlreadyCompleted(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.POST("/sms/job/stop/:id", ctrl.StopJob)

	job := model.SmsJob{
		Name:   "已完成任务",
		Status: "completed",
	}
	db.GetDB().Create(&job)

	req, _ := http.NewRequest("POST", "/sms/job/stop/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 状态不对是**业务校验**失败 ⇒ 400（HandleServiceError），不是 500。
	if w.Code != http.StatusBadRequest {
		t.Errorf("状态不对应回 400，实际 %d，body: %s", w.Code, w.Body.String())
	}
}

// TestSmsController_DeleteJob_Success 测试删除任务成功
func TestSmsController_DeleteJob_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.DELETE("/sms/job/:id", ctrl.DeleteJob)

	job := model.SmsJob{
		Name:   "已完成任务",
		Status: "completed",
	}
	db.GetDB().Create(&job)

	req, _ := http.NewRequest("DELETE", "/sms/job/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSmsController_DeleteJob_NotCompleted 测试删除非完成/失败任务
func TestSmsController_DeleteJob_NotCompleted(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.DELETE("/sms/job/:id", ctrl.DeleteJob)

	job := model.SmsJob{
		Name:   "运行中任务",
		Status: "running",
	}
	db.GetDB().Create(&job)

	req, _ := http.NewRequest("DELETE", "/sms/job/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 状态守卫是业务校验（「只能删除已完成或失败的任务」），应为 400 而非 500。
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSmsController_GetJobRecords_Success 测试获取任务记录成功
func TestSmsController_GetJobRecords_Success(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/job/:id/records", ctrl.GetJobRecords)

	job := model.SmsJob{
		Name:   "测试任务",
		Status: "running",
	}
	db.GetDB().Create(&job)

	detail := model.SmsJobDetail{
		JobID:   job.ID,
		Phone:   "13800138000",
		Content: "任务详情内容",
		Status:  "sent",
	}
	db.GetDB().Create(&detail)

	req, _ := http.NewRequest("GET", "/sms/job/1/records?page=1&limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code SUCCESS, got %v", response["code"])
	}
}

// TestSmsController_GetJobRecords_InvalidID 测试无效 ID
func TestSmsController_GetJobRecords_InvalidID(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/job/:id/records", ctrl.GetJobRecords)

	req, _ := http.NewRequest("GET", "/sms/job/invalid/records", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSmsController_GetJobRecords_NotFound 测试任务不存在
func TestSmsController_GetJobRecords_NotFound(t *testing.T) {
	setupTestSmsDB(t)
	ctrl := setupSmsController()
	router := setupGinEngine()
	router.GET("/sms/job/:id/records", ctrl.GetJobRecords)

	req, _ := http.NewRequest("GET", "/sms/job/999/records?page=1&limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status Not Found, got %d", w.Code)
	}
}

func setupSMSTestDB_Merged(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.SmsRecord{},
	)
	db.SetTestDB(database)
	return database
}

// TestSMSController_SendSMS_MergedInvalidJSON 测试 SMS 发送无效 JSON(合并自 sms_extra_test.go)
func TestSMSController_SendSMS_MergedInvalidJSON(t *testing.T) {
	setupSMSTestDB_Merged(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	smsRepo := repository.NewSmsRepository()
	ctrl := NewSmsController(smssvc.NewSmsService(smsRepo))
	ctrl.RegisterRoutes(router.Group("/sms"))

	req, _ := http.NewRequest("POST", "/sms/sms/send", bytes.NewReader([]byte("bad")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

// TestSMSController_GetList_MergedSuccess 测试 SMS 列表(合并自 sms_extra_test.go)
func TestSMSController_GetList_MergedSuccess(t *testing.T) {
	setupSMSTestDB_Merged(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	smsRepo := repository.NewSmsRepository()
	ctrl := NewSmsController(smssvc.NewSmsService(smsRepo))
	ctrl.RegisterRoutes(router.Group("/sms"))

	req, _ := http.NewRequest("GET", "/sms/sms/list?page=1&page_size=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}
