package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// WeComAccountHealthService 企微账号健康度服务
type WeComAccountHealthService struct {
	accountRepo *repository.WeComAccountRepository
	healthRepo  *repository.WeComAccountHealthRepository
}

// 常量定义
const (
	WeComHealthScoreNormal   = 100
	WeComHealthScoreWarning  = 70
	WeComHealthScoreCritical = 40
	WeComHealthScoreBanned   = 0

	WeComRiskNormal   = "normal"
	WeComRiskWarning  = "warning"
	WeComRiskCritical = "critical"
	WeComRiskBanned   = "banned"

	WeComLoginOnline  = "online"
	WeComLoginOffline = "offline"
	WeComLoginBanned  = "banned"

	WeComDefaultWeight = 100

	// WeComErrorRateDegradeThreshold / WeComQuotaDegradeThreshold 为 fallback 默认值（DB 驱动）
	// 运行时通过 GlobalConfigParam() 按 group=wecom 读取 DB 参数：
	//   wecom.error_rate_degrade → WeComErrorRateDegradeThreshold (fallback)
	//   wecom.quota_degrade → WeComQuotaDegradeThreshold (fallback)
	WeComErrorRateDegradeThreshold = 0.3
	WeComQuotaDegradeThreshold     = 0.9
)

func runtimeWeComQuotaDegrade(ctx context.Context) float64 {
	return GlobalConfigParam().GetFloat(ctx, "wecom", "quota_degrade", WeComQuotaDegradeThreshold)
}

// 错误定义
var (
	ErrWeComAccountNotFound  = errors.New("account not found")
	ErrWeComInvalidAccountID = errors.New("invalid account_id")
	ErrWeComQuotaExceeded    = errors.New("daily quota exceeded")
	ErrWeComAccountBanned    = errors.New("account is banned")
	ErrWeComAccountOffline   = errors.New("account is offline")
	ErrWeComHealthNotFound   = errors.New("health record not found")
)

// NewWeComAccountHealthService 创建账号健康度服务
func NewWeComAccountHealthService(db *gorm.DB) *WeComAccountHealthService {
	accountRepo := repository.NewWeComAccountRepository()
	healthRepo := repository.NewWeComAccountHealthRepository()
	if db != nil {
		accountRepo.SetDB(context.Background(), db)
		healthRepo.SetDB(context.Background(), db)
	}
	return &WeComAccountHealthService{
		accountRepo: accountRepo,
		healthRepo:  healthRepo,
	}
}

// ReportHealthRequest 上报健康度
type ReportHealthRequest struct {
	AccountID   uint           `json:"account_id"`
	Platform    string         `json:"platform"`
	LoginState  string         `json:"login_state"`
	QuotaUsed   int            `json:"quota_used"`
	QuotaTotal  int            `json:"quota_total"`
	SuccessRate float64        `json:"success_rate"`
	ErrorCount  int            `json:"error_count"`
	LastError   string         `json:"last_error"`
	Metrics     map[string]any `json:"metrics"`
}

// ReportHealth 上报账号健康度，自动计算健康分与风险等级
func (s *WeComAccountHealthService) ReportHealth(ctx context.Context, req *ReportHealthRequest) (*model.WeComAccountHealth, error) {
	if s.healthRepo == nil {
		return nil, fmt.Errorf("db is nil")
	}
	if req.AccountID == 0 {
		return nil, ErrWeComInvalidAccountID
	}
	platform := req.Platform
	if platform == "" {
		platform = "wecom"
	}

	quotaRate := 0.0
	if req.QuotaTotal > 0 {
		quotaRate = float64(req.QuotaUsed) / float64(req.QuotaTotal)
	}

	score := computeHealthScore(req.LoginState, quotaRate, req.SuccessRate, req.ErrorCount)

	risk := computeRiskLevel(score, req.LoginState)

	now := time.Now()
	rec := &model.WeComAccountHealth{

		AccountID:      req.AccountID,
		Platform:       platform,
		HealthScore:    score,
		RiskLevel:      risk,
		LoginState:     req.LoginState,
		QuotaUsed:      req.QuotaUsed,
		QuotaTotal:     req.QuotaTotal,
		QuotaUsageRate: quotaRate * 100,
		SuccessRate:    req.SuccessRate,
		ErrorCount:     req.ErrorCount,
		LastError:      req.LastError,
		Metrics:        model.JSONMap(req.Metrics),
		ReportedAt:     now,
	}
	if rec.Metrics == nil {
		rec.Metrics = model.JSONMap{}
	}
	if err := s.healthRepo.Create(ctx, rec); err != nil {
		return nil, err
	}

	s.syncAccountState(ctx, req.AccountID, req.LoginState, score, risk, quotaRate, req.LastError)

	return rec, nil
}

