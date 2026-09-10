package service

import (
	"context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

type ClueService struct {
	repo repository.ClueRepository
}

func NewClueService() *ClueService {
	return &ClueService{repo: repository.NewClueRepository()}
}

func (s *ClueService) Register(ctx context.Context, clue model.Clue) (*model.Clue, error) {
	if err := s.repo.Create(ctx, &clue); err != nil {
		return nil, err
	}
	return &clue, nil
}

func (s *ClueService) GetClue(ctx context.Context, id string) (*model.Clue, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *ClueService) GetClueList(ctx context.Context, page int, limit int) ([]*model.Clue, int64, error) {
	return s.repo.GetClueList(ctx, page, limit)
}

func (s *ClueService) DeleteClue(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}
func (s *ClueService) GetRecentClueList(ctx context.Context) ([]*model.Clue, error) {
	return s.repo.GetRecentClueList(ctx)
}

func (s *ClueService) GetClueStatistics(ctx context.Context) ([]map[string]any, error) {
	return s.repo.GetClueStatistics(ctx)
}
func (s *ClueService) BatchSaveClue(ctx context.Context, clueList []*model.Clue) error {
	for _, clue := range clueList {
		if _, err := s.Register(ctx, *clue); err != nil {
			return err
		}
	}
	return nil
}

func (s *ClueService) GetClueAllList(ctx context.Context, clueType int64) ([]*model.Clue, int64, error) {
	return s.repo.GetClueAllList(ctx, clueType)
}

// GetWhatsappClues 取全部 WhatsApp 线索（type IN (5,7)，兼容历史错误类型）
func (s *ClueService) GetWhatsappClues(ctx context.Context) ([]*model.Clue, int64, error) {
	return s.repo.GetWhatsappClues(ctx)
}

func (s *ClueService) BatchImportClues(ctx context.Context, clues []*model.Clue) (successCount, skipCount int64, err error) {
	return s.repo.BatchCreateWithDedup(ctx, clues)
}

// ClueType 线索类型
type ClueType struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

const (
	ClueTypeQQ         int64 = 1
	ClueTypeWeChat     int64 = 2
	ClueTypePhone      int64 = 3
	ClueTypeTelegram   int64 = 4
	ClueTypeWhatsapp   int64 = 5
	ClueTypeTwitter    int64 = 6
	ClueTypeWeCom      int64 = 7
	ClueTypeLeadMining int64 = 8
	// ClueTypeGeoCapture GEO 决策链捕获线索（v3）：与 LLM 挖掘(8)区分，
	// SourceID 存思维链 chain_id 供转化归因回写
	ClueTypeGeoCapture  int64 = 9
	ClueTypeDouyin      int64 = 9
	ClueTypeKuaishou    int64 = 10
	ClueTypeXiaohongshu int64 = 11
	ClueTypeXianyu      int64 = 12
	ClueTypeFeishu      int64 = 13
	ClueTypeTikTok      int64 = 14
	ClueTypeWebWidget   int64 = 15
	ClueTypeEmail       int64 = 16
	ClueTypeSMS         int64 = 17
	ClueTypeCustom      int64 = 99
)

// IsWhatsappType 判断是否为 WhatsApp 线索类型（兼容历史错误 type=7）
func IsWhatsappType(t int64) bool {
	return t == ClueTypeWhatsapp || t == 7
}

// PlatformToClueType 平台渠道名 → 线索类型 ID 映射
func PlatformToClueType(platform string) int64 {
	switch platform {
	case "qq":
		return ClueTypeQQ
	case "wechat", "wx":
		return ClueTypeWeChat
	case "phone", "tel":
		return ClueTypePhone
	case "telegram", "tg":
		return ClueTypeTelegram
	case "whatsapp", "wa":
		return ClueTypeWhatsapp
	case "twitter", "x":
		return ClueTypeTwitter
	case "wecom", "wechat_work":
		return ClueTypeWeCom
	case "douyin":
		return ClueTypeDouyin
	case "kuaishou":
		return ClueTypeKuaishou
	case "xiaohongshu", "xhs":
		return ClueTypeXiaohongshu
	case "xianyu":
		return ClueTypeXianyu
	case "feishu", "lark":
		return ClueTypeFeishu
	case "tiktok":
		return ClueTypeTikTok
	case "web", "web_embed", "widget":
		return ClueTypeWebWidget
	case "email", "mail":
		return ClueTypeEmail
	case "sms":
		return ClueTypeSMS
	case "lead_mining":
		return ClueTypeLeadMining
	default:
		return ClueTypeCustom
	}
}

var defaultClueTypes = []ClueType{
	{Value: "1", Label: "QQ"},
	{Value: "2", Label: "微信"},
	{Value: "3", Label: "电话"},
	{Value: "4", Label: "Telegram"},
	{Value: "5", Label: "WhatsApp"},
	{Value: "6", Label: "Twitter/X"},
	{Value: "7", Label: "企业微信"},
	{Value: "8", Label: "智能线索发掘"},
	{Value: "9", Label: "抖音"},
	{Value: "10", Label: "快手"},
	{Value: "11", Label: "小红书"},
	{Value: "12", Label: "闲鱼"},
	{Value: "13", Label: "飞书"},
	{Value: "14", Label: "TikTok"},
	{Value: "15", Label: "网页组件"},
	{Value: "16", Label: "邮件"},
	{Value: "17", Label: "短信"},
	{Value: "99", Label: "自定义"},
}

// GetClueTypes 获取线索类型列表
func (s *ClueService) GetClueTypes(ctx context.Context) ([]ClueType, error) {
	return defaultClueTypes, nil
}
