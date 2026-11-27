package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"
)

// T05 语义契约错误：线索通知配置属于系统配置，收件人只能是本系统员工
// （system_users.id），不接受智能体 ID 或自由文本；通道仅站内/TG/飞书。
// 包 utils.ErrInvalidInput 使 controller 的 ErrorFromDB 返回 400 而非 500。
var (
	ErrInvalidNotifyRecipient = fmt.Errorf("%w: 接收人必须是系统用户 ID（员工），不支持智能体 ID", utils.ErrInvalidInput)
	ErrInvalidNotifyType      = fmt.Errorf("%w: 不支持的通知通道（仅站内/TG/飞书）", utils.ErrInvalidInput)
)

// validateNotifyRouteSemantics 在写入前固化「系统配置通知员工」前后端契约：
// notify_user_ids 元素必须是正整数系统用户 ID（空数组允许，表示走账号维护人回退）；
// notify_types 只允许 in_app/inapp/telegram/tg/feishu/lark（大小写与空白不敏感）。
// 该函数不碰 DB，便于契约测试独立验证；不引入任何智能体字段。
func validateNotifyRouteSemantics(m *model.LeadMiningNotifyRoute) error {
	for _, raw := range m.NotifyUserIDs {
		uid := strings.TrimSpace(string(raw))
		if uid == "" {
			continue
		}
		id, err := strconv.ParseUint(uid, 10, 64)
		if err != nil || id == 0 {
			return fmt.Errorf("%w: %q", ErrInvalidNotifyRecipient, string(raw))
		}
	}
	for _, raw := range m.NotifyTypes {
		switch strings.ToLower(strings.TrimSpace(string(raw))) {
		case "", "in_app", "inapp", "telegram", "tg", "feishu", "lark":
		default:
			return fmt.Errorf("%w: %q", ErrInvalidNotifyType, string(raw))
		}
	}
	return nil
}

type LeadMiningNotifyRouteService struct {
	repo *repository.LeadMiningNotifyRouteRepository
}

func NewLeadMiningNotifyRouteService() *LeadMiningNotifyRouteService {
	return &LeadMiningNotifyRouteService{repo: repository.NewLeadMiningNotifyRouteRepository()}
}

func (s *LeadMiningNotifyRouteService) Create(ctx context.Context, m *model.LeadMiningNotifyRoute) error {
	if err := validateNotifyRouteSemantics(m); err != nil {
		return err
	}
	if m.RouteID == "" {
		m.RouteID = fmt.Sprintf("lnr_%d", time.Now().UnixNano())
	}
	return s.repo.Create(ctx, m)
}

func (s *LeadMiningNotifyRouteService) List(ctx context.Context, channel, accountID string) ([]model.LeadMiningNotifyRoute, error) {
	return s.repo.List(ctx, channel, accountID)
}

func (s *LeadMiningNotifyRouteService) Delete(ctx context.Context, routeID string) error {
	return s.repo.Delete(ctx, routeID)
}

func (s *LeadMiningNotifyRouteService) ListEnabledByAccount(ctx context.Context, channel, accountID string) ([]model.LeadMiningNotifyRoute, error) {
	return s.repo.ListEnabledByAccount(ctx, channel, accountID)
}