// GetLatestHealth 获取最新健康度
func (s *WeComAccountHealthService) GetLatestHealth(ctx context.Context, accountID uint) (*model.WeComAccountHealth, error) {
	if s.healthRepo == nil {
		return nil, fmt.Errorf("db is nil")
	}
	rec, err := s.healthRepo.GetLatestByAccountID(ctx, accountID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWeComHealthNotFound
		}
		return nil, err
	}
	return rec, nil
}

// ListHealthHistory 列出健康度历史
func (s *WeComAccountHealthService) ListHealthHistory(ctx context.Context, accountID uint, page, pageSize int) ([]model.WeComAccountHealth, int64, error) {
	if s.healthRepo == nil {
		return nil, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	return s.healthRepo.ListByAccountIDPaged(ctx, accountID, page, pageSize)
}

// GetRiskAccounts 列出风险账号
func (s *WeComAccountHealthService) GetRiskAccounts(ctx context.Context) ([]model.WeComAccount, error) {
	if s.accountRepo == nil {
		return nil, nil
	}
	return s.accountRepo.FindByRiskLevels(ctx,
		[]string{WeComRiskWarning, WeComRiskCritical, WeComRiskBanned})
}

// SelectHealthyAccount 路由选号 - 选出最佳账号
//
// 三步降级策略（避免把"无健康账号"误报为业务错误）：
//  1. 优先返回健康账号（risk IN normal/warning 且 login_state NOT IN banned/offline）
//  2. 若无健康账号，降级返回任何已启用、未封禁的账号（status=1, login_state!=banned）
//  3. 完全没有可用账号才返回 ErrWeComAccountNotFound（交由 controller 做最终降级）
//
// 返回值签名不变（*model.WeComAccount, error），controller 层只需区分 error 为 nil 还是 not nil。
func (s *WeComAccountHealthService) SelectHealthyAccount(ctx context.Context) (*model.WeComAccount, error) {
	if s.accountRepo == nil {
		return nil, fmt.Errorf("db is nil")
	}

	accounts, err := s.accountRepo.FindHealthyAccounts(ctx,
		[]string{WeComRiskNormal, WeComRiskWarning},
		[]string{WeComLoginBanned, WeComLoginOffline})
	if err != nil {
		return nil, err
	}

	if len(accounts) == 0 {

		active, err := s.accountRepo.FindActiveAccounts(ctx)
		if err != nil {
			return nil, err
		}
		if len(active) == 0 {

			return nil, ErrWeComAccountNotFound
		}

		return &active[0], nil
	}

	sort.SliceStable(accounts, func(i, j int) bool {
		scoreI := accountHealthFromModel(&accounts[i])
		scoreJ := accountHealthFromModel(&accounts[j])
		if scoreI != scoreJ {
			return scoreI > scoreJ
		}
		if accounts[i].Weight != accounts[j].Weight {
			return accounts[i].Weight > accounts[j].Weight
		}
		rateI := 1.0
		if accounts[i].DailyMsgQuota > 0 {
			rateI = float64(accounts[i].DailyMsgUsed) / float64(accounts[i].DailyMsgQuota)
		}
		rateJ := 1.0
		if accounts[j].DailyMsgQuota > 0 {
			rateJ = float64(accounts[j].DailyMsgUsed) / float64(accounts[j].DailyMsgQuota)
		}
		return rateI < rateJ
	})
	best := accounts[0]
	return &best, nil
}

// ConsumeQuota 消耗配额
//
// W-7 原子化：原「GetByID 读 used → 内存判断 → UpdateFields 写回 used+count」存在
// 读改写竞态（并发扣减互相覆盖导致超发）。现改为单条条件 UPDATE 原子校验+扣减
// （见 repository.ConsumeQuotaAtomic）；预检保留用于给出精确的错误语义
// （封禁/禁用/不存在），最终以 UPDATE 的 WHERE 条件为准，未命中视为配额不足。
func (s *WeComAccountHealthService) ConsumeQuota(ctx context.Context, accountID uint, count int) error {
	if s.accountRepo == nil {
		return nil
	}
	if count <= 0 {
		return nil
	}
	acc, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	if acc.Status != 1 {
		return ErrWeComAccountBanned
	}
	if acc.LoginState == WeComLoginBanned {
		return ErrWeComAccountBanned
	}
	ok, err := s.accountRepo.ConsumeQuotaAtomic(ctx, accountID, count)
	if err != nil {
		return err
	}
	if !ok {
		return ErrWeComQuotaExceeded
	}
	return nil
}

// ResetDailyQuota 重置日配额（每日凌晨）
func (s *WeComAccountHealthService) ResetDailyQuota(ctx context.Context) (int64, error) {
	if s.accountRepo == nil {
		return 0, nil
	}
	now := time.Now()
	return s.accountRepo.UpdateAllFields(ctx, map[string]any{
		"daily_msg_used": 0,
		"quota_reset_at": now,
	})
}

func (s *WeComAccountHealthService) ResetAllDailyQuotas(ctx context.Context) (int64, error) {
	return s.ResetDailyQuota(ctx)
}

// AccountHealthSummary 账号健康概览
type AccountHealthSummary struct {
	TotalAccounts int                         `json:"total_accounts"`
	OnlineCount   int                         `json:"online_count"`
	OfflineCount  int                         `json:"offline_count"`
	BannedCount   int                         `json:"banned_count"`
	WarningCount  int                         `json:"warning_count"`
	CriticalCount int                         `json:"critical_count"`
	AvgScore      float64                     `json:"avg_score"`
	TotalQuota    int                         `json:"total_quota"`
	TotalUsed     int                         `json:"total_used"`
	Accounts      []AccountHealthSummaryEntry `json:"accounts"`
	RiskAccounts  []model.WeComAccount        `json:"risk_accounts"`
}

// AccountHealthSummaryEntry 单账号健康概览
type AccountHealthSummaryEntry struct {
	AccountID      uint    `json:"account_id"`
	CorpID         string  `json:"corp_id"`
	LoginState     string  `json:"login_state"`
	RiskLevel      string  `json:"risk_level"`
	HealthScore    int     `json:"health_score"`
	QuotaUsageRate float64 `json:"quota_usage_rate"`
	SuccessRate    float64 `json:"success_rate"`
	TotalSent      int64   `json:"total_sent"`
	TotalReceived  int64   `json:"total_received"`
	ErrorCount     int     `json:"error_count"`
}

// GetHealthSummary 汇总账号健康度
func (s *WeComAccountHealthService) GetHealthSummary(ctx context.Context) (*AccountHealthSummary, error) {
	if s.accountRepo == nil {
		return nil, fmt.Errorf("db is nil")
	}
	accounts, err := s.accountRepo.ListAllOrderByIDDesc(ctx)
	if err != nil {
		return nil, err
	}

	summary := &AccountHealthSummary{
		TotalAccounts: len(accounts),
	}
	if len(accounts) == 0 {
		return summary, nil
	}
	scoreSum := 0
	for _, a := range accounts {
		switch a.LoginState {
		case WeComLoginOnline:
			summary.OnlineCount++
		case WeComLoginBanned:
			summary.BannedCount++
		default:
			summary.OfflineCount++
		}
		switch a.RiskLevel {
		case WeComRiskWarning:
			summary.WarningCount++
		case WeComRiskCritical:
			summary.CriticalCount++
		case WeComRiskBanned:
			summary.BannedCount++
		}
		summary.TotalQuota += a.DailyMsgQuota
		summary.TotalUsed += a.DailyMsgUsed
		scoreSum += a.Weight
		quotaRate := 0.0
		if a.DailyMsgQuota > 0 {
			quotaRate = float64(a.DailyMsgUsed) / float64(a.DailyMsgQuota)
		}
		summary.Accounts = append(summary.Accounts, AccountHealthSummaryEntry{
			AccountID:      a.ID,
			CorpID:         a.CorpID,
			LoginState:     a.LoginState,
			RiskLevel:      a.RiskLevel,
			HealthScore:    a.Weight,
			QuotaUsageRate: quotaRate,
			TotalSent:      a.TotalSent,
			TotalReceived:  a.TotalReceived,
			ErrorCount:     a.ErrorCount,
		})
	}
	summary.AvgScore = float64(scoreSum) / float64(len(accounts))

	riskAccounts, _ := s.GetRiskAccounts(ctx)
	summary.RiskAccounts = riskAccounts
	return summary, nil
}

func computeHealthScore(loginState string, quotaRate, successRate float64, errorCount int) int {
	score := 100
	quotaDegrade := runtimeWeComQuotaDegrade(context.Background())
	if loginState == WeComLoginBanned {
		return WeComHealthScoreBanned
	}
	if loginState == WeComLoginOffline {
		score -= 50
	}
	if quotaRate > 0.95 {
		score -= 25
	} else if quotaRate > quotaDegrade {
		score -= 15
	} else if quotaRate > 0.7 {
		score -= 5
	}
	if successRate < 50 {
		score -= 30
	} else if successRate < 80 {
		score -= 15
	} else if successRate < 95 {
		score -= 5
	}
	if errorCount > 50 {
		score -= 35
	} else if errorCount > 20 {
		score -= 15
	} else if errorCount > 5 {
		score -= 5
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

func computeRiskLevel(score int, loginState string) string {
	if loginState == WeComLoginBanned {
		return WeComRiskBanned
	}
	switch {
	case score >= WeComHealthScoreNormal:
		return WeComRiskNormal
	case score >= WeComHealthScoreWarning:
		return WeComRiskWarning
	case score >= WeComHealthScoreCritical:
		return WeComRiskCritical
	default:
		return WeComRiskCritical
	}
}

func (s *WeComAccountHealthService) syncAccountState(ctx context.Context, accountID uint, loginState string, score int, risk string, quotaRate float64, lastErr string) {
	if s.accountRepo == nil {
		return
	}
	updates := map[string]any{
		"login_state":    loginState,
		"risk_level":     risk,
		"weight":         score,
		"last_active_at": time.Now(),
	}
	if lastErr != "" {
		updates["last_error_at"] = time.Now()
		updates["last_error_msg"] = lastErr
		updates["error_count"] = gorm.Expr("error_count + 1")
	}
	if quotaRate > runtimeWeComQuotaDegrade(ctx) && risk == WeComRiskNormal {
		updates["risk_level"] = WeComRiskWarning
	}
	_ = s.accountRepo.UpdateFields(ctx, accountID, updates)
}

func accountHealthFromModel(a *model.WeComAccount) int {
	if a == nil {
		return 0
	}
	return a.Weight
}

var (
	wecomHealthOnce     sync.Once
	wecomHealthInstance *WeComAccountHealthService
)

// GetWeComAccountHealthService 获取账号健康度服务
//
// 当全局实例尚未初始化时（如：单元测试、嵌入式使用），自动以 nil db 兜底构造一个实例
// 避免 panic。生产路径仍应在启动时调用 InitWeComAccountHealthService(db) 注入真实 db。
func GetWeComAccountHealthService() *WeComAccountHealthService {
	if wecomHealthInstance == nil {
		wecomHealthInstance = &WeComAccountHealthService{}
	}
	return wecomHealthInstance
}

// InitWeComAccountHealthService 初始化账号健康度服务
func InitWeComAccountHealthService(db *gorm.DB) *WeComAccountHealthService {
	wecomHealthOnce.Do(func() {
		wecomHealthInstance = NewWeComAccountHealthService(db)
	})
	return wecomHealthInstance
}
