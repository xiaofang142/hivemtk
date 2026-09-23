package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"context"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/httpclient"
	"hivemtk-user/internal/repository"
)

// SmsService 短信服务接口
type SmsService interface {
	GetConfig(ctx context.Context) (*dto.SmsConfigResponse, error)
	SaveConfig(ctx context.Context, req *dto.SmsConfigRequest) error
	IsProviderConfigured(ctx context.Context, provider string) (bool, error)

	GetSmsList(ctx context.Context, req *dto.SmsListRequest) ([]*model.SmsRecord, int64, error)
	GetSmsByID(ctx context.Context, id uint) (*model.SmsRecord, error)
	SendSms(ctx context.Context, req *dto.SmsSendRequest) error
	ResendSms(ctx context.Context, id uint) error

	GetDraftList(ctx context.Context, req *dto.SmsDraftListRequest) ([]*model.SmsDraft, int64, error)
	GetDraftByID(ctx context.Context, id uint) (*model.SmsDraft, error)
	CreateDraft(ctx context.Context, req *dto.SmsDraftCreateRequest) error
	UpdateDraft(ctx context.Context, id uint, req *dto.SmsDraftUpdateRequest) error
	DeleteDraft(ctx context.Context, id uint) error
	SendDraft(ctx context.Context, id uint, phone string) error

	GetJobList(ctx context.Context, req *dto.SmsJobListRequest) ([]*model.SmsJob, int64, error)
	GetJobByID(ctx context.Context, id uint) (*model.SmsJob, error)
	CreateJob(ctx context.Context, req *dto.SmsJobCreateRequest) error
	PauseJob(ctx context.Context, id uint) error
	ResumeJob(ctx context.Context, id uint) error
	StopJob(ctx context.Context, id uint) error
	DeleteJob(ctx context.Context, id uint) error
	GetJobRecords(ctx context.Context, id uint, page, limit int) ([]*model.SmsJobDetail, int64, error)
}

type smsService struct {
	repo repository.SmsRepository

	// unsubSvc 退订查询的注入位；nil 时沿用包级单例（见 unsub()）。
	unsubSvc *SmsUnsubscribeService

	// 三家网关的接口域。生产由 NewSmsService 填官方常量；测试指向本地 httptest／关掉的端口。
	//
	// 为什么是字段而不是包级注入点：本仓已有的三处 URL 覆盖量（dingtalkOpenAPIBase、
	// tgAPIBaseOverride、dyAPIBaseOverride）都是包级全局，于是各要一把 RWMutex、两扇
	// accessor、一行 seam-guard 注册表和一条 -race 腿（第四十三轮 R28 的账）才敢被异步链读到。
	// 短信这三条腿只在请求协程里同步用，跟着实例走既省掉那套机制，也不会让一个用例的桩
	// 活到下一个用例里去。
	aliyunAPIURL  string
	tencentAPIURL string
	huaweiAPIURL  string
}

// 三家短信网关的官方接口域（生产默认值）。
const (
	smsAliyunAPIURL  = "https://dysmsapi.aliyuncs.com/"
	smsTencentAPIURL = "https://sms.tencentcloudapi.com/"
	// 华为的域里带区域段，与 sendHuawei 旧写法一致取 cn-north-4。
	smsHuaweiAPIURL = "https://smsapi.cn-north-4.boe-business.huaweicloud.com:443/sms/batchSendSms/v1"
)

// NewSmsService 创建短信服务
func NewSmsService(repo repository.SmsRepository) SmsService {
	return &smsService{
		repo:          repo,
		aliyunAPIURL:  smsAliyunAPIURL,
		tencentAPIURL: smsTencentAPIURL,
		huaweiAPIURL:  smsHuaweiAPIURL,
	}
}

var smsPhoneRe = regexp.MustCompile(`^\+?[1-9]\d{6,14}$`)

func validateSMSPhone(phone string) error {
	if !smsPhoneRe.MatchString(strings.TrimSpace(phone)) {
		return fmt.Errorf("手机号格式无效: %q", phone)
	}
	return nil
}

var (
	smsUnsubOnce sync.Once
	smsUnsubSvc  *SmsUnsubscribeService
)

func (s *smsService) unsub() *SmsUnsubscribeService {
	if s.unsubSvc != nil {
		return s.unsubSvc
	}
	smsUnsubOnce.Do(func() {
		smsUnsubSvc = NewSmsUnsubscribeService(nil)
	})
	return smsUnsubSvc
}

