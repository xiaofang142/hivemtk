package service

import (
	"context"
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

const (
	leadMiningExternalDedupWindow = 10 * time.Minute
	leadMiningExternalMaxContent  = 280
)

type leadMiningExternalDedupEntry struct {
	at time.Time
}

var (
	leadMiningExternalDedupMu sync.Mutex
	leadMiningExternalDedup   = map[string]leadMiningExternalDedupEntry{}
)

func init() {
	// Hook into NotifyBusiness (clean file) so LM-PR4 external dispatch works
	// even when notifyLeadDiscovery in dirty lead_mining.go cannot be modified.
	RegisterLeadMiningExternalDispatchHook(dispatchLeadMiningExternalFromNotification)
}

func dispatchLeadMiningExternalFromNotification(ctx context.Context, n *model.Notification) {
	if n == nil || !isLeadMiningDiscoveryNotification(n) {
		return
	}
	clue, err := findRecentLeadMiningClueForNotification(ctx, n)
	if err != nil {
		logger.Warnf("[lead-mining-notify] 查找线索失败，跳过外发: %v", err)
		return
	}
	if clue == nil {
		logger.Warnf("[lead-mining-notify] 未找到匹配线索，跳过外发 title=%q", n.Title)
		return
	}
	// LM-PR3 best-effort: enrich customer/OneID linkage without touching dirty files.
	enrichLeadMiningClueCustomer(ctx, clue)
	dispatchLeadMiningExternalNotifications(ctx, clue)
}

func isLeadMiningDiscoveryNotification(n *model.Notification) bool {
	if n == nil {
		return false
	}
	if n.Link == "/lead-mining" && strings.HasPrefix(strings.TrimSpace(n.Title), "发现新线索：") {
		return true
	}
	return n.Source == model.NotificationSourceLocal && strings.Contains(n.Link, "lead-mining") && strings.Contains(n.Title, "新线索")
}

func findRecentLeadMiningClueForNotification(ctx context.Context, n *model.Notification) (*model.Clue, error) {
	name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(n.Title), "发现新线索："))
	if name == "" {
		return nil, nil
	}
	repo := repository.NewClueRepositoryDefault()
	// Best-effort: fetch recent lead mining clues and match by name/summary.
	// ListWithQuery requires page/limit; use small recent window.
	clues, _, err := repo.ListWithQuery(ctx, repository.ClueQuery{Keyword: name, Type: clueTypePtr(ClueTypeLeadMining)}, 1, 20)
	if err != nil {
		// fallback: broader query without type filter in case type differs
		clues, _, err = repo.ListWithQuery(ctx, repository.ClueQuery{Keyword: name}, 1, 20)
		if err != nil {
			return nil, err
		}
	}
	var best *model.Clue
	var bestAt int64
	for _, c := range clues {
		if c == nil {
			continue
		}
		if c.Type != ClueTypeLeadMining && c.SourceID != "lead_mining" {
			continue
		}
		if strings.TrimSpace(c.Name) != name && !strings.Contains(strings.TrimSpace(c.Name), name) {
			continue
		}
		at := c.CreateTime
		if c.UpdatedAt > at {
			at = c.UpdatedAt
		}
		if best == nil || at > bestAt {
			cc := *c
			best = &cc
			bestAt = at
		}
	}
	if best != nil {
		return best, nil
	}
	// Last fallback: recent lead mining clues, pick newest created within 5 min.
	clues, _, err = repo.ListWithQuery(ctx, repository.ClueQuery{Type: clueTypePtr(ClueTypeLeadMining)}, 1, 20)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-5 * time.Minute).Unix()
	for _, c := range clues {
		if c == nil || c.CreateTime < cutoff {
			continue
		}
		at := c.CreateTime
		if best == nil || at > bestAt {
			cc := *c
			best = &cc
			bestAt = at
		}
	}
	return best, nil
}

func clueTypePtr(v int64) *int64 { return &v }

