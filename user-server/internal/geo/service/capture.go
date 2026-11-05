package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/utils/logger"
	baseservice "hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// captureLeadLimit 单条承接链接的每日留资上限，数据源是参数中心
// geo.capture_lead_daily_limit；缺省/非法一律回落编译期 100。
func captureLeadLimit(ctx context.Context) int {
	if ctx == nil {
		return CaptureLeadDailyLimit
	}
	n := baseservice.GlobalConfigParam().GetInt(ctx, "geo", "capture_lead_daily_limit", CaptureLeadDailyLimit)
	if n <= 0 {
		return CaptureLeadDailyLimit
	}
	return n
}

// captureAskLimit 单条承接链接的每日问答上限，数据源是参数中心
// geo.capture_ask_daily_limit；缺省/非法一律回落编译期 200。
func captureAskLimit(ctx context.Context) int {
	if ctx == nil {
		return CaptureAskDailyLimit
	}
	n := baseservice.GlobalConfigParam().GetInt(ctx, "geo", "capture_ask_daily_limit", CaptureAskDailyLimit)
	if n <= 0 {
		return CaptureAskDailyLimit
	}
	return n
}

const (
	// CaptureAskDailyLimit 单条承接链接每天最多回答的问题数（防 LLM 成本被打穿）。
	CaptureAskDailyLimit = 200
	// CaptureLeadDailyLimit 单条承接链接每天最多接收的线索数。
	CaptureLeadDailyLimit = 100

	captureQuestionMaxRunes = 500
	captureContactMaxRunes  = 120
	captureMessageMaxRunes  = 1000
)

// ShortLinkRef 装配层短链端口返回的引用（geo 不反向依赖 cards 域）。
type ShortLinkRef struct {
	ID   uint
	Code string
	URL  string
}

// ShortLinkPort 承接链接复用的短链能力，由 router 装配 cards.ShortLinkService。
type ShortLinkPort interface {
	CreateCaptureShortLink(ctx context.Context, code, targetURL, title string) (*ShortLinkRef, error)
	DeleteShortLink(ctx context.Context, id uint) error
}

// KnowledgeAskPort 公开 FAQ 问答端口，KBService 实现。
type KnowledgeAskPort interface {
	Ask(ctx context.Context, question string) (*dto.KBAskResponse, error)
}

// CaptureLinkInput 创建承接链接的入参。
type CaptureLinkInput struct {
	Code      string `json:"code"`
	Title     string `json:"title"`
	TargetURL string `json:"target_url"`
	Keyword   string `json:"keyword"`
	ArticleID uint   `json:"article_id"`
	ChainID   string `json:"chain_id"`
}

// CaptureLinkView 管理端视图：落地页地址与首响统计。
type CaptureLinkView struct {
	model.GeoCaptureLink
	LandingPath string                          `json:"landing_path"`
	LandingURL  string                          `json:"landing_url"`
	ShortURL    string                          `json:"short_url"`
	Responses   repository.CaptureResponseStats `json:"responses"`
	Connected   bool                            `json:"connected"`
}

// CaptureLandingView 公开落地页数据。
type CaptureLandingView struct {
	Code      string `json:"code"`
	Title     string `json:"title"`
	Keyword   string `json:"keyword"`
	TargetURL string `json:"target_url"`
}

// CaptureService 承接闭环：链接 → 落地页 → FAQ/留资 → 线索 → 首响。
// CaptureLinkQuery 仓储查询 DTO 的对外别名：controller 只依赖 service 层。
type CaptureLinkQuery = repository.CaptureLinkQuery

type CaptureService struct {
	links      repository.GeoCaptureLinkRepository
	kb         KnowledgeAskPort
	lead       LeadCapturePort
	shorts     ShortLinkPort
	publicBase string
	now        func() time.Time
}

func NewCaptureService(links repository.GeoCaptureLinkRepository, kb KnowledgeAskPort, lead LeadCapturePort, shorts ShortLinkPort, publicBase string) *CaptureService {
	return &CaptureService{
		links:      links,
		kb:         kb,
		lead:       lead,
		shorts:     shorts,
		publicBase: strings.TrimRight(strings.TrimSpace(publicBase), "/"),
		now:        time.Now,
	}
}

func captureCode() (string, error) {
	const alphabet = "abcdefghijkmnpqrstuvwxyz23456789"
	buf := make([]byte, 10)
	for i := range buf {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		buf[i] = alphabet[n.Int64()]
	}
	return string(buf), nil
}

func validCaptureCode(code string) bool {
	if len(code) < 4 || len(code) > 32 {
		return false
	}
	for _, ch := range code {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			continue
		}
		return false
	}
	return true
}

