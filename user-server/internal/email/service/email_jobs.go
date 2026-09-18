package email

import (
	"fmt"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"context"

	"github.com/google/uuid"
)

// EmailJobsService 任务服务
type EmailJobsService struct {
	repo repository.EmailJobsRepository
}

// NewEmailJobsService 创建任务服务实例
func NewEmailJobsService() *EmailJobsService {
	return &EmailJobsService{repo: repository.NewEmailJobsRepository()}
}

// CreateEmailJobs 创建任务
func (s *EmailJobsService) CreateEmailJobs(ctx context.Context, jobs model.EmailJobs) (*model.EmailJobs, error) {
	if err := s.repo.Create(ctx, &jobs); err != nil {
		return nil, err
	}
	return &jobs, nil
}

// GetEmailJobsByID 根据ID获取任务
func (s *EmailJobsService) GetEmailJobsByID(ctx context.Context, id uuid.UUID) (*model.EmailJobs, error) {
	return s.repo.GetByID(ctx, id)
}

// GetEmailJobsList 获取任务列表
func (s *EmailJobsService) GetEmailJobsList(ctx context.Context, page int, pageSize int) ([]*model.EmailJobs, int64, error) {
	return s.repo.List(ctx, page, pageSize)
}

// UpdateEmailJobs 更新任务
func (s *EmailJobsService) UpdateEmailJobs(ctx context.Context, jobs model.EmailJobs) error {
	return s.repo.Update(ctx, &jobs)
}

// DeleteEmailJobs 删除任务
func (s *EmailJobsService) DeleteEmailJobs(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}

// IncreaseSendTotal 增加发送总数
func (s *EmailJobsService) IncreaseSendTotal(ctx context.Context, jobs_id uuid.UUID) error {
	jobs, err := s.repo.GetByID(ctx, jobs_id)
	if err != nil {
		logger.Error(fmt.Errorf("failed to get email job: %v", err), "获取邮件任务失败")
		return err
	}
	if jobs == nil {
		logger.Error(fmt.Errorf("email job %s not found", jobs_id), "查找邮件任务失败")
		return fmt.Errorf("email job not found")
	}
	jobs.SendTotal++
	return s.repo.Update(ctx, jobs)
}

// IncreaseSuccessTotal 增加成功总数
func (s *EmailJobsService) IncreaseSuccessTotal(ctx context.Context, jobs_id uuid.UUID) error {
	jobs, err := s.repo.GetByID(ctx, jobs_id)
	if err != nil {
		return err
	}
	if jobs == nil {
		logger.Error(fmt.Errorf("email job %s not found", jobs_id), "查找邮件任务失败")
		return fmt.Errorf("email job not found")
	}
	jobs.SuccessTotal++
	return s.repo.Update(ctx, jobs)
}

// IncreaseFailTotal 增加失败总数
func (s *EmailJobsService) IncreaseFailTotal(ctx context.Context, jobs_id uuid.UUID) error {
	jobs, err := s.repo.GetByID(ctx, jobs_id)
	if err != nil {
		return err
	}
	if jobs == nil {
		logger.Error(fmt.Errorf("email job %s not found", jobs_id), "查找邮件任务失败")
		return fmt.Errorf("email job not found")
	}
	jobs.FailTotal++
	return s.repo.Update(ctx, jobs)
}

// IncreaseReadTotal 增加阅读总数
func (s *EmailJobsService) IncreaseReadTotal(ctx context.Context, jobs_id uuid.UUID) error {
	jobs, err := s.repo.GetByID(ctx, jobs_id)
	if err != nil {
		return err
	}
	if jobs == nil {
		logger.Error(fmt.Errorf("email job %s not found", jobs_id), "查找邮件任务失败")
		return fmt.Errorf("email job not found")
	}
	jobs.ReadTotal++
	return s.repo.Update(ctx, jobs)
}

// CreateEmailJobsDTO 通过请求 DTO 创建任务
func (s *EmailJobsService) CreateEmailJobsDTO(ctx context.Context, req dto.CreateEmailJobsRequest) (*dto.EmailJobsResponse, error) {
	created, err := s.CreateEmailJobs(ctx, model.EmailJobs{
		Subject:      req.Subject,
		EmailTotal:   req.EmailTotal,
		SendTotal:    req.SendTotal,
		ReadTotal:    req.ReadTotal,
		SuccessTotal: req.SuccessTotal,
		FailTotal:    req.FailTotal,
	})
	if err != nil {
		return nil, err
	}
	return toEmailJobsResponse(created), nil
}

// GetEmailJobsListDTO 获取任务列表（返回 DTO）
func (s *EmailJobsService) GetEmailJobsListDTO(ctx context.Context, page, pageSize int) (*dto.GetEmailJobsListResponse, error) {
	jobs, total, err := s.GetEmailJobsList(ctx, page, pageSize)
	if err != nil {
		return nil, err
	}
	list := make([]*dto.EmailJobsResponse, 0, len(jobs))
	for _, j := range jobs {
		list = append(list, toEmailJobsResponse(j))
	}
	return &dto.GetEmailJobsListResponse{List: list, Total: total}, nil
}

// GetEmailJobsByIDDTO 根据 ID 获取任务（返回 DTO）
func (s *EmailJobsService) GetEmailJobsByIDDTO(ctx context.Context, id uuid.UUID) (*dto.EmailJobsResponse, error) {
	j, err := s.GetEmailJobsByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return toEmailJobsResponse(j), nil
}

// UpdateEmailJobsDTO 通过请求 DTO 更新任务
func (s *EmailJobsService) UpdateEmailJobsDTO(ctx context.Context, req dto.UpdateEmailJobsRequest) error {
	id, err := uuid.Parse(req.ID)
	if err != nil {
		return err
	}
	return s.UpdateEmailJobs(ctx, model.EmailJobs{
		ID:           id,
		Subject:      req.Subject,
		EmailTotal:   req.EmailTotal,
		ReadTotal:    req.ReadTotal,
		SendTotal:    req.SendTotal,
		SuccessTotal: req.SuccessTotal,
		FailTotal:    req.FailTotal,
	})
}

func toEmailJobsResponse(j *model.EmailJobs) *dto.EmailJobsResponse {
	if j == nil {
		return nil
	}
	return &dto.EmailJobsResponse{
		ID:           j.ID.String(),
		Subject:      j.Subject,
		EmailTotal:   j.EmailTotal,
		SendTotal:    j.SendTotal,
		ReadTotal:    j.ReadTotal,
		SuccessTotal: j.SuccessTotal,
		FailTotal:    j.FailTotal,
		CreatedAt:    j.CreatedAt,
		UpdatedAt:    j.UpdatedAt,
	}
}
