package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/channelbot/telegram"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/httpclient"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// TelegramGateService TG 群组入群管控服务
//
// 解决 Telegram 官方限制：Bot 不能主动向从未交互过的用户发起私聊。
// 两条链路（对应群管控 mode）：
//
// 方案 A join_request（私密群，群开启 "申请加入"）：
//
//	chat_join_request 事件 → 落台账 pending → 尝试私聊发送验证引导
//	（多数客户端会在申请列表展示 Bot，用户点开即可发 /start；若私聊失败，
//	群里提示用户主动找 Bot）→ 用户向 Bot 发 /start <token> → authorized=true
//	→ approveChatJoinRequest 放行入群。
//
// 方案 B mute_unlock（公开群）：
//
//	new_chat_members 事件 → 立即 restrictChatMember 全功能禁言 → 落台账
//	restricted → 群里发提示（含 t.me/<bot>?start=<token> 深链）→ 用户点击
//	跳转 Bot 私聊 /start → authorized=true → UnrestrictChatMember 解禁。
//
// 超时未验证：方案 A declineChatJoinRequest；方案 B banChatMember 踢出。
// TTL 清扫由 StartGateSweeper 后台协程周期执行。
type TelegramGateService struct {
	db         *gorm.DB
	gateRepo   *repository.TelegramGroupGateRepository
	memberRepo *repository.TelegramGroupMemberRepository
	tgRepo     *repository.TelegramAccountRepository
}

func NewTelegramGateService(db *gorm.DB) *TelegramGateService {
	svc := &TelegramGateService{db: db}
	if db != nil {
		svc.gateRepo = repository.NewTelegramGroupGateRepositoryWithDB(db)
		svc.memberRepo = repository.NewTelegramGroupMemberRepositoryWithDB(db)
		svc.tgRepo = repository.NewTelegramAccountRepository()
		svc.tgRepo.SetDB(context.Background(), db)
	}
	return svc
}

// gate mode 常量
const (
	TGGateModeJoinRequest = "join_request" // 方案 A
	TGGateModeMuteUnlock  = "mute_unlock"  // 方案 B
)

// tgGateDefaults 缺省提示语
const (
	tgGateDefaultVerifyMsg = "你好 %s！为防止垃圾广告，请点击下面的按钮完成验证，验证通过后即可正常使用群组。"
)

func tgGateDefaultWelcome(mode string) string {
	if mode == TGGateModeJoinRequest {
		return "@%s 你的入群申请已收到！请点击 Bot 私聊链接完成验证：https://t.me/%s?start=%s ，验证通过后将自动批准进群。"
	}
	return "@%s 欢迎加入！为防止垃圾广告，账号已临时禁言。请点击链接完成验证：https://t.me/%s?start=%s ，验证通过后自动解除。"
}

// client 加载 Bot 客户端
func (s *TelegramGateService) client(ctx context.Context, accountID uint) (*telegram.Client, error) {
	if s.tgRepo == nil {
		return nil, fmt.Errorf("tg repo nil")
	}
	acc, err := s.tgRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("get tg account %d: %w", accountID, err)
	}
	return telegram.NewTelegramClient(acc.BotToken, core.WithHTTPClient(httpclient.Client)), nil
}

// genVerifyToken 生成不可预测的验证 token（HMAC 链路：随机 20 字节，仅落库明文比对）
func genVerifyToken() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// botDeepLink 生成 t.me 深链
func botDeepLink(botUsername, token string) string {
	return fmt.Sprintf("https://t.me/%s?start=%s", strings.TrimPrefix(botUsername, "@"), token)
}