// SetSmsUnsubscribe 注入退订查询（测试与自定义装配）。
//
// 合规闸门读的是哪一份名单必须可指定：包级单例在**第一次调用时**才解析全局 DB 句柄，
// 于是它读到的是"谁先跑"而不是"配了哪套库"，这条判据在测试里就无法稳定成立。
func (s *smsService) SetSmsUnsubscribe(u *SmsUnsubscribeService) {
	s.unsubSvc = u
}

func (s *smsService) GetConfig(ctx context.Context) (*dto.SmsConfigResponse, error) {
	config, err := s.repo.GetConfig(ctx)
	if err != nil {
		return nil, err
	}

	aliyunConfig, err := s.repo.GetAliyunConfig(ctx)
	if err != nil {
		return nil, err
	}

	tencentConfig, err := s.repo.GetTencentConfig(ctx)
	if err != nil {
		return nil, err
	}

	huaweiConfig, err := s.repo.GetHuaweiConfig(ctx)
	if err != nil {
		return nil, err
	}

	return &dto.SmsConfigResponse{
		DefaultProvider: config.DefaultProvider,
		RateLimit:       config.RateLimit,
		DailyLimit:      config.DailyLimit,
		RetryTimes:      config.RetryTimes,
		Aliyun: dto.SmsAliyunConfig{
			AccessKeyId:     aliyunConfig.AccessKeyID,
			AccessKeySecret: aliyunConfig.AccessKeySecret,
			SignName:        aliyunConfig.SignName,
		},
		Tencent: dto.SmsTencentConfig{
			SecretId:  tencentConfig.SecretID,
			SecretKey: tencentConfig.SecretKey,
			AppId:     tencentConfig.AppID,
			SignName:  tencentConfig.SignName,
		},
		Huawei: dto.SmsHuaweiConfig{
			AppKey:    huaweiConfig.AppKey,
			AppSecret: huaweiConfig.AppSecret,
			Sender:    huaweiConfig.Sender,
			Signature: huaweiConfig.Signature,
		},
	}, nil
}

func (s *smsService) SaveConfig(ctx context.Context, req *dto.SmsConfigRequest) error {
	config := &model.SmsConfig{
		DefaultProvider: req.DefaultProvider,
		RateLimit:       req.RateLimit,
		DailyLimit:      req.DailyLimit,
		RetryTimes:      req.RetryTimes,
	}
	if err := s.repo.SaveConfig(ctx, config); err != nil {
		return err
	}

	aliyunConfig := &model.SmsAliyunConfig{
		AccessKeyID:     req.Aliyun.AccessKeyId,
		AccessKeySecret: req.Aliyun.AccessKeySecret,
		SignName:        req.Aliyun.SignName,
	}
	if err := s.repo.SaveAliyunConfig(ctx, aliyunConfig); err != nil {
		return err
	}

	tencentConfig := &model.SmsTencentConfig{
		SecretID:  req.Tencent.SecretId,
		SecretKey: req.Tencent.SecretKey,
		AppID:     req.Tencent.AppId,
		SignName:  req.Tencent.SignName,
	}
	if err := s.repo.SaveTencentConfig(ctx, tencentConfig); err != nil {
		return err
	}

	huaweiConfig := &model.SmsHuaweiConfig{
		AppKey:    req.Huawei.AppKey,
		AppSecret: req.Huawei.AppSecret,
		Sender:    req.Huawei.Sender,
		Signature: req.Huawei.Signature,
	}
	return s.repo.SaveHuaweiConfig(ctx, huaweiConfig)
}

func (s *smsService) IsProviderConfigured(ctx context.Context, provider string) (bool, error) {
	switch provider {
	case "aliyun":
		cfg, err := s.repo.GetAliyunConfig(ctx)
		if err != nil {
			return false, err
		}
		return cfg.AccessKeyID != "" && cfg.AccessKeySecret != "", nil
	case "tencent":
		cfg, err := s.repo.GetTencentConfig(ctx)
		if err != nil {
			return false, err
		}
		return cfg.SecretID != "" && cfg.SecretKey != "", nil
	case "huawei":
		cfg, err := s.repo.GetHuaweiConfig(ctx)
		if err != nil {
			return false, err
		}
		return cfg.AppKey != "" && cfg.AppSecret != "", nil
	default:
		return false, fmt.Errorf("unknown sms provider: %s", provider)
	}
}