// dispatchLeadMiningExternalNotifications sends best-effort external notifications
// (telegram/feishu) according to lead_mining_notify_routes. It must never block/panic
// the main lead mining flow.
func dispatchLeadMiningExternalNotifications(ctx context.Context, clue *model.Clue) {
	if clue == nil {
		return
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		tid = tenant.DefaultTenantID
	}
	routes, err := repository.NewLeadMiningNotifyRouteRepository().ListEnabledByAccount(ctx, clue.SourceChannel, clue.SourceAccountID)
	if err != nil {
		logger.Warnf("[lead-mining-notify] 查询通知路由失败 clue=%s: %v", clue.ID, err)
		return
	}
	routes = selectLeadMiningNotifyRoutes(routes, clue.SourceChannel, clue.SourceAccountID)
	if len(routes) == 0 {
		return
	}
	msg := buildLeadMiningExternalMessage(clue)
	for _, r := range routes {
		route := r
		if !route.Enabled || strings.TrimSpace(route.Target) == "" {
			continue
		}
		channel := strings.ToLower(strings.TrimSpace(route.NotifyChannel))
		if channel == "" || channel == "in_app" {
			continue
		}
		dedupKey := leadMiningExternalDedupKey(tid, clue.ID, channel, strings.TrimSpace(route.Target))
		if !claimLeadMiningExternalDedup(dedupKey) {
			logger.Infof("[lead-mining-notify] dedup skip clue=%s channel=%s target=%s", clue.ID, channel, maskLeadMiningTarget(route.Target))
			continue
		}
		bgCtx := tenant.OnlyTenant(context.Background(), tid)
		switch channel {
		case "telegram", "tg":
			go sendLeadMiningTelegram(bgCtx, clue, route, msg)
		case "feishu", "lark":
			go sendLeadMiningFeishu(bgCtx, clue, route, msg)
		default:
			logger.Warnf("[lead-mining-notify] unknown notify_channel=%s route=%s", channel, route.RouteID)
		}
	}
}

