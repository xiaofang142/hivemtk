package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// OutboundHubPusher 消息中枢出站台账的最小接缝（接口注入）：
// Send 成功发信后把台账推给根包的 MessageHubService。MessageHubService 属于
// bridge/channel 域（feishu/telegram/qq/wecom 共用），不能跟着 email 走；本子包
// 只认这个接口，由装配侧（根包 email_compat.go 给 *MessageHubService 挂
// PushEmailOutbound 方法）注入，保证拆包依赖单向：root → email，子包不反向依赖根包。
type OutboundHubPusher interface {
	PushEmailOutbound(ctx context.Context, rec EmailOutboundRecord)
}

// EmailOutboundRecord 一次成功出站的台账字段（MessageHubService.Push 入参里
// email 这条腿实际用到的字段子集）。Content 口径与既有 message_hub 台账一致：
// subject + "\n" + body。
type EmailOutboundRecord struct {
	MsgID      string
	AccountID  string
	ReceiverID string
	Content    string
}

// EmailService 邮件发送服务（基于 SMTP，纯协议层，不依赖第三方 SaaS）
type EmailService struct {
	accRepo *repository.EmailAccountRepository
	hub     OutboundHubPusher
}

// NewEmailService 创建邮件服务。hub 为出站台账注入位（nil 则只发不记账）。
func NewEmailService(hub OutboundHubPusher) *EmailService {
	return &EmailService{
		accRepo: repository.NewEmailAccountRepository(),
		hub:     hub,
	}
}

// NewEmailServiceAuto 创建邮件服务（无参仓储工厂，对齐 message_hub 先例）
func NewEmailServiceAuto(hub OutboundHubPusher) *EmailService {
	return &EmailService{
		accRepo: repository.NewEmailAccountRepository(),
		hub:     hub,
	}
}

// Send 发送邮件（通过指定 accountID 或默认账号）
func (s *EmailService) Send(ctx context.Context, accountID uint, to, subject, content string, attachments []string) (string, error) {
	if s == nil {
		return "", errors.New("email service not initialized")
	}
	if to == "" || subject == "" {
		return "", errors.New("to and subject are required")
	}

	var acc *model.EmailAccount
	if s.accRepo != nil {
		if accountID > 0 {
			row, err := s.accRepo.GetByID(ctx, accountID)
			if err != nil {
				return "", fmt.Errorf("email account not found: %w", err)
			}
			acc = row
			if acc.ID > 0 && acc.DailyUsed >= acc.DailyQuota {
				return "", errors.New("email account quota exceeded")
			}
		} else {

			candidates, err := s.accRepo.ListActiveBelowQuota(ctx)
			if err != nil || len(candidates) == 0 {
				acc = emailFromEnv()
			} else {
				acc = &candidates[0]
			}
		}
	}
	if acc == nil {
		acc = emailFromEnv()
	}
	if acc == nil {
		return "", errors.New("no email account configured (set SMTP_HOST etc. or create via API)")
	}

	msgID, err := s.smtpSend(ctx, acc, to, subject, content, attachments)
	if err != nil {
		return "", fmt.Errorf("smtp send: %w", err)
	}

	if s.accRepo != nil && acc.ID > 0 {
		// 配额计数自增失败不阻断发信，但必须留痕：静默漂移会导致超额发送
		if err := s.accRepo.IncDailyUsed(ctx, acc.ID); err != nil {
			logger.Warnf("[email] daily_used 自增失败 account=%d（配额计数漂移）: %v", acc.ID, err)
		}
	}

	if s.hub != nil {
		s.hub.PushEmailOutbound(ctx, EmailOutboundRecord{
			MsgID:      msgID,
			AccountID:  strconv.FormatUint(uint64(acc.ID), 10),
			ReceiverID: to,
			Content:    subject + "\n" + content,
		})
	}

	logger.Infof("[Email] sent to=%s subject=%s msg_id=%s", to, subject, msgID)
	return msgID, nil
}

func (s *EmailService) smtpSend(ctx context.Context, acc *model.EmailAccount, to, subject, content string, attachments []string) (string, error) {
	// 附件在这条路径上挂不上：下面是手写的单部件 text/html 报文，没有 multipart 能力。
	// 今天它确实没收到过附件，但这条前提不在看得见的位置，所以在这里把账算清。
	// Send 的非测试调用点共 7 处，其中 6 处传字面 nil（本包的 auth.go、r48_growth.go、
	// system_user.go、password_reset.go，controller/r44_gap_endpoints.go，以及 alert_checker.go
	// 里那个 EmailSender 抽象的调用点，它的实现就是本类型），第 7 处是 proactive reach 的
	// 注册闭包（proactive_reach.go 的 SetEmailRegistry 回调）原样透传形参，
	// 而它唯一的产出方 sendEmail 在同一文件里也传字面 nil。
	// 给 Send 加一个真带附件的调用，附件就会在这里静默丢掉：正解是走
	// email/service.EmailSendService，那条经 pkg/mail 的本站附件解析器会真挂上，
	// 挂不上也会出声（见 attachmentPaths）。
	_ = attachments
	addr := net.JoinHostPort(acc.Host, strconv.Itoa(acc.Port))
	from := acc.FromAddr
	if from == "" {
		from = acc.Username
	}
	header := make(map[string]string)
	header["From"] = fmt.Sprintf("%s <%s>", acc.FromName, from)
	header["To"] = to
	header["Subject"] = subject
	header["MIME-Version"] = "1.0"
	header["Content-Type"] = "text/html; charset=UTF-8"
	header["Date"] = time.Now().Format(time.RFC1123Z)

	var b strings.Builder
	for k, v := range header {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	b.WriteString(content)

	var auth smtp.Auth
	if acc.Username != "" && acc.Password != "" {
		auth = smtp.PlainAuth("", acc.Username, acc.Password, acc.Host)
	}

	msgID := fmt.Sprintf("em_%d", time.Now().UnixNano())

	sendFn := func() error {
		if acc.UseSSL {
			return sendMailSSL(addr, acc.Host, auth, from, []string{to}, []byte(b.String()))
		}
		return smtp.SendMail(addr, auth, from, []string{to}, []byte(b.String()))
	}

	done := make(chan error, 1)

	utils.SafeGo(ctx, "email.send", func(_ context.Context) { done <- sendFn() })
	select {
	case err := <-done:
		return msgID, err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func sendMailSSL(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return err
	}
	// 注册在 smtp.NewClient 之前：NewClient 在"TCP 通但对端不吐 220 问候行"时失败，
	// 那次 return 走在这里原本唯一关闭点（c.Close 的 defer）之前，TLS conn 就没人关。
	// LIFO 顺序是 c.Close() 先、conn.Close() 后，双关同一条底层连接是幂等的。
	defer func() { _ = conn.Close() }()
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return err
			}
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	return w.Close()
}

func emailFromEnv() *model.EmailAccount {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		return nil
	}
	port, _ := strconv.Atoi(os.Getenv("SMTP_PORT"))
	if port == 0 {
		port = 465
	}
	useSSL := os.Getenv("SMTP_SSL") != "false"
	return &model.EmailAccount{
		Name:     "env-default",
		Host:     host,
		Port:     port,
		Username: os.Getenv("SMTP_USER"),
		Password: os.Getenv("SMTP_PASSWORD"),
		FromAddr: os.Getenv("SMTP_FROM"),
		FromName: os.Getenv("SMTP_FROM_NAME"),
		UseSSL:   useSSL,
		Status:   "active",
	}
}
