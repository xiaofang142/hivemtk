package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
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
// 超时未验证：方案 A declineChatJoinRequest；方案 B banChatMember 移出（可再次入群）。
// 对真人的处置以"提示确实送达过"为前提，详见 SweepExpired。
// TTL 清扫由 StartGateSweeper 后台协程周期执行，多实例共库时由 cron_job_leases 选主。
// TelegramGateService L4 门面：只持 repository，不持有 *gorm.DB（五层架构 §三.4，
// 对齐 telegram_polling_lock.go 先例）。nil 守卫统一判 gateRepo（构造期与 db 同生共死）。
type TelegramGateService struct {
	gateRepo   *repository.TelegramGroupGateRepository
	memberRepo *repository.TelegramGroupMemberRepository
	tgRepo     *repository.TelegramAccountRepository
	leaseRepo  *repository.CronJobLeaseRepository

	// apiBase 覆盖 Telegram Bot API 基址（空串=官方 api.telegram.org）。
	// 供测试指向 httptest 服务端，避免 happy path 因真实 API 401 而无法覆盖；
	// 也便于将来接自建网关/代理。见 SetAPIBase。
	apiBase string
}

// SetAPIBase 覆盖 Bot API 基址（测试/代理场景）。
//
// 为什么需要这个接缝：AuthorizeMember 的放行动作（UnrestrictChatMember /
// ApproveChatJoinRequest）必须打到 TG 侧；没有接缝时测试只能用假 token 打真 API，
// 必然 401，于是"授权成功"这条主路径永远无法被验证（历史上该用例因此长期失败）。
// 与 db.SetTestDB / config.SetAppConfig 同属项目既有的测试注入约定。
func (s *TelegramGateService) SetAPIBase(u string) { s.apiBase = u }

func NewTelegramGateService(db *gorm.DB) *TelegramGateService {
	svc := &TelegramGateService{}
	if db != nil {
		svc.gateRepo = repository.NewTelegramGroupGateRepositoryWithDB(db)
		svc.memberRepo = repository.NewTelegramGroupMemberRepositoryWithDB(db)
		svc.tgRepo = repository.NewTelegramAccountRepositoryWithDB(db)
		svc.leaseRepo = repository.NewCronJobLeaseRepositoryWithDB(db)
	}
	return svc
}

// wired 依赖装配判定（gateRepo 与 db 同生共死，判它即判全部）
func (s *TelegramGateService) wired() bool { return s != nil && s.gateRepo != nil }

// gate mode 常量
const (
	TGGateModeJoinRequest = "join_request" // 方案 A
	TGGateModeMuteUnlock  = "mute_unlock"  // 方案 B
)

