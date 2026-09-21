package email

import (
	"context"
	"errors"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/crypto"
	"hivemtk-user/internal/repository"
)

type EmailSmtpService struct {
	repo repository.EmailSmtpRepository
}

func NewEmailSmtpService() *EmailSmtpService {
	return &EmailSmtpService{repo: repository.NewEmailSmtpRepository()}
}

func (s *EmailSmtpService) CreateEmailSmtp(ctx context.Context, emailSmtp model.EmailSmtp) (*model.EmailSmtp, error) {

	enc, err := crypto.Encrypt(emailSmtp.Password)
	if err != nil {
		return nil, errors.New("SMTP 密码加密失败（FIELD_ENCRYPTION_KEY 未配置或无效），已拒绝明文落库: " + err.Error())
	}
	emailSmtp.Password = enc
	if err := s.repo.Create(ctx, &emailSmtp); err != nil {
		return nil, err
	}
	return &emailSmtp, nil
}

func (s *EmailSmtpService) GetEmailSmtp(ctx context.Context, id string) (*model.EmailSmtp, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *EmailSmtpService) GetEmailSmtpList(ctx context.Context) ([]*model.EmailSmtp, error) {
	return s.repo.GetEmailSmtpList(ctx)
}

func (s *EmailSmtpService) UpdateEmailSmtp(ctx context.Context, emailSmtp model.EmailSmtp) error {

	if emailSmtp.Password != "" {
		enc, err := crypto.Encrypt(emailSmtp.Password)
		if err != nil {
			return errors.New("SMTP 密码加密失败（FIELD_ENCRYPTION_KEY 未配置或无效），已拒绝明文落库: " + err.Error())
		}
		emailSmtp.Password = enc
	}
	return s.repo.Update(ctx, &emailSmtp)
}

func (s *EmailSmtpService) DeleteEmailSmtp(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

func (s *EmailSmtpService) GetRandEmailSmtp(ctx context.Context) (*model.EmailSmtp, error) {
	emailSmtpList, err := s.repo.GetEmailSmtpList(ctx)
	if err != nil {
		return nil, err
	}

	for i := range emailSmtpList {
		if dec, derr := crypto.Decrypt(emailSmtpList[i].Password); derr == nil && dec != "" {
			emailSmtpList[i].Password = dec
		}
	}
	emailListService := NewEmailListService()
	for _, emailSmtp := range emailSmtpList {
		// 记账键必须是 Username：email_list.from 落的是发信账号，读展示名等于永远数到 0，
		// Limit 形同不存在（日上限是唯一挡着发信域被拉黑的闸门）。
		todayCount, err := emailListService.GetTodayCountByFrom(ctx, emailSmtp.Username)
		if err != nil {
			return nil, err
		}
		if todayCount < emailSmtp.Limit {
			return emailSmtp, nil
		}
	}
	return nil, errors.New("没有找到可用的smtp")
}

// CreateEmailSmtpDTO 通过请求 DTO 创建 SMTP 配置
func (s *EmailSmtpService) CreateEmailSmtpDTO(ctx context.Context, req dto.CreateEmailSmtpRequest) (*dto.EmailSmtpResponse, error) {
	created, err := s.CreateEmailSmtp(ctx, model.EmailSmtp{
		Name:     req.Name,
		Server:   req.Server,
		Port:     req.Port,
		Username: req.Username,
		Password: req.Password,
		Limit:    req.Limit,
	})
	if err != nil {
		return nil, err
	}
	return toEmailSmtpResponse(created), nil
}

// GetEmailSmtpListDTO 获取 SMTP 配置列表（返回 DTO）
func (s *EmailSmtpService) GetEmailSmtpListDTO(ctx context.Context) (*dto.GetEmailSmtpListResponse, error) {
	list, err := s.GetEmailSmtpList(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*dto.EmailSmtpResponse, 0, len(list))
	for _, item := range list {
		result = append(result, toEmailSmtpResponse(item))
	}
	return &dto.GetEmailSmtpListResponse{List: result, Total: int64(len(result))}, nil
}

// GetEmailSmtpDTO 根据 ID 获取 SMTP 配置（返回 DTO）
func (s *EmailSmtpService) GetEmailSmtpDTO(ctx context.Context, id string) (*dto.EmailSmtpResponse, error) {
	item, err := s.GetEmailSmtp(ctx, id)
	if err != nil {
		return nil, err
	}
	return toEmailSmtpResponse(item), nil
}

func (s *EmailSmtpService) UpdateEmailSmtpDTO(ctx context.Context, req dto.UpdateEmailSmtpRequest) error {
	if req.Password == "" {
		old, err := s.GetEmailSmtp(ctx, req.ID)
		if err != nil {
			return err
		}
		req.Password = old.Password
	}
	// 零值=保留原值：防止部分字段漏传把端口/限额静默清零（对齐 Password 的保留语义）
	if req.Port == 0 || req.Limit == 0 {
		old, err := s.GetEmailSmtp(ctx, req.ID)
		if err != nil {
			return err
		}
		if req.Port == 0 {
			req.Port = old.Port
		}
		if req.Limit == 0 {
			req.Limit = old.Limit
		}
	}
	return s.UpdateEmailSmtp(ctx, model.EmailSmtp{
		ID:       req.ID,
		Name:     req.Name,
		Server:   req.Server,
		Port:     req.Port,
		Username: req.Username,
		Password: req.Password,
		Limit:    req.Limit,
	})
}

func toEmailSmtpResponse(s *model.EmailSmtp) *dto.EmailSmtpResponse {
	if s == nil {
		return nil
	}
	return &dto.EmailSmtpResponse{
		ID:       s.ID,
		Name:     s.Name,
		Server:   s.Server,
		Port:     s.Port,
		Username: s.Username,
		Limit:    s.Limit,
	}
}
