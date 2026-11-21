package email

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/crypto"
	"hivemtk-user/internal/pkg/mail"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/storage"

	"github.com/google/uuid"
	"gopkg.in/gomail.v2"
)

// 邮件状态常量。真值在 model（仓库侧写状态时要用同一套数），这里保留旧名不拆散调用点。
const (
	EmailStatusPending = model.EmailStatusPending
	EmailStatusSent    = model.EmailStatusSent
	EmailStatusFailed  = model.EmailStatusFailed
)

const (
	// EmailPendingTTL pending 行的最长存活时间。排水一旦被装上进程，没有龄上限就等于
	// "进程停机三周后重启，把三周前排的邮件一次性群发出去" —— 那种投递对收件人是骚扰，
	// 对发信域是声誉损失。24h 与 reach_delayed_outbound 的到期口径同源（同一件事在两处
	// 各写一个数的话，两处就迟早会各说各话）。
	EmailPendingTTL = 24 * time.Hour

	// EmailSendingStaleAfter sending 行被视为"认领它的进程已经死了"的窗口。
	//
	// 必须大于单轮最坏耗时：一轮上限 emailDrainBatch 封、串行投递， SMTP 拨号超时就按
	// 分钟级计。窗口小于轮耗时的后果不是报错而是双投 —— 副本 A 还在投，副本 B 把它
	// 回捞重投了。取 30min 是"明显大于正常一轮"与"崩溃遗留不无限期卡住"之间的取舍。
	EmailSendingStaleAfter = 30 * time.Minute

	// emailDrainBatch 单轮上限。沿用原 GetPendingEmails 的 100：一轮认领的行数必须有界，
	// 不能随积压量增长（防无界载入这条口径在本仓的每条队列上都有）。
	emailDrainBatch = 100
)

// UnsubscribeLinker 签发退订链接的能力（由 *service.EmailUnsubscribeService 满足）。
//
// 接口写在本包而不是直接用 internal/service 的类型：本包对外只依赖"能签出一条链接"这件事。
// jobID 目前恒为空 —— EmailSend 这张表没有任务归属（群发侧的归属在 email_lists.jobs_id）。
type UnsubscribeLinker interface {
	GenerateUnsubscribeLink(ctx context.Context, email, jobID string) (string, error)
}

// OpenPixelLinker 打开追踪像素的签发器。消费侧（像素路由、事件落库）早已装配，
// 本接口接的是缺的那一半：把 URL 塞进正文。
type OpenPixelLinker interface {
	GenerateOpenPixelURL(ctx context.Context, email, jobID string) (string, error)
}

type EmailSendService struct {
	repo      repository.EmailSendRepository
	smtpRepo  repository.EmailSmtpRepository
	unsubRepo repository.EmailUnsubscribeRepository

	// unsubLinker 退订链接签发器，默认构造即带（见 NewEmailSendService）。
	unsubLinker UnsubscribeLinker
	// unsubWarnOnce 让"签发失败"只出声一次：缺密钥是配置错误，看第一行就够，
	// 每封一行只会把别的日志埋掉。
	unsubWarnOnce sync.Once

	// openPixel 打开追踪像素签发器，默认构造即带；nil 或未配 EMAIL_TRACKING_SECRET ⇒ 正文不带像素。
	openPixel OpenPixelLinker
	// pixelWarnOnce 与 unsubWarnOnce 同一取舍。
	pixelWarnOnce sync.Once

	// deliver 是"把这一封真的投出去"的接缝，默认为 sendActualEmail（连真 SMTP）。
	// 抽出来的理由与 SMS 侧同源：合规判据与节拍逻辑要在测试里跑，而测试不该连 SMTP。
	deliver func(ctx context.Context, email *model.EmailSend) error

	// attachments 附件列 → 磁盘路径的解析器，nil = 按环境变量现推（见 attachmentResolver）。
	attachments mail.AttachmentResolver
}

