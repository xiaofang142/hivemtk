package service

import (
	"bytes"

	"context"

	"encoding/json"

	"errors"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/httpclient"
	"hivemtk-user/internal/pkg/utils/logger"

	"hivemtk-user/internal/repository"

	"io"

	"math"

	"net/http"

	"net/url"

	"fmt"
	"time"
)

func yuanToFen(yuan float64) int64 {
	return int64(math.Round(yuan * 100))
}

type IntegrationService struct {
	accountRepo      *repository.IntegrationAccountRepository
	syncLogRepo      *repository.SyncLogRepository
	customerRepo     *repository.ExternalCustomerRepository
	orderRepo        *repository.ExternalOrderRepository
	productRepo      *repository.ExternalProductRepository
	webhookEventRepo *repository.WebhookEventRepository

	// payments 回款腿（T-P7-02）。**不在构造函数里 new**：PaymentService 要的是
	// 两个仓储的注入（payments + bills），而本服务是"全局 DB 句柄"那一代的老形状。
	// 生产上这一格由构造函数从全局登记处取（见下面的 GlobalPaymentService 那一支），
	// 测试里可以再用 SetOrderPaymentSink 换成假腿（判据：中间那一格要看得清）。
	// 没塞 ⇒ 带 payment 格子的回调明确报错（见 UpsertOrderFromWebhook），不静默丢一笔钱。
	payments OrderPaymentSink
}

var (
	_ *repository.IntegrationAccountRepository
)

// NewIntegrationService 构造。回款腿在**这一次**取全局：与路由侧同一条口径
// （装配点必须先于构造，判据见 app.InitPaymentRuntime 的文件头与 router.go 的顺序）。
//
// 为什么不在每次回调里现取全局：那会让请求路径（含 webhook 的处理协程）读包级全局，
// 而本仓对这一类站点有一道静态门（scripts/check-async-global-read.py 的零条目基线）。
// 构造函数在这里跑一次，正好躲开那一格——代价是"装配晚于构造"会永久记不到钱，
// 所以 router.go 里那两行的顺序由 order_webhook_payment_wiring_test.go 与它自己的用例守着。
//
// 判空写成"取出来再判"而不是直接赋值：全局里放着的可能是 typed-nil，
// 赋给接口格子之后 `!= nil` 为真，"有没有腿"这一格从此没有可信答案（判据见那一份用例的 ②）。
func NewIntegrationService() *IntegrationService {
	svc := &IntegrationService{
		accountRepo:      repository.NewIntegrationAccountRepository(),
		syncLogRepo:      repository.NewSyncLogRepository(),
		customerRepo:     repository.NewExternalCustomerRepository(),
		orderRepo:        repository.NewExternalOrderRepository(),
		productRepo:      repository.NewExternalProductRepository(),
		webhookEventRepo: repository.NewWebhookEventRepository(),
	}
	if pay := GlobalPaymentService(); pay != nil {
		svc.payments = pay
	}
	return svc
}

// SetOrderPaymentSink 注入回款腿；传 nil 即摘掉（测试与"账单域没启用"的环境）。
//
// 老规矩：晚于构造注入的依赖一律显式走 setter（与 SetWebhookEventRepoDB 那族同一个理由 ——
// 构造函数不改签名，就不会连带改掉十几个调用点）。
func (s *IntegrationService) SetOrderPaymentSink(sink OrderPaymentSink) {
	if s == nil {
		return
	}
	s.payments = sink
}

type Platform string

const (
	PlatformXiaoshouyi Platform = "crm_xiaoshouyi"

	PlatformFenxiangxiao Platform = "crm_fenxiangxiao"

	PlatformTaobao Platform = "ecommerce_taobao"

	PlatformJD Platform = "ecommerce_jd"
)

type CreateIntegrationAccountRequest struct {
	Platform    string         `json:"platform" binding:"required"`
	AccountName string         `json:"account_name"`
	APIKey      string         `json:"api_key"`
	APISecret   string         `json:"api_secret"`
	Config      map[string]any `json:"config"`
}

func (s *IntegrationService) CreateIntegrationAccount(ctx context.Context, req *CreateIntegrationAccountRequest) (*model.IntegrationAccount, error) {
	configJSON := ""
	if req.Config != nil {
		data, _ := json.Marshal(req.Config)
		configJSON = string(data)
	}

	account := &model.IntegrationAccount{
		Platform:    req.Platform,
		AccountName: req.AccountName,
		APIKey:      req.APIKey,
		APISecret:   req.APISecret,
		Config:      configJSON,
		Status:      1,
	}

	if err := s.accountRepo.Create(ctx, account); err != nil {
		return nil, err
	}

	return account, nil
}

func (s *IntegrationService) GetIntegrationAccountList(ctx context.Context) ([]*model.IntegrationAccount, error) {
	return s.accountRepo.GetAll(ctx)
}

func (s *IntegrationService) GetIntegrationAccountByID(ctx context.Context, id uint) (*model.IntegrationAccount, error) {
	return s.accountRepo.GetByID(ctx, id)
}