func selectLeadMiningNotifyRoutes(routes []model.LeadMiningNotifyRoute, channel, accountID string) []model.LeadMiningNotifyRoute {
	if len(routes) == 0 {
		return nil
	}
	channel = strings.ToLower(strings.TrimSpace(channel))
	accountID = strings.TrimSpace(accountID)
	// account+channel exact > channel default > global default
	var exact, channelDefault, globalDefault []model.LeadMiningNotifyRoute
	for _, r := range routes {
		if !r.Enabled {
			continue
		}
		rc := strings.ToLower(strings.TrimSpace(r.Channel))
		ra := strings.TrimSpace(r.AccountID)
		switch {
		case ra != "" && accountID != "" && ra == accountID && (rc == "" || rc == channel):
			exact = append(exact, r)
		case ra == "" && rc != "" && rc == channel:
			channelDefault = append(channelDefault, r)
		case ra == "" && rc == "":
			globalDefault = append(globalDefault, r)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	if len(channelDefault) > 0 {
		return channelDefault
	}
	return globalDefault
}

func buildLeadMiningExternalMessage(clue *model.Clue) string {
	roleLabel := map[string]string{"demand": "需求方", "supply": "供货方", "resource": "资源方", "broker": "中介", "unknown": "待识别"}[clue.LeadRole]
	if roleLabel == "" {
		roleLabel = "待识别"
	}
	summary := sanitizeLeadMiningNotifyText(clue.Summary)
	if summary == "" {
		summary = sanitizeLeadMiningNotifyText(clue.Desc)
	}
	summary = truncateLeadMiningRunes(summary, 80)
	parts := []string{
		"[线索发掘] 发现新线索",
		fmt.Sprintf("渠道: %s", sanitizeLeadMiningNotifyText(clue.SourceChannel)),
		fmt.Sprintf("角色: %s", roleLabel),
		fmt.Sprintf("意向: %d", clue.IntentScore),
	}
	if summary != "" {
		parts = append(parts, "摘要: "+summary)
	}
	parts = append(parts, "详情请登录系统查看 /lead-mining")
	msg := strings.Join(parts, "\n")
	return truncateLeadMiningRunes(msg, leadMiningExternalMaxContent)
}

func sanitizeLeadMiningNotifyText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// simple desensitization: mask phone/email-like tokens
	fields := strings.Fields(s)
	for i, f := range fields {
		ff := f
		if leadMiningLooksLikePhone(ff) {
			fields[i] = leadMiningMaskPhone(ff)
			continue
		}
		if strings.Contains(ff, "@") && strings.Contains(ff, ".") {
			fields[i] = leadMiningMaskEmail(ff)
		}
	}
	return strings.Join(fields, " ")
}

func leadMiningLooksLikePhone(s string) bool {
	digits := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	return digits >= 7 && digits <= 15
}

func leadMiningMaskPhone(s string) string {
	var b strings.Builder
	digits := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	idx := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			idx++
			if idx > 3 && idx <= digits-2 {
				b.WriteRune('*')
			} else {
				b.WriteRune(r)
			}
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func leadMiningMaskEmail(s string) string {
	at := strings.Index(s, "@")
	if at <= 1 {
		return "***" + s[at:]
	}
	return s[:1] + "***" + s[at:]
}

func truncateLeadMiningRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func leadMiningExternalDedupKey(tenantID uint, clueID, channel, target string) string {
	return fmt.Sprintf("%d|%s|%s|%s", tenantID, clueID, channel, target)
}

func claimLeadMiningExternalDedup(key string) bool {
	now := time.Now()
	leadMiningExternalDedupMu.Lock()
	defer leadMiningExternalDedupMu.Unlock()
	if e, ok := leadMiningExternalDedup[key]; ok && now.Sub(e.at) < leadMiningExternalDedupWindow {
		return false
	}
	// lazy cleanup
	if len(leadMiningExternalDedup) > 4096 {
		cutoff := now.Add(-leadMiningExternalDedupWindow)
		for k, v := range leadMiningExternalDedup {
			if v.at.Before(cutoff) {
				delete(leadMiningExternalDedup, k)
			}
		}
	}
	leadMiningExternalDedup[key] = leadMiningExternalDedupEntry{at: now}
	return true
}

func maskLeadMiningTarget(t string) string {
	t = strings.TrimSpace(t)
	if len(t) <= 4 {
		return "***"
	}
	return t[:2] + "***" + t[len(t)-2:]
}

func sendLeadMiningTelegram(ctx context.Context, clue *model.Clue, route model.LeadMiningNotifyRoute, msg string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[lead-mining-notify] telegram panic clue=%s: %v", clue.ID, r)
		}
	}()
	target := strings.TrimSpace(route.Target)
	parts := strings.SplitN(target, ":", 2)
	if len(parts) != 2 {
		logger.Warnf("[lead-mining-notify] telegram target 格式应为 accountID:chatID route=%s target=%s", route.RouteID, maskLeadMiningTarget(target))
		return
	}
	accID, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || accID == 0 {
		logger.Warnf("[lead-mining-notify] telegram accountID 非法 route=%s", route.RouteID)
		return
	}
	chatID, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil || chatID == 0 {
		logger.Warnf("[lead-mining-notify] telegram chatID 非法 route=%s", route.RouteID)
		return
	}
	svc := NewTelegramIntegrationService(repository.NewTelegramAccountRepository())
	if err := svc.SendMessage(ctx, uint(accID), chatID, msg); err != nil {
		logger.Warnf("[lead-mining-notify] telegram 发送失败 clue=%s route=%s: %v", clue.ID, route.RouteID, err)
		return
	}
	logger.Infof("[lead-mining-notify] telegram 发送成功 clue=%s route=%s", clue.ID, route.RouteID)
}