// SetUnsubscribeLinker 替换退订链接签发器（传 nil = 明确不要退订出口）。
func (s *EmailSendService) SetUnsubscribeLinker(linker UnsubscribeLinker) {
	s.unsubLinker = linker
}

// SetOpenPixelLinker 替换打开追踪像素签发器（传 nil = 明确不要打开统计）。
func (s *EmailSendService) SetOpenPixelLinker(linker OpenPixelLinker) {
	s.openPixel = linker
}

// SetEmailUnsubscribeRepository 注入退订名单读取句柄。
//
// 不注入时退订检查走包级单例 ⇒ 全局 DB 句柄，于是这条合规判据读到的是"谁先跑"而不是
// "配了哪套库"（排水 worker 在独立协程里跑，这个先后顺序没人保证）。装配层应显式注入。
func (s *EmailSendService) SetEmailUnsubscribeRepository(repo repository.EmailUnsubscribeRepository) {
	s.unsubRepo = repo
}

func (s *EmailSendService) deliveryFn() func(context.Context, *model.EmailSend) error {
	if s.deliver != nil {
		return s.deliver
	}
	return s.sendActualEmail
}

// isUnsubscribed 查询退订名单（FA-EMAIL-02：查询故障独立于"未退订"——
// 读取失败返回错误由调用方拒发/延期，绝不当作"未退订"继续投递）。
func (s *EmailSendService) isUnsubscribed(ctx context.Context, email string) (bool, error) {
	if s.unsubRepo == nil {
		s.unsubRepo = repository.NewEmailUnsubscribeRepository(nil)
	}
	exists, err := s.unsubRepo.ExistsByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return false, fmt.Errorf("退订名单查询失败：%w", err)
	}
	return exists, nil
}

func NewEmailSendService() *EmailSendService {
	return &EmailSendService{
		repo:     repository.NewEmailSendRepository(),
		smtpRepo: repository.NewEmailSmtpRepository(),
		// 默认就带签发器：本构造函数有三个调用点（排水装配、HTTP controller、reach 适配器），
		// 只在装配层注入会让"客户点一下立即发送"这条路没有退订出口 —— 而那条路恰恰是
		// Gmail/Yahoo 会抽样看到的那条路。退订链接只依赖 HMAC 密钥与 base URL，不碰 DB。
		unsubLinker: service.NewEmailUnsubscribeService(nil),
		// 像素签发器同样默认带上：它和退订链接一样只依赖 HMAC 密钥与 base URL，不碰 DB，
		// 未配 EMAIL_TRACKING_SECRET 时签发本身 fail-closed ⇒ 正文不带像素，装配多这一行零成本。
		openPixel: service.NewEmailOpenTrackerService(nil, nil),
	}
}

func sanitizeEmailHeader(v string) (string, error) {
	if strings.ContainsAny(v, "\r\n") {
		return "", errors.New("邮件头部字段不允许包含换行符")
	}
	return strings.TrimSpace(v), nil
}

var emailSendSem = make(chan struct{}, 10)

