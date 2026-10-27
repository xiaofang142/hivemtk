package service

import (
	"context"
	"errors"
	"fmt"
	smssvc "hivemtk-user/internal/service/sms"
	"strings"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/reach"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// ErrNoChannelIdentity / ErrNoActiveAccount 选不出渠道的两类根因哨兵。
// recovery_queue_worker 按 errors.Is 分诊（2026-10-25 前是字符串匹配，CONVENTIONS §3.3 反模式）。
var (
	// ErrNoChannelIdentity 客户没有任何渠道身份
	ErrNoChannelIdentity = errors.New("no channel identity")
	// ErrNoActiveAccount 有渠道身份但无可用账号
	ErrNoActiveAccount = errors.New("no active account")
)

// 外发被拦的两类原因用哨兵错误表达，供调用方分支判断。
//
// 为什么单独定义：队列消费侧的调用方（挽回队列 worker，见 recovery_queue_worker.go）
// 必须区分"对方退订了，这条该终止"和"刚发过，稍后再试"，
// 两者的队列处置完全相反（cancelled vs 退避重试）。只靠 fmt.Errorf 的文案区分，
// 就等于把控制流建在日志字符串上——改一句措辞就会静默改变队列行为。
// 包装后 Error() 前缀与原来逐字一致，字符串匹配的既有调用方不受影响。
var (
	// ErrDoNotContact 命中全局退订标志位：该收件人不可再外发。
	// 值已下沉到 internal/pkg/reach（按域拆包时子包要能直接引用，见该包注释）；
	// 这里保留别名，既有调用点与本包内 errors.Is 判据不变。
	ErrDoNotContact = reach.ErrDoNotContact
	// ErrReachCooldown 冷却期内已发过：稍后可重试
	ErrReachCooldown = errors.New("cooldown")
	// ErrReachApprovalDenied 未获外发授权：与 DNC 相反，它是**可补的**（加白名单即可重发），
	// 所以调用方必须能与另两个哨兵区分开——队列侧据此决定"退避重试"还是"终止"。
	ErrReachApprovalDenied = errors.New("reach_approval_denied")
	// ErrReachComplianceDenied E1：命中地区×行业合规策略。
	//
	// 与 ErrReachApprovalDenied 的区别是**谁来解**：授权是被人工逐个加白名单（等不了多久），
	// 合规是策略层面的（改地区/行业策略或换渠道，通常不是"等一会儿就好"）。所以两者必须是
	// 两个哨兵：队列侧对前者退避、对后者终止 + 报原因码，混成一个就是让运营去猜。
	ErrReachComplianceDenied = errors.New("reach_compliance_denied")
)

// ReachSubject 发送前审批的判定对象。
//
// Key 是裁决用的唯一键，其余字段是给人看的上下文：闸门报告要能回答"这次要发到哪"，
// 只有一把 one_id 不够。
type ReachSubject struct {
	// Key 判定键，见 reachApprovalKey：one_id > customer_id > "渠道:收件人"，恒非空才有效
	Key string
	// OneID 客户 OneID（可能为空：直发手机号/邮箱时不查客户）
	OneID string
	// CustomerID 客户主键（可能为空，同上）
	CustomerID string
	// Channel 已选定的外发渠道
	Channel string
	// Recipient 已选定的收件人标识
	Recipient string
	// AccountID 渠道账号ID；短信/邮箱/钉钉渠道恒为空，**因此它不参与判定键**
	AccountID string
	// Region / Industry / OptIn E1：合规策略判定的地区/行业/同意状态，
	// 由 ProactiveReachRequest 的同名字段透传（缺省即空 ⇒ 走策略默认地区）。
	Region   string
	Industry string
	OptIn    bool
}

// ReachPreSendChecker 发送前审批钩子：在所有外发渠道的唯一出口上决定是否放行。
//
// 裁决来源对 service 层不可见（当前是 internal/app 接的 W-1 白名单，将来可以是审批工单表），
// 所以这里只有一个布尔 + 一个理由串。
type ReachPreSendChecker interface {
	CheckReachPreSend(ctx context.Context, sub ReachSubject) (allowed bool, reason string)
}

// ProactiveReachRequest 主动触达请求（按 OneID 智能选渠道）
type ProactiveReachRequest struct {
	// CustomerID 客户主键 ID（从线索/客户库中获取）
	CustomerID string `json:"customer_id,omitempty"`
	// OneID 客户 OneID（与 CustomerID 二选一）
	OneID string `json:"one_id,omitempty"`
	// Phone 手动指定手机号（最高优先级，绕过智能选渠道）
	Phone string `json:"phone,omitempty"`
	// Email 手动指定邮箱
	Email string `json:"email,omitempty"`
	// AccountID 渠道账号ID（用于出站发送）
	AccountID string `json:"account_id,omitempty"`
	// PreferredChannels 客户偏好渠道（按顺序）
	PreferredChannels []string `json:"preferred_channels,omitempty"`
	// Content 消息内容
	Content string `json:"content" binding:"required"`
	// Subject 邮件主题
	Subject string `json:"subject,omitempty"`
	// TemplateID 模板ID（邮件/短信/WA 模板）
	TemplateID string `json:"template_id,omitempty"`
	// Params 模板参数
	Params map[string]string `json:"params,omitempty"`
	// Region E1：收件人所在地区（大写，如 US/CN）。
	// 不填则回落参数中心策略文档的 default_region；两者都空即「无地区规则」。
	// 刻意不给客户模型加地区字段：那要连带改导入链路与客户档案页，是另一张卡的事。
	Region string `json:"region,omitempty"`
	// Industry E1：所属行业（小写，如 medical/finance），对应策略文档的 industries 键。
	Industry string `json:"industry,omitempty"`
	// OptIn E1：是否已取得明确同意（医美/金融等 require_opt_in 规则必填）。
	OptIn bool `json:"opt_in,omitempty"`
	// DryRun 试运行
	DryRun bool `json:"dry_run,omitempty"`
}

// ProactiveReachResponse 主动触达响应
type ProactiveReachResponse struct {
	// MessageID 消息ID
	MessageID string `json:"message_id"`
	// Channel 实际使用的渠道
	Channel string `json:"channel"`
	// RecipientID 实际接收方 ID
	RecipientID string `json:"recipient_id"`
	// AccountID 渠道账号ID
	AccountID string `json:"account_id"`
	// Status 状态
	Status string `json:"status"`
	// SentAt 发送时间
	SentAt time.Time `json:"sent_at"`
	// Strategy 选渠道策略说明（透明化）
	Strategy string `json:"strategy"`
	// HubID（D-9 回执契约，2026-10-23）：message_hub 行号。仅 bridge 渠道
	// （走 DeliverBridgeOutbound、先落 hub 行再下发）有值；sms/email 等直发
	// 渠道没有 hub 行，此键缺省（omitempty）。平台号 vs hub 行号的合同：
	//   - MessageID：发送腿返回的回执键（bridge 渠道 = hub 行 MsgID，即
	//     tg-out-.../wamid/feishu-out-... 或内容哈希；直发渠道 = 平台回执 ID）。
	//     Recall 优先用它（IntegrationReachAdapter.Recall 按 msg_id 反查 hub 行）。
	//   - HubID：hub 行号，用于管理端定位/对账；无 hub 行的渠道不出现。
	HubID uint64 `json:"hub_id,omitempty"`
	// PlatformMsgID 平台侧消息号（平台返回后才存在；发送时通常还没有 ⇒ 常为空）。
	// 出现时与 message_hub.msg_id（平台号回写后）一致。
	PlatformMsgID string `json:"platform_msg_id,omitempty"`
}

type ProactiveReachService struct {
	repo          *repository.ProactiveReachRepository
	customerRepo  *customerRepo
	accountLookup AccountLookup

	dnc *DoNotContactService

	// preSendGate 发送前审批钩子；nil 表示"没装闸门"，外发行为与接线前一致。
	preSendGate ReachPreSendChecker

	smsRegistry      func() (func(ctx context.Context, phone, content, templateID string, params map[string]string) (string, error), error)
	emailRegistry    func(ctx context.Context, accountID uint, to, subject, content string, attachments []string) (string, error)
	telegramRegistry func(ctx context.Context, accountID uint, chatID int64, content string) error
	whatsAppRegistry func(ctx context.Context, accountID uint, toPhone, content string) error
	weComRegistry    func(ctx context.Context, accountID uint, externalUserID, msgType, content string, isAIReply bool, agent string) (string, error)
	feishuRegistry   func(ctx context.Context, accountID uint, openID, content, receiveIDType string) error
	dingTalkRegistry func(ctx context.Context, webhookOrToken, secret, msgType, content string) (string, error)
	wechatRegistry   func(ctx context.Context, accountID uint, openID, msgType, content string) (string, error)
}

// SetSMSRegistry 设置 SMS 发送器
func (s *ProactiveReachService) SetSMSRegistry(fn func() (func(ctx context.Context, phone, content, templateID string, params map[string]string) (string, error), error)) {
	s.smsRegistry = fn
}

// SetEmailRegistry 设置 Email 发送器
func (s *ProactiveReachService) SetEmailRegistry(fn func(ctx context.Context, accountID uint, to, subject, content string, attachments []string) (string, error)) {
	s.emailRegistry = fn
}

// SetTelegramRegistry 设置 Telegram 发送器
func (s *ProactiveReachService) SetTelegramRegistry(fn func(ctx context.Context, accountID uint, chatID int64, content string) error) {
	s.telegramRegistry = fn
}

// SetWhatsAppRegistry 设置 WhatsApp 发送器
func (s *ProactiveReachService) SetWhatsAppRegistry(fn func(ctx context.Context, accountID uint, toPhone, content string) error) {
	s.whatsAppRegistry = fn
}

// SetWeComRegistry 设置企微发送器
func (s *ProactiveReachService) SetWeComRegistry(fn func(ctx context.Context, accountID uint, externalUserID, msgType, content string, isAIReply bool, agent string) (string, error)) {
	s.weComRegistry = fn
}

// SetFeishuRegistry 设置飞书发送器
func (s *ProactiveReachService) SetFeishuRegistry(fn func(ctx context.Context, accountID uint, openID, content, receiveIDType string) error) {
	s.feishuRegistry = fn
}

// SetDingTalkRegistry 设置钉钉发送器
func (s *ProactiveReachService) SetDingTalkRegistry(fn func(ctx context.Context, webhookOrToken, secret, msgType, content string) (string, error)) {
	s.dingTalkRegistry = fn
}

// SetWechatRegistry 设置微信公众号发送器
func (s *ProactiveReachService) SetWechatRegistry(fn func(ctx context.Context, accountID uint, openID, msgType, content string) (string, error)) {
	s.wechatRegistry = fn
}

// AccountLookup 渠道账号查找接口
type AccountLookup interface {
	// FindActiveAccount 查找某渠道下第一个 active 账号
	FindActiveAccount(ctx context.Context, channel string) (accountID string, err error)
}

// NewProactiveReachService 创建主动触达服务。
//
// 依赖全部由装配点注入（仓库对象而非 DB 句柄，见五层架构 §3.2）：
//   - repo：客户/偏好渠道/账号查找；
//   - dncRepo：退订标志位。**必须与 repo 用同一句柄**——曾经这里靠"传 db 或不传"来表达
//     同一件事，于是"注入式装配"名不副实：同一个服务读客户用参数库、读退订用全局库，
//     两个库不是同一个句柄时退订检查会打到错的库。现在这件事由装配点显式决定（传 nil
//     则退到 NewDoNotContactService 的默认路径）。
//   - lookup：账号查找口（nil 时用默认实现）。
func NewProactiveReachService(repo *repository.ProactiveReachRepository, dncRepo repository.CustomerDoNotContactRepository, lookup AccountLookup) *ProactiveReachService {
	if repo == nil {
		// 与"拿到 nil 句柄"的历史语义逐字一致：仓库在位但无连接，读到的是一条明确错误
		// （或空结果）而不是 panic。
		repo = repository.NewProactiveReachRepository(nil)
	}
	if lookup == nil {
		lookup = &defaultAccountLookup{repo: repo}
	}
	return &ProactiveReachService{
		repo:          repo,
		customerRepo:  newCustomerRepo(repo),
		accountLookup: lookup,
		dnc:           NewDoNotContactService(dncRepo),
	}
}

// SetDoNotContact 注入全局退订标志位服务（测试或自定义装配时使用）
func (s *ProactiveReachService) SetDoNotContact(dnc *DoNotContactService) {
	s.dnc = dnc
}

// SetPreSendApprovalChecker 注入发送前审批闸门。
//
// 为什么是注入而不是构造参数：闸门依赖 W-1 的白名单与旗子，那些在 main 启动序列里
// 晚于本服务构造才就绪；与 SetSMSRegistry 等 8 个外发器同一套装配时序。
func (s *ProactiveReachService) SetPreSendApprovalChecker(gate ReachPreSendChecker) {
	s.preSendGate = gate
}

// PreSendApprovalChecker 读回当前装的闸门（nil = 未装），供接线层与快照接口判断"到底装上没"。
//
// 装配是否真的生效必须有可读回的地方，否则「测试里手动接、生产里没接」这类断链在进程内不可见
// ——W-1 当年的 AC③ 就是靠"生产可达性"断言堵的这个口。
func (s *ProactiveReachService) PreSendApprovalChecker() ReachPreSendChecker {
	return s.preSendGate
}

// reachApprovalKey 判定键：客户身份优先，退到"渠道:收件人"。
//
// 刻意不含 AccountID：短信/邮箱/钉钉渠道的 accountID 在装配里恒为空，用它当键会得到
// 一把恒空的白名单——判了等于没判（T-P1-05 的教训，本处的 AC②）。
func reachApprovalKey(oneID, customerID, channel, recipient string) string {
	if oneID != "" {
		return oneID
	}
	if customerID != "" {
		return customerID
	}
	if recipient == "" {
		return ""
	}
	return channel + ":" + recipient
}

// enforcePreSendApproval 外发前的最后一道授权判定。
//
// 返回非 nil 时调用方**必须**放弃外发：本函数在所有渠道的出口处被调用，
// 所以三个非工具路径（直接 API / cron / 工具）自动同受约束，不需要各自记得接。
func (s *ProactiveReachService) enforcePreSendApproval(ctx context.Context, sub ReachSubject) error {
	if s.preSendGate == nil {
		return nil
	}
	if sub.Key == "" {
		// 走到这里说明调用方没给出任何身份——fail-closed：宁可不发，也不能凭空放行。
		return fmt.Errorf("%w: 无法确定判定对象（缺少 one_id / customer_id / 收件人），外发已跳过", ErrReachApprovalDenied)
	}
	allowed, reason := s.preSendGate.CheckReachPreSend(ctx, sub)
	if allowed {
		return nil
	}
	if reason == "" {
		reason = "unspecified"
	}
	// 理由与渠道进日志（不含收件人），错误串里只带理由：这条错误会被调用方写进
	// recovery_queue.last_result 与 HTTP 响应，收件人标识一旦进串就等于落进台账。
	logger.Warnf("[ReachGate] 外发被拒 channel=%s key=%s reason=%s", sub.Channel, sub.Key, reason)
	return fmt.Errorf("%w: 收件对象未获外发授权（%s）", ErrReachApprovalDenied, reason)
}

// enforceOutboundGates 外发前的两道判据，顺序固定：授权（W-1）→ 合规（E1 地区×行业）。
//
// 为什么必须是一个函数而不是两个调用点并排写：三条外发调用点（手机号/邮箱/选路后）
// 各写两行，等于给「将来加第三道判据」预留了三次漏接的机会。出口只有这一个函数，
// 新增判据加在这里就自动覆盖全部渠道。
func (s *ProactiveReachService) enforceOutboundGates(ctx context.Context, sub ReachSubject) error {
	if err := s.enforcePreSendApproval(ctx, sub); err != nil {
		return err
	}
	return s.enforceCompliance(ctx, sub)
}

// enforceCompliance E1：外发前的合规策略判定（地区×行业）。
//
// 为什么挂在 enforcePreSendApproval **之后**（同一出口、两道判据）：
//   - 授权是"这个对象能不能发"，合规是"在这个地区/行业能不能用这个渠道发"；
//     先授权后合规，被拒的理由才是运营最该先解决的那一个；
//   - 两者都在唯一出口上判，工具/API/cron 三条路径自动同受约束，不需要各自记得接。
//
// 同步路径无可用的延迟队列，因此**静默窗口一律按拒绝返回**（带原因码 quiet_hours），
// 由上层（recovery queue / SOP 节点）按退避重排——把消息塞进一个不存在的队列才是真丢消息。
func (s *ProactiveReachService) enforceCompliance(ctx context.Context, sub ReachSubject) error {
	policy := GlobalCompliancePolicy()
	decision := policy.Decide(ctx, ComplianceInput{
		Channel:     sub.Channel,
		CustomerID:  sub.CustomerID,
		RecipientID: sub.Recipient,
		Region:      sub.Region,
		Industry:    sub.Industry,
		OptIn:       sub.OptIn,
	})
	recordComplianceDecisionEntry(ctx, &ReachComplianceLog{
		Channel:     sub.Channel,
		RecipientID: sub.Recipient,
		Decision:    decision.Decision(),
		Reason:      decision.Reason,
		Region:      decision.Region,
		Industry:    decision.Industry,
		PolicyScope: decision.PolicyScope,
	})
	if decision.Allowed && !decision.Deferred {
		return nil
	}
	reason := decision.Reason
	if decision.Deferred {
		// 同步路径不可延后：把「本可延后」这件事写进理由，排障才知道该等窗口还是该改策略。
		reason = fmt.Sprintf("%s(可延后至 %s)", decision.Reason, decision.SendAt.Format(time.RFC3339))
	}
	logger.Warnf("[COMPLIANCE] 外发被合规策略拦截 channel=%s region=%s industry=%s reason=%s scope=%s",
		sub.Channel, decision.Region, decision.Industry, decision.Reason, decision.PolicyScope)
	return fmt.Errorf("%w: %s", ErrReachComplianceDenied, reason)
}

func (s *ProactiveReachService) dncService() *DoNotContactService {
	if s.dnc == nil {
		s.dnc = NewDoNotContactService(nil)
	}
	return s.dnc
}

// customerRepo 客户读取的进程内适配（gorm 收敛到 repository.ProactiveReachRepository）
type customerRepo struct {
	repo *repository.ProactiveReachRepository
}

func newCustomerRepo(repo *repository.ProactiveReachRepository) *customerRepo {
	if repo == nil {
		repo = repository.NewProactiveReachRepository(nil)
	}
	return &customerRepo{repo: repo}
}

func (r *customerRepo) GetByID(ctx context.Context, id string) (*model.Customer, error) {
	return r.repo.GetCustomerByID(ctx, id)
}

func (r *customerRepo) GetByUnifiedID(ctx context.Context, oneID string) (*model.Customer, error) {
	return r.repo.GetCustomerByUnifiedID(ctx, oneID)
}

// ReachByCustomer 按客户主键或 OneID 智能选择渠道发送
//
// 选渠道策略（不猜测）：
//  1. 显式指定 Phone/Email → 直发该渠道
//  2. 从 Customer 读取所有渠道身份字段，列出有完整身份的渠道
//  3. 客户偏好渠道（CustomerChannels.preferred_rank）排在最前
//  4. 默认顺序：SMS > Email > TG > WhatsApp > 企微 > 微信 > 飞书 > 抖音 > ...
//  5. 第一个有 active 账号 + 完整身份的渠道即为最终选择
//  6. 全部不可用 → 返回明确错误（带详细原因）
func (s *ProactiveReachService) ReachByCustomer(ctx context.Context, req *ProactiveReachRequest) (resp *ProactiveReachResponse, err error) {
	if req == nil || req.Content == "" {
		return nil, errors.New("content is required")
	}

	// E1：成功外发后计一次当日触达（地区日上限用）。
	//
	// 用「命名返回值 + defer」而不是在每个渠道分支各写一行：本函数有 10 个 return 分支，
	// 逐个手写迟早漏一个，而漏掉的后果是「某渠道的日上限静默失效」——一条只在特定渠道
	// 才不生效的合规规则，比完全没规则更难发现。sentChannel 在真正委派前才赋值，
	// 因此被拒（DNC/授权/合规/冷却）与 dry-run 都不会计数。
	var sentChannel, sentRecipient string
	defer func() {
		if err != nil || resp == nil || sentChannel == "" {
			return
		}
		GlobalCompliancePolicy().RecordSentInput(ctx, ComplianceInput{
			Channel:     sentChannel,
			CustomerID:  req.CustomerID,
			RecipientID: sentRecipient,
			Region:      req.Region,
			Industry:    req.Industry,
		})
	}()

	if req.Phone != "" {
		if s.checkDoNotContact(ctx, "", "sms", req.Phone) {
			return nil, fmt.Errorf("%w: phone %s has opted out globally, send skipped", ErrDoNotContact, req.Phone)
		}
		if err := s.enforceOutboundGates(ctx, ReachSubject{
			Key:   reachApprovalKey(req.OneID, req.CustomerID, "sms", req.Phone),
			OneID: req.OneID, CustomerID: req.CustomerID,
			Channel: "sms", Recipient: req.Phone, AccountID: req.AccountID,
			Region: req.Region, Industry: req.Industry, OptIn: req.OptIn,
		}); err != nil {
			return nil, err
		}
		sentChannel, sentRecipient = "sms", req.Phone
		return s.sendSMS(ctx, req, req.Phone)
	}
	if req.Email != "" {
		normalized := NormalizeEmail(req.Email)
		if s.checkDoNotContact(ctx, "", "email", "email:"+normalized) {
			return nil, fmt.Errorf("%w: email %s has opted out globally, send skipped", ErrDoNotContact, req.Email)
		}
		if err := s.enforceOutboundGates(ctx, ReachSubject{
			Key:   reachApprovalKey(req.OneID, req.CustomerID, "email", normalized),
			OneID: req.OneID, CustomerID: req.CustomerID,
			Channel: "email", Recipient: normalized, AccountID: req.AccountID,
			Region: req.Region, Industry: req.Industry, OptIn: req.OptIn,
		}); err != nil {
			return nil, err
		}
		sentChannel, sentRecipient = "email", normalized
		return s.sendEmail(ctx, req, req.Email)
	}

	customer, err := s.loadCustomer(ctx, req.CustomerID, req.OneID)
	if err != nil {
		return nil, fmt.Errorf("load customer: %w", err)
	}
	if customer == nil {
		return nil, errors.New("customer not found: provide customer_id, one_id, phone, or email")
	}

	// 身份来源取「反规范化列 + customer_channels 绑定表」的并集：
	// 渠道绑定只落在绑定表里的客户（直接 SQL 播种、导入脚本、迁移）原先一律被判成
	// 「无渠道身份」，于是报价次发与主动触达在这条路上全线失败——见 CustomerIdentity 注释。
	ident, err := s.loadCustomerIdentity(ctx, customer)
	if err != nil {
		// 读绑定表失败不阻断外发：退回只认反规范化列是旧行为（保守、少发），
		// 而在这里报错会把「多写一份表」这种数据形态升级成外发全线不可用。
		logger.Warnf("[ProactiveReach] 读渠道绑定表失败，退回仅反规范化列 one_id=%s: %v", customer.UnifiedID, err)
		ident = NewCustomerIdentity(customer, nil)
	}

	available := ident.Available(req.PreferredChannels)
	if len(available) == 0 {
		return nil, fmt.Errorf("customer %s has no channel identity on file, please bind at least one channel", customer.UnifiedID)
	}

	available = s.filterDoNotContactChannels(ctx, customer.UnifiedID, available)
	if len(available) == 0 {
		return nil, fmt.Errorf("%w: customer %s has opted out on all available channels", ErrDoNotContact, customer.UnifiedID)
	}

	preferred, _ := s.loadCustomerPreferredOrder(ctx, customer.UnifiedID, available)
	if len(preferred) > 0 {
		available = preferred
	}

	if req.DryRun {
		channel, recipient, accountID, err := s.pickChannelDryRun(available, ident)
		if err != nil {
			return nil, err
		}
		return &ProactiveReachResponse{
			MessageID:   "dry_run",
			Channel:     channel,
			RecipientID: recipient,
			AccountID:   accountID,
			Status:      "dry_run",
			SentAt:      time.Now(),
			Strategy:    fmt.Sprintf("available=[%s], picked=%s", strings.Join(available, ","), channel),
		}, nil
	}

	channel, recipient, accountID, err := s.pickChannel(ctx, available, ident)
	if err != nil {
		return nil, err
	}

	// 闸门问在选路之后、写冷却键之前：
	//   · 之后 —— 闸门要看得见最终渠道与收件人，否则报告答不上"这次发到哪"；
	//   · 之前 —— 拒发不能占用冷却窗。占了窗等于一次没发生的外发产生与真外发相同的后续效果，
	//     运营补完授权还得再等一小时（有测试钉住这条）。
	// 顺带的口径变化：选不出渠道（no active account）时，以前若正处冷却会先报 cooldown，
	// 现在先报选路失败。两者都是"这条没发出去"，队列处置同为退避，不影响外发次数。
	if err := s.enforceOutboundGates(ctx, ReachSubject{
		Key:   reachApprovalKey(customer.UnifiedID, customer.ID, channel, recipient),
		OneID: customer.UnifiedID, CustomerID: customer.ID,
		Channel: channel, Recipient: recipient, AccountID: accountID,
		Region: req.Region, Industry: req.Industry, OptIn: req.OptIn,
	}); err != nil {
		return nil, err
	}

	if !s.checkCooldown(ctx, customer.UnifiedID) {
		return nil, fmt.Errorf("%w: customer %s recently received a message, please wait", ErrReachCooldown, customer.UnifiedID)
	}

	sentChannel, sentRecipient = channel, recipient

	switch channel {
	case "sms":
		return s.sendSMS(ctx, req, recipient)
	case "email":
		return s.sendEmail(ctx, req, recipient)
	case "telegram":
		return s.sendTelegram(ctx, req, recipient, accountID)
	case "whatsapp":
		return s.sendWhatsApp(ctx, req, recipient, accountID)
	case "wecom":
		return s.sendWeCom(ctx, req, recipient, accountID)
	case "wechat":
		return s.sendWeChat(ctx, req, recipient)
	case "feishu":
		return s.sendFeishu(ctx, req, recipient, accountID)
	case "douyin", "tiktok", "kuaishou", "xiaohongshu", "xianyu":
		return s.sendBridge(ctx, channel, req, recipient, accountID)
	case "dingtalk":
		return s.sendDingTalk(ctx, req, recipient)
	}

	return nil, fmt.Errorf("unsupported channel: %s", channel)
}

func (s *ProactiveReachService) loadCustomer(ctx context.Context, customerID, oneID string) (*model.Customer, error) {
	if s.customerRepo == nil {
		return nil, nil
	}
	if customerID != "" {
		return s.customerRepo.GetByID(ctx, customerID)
	}
	if oneID != "" {
		return s.customerRepo.GetByUnifiedID(ctx, oneID)
	}
	return nil, nil
}

// LoadCustomer 公开方法（供 controller 调用）
func (s *ProactiveReachService) LoadCustomer(ctx context.Context, customerID, oneID string) (*model.Customer, error) {
	return s.loadCustomer(ctx, customerID, oneID)
}

// CustomerChannelAvailability 单个渠道的可用性与身份（预览用）。
type CustomerChannelAvailability struct {
	Channel  string
	Identity string
}

// ListChannelAvailability 列出客户可用渠道及其身份（预览用）。
//
// 与 ReachByCustomer 的闸门同源同口径：同样走 loadCustomerIdentity，
// 于是「预览说有渠道」与「外发时选得出渠道」永远一致，不会两处各判各的。
// 一次调用出全量（available + identity 同源），不为每个渠道重查一次绑定表。
func (s *ProactiveReachService) ListChannelAvailability(ctx context.Context, c *model.Customer) ([]CustomerChannelAvailability, error) {
	ident, err := s.loadCustomerIdentity(ctx, c)
	if err != nil {
		logger.Warnf("[ProactiveReach] 预览渠道读绑定表失败，退回仅反规范化列 one_id=%s: %v", c.UnifiedID, err)
	}
	available := ident.Available(nil)
	out := make([]CustomerChannelAvailability, 0, len(available))
	for _, ch := range available {
		out = append(out, CustomerChannelAvailability{Channel: ch, Identity: ident.Identity(ch)})
	}
	return out, nil
}

func (s *ProactiveReachService) loadCustomerPreferredOrder(ctx context.Context, oneID string, fallback []string) ([]string, error) {
	if s.repo == nil {
		return nil, nil
	}
	rows, err := s.repo.ListCustomerChannelsByOneID(ctx, oneID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	ordered := make([]string, 0, len(rows))
	for _, r := range rows {
		for _, f := range fallback {
			if r.Channel == f {
				ordered = append(ordered, r.Channel)
				break
			}
		}
	}
	return ordered, nil
}

// loadCustomerIdentity 读「反规范化列 + 绑定表」并成集。
//
// 只在绑定表读得到行时才把它折进 bindings；读不到行不是错误（新客户本来就没有绑定），
// 与「读失败」区分开，免得把数据形态当故障报。
func (s *ProactiveReachService) loadCustomerIdentity(ctx context.Context, c *model.Customer) (*CustomerIdentity, error) {
	if s.repo == nil || c.UnifiedID == "" {
		return NewCustomerIdentity(c, nil), nil
	}
	rows, err := s.repo.ListCustomerChannelsByOneID(ctx, c.UnifiedID)
	if err != nil {
		return NewCustomerIdentity(c, nil), err
	}
	if len(rows) == 0 {
		return NewCustomerIdentity(c, nil), nil
	}
	return NewCustomerIdentity(c, CustomerChannelBindings(rows)), nil
}

func (s *ProactiveReachService) pickChannelDryRun(candidates []string, ident *CustomerIdentity) (channel, recipient, accountID string, err error) {
	for _, ch := range candidates {
		recipient = ident.Identity(ch)
		if recipient == "" {
			continue
		}
		if ch == "sms" || ch == "email" || ch == "dingtalk" {
			return ch, recipient, "", nil
		}
		return ch, recipient, "dry_run_account", nil
	}
	return "", "", "", fmt.Errorf("%w for customer %s", ErrNoChannelIdentity, ident.cust.UnifiedID)
}

func (s *ProactiveReachService) pickChannel(ctx context.Context, candidates []string, ident *CustomerIdentity) (channel, recipient, accountID string, err error) {
	var tried []string
	for _, ch := range candidates {
		tried = append(tried, ch)
		recipient = ident.Identity(ch)
		if recipient == "" {
			continue
		}

		if ch == "sms" || ch == "email" || ch == "dingtalk" {
			return ch, recipient, "", nil
		}

		acc, err := s.accountLookup.FindActiveAccount(ctx, ch)
		if err != nil {
			logger.Warnf("[ProactiveReach] 查找 %s 账号失败: %v", ch, err)
			continue
		}
		if acc == "" {
			continue
		}
		return ch, recipient, acc, nil
	}
	return "", "", "", fmt.Errorf("%w for customer %s, tried: %s", ErrNoActiveAccount, ident.cust.UnifiedID, strings.Join(tried, ","))
}

// reachCooldownWindow 同一客户两次外发的最小间隔。
//
// 提成常量是为了让依赖它的不变量可断言：挽回 worker 的退避基数必须严格大于这个窗口
// （见 recovery_queue_worker.go 的 NewRecoveryQueueWorker），否则每次到期都会先撞冷却、
// 白耗一轮。以前它只写在 SetNX 调用里，改一处就得靠记忆去改另一处。
const reachCooldownWindow = 60 * time.Minute

func (s *ProactiveReachService) checkCooldown(ctx context.Context, oneID string) bool {
	if oneID == "" {
		return true
	}
	key := "mtk:reach:cooldown:" + oneID
	set, err := cache.GetGlobalCache().SetNX(ctx, key, "1", reachCooldownWindow)
	if err != nil {
		return true
	}
	return set
}

func (s *ProactiveReachService) checkDoNotContact(ctx context.Context, oneID, channel, fallbackKey string) bool {
	key := oneID
	if key == "" {
		key = fallbackKey
	}
	blocked := s.dncService().IsBlocked(ctx, key, channel)
	if blocked {

		logger.Warnf("[DNC] 跳过发送 one_id=%s channel=%s（命中全局退订标志位）", key, channel)
	}
	return blocked
}

func (s *ProactiveReachService) filterDoNotContactChannels(ctx context.Context, oneID string, candidates []string) []string {
	dnc := s.dncService()
	kept := make([]string, 0, len(candidates))
	for _, ch := range candidates {
		if dnc.IsBlocked(ctx, oneID, ch) {
			logger.Warnf("[DNC] 跳过渠道 one_id=%s channel=%s（命中全局退订标志位）", oneID, ch)
			continue
		}
		kept = append(kept, ch)
	}
	return kept
}

func (s *ProactiveReachService) sendSMS(ctx context.Context, req *ProactiveReachRequest, phone string) (*ProactiveReachResponse, error) {
	registry := s.smsRegistry
	if registry == nil {
		return nil, errors.New("sms service not registered")
	}
	svc, err := registry()
	if err != nil {
		return nil, err
	}
	msgID, err := svc(ctx, phone, req.Content, req.TemplateID, req.Params)
	if err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   msgID,
		Channel:     "sms",
		RecipientID: phone,
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    "explicit phone",
	}, nil
}

func (s *ProactiveReachService) sendEmail(ctx context.Context, req *ProactiveReachRequest, to string) (*ProactiveReachResponse, error) {
	sender := s.emailRegistry
	if sender == nil {
		return nil, errors.New("email service not registered")
	}
	subject := req.Subject
	if subject == "" {
		subject = "来自 HiveMtk 的消息"
	}
	msgID, err := sender(ctx, 0, to, subject, req.Content, nil)
	if err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   msgID,
		Channel:     "email",
		RecipientID: to,
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    "explicit email",
	}, nil
}

