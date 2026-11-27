package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// LeadNotifyService 线索归档通知路由（FR-015 / T042）。
// 所有投递先进入耐久 outbox，再由同一发送/记账路径处理；路由 JSONB 不再是权威送达历史。
type LeadNotifyService struct {
	routes   leadNotifyRouteStore
	accounts repository.ChannelAccountRepository
	users    leadNotifyUserStore
	outbox   leadNotifyOutboxStore
	sender   leadNotifySender
}

type leadNotifyUserStore interface {
	GetByID(ctx context.Context, id uint) (*model.SystemUser, error)
}

// leadNotifyRouteStore 是通知服务实际使用的最小路由读写边界。
type leadNotifyRouteStore interface {
	ListByChannelAccount(ctx context.Context, channelAccountID uint, channel string) ([]model.LeadMiningNotifyRoute, error)
	AppendDeliveryRecords(ctx context.Context, routeID, records string) error
}

type leadNotifySender interface {
	Send(ctx context.Context, item *model.LeadNotifyOutbox) error
}

type leadNotifyOutboxStore interface {
	Enqueue(ctx context.Context, item *model.LeadNotifyOutbox) (bool, error)
	ClaimByIDForDelivery(ctx context.Context, id uint64, now time.Time) (*model.LeadNotifyOutbox, bool, error)
	ClaimDueForUpdate(ctx context.Context, now time.Time, limit int) ([]*model.LeadNotifyOutbox, error)
	MarkDelivered(ctx context.Context, id uint64, token string, at time.Time) error
	ScheduleRetry(ctx context.Context, id uint64, token string, nextAt time.Time, lastError string) error
	MarkExhausted(ctx context.Context, id uint64, token, lastError string) error
	MarkReceiptUnknown(ctx context.Context, id uint64, token, reason string) error
	ReclaimStaleSending(ctx context.Context, olderThan time.Time) (int64, error)
	RetryManually(ctx context.Context, id uint64) error
	Cancel(ctx context.Context, id uint64) error
	RecordDelivery(ctx context.Context, record *model.LeadNotifyDelivery) error
	ListDeliveries(ctx context.Context, query model.LeadNotifyDeliveryQuery) ([]*model.LeadNotifyDelivery, error)
	SummarizeDeliveries(ctx context.Context, query model.LeadNotifyDeliveryQuery) ([]model.LeadNotifyDeliverySummary, error)
	ListOutboxByEvent(ctx context.Context, tenantID uint, eventID string) ([]*model.LeadNotifyOutbox, error)
}

// LeadNotifyReceiptUnknownError 表示上游可能已接收但回执丢失；worker 不得自动重发。
type LeadNotifyReceiptUnknownError struct {
	Reason string
}

func (e *LeadNotifyReceiptUnknownError) Error() string {
	if e == nil || strings.TrimSpace(e.Reason) == "" {
		return "notification receipt unknown"
	}
	return "notification receipt unknown: " + e.Reason
}

var errLeadNotifyAuthorization = errors.New("lead notify recipient authorization denied")

func NewLeadNotifyService() *LeadNotifyService {
	if svc := GlobalLeadNotifyService(); svc != nil {
		return svc
	}
	return NewLeadNotifyServiceWithDeps(
		repository.NewLeadMiningNotifyRouteRepository(),
		repository.NewChannelAccountRepository(),
		repository.NewLeadNotifyUserRepository(nil),
		repository.NewLeadNotifyRepository(nil),
		defaultLeadNotifySender{},
	)
}

func NewLeadNotifyServiceWith(routes leadNotifyRouteStore, accounts repository.ChannelAccountRepository) *LeadNotifyService {
	return NewLeadNotifyServiceWithDeps(
		routes,
		accounts,
		repository.NewLeadNotifyUserRepository(nil),
		repository.NewLeadNotifyRepository(nil),
		defaultLeadNotifySender{},
	)
}