// 发送邮件
func (s *EmailSendService) SendEmail(ctx context.Context, req dto.SendEmailRequest) (*model.EmailSend, error) {
	cleanTo, err := sanitizeEmailHeader(req.To)
	if err != nil {
		return nil, err
	}
	req.To = cleanTo
	cleanSubject, serr := sanitizeEmailHeader(req.Subject)
	if serr != nil {
		return nil, serr
	}
	req.Subject = cleanSubject

	emailSend := &model.EmailSend{
		ID:          uuid.New().String(),
		To:          req.To,
		Subject:     req.Subject,
		Content:     req.Content,
		Attachments: strings.Join(req.Attachments, ","),
		SmtpID:      req.SmtpId,
		Status:      EmailStatusPending,
	}

	if req.ImmediateSend {
		now := time.Now()
		emailSend.SendTime = &now
	} else if req.SendTime != nil {
		emailSend.SendTime = req.SendTime
	}

	if err := s.repo.Create(ctx, emailSend); err != nil {
		return nil, err
	}

	if req.ImmediateSend {

		// FA-EMAIL-02：退读故障不得当作"未退订"继续投递——返回错误让本次调用失败，
		// 行保持 pending 由耐久投递侧在认领时重查退订（库恢复后自然续投）。
		unsub, uerr := s.isUnsubscribed(ctx, req.To)
		if uerr != nil {
			logger.Errorf("退订名单查询失败 [%s]（拒发待续投）: %v", emailSend.ID, uerr)
			return emailSend, uerr
		}
		if unsub {
			if u, perr := uuid.Parse(emailSend.ID); perr == nil {
				_ = s.repo.UpdateStatus(ctx, u, EmailStatusFailed)
			}
			return emailSend, nil
		}
		go func() {

			emailSendSem <- struct{}{}
			defer func() { <-emailSendSem }()
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("邮件发送异步协程 panic [%s]: %v\n%s", emailSend.ID, r, debug.Stack())
				}
			}()
			sendCtx := context.WithoutCancel(ctx)
			// FA-EMAIL-01：即时投递前先原子认领（与排水 worker 同一原语）——
			// 行仍 pending 才归本 goroutine 投；已被 worker 认领则让位，恰好一次投递。
			claimed, ok, cerr := s.repo.ClaimByIDForDelivery(sendCtx, emailSend.ID)
			if cerr != nil {
				logger.Errorf("即时邮件认领失败 [%s] ⇒ 行保持 pending 由排水侧接管: %v", emailSend.ID, cerr)
				return
			}
			if !ok {
				logger.Infof("即时邮件 [%s] 已被排水侧认领，本协程让位（防双投）", emailSend.ID)
				return
			}
			s.deliverClaimed(sendCtx, claimed, s.deliveryFn())
		}()
	}

	return emailSend, nil
}

// ProcessPendingEmails 排期邮件的排水轮：回捞崩溃遗留 → 超龄判过期 → 认领到期 → 逐封投递。
//
// 顺序是有意义的：回捞必须排在过期与认领之前，否则上一进程遗留的 sending 行要空等一轮；
// 而过期必须排在认领之前，否则刚被回捞回来的老邮件会先被投出去、下一轮才被判过期。
//
// 三个步骤各自的失败都不阻断本轮剩下的事，但一律出声：这里静默失败的原症状就是
// "邮件永远停在待发送、一行错误都没有"，不能再把它换成另一句没人看见的话。
//
// 后台任务租户扇出（specs/001-saas-multi-tenant T020）：TENANT_MODE=on 时 email_sends 是
// 租户表，排水 worker 由 context.Background() 起的节拍调用、没有 HTTP 请求 ctx 可绑，
// 无租户 ctx 的 ClaimDueForUpdate（query）、ReclaimStaleSending/ExpireStalePending
// （update）都会被隔离 callback fail-closed 拒绝（生产日志实证："[email-drain] 本轮排水失败 ⇒
// 未认领的邮件留待下一轮: [tenant] 表 email_sends 需要租户上下文但 ctx 未绑定（query）"）。
//
// 修法是 Fanout 而不是 system scope：认领→投递是**逐租户的业务动作**，deliverClaimed 往下
// 要按 ctx 取退订名单（email_unsubscribe）、按 SmtpID 取发信配置（email_smtp）、再回写行状态
// —— system scope 虽能放行查询，但这些读会摊成跨租户全量（A 租户的退订名单挡住 B 的信），
// 新建行还会落到列默认租户 1（写错租户）。Fanout 逐活跃租户注入 ctx（Query 加
// WHERE tenant_id=N、Create 填 N），off 模式退化为单默认租户，等价既有行为 ——
// 与 sop_scheduler.tick / churn_score_job 同一形态。
func (s *EmailSendService) ProcessPendingEmails(ctx context.Context) error {
	deliver := s.deliveryFn()
	if deliver == nil {
		return errors.New("邮件投递实现未装配")
	}
	now := time.Now()

	_, err := tenant.Fanout(ctx, nil, func(ctx context.Context, _ uint) (int64, error) {
		if n, err := s.repo.ReclaimStaleSending(ctx, now.Add(-EmailSendingStaleAfter)); err != nil {
			logger.Errorf("[email-drain] 投递中途崩溃的 sending 行回捞失败 ⇒ 这些邮件本轮不重投：%v", err)
		} else if n > 0 {
			// 重投是 at-least-once：崩溃点若在 SMTP 已递交之后，收件人会收到重复邮件。
			// 这个代价换的是"不会有一封邮件永久卡在 sending"，必须留一行能查到的说明。
			logger.Warnf("[email-drain] 回捞 %d 封认领后未投递完的邮件并重投（收件人可能收到重复邮件）", n)
		}
		if n, err := s.repo.ExpireStalePending(ctx, now.Add(-EmailPendingTTL)); err != nil {
			logger.Errorf("[email-drain] 超龄 pending 判过期失败 ⇒ 这些行会在队列里越积越多：%v", err)
		} else if n > 0 {
			logger.Warnf("[email-drain] %d 封排期邮件已超过 %s 的龄上限 ⇒ 判过期不补发（进程停机期间的排期不会突然群发）",
				n, EmailPendingTTL)
		}

		pendingEmails, err := s.repo.ClaimDueForUpdate(ctx, now, emailDrainBatch)
		if err != nil {
			// 认领失败必须透给调用方（worker 的"本轮排水失败"日志与
			// TestEmailDrainWorker_RunOnceSurfacesRepoFailure 都压在这一条上）；
			// Fanout 负责跨租户汇总：全失败时原样返回（错误已按租户包裹）。
			return 0, err
		}

		for _, email := range pendingEmails {
			s.deliverClaimed(ctx, email, deliver)
		}
		return int64(len(pendingEmails)), nil
	})
	return err
}