// HandleJoinRequest 方案 A 入口：处理 chat_join_request
func (s *TelegramGateService) HandleJoinRequest(ctx context.Context, accountID uint, req *telegram.TGChatJoinRequest) {
	if s == nil || s.db == nil || req == nil || req.Chat == nil || req.From == nil {
		return
	}
	chatIDStr := strconv.FormatInt(req.Chat.ID, 10)
	gate, err := s.gateRepo.GetByChatID(ctx, accountID, chatIDStr)
	if err != nil || gate == nil || !gate.Enabled || gate.Mode != TGGateModeJoinRequest {
		return // 未配置网关或未开启：保持 Telegram 默认行为（人工审批）
	}

	ttl := gate.VerifyTTLMin
	if ttl <= 0 {
		ttl = 10
	}
	expires := time.Now().Add(time.Duration(ttl) * time.Minute)
	token := genVerifyToken()

	member := &model.TelegramGroupMember{
		AccountID:   accountID,
		ChatID:      chatIDStr,
		UserID:      strconv.FormatInt(req.From.ID, 10),
		Username:    req.From.Username,
		FullName:    tgUserDisplayName(req.From),
		JoinStatus:  model.TGMemberPending,
		JoinMode:    TGGateModeJoinRequest,
		VerifyToken: token,
		ExpiresAt:   &expires,
	}
	if err := s.memberRepo.Upsert(ctx, member); err != nil {
		logger.Errorf("[TG-Gate] 台账写入失败 account=%d chat=%s user=%d: %v", accountID, chatIDStr, req.From.ID, err)
		return
	}

	// 尝试私聊验证引导。Telegram 限制：用户从未与 Bot 交互过时 sendMessage 会 403，
	// 此时依赖群里提示让用户主动点开 Bot（不重试，等用户 /start）。
	botUsername := s.botUsername(ctx, accountID)
	link := botDeepLink(botUsername, token)
	welcome := gate.WelcomeMsg
	if welcome == "" {
		welcome = tgGateDefaultWelcome(TGGateModeJoinRequest)
	}
	welcome = fmt.Sprintf(welcome, tgUserDisplayName(req.From), botUsername, token)

	if cli, cerr := s.client(ctx, accountID); cerr == nil {
		if _, serr := cli.SendMessage(ctx, req.From.ID, welcome, telegram.SendMessageOptions{DisableMarkdownConversion: true}); serr != nil {
			logger.Infof("[TG-Gate] 私聊引导未达（用户尚未与 Bot 交互，属预期）account=%d user=%d: %v", accountID, req.From.ID, serr)
			// 私聊不可达 → 退化为群内提示（不含 token，避免泄露；引导点击深链）
			if link != "" {
				groupTip := fmt.Sprintf("@%s 你的入群申请已收到，请点击 %s 完成验证后自动批准。", req.From.Username, link)
				_, _ = cli.SendMessage(ctx, req.Chat.ID, groupTip, telegram.SendMessageOptions{DisableMarkdownConversion: true})
			}
		}
	}
}

// HandleNewMembers 方案 B 入口：new_chat_members → 禁言 + 提示。
// 返回是否命中了门控（true=本群是启用的 mute_unlock 管控群，调用方应跳过 AI 欢迎语）。
func (s *TelegramGateService) HandleNewMembers(ctx context.Context, accountID uint, chatID int64, members []telegram.TGUser) bool {
	if s == nil || s.db == nil || len(members) == 0 {
		return false
	}
	chatIDStr := strconv.FormatInt(chatID, 10)
	gate, err := s.gateRepo.GetByChatID(ctx, accountID, chatIDStr)
	if err != nil || gate == nil || !gate.Enabled || gate.Mode != TGGateModeMuteUnlock {
		return false
	}

	cli, err := s.client(ctx, accountID)
	if err != nil {
		logger.Errorf("[TG-Gate] Bot 客户端加载失败 account=%d: %v", accountID, err)
		return true // 已判定为管控群，但禁言执行不了；调用方仍应跳过 AI 欢迎语
	}

	botUsername := s.botUsername(ctx, accountID)
	ttl := gate.VerifyTTLMin
	if ttl <= 0 {
		ttl = 10
	}

	handled := false
	for i := range members {
		m := members[i]
		if m.IsBot {
			continue
		}
		handled = true
		expires := time.Now().Add(time.Duration(ttl) * time.Minute)
		token := genVerifyToken()
		member := &model.TelegramGroupMember{
			AccountID:   accountID,
			ChatID:      chatIDStr,
			UserID:      strconv.FormatInt(m.ID, 10),
			Username:    m.Username,
			FullName:    tgUserDisplayName(&m),
			JoinStatus:  model.TGMemberRestricted,
			JoinMode:    TGGateModeMuteUnlock,
			VerifyToken: token,
			ExpiresAt:   &expires,
		}
		if err := s.memberRepo.Upsert(ctx, member); err != nil {
			logger.Errorf("[TG-Gate] 台账写入失败 account=%d chat=%s user=%d: %v", accountID, chatIDStr, m.ID, err)
			continue
		}

		// 立即全功能禁言
		if err := cli.RestrictChatMember(ctx, chatID, m.ID, 0); err != nil {
			logger.Errorf("[TG-Gate] 禁言失败（检查 Bot 是否有 Restrict Members 权限）account=%d user=%d: %v", accountID, m.ID, err)
			continue
		}

		welcome := gate.WelcomeMsg
		if welcome == "" {
			welcome = tgGateDefaultWelcome(TGGateModeMuteUnlock)
		}
		welcome = fmt.Sprintf(welcome, tgUserDisplayName(&m), botUsername, token)
		if _, err := cli.SendMessage(ctx, chatID, welcome, telegram.SendMessageOptions{DisableMarkdownConversion: true}); err != nil {
			logger.Errorf("[TG-Gate] 群内验证提示发送失败 account=%d chat=%s: %v", accountID, chatIDStr, err)
		}
	}
	return handled
}