func (s *smsService) GetSmsList(ctx context.Context, req *dto.SmsListRequest) ([]*model.SmsRecord, int64, error) {
	return s.repo.GetSmsList(ctx, req.Page, req.Limit, req.Phone, req.Status, req.StartDate, req.EndDate)
}

func (s *smsService) GetSmsByID(ctx context.Context, id uint) (*model.SmsRecord, error) {
	return s.repo.GetSmsByID(ctx, id)
}

var smsNightRestrictedFn = isSMSNightRestricted

func isSMSNightRestricted(t time.Time) bool {

	if os.Getenv("SMS_ALLOW_NIGHT_SEND") == "true" {
		return false
	}
	cst := time.FixedZone("CST", 8*3600)
	hour := t.In(cst).Hour()
	return hour >= 22 || hour < 8
}

func (s *smsService) SendSms(ctx context.Context, req *dto.SmsSendRequest) error {

	if smsNightRestrictedFn(time.Now()) {
		return errors.New("当前处于夜间禁发时段(22:00-8:00)，短信已拦截")
	}

	if err := validateSMSPhone(req.Phone); err != nil {
		return err
	}

	if s.unsub().IsUnsubscribed(ctx, req.Phone) {
		// 这里返回哨兵而不是 nil：nil 会被每一个调用方读成"发成功了"
		// （外发链据此记成功、烧幂等键、把这条算进送达率分母）。错误串不带手机号，理由见
		// service/proactive_reach.go 里同一族哨兵的注释——这条串会落进队列台账。
		return fmt.Errorf("%w: 该号码已在短信退订名单里，未向渠道提交任何内容", ErrDoNotContact)
	}
	config, err := s.repo.GetConfig(ctx)
	if err != nil {
		return err
	}

	record := &model.SmsRecord{
		Phone:    req.Phone,
		Content:  req.Content,
		Provider: config.DefaultProvider,
		Status:   "sending",
	}

	if err := s.repo.CreateSmsRecord(ctx, record); err != nil {
		return err
	}

	sentTime, errCode, errMsg, err := s.dispatchToProvider(ctx, req.Phone, req.Content, config.DefaultProvider)
	if err != nil {
		record.Status = "failed"
		record.ErrorCode = errCode
		record.ErrorMsg = errMsg
		record.SendTime = &time.Time{}
		_ = s.repo.UpdateSmsRecord(ctx, record)
		return fmt.Errorf("send sms failed: %w", err)
	}

	record.SendTime = &sentTime
	record.Status = "sent"

	return s.repo.UpdateSmsRecord(ctx, record)
}

// smsErrCodeUnspecified 是"渠道失败了但没给出错误码"时落到台账上的占位码。
// 它不进 sms_retryable_error_prefixes，因此不会被 RetryFailedMessages 认成可重试；
// 存在的意义只是让那一行"failed"在页面与导出里不再是空白。
const smsErrCodeUnspecified = "unspecified_error"

func (s *smsService) dispatchToProvider(ctx context.Context, phone, content, provider string) (time.Time, string, string, error) {
	var sentTime time.Time
	var errCode, errMsg string
	var err error
	switch provider {
	case "aliyun":
		sentTime, errCode, errMsg, err = s.sendAliyun(ctx, phone, content)
	case "tencent":
		sentTime, errCode, errMsg, err = s.sendTencent(ctx, phone, content)
	case "huawei":
		sentTime, errCode, errMsg, err = s.sendHuawei(ctx, phone, content)
	default:
		err = fmt.Errorf("unknown sms provider: %s", provider)
	}
	// 三家网关在"连不上""回包解不开"这类分支上把 code/msg 交回空串，而两个调用方都是
	// record.ErrorCode = errCode / record.ErrorMsg = errMsg 直接落账 ⇒ 台账上出现
	// "status=failed、原因一片空白"的一行，真实原因此时只活在这条调用的返回值里。
	// 空串在这里补成错误本身，别让它带着空白进台账。
	if err != nil {
		if errMsg == "" {
			errMsg = err.Error()
		}
		if errCode == "" {
			errCode = smsErrCodeUnspecified
		}
	}
	return sentTime, errCode, errMsg, err
}

