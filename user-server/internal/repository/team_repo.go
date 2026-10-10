package repository

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// team_repo.go 团队（租户）仓储（L5）。多租户 P0：teams 表 CRUD + 成员面。

// TeamRepository 团队仓储。
type TeamRepository struct{ db *gorm.DB }

func NewTeamRepository() *TeamRepository {
	return &TeamRepository{db: db.GetDB()}
}

// Available 分诊纪律同 MCPCredentialRepo：句柄缺失（503）与查询报错（500）分开。
func (r *TeamRepository) Available() bool { return r != nil && r.db != nil }

func (r *TeamRepository) Create(ctx context.Context, t *model.Team) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *TeamRepository) GetByID(ctx context.Context, id uint) (*model.Team, error) {
	var row model.Team
	if err := r.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *TeamRepository) GetByName(ctx context.Context, name string) (*model.Team, error) {
	var row model.Team
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *TeamRepository) Update(ctx context.Context, id uint, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.Team{}).Where("id = ?", id).Updates(updates).Error
}

func (r *TeamRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.Team{}, id).Error
}

// List 分页列表（平台管理员面；团队数量小，无需 keyset）。
func (r *TeamRepository) List(ctx context.Context, page, size int) ([]model.Team, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	q := r.db.WithContext(ctx).Model(&model.Team{})
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.Team
	err := q.Order("id ASC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	// 成员数聚合（团队量小，一条 GROUP BY 足够；非持久化列）
	if len(rows) > 0 {
		type cntRow struct {
			TeamID uint
			Cnt    int64
		}
		var counts []cntRow
		if err := r.db.WithContext(ctx).Model(&model.SystemUser{}).
			Select("team_id, COUNT(*) AS cnt").Group("team_id").Scan(&counts).Error; err == nil {
			m := map[uint]int64{}
			for _, c := range counts {
				m[c.TeamID] = c.Cnt
			}
			for i := range rows {
				rows[i].MemberCount = m[rows[i].ID]
			}
		}
	}
	return rows, total, nil
}

// CountMembers 团队成员数（system_users.team_id = team）。
func (r *TeamRepository) CountMembers(ctx context.Context, teamID uint) (int64, error) {
	var cnt int64
	err := r.db.WithContext(ctx).Model(&model.SystemUser{}).
		Where("team_id = ?", teamID).Count(&cnt).Error
	return cnt, err
}

// ListMembers 团队成员分页（团队管理员/平台管理员共用）。
func (r *TeamRepository) ListMembers(ctx context.Context, teamID uint, page, size int) ([]model.SystemUser, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	q := r.db.WithContext(ctx).Model(&model.SystemUser{}).Where("team_id = ?", teamID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.SystemUser
	err := q.Order("id ASC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}
