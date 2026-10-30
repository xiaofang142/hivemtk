package service

import (
	"context"
	"errors"
	"strings"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// ClueService 线索域业务服务。
//
// 依赖在装配点注入：repo 由 router/app 用显式连接构造，
// scoreSvc 可为 nil（未注入时导入后不补算评分）。
type ClueService struct {
	repo     repository.ClueRepository
	scoreSvc *ClueScoreService
}

func NewClueService(repo repository.ClueRepository, scoreSvc *ClueScoreService) *ClueService {
	return &ClueService{repo: repo, scoreSvc: scoreSvc}
}

// NewClueServiceWithDB 用给定连接构造线索服务（仓储在这一层构造）。
//
// 为什么要有它：线索仓储属于 repository 包，**controller 层不许 import repository**
// （depguard 的 controller-layer 清单）。以前控制器为了兜底「没注入线索服务」
// 就在自己身上 new 仓储 —— 那是五层架构的破口（Router→Controller→Repository 跳层）。
// 兜底逻辑挪到这一层之后，控制器只认「注入好的服务」，缺依赖就在装配点补。
func NewClueServiceDefault(scoreSvc *ClueScoreService) *ClueService {
	return NewClueService(repository.NewClueRepositoryDefault(), scoreSvc)
}

// Register 建线索。(type, account) 去重是业务判据，放在这里：
// 仓储只负责写入，不再在 SQL 层偷偷加一条查询。
//
// 空 account 不参与去重：空串会让同类型下所有「没有账号」的线索互相判重，
// 第一条之后全部被拒（邮件列表等链路本来就存在空账号的线索）。
func (s *ClueService) Register(ctx context.Context, clue model.Clue) (*model.Clue, error) {
	if strings.TrimSpace(clue.Account) != "" {
		exists, err := s.repo.ExistsByTypeAndAccount(ctx, clue.Type, clue.Account)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, errors.New("重复数据")
		}
	}
	if err := s.repo.Create(ctx, &clue); err != nil {
		return nil, err
	}
	return &clue, nil
}

// ListWithQuery 带筛选条件的分页查询：筛选在 DB 侧完成，total 与筛选同口径。
// 页面上的「线索类型/关键字/验证状态」必须走这里 —— 前端自己过滤只能过滤已加载的那一页。
func (s *ClueService) ListWithQuery(ctx context.Context, req dto.GetClueRequest) ([]*model.Clue, int64, error) {
	q := repository.ClueQuery{
		Keyword:   req.Keyword,
		Type:      req.Type,
		IsVerify:  req.IsVerify,
		StartUnix: req.Start,
		EndUnix:   req.End,
	}
	return s.repo.ListWithQuery(ctx, q, req.Page, req.PageSize)
}

func (s *ClueService) DeleteClue(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

func (s *ClueService) GetClueStatistics(ctx context.Context) ([]map[string]any, error) {
	return s.repo.GetClueStatistics(ctx)
}

func (s *ClueService) GetClueAllList(ctx context.Context, clueType int64) ([]*model.Clue, int64, error) {
	return s.repo.GetClueAllList(ctx, clueType)
}

// GetWhatsappClues 取全部 WhatsApp 线索（type IN (5,7)，兼容历史错误类型）
// GetByIDs 按主键批量取线索（群发目标是用户选中的具体线索，与线索类型无关）。
func (s *ClueService) GetByIDs(ctx context.Context, ids []string) ([]*model.Clue, error) {
	return s.repo.GetByIDs(ctx, ids)
}

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
	ClueTypeQQ          int64 = 1
	ClueTypeWeChat      int64 = 2
	ClueTypePhone       int64 = 3
	ClueTypeTelegram    int64 = 4
	ClueTypeWhatsapp    int64 = 5
	ClueTypeTwitter     int64 = 6
	ClueTypeWeCom       int64 = 7
	ClueTypeLeadMining  int64 = 8
	ClueTypeDouyin      int64 = 9
	ClueTypeKuaishou    int64 = 10
	ClueTypeXiaohongshu int64 = 11
	ClueTypeXianyu      int64 = 12
	ClueTypeFeishu      int64 = 13
	ClueTypeTikTok      int64 = 14
	ClueTypeWebWidget   int64 = 15
	ClueTypeEmail       int64 = 16
	ClueTypeSMS         int64 = 17
	// ClueTypeGeoCapture GEO 决策链捕获线索（v3）：SourceID 存思维链 chain_id 供转化归因回写。
	//
	// 它早先等于 9，与 ClueTypeDouyin 撞号 —— 撞号不是显示瑕疵，是三处实打实的错账：
	// GEO 捕获的线索在列表页显示成「抖音」、评分走抖音档、并且与抖音线索共用
	// (type, account) 去重键（同一个联系人被两个来源各抓一次时，第二个会被当成重复丢掉）。
	// 18 是这张表第一个没被占用的号（1-17 连续、99 留给自定义）。
	//
	// 改值必须连同存量 clues.type 一起迁移，否则老库里 GEO 捕获的行仍写着 9、
	// 新写入的行写 18，同一个来源被拆成两类。迁移见 v3.56.0；
	// 判据是 source_id 命中 geo_query_chains.chain_id（chain_id 形如
	// "verify:<品牌>:<uuid>"，与抖音侧的群号/lead_mining 字面量不同形，误伤面为零）。
	ClueTypeGeoCapture int64 = 18
	ClueTypeCustom     int64 = 99
)

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
	// 18 = GEO 决策链捕获。这一档以前不存在：它和抖音共用 9，所以列表页把 GEO 捕获
	// 显示成「抖音」、按类型筛选时也筛不出来。类型号由 v3.56.0 迁移从存量 9 里分出来。
	{Value: "18", Label: "GEO 决策链捕获"},
	{Value: "99", Label: "自定义"},
}

// IsValidClueType 判断线索类型是否为产品对外暴露的类型号（1-18 + 99 自定义）。
// 导入时必须挡掉未知类型：否则一条 type=999 的线索会写进库，列表里显示成
// 空类型、按类型筛选永远筛不出来，统计里还多出一档无名分类。
func IsValidClueType(t int64) bool {
	switch t {
	case ClueTypeQQ, ClueTypeWeChat, ClueTypePhone, ClueTypeTelegram, ClueTypeWhatsapp,
		ClueTypeTwitter, ClueTypeWeCom, ClueTypeLeadMining, ClueTypeDouyin, ClueTypeKuaishou,
		ClueTypeXiaohongshu, ClueTypeXianyu, ClueTypeFeishu, ClueTypeTikTok, ClueTypeWebWidget,
		ClueTypeEmail, ClueTypeSMS, ClueTypeGeoCapture, ClueTypeCustom:
		return true
	}
	return false
}

// GetClueTypes 获取线索类型列表
func (s *ClueService) GetClueTypes(ctx context.Context) ([]ClueType, error) {
	return defaultClueTypes, nil
}