func (s *IntegrationService) UpdateIntegrationAccount(ctx context.Context, id uint, req *CreateIntegrationAccountRequest) (*model.IntegrationAccount, error) {
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	account.AccountName = req.AccountName
	account.APIKey = req.APIKey
	account.APISecret = req.APISecret
	if req.Config != nil {
		data, _ := json.Marshal(req.Config)
		account.Config = string(data)
	}

	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, err
	}

	return account, nil
}

func (s *IntegrationService) DeleteIntegrationAccount(ctx context.Context, id uint) error {
	return s.accountRepo.Delete(ctx, id)
}

type XiaoshouyiClient struct {
	accountRepo *repository.IntegrationAccountRepository
	account     *model.IntegrationAccount
	httpClient  *http.Client
}

func NewXiaoshouyiClient(account *model.IntegrationAccount, accountRepo *repository.IntegrationAccountRepository) *XiaoshouyiClient {
	return &XiaoshouyiClient{
		accountRepo: accountRepo,
		account:     account,
		httpClient:  httpclient.NewWithTimeout(30 * time.Second),
	}
}

func (c *XiaoshouyiClient) GetAccessToken(ctx context.Context) (string, error) {
	if c.account.AccessToken != "" && c.account.TokenExpires != nil && time.Now().Before(*c.account.TokenExpires) {
		return c.account.AccessToken, nil
	}

	tokenURL := "https://api.xiaoshouyi.com/oauth/token"
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.account.APIKey},
		"client_secret": {c.account.APISecret},
	}

	resp, err := c.httpClient.PostForm(tokenURL, data)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if result.Error != "" {
		return "", errors.New(result.Error)
	}

	expiresTime := time.Now().Add(time.Duration(result.ExpiresIn-600) * time.Second)
	if e := c.accountRepo.UpdateToken(ctx, c.account.ID, result.AccessToken, &expiresTime); e != nil {
		logger.Warnf("[Integration] Token 落库失败 accountID=%d: %v", c.account.ID, e)
	}

	return result.AccessToken, nil
}

func (s *IntegrationService) SyncCustomers(ctx context.Context, account *model.IntegrationAccount) (int, error) {
	switch account.Platform {
	case string(PlatformXiaoshouyi):
		return s.syncXiaoshouyiCustomers(ctx, account)
	case string(PlatformFenxiangxiao):
		return s.syncFenxiangxiaoCustomers(ctx, account)
	default:
		return 0, errors.New("不支持的平台")
	}
}

