package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"
)

// WhatsappService WhatsApp 账号 / 会话 / 群发作业的业务入口。
//
// A2-c 之后它**不直接持有任何协议客户端**：发送、登录、取二维码全部经
// `WhatsAppProvider`（见 whatsapp_provider.go）。这一层的职责只剩
// 「读账号 → 选实现 → 计费/节奏校验 → 委派」。W-3 换库收口（2026-10-25）
// 删掉个人版实现时，本文件确实只动了三行：legacy 字段、构造、loginProvider。
type WhatsappService struct {
	repo     repository.WhatsappRepository
	clueRepo repository.ClueRepository

	// cloud Cloud API 实现（无状态，按账号凭据临时构造客户端）。
	cloud WhatsAppProvider
}

// NewWhatsappService 构造服务：线索仓储用装配点注入的连接（db 为 nil 时该依赖不可用，
// 与调用方显式传入的语义一致，不回落全局句柄）。
func NewWhatsappService(clueRepo repository.ClueRepository) *WhatsappService {
	repo := repository.NewWhatsappRepository()
	return &WhatsappService{
		repo:     repo,
		clueRepo: clueRepo,
		cloud:    cloudWhatsAppProvider{},
	}
}

// providerFor 选账号对应的发送实现。
func (s *WhatsappService) providerFor(acc *model.WhatsappAccount) WhatsAppProvider {
	if acc != nil && acc.Type == "cloud" && acc.PhoneID != "" && acc.Token != "" {
		if s.cloud != nil {
			return s.cloud
		}
		return cloudWhatsAppProvider{}
	}
	return whatsAppProviderFor(acc)
}

// Accounts
func (s *WhatsappService) CreateAccount(ctx context.Context, name, remark string) (*model.WhatsappAccount, error) {
	acc := &model.WhatsappAccount{Name: name, Remark: remark, Status: model.WhatsappStatusPending}
	return acc, s.repo.CreateAccount(ctx, acc)
}

func (s *WhatsappService) ListAccounts(ctx context.Context) ([]*model.WhatsappAccount, error) {
	return s.repo.ListAccounts(ctx)
}

func (s *WhatsappService) UpdateAccount(ctx context.Context, acc *model.WhatsappAccount) error {
	return s.repo.UpdateAccount(ctx, acc)
}

func (s *WhatsappService) DeleteAccount(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteAccount(ctx, id)
}

func (s *WhatsappService) GetAccount(ctx context.Context, id uuid.UUID) (*model.WhatsappAccount, error) {
	return s.repo.GetAccount(ctx, id)
}

// StartLogin / GetLoginQR 个人版扫码登录已随 gowa 下线（W-3 换库收口）。
// 端点保留并回**明确错误**（老前端拿到文案而不是 404/panic）；Cloud API 账号
// 本就没有扫码登录这一步。个人版存量会话行（whatsapp_sessions）只读不写，
// LoginStatus 的读路径不受影响。
func (s *WhatsappService) StartLogin(_ context.Context, _ uuid.UUID, _ time.Duration) (string, error) {
	return "", fmt.Errorf("%w: 个人版扫码登录已下线（gowa 弃维护，W-3 换库收口），请使用 Cloud API 账号（PhoneID+Token）",
		ErrWhatsAppProviderUnsupported)
}

func (s *WhatsappService) GetLoginQR(_ context.Context, _ uuid.UUID) (string, bool) {
	return "", false
}

func (s *WhatsappService) LoginStatus(ctx context.Context, accountID uuid.UUID) (bool, error) {
	sess, err := s.repo.GetSession(ctx, accountID)
	if err != nil {
		return false, err
	}
	return sess != nil, nil
}

// SendTextMessage 发送文本消息到指定 JID。
// 兼容个人版（gowa，见 whatsapp_provider.go 的 seam 说明）与 Cloud API 两种账号。
func (s *WhatsappService) SendTextMessage(ctx context.Context, accountID uuid.UUID, toJID, text string) (string, error) {
	acc, err := s.repo.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	if acc == nil {
		return "", errors.New("whatsapp account not found")
	}
	if err := enforceWhatsAppTierPacing(toJID, "", time.Now()); err != nil {
		return "", err
	}
	return s.providerFor(acc).SendText(ctx, acc, toJID, text)
}

