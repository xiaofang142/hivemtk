package service

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

	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

// EmailAccount 邮件账号配置（独立表，不依赖既有 schema）
type EmailAccount struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	Name       string    `gorm:"type:varchar(100)" json:"name"`
	Host       string    `gorm:"type:varchar(255);not null" json:"host"`
	Port       int       `gorm:"default:465" json:"port"`
	Username   string    `gorm:"type:varchar(255)" json:"username"`
	Password   string    `gorm:"type:varchar(255)" json:"-"`
	FromAddr   string    `gorm:"type:varchar(255);not null" json:"from_addr"`
	FromName   string    `gorm:"type:varchar(100)" json:"from_name"`
	UseSSL     bool      `gorm:"default:true" json:"use_ssl"`
	DailyQuota int       `gorm:"default:500" json:"daily_quota"`
	DailyUsed  int       `gorm:"default:0" json:"daily_used"`
	Status     string    `gorm:"type:varchar(20);default:'active'" json:"status"`
	CreatedAt  time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (EmailAccount) TableName() string { return "email_accounts" }

// EmailService 邮件发送服务（基于 SMTP，纯协议层，不依赖第三方 SaaS）
type EmailService struct {
	accRepo *repository.EmailAccountRepository
	hub     *MessageHubService
}

// NewEmailService 创建邮件服务
func NewEmailService(db *gorm.DB) *EmailService {
	return &EmailService{
		accRepo: repository.NewEmailAccountRepository(db),
		hub:     NewMessageHubServiceWithDB(db, nil),
	}
}

// NewEmailServiceAuto 创建邮件服务（自动从全局 DB 获取连接，用于 controller 层解耦）
func NewEmailServiceAuto() *EmailService {
	return NewEmailService(_db.GetDB())
}

// emailAccountFromRow repo 行模型 → service 账号模型（字段一一对应）
func emailAccountFromRow(r *repository.EmailAccountRow) *EmailAccount {
	if r == nil {
		return nil
	}
	return &EmailAccount{
		ID:         r.ID,
		Name:       r.Name,
		Host:       r.Host,
		Port:       r.Port,
		Username:   r.Username,
		Password:   r.Password,
		FromAddr:   r.FromAddr,
		FromName:   r.FromName,
		UseSSL:     r.UseSSL,
		DailyQuota: r.DailyQuota,
		DailyUsed:  r.DailyUsed,
		Status:     r.Status,
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

	var acc *EmailAccount
	if s.accRepo != nil {
		if accountID > 0 {
			row, err := s.accRepo.GetByID(ctx, accountID)
			if err != nil {
				return "", fmt.Errorf("email account not found: %w", err)
			}
			acc = emailAccountFromRow(row)
			if acc.ID > 0 && acc.DailyUsed >= acc.DailyQuota {
				return "", errors.New("email account quota exceeded")
			}
		} else {

			candidates, err := s.accRepo.ListActiveBelowQuota(ctx)
			if err != nil || len(candidates) == 0 {
				acc = emailFromEnv()
			} else {
				acc = emailAccountFromRow(&candidates[0])
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
		_, _ = s.hub.Push(ctx, &PushMessageRequest{
			Platform:       model.ChannelEmail,
			AccountID:      strconv.FormatUint(uint64(acc.ID), 10),
			MsgID:          msgID,
			Direction:      "outbound",
			MsgType:        "text",
			ReceiverID:     to,
			Content:        subject + "\n" + content,
			ConversationID: "email-" + to,
			IsAIReply:      true,
		})
	}

	logger.Infof("[Email] sent to=%s subject=%s msg_id=%s", to, subject, msgID)
	return msgID, nil
}

func (s *EmailService) smtpSend(ctx context.Context, acc *EmailAccount, to, subject, content string, attachments []string) (string, error) {
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
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
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

func init() { _db.RegisterExtraModels(&EmailAccount{}) }

func emailFromEnv() *EmailAccount {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		return nil
	}
	port, _ := strconv.Atoi(os.Getenv("SMTP_PORT"))
	if port == 0 {
		port = 465
	}
	useSSL := os.Getenv("SMTP_SSL") != "false"
	return &EmailAccount{
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