// deliverClaimed 投递这一封已被本轮独占认领的邮件，并把结果写回状态。
func (s *EmailSendService) deliverClaimed(ctx context.Context, email *model.EmailSend, deliver func(context.Context, *model.EmailSend) error) {
	emailUUID, parseErr := uuid.Parse(email.ID)
	if parseErr != nil {
		// ID 解析不了 ⇒ 状态写不回去。这一行会留在 sending，由 EmailSendingStaleAfter 那格
		// 回捞兜底，不会永久卡住；但它也不该被当成"没发生"。
		logger.Errorf("邮件 ID 解析失败 [%s]: %v", email.ID, parseErr)
		return
	}
	// 排期这条腿过去没有退订检查：即时发送的合规判据挡不住"排到明早 9 点"这条路。
	// FA-EMAIL-02：排期腿同样拒把查询故障当"未退订"——失败时本轮跳过、行保持
	// pending，由下一轮认领重查（可恢复延期），故障原因随日志留痕。
	unsub, uerr := s.isUnsubscribed(ctx, email.To)
	if uerr != nil {
		logger.Errorf("退订名单查询失败 [%s]（本轮跳过待续投）: %v", email.ID, uerr)
		return
	}
	if unsub {
		if err := s.repo.UpdateStatus(ctx, emailUUID, EmailStatusFailed); err != nil {
			logger.Warnf("邮件状态落库失败(收件人已退订) [%s]: %v", email.ID, err)
		}
		logger.Infof("邮件 %s 跳过投递：收件人 %s 已在退订名单", email.ID, email.To)
		return
	}
	if err := deliver(ctx, email); err != nil {
		logger.Errorf("邮件发送失败 [%s]: %v", email.ID, err)
		if e := s.repo.UpdateStatus(ctx, emailUUID, EmailStatusFailed); e != nil {
			logger.Warnf("邮件状态落库失败(发送失败) [%s]: %v", email.ID, e)
		}
		return
	}
	if e := s.repo.UpdateStatus(ctx, emailUUID, EmailStatusSent); e != nil {
		logger.Warnf("邮件状态落库失败(发送成功) [%s]: %v", email.ID, e)
	}
}