func normalizeCaptureTarget(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("目标链接不能为空")
	}
	if utf8.RuneCountInString(raw) > 2048 {
		return "", fmt.Errorf("目标链接过长")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("目标链接必须是 http(s) 地址")
	}
	return u.String(), nil
}

func (s *CaptureService) landingPath(code string) string {
	return "/api/public/geo/l/" + code
}

func (s *CaptureService) landingURL(code string) string {
	if s.publicBase == "" {
		return s.landingPath(code)
	}
	return s.publicBase + s.landingPath(code)
}

// shortURL 短码在装配层固定为 "g"+承接码，详情/列表据此还原，避免额外一次短链查询。
func (s *CaptureService) shortURL(link *model.GeoCaptureLink) string {
	if link.ShortLinkID == 0 || s.publicBase == "" || len("g"+link.Code) > 20 {
		return ""
	}
	return s.publicBase + "/api/s/g" + link.Code
}

func (s *CaptureService) baseView(link *model.GeoCaptureLink) *CaptureLinkView {
	return &CaptureLinkView{
		GeoCaptureLink: *link,
		LandingPath:    s.landingPath(link.Code),
		LandingURL:     s.landingURL(link.Code),
		ShortURL:       s.shortURL(link),
	}
}

// Create 创建承接链接：先建短链（可选），再落承接记录；落库失败回收短链。
func (s *CaptureService) Create(ctx context.Context, in CaptureLinkInput, createdBy string) (*CaptureLinkView, error) {
	target, err := normalizeCaptureTarget(in.TargetURL)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(in.Title)
	if utf8.RuneCountInString(title) > 255 {
		return nil, fmt.Errorf("标题过长（最多 255 字）")
	}
	keyword := strings.TrimSpace(in.Keyword)
	if utf8.RuneCountInString(keyword) > 255 {
		return nil, fmt.Errorf("关键词过长（最多 255 字）")
	}
	if title == "" {
		title = keyword
	}
	if title == "" {
		title = "在线咨询"
	}
	chainID := strings.TrimSpace(in.ChainID)

	code := strings.TrimSpace(in.Code)
	if code == "" {
		code, err = captureCode()
		if err != nil {
			return nil, fmt.Errorf("生成承接码失败: %w", err)
		}
	} else if !validCaptureCode(code) {
		return nil, fmt.Errorf("承接码只能包含字母/数字/-/_，长度 4-32")
	}

	sourceID := chainID
	if sourceID == "" {
		sourceID = "geo_link:" + code
	}
	link := &model.GeoCaptureLink{
		Code:      code,
		ArticleID: in.ArticleID,
		ChainID:   chainID,
		SourceID:  sourceID,
		Keyword:   keyword,
		Title:     title,
		TargetURL: target,
		CreatedBy: createdBy,
	}

	// code 是全局唯一索引，Delete 又是软删除：同码重建时旧行仍占着索引，
	// 直接 INSERT 会抛 23505。删过就原地复活，并把旧计数清空。
	revived := false
	existing, err := s.links.GetByCodeUnscoped(ctx, code)
	if err == nil {
		if !existing.DeletedAt.Valid {
			return nil, fmt.Errorf("承接码 %s 已被使用，请换一个", code)
		}
		link.ID = existing.ID
		revived = true
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var shortRef *ShortLinkRef
	if s.shorts != nil && strings.HasPrefix(s.publicBase, "https://") && len("g"+code) <= 20 {
		shortCode := "g" + code
		shortRef, err = s.shorts.CreateCaptureShortLink(ctx, shortCode, s.landingURL(code), title)
		if err != nil {
			logger.Warnf("[GEO capture] 短链创建失败 code=%s: %v", code, err)
			shortRef = nil
		} else if shortRef != nil {
			link.ShortLinkID = shortRef.ID
		}
	}

	if revived {
		if err := s.links.Restore(ctx, link); err != nil {
			if shortRef != nil {
				_ = s.shorts.DeleteShortLink(ctx, shortRef.ID)
			}
			return nil, err
		}
		return s.baseView(link), nil
	}

	if err := s.links.Create(ctx, link); err != nil {
		if shortRef != nil {
			_ = s.shorts.DeleteShortLink(ctx, shortRef.ID)
		}
		return nil, err
	}

	return s.baseView(link), nil
}

func (s *CaptureService) List(ctx context.Context, q repository.CaptureLinkQuery) ([]*CaptureLinkView, int64, error) {
	links, total, err := s.links.List(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	views := make([]*CaptureLinkView, 0, len(links))
	for _, link := range links {
		views = append(views, s.baseView(link))
	}
	return views, total, nil
}

// Get 详情带首响统计。Matched=0 时 connected=false，页面显示“未接入”。
func (s *CaptureService) Get(ctx context.Context, id uint) (*CaptureLinkView, error) {
	link, err := s.links.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	stats, err := s.links.ResponseStats(ctx, link.SourceID)
	if err != nil {
		return nil, err
	}
	view := s.baseView(link)
	view.Responses = stats
	view.Connected = stats.Matched > 0
	return view, nil
}

func (s *CaptureService) Delete(ctx context.Context, id uint) error {
	link, err := s.links.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.links.Delete(ctx, id); err != nil {
		return err
	}
	if s.shorts != nil && link.ShortLinkID > 0 {
		if err := s.shorts.DeleteShortLink(ctx, link.ShortLinkID); err != nil {
			logger.Warnf("[GEO capture] 短链删除失败 link_id=%d short_id=%d: %v", id, link.ShortLinkID, err)
		}
	}
	return nil
}

// Landing 公开落地页读取；countClick=true 时累加点击（页面渲染路径）。
func (s *CaptureService) Landing(ctx context.Context, code string, countClick bool) (*model.GeoCaptureLink, error) {
	link, err := s.links.GetByCode(ctx, strings.TrimSpace(code))
	if err != nil {
		return nil, err
	}
	if countClick {
		if err := s.links.IncrementClicks(ctx, link.ID); err != nil {
			logger.Warnf("[GEO capture] 点击计数失败 link_id=%d: %v", link.ID, err)
		} else {
			link.Clicks++
		}
	}
	return link, nil
}

// Ask 公开 FAQ：只答知识库范围，按链接做每日配额。
func (s *CaptureService) Ask(ctx context.Context, code, question string) (*dto.KBAskResponse, error) {
	question = strings.TrimSpace(question)
	if utf8.RuneCountInString(question) < 2 {
		return nil, fmt.Errorf("问题太短")
	}
	if utf8.RuneCountInString(question) > captureQuestionMaxRunes {
		return nil, fmt.Errorf("问题最多 %d 字", captureQuestionMaxRunes)
	}
	if s.kb == nil {
		return nil, fmt.Errorf("知识库问答未接入")
	}
	link, err := s.links.GetByCode(ctx, strings.TrimSpace(code))
	if err != nil {
		return nil, err
	}
	ok, err := s.links.ReserveAsk(ctx, link.ID, businessDay(s.now()), captureAskLimit(ctx))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("今日问答次数已用完，请明天再来或直接留言")
	}
	return s.kb.Ask(ctx, question)
}

// SubmitLead 公开留资：先占配额，落线索失败则回滚计数。
func (s *CaptureService) SubmitLead(ctx context.Context, code, contact, contactType, message string) (string, error) {
	contact = strings.TrimSpace(contact)
	if utf8.RuneCountInString(contact) < 3 {
		return "", fmt.Errorf("联系方式太短")
	}
	if utf8.RuneCountInString(contact) > captureContactMaxRunes {
		return "", fmt.Errorf("联系方式最多 %d 字", captureContactMaxRunes)
	}
	contactType = strings.TrimSpace(contactType)
	switch contactType {
	case "phone", "email", "wechat", "other":
	case "":
		contactType = "other"
	default:
		return "", fmt.Errorf("不支持的联系方式类型")
	}
	message = strings.TrimSpace(message)
	if utf8.RuneCountInString(message) > captureMessageMaxRunes {
		return "", fmt.Errorf("留言最多 %d 字", captureMessageMaxRunes)
	}
	if s.lead == nil {
		return "", fmt.Errorf("线索通道未接入")
	}

	link, err := s.links.GetByCode(ctx, strings.TrimSpace(code))
	if err != nil {
		return "", err
	}
	day := businessDay(s.now())
	ok, err := s.links.ReserveLead(ctx, link.ID, day, captureLeadLimit(ctx))
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("今日留言名额已满，请明天再来")
	}

	intent := strings.TrimSpace(link.Keyword)
	if message != "" {
		if intent != "" {
			intent += "｜"
		}
		intent += message
	}
	if utf8.RuneCountInString(intent) > 500 {
		intent = string([]rune(intent)[:500])
	}
	clueID, err := s.lead.CaptureLead(ctx, contact, contactType, link.SourceID, intent)
	if err != nil {
		if relErr := s.links.ReleaseLead(ctx, link.ID, day); relErr != nil {
			logger.Warnf("[GEO capture] 留资失败后回滚配额失败 link_id=%d: %v", link.ID, relErr)
		}
		return "", err
	}
	return clueID, nil
}

func businessDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
