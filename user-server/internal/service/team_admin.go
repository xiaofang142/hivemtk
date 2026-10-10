package service

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"
)

// team_admin.go 团队（租户）管理服务（L3 controller → L4 service → L5 repository）。
//
// 多租户 P0 的管理面：
//   - 平台管理员（role=admin）：团队全量 CRUD、成员跨团队调拨、任免团队管理员；
//   - 团队管理员（role=team_admin）：仅本团队——列成员、授/撤本团队 team_admin、
//     改本团队备注；建团/跨团队调拨/删团是平台管理员专属。
// 服务端强制，前端只做显隐（硬约束）。

// EnsureDefaultTeamID 保证「默认团队」存在并返回其 id（幂等，注册与回填共用）。
// 并发双开用 name 唯一索引兜底：撞唯一冲突的一方回查即可。
func EnsureDefaultTeamID(ctx context.Context) uint {
	repo := repository.NewTeamRepository()
	if !repo.Available() {
		return 0
	}
	if t, err := repo.GetByName(ctx, model.DefaultTeamName); err == nil && t != nil {
		return t.ID
	}
	t := &model.Team{Name: model.DefaultTeamName, Plan: model.TeamPlanFree, Status: model.TeamStatusActive}
	if err := repo.Create(ctx, t); err != nil {
		if again, e2 := repo.GetByName(ctx, model.DefaultTeamName); e2 == nil && again != nil {
			return again.ID
		}
		return 0
	}
	return t.ID
}

// TeamAdminService 团队管理。
type TeamAdminService struct {
	repo     *repository.TeamRepository
	userRepo repository.SystemUserRepository
}

func NewTeamAdminService() *TeamAdminService {
	r := repository.NewTeamRepository()
	if !r.Available() {
		// userRepo 与 team repo 同源（db.GetDB()）：team 句柄缺失即整体未装配，
		// 503 分诊以 team repo 为准（分诊纪律同 McpCredentialAdminService）。
		return &TeamAdminService{}
	}
	return &TeamAdminService{repo: r, userRepo: repository.NewSystemUserRepository()}
}

func (s *TeamAdminService) Ready() bool { return s.repo != nil }

// ---- 平台管理员面 ----

func (s *TeamAdminService) List(ctx context.Context, page, size int) ([]model.Team, int64, error) {
	return s.repo.List(ctx, page, size)
}

