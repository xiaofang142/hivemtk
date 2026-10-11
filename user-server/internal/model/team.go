package model

import "time"

// Team 租户（团队）模型——多租户 P0 的锚点表。
//
// 语义（与 docs/multi-tenant-rbac-saas-plan.md P0 对齐）：
//   - 每个业务行通过 team_id 归属一个团队；team_id=0 是"平台侧"保留值
//     （平台管理员 role=admin 直通全量，不依赖具体团队）。
//   - 存量数据由 postMigrateBackfillDefaultTeam 一次性归入「默认团队」；
//     该团队不可删除（回填与注册流程都以它为兜底归属）。
//   - Plan 是 SaaS 计费预留位（P2 起消费），P0 只存不判。
type Team struct {
	ID   uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Name string `gorm:"type:varchar(128);not null;uniqueIndex" json:"name"`
	// Plan 计费档位预留：free / pro / enterprise（P0 只登记不判定配额）
	Plan string `gorm:"type:varchar(32);default:'free'" json:"plan"`
	// Status 生命周期：active / disabled（停用后成员登录仍可，业务写入面在 P2 收紧）
	Status string `gorm:"type:varchar(16);default:'active';index" json:"status"`
	// OwnerUserID 团队主管理员（system_users.id，可 NULL=暂无）
	OwnerUserID *uint `gorm:"column:owner_user_id;index" json:"owner_user_id"`
	// MemberCount 成员数（非持久化列，TeamRepository.List 聚合填充）
	MemberCount int64  `gorm:"-" json:"member_count"`
	Remark      string `gorm:"type:varchar(255)" json:"remark"`
	// BrandName / BrandLogoURL 团队品牌（SaaS P2）：登录页/团队页展示位，
	// 平台级品牌仍在 system_config（LogoURL/SiteName）——团队级只在自身触达面生效。
	BrandName    string    `gorm:"column:brand_name;type:varchar(64)" json:"brand_name"`
	BrandLogoURL string    `gorm:"column:brand_logo_url;type:varchar(500)" json:"brand_logo_url"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (Team) TableName() string { return "teams" }

// 团队状态与计费档位字面量（controller/前端共用口径）。
const (
	TeamStatusActive   = "active"
	TeamStatusDisabled = "disabled"
)

const (
	TeamPlanFree       = "free"
	TeamPlanPro        = "pro"
	TeamPlanEnterprise = "enterprise"
)

// TeamStatusValid / TeamPlanValid 防脏值入库。
func TeamStatusValid(s string) bool {
	return s == TeamStatusActive || s == TeamStatusDisabled
}

func TeamPlanValid(s string) bool {
	return s == TeamPlanFree || s == TeamPlanPro || s == TeamPlanEnterprise
}

// DefaultTeamName 存量回填与注册兜底使用的默认团队名。
const DefaultTeamName = "默认团队"