func (s *smsService) sendAliyun(ctx context.Context, phone, content string) (time.Time, string, string, error) {
	cfg, err := s.repo.GetAliyunConfig(ctx)
	if err != nil {
		return time.Time{}, "", "", err
	}
	if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return time.Time{}, "", "", errors.New("aliyun sms config missing")
	}

	apiURL := s.aliyunAPIURL

	params := url.Values{}
	params.Set("PhoneNumbers", phone)
	params.Set("SignName", cfg.SignName)
	params.Set("TemplateCode", "SMS_000000001")
	params.Set("TemplateParam", `{"content":"`+escapeJSON(content)+`"}`)
	params.Set("AccessKeyId", cfg.AccessKeyID)
	params.Set("Timestamp", time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	params.Set("Format", "JSON")
	params.Set("SignatureMethod", "HMAC-SHA1")
	params.Set("SignatureVersion", "1.0")
	params.Set("SignatureNonce", randomNonce())
	params.Set("Action", "SendSms")
	params.Set("Version", "2017-05-25")
	params.Set("RegionId", "cn-hangzhou")

	_ = specialURLEncode(apiURL) + "&" + specialURLEncode(percentEncode(params.Encode()))
	mac := hmac.New(sha256.New, []byte(cfg.AccessKeySecret+"&"))
	_ = mac
	sign := signAliyun(params, cfg.AccessKeySecret)
	params.Set("Signature", sign)

	client := httpclient.NewWithTimeout(30 * time.Second)
	var resp *http.Response
	var lastErr error
	for attempt := 0; attempt <= 2; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
		}
		resp, lastErr = client.PostForm(apiURL, params)
		if lastErr != nil {
			continue
		}
		if resp.StatusCode >= 500 {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("aliyun sms http status %d", resp.StatusCode)
			continue
		}
		break
	}
	if resp == nil {
		return time.Time{}, "", "", fmt.Errorf("aliyun sms request failed after retries: %w", lastErr)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Code      string `json:"Code"`
		Message   string `json:"Message"`
		RequestID string `json:"RequestId"`
		BizID     string `json:"BizId"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return time.Time{}, "", "", fmt.Errorf("decode aliyun response: %w", err)
	}
	if result.Code != "OK" {
		return time.Time{}, result.Code, result.Message, fmt.Errorf("aliyun sms error: %s", result.Message)
	}
	return time.Now(), result.Code, result.Message, nil
}

func (s *smsService) sendTencent(ctx context.Context, phone, content string) (time.Time, string, string, error) {
	cfg, err := s.repo.GetTencentConfig(ctx)
	if err != nil {
		return time.Time{}, "", "", err
	}
	if cfg.SecretID == "" || cfg.SecretKey == "" {
		return time.Time{}, "", "", errors.New("tencent sms config missing")
	}

	apiHost := "sms.tencentcloudapi.com"
	// 签名与 Host 头按官方域算（apiHost），实际请求发往 s.tencentAPIURL —— 测试把它指向本地服务时，
	// 签名串保持官方形状，被替换的只有"发给谁"。
	apiURL := s.tencentAPIURL
	service := "sms"
	version := "2021-01-11"
	action := "SendSms"
	algorithm := "TC3-HMAC-SHA256"

	reqBody := map[string]any{
		"PhoneNumberSet":   []string{phone},
		"SmsSdkAppId":      cfg.AppID,
		"SignName":         cfg.SignName,
		"TemplateId":       "0000000",
		"TemplateParamSet": []string{content},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	date := time.Now().UTC().Format("2006-01-02")
	canonicalHeaders := "content-type:application/json; charset=utf-8\n" +
		"host:" + apiHost + "\n" +
		"x-tc-action:" + strings.ToLower(action) + "\n"
	signedHeaders := "content-type;host;x-tc-action"
	hashedRequestPayload := sha256Hex(string(bodyBytes))
	canonicalRequest := "POST\n/\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + hashedRequestPayload

	credentialScope := date + "/" + service + "/tc3_request"
	hashedCanonicalRequest := sha256Hex(canonicalRequest)
	stringToSign := algorithm + "\n" + timestamp + "\n" + credentialScope + "\n" + hashedCanonicalRequest

	secretDate := hmacSHA256([]byte("TC3"+cfg.SecretKey), date)
	secretService := hmacSHA256(secretDate, service)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))

	authorization := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm, cfg.SecretID, credentialScope, signedHeaders, signature)

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return time.Time{}, "", "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Host", apiHost)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Timestamp", timestamp)
	req.Header.Set("X-TC-Version", version)
	req.Header.Set("Authorization", authorization)

	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return time.Time{}, "", "", err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Response struct {
			Error struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
			RequestID string `json:"RequestId"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return time.Time{}, "", "", fmt.Errorf("decode tencent response: %w", err)
	}
	if result.Response.Error.Code != "" {
		return time.Time{}, result.Response.Error.Code, result.Response.Error.Message,
			fmt.Errorf("tencent sms error: %s", result.Response.Error.Message)
	}
	return time.Now(), "OK", "OK", nil
}