func (s *ProactiveReachService) sendTelegram(ctx context.Context, req *ProactiveReachRequest, recipientID, accountID string) (*ProactiveReachResponse, error) {
	sender := s.telegramRegistry
	if sender == nil {
		return nil, errors.New("telegram service not registered")
	}
	accID := parseUint(accountID)
	chatID := parseInt64(recipientID)
	if chatID == 0 {
		return nil, fmt.Errorf("telegram: invalid chat_id %s", recipientID)
	}
	if err := sender(ctx, accID, chatID, req.Content); err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   fmt.Sprintf("tg_%d", time.Now().UnixNano()),
		Channel:     "telegram",
		RecipientID: recipientID,
		AccountID:   accountID,
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    "customer has telegram_chat_id",
	}, nil
}

func (s *ProactiveReachService) sendWhatsApp(ctx context.Context, req *ProactiveReachRequest, recipientID, accountID string) (*ProactiveReachResponse, error) {
	sender := s.whatsAppRegistry
	if sender == nil {
		return nil, errors.New("whatsapp service not registered")
	}
	accID := parseUint(accountID)
	if err := sender(ctx, accID, recipientID, req.Content); err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   fmt.Sprintf("wa_%d", time.Now().UnixNano()),
		Channel:     "whatsapp",
		RecipientID: recipientID,
		AccountID:   accountID,
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    "customer has whatsapp_phone",
	}, nil
}