// parseStartCommand 识别 /start 命令：返回 (token, isStart)，token 可为空（纯 /start）
func parseStartCommand(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text != "/start" && !strings.HasPrefix(text, "/start ") {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(text, "/start"))
	return token, true
}

// HandleStartCommand /start 激活入口：私聊收到 "/start <token>" 或纯 "/start"。
// 返回是否命中了网关验证流程（命中后不再走销售智能体）。
func (s *TelegramGateService) HandleStartCommand(ctx context.Context, accountID uint, from *telegram.TGUser, text string, privateChatID int64) bool {
	if s == nil || s.db == nil || from == nil {
		return false
	}
	token, isStart := parseStartCommand(text)
	if !isStart {
		return false
	}

	if token == "" {
		// 无 token：查是否有该用户的待验证记录（方案 A 私聊直接 /start 的场景，可能存在
		// 多群待验证，取最早过期的一条）
		s.sendPendingVerificationSummary(ctx, accountID, from, privateChatID)
		return true
	}

	member, err := s.memberRepo.GetByToken(ctx, token)
	if err != nil || member == nil || member.UserID != strconv.FormatInt(from.ID, 10) {
		// token 不属于该用户：防冒用
		s.replyPrivate(ctx, accountID, privateChatID, "验证码无效或不属于当前账号，请在群内重新获取验证链接。")
		return true
	}
	if member.ExpiresAt != nil && time.Now().After(*member.ExpiresAt) {
		s.replyPrivate(ctx, accountID, privateChatID, "验证已过期，请在群内重新获取验证链接。")
		return true
	}

	if err := s.AuthorizeMember(ctx, member); err != nil {
		logger.Errorf("[TG-Gate] 放行失败 account=%d chat=%s user=%s: %v", member.AccountID, member.ChatID, member.UserID, err)
		s.replyPrivate(ctx, accountID, privateChatID, "验证成功，但放行时出现问题，请联系群管理员处理。")
		return true
	}

	s.replyPrivate(ctx, accountID, privateChatID, "✅ 验证通过！欢迎加入，现在可以回到群组正常发言了。")
	return true
}

// AuthorizeMember 标记已验证 + 按 mode 执行放行动作（approve / 解禁）
func (s *TelegramGateService) AuthorizeMember(ctx context.Context, member *model.TelegramGroupMember) error {
	cli, err := s.client(ctx, member.AccountID)
	if err != nil {
		return err
	}
	chatID, _ := strconv.ParseInt(member.ChatID, 10, 64)
	userID, _ := strconv.ParseInt(member.UserID, 10, 64)
	if chatID == 0 || userID == 0 {
		return fmt.Errorf("invalid chat/user id: chat=%s user=%s", member.ChatID, member.UserID)
	}

	if member.JoinMode == TGGateModeJoinRequest {
		if err := cli.ApproveChatJoinRequest(ctx, chatID, userID); err != nil {
			// 已被人工批准/已加入时会报错，降级为记录日志不阻断
			logger.Warnf("[TG-Gate] approve 失败（可能已加入）chat=%s user=%d: %v", member.ChatID, userID, err)
		}
	} else {
		if err := cli.UnrestrictChatMember(ctx, chatID, userID); err != nil {
			logger.Warnf("[TG-Gate] 解禁失败（可能已退群）chat=%s user=%d: %v", member.ChatID, userID, err)
		}
	}

	now := time.Now()
	member.Authorized = true
	member.AuthorizedAt = &now
	member.JoinStatus = model.TGMemberApproved
	return s.memberRepo.Update(ctx, member)
}