func (s *IntegrationService) syncXiaoshouyiCustomers(ctx context.Context, account *model.IntegrationAccount) (int, error) {
	client := NewXiaoshouyiClient(account, s.accountRepo)
	token, err := client.GetAccessToken(ctx)
	if err != nil {
		return 0, err
	}

	syncLog := &model.SyncLog{
		Platform:  account.Platform,
		SyncType:  "customer",
		Status:    0,
		StartTime: time.Now(),
	}
	if err := s.syncLogRepo.Create(ctx, syncLog); err != nil {
		return 0, err
	}

	apiURL := "https://api.xiaoshouyi.com/crm/v2/leads"
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.httpClient.Do(req)
	if err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}

	var result struct {
		Data []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Phone        string `json:"phone"`
			Email        string `json:"email"`
			Company      string `json:"company"`
			Industry     string `json:"industry"`
			OwnerID      string `json:"owner_id"`
			OwnerName    string `json:"owner_name"`
			Status       string `json:"status"`
			Source       string `json:"source"`
			CreatedTime  int64  `json:"created_time"`
			ModifiedTime int64  `json:"modified_time"`
		} `json:"data"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}

	if result.Error.Code != 0 {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, result.Error.Message); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, errors.New(result.Error.Message)
	}

	count := 0
	for _, c := range result.Data {
		customer := &model.ExternalCustomer{
			Platform:      account.Platform,
			ExternalID:    c.ID,
			Name:          c.Name,
			Phone:         c.Phone,
			Email:         c.Email,
			Company:       c.Company,
			Industry:      c.Industry,
			OwnerID:       c.OwnerID,
			OwnerName:     c.OwnerName,
			Status:        c.Status,
			Source:        c.Source,
			LastContactAt: func() *time.Time { t := time.Unix(c.ModifiedTime, 0); return &t }(),
		}

		existing, _ := s.customerRepo.GetByExternalID(ctx, account.Platform, c.ID)
		if existing != nil {
			customer.ID = existing.ID
			if e := s.customerRepo.Update(ctx, customer); e != nil {
				logger.Errorf("[Integration] 客户落库失败 extID=%s: %v", c.ID, e)
				continue
			}
		} else if e := s.customerRepo.Create(ctx, customer); e != nil {
			logger.Errorf("[Integration] 客户创建失败 extID=%s: %v", c.ID, e)
			continue
		}
		count++
	}

	if e := s.accountRepo.UpdateSyncTime(ctx, account.ID); e != nil {
		logger.Warnf("[Integration] 更新同步时间失败 accountID=%d: %v", account.ID, e)
	}
	if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 1, count, ""); e != nil {
		logger.Warnf("[Integration] 同步成功状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
	}

	return count, nil
}

type FenxiangxiaoClient struct {
	accountRepo *repository.IntegrationAccountRepository
	account     *model.IntegrationAccount
	httpClient  *http.Client
}

func NewFenxiangxiaoClient(account *model.IntegrationAccount, accountRepo *repository.IntegrationAccountRepository) *FenxiangxiaoClient {
	return &FenxiangxiaoClient{
		accountRepo: accountRepo,
		account:     account,
		httpClient:  httpclient.NewWithTimeout(30 * time.Second),
	}
}

func (c *FenxiangxiaoClient) GetAccessToken(ctx context.Context) (string, error) {
	if c.account.AccessToken != "" && c.account.TokenExpires != nil && time.Now().Before(*c.account.TokenExpires) {
		return c.account.AccessToken, nil
	}

	tokenURL := "https://api.fxiaoke.com/oauth2/token"
	data := url.Values{
		"grant_type": {"client_credentials"},
		"app_key":    {c.account.APIKey},
		"app_secret": {c.account.APISecret},
	}

	resp, err := c.httpClient.PostForm(tokenURL, data)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Errcode     int    `json:"errcode"`
		Errmsg      string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if result.Errcode != 0 {
		return "", errors.New(result.Errmsg)
	}

	expiresTime := time.Now().Add(time.Duration(result.ExpiresIn-600) * time.Second)
	if e := c.accountRepo.UpdateToken(ctx, c.account.ID, result.AccessToken, &expiresTime); e != nil {
		logger.Warnf("[Integration] Token 落库失败 accountID=%d: %v", c.account.ID, e)
	}

	return result.AccessToken, nil
}

func (s *IntegrationService) syncFenxiangxiaoCustomers(ctx context.Context, account *model.IntegrationAccount) (int, error) {
	client := NewFenxiangxiaoClient(account, s.accountRepo)
	token, err := client.GetAccessToken(ctx)
	if err != nil {
		return 0, err
	}

	syncLog := &model.SyncLog{
		Platform:  account.Platform,
		SyncType:  "customer",
		Status:    0,
		StartTime: time.Now(),
	}
	if err := s.syncLogRepo.Create(ctx, syncLog); err != nil {
		return 0, err
	}

	apiURL := "https://api.fxiaoke.com/crm/lead/v2/list"
	reqBody := map[string]any{
		"page":     1,
		"pagesize": 100,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", apiURL, bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.httpClient.Do(req)
	if err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}

	var result struct {
		Response struct {
			Data []struct {
				ID        string   `json:"id"`
				Name      string   `json:"name"`
				Phone     string   `json:"mobile"`
				Email     string   `json:"email"`
				Company   string   `json:"company_name"`
				Position  string   `json:"position"`
				OwnerID   string   `json:"owner_id"`
				OwnerName string   `json:"owner_name"`
				Status    string   `json:"status"`
				Source    string   `json:"source"`
				Tags      []string `json:"tags"`
			} `json:"data"`
			TotalCount int `json:"total_count"`
		} `json:"response"`
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}

	if result.Errcode != 0 {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, result.Errmsg); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, errors.New(result.Errmsg)
	}

	count := 0
	for _, c := range result.Response.Data {
		tagsJSON, _ := json.Marshal(c.Tags)
		customer := &model.ExternalCustomer{
			Platform:   account.Platform,
			ExternalID: c.ID,
			Name:       c.Name,
			Phone:      c.Phone,
			Email:      c.Email,
			Company:    c.Company,
			Position:   c.Position,
			OwnerID:    c.OwnerID,
			OwnerName:  c.OwnerName,
			Status:     c.Status,
			Source:     c.Source,
			Tags:       string(tagsJSON),
		}

		existing, _ := s.customerRepo.GetByExternalID(ctx, account.Platform, c.ID)
		if existing != nil {
			customer.ID = existing.ID
			if e := s.customerRepo.Update(ctx, customer); e != nil {
				logger.Errorf("[Integration] 客户落库失败 extID=%s: %v", c.ID, e)
				continue
			}
		} else if e := s.customerRepo.Create(ctx, customer); e != nil {
			logger.Errorf("[Integration] 客户创建失败 extID=%s: %v", c.ID, e)
			continue
		}
		count++
	}

	if e := s.accountRepo.UpdateSyncTime(ctx, account.ID); e != nil {
		logger.Warnf("[Integration] 更新同步时间失败 accountID=%d: %v", account.ID, e)
	}
	if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 1, count, ""); e != nil {
		logger.Warnf("[Integration] 同步成功状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
	}

	return count, nil
}

type TaobaoClient struct {
	account    *model.IntegrationAccount
	httpClient *http.Client
	appKey     string
	appSecret  string
}

func NewTaobaoClient(account *model.IntegrationAccount) *TaobaoClient {
	return &TaobaoClient{
		account:    account,
		httpClient: httpclient.NewWithTimeout(30 * time.Second),
		appKey:     account.APIKey,
		appSecret:  account.APISecret,
	}
}

func (s *IntegrationService) SyncOrders(ctx context.Context, account *model.IntegrationAccount) (int, error) {
	switch account.Platform {
	case string(PlatformTaobao):
		return s.syncTaobaoOrders(ctx, account)
	case string(PlatformJD):
		return s.syncJDOrders(ctx, account)
	default:
		return 0, errors.New("不支持的平台")
	}
}

func (s *IntegrationService) syncTaobaoOrders(ctx context.Context, account *model.IntegrationAccount) (int, error) {
	client := NewTaobaoClient(account)

	syncLog := &model.SyncLog{
		Platform:  account.Platform,
		SyncType:  "order",
		Status:    0,
		StartTime: time.Now(),
	}
	if err := s.syncLogRepo.Create(ctx, syncLog); err != nil {
		return 0, err
	}

	apiURL := "https://gw.api.taobao.com/router/rest"
	params := url.Values{
		"app_key":       {client.appKey},
		"method":        {"taobao.trades.sold.get"},
		"format":        {"json"},
		"v":             {"2.0"},
		"start_created": {time.Now().AddDate(0, -1, 0).Format("2006-01-02 15:04:05")},
		"end_created":   {time.Now().Format("2006-01-02 15:04:05")},
		"page":          {"1"},
		"page_size":     {"100"},
		"fields":        {"tid,type,status,payment,receiver_name,receiver_phone,created,orders"},
	}

	resp, err := client.httpClient.Get(apiURL + "?" + params.Encode())
	if err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}

	var result struct {
		TradesSoldGetResponse struct {
			Trades struct {
				Trade []struct {
					TID           string  `json:"tid"`
					Type          string  `json:"type"`
					Status        string  `json:"status"`
					Payment       float64 `json:"payment"`
					ReceiverName  string  `json:"receiver_name"`
					ReceiverPhone string  `json:"receiver_phone"`
					Created       string  `json:"created"`
					PayTime       string  `json:"pay_time"`
					ConsentTime   string  `json:"consign_time"`
					Orders        struct {
						Order []struct {
							Title    string  `json:"title"`
							Price    float64 `json:"price"`
							Num      int     `json:"num"`
							OuterIid string  `json:"outer_iid"`
						} `json:"order"`
					} `json:"orders"`
				} `json:"trade"`
			} `json:"trades"`
		} `json:"trades_sold_get_response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}

	count := 0
	for _, t := range result.TradesSoldGetResponse.Trades.Trade {

		var items []map[string]any
		for _, o := range t.Orders.Order {
			items = append(items, map[string]any{
				"title":      o.Title,
				"price":      o.Price,
				"quantity":   o.Num,
				"product_id": o.OuterIid,
			})
		}
		itemsJSON, _ := json.Marshal(items)

		var payTime, shipTime, orderTime *time.Time
		if t.Created != "" {
			if ct, err := time.Parse("2006-01-02 15:04:05", t.Created); err == nil {
				orderTime = &ct
			}
		}
		if t.PayTime != "" {
			pt, _ := time.Parse("2006-01-02 15:04:05", t.PayTime)
			payTime = &pt
		}
		if t.ConsentTime != "" {
			st, _ := time.Parse("2006-01-02 15:04:05", t.ConsentTime)
			shipTime = &st
		}

		order := &model.ExternalOrder{
			Platform:  account.Platform,
			OrderID:   t.TID,
			Status:    t.Status,
			OrderTime: orderTime,
			PayAmount: yuanToFen(t.Payment),
			UserName:  t.ReceiverName,
			UserPhone: t.ReceiverPhone,
			PayTime:   payTime,
			ShipTime:  shipTime,
			Items:     string(itemsJSON),
		}

		existing, lookErr := s.orderRepo.GetByOrderID(ctx, account.Platform, t.TID)
		if lookErr != nil {
			// 读不动库时**不猜**它是新单：老代码 `existing, _ :=` 把读故障读成"没见过"，
			// 于是走 Create —— 轻则撞唯一键丢单，重则插出同一单的第二行（G15 第④条）。
			logger.Errorf("[Integration] 订单读取失败, 本轮跳过(下轮重同步) tid=%s: %v", t.TID, lookErr)
			continue
		}
		if existing != nil {
			order.ID = existing.ID
			// 账单线索由 webhook 的回款腿写，同步这一路不认识它：不抄回来就等于每次拉取
			// 把"这单挂在哪张应收上"抹一次。
			order.BillID = existing.BillID
			if e := s.orderRepo.Update(ctx, order); e != nil {
				logger.Errorf("[Integration] 订单更新失败: %v", e)
				continue
			}
		} else if e := s.orderRepo.Create(ctx, order); e != nil {
			logger.Errorf("[Integration] 订单创建失败: %v", e)
			continue
		}
		count++
	}

	if e := s.accountRepo.UpdateSyncTime(ctx, account.ID); e != nil {
		logger.Warnf("[Integration] 更新同步时间失败 accountID=%d: %v", account.ID, e)
	}
	if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 1, count, ""); e != nil {
		logger.Warnf("[Integration] 同步成功状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
	}

	return count, nil
}