func (s *EmailSendService) sendActualEmail(ctx context.Context, email *model.EmailSend) error {

	var smtpConfig *model.EmailSmtp
	// 显式 SmtpID：这封信被明确指到某个 SMTP 账号（页面/业务写死的选择），优先级最高，
	// 不参与下面「默认通道」的 env > DB 链（改它等于推翻一次刻意的选择）。
	if email.SmtpID != "" {
		cfg, err := s.smtpRepo.GetByID(ctx, email.SmtpID)
		if err == nil && cfg != nil && cfg.Server != "" && cfg.Username != "" {
			// FA-EMAIL-03：显式选择必须统一解密——密文直投拨号器等于用错密码；
			// 解密失败=凭据不可用，显式选择不允许悄悄换账号，直接报错收口。
			plain, derr := smtpPassword(cfg)
			if derr != nil {
				return fmt.Errorf("邮件发送失败：显式 SMTP 账号（SmtpID=%s）密码解密失败：%w", email.SmtpID, derr)
			}
			cfg.Password = plain
			smtpConfig = cfg
		} else if err != nil {
			logger.Warnf("邮件 SMTP 配置读取失败（SmtpID=%s），尝试环境变量兜底: %v", email.SmtpID, err)
		}
	}

	// 默认通道（R2 路径 A 接线 + F3 备用通道 DB 化；SaaS 配置面改造 B3，2026-11-17）：
	// 显式 SmtpID > DB > 无。EMAIL_163_*/QQ_EMAIL*/SMTP_* 环境变量回退已退役——
	// DB 是「运营在 /email/smtp 页面配的账号」，改页面即驱动发送、免重启。
	// 表空/全不可用时 resolveSmtpFromDB 返 nil，回落下面的报错分支（fail-closed）。
	//
	// F3：两条备用通道（163/QQ）同样 DB 驱动——email_smtp 表里 Name 为
	// "env:163" / "env:qq"（历史命名，语义已变为「命名备用通道」）的行
	// 按 163 → QQ 同序优先于任意首条可用行被选中（见 resolveSmtpFromDB）。
	if smtpConfig == nil {
		smtpConfig = s.resolveSmtpFromDB(ctx)
	}

	if smtpConfig == nil {
		logger.Errorf("邮件发送失败 [%s]: 无任何可用 SMTP 配置（SmtpID=%q，email_smtp 表也无可用行）",
			email.ID, email.SmtpID)
		return fmt.Errorf("邮件发送失败：未找到可用的 SMTP 配置（请配置 SmtpID，或在邮件账号页新增一条 SMTP）")
	}

	m := s.buildEmailMessage(ctx, smtpConfig, email)

	d := gomail.NewDialer(smtpConfig.Server, smtpConfig.Port, smtpConfig.Username, smtpConfig.Password)

	return d.DialAndSend(m)
}

// smtpPassword 统一 SMTP 凭据解析（FA-EMAIL-03）：所有选路复用同一解密口，
// 拒绝空密钥与解密失败——调用方拿到的要么是可用明文，要么是明确错误。
func smtpPassword(cfg *model.EmailSmtp) (string, error) {
	if cfg == nil || cfg.Password == "" {
		return "", fmt.Errorf("SMTP 账号密码为空")
	}
	plain, err := crypto.Decrypt(cfg.Password)
	if err != nil {
		return "", err
	}
	if plain == "" {
		return "", fmt.Errorf("SMTP 账号密码解密结果为空")
	}
	return plain, nil
}