// tgGateDefaultWelcome 门控未配置自定义文案时的兜底模板。
// 命名占位符 {{display}} / {{verify_link}} 由 renderTGGateWelcome 替换；两种门控模式
// 当前共用这一段（差别只在送达面：join_request 走私聊，mute_unlock 私聊不可达时退化成群内提示）。
func tgGateDefaultWelcome() string {
	return "👋 欢迎 {{display}}！\n\n请点击这里完成验证：{{verify_link}}\n\n验证后即可正常发言~"
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
	opts := []core.ClientOption{core.WithHTTPClient(httpclient.Client)}
	if s.apiBase != "" {
		opts = append(opts, core.WithBaseURL(s.apiBase))
	}
	return telegram.NewTelegramClient(acc.BotToken, opts...), nil
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

// renderTGGateWelcome 渲染门控的欢迎/验证提示。
//
// 去掉「你正在通过 @xx 加入群组」那段＝模板里少一个位置占位符，而按位置序传参时
// 后面的 https://t.me/%s?start=%s 会整体左移一位（domain 位拿到 bot 名、token 位拿到
// domain）⇒ 验证链接直接坏掉，且坏得很安静。所以新模板改用命名占位符，替换与出现
// 顺序/个数无关；库里已存在的 %s 老模板继续按 [display, bot, domain, token] 的位置序
// 走原路径——改文案不该把别人配过的群提示打坏。
func renderTGGateWelcome(tpl, display, botUsername, token string) string {
	if strings.Contains(tpl, "{{") {
		out := strings.NewReplacer(
			"{{display}}", display,
			"{{bot}}", botUsername,
			"{{verify_link}}", botDeepLink(botUsername, token),
			"{{token}}", token,
		).Replace(tpl)
		if strings.Contains(out, "{{") {
			// 未知命名占位符会原文外发（如 {{foo}}），比缺文案更伤：直接退回默认模板。
			logger.Errorf("[TG-Gate] 欢迎模板含未知占位符，已退回默认模板 tpl=%q", tpl)
			return renderTGGateWelcomeDefault(display, botUsername, token)
		}
		return out
	}
	// 老 %s 模板：按 [display, bot, domain, token] 位置序逐个填，verb 与实参
	// 数量不一致时绝不再调 Sprintf（多余实参会变成 %!(EXTRA …) 发出去，缺少的
	// 会变成 %!s(MISSING)，两种都是线上事故）。
	args := []string{display, botUsername, strings.TrimPrefix(botUsername, "@"), token}
	out := fillPositionalVerbs(tpl, args)
	if hasUnfilledVerb(out) {
		logger.Errorf("[TG-Gate] 欢迎模板动词无法完全填充，已退回默认模板 tpl=%q", tpl)
		return renderTGGateWelcomeDefault(display, botUsername, token)
	}
	return out
}

// renderTGGateWelcomeDefault 用已知干净的默认模板渲染（不递归回主入口）。
func renderTGGateWelcomeDefault(display, botUsername, token string) string {
	return strings.NewReplacer(
		"{{display}}", display,
		"{{bot}}", botUsername,
		"{{verify_link}}", botDeepLink(botUsername, token),
		"{{token}}", token,
	).Replace(tgGateDefaultWelcome())
}

// fillPositionalVerbs 把模板里的 %s 按出现顺序依次换成 args；args 用尽后
// 剩余的 %s 原样保留（由 hasUnfilledVerb 判定退回默认模板）。
func fillPositionalVerbs(tpl string, args []string) string {
	if !strings.Contains(tpl, "%s") || len(args) == 0 {
		return tpl
	}
	var b strings.Builder
	ai := 0
	for i := 0; i < len(tpl); i++ {
		if tpl[i] == '%' && i+1 < len(tpl) && tpl[i+1] == 's' && ai < len(args) {
			b.WriteString(args[ai])
			ai++
			i++
			continue
		}
		b.WriteByte(tpl[i])
	}
	return b.String()
}

// hasUnfilledVerb 报告文本里是否还有未被填充的 % 动词（% 后跟字母即动词，
// %% 是转义不算；"100%" 这类 % 后非字母的不算）。
func hasUnfilledVerb(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		c := s[i+1]
		if c == '%' {
			i++
			continue
		}
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
}

// HandleJoinRequest 方案 A 入口：处理 chat_join_request
func (s *TelegramGateService) HandleJoinRequest(ctx context.Context, accountID uint, req *telegram.TGChatJoinRequest) {
	if !s.wired() || req == nil || req.Chat == nil || req.From == nil {
		return
	}
	chatIDStr := strconv.FormatInt(req.Chat.ID, 10)
	gate, err := s.gateRepo.GetByChatID(ctx, accountID, chatIDStr)
	if err != nil || gate == nil || !gate.Enabled || gate.Mode != TGGateModeJoinRequest {
		return // 未配置网关或未开启：保持 Telegram 默认行为（人工审批）
	}

	ttl := normalizeTGGateTTL(gate.VerifyTTLMin)
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
	//
	// 送达结果必须落台账：申请被批准前人看不到群消息，私聊是唯一能触达他的通道，
	// 于是 welcome_sent_at 就是"这个人有没有被告知"的唯一事实源（补发计数与
	// 超时处置都读它），而不是读一条自己都没发出去过的消息。
	botUsername := s.botUsername(ctx, accountID)
	link := botDeepLink(botUsername, token)
	welcome := gate.WelcomeMsg
	if welcome == "" {
		welcome = tgGateDefaultWelcome()
	}
	welcome = renderTGGateWelcome(welcome, tgUserDisplayName(req.From), botUsername, token)

	delivered := false
	if cli, cerr := s.client(ctx, accountID); cerr == nil {
		if _, serr := cli.SendMessage(ctx, req.From.ID, welcome, telegram.SendMessageOptions{DisableMarkdownConversion: true}); serr != nil {
			logger.Infof("[TG-Gate] 私聊引导未达（用户尚未与 Bot 交互，属预期）account=%d user=%d: %v", accountID, req.From.ID, serr)
			// 私聊不可达 → 退化为群内提示（不含 token，避免泄露；引导点击深链）
			if link != "" {
				groupTip := fmt.Sprintf("@%s 你的入群申请已收到，请点击 %s 完成验证后自动批准。", req.From.Username, link)
				_, _ = cli.SendMessage(ctx, req.Chat.ID, groupTip, telegram.SendMessageOptions{DisableMarkdownConversion: true})
			}
		} else {
			delivered = true
		}
	}
	if delivered {
		s.markWelcomeSent(ctx, accountID, chatIDStr, member.UserID)
	} else {
		s.bumpWelcomeResend(ctx, accountID, chatIDStr, member.UserID)
	}
}

// HandleNewMembers 方案 B 入口：new_chat_members → 禁言 + 提示。
// 返回是否命中了门控（true=本群是启用的 mute_unlock 管控群，调用方应跳过 AI 欢迎语）。
func (s *TelegramGateService) HandleNewMembers(ctx context.Context, accountID uint, chatID int64, members []telegram.TGUser) bool {
	if !s.wired() || len(members) == 0 {
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
	ttl := normalizeTGGateTTL(gate.VerifyTTLMin)

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

		// 先禁言再发提示（顺序不能反：先发提示万一禁言失败，用户会以为可发言）。
		// 禁言/提示任何一步失败都不 continue——台账保持 restricted 且 welcome_sent_at
		// 为空，交给 StartGateSweeper 的补偿循环（HandleNewMembers 本身可能已被 30s
		// webhook 超时打断，在请求内重试只会加剧超时）。
		if err := cli.RestrictChatMember(ctx, chatID, m.ID, 0); err != nil {
			logger.Errorf("[TG-Gate] 禁言失败（已入补偿队列，清扫器将重试）account=%d user=%d: %v", accountID, m.ID, err)
			continue
		}

		welcome := gate.WelcomeMsg
		if welcome == "" {
			welcome = tgGateDefaultWelcome()
		}
		welcome = renderTGGateWelcome(welcome, tgUserDisplayName(&m), botUsername, token)
		if _, err := cli.SendMessage(ctx, chatID, welcome, telegram.SendMessageOptions{DisableMarkdownConversion: true}); err != nil {
			// 提示没送达 = 用户不知道要验证。禁言已生效、不致命，补发计数 +1 后由
			// 清扫器带 verify_token 补发（上限 tgGateWelcomeResendMax 次）。
			// 这条支路必须 continue：把"发失败"也登记成送达，等于同时关掉补偿循环
			// 的补发（查询只取未送达的行）和清扫器的免罚判据——人被判了刑却没收到传票。
			logger.Errorf("[TG-Gate] 群内验证提示发送失败（已入补偿队列，清扫器将补发）account=%d chat=%s: %v", accountID, chatIDStr, err)
			s.bumpWelcomeResend(ctx, accountID, chatIDStr, strconv.FormatInt(m.ID, 10))
			continue
		}
		s.markWelcomeSent(ctx, accountID, chatIDStr, strconv.FormatInt(m.ID, 10))
	}
	return handled
}

// markWelcomeSent 登记"这个人的验证提示已经送达"。
//
// 送达登记是重复播报的唯一刹车：补偿循环只补 welcome_sent_at 为空的行，
// 写失败会让下一个人也被重播，所以失败要出声。
func (s *TelegramGateService) markWelcomeSent(ctx context.Context, accountID uint, chatID, userID string) {
	if err := s.memberRepo.MarkWelcomeSent(ctx, accountID, chatID, userID, time.Now()); err != nil {
		logger.Errorf("[TG-Gate] 提示送达登记失败 account=%d chat=%s user=%s: %v", accountID, chatID, userID, err)
	}
}

func (s *TelegramGateService) bumpWelcomeResend(ctx context.Context, accountID uint, chatID, userID string) {
	if err := s.memberRepo.BumpWelcomeResend(ctx, accountID, chatID, userID); err != nil {
		logger.Errorf("[TG-Gate] 提示补发计数失败 account=%d chat=%s user=%s: %v", accountID, chatID, userID, err)
	}
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
	if !s.wired() || from == nil {
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
		// 先落库再解禁：Authorized 翻 true 后，RecoverStalled/SweepExpired 都会
		// 跳过该成员，杜绝"补偿循环补禁言"与"/start 解禁"交叉把用户重新禁言
		// 一年的竞态（补偿循环与清扫器共用 authorized=false 过滤）。
		now := time.Now()
		member.Authorized = true
		member.AuthorizedAt = &now
		member.JoinStatus = model.TGMemberApproved
		if err := s.memberRepo.Update(ctx, member); err != nil {
			return fmt.Errorf("mark approved: %w", err)
		}
		if err := cli.UnrestrictChatMember(ctx, chatID, userID); err != nil {
			// 解禁失败必须回滚 approved：否则台账显示已放行而 TG 侧仍受限
			// （用户被告知"验证通过"却发不了言）。回滚后清扫器补偿循环会再试。
			member.Authorized = false
			member.AuthorizedAt = nil
			member.JoinStatus = model.TGMemberRestricted
			if uErr := s.memberRepo.Update(ctx, member); uErr != nil {
				logger.Errorf("[TG-Gate] 回滚 approved 失败（台账与 TG 侧可能不一致，需人工核对）chat=%s user=%s: %v", member.ChatID, member.UserID, uErr)
			}
			return fmt.Errorf("unrestrict chat=%s user=%s: %w", member.ChatID, member.UserID, err)
		}
	}

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

// tgPrivilegedStatus 群主/管理员：Bot 对他们做不了 restrict（TG 侧直接拒），
// 而门控的「先禁言 ⇒ 再发验证提示」这条链在禁言失败时就 continue，提示也就不发了。
func tgPrivilegedStatus(status string) bool {
	switch status {
	case "creator", "administrator":
		return true
	}
	return false
}

// exemptPrivilegedSpeaker 无台账的门控群发言人若是群主/管理员 ⇒ 补一条已授权台账并放行。
//
// 返回 true 表示已确认特权（调用方据此不再互锁、也不再走补发邀请）。
// 查不到 / API 出错 / 状态不可解析一律返回 false，维持既有互锁（宁可多拦不误放），
// 但红因必须进日志：这条链失效的表现就是「群里有人发言、外面什么都没回」，
// 静默返回 false 等于把本次报障原样留着。
func (s *TelegramGateService) exemptPrivilegedSpeaker(ctx context.Context, accountID uint, chatID, userID string) bool {
	chatIDInt, cerr := strconv.ParseInt(chatID, 10, 64)
	userIDInt, uerr := strconv.ParseInt(userID, 10, 64)
	if cerr != nil || uerr != nil {
		return false
	}
	cli, err := s.client(ctx, accountID)
	if err != nil {
		logger.Errorf("[TG-Gate] 特权发言人判定失败（Bot 客户端加载不了，继续按未验证处理）account=%d chat=%s user=%s: %v",
			accountID, chatID, userID, err)
		return false
	}
	res, err := cli.GetChatMember(ctx, chatIDInt, userIDInt)
	if err != nil {
		logger.Errorf("[TG-Gate] 特权发言人判定失败（getChatMember 打不通，继续按未验证处理）account=%d chat=%s user=%s: %v",
			accountID, chatID, userID, err)
		return false
	}
	status, _ := res["status"].(string)
	if !tgPrivilegedStatus(status) {
		return false
	}
	now := time.Now()
	if err := s.memberRepo.Upsert(ctx, &model.TelegramGroupMember{
		AccountID:    accountID,
		ChatID:       chatID,
		UserID:       userID,
		JoinStatus:   model.TGMemberApproved,
		JoinMode:     TGGateModeMuteUnlock,
		Authorized:   true,
		AuthorizedAt: &now,
	}); err != nil {
		logger.Errorf("[TG-Gate] 特权发言人台账写入失败（本次仍按未验证互锁）account=%d chat=%s user=%s status=%s: %v",
			accountID, chatID, userID, status, err)
		return false
	}
	logger.Infof("[TG-Gate] 群主/管理员在门控群无台账 ⇒ 直接授权放行 account=%d chat=%s user=%s status=%s",
		accountID, chatID, userID, status)
	return true
}

// MemberUnverified 判断成员在指定群是否处于未过验证状态（pending/restricted/kicked 且未激活）。
// 用于门控群 AI 互锁：未验证成员的群发言不触发销售 AI / 线索商机挖掘。
// 语义：群有启用中的 mute_unlock 门控但该成员无台账 → 视为门控生效前已在群的
// 历史成员/绕过入群事件的人，同样拦截（gateWhitelisted 为 false）；
// 群无门控或成员已验证 → 放行。
func (s *TelegramGateService) MemberUnverified(ctx context.Context, accountID uint, chatID, userID string) bool {
	if !s.wired() {
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
	if gerr != nil || gate == nil || !gate.Enabled || gate.Mode != TGGateModeMuteUnlock {
		return false
	}
	// 门控装群之前就在线上的群主/管理员：他们永远不会产生入群事件，而补发邀请那条路
	// 要先禁言（Bot 无权动群主 ⇒ 必失败 ⇒ 连邀请都不发）⇒ 互锁对他是终身的，
	// 群里他说话外面一声不响。先问一次 TG 侧的真实身份，特权就直接授权放行。
	if s.exemptPrivilegedSpeaker(ctx, accountID, chatID, userID) {
		return false
	}
	return true
}

// MemberLacksLedger 该发言人在门控群里**一行台账都没有**。
//
// 与 MemberUnverified 的分工：后者把"有行但未验证"和"根本没有行"合并成同一个 true，
// 而这两件事的处置完全不同——前者是他自己没点验证链接，后者是门控装群时他早已在群里，
// 系统从未给他发过验证入口（提示只在 new_chat_members 事件发，
// sendPendingVerificationSummary 也自认 token "无法凭空重建"）⇒ 互锁对他永久成立。
// 调用方据此只对后者补发邀请，不重复骚扰前者。
func (s *TelegramGateService) MemberLacksLedger(ctx context.Context, accountID uint, chatID, userID string) bool {
	if !s.wired() {
		return false
	}
	if m, err := s.memberRepo.Get(ctx, accountID, chatID, userID); err == nil && m != nil {
		return false
	}
	gate, gerr := s.gateRepo.GetByChatID(ctx, accountID, chatID)
	return gerr == nil && gate != nil && gate.Enabled && gate.Mode == TGGateModeMuteUnlock
}

// MemberVerified 正面回答「这名发言人是启用中的禁言解锁门控群里的已验证成员」。
//
// 它不能写成 !MemberUnverified：后者返回 false 还包含三种"跟验证无关"的情形——
// 群没装门控、台账里是已退群的人、服务未装配。宽松触发策略（群内问话不必 @ 机器人
// 也交给智能体）一旦建在取反上，就会把这三种群/人也一并泼进去，覆盖面远超门控群。
// 只认 mute_unlock 这一种门控：join_request 群没过审的人根本进不来说话，
// 那条路上没有"已验证成员闲聊"这个问题，不在本策略内。
func (s *TelegramGateService) MemberVerified(ctx context.Context, accountID uint, chatID, userID string) bool {
	if !s.wired() {
		return false
	}
	gate, gerr := s.gateRepo.GetByChatID(ctx, accountID, chatID)
	if gerr != nil || gate == nil || !gate.Enabled || gate.Mode != TGGateModeMuteUnlock {
		return false
	}
	m, err := s.memberRepo.Get(ctx, accountID, chatID, userID)
	if err != nil || m == nil {
		return false
	}
	return m.Authorized || m.JoinStatus == model.TGMemberApproved
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
	member, err := s.memberRepo.GetMemberByID(ctx, memberID)
	if err != nil {
		return err
	}
	if member.Authorized {
		return nil
	}
	return s.AuthorizeMember(ctx, member)
}

// tgGateWelcomeResendMax 群内验证提示的最大补发次数（含入群时的首次尝试）。
// 上限存在的理由：提示发不出去一般不是抖动，而是 Bot 在这个群里没了发言权限；
// 对这种成员每分钟重发一次只会把群刷屏（线上实测 6 名成员被重播 219 条）。
const tgGateWelcomeResendMax = 3

// RecoverStalled 入群响应补偿循环（可靠性的最后兜底）。
//
// HandleNewMembers 在 webhook 请求内同步执行，TG API 网络抖动可能让禁言或
// 验证提示失败。台账里 restricted 但"提示从未送达"的成员，若不补偿就会静默
// 卡死（用户没人管，体验=入群没人响应）。
//
// 取数判据是 welcome_sent_at 而不是 expires_at：ListStalledRestricted 只返回"提示
// 从未送达"的 restricted 成员，所以已被告知的人根本进不了这个循环——一次播报都不发，
// 计时也不再被顶回去（历史上"临近到期"被当成"提示没送达"，每个 TTL 给同一个人重播
// 一次群消息，且重播顺手把 expires_at 顶回去，TTL 清理对这个人永不触发）。
//
// 进来的人两条路：
//   - 补发未到上限：补禁言 + 补发，成功登记送达并把计时改为"送达时刻 + TTL"（验证窗口
//     从他真被告知那刻起算），失败计一次补发数；
//   - 已到上限：只停手，让 expires_at 自然到期，由清扫器按"从未送达"不对他处置。
//
// 每次最多处理 limit 条，避免清扫周期被拖垮。
func (s *TelegramGateService) RecoverStalled(ctx context.Context, limit int) {
	if !s.wired() {
		return
	}
	// 窗口还开着的人才是补偿对象：窗口已经走完的交给 SweepExpired 的到期口径，
	// 不要在这里再走一遍"发不出去就计数"的空转。
	stalled, err := s.memberRepo.ListStalledRestricted(ctx, time.Now(), limit)
	if err != nil || len(stalled) == 0 {
		return
	}
	for _, m := range stalled {
		chatID, _ := strconv.ParseInt(m.ChatID, 10, 64)
		userID, _ := strconv.ParseInt(m.UserID, 10, 64)
		if chatID == 0 || userID == 0 {
			continue
		}
		// 按 chat+user 找群配置（每个成员单独取，容忍个别群配置已删）
		gate, gerr := s.gateRepo.GetByChatID(ctx, m.AccountID, m.ChatID)
		if gerr != nil || gate == nil || !gate.Enabled {
			continue
		}
		ttl := normalizeTGGateTTL(gate.VerifyTTLMin)

		// 补发已到上限＝这条链路上发不出去（Bot 被移出群、chat 不可达之类）。继续
		// 重发只会刷屏，也不该把计时顶回去：让 expires_at 自然到期，清扫器会认出
		// "提示从未送达"而不对他处置。
		if m.WelcomeResends >= tgGateWelcomeResendMax {
			logger.Warnf("[TG-Gate] 提示补发已到上限（%d 次）：停止播报 chat=%s user=%s",
				tgGateWelcomeResendMax, m.ChatID, m.UserID)
			continue
		}

		cli, cerr := s.client(ctx, m.AccountID)
		if cerr != nil {
			return // 账号级故障（token/DB），本轮放弃
		}
		// 原子认领：webhook 重试 / 多个清扫器实例可能同时盯上同一行，
		// 先 List 再发拦不住对方——认领失败说明已被认领/已送达/已到上限，直接跳过。
		// 认领已把本次尝试计入 welcome_resends，后续失败支路不再 bump。
		claimed, clerr := s.memberRepo.ClaimStalledResend(ctx, m.ID, m.WelcomeResends, tgGateWelcomeResendMax)
		if clerr != nil {
			logger.Warnf("[TG-Gate] 补偿认领失败 chat=%s user=%s: %v", m.ChatID, m.UserID, clerr)
			continue
		}
		if !claimed {
			continue
		}
		// 幂等补禁言：若首次禁言就成功，重复 restrict 只是幂等写，无副作用
		if err := cli.RestrictChatMember(ctx, chatID, userID, 0); err != nil {
			logger.Warnf("[TG-Gate] 补偿禁言失败 chat=%s user=%s: %v", m.ChatID, m.UserID, err)
			continue
		}
		botUsername := s.botUsername(ctx, m.AccountID)
		welcome := gate.WelcomeMsg
		if welcome == "" {
			welcome = tgGateDefaultWelcome()
		}
		welcome = renderTGGateWelcome(welcome, m.FullName, botUsername, m.VerifyToken)
		if _, err := cli.SendMessage(ctx, chatID, welcome, telegram.SendMessageOptions{DisableMarkdownConversion: true}); err != nil {
			logger.Warnf("[TG-Gate] 补偿提示发送失败 chat=%s user=%s: %v", m.ChatID, m.UserID, err)
			continue // 认领时已计数，到上限后不再重发
		}
		s.markWelcomeSent(ctx, m.AccountID, m.ChatID, m.UserID)
		s.retimeGateWindow(ctx, m, ttl)
		logger.Infof("[TG-Gate] 补偿完成：已补禁言+补发提示 chat=%s user=%s", m.ChatID, m.UserID)
	}
}

// retimeGateWindow 把验证窗口改为"现在 + TTL"，只在补偿循环真把提示发出去之后调用：
// 倒计时从被告知的时刻起算才成立，入群时那次写入可能对应一条根本没送达的消息。
// 除此之外不再有"重新计时"这条路——无限续期就是 TTL 清理永不触发的原因。
func (s *TelegramGateService) retimeGateWindow(ctx context.Context, m *model.TelegramGroupMember, ttlMinutes int) {
	newExp := time.Now().Add(time.Duration(ttlMinutes) * time.Minute)
	if err := s.memberRepo.RetimeVerification(ctx, m.ID, newExp); err != nil {
		logger.Errorf("[TG-Gate] 计时刷新失败 id=%d: %v", m.ID, err)
	}
}

// SweepExpired TTL 到期处置：方案 A declineChatJoinRequest；方案 B 移出群（仍可再次入群）并落 kicked。
//
// 方案 B 的判据有两层，两层都只在"别惩罚没被告知的人"这一条上收紧：
//  1. welcome_sent_at 为空 —— 验证提示从没送达（入群时发送失败、补发又到上限），
//     这个人超时不是他的过错，不处置，继续由补偿循环维持禁言与计时；
//  2. 提示送达过、verify_ttl_min 的窗口也确实过去了 —— 再宽限一个 TTL 才移人。
//     在群里点开链接、跳私聊、发 /start 这条链路本来就慢，一次窗口没点不等于不想入群。
//
// 方案 A 不受这两层约束：decline 只把申请退回"可以再次申请"，人既没被移出也没被拉黑。
//
// 台账重读失败时本轮直接跳过：处置判据（是否送达、何时到期）只能来自重读出来的那一行，
// 读不出来就不拿二手数据对真人动手。
func (s *TelegramGateService) SweepExpired(ctx context.Context, limit int) (int, error) {
	if !s.wired() {
		return 0, nil
	}
	s.RecoverStalled(ctx, 50) // 补偿：提示从未送达的禁言成员
	now := time.Now()
	expired, err := s.memberRepo.ListExpired(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	swept := 0
	for _, member := range expired {
		// 处置前按主键重读：成员可能刚在过期那一瞬被 /start 放行，也可能送达状态刚变。
		// 重读要认主键——处置对象就是这一行，而 verify_token 是会被重新入群改写的业务列，
		// 拿一个会被改写的列当"我还是不是我要动的那一行"的依据，判据就挂在会动的东西上了。
		fresh, err := s.memberRepo.GetMemberByID(ctx, member.ID)
		if err != nil || fresh == nil {
			continue
		}
		if fresh.Authorized {
			continue // 已过审：上一行的放行路径负责解禁，这里不重复动他
		}
		chatID, _ := strconv.ParseInt(fresh.ChatID, 10, 64)
		userID, _ := strconv.ParseInt(fresh.UserID, 10, 64)
		if chatID == 0 || userID == 0 {
			continue
		}
		if fresh.JoinMode != TGGateModeJoinRequest {
			// 方案 B：没送达过提示的人不罚
			if fresh.WelcomeSentAt == nil {
				logger.Debugf("[TG-Gate] 提示从未送达，超时也不处置 chat=%s user=%s", fresh.ChatID, fresh.UserID)
				continue
			}
			gate, gerr := s.gateRepo.GetByChatID(ctx, fresh.AccountID, fresh.ChatID)
			if gerr != nil || gate == nil {
				continue // 群配置已删：没有配置可依据，就不该替他做移出决定
			}
			grace := time.Duration(normalizeTGGateTTL(gate.VerifyTTLMin)) * time.Minute
			if fresh.ExpiresAt != nil && now.Sub(*fresh.ExpiresAt) < grace {
				logger.Debugf("[TG-Gate] 已到期但在宽限期内（再等 %d 分钟）chat=%s user=%s", int64(grace/time.Minute), fresh.ChatID, fresh.UserID)
				continue
			}
		}
		cli, err := s.client(ctx, fresh.AccountID)
		if err != nil {
			continue
		}
		if fresh.JoinMode == TGGateModeJoinRequest {
			if err := cli.DeclineChatJoinRequest(ctx, chatID, userID); err != nil {
				logger.Warnf("[TG-Gate] decline 超时申请失败 chat=%s user=%d: %v", fresh.ChatID, userID, err)
			}
		} else {
			// 踢出（untilDate=过去时间 → 只踢不拉黑，用户可再次申请加入）
			if err := cli.BanChatMember(ctx, chatID, userID, now.Add(-time.Minute).Unix()); err != nil {
				logger.Warnf("[TG-Gate] 踢出超时成员失败 chat=%s user=%d: %v", fresh.ChatID, userID, err)
				continue // TG 侧没动成功就别把台账写成 kicked，下轮还会重试
			} else {
				// Telegram 实际行为：banChatMember 过去时间 = 永久拉黑。补一次 unban
				// 解除拉黑，保留"只踢不拉黑、可再次申请加入"的产品语义
				if err := cli.UnbanChatMember(ctx, chatID, userID); err != nil {
					logger.Warnf("[TG-Gate] 解除拉黑失败（仍为拉黑状态，用户无法再次加入）chat=%s user=%d: %v", fresh.ChatID, userID, err)
				}
			}
			logger.Infof("[TG-Gate] 超时未验证已移出（可重新入群）chat=%s user=%s 提示送达于=%s",
				fresh.ChatID, fresh.UserID, fresh.WelcomeSentAt.Format("2006-01-02 15:04:05"))
		}
		if err := s.memberRepo.MarkKicked(ctx, fresh.ID); err != nil {
			logger.Errorf("[TG-Gate] kicked 状态回写失败 id=%d: %v", fresh.ID, err)
		}
		swept++
	}
	return swept, nil
}

// normalizeTGGateTTL 群配置的验证窗口（分钟），非正值按 10 分钟兜底。
func normalizeTGGateTTL(minutes int) int {
	if minutes <= 0 {
		return 10
	}
	return minutes
}

// 清扫周期与租约作业名。
//
// tgGateSweeperLeaseJob 是 cron_job_leases 里这一路的键：清扫器会**对真人动手**
// （移出群 / 拒绝申请），而同一套库上可以同时住着多个进程（本地开发常见的
// "正式实例 + air 实例 + 手工起的旧二进制"）。没有跨进程互斥时每个实例各自
// 每分钟广播一遍，入群提示的重复播报量就是实例数的整数倍。
const (
	gateSweeperInterval    = time.Minute
	gateSweeperTickTimeout = 30 * time.Second
	gateSweeperBatchLimit  = 100
	tgGateSweeperLeaseJob  = "telegram_gate_sweeper"
)

// gateSweeperRunner 后台协程的关停把手（cancel 停 ticker，done 表示协程真退出了）。
type gateSweeperRunner struct {
	workerID string
	svc      *TelegramGateService
	interval time.Duration
	cancel   context.CancelFunc
	done     chan struct{}
}

// 清扫器是进程级单实例：全局状态只有这里这一处，用锁守住"检查+赋值"这个动作，
// Stop/自愈重启/重复 Start 三条路径都从同一把锁进。
var (
	gateSweeperMu sync.Mutex
	gateSweeper   *gateSweeperRunner
)

// sweepOnce 执行一轮门控清扫，返回 (处置数, 本轮是否由本进程执行, 错误)。
//
// 先抢/续租约，抢不到就直接返回、一次 TG 调用都不发——这条判据只有在"非持有者
// 零动作"上成立才有意义，所以把租约判定和 SweepExpired 的先后顺序放进一个能被
// 用例直接调用的函数里（等一分钟的 ticker 不是测试）。
//
// leaseRepo 为 nil＝没有跨进程协调的能力（无库/未装配），退回单实例语义直接跑。
func sweepOnce(ctx context.Context, svc *TelegramGateService, workerID string) (int, bool, error) {
	if svc == nil {
		return 0, false, nil
	}
	if svc.leaseRepo == nil {
		n, err := svc.SweepExpired(ctx, gateSweeperBatchLimit)
		return n, true, err
	}
	held, err := svc.leaseRepo.Hold(ctx, tgGateSweeperLeaseJob, workerID)
	if err != nil {
		return 0, false, err
	}
	if !held {
		return 0, false, nil
	}
	n, err := svc.SweepExpired(ctx, gateSweeperBatchLimit)
	return n, true, err
}

// StartGateSweeper 启动后台 TTL 清扫协程；调用方在路由注册后直接调用（内部 go routine），
// 每分钟执行一次 SweepExpired。db 为 nil 时直接返回（测试/无 DB 场景）。
//
// 与 polling 锁共用 worker 标识（hostname:pid），日志里两个"谁在跑"能对上是同一个进程。
// 成对的 StopGateSweeper 必须在进程关停时调用（main.go 里 defer）。
func StartGateSweeper(db *gorm.DB) {
	if db == nil {
		return
	}
	workerID := GetPollingWorkerID()
	startGateSweeper(NewTelegramGateService(db), workerID)
	logger.Infof("[TG-Gate] TTL 清扫器已启动（每分钟一次，租约 worker=%s）", workerID)
}

func startGateSweeper(svc *TelegramGateService, workerID string) {
	startGateSweeperAt(svc, workerID, gateSweeperInterval)
}

// startGateSweeperAt 登记一台清扫器并指定节拍。
//
// 节拍做成参数只有一个原因：一分钟一次的线上节拍进不了用例，而"注册了协程 ⇒ 它自己抢租约、
// 真跑一轮清扫、Stop 时把租约交回"这一整条只能在毫秒节拍上观测到。
func startGateSweeperAt(svc *TelegramGateService, workerID string, interval time.Duration) {
	gateSweeperMu.Lock()
	defer gateSweeperMu.Unlock()
	if gateSweeper != nil {
		return // 幂等：重复启动不换实例，否则旧 ticker 泄漏成两条并行清扫流
	}
	runCtx, cancel := context.WithCancel(context.Background())
	r := &gateSweeperRunner{workerID: workerID, svc: svc, interval: interval, cancel: cancel, done: make(chan struct{})}
	gateSweeper = r
	go r.tickLoop(runCtx)
}

// tickLoop 清扫主循环（节拍来自 runner.interval，线上是 gateSweeperInterval）。
func (r *gateSweeperRunner) tickLoop(ctx context.Context) {
	defer close(r.done)
	defer func() {
		rec := recover()
		if rec == nil {
			return
		}
		logger.Errorf("[TG-Gate] TTL 清扫协程 panic 重启: %v", rec)
		r.restartAfterPanic(ctx)
	}()
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickCtx, cancelTick := context.WithTimeout(ctx, gateSweeperTickTimeout)
			swept, ran, err := sweepOnce(tickCtx, r.svc, r.workerID)
			cancelTick()
			switch {
			case err != nil:
				logger.Errorf("[TG-Gate] TTL 清扫失败: %v", err)
			case !ran:
				logger.Debugf("[TG-Gate] 清扫租约在别的实例名下，本轮零动作 worker=%s", r.workerID)
			case swept > 0:
				logger.Infof("[TG-Gate] TTL 清扫完成，处理 %d 个超时成员", swept)
			}
		}
	}
}

// restartAfterPanic 决定 panic 之后要不要把清扫器复活、以及怎么复活。
//
// 拆成方法只为了一件事：这两条分支各自拦一种事故，而它们只有在"直接调用"上才能各判各的
// ——走一分钟一次的 ticker 时，"关停窗口里不复活"这一条要等 panic 恰好落在 cancel 之前，
// 那是运气而不是判据。
//   - 正常运行中死掉却不复活 ⇒ 台账从此不再清理，而日志只留下一行 panic（静默失效）；
//   - 关停窗口里复活 ⇒ StopGateSweeper 已等旧协程退出并交回租约，新协程又用同一个 workerID
//     抢回租约继续对真人动手 —— 进程"已经停了"却还在踢人。
//
// 先摘全局再登记：全局指向的仍是这台已死实例时置空，避免复活出来的那台被当成重复启动丢掉。
func (r *gateSweeperRunner) restartAfterPanic(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	gateSweeperMu.Lock()
	if gateSweeper == r {
		gateSweeper = nil
	}
	gateSweeperMu.Unlock()
	startGateSweeperAt(r.svc, r.workerID, r.interval) // 沿用原节拍
}

// StopGateSweeper 进程关停时调用（与 StartGateSweeper 成对，main.go 里 defer）。
//
// 先取消 ticker 并等协程真退出，再释放租约——顺序反了就会出现"本进程还打着 TG、
// 另一个实例已经抢到租约开始移人"的双跑窗。等不到退出就不释放：租约会自然过期
// （repository.cronLeaseStaleAfter），只是下一个实例多等一轮，比双跑便宜。
func StopGateSweeper(ctx context.Context) {
	gateSweeperMu.Lock()
	r := gateSweeper
	gateSweeper = nil
	gateSweeperMu.Unlock()
	if r == nil {
		return
	}
	r.cancel()
	select {
	case <-r.done:
	case <-ctx.Done():
		logger.Warnf("[TG-Gate] 清扫协程未在关停窗口内退出，保留租约待其自然过期 worker=%s", r.workerID)
		return
	}
	if r.svc != nil && r.svc.leaseRepo != nil {
		if err := r.svc.leaseRepo.Release(ctx, tgGateSweeperLeaseJob, r.workerID); err != nil {
			logger.Warnf("[TG-Gate] 清扫租约释放失败（将自然过期）worker=%s: %v", r.workerID, err)
		}
	}
	logger.Infof("[TG-Gate] TTL 清扫器已停止 worker=%s", r.workerID)
}
