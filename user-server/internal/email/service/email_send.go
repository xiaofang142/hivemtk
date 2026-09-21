package email

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

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

type EmailSendService struct {
	repo      repository.EmailSendRepository
	smtpRepo  repository.EmailSmtpRepository
	unsubRepo repository.EmailUnsubscribeRepository

	// deliver 是"把这一封真的投出去"的接缝，默认为 sendActualEmail（连真 SMTP）。
	// 抽出来的理由与 SMS 侧同源：合规判据与节拍逻辑要在测试里跑，而测试不该连 SMTP。
	deliver func(ctx context.Context, email *model.EmailSend) error
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

func (s *EmailSendService) isUnsubscribed(ctx context.Context, email string) bool {
	if s.unsubRepo == nil {
		s.unsubRepo = repository.NewEmailUnsubscribeRepository(nil)
	}
	exists, err := s.unsubRepo.ExistsByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return false
	}
	return exists
}

func NewEmailSendService() *EmailSendService {
	return &EmailSendService{
		repo:     repository.NewEmailSendRepository(),
		smtpRepo: repository.NewEmailSmtpRepository(),
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

		if s.isUnsubscribed(ctx, req.To) {
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
					logger.Errorf("邮件发送异步协程 panic [%s]: %v", emailSend.ID, r)
				}
			}()
			sendCtx := context.WithoutCancel(ctx)
			err := s.deliveryFn()(sendCtx, emailSend)
			emailUUID, parseErr := uuid.Parse(emailSend.ID)
			if parseErr != nil {
				logger.Errorf("邮件 ID 解析失败：%v", parseErr)
				return
			}
			if err != nil {
				logger.Errorf("邮件发送失败 [%s]: %v", emailSend.ID, err)
				if e := s.repo.UpdateStatus(sendCtx, emailUUID, EmailStatusFailed); e != nil {
					logger.Warnf("邮件状态落库失败(发送失败) [%s]: %v", emailSend.ID, e)
				}
			} else {
				if e := s.repo.UpdateStatus(sendCtx, emailUUID, EmailStatusSent); e != nil {
					logger.Warnf("邮件状态落库失败(发送成功) [%s]: %v", emailSend.ID, e)
				}
			}
		}()
	}

	return emailSend, nil
}

// ProcessPendingEmails 排期邮件的排水轮：回捞崩溃遗留 → 超龄判过期 → 认领到期 → 逐封投递。
//
// 顺序是有意义的：回捞必须排在过期与认领之前，否则上一进程遗留的 sending 行要空等一轮；
// 而过期必须排在认领之前，否则刚被回捞回来的老邮件会先被投出去、下一轮才被判过期。
//
// 三个步骤各自的失败都不阻断本轮剩下的事，但一律出声：这一格静默失败的原症状就是
// "邮件永远停在待发送、一行错误都没有"，不能再把它换成另一句没人看见的话。
func (s *EmailSendService) ProcessPendingEmails(ctx context.Context) error {
	deliver := s.deliveryFn()
	if deliver == nil {
		return errors.New("邮件投递实现未装配")
	}
	now := time.Now()

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
		return err
	}

	for _, email := range pendingEmails {
		s.deliverClaimed(ctx, email, deliver)
	}

	return nil
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
	if s.isUnsubscribed(ctx, email.To) {
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
	if email.SmtpID != "" {
		cfg, err := s.smtpRepo.GetByID(ctx, email.SmtpID)
		if err == nil && cfg != nil && cfg.Server != "" && cfg.Username != "" {
			smtpConfig = cfg
		} else if err != nil {
			logger.Warnf("邮件 SMTP 配置读取失败（SmtpID=%s），尝试环境变量兜底: %v", email.SmtpID, err)
		}
	}

	if smtpConfig == nil {
		smtpConfig = resolveSmtpFromEnv()
	}

	if smtpConfig == nil {
		logger.Errorf("邮件发送失败 [%s]: 无任何可用 SMTP 配置（SmtpID=%q 且未配置 EMAIL_163_*/QQ_EMAIL_*/SMTP_* 环境变量）",
			email.ID, email.SmtpID)
		return fmt.Errorf("邮件发送失败：未找到可用的 SMTP 配置（请配置 SmtpID 或 EMAIL_163_*/QQ_EMAIL_*/SMTP_* 环境变量）")
	}

	m := gomail.NewMessage()
	m.SetHeader("From", smtpConfig.Username)
	m.SetHeader("To", email.To)
	m.SetHeader("Subject", email.Subject)
	m.SetBody("text/html", email.Content)

	if email.Attachments != "" {
		attachments := strings.Split(email.Attachments, ",")
		for _, attachment := range attachments {
			attachment = strings.TrimSpace(attachment)
			if attachment == "" {
				continue
			}

			attachment = filepath.Base(strings.ReplaceAll(strings.ReplaceAll(attachment, "\\", "/"), "/", ""))
			if attachment == "" || attachment == "." || attachment == ".." {
				continue
			}
			safePath := filepath.Join("uploads", "attachments", attachment)
			if _, err := os.Stat(safePath); err != nil {
				continue
			}
			m.Attach(safePath)
		}
	}

	d := gomail.NewDialer(smtpConfig.Server, smtpConfig.Port, smtpConfig.Username, smtpConfig.Password)

	return d.DialAndSend(m)
}

func resolveSmtpFromEnv() *model.EmailSmtp {
	if user := os.Getenv("EMAIL_163_USER"); user != "" {
		if pwd := os.Getenv("EMAIL_163_PASSWORD"); pwd != "" {
			host := os.Getenv("EMAIL_163_SMTP_HOST")
			if host == "" {
				host = "smtp.163.com"
			}
			return &model.EmailSmtp{
				Name:     "env:163",
				Server:   host,
				Port:     465,
				Username: user,
				Password: pwd,
			}
		}
	}
	if qq := os.Getenv("QQ_EMAIL"); qq != "" {
		if pwd := os.Getenv("QQ_EMAIL_PASSWORD"); pwd != "" {
			return &model.EmailSmtp{
				Name:     "env:qq",
				Server:   "smtp.qq.com",
				Port:     465,
				Username: qq,
				Password: pwd,
			}
		}
	}
	if host := os.Getenv("SMTP_HOST"); host != "" {
		if user := os.Getenv("SMTP_USER"); user != "" {
			if pwd := os.Getenv("SMTP_PASSWORD"); pwd != "" {
				port := 465
				if p := os.Getenv("SMTP_PORT"); p != "" {
					if n, err := strconv.Atoi(p); err == nil && n > 0 {
						port = n
					}
				}
				return &model.EmailSmtp{
					Name:     "env:smtp",
					Server:   host,
					Port:     port,
					Username: user,
					Password: pwd,
				}
			}
		}
	}
	return nil
}