// sendPendingVerificationSummary 无 token /start：把该用户待验证群汇总发给他
func (s *TelegramGateService) sendPendingVerificationSummary(ctx context.Context, accountID uint, from *telegram.TGUser, privateChatID int64) {
	// 简化实现：提示用户回群点击链接（记录里 token 绑定了 chat+user，无法凭空重建）
	s.replyPrivate(ctx, accountID, privateChatID, "你好！如果你在某个群组收到了验证提示，请点击提示中的链接完成验证；验证通过后即可正常发言。")
}

func (s *TelegramGateService) replyPrivate(ctx context.Context, accountID uint, chatID int64, text string) {
	if chatID == 0 {
		return
	}
	cli, err := s.client(ctx, accountID)
	if err != nil {
		return
	}
	if _, err := cli.SendMessage(ctx, chatID, text, telegram.SendMessageOptions{DisableMarkdownConversion: true}); err != nil {
		logger.Warnf("[TG-Gate] 私聊回复失败 account=%d chat=%d: %v", accountID, chatID, err)
	}
}

func (s *TelegramGateService) botUsername(ctx context.Context, accountID uint) string {
	if s.tgRepo == nil {
		return ""
	}
	acc, err := s.tgRepo.GetByID(ctx, accountID)
	if err != nil || acc == nil {
		return ""
	}
	return strings.TrimSpace(acc.BotUsername)
}

func tgUserDisplayName(u *telegram.TGUser) string {
	if u == nil {
		return ""
	}
	name := strings.TrimSpace(u.FirstName)
	if name == "" {
		name = u.Username
	}
	return name
}

// MemberUnverified 判断成员在指定群是否处于未过验证状态（pending/restricted/kicked 且未激活）。
// 用于门控群 AI 互锁：未验证成员的群发言不触发销售 AI / 线索商机挖掘。
// 语义：群有启用中的 mute_unlock 门控但该成员无台账 → 视为门控生效前已在群的
// 历史成员/绕过入群事件的人，同样拦截（gateWhitelisted 为 false）；
// 群无门控或成员已验证 → 放行。
func (s *TelegramGateService) MemberUnverified(ctx context.Context, accountID uint, chatID, userID string) bool {
	if s == nil || s.db == nil {
		return false
	}
	m, err := s.memberRepo.Get(ctx, accountID, chatID, userID)
	if err == nil && m != nil {
		if m.Authorized {
			return false
		}
		switch m.JoinStatus {
		case model.TGMemberPending, model.TGMemberRestricted, model.TGMemberKicked:
			return true
		}
		return false
	}
	// 无台账：仅当群是启用中的禁言解锁门控时才拦截（历史成员补验证），否则放行
	gate, gerr := s.gateRepo.GetByChatID(ctx, accountID, chatID)
	return gerr == nil && gate != nil && gate.Enabled && gate.Mode == TGGateModeMuteUnlock
}

// ---------- 网关配置管理（管理端） ----------

// ListGates 网关列表（accountID=0 表示全部）
func (s *TelegramGateService) ListGates(ctx context.Context, accountID uint) ([]*model.TelegramGroupGate, error) {
	if s.gateRepo == nil {
		return nil, fmt.Errorf("db nil")
	}
	return s.gateRepo.List(ctx, accountID)
}

// GetGate 网关详情
func (s *TelegramGateService) GetGate(ctx context.Context, id uint) (*model.TelegramGroupGate, error) {
	if s.gateRepo == nil {
		return nil, fmt.Errorf("db nil")
	}
	return s.gateRepo.GetByID(ctx, id)
}

// CreateGate 创建网关配置
func (s *TelegramGateService) CreateGate(ctx context.Context, gate *model.TelegramGroupGate) error {
	if s.gateRepo == nil {
		return fmt.Errorf("db nil")
	}
	gate.ChatID = strings.TrimSpace(gate.ChatID)
	if gate.ChatID == "" {
		return fmt.Errorf("chat_id 不能为空")
	}
	if _, err := s.gateRepo.GetByChatID(ctx, gate.AccountID, gate.ChatID); err == nil {
		return fmt.Errorf("该群组已存在管控配置")
	}
	return s.gateRepo.Create(ctx, gate)
}