// resolveSmtpFromDB 从 email_smtp 表挑默认发信通道（R2 路径 A 接线 + F3 备用通道 DB 化）。
//
// 为什么补这一层：/email/smtp 页面把 SMTP 持久化进 email_smtp 表，但发信侧原先只在
// 「email.SmtpID 显式指定」时读表；一旦某封信没带 SmtpID 且 env 未配，即便表里躺着
// 运营配好的账号也直接报错。接上之后：显式 SmtpID > env > 本函数（DB）> 报错。
//
// F3 备用通道 DB 化：行 Name 为 "env:163" / "env:qq" 的账号按 resolveSmtpFromEnv
// 同一顺序（163 → QQ）优先于「任意首条可用行」被选中——让页面上配的备用通道精确
// 顶替对应的 env 通道，而不是被首行顺序随机顶掉。存量表里没有这两个名字的行，
// 选径与接线前逐字节一致；密码同走 crypto 加密落库/解密发送（见下）。
//
// 密码落库时已按 crypto.Encrypt 加密（见 EmailSmtpService.CreateEmailSmtp），这里发送前
// 解回明文；解不动（FIELD_ENCRYPTION_KEY 换过 / 明文遗留行）就沿用原值，与
// GetRandEmailSmtp 同一口径（decrypt 成功且非空才替换）。挑第一个 Server/Username 都
// 非空的行；表空或全不可用返回 nil，让调用方回落报错。
func (s *EmailSendService) resolveSmtpFromDB(ctx context.Context) *model.EmailSmtp {
	if s.smtpRepo == nil {
		return nil
	}
	list, err := s.smtpRepo.GetEmailSmtpList(ctx)
	if err != nil || len(list) == 0 {
		return nil
	}
	// 备用通道行优先（与 env 链 163 → QQ 同序），再退任意首条可用行。
	for _, name := range []string{"env:163", "env:qq"} {
		for _, cfg := range list {
			if got := smtpRowReady(cfg, name); got != nil {
				return got
			}
		}
	}
	for _, cfg := range list {
		if got := smtpRowReady(cfg, ""); got != nil {
			return got
		}
	}
	return nil
}

// smtpRowReady 判一行是否可用并解回密码。wantName 非空时要求行 Name 精确匹配
// （备用通道行的选取）；为空时匹配任意行（通用首条可用行的选取）。
func smtpRowReady(cfg *model.EmailSmtp, wantName string) *model.EmailSmtp {
	if cfg == nil || cfg.Server == "" || cfg.Username == "" {
		return nil
	}
	if wantName != "" && cfg.Name != wantName {
		return nil
	}
	// FA-EMAIL-03：解密失败=凭据不可用，拒绝该行（不再把密文当密码塞进拨号器）；
	// 默认通道的候选选取语义下换下一行是既定行为，显式选择路径由调用方 fail-closed。
	dec, derr := crypto.Decrypt(cfg.Password)
	if derr != nil || dec == "" {
		logger.Warnf("SMTP 账号（id=%s name=%s）密码解密失败，跳过该候选: %v", cfg.ID, cfg.Name, derr)
		return nil
	}
	cfg.Password = dec
	return cfg
}

// buildEmailMessage 组装一封外发的信：头、正文、附件。
//
// 单独成函数是因为"这封信长什么样"是能断言的（本包其余部分的判据都要连 SMTP 或 DB），
// 而退订出口恰恰是最需要被断言的那部分 —— 它错了不会报错，只会让收件人找不到退订入口，
// 然后以举报率的形式打在发信域信誉上。
func (s *EmailSendService) buildEmailMessage(ctx context.Context, smtpConfig *model.EmailSmtp, email *model.EmailSend) *gomail.Message {
	unsub := s.unsubscribeLink(ctx, email.To)

	m := gomail.NewMessage()
	m.SetHeader("From", smtpConfig.Username)
	m.SetHeader("To", email.To)
	m.SetHeader("Subject", email.Subject)
	mail.Unsubscribe(unsub)(m)
	m.SetBody("text/html", s.emailBody(email, unsub, s.openPixelURL(ctx, email)))

	mail.AttachmentsFromPaths(s.attachmentPaths(email.Attachments))(m)
	return m
}