// SendTemplateMessage 发送模板消息（客服窗口外必须走模板）。
func (s *WhatsappService) SendTemplateMessage(ctx context.Context, accountID uuid.UUID, to, templateName, language string, params map[string]string) (string, error) {
	acc, err := s.repo.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	if acc == nil {
		return "", errors.New("whatsapp account not found")
	}
	if acc.PhoneID == "" || acc.Token == "" {
		// 模板消息只有 Cloud API 支持；个人版即便有会话也发不了模板。
		// 这条错误串保留原文案：既有前端/文档按它提示「去填 Cloud API 凭据」。
		return "", errors.New("cloud api credentials missing")
	}
	if err := enforceWhatsAppTierPacing(to, templateName, time.Now()); err != nil {
		return "", err
	}
	return s.providerFor(acc).SendTemplate(ctx, acc, to, templateName, language, params)
}

// Drafts
func (s *WhatsappService) CreateDraft(ctx context.Context, title, content string) (*model.WhatsappDraft, error) {
	d := &model.WhatsappDraft{Title: title, Content: content}
	return d, s.repo.CreateDraft(ctx, d)
}
func (s *WhatsappService) ListDrafts(ctx context.Context, page, pageSize int) ([]*model.WhatsappDraft, int64, error) {
	return s.repo.ListDrafts(ctx, page, pageSize)
}
func (s *WhatsappService) UpdateDraft(ctx context.Context, d *model.WhatsappDraft) error {
	return s.repo.UpdateDraft(ctx, d)
}
func (s *WhatsappService) DeleteDraft(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteDraft(ctx, id)
}
func (s *WhatsappService) GetDraft(ctx context.Context, id uuid.UUID) (*model.WhatsappDraft, error) {
	return s.repo.GetDraft(ctx, id)
}

func (s *WhatsappService) CreateBulkJob(ctx context.Context, draftID uuid.UUID) (*model.WhatsappJob, error) {
	draft, err := s.repo.GetDraft(ctx, draftID)
	if err != nil {
		return nil, err
	}
	if draft == nil {
		return nil, errors.New("draft not found")
	}
	job := &model.WhatsappJob{DraftID: draft.ID, Status: model.WhatsappJobPending}
	if err := s.repo.CreateJob(ctx, job); err != nil {
		return nil, err
	}

	clues, total, err := s.clueRepo.GetWhatsappClues(ctx)
	if err != nil {
		return nil, err
	}
	job.Total = total
	_ = s.repo.UpdateJob(ctx, job)

	accounts, err := s.repo.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, errors.New("no accounts available")
	}

	utils.SafeGo(ctx, "whatsapp.SendBulk", func(ctx context.Context) {
		job.Status = model.WhatsappJobRunning
		_ = s.repo.UpdateJob(ctx, job)
		idx := 0
		for _, clue := range clues {
			acc := accounts[idx%len(accounts)]
			idx++
			toJid := fmt.Sprintf("%s@s.whatsapp.net", clue.Account)
			detail := &model.WhatsappJobDetail{JobID: job.ID, AccountID: acc.ID, ToJid: toJid}
			_ = s.repo.CreateJobDetail(ctx, detail)
			// 群发同样走 provider：Cloud API 账号与个人版账号在同一个批次里混排时，
			// 原来那条"先 ensureConn 再 gowa.Send"的路径对 Cloud 账号必然失败。
			if _, err := s.providerFor(acc).SendText(ctx, acc, toJid, draft.Content); err != nil {
				detail.Status = model.WhatsappJobDetailFailed
				detail.ErrorMsg = err.Error()
				_ = s.repo.UpdateJobDetail(ctx, detail)
				job.Failed++
				_ = s.repo.UpdateJob(ctx, job)
				continue
			}
			detail.Status = model.WhatsappJobDetailSuccess
			_ = s.repo.UpdateJobDetail(ctx, detail)
			job.Success++
			_ = s.repo.UpdateJob(ctx, job)
			time.Sleep(500 * time.Millisecond)
		}
		job.Status = model.WhatsappJobFinished
		_ = s.repo.UpdateJob(ctx, job)
	})

	return job, nil
}

// ListJobs lists all bulk send jobs
func (s *WhatsappService) ListJobs(ctx context.Context, page, pageSize int) ([]*model.WhatsappJob, int64, error) {
	return s.repo.ListJobs(ctx, page, pageSize)
}

// GetJob returns a single bulk send job by ID
func (s *WhatsappService) GetJob(ctx context.Context, id uuid.UUID) (*model.WhatsappJob, error) {
	return s.repo.GetJob(ctx, id)
}

// DeleteJob deletes a bulk send job by ID
func (s *WhatsappService) DeleteJob(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteJob(ctx, id)
}