func (s *ProactiveReachService) sendWeCom(ctx context.Context, req *ProactiveReachRequest, recipientID, accountID string) (*ProactiveReachResponse, error) {
	sender := s.weComRegistry
	if sender == nil {
		return nil, errors.New("wecom service not registered")
	}
	accID := parseUint(accountID)
	msgID, err := sender(ctx, accID, recipientID, "text", req.Content, true, "ai_agent")
	if err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   msgID,
		Channel:     "wecom",
		RecipientID: recipientID,
		AccountID:   accountID,
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    "customer has wecom_external_id",
	}, nil
}

func (s *ProactiveReachService) sendWeChat(ctx context.Context, req *ProactiveReachRequest, recipientID string) (*ProactiveReachResponse, error) {
	sender := s.wechatRegistry
	if sender == nil {
		return nil, errors.New("wechat service not registered")
	}
	accountID := parseUint(req.AccountID)
	msgID, err := sender(ctx, accountID, recipientID, "text", req.Content)
	if err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   msgID,
		Channel:     "wechat",
		RecipientID: recipientID,
		AccountID:   fmt.Sprintf("%d", accountID),
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    "customer has wechat_open_id",
	}, nil
}

func (s *ProactiveReachService) sendFeishu(ctx context.Context, req *ProactiveReachRequest, recipientID, accountID string) (*ProactiveReachResponse, error) {
	sender := s.feishuRegistry
	if sender == nil {
		return nil, errors.New("feishu service not registered")
	}
	accID := parseUint(accountID)
	if err := sender(ctx, accID, recipientID, req.Content, "open_id"); err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   fmt.Sprintf("feishu_%d", time.Now().UnixNano()),
		Channel:     "feishu",
		RecipientID: recipientID,
		AccountID:   accountID,
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    "customer has feishu_open_id",
	}, nil
}