type JDClient struct {
	account    *model.IntegrationAccount
	httpClient *http.Client
	appKey     string
	appSecret  string
}

func NewJDClient(account *model.IntegrationAccount) *JDClient {
	return &JDClient{
		account:    account,
		httpClient: httpclient.NewWithTimeout(30 * time.Second),
		appKey:     account.APIKey,
		appSecret:  account.APISecret,
	}
}

func (s *IntegrationService) syncJDOrders(ctx context.Context, account *model.IntegrationAccount) (int, error) {
	client := NewJDClient(account)

	syncLog := &model.SyncLog{
		Platform:  account.Platform,
		SyncType:  "order",
		Status:    0,
		StartTime: time.Now(),
	}
	if err := s.syncLogRepo.Create(ctx, syncLog); err != nil {
		return 0, err
	}

	apiURL := "https://api.jd.com/routerjson"
	params := url.Values{
		"app_key":    {client.appKey},
		"method":     {"jingdong.pop.order.search"},
		"format":     {"json"},
		"v":          {"2.0"},
		"start_date": {time.Now().AddDate(0, -1, 0).Format("2006-01-02 15:04:05")},
		"end_date":   {time.Now().Format("2006-01-02 15:04:05")},
		"page":       {"1"},
		"page_size":  {"100"},
	}

	resp, err := client.httpClient.Get(apiURL + "?" + params.Encode())
	if err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}

	var result struct {
		OrderSearchResponse struct {
			Orders []struct {
				OrderID           string  `json:"order_id"`
				OrderStatus       string  `json:"order_status"`
				OrderTotal        float64 `json:"order_total_price"`
				OrderPayment      float64 `json:"order_payment"`
				Consignee         string  `json:"consignee"`
				Telephone         string  `json:"telephone"`
				OrderStartTime    string  `json:"order_start_time"`
				OrderPaymentTime  string  `json:"order_payment_time"`
				OrderOutboundTime string  `json:"order_outbound_time"`
				SKUList           []struct {
					SkuID   string  `json:"sku_id"`
					SkuName string  `json:"sku_name"`
					Price   float64 `json:"price"`
					Num     int     `json:"num"`
				} `json:"sku_list"`
			} `json:"orders"`
			Total int `json:"total"`
		} `json:"jingdong_pop_order_search_response"`
		ErrMsg string `json:"error_response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}

	count := 0
	for _, o := range result.OrderSearchResponse.Orders {

		var items []map[string]any
		for _, sku := range o.SKUList {
			items = append(items, map[string]any{
				"title":      sku.SkuName,
				"price":      sku.Price,
				"quantity":   sku.Num,
				"product_id": sku.SkuID,
			})
		}
		itemsJSON, _ := json.Marshal(items)

		var payTime, shipTime, orderTime *time.Time
		if o.OrderStartTime != "" {
			if st, err := time.Parse("2006-01-02 15:04:05", o.OrderStartTime); err == nil {
				orderTime = &st
			}
		}
		if o.OrderPaymentTime != "" {
			pt, _ := time.Parse("2006-01-02 15:04:05", o.OrderPaymentTime)
			payTime = &pt
		}
		if o.OrderOutboundTime != "" {
			st, _ := time.Parse("2006-01-02 15:04:05", o.OrderOutboundTime)
			shipTime = &st
		}

		order := &model.ExternalOrder{
			Platform:  account.Platform,
			OrderID:   o.OrderID,
			Status:    o.OrderStatus,
			OrderTime: orderTime,
			PayAmount: yuanToFen(o.OrderPayment),
			UserName:  o.Consignee,
			UserPhone: o.Telephone,
			PayTime:   payTime,
			ShipTime:  shipTime,
			Items:     string(itemsJSON),
		}

		existing, lookErr := s.orderRepo.GetByOrderID(ctx, account.Platform, o.OrderID)
		if lookErr != nil {
			logger.Errorf("[Integration] 订单读取失败, 本轮跳过(下轮重同步) orderId=%s: %v", o.OrderID, lookErr)
			continue
		}
		if existing != nil {
			order.ID = existing.ID
			order.BillID = existing.BillID // 理由同上：同步腿不认识账单线索，不许抹
			if e := s.orderRepo.Update(ctx, order); e != nil {
				logger.Errorf("[Integration] 订单更新失败: %v", e)
				continue
			}
		} else if e := s.orderRepo.Create(ctx, order); e != nil {
			logger.Errorf("[Integration] 订单创建失败: %v", e)
			continue
		}
		count++
	}

	if e := s.accountRepo.UpdateSyncTime(ctx, account.ID); e != nil {
		logger.Warnf("[Integration] 更新同步时间失败 accountID=%d: %v", account.ID, e)
	}
	if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 1, count, ""); e != nil {
		logger.Warnf("[Integration] 同步成功状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
	}

	return count, nil
}

// SyncProducts 同步商品
func (s *IntegrationService) SyncProducts(ctx context.Context, account *model.IntegrationAccount) (int, error) {
	switch account.Platform {
	case string(PlatformTaobao):
		return s.syncTaobaoProducts(ctx, account)
	case string(PlatformJD):
		return s.syncJDProducts(ctx, account)
	default:
		return 0, errors.New("不支持的平台")
	}
}

func (s *IntegrationService) syncTaobaoProducts(ctx context.Context, account *model.IntegrationAccount) (int, error) {
	client := NewTaobaoClient(account)

	syncLog := &model.SyncLog{
		Platform:  account.Platform,
		SyncType:  "product",
		Status:    0,
		StartTime: time.Now(),
	}
	if err := s.syncLogRepo.Create(ctx, syncLog); err != nil {
		return 0, err
	}

	apiURL := "https://gw.api.taobao.com/router/rest"
	params := url.Values{
		"app_key":   {client.appKey},
		"method":    {"taobao.items.seller.get"},
		"format":    {"json"},
		"v":         {"2.0"},
		"page":      {"1"},
		"page_size": {"100"},
		"fields":    {"num_iid,title,price,num,pic_url,cid,status"},
	}

	resp, err := client.httpClient.Get(apiURL + "?" + params.Encode())
	if err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}

	var result struct {
		ItemsSellerGetResponse struct {
			Items struct {
				Item []struct {
					NumIid string  `json:"num_iid"`
					Title  string  `json:"title"`
					Price  float64 `json:"price"`
					Num    int     `json:"num"`
					PicURL string  `json:"pic_url"`
					Cid    string  `json:"cid"`
					Status string  `json:"status"`
				} `json:"item"`
			} `json:"items"`
		} `json:"items_seller_get_response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}

	count := 0
	for _, item := range result.ItemsSellerGetResponse.Items.Item {
		imagesJSON, _ := json.Marshal([]string{item.PicURL})
		status := 1
		if item.Status != "onsale" {
			status = 0
		}

		product := &model.ExternalProduct{
			Platform:   account.Platform,
			ProductID:  item.NumIid,
			Name:       item.Title,
			Price:      yuanToFen(item.Price),
			Stock:      item.Num,
			CategoryID: item.Cid,
			Images:     string(imagesJSON),
			Status:     status,
		}

		existing, _ := s.productRepo.GetByProductID(ctx, account.Platform, item.NumIid)
		if existing != nil {
			product.ID = existing.ID
			if e := s.productRepo.Update(ctx, product); e != nil {
				logger.Errorf("[Integration] 商品更新失败: %v", e)
				continue
			}
		} else if e := s.productRepo.Create(ctx, product); e != nil {
			logger.Errorf("[Integration] 商品创建失败: %v", e)
			continue
		}
		count++
	}

	if e := s.accountRepo.UpdateSyncTime(ctx, account.ID); e != nil {
		logger.Warnf("[Integration] 更新同步时间失败 accountID=%d: %v", account.ID, e)
	}
	if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 1, count, ""); e != nil {
		logger.Warnf("[Integration] 同步成功状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
	}

	return count, nil
}