// attachmentPaths 把附件列换成"真能挂上的磁盘路径"，并在填了却一项都挂不上时出声。
//
// 静默丢弃是这里的历史病：上传侧落盘 {baseDir}/attachments/{yyyy}/{mm}/{uuid}.{ext}，
// 若把值抹掉全部斜杠后拼一个扁平根下的文件名，Stat 永远不中 ⇒ 用户的附件从来没
// 跟着邮件出去过，而邮件状态写着"已发送"。挂不上仍然照发是对的（附件是增值项），
// 但不能连"一个都没挂上"都不说。
func (s *EmailSendService) attachmentPaths(csv string) []string {
	paths := mail.AttachmentPaths(csv, s.attachmentResolver())
	if mail.AttachmentsDropped(csv, paths) {
		logger.Warnf("邮件附件列非空但没有一项能挂上（只认本站上传落盘的 /files/attachments/{yyyy}/{mm}/{文件名}）")
	}
	return paths
}

// attachmentResolver 未注入时按上传侧同一组环境变量现推。
//
// 与 unsubRepo 同一形状：NewEmailSendService 有三个调用点，只在装配层注入会让其中
// 某条路的附件列被整段忽略，而表现是"静静少个附件"。
func (s *EmailSendService) attachmentResolver() mail.AttachmentResolver {
	if s.attachments != nil {
		return s.attachments
	}
	return storage.LocalAttachmentResolver()
}

// emailBody 正文 = 用户录入的内容 + 系统追加的退订页脚 + （可选）打开追踪像素。
//
// 页脚与头部链接同源（都来自这一次 unsubscribeLink 的结果）：两边各签一条的话，
// 收件人点的和客户端读到的是两个 token，退订落库时归属就对不上。
// 像素排在页脚之后：它是给客户端读的，不该插在人和"退订"两个字中间。
func (s *EmailSendService) emailBody(email *model.EmailSend, unsub, pixel string) string {
	return mail.AppendOpenPixel(mail.AppendUnsubscribeFooter(email.Content, unsub), pixel)
}

// openPixelURL 现签一枚打开追踪像素 URL，空串 = 这封信不带像素。
//
// fail-open 的理由与退订链接同一条：缺 EMAIL_TRACKING_SECRET 是配置缺陷，
// 该让统计没有、不该让信发不出去。但两条路径都得出声一次，否则"打开数恒为 0"
// 会被读成"没人打开"，而真实原因是没人收到过像素。
func (s *EmailSendService) openPixelURL(ctx context.Context, email *model.EmailSend) string {
	if s.openPixel == nil {
		return ""
	}
	url, err := s.openPixel.GenerateOpenPixelURL(ctx, email.To, email.ID)
	if err != nil {
		s.pixelWarnOnce.Do(func() {
			logger.Warnf("打开追踪像素签发失败（本进程只报这一次），后续邮件将不带像素发出: %v", err)
		})
		return ""
	}
	return url
}

// unsubscribeLink 现签一条退订链接，空串 = 这封信没有退订出口。
//
// 走 fail-open（签不出来也照发）的理由：签发失败几乎总是 EMAIL_UNSUBSCRIBE_SECRET 没配，
// 那是配置缺陷。把它放大成"整批发不出去"是一句 503 能查的事，而"信发出去了但没退订出口"
// 要靠收件人举报才发现 —— 所以这里既不能阻断发信，也不能不出声。
func (s *EmailSendService) unsubscribeLink(ctx context.Context, to string) string {
	if s.unsubLinker == nil {
		return ""
	}
	link, err := s.unsubLinker.GenerateUnsubscribeLink(ctx, to, "")
	if err != nil {
		s.unsubWarnOnce.Do(func() {
			logger.Warnf("邮件退订链接签发失败（本进程只报这一次），后续邮件将不带 List-Unsubscribe 头发出: %v", err)
		})
		return ""
	}
	return link
}

// resolveSmtpFromEnv 已退役（SaaS 配置面改造 B3，2026-11-17）：EMAIL_163_*/QQ_EMAIL*/SMTP_*
// 环境变量回退整体删除，发信通道唯一来源是显式 SmtpID 与 email_smtp 表。