func (s *ProactiveReachService) sendBridge(ctx context.Context, channel string, req *ProactiveReachRequest, recipientID, accountID string) (*ProactiveReachResponse, error) {
	// D-9 回执契约（2026-10-23）：bridge 渠道走 DeliverBridgeOutboundWithReceipt，
	// 把真实 hub 行号 + hub 行 MsgID（回执键，Recall 按 msg_id 反查用）透给响应。
	// 不再用 fmt.Sprintf("%s_%d", channel, UnixNano()) 自造占位——那是个查不到
	// 任何行的假键，操作者拿它去撤回/对账必然扑空。
	hubID, msgID, err := DeliverBridgeOutboundWithReceipt(ctx, channel, accountID, recipientID, "text", req.Content, "")
	if err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   msgID,
		HubID:       hubID,
		Channel:     channel,
		RecipientID: recipientID,
		AccountID:   accountID,
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    fmt.Sprintf("customer has %s open_id", channel),
	}, nil
}

func (s *ProactiveReachService) sendDingTalk(ctx context.Context, req *ProactiveReachRequest, chatID string) (*ProactiveReachResponse, error) {
	sender := s.dingTalkRegistry
	if sender == nil {
		return nil, errors.New("dingtalk service not registered")
	}
	msgID, err := sender(ctx, chatID, "", "text", req.Content)
	if err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   msgID,
		Channel:     "dingtalk",
		RecipientID: chatID,
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    "dingtalk webhook",
	}, nil
}