func (s *smsService) sendHuawei(ctx context.Context, phone, content string) (time.Time, string, string, error) {
	cfg, err := s.repo.GetHuaweiConfig(ctx)
	if err != nil {
		return time.Time{}, "", "", err
	}
	if cfg.AppKey == "" || cfg.AppSecret == "" {
		return time.Time{}, "", "", errors.New("huawei sms config missing")
	}

	apiURL := s.huaweiAPIURL

	body := map[string]any{
		"from":          cfg.Sender,
		"to":            []string{phone},
		"templateId":    "0000000",
		"templateParas": []string{content},
		"signature":     cfg.Signature,
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return time.Time{}, "", "", err
	}
	auth := base64.StdEncoding.EncodeToString([]byte(cfg.AppKey + ":" + cfg.AppSecret))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("X-WSSE", buildWSSE(cfg.AppKey, cfg.AppSecret))

	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return time.Time{}, "", "", err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return time.Time{}, "", "", fmt.Errorf("decode huawei response: %w", err)
	}
	if result.Code != "000000" {
		return time.Time{}, result.Code, result.Message, fmt.Errorf("huawei sms error: %s", result.Message)
	}
	return time.Now(), "OK", "OK", nil
}

func (s *smsService) ResendSms(ctx context.Context, id uint) error {

	if smsNightRestrictedFn(time.Now()) {
		return errors.New("当前处于夜间禁发时段(22:00-8:00)，短信已拦截")
	}
	record, err := s.repo.GetSmsByID(ctx, id)
	if err != nil {
		return err
	}

	if record.Status != "failed" {
		return errors.New("只有失败的短信可以重发")
	}

	// 重发是同一条判据的第二个入口：首次发送之后号码才回复 TD 的情况很常见，
	// 而这里直连 dispatchToProvider，绕过了 SendSms 那道检查。
	// 放在改状态之前：拦下时不该在台账上留一条"正在发"。
	if s.unsub().IsUnsubscribed(ctx, record.Phone) {
		return fmt.Errorf("%w: 该号码已在短信退订名单里，重发同样不提交给渠道", ErrDoNotContact)
	}

	record.Status = "sending"
	record.ErrorCode = ""
	record.ErrorMsg = ""

	if err := s.repo.UpdateSmsRecord(ctx, record); err != nil {
		return err
	}

	config, err := s.repo.GetConfig(ctx)
	if err != nil {
		record.Status = "failed"
		record.ErrorMsg = "获取短信配置失败: " + err.Error()
		_ = s.repo.UpdateSmsRecord(ctx, record)
		return err
	}

	sentTime, errCode, errMsg, err := s.dispatchToProvider(ctx, record.Phone, record.Content, config.DefaultProvider)
	if err != nil {
		record.Status = "failed"
		record.ErrorCode = errCode
		record.ErrorMsg = errMsg
		_ = s.repo.UpdateSmsRecord(ctx, record)
		return fmt.Errorf("resend sms failed: %w", err)
	}

	record.SendTime = &sentTime
	record.Status = "sent"

	return s.repo.UpdateSmsRecord(ctx, record)
}

func (s *smsService) GetDraftList(ctx context.Context, req *dto.SmsDraftListRequest) ([]*model.SmsDraft, int64, error) {
	return s.repo.GetDraftList(ctx, req.Page, req.Limit, req.Title)
}

func (s *smsService) GetDraftByID(ctx context.Context, id uint) (*model.SmsDraft, error) {
	return s.repo.GetDraftByID(ctx, id)
}

func (s *smsService) CreateDraft(ctx context.Context, req *dto.SmsDraftCreateRequest) error {
	draft := &model.SmsDraft{
		Title:   req.Title,
		Content: req.Content,
	}
	return s.repo.CreateDraft(ctx, draft)
}