func sendLeadMiningFeishu(ctx context.Context, clue *model.Clue, route model.LeadMiningNotifyRoute, msg string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[lead-mining-notify] feishu panic clue=%s: %v", clue.ID, r)
		}
	}()
	target := strings.TrimSpace(route.Target)
	// format: accountID:receiveID or accountID:receiveID:receiveIDType
	parts := strings.SplitN(target, ":", 3)
	if len(parts) < 2 {
		logger.Warnf("[lead-mining-notify] feishu target 格式应为 accountID:receiveID[:receiveIDType] route=%s target=%s", route.RouteID, maskLeadMiningTarget(target))
		return
	}
	accID, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || accID == 0 {
		logger.Warnf("[lead-mining-notify] feishu accountID 非法 route=%s", route.RouteID)
		return
	}
	receiveID := strings.TrimSpace(parts[1])
	receiveIDType := "open_id"
	if len(parts) == 3 && strings.TrimSpace(parts[2]) != "" {
		receiveIDType = strings.TrimSpace(parts[2])
	} else if strings.HasPrefix(receiveID, "oc_") {
		receiveIDType = "chat_id"
	}
	svc := NewFeishuIntegrationService(repository.NewFeishuAccountRepository(), repository.NewFeishuMessageRepository())
	if err := svc.SendMessage(ctx, uint(accID), receiveID, msg, receiveIDType, ""); err != nil {
		logger.Warnf("[lead-mining-notify] feishu 发送失败 clue=%s route=%s: %v", clue.ID, route.RouteID, err)
		return
	}
	logger.Infof("[lead-mining-notify] feishu 发送成功 clue=%s route=%s", clue.ID, route.RouteID)
}

func enrichLeadMiningClueCustomer(ctx context.Context, clue *model.Clue) {
	if clue == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logger.Warnf("[lead-mining-enrich] panic clue=%s: %v", clue.ID, r)
		}
	}()
	custRepo := repository.NewCustomerRepository()
	clueRepo := repository.NewClueRepositoryDefault()
	var customer *model.Customer
	// 1) by clue.OneID
	if strings.TrimSpace(clue.OneID) != "" {
		if c, err := custRepo.GetByUnifiedID(ctx, strings.TrimSpace(clue.OneID)); err == nil && c != nil {
			customer = c
		}
	}
	// 2) by platform identity
	if customer == nil {
		platform := strings.TrimSpace(clue.SourceChannel)
		if platform == "" {
			platform = strings.TrimSpace(strings.SplitN(clue.Account, ":", 2)[0])
		}
		senderID := ""
		if parts := strings.SplitN(clue.Account, ":", 2); len(parts) == 2 {
			senderID = strings.TrimSpace(parts[1])
		}
		if senderID != "" {
			candidate := &model.Customer{}
			applyLeadMiningIdentity(candidate, platform, senderID)
			uid := model.GenerateCustomerUnifiedID(candidate)
			if uid != "" {
				if c, err := custRepo.GetByUnifiedID(ctx, uid); err == nil && c != nil {
					customer = c
				}
			}
			if customer == nil {
				// try FindByIdentity for common fields
				if c, err := custRepo.FindByIdentity(ctx, candidate.Phone, candidate.Email, candidate.WechatOpenID, candidate.DouyinOpenID, candidate.XiaohongshuID); err == nil && c != nil {
					customer = c
				}
			}
			// 3) legacy lm: key
			if customer == nil && platform != "" {
				legacy := "lm:" + platform + ":" + senderID
				if c, err := custRepo.GetByUnifiedID(ctx, legacy); err == nil && c != nil {
					customer = c
				}
			}
		}
	}
	if customer == nil {
		return
	}
	now := time.Now()
	updates := map[string]any{}
	if strings.TrimSpace(clue.OneID) == "" || strings.HasPrefix(strings.TrimSpace(clue.OneID), "lm:") {
		if strings.TrimSpace(customer.UnifiedID) != "" {
			updates["one_id"] = strings.TrimSpace(customer.UnifiedID)
			clue.OneID = strings.TrimSpace(customer.UnifiedID)
		}
	}
	if strings.TrimSpace(clue.SourceChannel) != "" && strings.TrimSpace(customer.SourceChannel) == "" {
		customer.SourceChannel = strings.TrimSpace(clue.SourceChannel)
	}
	if strings.TrimSpace(clue.SourceAccountID) != "" && strings.TrimSpace(customer.SourceAccountID) == "" {
		customer.SourceAccountID = strings.TrimSpace(clue.SourceAccountID)
	}
	if customer.LastTouchAt == nil || now.After(*customer.LastTouchAt) {
		t := now
		customer.LastTouchAt = &t
	}
	_ = custRepo.Update(ctx, customer)
	if len(updates) > 0 {
		_ = clueRepo.UpdateByID(ctx, clue.ID, updates)
	}
}