func parseUint(s string) uint {
	var n uint
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}

func parseInt64(s string) int64 {
	var n int64
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}

type defaultAccountLookup struct {
	repo *repository.ProactiveReachRepository
}

func (l *defaultAccountLookup) FindActiveAccount(ctx context.Context, channel string) (string, error) {
	if l.repo == nil {
		return "", errors.New("db not available")
	}
	return l.repo.FindActiveAccountID(ctx, channel, int(BridgeOnlineGraceWindow(ctx).Seconds()))
}

// ProactiveReachSenderSet 主动触达用到的一组外发服务。
//
// 为什么交"造好的服务"而不是"一个 DB 句柄"：装配点（router / app）比服务层更清楚
// 这些外发腿该连哪个库；把句柄传进来就意味着本文件也要握 GORM 句柄（五层架构 §3.2 阻断项），
// 而且每个腿的仓储选择会重新散落成各写各的。字段留 nil 表示该渠道不装配。
type ProactiveReachSenderSet struct {
	Telegram *TelegramIntegrationService
	WhatsApp *WhatsAppCloudIntegrationService
	Feishu   *FeishuIntegrationService
	WeCom    *WeComIntegrationService
	Wechat   *WechatService
}

// BindProactiveReachSenders 把 Reach Sender 注册到 ProactiveReachService
//
//	在 router 启动时调用，把真实的 SMS/Email/TG/... 发送器注入。
func BindProactiveReachSenders(svc *ProactiveReachService, senders ProactiveReachSenderSet) {
	if svc == nil {
		return
	}

	svc.SetSMSRegistry(func() (func(ctx context.Context, phone, content, templateID string, params map[string]string) (string, error), error) {
		smsSvc := smssvc.NewSmsService(repository.NewSmsRepository())
		return func(ctx context.Context, phone, content, templateID string, params map[string]string) (string, error) {
			if err := smsSvc.SendSms(ctx, &dto.SmsSendRequest{Phone: phone, Content: content}); err != nil {
				return "", err
			}
			return "sms_out", nil
		}, nil
	})

	svc.SetEmailRegistry(func(ctx context.Context, accountID uint, to, subject, content string, attachments []string) (string, error) {
		emailSvc := NewEmailService()
		return emailSvc.Send(ctx, accountID, to, subject, content, attachments)
	})

	if senders.Telegram != nil {
		tg := senders.Telegram
		svc.SetTelegramRegistry(func(ctx context.Context, accountID uint, chatID int64, content string) error {
			return tg.SendMessage(ctx, accountID, chatID, content)
		})
	}

	if senders.WhatsApp != nil {
		wa := senders.WhatsApp
		svc.SetWhatsAppRegistry(func(ctx context.Context, accountID uint, toPhone, content string) error {
			return wa.SendMessage(ctx, accountID, toPhone, content)
		})
	}

	if senders.WeCom != nil {
		wecom := senders.WeCom
		svc.SetWeComRegistry(func(ctx context.Context, accountID uint, externalUserID, msgType, content string, isAIReply bool, agent string) (string, error) {
			_, err := wecom.SendMessage(ctx, &WeComSendRequest{
				AccountID:      accountID,
				ExternalUserID: externalUserID,
				MsgType:        msgType,
				Content:        content,
				IsAIReply:      isAIReply,
				AIAgent:        agent,
			})
			if err != nil {
				return "", err
			}
			return "wecom_out", nil
		})
	}

	if senders.Feishu != nil {
		feishu := senders.Feishu
		svc.SetFeishuRegistry(func(ctx context.Context, accountID uint, openID, content, receiveIDType string) error {
			return feishu.SendMessage(ctx, accountID, openID, content, receiveIDType, "")
		})
	}

	svc.SetDingTalkRegistry(func(ctx context.Context, webhookOrToken, secret, msgType, content string) (string, error) {
		return NewDingTalkService().SendRobot(ctx, webhookOrToken, secret, msgType, content)
	})

	if senders.Wechat != nil {
		wechat := senders.Wechat
		svc.SetWechatRegistry(func(ctx context.Context, accountID uint, openID, msgType, content string) (string, error) {
			return wechat.SendCustomMessage(ctx, accountID, openID, msgType, content)
		})
	}
}