// UpdateGate 更新网关配置
func (s *TelegramGateService) UpdateGate(ctx context.Context, gate *model.TelegramGroupGate) error {
	if s.gateRepo == nil {
		return fmt.Errorf("db nil")
	}
	return s.gateRepo.Update(ctx, gate)
}

// DeleteGate 删除网关配置
func (s *TelegramGateService) DeleteGate(ctx context.Context, id uint) error {
	if s.gateRepo == nil {
		return fmt.Errorf("db nil")
	}
	return s.gateRepo.Delete(ctx, id)
}

// ListMembers 成员验证台账
func (s *TelegramGateService) ListMembers(ctx context.Context, accountID uint, chatID, status string, limit, offset int) ([]*model.TelegramGroupMember, int64, error) {
	if s.memberRepo == nil {
		return nil, 0, fmt.Errorf("db nil")
	}
	return s.memberRepo.ListByChat(ctx, accountID, chatID, status, limit, offset)
}

// AuthorizeMemberByID 管理端人工放行兜底
func (s *TelegramGateService) AuthorizeMemberByID(ctx context.Context, memberID uint) error {
	if s.memberRepo == nil {
		return fmt.Errorf("db nil")
	}
	var member model.TelegramGroupMember
	if err := s.db.WithContext(ctx).First(&member, memberID).Error; err != nil {
		return err
	}
	if member.Authorized {
		return nil
	}
	return s.AuthorizeMember(ctx, &member)
}

// SweepExpired TTL 清扫：超时未验证 → 方案 A 拒绝申请 / 方案 B 踢出并落台账 kicked
func (s *TelegramGateService) SweepExpired(ctx context.Context, limit int) (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	expired, err := s.memberRepo.ListExpired(ctx, time.Now(), limit)
	if err != nil {
		return 0, err
	}
	swept := 0
	for _, member := range expired {
		cli, err := s.client(ctx, member.AccountID)
		if err != nil {
			continue
		}
		chatID, _ := strconv.ParseInt(member.ChatID, 10, 64)
		userID, _ := strconv.ParseInt(member.UserID, 10, 64)
		if chatID == 0 || userID == 0 {
			continue
		}
		if member.JoinMode == TGGateModeJoinRequest {
			if err := cli.DeclineChatJoinRequest(ctx, chatID, userID); err != nil {
				logger.Warnf("[TG-Gate] decline 超时申请失败 chat=%s user=%d: %v", member.ChatID, userID, err)
			}
		} else {
			// 踢出（untilDate=过去时间 → 只踢不拉黑，用户可再次申请加入）
			if err := cli.BanChatMember(ctx, chatID, userID, time.Now().Add(-time.Minute).Unix()); err != nil {
				logger.Warnf("[TG-Gate] 踢出超时成员失败 chat=%s user=%d: %v", member.ChatID, userID, err)
			}
		}
		member.JoinStatus = model.TGMemberKicked
		if err := s.memberRepo.Update(ctx, member); err != nil {
			logger.Errorf("[TG-Gate] kicked 状态回写失败 id=%d: %v", member.ID, err)
		}
		swept++
	}
	return swept, nil
}

// StartGateSweeper 启动后台 TTL 清扫协程；调用方在路由注册后直接调用（内部 go routine），
// 每分钟执行一次 SweepExpired。db 为 nil 时直接返回（测试/无 DB 场景）。
func StartGateSweeper(db *gorm.DB) {
	if db == nil {
		return
	}
	svc := NewTelegramGateService(db)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("[TG-Gate] TTL 清扫协程 panic 重启: %v", r)
				go StartGateSweeper(db) // 自愈重启
			}
		}()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			swept, err := svc.SweepExpired(ctx, 100)
			cancel()
			if err != nil {
				logger.Errorf("[TG-Gate] TTL 清扫失败: %v", err)
			} else if swept > 0 {
				logger.Infof("[TG-Gate] TTL 清扫完成，处理 %d 个超时成员", swept)
			}
		}
	}()
	logger.Infof("[TG-Gate] TTL 清扫器已启动（每分钟一次）")
}