func (s *smsService) UpdateDraft(ctx context.Context, id uint, req *dto.SmsDraftUpdateRequest) error {
	draft, err := s.repo.GetDraftByID(ctx, id)
	if err != nil {
		return err
	}

	draft.Title = req.Title
	draft.Content = req.Content
	return s.repo.UpdateDraft(ctx, draft)
}

func (s *smsService) DeleteDraft(ctx context.Context, id uint) error {
	return s.repo.DeleteDraft(ctx, id)
}

func (s *smsService) SendDraft(ctx context.Context, id uint, phone string) error {
	draft, err := s.repo.GetDraftByID(ctx, id)
	if err != nil {
		return err
	}

	req := &dto.SmsSendRequest{
		Phone:   phone,
		Content: draft.Content,
	}
	return s.SendSms(ctx, req)
}

func (s *smsService) GetJobList(ctx context.Context, req *dto.SmsJobListRequest) ([]*model.SmsJob, int64, error) {
	return s.repo.GetJobList(ctx, req.Page, req.Limit, req.Status, req.Name)
}

func (s *smsService) GetJobByID(ctx context.Context, id uint) (*model.SmsJob, error) {
	return s.repo.GetJobByID(ctx, id)
}

func (s *smsService) CreateJob(ctx context.Context, req *dto.SmsJobCreateRequest) error {
	job := &model.SmsJob{
		Name:         req.Name,
		Total:        len(req.PhoneList),
		Sent:         0,
		Failed:       0,
		Status:       "pending",
		ScheduleTime: req.ScheduleTime,
	}

	if req.ScheduleTime != nil && smsNightRestrictedFn(*req.ScheduleTime) {
		return errors.New("预约时间处于夜间禁发时段(22:00-8:00)，短信已拦截")
	}
	for _, phone := range req.PhoneList {
		if err := validateSMSPhone(phone); err != nil {
			return err
		}
	}

	if err := s.repo.CreateJob(ctx, job); err != nil {
		return err
	}

	details := make([]*model.SmsJobDetail, 0, len(req.PhoneList))
	for _, phone := range req.PhoneList {
		detail := &model.SmsJobDetail{
			JobID:   job.ID,
			Phone:   phone,
			Content: req.Content,
			Status:  "pending",
		}
		details = append(details, detail)
	}

	if err := s.repo.CreateJobDetails(ctx, details); err != nil {
		return err
	}

	if req.ScheduleTime == nil || req.ScheduleTime.Before(time.Now()) {
		job.Status = "running"
		return s.repo.UpdateJob(ctx, job)
	}

	return nil
}

func (s *smsService) PauseJob(ctx context.Context, id uint) error {
	job, err := s.repo.GetJobByID(ctx, id)
	if err != nil {
		return err
	}

	if job.Status != "running" {
		return errors.New("只能暂停运行中的任务")
	}

	job.Status = "paused"
	return s.repo.UpdateJob(ctx, job)
}

func (s *smsService) ResumeJob(ctx context.Context, id uint) error {
	job, err := s.repo.GetJobByID(ctx, id)
	if err != nil {
		return err
	}

	if job.Status != "paused" {
		return errors.New("只能继续已暂停的任务")
	}

	job.Status = "running"
	return s.repo.UpdateJob(ctx, job)
}

func (s *smsService) StopJob(ctx context.Context, id uint) error {
	job, err := s.repo.GetJobByID(ctx, id)
	if err != nil {
		return err
	}

	if job.Status != "running" && job.Status != "paused" && job.Status != "pending" {
		return errors.New("只能停止运行中、暂停或待执行的任务")
	}

	job.Status = "failed"
	return s.repo.UpdateJob(ctx, job)
}

func (s *smsService) DeleteJob(ctx context.Context, id uint) error {
	job, err := s.repo.GetJobByID(ctx, id)
	if err != nil {
		return err
	}

	if job.Status != "completed" && job.Status != "failed" {
		return errors.New("只能删除已完成或失败的任务")
	}

	if err := s.repo.DeleteJobDetails(ctx, id); err != nil {
		return err
	}

	return s.repo.DeleteJob(ctx, id)
}

func (s *smsService) GetJobRecords(ctx context.Context, id uint, page, limit int) ([]*model.SmsJobDetail, int64, error) {
	_, err := s.repo.GetJobByID(ctx, id)
	if err != nil {
		return nil, 0, err
	}

	return s.repo.GetJobDetails(ctx, id, page, limit)
}