func NewLeadNotifyServiceWithRepositories(
	routes *repository.LeadMiningNotifyRouteRepository,
	accounts repository.ChannelAccountRepository,
	users *repository.LeadNotifyUserRepository,
	outbox *repository.LeadNotifyRepository,
) *LeadNotifyService {
	return NewLeadNotifyServiceWithDeps(routes, accounts, users, outbox, defaultLeadNotifySender{})
}

func NewLeadNotifyServiceWithDeps(
	routes leadNotifyRouteStore,
	accounts repository.ChannelAccountRepository,
	users leadNotifyUserStore,
	outbox leadNotifyOutboxStore,
	sender leadNotifySender,
) *LeadNotifyService {
	return &LeadNotifyService{routes: routes, accounts: accounts, users: users, outbox: outbox, sender: sender}
}

var _ CustomerOwnerNotifier = (*LeadNotifyService)(nil)

func marshalLeadNotifyDeliveryRecords(records []model.JSONPayload) (string, error) {
	raw, err := json.Marshal(records)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func normalizeLeadNotifyChannel(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "inapp":
		return model.LeadNotifyChannelInApp
	case "tg":
		return model.LeadNotifyChannelTelegram
	case "lark":
		return model.LeadNotifyChannelFeishu
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func uniqueLeadNotifyStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// NotifyOwner 实现 CustomerOwnerNotifier：路由解析 → 幂等入队 → 即时尝试；失败意图留给 worker 重试。
func (s *LeadNotifyService) NotifyOwner(ctx context.Context, in ArchiveClueInput, customerID string) error {
	if s == nil || s.routes == nil || s.outbox == nil || s.sender == nil {
		return errors.New("通知服务未完整装配")
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		if tenant.IsMultiTenant() {
			return repository.ErrLeadTenantRequired
		}
		tid = tenant.DefaultTenantID
	}
	eventID := strings.TrimSpace(in.ClueID)
	if eventID == "" {
		eventID = strings.TrimSpace(customerID)
	}
	if eventID == "" {
		return errors.New("通知事件缺少 clue_id/customer_id")
	}

	channel := strings.TrimSpace(in.SourceChannel)
	accountID := resolveLeadNotifyAccountID(ctx, channel, in.SourceAccountID)
	routes, err := s.routes.ListByChannelAccount(ctx, accountID, channel)
	if err != nil {
		return fmt.Errorf("查询通知路由失败: %w", err)
	}
	route := selectLeadNotifyRoute(routes, accountID, channel)
	if route == nil {
		route = s.fallbackAccountRoute(ctx, tid, accountID, channel)
	}
	if route == nil {
		auditErr := s.recordAudit(ctx, tid, eventID, in.ClueID, customerID, "", "", model.LeadNotifyChannelInApp, "", model.LeadNotifyStatusNoRoute, "无可用通知路由（账号/渠道/租户三级均未配置）")
		logger.Ctx(ctx).Warn().Str("customer_id", customerID).Msg("[lead-notify] 无可用通知路由（账号/渠道/租户三级均未配置）")
		if auditErr != nil {
			return fmt.Errorf("无可用通知路由；审计写入失败: %w", auditErr)
		}
		return errors.New("无可用通知路由（账号/渠道/租户三级均未配置）")
	}

	recipients := uniqueLeadNotifyStrings([]string(route.NotifyUserIDs))
	if len(recipients) == 0 && accountID > 0 && s.accounts != nil {
		if acc, aerr := s.accounts.GetByID(ctx, tid, accountID); aerr == nil && acc != nil && acc.TenantID == tid && acc.Status == 1 {
			recipients = uniqueLeadNotifyStrings([]string(acc.OwnerUserIDs))
		}
	}
	types := uniqueLeadNotifyStrings([]string(route.NotifyTypes))
	if len(types) == 0 {
		types = []string{model.LeadNotifyChannelInApp}
	}

	title := "发现新线索（已归档）"
	content := fmt.Sprintf("客户=%s 线索=%s 渠道=%s 摘要=%s", customerID, in.ClueID, in.SourceChannel, in.Summary)
	externalContent, maskPolicy := maskLeadNotifySummary(content, in.SourceChannel)
	succeeded := 0
	var lastErr error
	for _, rawType := range types {
		notifyType := normalizeLeadNotifyChannel(rawType)
		switch notifyType {
		case model.LeadNotifyChannelInApp:
			if len(recipients) == 0 {
				if err := s.recordAudit(ctx, tid, eventID, in.ClueID, customerID, route.RouteID, route.Scope, notifyType, "", model.LeadNotifyStatusNoRecipient, "no_recipient"); err != nil {
					return err
				}
				lastErr = errors.New("无通知收件人")
				continue
			}
			for _, recipient := range recipients {
				userID, convErr := strconv.ParseUint(recipient, 10, 64)
				if convErr != nil || userID == 0 {
					if err := s.recordAudit(ctx, tid, eventID, in.ClueID, customerID, route.RouteID, route.Scope, notifyType, recipient, "failed", "notify_user_id 非法"); err != nil {
						return err
					}
					lastErr = errors.New("notify_user_id 非法")
					continue
				}
				item := &model.LeadNotifyOutbox{
					TenantID: tid, ClueID: in.ClueID, EventID: eventID, CustomerID: customerID,
					Channel: notifyType, Recipient: recipient, RouteID: route.RouteID, RouteScope: route.Scope,
					AccountID: accountID, Title: title, Content: content, Link: "/lead-mining",
					IdempotencyKey: leadNotifyIdempotencyKey(tid, in.ClueID, eventID, recipient, notifyType),
				}
				ok, err := s.enqueueAndAttempt(ctx, item)
				if ok {
					succeeded++
				}
				if err != nil {
					lastErr = err
				}
			}
		case model.LeadNotifyChannelTelegram, model.LeadNotifyChannelFeishu:
			target := strings.TrimSpace(route.Target)
			targetAccountID, targetErr := parseLeadNotifyTargetAccountID(target)
			if target == "" || targetErr != nil {
				if err := s.recordAudit(ctx, tid, eventID, in.ClueID, customerID, route.RouteID, route.Scope, notifyType, target, "no_target", "路由未配置合法 target（accountID:chatID / accountID:receiveID）"); err != nil {
					return err
				}
				lastErr = errors.New("通知路由 target 缺失或非法")
				continue
			}
			item := &model.LeadNotifyOutbox{
				TenantID: tid, ClueID: in.ClueID, EventID: eventID, CustomerID: customerID,
				Channel: notifyType, Recipient: target, RouteID: route.RouteID, RouteScope: route.Scope,
				AccountID: targetAccountID, Title: title, Content: externalContent, Link: "/lead-mining", MaskPolicy: maskPolicy,
				IdempotencyKey: leadNotifyIdempotencyKey(tid, in.ClueID, eventID, target, notifyType),
			}
			ok, err := s.enqueueAndAttempt(ctx, item)
			if ok {
				succeeded++
			}
			if err != nil {
				lastErr = err
			}
		default:
			if err := s.recordAudit(ctx, tid, eventID, in.ClueID, customerID, route.RouteID, route.Scope, notifyType, "", "failed", "不支持的通知类型"); err != nil {
				return err
			}
			lastErr = fmt.Errorf("不支持的通知类型: %s", notifyType)
		}
	}
	if succeeded == 0 {
		if lastErr != nil {
			return lastErr
		}
		return errors.New("全部通知通道送达失败")
	}
	return nil
}

// EnqueueExternalForClue 将另一条 lead-mining 发现通知路径收口到同一耐久 outbox/发送出口，只投外部通道。
func (s *LeadNotifyService) EnqueueExternalForClue(ctx context.Context, clue *model.Clue) error {
	if s == nil || clue == nil || s.routes == nil || s.outbox == nil || s.sender == nil {
		return errors.New("通知服务未完整装配")
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		if tenant.IsMultiTenant() {
			return repository.ErrLeadTenantRequired
		}
		tid = tenant.DefaultTenantID
	}
	accountID := resolveLeadNotifyAccountID(ctx, clue.SourceChannel, clue.SourceAccountID)
	routes, err := s.routes.ListByChannelAccount(ctx, accountID, clue.SourceChannel)
	if err != nil {
		return err
	}
	route := selectLeadNotifyRoute(routes, accountID, clue.SourceChannel)
	if route == nil {
		return s.recordAudit(ctx, tid, clue.ID, clue.ID, clue.CustomerID, "", "", "external", "", model.LeadNotifyStatusNoRoute, "无可用外部通知路由")
	}
	channels := []string{route.NotifyChannel}
	channels = append(channels, []string(route.NotifyTypes)...)
	seen := map[string]struct{}{}
	content, maskPolicy := maskLeadNotifySummary(buildLeadMiningExternalMessage(clue), clue.SourceChannel)
	var lastErr error
	for _, raw := range channels {
		channel := normalizeLeadNotifyChannel(raw)
		if channel != model.LeadNotifyChannelTelegram && channel != model.LeadNotifyChannelFeishu {
			continue
		}
		if _, ok := seen[channel]; ok {
			continue
		}
		seen[channel] = struct{}{}
		target := strings.TrimSpace(route.Target)
		targetAccountID, targetErr := parseLeadNotifyTargetAccountID(target)
		if target == "" || targetErr != nil {
			lastErr = errors.New("外部通知路由 target 缺失或非法")
			continue
		}
		item := &model.LeadNotifyOutbox{
			TenantID: tid, ClueID: clue.ID, EventID: clue.ID, CustomerID: clue.CustomerID,
			Channel: channel, Recipient: target, RouteID: route.RouteID, RouteScope: route.Scope,
			AccountID: targetAccountID, Title: "发现新线索", Content: content, Link: "/lead-mining", MaskPolicy: maskPolicy,
			IdempotencyKey: leadNotifyIdempotencyKey(tid, clue.ID, clue.ID, target, channel),
		}
		if _, err := s.enqueueAndAttempt(ctx, item); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (s *LeadNotifyService) enqueueAndAttempt(ctx context.Context, item *model.LeadNotifyOutbox) (bool, error) {
	created, err := s.outbox.Enqueue(ctx, item)
	if err != nil {
		return false, err
	}
	if !created {
		switch item.Status {
		case model.LeadNotifyStatusDelivered:
			return true, nil
		case model.LeadNotifyStatusPending:
		default:
			return false, fmt.Errorf("通知意图已存在，状态=%s", item.Status)
		}
	}
	claimed, ok, err := s.outbox.ClaimByIDForDelivery(ctx, item.ID, time.Now().UTC())
	if err != nil {
		return false, err
	}
	if !ok {
		return item.Status == model.LeadNotifyStatusDelivered, nil
	}
	status, err := s.processClaimed(ctx, claimed)
	return status == model.LeadNotifyStatusDelivered, err
}

func (s *LeadNotifyService) processClaimed(ctx context.Context, item *model.LeadNotifyOutbox) (string, error) {
	if item == nil || item.ClaimToken == "" {
		return "", errors.New("通知意图缺少 claim token")
	}
	itemCtx := tenant.OnlyTenant(ctx, item.TenantID)
	if err := s.authorizeItem(itemCtx, item); err != nil {
		status := "authorization_denied"
		if recordErr := s.recordAttempt(itemCtx, item, status, false, err); recordErr != nil {
			return status, fmt.Errorf("授权拒绝记账失败: %w", recordErr)
		}
		if stateErr := s.outbox.MarkExhausted(itemCtx, item.ID, item.ClaimToken, err.Error()); stateErr != nil {
			return status, fmt.Errorf("授权拒绝状态写入失败: %w", stateErr)
		}
		return status, err
	}

	sendErr := s.sender.Send(itemCtx, item)
	if sendErr == nil {
		if err := s.recordAttempt(itemCtx, item, model.LeadNotifyStatusDelivered, true, nil); err != nil {
			_ = s.outbox.MarkReceiptUnknown(itemCtx, item.ID, item.ClaimToken, "delivery history write failed: "+err.Error())
			return model.LeadNotifyStatusReceiptUnknown, err
		}
		if err := s.outbox.MarkDelivered(itemCtx, item.ID, item.ClaimToken, time.Now().UTC()); err != nil {
			_ = s.outbox.MarkReceiptUnknown(itemCtx, item.ID, item.ClaimToken, "delivered state write failed: "+err.Error())
			return model.LeadNotifyStatusReceiptUnknown, err
		}
		return model.LeadNotifyStatusDelivered, nil
	}

	var unknown *LeadNotifyReceiptUnknownError
	if errors.As(sendErr, &unknown) {
		status := model.LeadNotifyStatusReceiptUnknown
		if err := s.recordAttempt(itemCtx, item, status, false, sendErr); err != nil {
			return status, err
		}
		if err := s.outbox.MarkReceiptUnknown(itemCtx, item.ID, item.ClaimToken, sendErr.Error()); err != nil {
			return status, err
		}
		return status, sendErr
	}

	status := model.LeadNotifyStatusRetry
	if item.Attempts >= item.MaxAttempts {
		status = model.LeadNotifyStatusExhausted
	}
	if err := s.recordAttempt(itemCtx, item, status, false, sendErr); err != nil {
		return status, err
	}
	next := time.Now().UTC().Add(leadNotifyBackoff(item.Attempts))
	if err := s.outbox.ScheduleRetry(itemCtx, item.ID, item.ClaimToken, next, sendErr.Error()); err != nil {
		return status, err
	}
	return status, sendErr
}

func (s *LeadNotifyService) authorizeItem(ctx context.Context, item *model.LeadNotifyOutbox) error {
	if item.TenantID == 0 {
		return fmt.Errorf("%w: invalid tenant", errLeadNotifyAuthorization)
	}
	switch normalizeLeadNotifyChannel(item.Channel) {
	case model.LeadNotifyChannelInApp:
		if s.users == nil {
			return fmt.Errorf("%w: user store unavailable", errLeadNotifyAuthorization)
		}
		uid, err := strconv.ParseUint(strings.TrimSpace(item.Recipient), 10, 64)
		if err != nil || uid == 0 {
			return fmt.Errorf("%w: invalid owner", errLeadNotifyAuthorization)
		}
		user, err := s.users.GetByID(ctx, uint(uid))
		if err != nil || user == nil {
			return fmt.Errorf("%w: recipient not found", errLeadNotifyAuthorization)
		}
		if user.TenantID != item.TenantID || user.Status != 1 || !user.Enabled || !model.IsValidSystemUserRole(user.Role) {
			return fmt.Errorf("%w: recipient disabled/status/role", errLeadNotifyAuthorization)
		}
		if item.RouteScope == "account" && item.AccountID > 0 {
			acc, err := s.accountByID(ctx, item.TenantID, item.AccountID)
			if err != nil || acc == nil {
				return fmt.Errorf("%w: channel account unavailable", errLeadNotifyAuthorization)
			}
			if user.Role != model.SystemUserRoleAdmin && !leadNotifyContains(acc.OwnerUserIDs, item.Recipient) {
				return fmt.Errorf("%w: account ownership revoked", errLeadNotifyAuthorization)
			}
		}
		return nil
	case model.LeadNotifyChannelTelegram, model.LeadNotifyChannelFeishu:
		targetAccountID, err := parseLeadNotifyTargetAccountID(item.Recipient)
		if err != nil || targetAccountID == 0 || (item.AccountID != 0 && item.AccountID != targetAccountID) {
			return fmt.Errorf("%w: invalid external target", errLeadNotifyAuthorization)
		}
		acc, err := s.accountByID(ctx, item.TenantID, targetAccountID)
		if err != nil || acc == nil || acc.TenantID != item.TenantID || acc.Status != 1 {
			return fmt.Errorf("%w: channel account disabled/cross-tenant", errLeadNotifyAuthorization)
		}
		return nil
	default:
		return fmt.Errorf("%w: unsupported channel", errLeadNotifyAuthorization)
	}
}

func (s *LeadNotifyService) accountByID(ctx context.Context, tid, id uint) (*model.ChannelAccount, error) {
	if s.accounts == nil || tid == 0 || id == 0 {
		return nil, repository.ErrChannelStoreUnavailable
	}
	acc, err := s.accounts.GetByID(ctx, tid, id)
	if err != nil {
		return nil, err
	}
	if acc == nil || acc.TenantID != tid {
		return nil, gormErrLeadNotifyAccountNotFound()
	}
	return acc, nil
}

func gormErrLeadNotifyAccountNotFound() error {
	return errors.New("lead notify channel account not found")
}

func leadNotifyContains(values model.JSONStrings, want string) bool {
	want = strings.TrimSpace(want)
	for _, value := range values {
		if strings.TrimSpace(string(value)) == want {
			return true
		}
	}
	return false
}

func (s *LeadNotifyService) recordAudit(ctx context.Context, tid uint, eventID, clueID, customerID, routeID, scope, channel, recipient, status, reason string) error {
	key := "audit:" + leadNotifyIdempotencyKey(tid, clueID, eventID, recipient, channel+":"+status)
	return s.outbox.RecordDelivery(ctx, &model.LeadNotifyDelivery{
		TenantID: tid, DeliveryKey: key, ClueID: clueID, EventID: eventID, CustomerID: customerID,
		Recipient: leadNotifyHistoryRecipient(channel, recipient), Channel: channel, RouteID: routeID, RouteScope: scope,
		Success: false, Status: status, Error: reason,
	})
}

func (s *LeadNotifyService) recordAttempt(ctx context.Context, item *model.LeadNotifyOutbox, status string, success bool, sendErr error) error {
	reason := ""
	if sendErr != nil {
		reason = sendErr.Error()
	}
	key := fmt.Sprintf("attempt:%d:%d", item.ID, item.Attempts)
	return s.outbox.RecordDelivery(ctx, &model.LeadNotifyDelivery{
		TenantID: item.TenantID, DeliveryKey: key, OutboxID: item.ID, ClueID: item.ClueID, EventID: item.EventID,
		CustomerID: item.CustomerID, Recipient: leadNotifyHistoryRecipient(item.Channel, item.Recipient), Channel: item.Channel,
		RouteID: item.RouteID, RouteScope: item.RouteScope, Success: success, Status: status, Error: reason,
		MaskPolicy: item.MaskPolicy, Attempt: item.Attempts,
	})
}

func leadNotifyHistoryRecipient(channel, recipient string) string {
	if normalizeLeadNotifyChannel(channel) == model.LeadNotifyChannelInApp {
		return strings.TrimSpace(recipient)
	}
	return maskLeadMiningTarget(recipient)
}

func parseLeadNotifyTargetAccountID(target string) (uint, error) {
	parts := strings.SplitN(strings.TrimSpace(target), ":", 3)
	if len(parts) < 2 {
		return 0, errors.New("target format invalid")
	}
	id, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || id == 0 {
		return 0, errors.New("target account id invalid")
	}
	return uint(id), nil
}

func (s *LeadNotifyService) fallbackAccountRoute(ctx context.Context, tid, accountID uint, channel string) *model.LeadMiningNotifyRoute {
	if accountID == 0 || s.accounts == nil {
		return nil
	}
	acc, err := s.accounts.GetByID(ctx, tid, accountID)
	if err != nil || acc == nil || acc.TenantID != tid || acc.Status != 1 || len(acc.OwnerUserIDs) == 0 {
		return nil
	}
	return &model.LeadMiningNotifyRoute{
		TenantID: tid, RouteID: "account-owner-fallback", Channel: channel,
		ChannelAccountID: accountID, Scope: "account",
		NotifyUserIDs: acc.OwnerUserIDs, NotifyTypes: model.JSONStrings{"in_app"}, Enabled: true,
	}
}

func resolveLeadNotifyAccountID(ctx context.Context, channel, external string) uint {
	if strings.TrimSpace(external) == "" {
		return 0
	}
	if resolved, err := NewChannelResolver().ResolveByAccountRef(ctx, channel, external); err == nil && resolved != nil {
		return resolved.AccountID
	}
	return 0
}

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

type defaultLeadNotifySender struct{}

var (
	leadNotifyGlobalMu  sync.RWMutex
	leadNotifyGlobalSvc *LeadNotifyService
)

// SetGlobalLeadNotifyService 登记生产通知出口；归档、发掘与历史 API 共用同一实例。
func SetGlobalLeadNotifyService(svc *LeadNotifyService) {
	leadNotifyGlobalMu.Lock()
	leadNotifyGlobalSvc = svc
	leadNotifyGlobalMu.Unlock()
}

// GlobalLeadNotifyService 返回当前生产通知出口，未装配时为 nil。
func GlobalLeadNotifyService() *LeadNotifyService {
	leadNotifyGlobalMu.RLock()
	defer leadNotifyGlobalMu.RUnlock()
	return leadNotifyGlobalSvc
}

func (defaultLeadNotifySender) Send(ctx context.Context, item *model.LeadNotifyOutbox) error {
	if item == nil {
		return errors.New("nil notification intent")
	}
	switch normalizeLeadNotifyChannel(item.Channel) {
	case model.LeadNotifyChannelInApp:
		uid, err := strconv.ParseUint(strings.TrimSpace(item.Recipient), 10, 64)
		if err != nil || uid == 0 {
			return errors.New("站内通知收件人非法")
		}
		if !NotifyBusiness(ctx, &model.Notification{
			UserID: uint(uid), Type: model.NotificationTypeInfo,
			Title: item.Title, Content: item.Content, Link: item.Link,
			Source: model.NotificationSourceLocal, Level: model.NotificationLevelInfo,
		}) {
			return errors.New("站内通知写入失败")
		}
		return nil
	case model.LeadNotifyChannelTelegram:
		parts := strings.SplitN(strings.TrimSpace(item.Recipient), ":", 2)
		if len(parts) != 2 {
			return errors.New("telegram target 格式非法")
		}
		accountID, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil || accountID == 0 {
			return errors.New("telegram accountID 非法")
		}
		chatID, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil || chatID == 0 {
			return errors.New("telegram chatID 非法")
		}
		return NewTelegramIntegrationService(repository.NewTelegramAccountRepository()).SendMessage(ctx, uint(accountID), chatID, item.Content)
	case model.LeadNotifyChannelFeishu:
		parts := strings.SplitN(strings.TrimSpace(item.Recipient), ":", 3)
		if len(parts) < 2 {
			return errors.New("feishu target 格式非法")
		}
		accountID, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil || accountID == 0 {
			return errors.New("feishu accountID 非法")
		}
		receiveID := strings.TrimSpace(parts[1])
		receiveIDType := "open_id"
		if len(parts) == 3 && strings.TrimSpace(parts[2]) != "" {
			receiveIDType = strings.TrimSpace(parts[2])
		} else if strings.HasPrefix(receiveID, "oc_") {
			receiveIDType = "chat_id"
		}
		return NewFeishuIntegrationService(repository.NewFeishuAccountRepository(), repository.NewFeishuMessageRepository()).SendMessage(ctx, uint(accountID), receiveID, item.Content, receiveIDType, "")
	default:
		return fmt.Errorf("不支持的通知通道: %s", item.Channel)
	}
}