func (s *TeamAdminService) Get(ctx context.Context, id uint) (*model.Team, error) {
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *TeamAdminService) Create(ctx context.Context, name, plan, remark string, ownerUserID *uint) (*model.Team, error) {
	if name == "" {
		return nil, errors.New("团队名称不能为空")
	}
	if plan != "" && !model.TeamPlanValid(plan) {
		return nil, errors.New("计费档位非法（free/pro/enterprise）")
	}
	if existing, _ := s.repo.GetByName(ctx, name); existing != nil {
		return nil, fmt.Errorf("团队名 %q 已存在", name)
	}
	if plan == "" {
		plan = model.TeamPlanFree
	}
	t := &model.Team{Name: name, Plan: plan, Status: model.TeamStatusActive, OwnerUserID: ownerUserID, Remark: remark}
	if err := s.repo.Create(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// Update 名称/档位/状态/主管理员/备注（部分更新，nil=不动）。
func (s *TeamAdminService) Update(ctx context.Context, id uint, name, plan, status, remark *string, ownerUserID *uint) (*model.Team, error) {
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{}
	if name != nil && *name != "" {
		updates["name"] = *name
	}
	if plan != nil {
		if !model.TeamPlanValid(*plan) {
			return nil, errors.New("计费档位非法")
		}
		updates["plan"] = *plan
	}
	if status != nil {
		if !model.TeamStatusValid(*status) {
			return nil, errors.New("状态非法（active/disabled）")
		}
		updates["status"] = *status
	}
	if remark != nil {
		updates["remark"] = *remark
	}
	if ownerUserID != nil {
		updates["owner_user_id"] = *ownerUserID
	}
	if len(updates) == 0 {
		return t, nil
	}
	if err := s.repo.Update(ctx, id, updates); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, id)
}

// Delete 删除团队：成员未清空时拒绝（409 语义由 controller 映射）——
// 团队删除是 P2 租户生命周期的收尾动作，P0 只放行空团队，防误删吞掉业务行归属。
func (s *TeamAdminService) Delete(ctx context.Context, id uint) error {
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if t.Name == model.DefaultTeamName {
		return errors.New("默认团队不可删除（存量归属与注册兜底都指向它）")
	}
	cnt, err := s.repo.CountMembers(ctx, id)
	if err != nil {
		return err
	}
	if cnt > 0 {
		return fmt.Errorf("团队仍有 %d 名成员，请先移出成员再删除", cnt)
	}
	return s.repo.Delete(ctx, id)
}

// ---- 成员面（平台管理员全量；团队管理员仅本团队） ----

// ListMembers 成员列表。callerIsPlatform 由 controller 依据 JWT role 传入。
func (s *TeamAdminService) ListMembers(ctx context.Context, teamID uint, callerIsPlatform bool, callerTeamID uint, page, size int) ([]model.SystemUser, int64, error) {
	if err := s.authorizeTeamAccess(ctx, teamID, callerIsPlatform, callerTeamID); err != nil {
		return nil, 0, err
	}
	return s.repo.ListMembers(ctx, teamID, page, size)
}

// SetMemberRole 在团队内授/撤 team_admin（user ↔ team_admin）。
// 平台管理员可操作任何团队；团队管理员只能操作本团队，且不能动自己以外的
// 平台管理员（本团队里本就没有平台管理员，这层是纵深）。
func (s *TeamAdminService) SetMemberRole(ctx context.Context, teamID, userID uint, role string, callerIsPlatform bool, callerTeamID uint) (*model.SystemUser, error) {
	if err := s.authorizeTeamAccess(ctx, teamID, callerIsPlatform, callerTeamID); err != nil {
		return nil, err
	}
	if role != model.SystemUserRoleUser && role != model.SystemUserRoleTeamAdmin {
		return nil, errors.New("团队内角色只能是 user 或 team_admin")
	}
	u, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, errors.New("用户不存在")
	}
	if u.TeamID != teamID {
		return nil, errors.New("用户不属于该团队")
	}
	u.Role = role
	if err := s.userRepo.Update(ctx, u); err != nil {
		return nil, err
	}
	return s.userRepo.GetByID(ctx, userID)
}

// MoveMember 把现有用户调入/调出团队（toTeamID=0 表示调入平台侧）。
// 平台管理员专属；团队变更后该用户全部令牌即时吊销（新团队身份必须重新登录生效）。
func (s *TeamAdminService) MoveMember(ctx context.Context, userID, toTeamID uint) (*model.SystemUser, error) {
	u, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, errors.New("用户不存在")
	}
	if u.Role == model.SystemUserRoleAdmin {
		return nil, errors.New("平台管理员不属于任何团队，不可调拨")
	}
	if toTeamID != 0 {
		if _, err := s.repo.GetByID(ctx, toTeamID); err != nil {
			return nil, errors.New("目标团队不存在")
		}
		// 计费配额闸（SaaS P2）：有订阅的团队按方案 MaxUsers 限员；未订阅不限。
		if err := NewBillingAdminService().CheckMemberQuota(ctx, toTeamID); err != nil {
			return nil, err
		}
	}
	u.TeamID = toTeamID
	if err := s.userRepo.Update(ctx, u); err != nil {
		return nil, err
	}
	// 团队变了，旧 token 里的 team_id 立即作废（新令牌由下次登录/刷新签发）。
	utils.RevokeUserTokens(ctx, userID)
	return s.userRepo.GetByID(ctx, userID)
}

// authorizeTeamAccess 团队归属校验：平台管理员直通；团队管理员必须本团队。
func (s *TeamAdminService) authorizeTeamAccess(ctx context.Context, teamID uint, callerIsPlatform bool, callerTeamID uint) error {
	if callerIsPlatform {
		return nil
	}
	if callerTeamID != teamID {
		return errTeamForbidden
	}
	return nil
}

// errTeamForbidden 跨团队访问（403 语义；controller 映射）。
var errTeamForbidden = errors.New("无权访问其他团队")

// IsTeamForbidden controller 判 403/404 用。
func IsTeamForbidden(err error) bool { return errors.Is(err, errTeamForbidden) }