func (s *IntegrationService) syncJDProducts(ctx context.Context, account *model.IntegrationAccount) (int, error) {
	client := NewJDClient(account)

	syncLog := &model.SyncLog{
		Platform:  account.Platform,
		SyncType:  "product",
		Status:    0,
		StartTime: time.Now(),
	}
	if err := s.syncLogRepo.Create(ctx, syncLog); err != nil {
		return 0, err
	}

	apiURL := "https://api.jd.com/routerjson"
	params := url.Values{
		"app_key":   {client.appKey},
		"method":    {"jingdong.pop.ware.sku.list"},
		"format":    {"json"},
		"v":         {"2.0"},
		"page":      {"1"},
		"page_size": {"100"},
	}

	resp, err := client.httpClient.Get(apiURL + "?" + params.Encode())
	if err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}

	var result struct {
		SkuListResponse struct {
			Skus []struct {
				SkuId    string   `json:"sku_id"`
				Name     string   `json:"name"`
				Price    float64  `json:"price"`
				StockNum int      `json:"stock_num"`
				Category string   `json:"category"`
				Images   []string `json:"images"`
				Status   int      `json:"status"`
			} `json:"skus"`
			Total int `json:"total"`
		} `json:"jingdong_pop_ware_sku_list_response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 2, 0, err.Error()); e != nil {
			logger.Warnf("[Integration] 同步失败状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
		}
		return 0, err
	}

	count := 0
	for _, sku := range result.SkuListResponse.Skus {
		imagesJSON, _ := json.Marshal(sku.Images)

		product := &model.ExternalProduct{
			Platform:   account.Platform,
			ProductID:  sku.SkuId,
			Name:       sku.Name,
			Price:      yuanToFen(sku.Price),
			Stock:      sku.StockNum,
			CategoryID: sku.Category,
			Images:     string(imagesJSON),
			Status:     sku.Status,
		}

		existing, _ := s.productRepo.GetByProductID(ctx, account.Platform, sku.SkuId)
		if existing != nil {
			product.ID = existing.ID
			if e := s.productRepo.Update(ctx, product); e != nil {
				logger.Errorf("[Integration] 商品更新失败: %v", e)
				continue
			}
		} else if e := s.productRepo.Create(ctx, product); e != nil {
			logger.Errorf("[Integration] 商品创建失败: %v", e)
			continue
		}
		count++
	}

	if e := s.accountRepo.UpdateSyncTime(ctx, account.ID); e != nil {
		logger.Warnf("[Integration] 更新同步时间失败 accountID=%d: %v", account.ID, e)
	}
	if e := s.syncLogRepo.UpdateStatus(ctx, syncLog.ID, 1, count, ""); e != nil {
		logger.Warnf("[Integration] 同步成功状态落库失败 syncLogID=%d: %v", syncLog.ID, e)
	}

	return count, nil
}

// GetSyncLogs 获取同步日志
func (s *IntegrationService) GetSyncLogs(ctx context.Context, page, pageSize int) ([]*model.SyncLog, int64, error) {
	return s.syncLogRepo.GetAll(ctx, page, pageSize)
}

// GetExternalCustomers 获取外部客户列表
func (s *IntegrationService) GetExternalCustomers(ctx context.Context, platform string, page, pageSize int) ([]*model.ExternalCustomer, int64, error) {
	if platform != "" {
		return s.customerRepo.GetByPlatform(ctx, platform, page, pageSize)
	}
	return s.customerRepo.GetAll(ctx, page, pageSize)
}

// GetExternalOrders 获取外部订单列表
func (s *IntegrationService) GetExternalOrders(ctx context.Context, platform string, page, pageSize int) ([]*model.ExternalOrder, int64, error) {
	if platform != "" {
		return s.orderRepo.GetByPlatform(ctx, platform, page, pageSize)
	}
	return s.orderRepo.GetAll(ctx, page, pageSize)
}

// GetExternalOrdersByCustomer 按客户手机/姓名查询近期外部订单（客服 360 视图 / 答单上下文用）。
// 订单是外部电商同步进来的只读镜像，此处只查询、不写。
func (s *IntegrationService) GetExternalOrdersByCustomer(ctx context.Context, phone, name string) ([]*model.ExternalOrder, error) {
	return s.orderRepo.GetByCustomer(ctx, phone, name)
}

// UpsertOrderFromWebhook 处理电商订单状态推送（近实时刷新本地订单镜像），并接住载荷里那一笔钱。
//
// 这是"拉取同步(B)"之外的"事件推送(C)"补强：电商订单状态变更时主动推送，
// 本系统记录 WebhookEvent 并 upsert ExternalOrder，使客服看到的订单状态与电商一致（防漂移）。
// 订单镜像为只读，客服不创建/履约订单。
//
// T-P7-02 在这一条腿上做了两类事：**接回款**（G15 第③条：钱与账单之间原来没有任何一行数据连着）
// 和**修四个既有的静默坏法**。顺序是本函数最容易被人日后"顺手优化"掉的地方，所以逐段写明：
//
//	① 事件留痕（内容派生键，见 orderWebhookEventKey）；
//	② 读镜像 —— 读故障**必须**中止：老代码 `existing, _ :=` 把"库查不动"读成"第一次见这单"，
//	   于是走 Create（G15 第④条）；
//	③ 写镜像；
//	④ 回款腿 —— 排在镜像**之后**：一单先要存在，钱才谈得上挂在它身上，
//	   而这一腿可能拒（账单不存在/币种不符/流水号复用）。
//
// ④ 失败时本函数**回错误但保留镜像**：镜像比回款便宜（它是外部事实的副本，重推即可修），
// 而回款那一腿的错误全部是"载荷或数据坏了"，让渠道看见才有救。
// 两个判据因此都写进错误文案里：钱记没记上、镜像动没动 —— 后者决定重推是否安全。
func (s *IntegrationService) UpsertOrderFromWebhook(ctx context.Context, platform, orderID, status string, raw map[string]any) (*OrderWebhookResult, error) {
	if err := s.recordOrderWebhookEvent(ctx, platform, orderID, status, raw); err != nil {
		return nil, err
	}

	existing, err := s.orderRepo.GetByOrderID(ctx, platform, orderID)
	if err != nil {
		return nil, fmt.Errorf("读订单镜像失败（platform=%s order_id=%s，一行都没改，重推可修）：%w", platform, orderID, err)
	}

	// 解析先于写：它不碰库，且"这笔钱长什么样"要参与镜像上的 bill_id 那一格。
	in, present, perr := ParseOrderWebhookPayment(raw, platform, orderID)
	res := &OrderWebhookResult{PaymentPresent: present}

	created := existing == nil
	o := existing
	if created {
		o = &model.ExternalOrder{Platform: platform, OrderID: orderID}
	}
	// 只挡"往回"这一格，别的列照旧更新：镜像的价值在于它尽量新，
	// 而一次迟到的 created 把已付的单擦回待付是假事实（G15 第②条）。
	if !model.ExternalOrderStatusRegresses(o.Status, status) {
		o.Status = status
	}
	if raw != nil {
		if v, ok := raw["order_no"].(string); ok && v != "" {
			o.OrderNo = v
		}
		if v, ok := raw["user_phone"].(string); ok && v != "" {
			o.UserPhone = v
		}
		if v, ok := raw["user_name"].(string); ok && v != "" {
			o.UserName = v
		}
		if v, ok := webhookMoneyCent(raw["total_amount"]); ok {
			o.TotalAmount = v
		}
		if v, ok := webhookMoneyCent(raw["pay_amount"]); ok {
			o.PayAmount = v
		}
		if v, ok := raw["items"].(string); ok && v != "" {
			o.Items = v
		}
		if t, ok := parseWebhookTime(raw["order_time"]); ok {
			o.OrderTime = t
		} else if t, ok := parseWebhookTime(raw["pay_time"]); ok {
			o.OrderTime = t
		}
	}
	// 只在真的解析出账单号时写这一格：**不**把已有的值抹成空串。
	// 抹掉等于把"这单付过账"的来路线索洗掉，而后续那条不带 payment 的普通状态推送
	// 完全没有资格声明"这单与账单无关"。
	if present && perr == nil && in.BillID != "" {
		o.BillID = in.BillID
	}
	if err := s.saveOrderMirror(ctx, o, created); err != nil {
		return nil, fmt.Errorf("写订单镜像失败（platform=%s order_id=%s）：%w", platform, orderID, err)
	}

	if present {
		if err := s.bookOrderPayment(ctx, res, in, perr); err != nil {
			return res, err
		}
	}
	return res, nil
}

// recordOrderWebhookEvent 落一条事件留痕。
//
// 撞到 event_id 唯一键 ⇒ 同一条通知的第二次投递，**继续处理**而不是报错：
// 这一格只是账本，不是闸门（拿它当闸门会让"第一次写坏了、重推被挡在门外"变成死局）。
// 这里只按 SQLSTATE 23505 判重、不核索引名，与 bills/payments 那一族不同：
// 那两族认错 23505 会把一次该失败的写入吞成成功复用（吞的是钱），
// 而本表认错的最坏结果是"少留一行痕"，镜像与回款两腿都照常走。
// Processed 恒为 true 是**必须**的：webhook_events 上 processed=false 是恢复扫描器的待办队列，
// 订单事件由渠道自己重投，塞进那个队列只会让它按另一套契约被反复回放。
func (s *IntegrationService) recordOrderWebhookEvent(ctx context.Context, platform, orderID, status string, raw map[string]any) error {
	event := &model.WebhookEvent{
		Platform:  platform,
		EventID:   orderWebhookEventKey(platform, orderID, status, raw),
		EventType: "order.updated",
		RawData:   orderWebhookCanonicalText(raw),
		Processed: true,
	}
	if err := s.webhookEventRepo.Create(ctx, event); err != nil {
		if repository.IsDuplicateKeyErr(err) {
			return nil
		}
		return fmt.Errorf("webhook 事件留痕失败（订单镜像一行都没改，重推可修）：%w", err)
	}
	return nil
}

// saveOrderMirror 写镜像那一行。
//
// created 为假时直接 Save（整行覆盖：这一行本来就是我们从它出发改的）。
// 为真时撞唯一键不是失败而是**并发**：两条同号首次投递同时读到"没有这一行"，
// 后写的那条被 uq_external_orders_platform_order_id 挡下。判据是回读一次：
// 读得回来 ⇒ 那一行确实存在了，改走更新；读不回来（或读又出错）⇒ 不是并发，把 Create 的错误回出去。
func (s *IntegrationService) saveOrderMirror(ctx context.Context, o *model.ExternalOrder, created bool) error {
	if !created {
		return s.orderRepo.Update(ctx, o)
	}
	err := s.orderRepo.Create(ctx, o)
	if err == nil {
		return nil
	}
	back, gerr := s.orderRepo.GetByOrderID(ctx, o.Platform, o.OrderID)
	if gerr != nil || back == nil {
		return err
	}
	o.ID = back.ID
	return s.orderRepo.Update(ctx, o)
}

// bookOrderPayment 回款腿：把载荷里那一笔钱交给回款服务，并把三种"进不去"分开说清。
//
// 三条分支的共同点：**都回错误**，都不静默。少记一笔钱的代价是"应收挂着、客户说付过了"，
// 而对账时两边都有数据、只差我们这一行 —— 那种账只能在报错的当天查。
func (s *IntegrationService) bookOrderPayment(ctx context.Context, res *OrderWebhookResult, in RecordPaymentInput, perr error) error {
	if perr != nil {
		return fmt.Errorf("载荷报了钱而这一笔没被记下（订单镜像已写入，重推同一份载荷只补这一腿）：%w", perr)
	}
	if s.payments == nil || !s.payments.Available() {
		return fmt.Errorf("回款腿未装配，载荷里的这笔钱没有入账（订单镜像已写入；装配修好后重推同一份载荷即可补上）：%w", ErrPaymentServiceUnavailable)
	}
	receipt, err := s.payments.RecordPayment(ctx, in)
	if err != nil {
		return fmt.Errorf("回款腿拒了这笔钱（bill_id=%s channel_ref=%s，订单镜像已写入）：%w", in.BillID, in.ChannelRef, err)
	}
	res.Payment = receipt
	return nil
}

func parseWebhookTime(v any) (*time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return &t, true
	case *time.Time:
		return t, true
	case string:
		if t == "" {
			return nil, false
		}
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"} {
			if parsed, err := time.Parse(layout, t); err == nil {
				return &parsed, true
			}
		}
		return nil, false
	default:
		return nil, false
	}
}

// GetExternalProducts 获取外部商品列表
func (s *IntegrationService) GetExternalProducts(ctx context.Context, platform string, page, pageSize int) ([]*model.ExternalProduct, int64, error) {
	_ = platform
	return s.productRepo.GetAll(ctx, page, pageSize)
}

// TestConnection 测试对接账号连接是否正常
func (s *IntegrationService) TestConnection(ctx context.Context, account *model.IntegrationAccount) error {
	if account == nil {
		return errors.New("账号不能为空")
	}
	if account.APIKey == "" {
		return errors.New("API Key 不能为空")
	}
	if account.APISecret == "" {
		return errors.New("API Secret 不能为空")
	}
	if account.Status != 1 {
		return errors.New("账号已被禁用")
	}
	return nil
}
