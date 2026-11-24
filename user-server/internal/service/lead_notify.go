package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// LeadNotifyService 线索归档通知路由（FR-015 / T042）。
//
// 路由优先级：账号维护人（scope=account）→ 渠道维护人（scope=channel）→ 租户默认（scope=tenant）。
// 站内走 NotifyBusiness；TG/飞书复用仓内既有 IntegrationService.SendMessage——
// 这些服务已按宪法 IV 判响应业务码（飞书 code、TG ok），不以 HTTP 200 当成功
// （feishu_outbound_wire_contract_test.go 实证）。每次调用结果落送达记录。
//
// 本服务实现 Phase 3 的 CustomerOwnerNotifier，由 Router 通过
// ClueArchiveService.SetNotifier / LeadClueArchiveService.SetNotifier 注入。
type LeadNotifyService struct {
	routes   *repository.LeadMiningNotifyRouteRepository
	accounts repository.ChannelAccountRepository
}

func NewLeadNotifyService() *LeadNotifyService {
	return &LeadNotifyService{
		routes:   repository.NewLeadMiningNotifyRouteRepository(),
		accounts: repository.NewChannelAccountRepository(),
	}
}

func NewLeadNotifyServiceWith(routes *repository.LeadMiningNotifyRouteRepository, accounts repository.ChannelAccountRepository) *LeadNotifyService {
	return &LeadNotifyService{routes: routes, accounts: accounts}
}

var _ CustomerOwnerNotifier = (*LeadNotifyService)(nil)

// NotifyOwner 实现 CustomerOwnerNotifier：按三级回退找路由并送达。
func (s *LeadNotifyService) NotifyOwner(ctx context.Context, in ArchiveClueInput, customerID string) error {
	if s == nil || s.routes == nil {
		return errors.New("通知路由服务未装配")
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		if tenant.IsMultiTenant() {
			return repository.ErrLeadTenantRequired
		}
		tid = tenant.DefaultTenantID
	}

	accountID := resolveLeadNotifyAccountID(ctx, in.SourceAccountID)
	channel := in.SourceChannel
	routes, err := s.routes.ListByChannelAccount(ctx, accountID, channel)
	if err != nil {
		return fmt.Errorf("查询通知路由失败: %w", err)
	}
	route := selectLeadNotifyRoute(routes, accountID, channel)
	if route == nil {
		// 无显式路由时退回渠道账号的 owner_user_ids（账号维护人）。
		route = s.fallbackAccountRoute(ctx, tid, accountID, channel)
	}
	if route == nil {
		logger.Ctx(ctx).Warn().Str("customer_id", customerID).
			Msg("[lead-notify] 无可用通知路由（账号/渠道/租户三级均未配置）")
		return nil
	}

	recipients := []string(route.NotifyUserIDs)
	if len(recipients) == 0 && accountID > 0 && s.accounts != nil {
		if acc, aerr := s.accounts.GetByID(ctx, tid, accountID); aerr == nil && acc != nil {
			recipients = []string(acc.OwnerUserIDs)
		}
	}
	types := []string(route.NotifyTypes)
	if len(types) == 0 {
		types = []string{"in_app"}
	}

	title := "发现新线索（已归档）"
	content := fmt.Sprintf("客户=%s 线索=%s 渠道=%s 摘要=%s", customerID, in.ClueID, in.SourceChannel, in.Summary)
	records := make([]model.JSONPayload, 0, len(types)*max(1, len(recipients)))
	for _, notifyType := range types {
		switch strings.ToLower(strings.TrimSpace(notifyType)) {
		case "in_app", "inapp", "":
			for _, uid := range recipients {
				userID, convErr := strconv.ParseUint(strings.TrimSpace(uid), 10, 64)
				r := model.JSONPayload{"type": "in_app", "target": uid, "at": time.Now().Unix()}
				if convErr != nil || userID == 0 {
					r["success"] = false
					r["error"] = "notify_user_id 非法"
				} else {
					ok := NotifyBusiness(ctx, &model.Notification{
						UserID: uint(userID), Type: model.NotificationTypeInfo,
						Title: title, Content: content, Link: "/lead-mining",
						Source: model.NotificationSourceLocal, Level: model.NotificationLevelInfo,
					})
					r["success"] = ok
					if !ok {
						r["error"] = "站内通知写入失败"
					}
				}
				records = append(records, r)
			}
		case "telegram", "tg":
			records = append(records, s.deliverExternal(ctx, "telegram", *route, content)...)
		case "feishu", "lark":
			records = append(records, s.deliverExternal(ctx, "feishu", *route, content)...)
		default:
			records = append(records, model.JSONPayload{"type": notifyType, "success": false, "error": "不支持的通知类型"})
		}
	}

	raw, _ := json.Marshal(records)
	if err := s.routes.UpdateDeliveryRecords(ctx, route.RouteID, string(raw)); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("route_id", route.RouteID).Msg("[lead-notify] 送达记录落库失败")
		return fmt.Errorf("送达记录落库失败: %w", err)
	}
	succeeded := 0
	for _, r := range records {
		if ok, _ := r["success"].(bool); ok {
			succeeded++
		}
	}
	if succeeded == 0 && len(records) > 0 {
		return fmt.Errorf("全部通知通道送达失败（记录 %d 条）", len(records))
	}
	return nil
}

func (s *LeadNotifyService) fallbackAccountRoute(ctx context.Context, tid, accountID uint, channel string) *model.LeadMiningNotifyRoute {
	if accountID == 0 || s.accounts == nil {
		return nil
	}
	acc, err := s.accounts.GetByID(ctx, tid, accountID)
	if err != nil || acc == nil || len(acc.OwnerUserIDs) == 0 {
		return nil
	}
	return &model.LeadMiningNotifyRoute{
		TenantID: tid, RouteID: "account-owner-fallback", Channel: channel,
		ChannelAccountID: accountID, Scope: "account",
		NotifyUserIDs: acc.OwnerUserIDs, NotifyTypes: model.JSONStrings{"in_app"}, Enabled: true,
	}
}

// deliverExternal 外发 TG/飞书。成功判据是既有 IntegrationService 返回 nil，
// 它们内部解析平台业务码（飞书 code==0 / TG ok==true），HTTP 200 但业务失败同样返回 error。
func (s *LeadNotifyService) deliverExternal(ctx context.Context, kind string, route model.LeadMiningNotifyRoute, content string) []model.JSONPayload {
	target := strings.TrimSpace(route.Target)
	if target == "" {
		return []model.JSONPayload{{
			"type": kind, "success": false, "error": "路由未配置 target（accountID:chatID / accountID:receiveID）",
			"at": time.Now().Unix(),
		}}
	}
	parts := strings.SplitN(target, ":", 3)
	accID, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || accID == 0 || len(parts) < 2 {
		return []model.JSONPayload{{
			"type": kind, "success": false, "error": "target 格式非法", "at": time.Now().Unix(),
		}}
	}
	var sendErr error
	if kind == "telegram" {
		chatID, perr := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if perr != nil || chatID == 0 {
			return []model.JSONPayload{{"type": kind, "success": false, "error": "chatID 非法", "at": time.Now().Unix()}}
		}
		svc := NewTelegramIntegrationService(repository.NewTelegramAccountRepository())
		sendErr = svc.SendMessage(ctx, uint(accID), chatID, content)
	} else {
		receiveID := strings.TrimSpace(parts[1])
		receiveIDType := "open_id"
		if len(parts) == 3 && strings.TrimSpace(parts[2]) != "" {
			receiveIDType = strings.TrimSpace(parts[2])
		} else if strings.HasPrefix(receiveID, "oc_") {
			receiveIDType = "chat_id"
		}
		svc := NewFeishuIntegrationService(repository.NewFeishuAccountRepository(), repository.NewFeishuMessageRepository())
		sendErr = svc.SendMessage(ctx, uint(accID), receiveID, content, receiveIDType, "")
	}
	r := model.JSONPayload{"type": kind, "target": maskLeadMiningTarget(target), "at": time.Now().Unix(), "success": sendErr == nil}
	if sendErr != nil {
		// 业务码失败的具体 message 已在 IntegrationService 内解析并记录；
		// 这里记录错误摘要，保证送达记录可审计（不泄漏密钥）。
		r["error"] = sendErr.Error()
	}
	return []model.JSONPayload{r}
}

// resolveLeadNotifyAccountID 把外部账号标识解析为渠道账号数值 id（复用 Phase 2 ChannelResolver 口径）。
func resolveLeadNotifyAccountID(ctx context.Context, external string) uint {
	if strings.TrimSpace(external) == "" {
		return 0
	}
	if resolved, err := NewChannelResolver().ResolveByIdentifyCode(ctx, external); err == nil && resolved != nil {
		return resolved.AccountID
	}
	return 0
}

// selectLeadNotifyRoute 三级回退：account > channel > tenant；老路由（无 scope）走既有优先级函数。
func selectLeadNotifyRoute(routes []model.LeadMiningNotifyRoute, accountID uint, channel string) *model.LeadMiningNotifyRoute {
	channel = strings.ToLower(strings.TrimSpace(channel))
	var accountRoute, channelRoute, tenantRoute *model.LeadMiningNotifyRoute
	for i := range routes {
		r := &routes[i]
		if !r.Enabled {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(r.Scope)) {
		case "account":
			if accountID > 0 && r.ChannelAccountID == accountID && accountRoute == nil {
				accountRoute = r
			}
		case "channel":
			if channel != "" && strings.EqualFold(strings.TrimSpace(r.Channel), channel) && channelRoute == nil {
				channelRoute = r
			}
		case "tenant":
			if tenantRoute == nil {
				tenantRoute = r
			}
		}
	}
	if accountRoute != nil {
		return accountRoute
	}
	if channelRoute != nil {
		return channelRoute
	}
	if tenantRoute != nil {
		return tenantRoute
	}
	// 兼容 085 旧路由（无 scope 字段）：沿用既有 account+channel > channel > global 口径。
	legacy := selectLeadMiningNotifyRoutes(routes, channel, strconv.FormatUint(uint64(accountID), 10))
	if len(legacy) > 0 {
		return &legacy[0]
	}
	return nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
