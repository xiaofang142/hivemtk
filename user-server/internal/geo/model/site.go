package model

import (
	"time"

	"github.com/google/uuid"

	"gorm.io/gorm"
)

// GeoSite 站点部署配置
//
// 每个品牌可以配一个主域名，关联 Hugo 本地路径、Git 仓库、
// Cloudflare Pages 项目名。SiteDeployService 从这里读配置。
type GeoSite struct {
	ID           string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	Domain       string     `gorm:"type:varchar(255);uniqueIndex" json:"domain"`
	Provider     string     `gorm:"type:varchar(50);default:'cloudflare'" json:"provider"`
	HugoPath     string     `gorm:"type:varchar(500)" json:"hugo_path"`
	GitRepo      string     `gorm:"type:varchar(255)" json:"git_repo"`
	DeployCmd    string     `gorm:"type:varchar(255)" json:"deploy_cmd"`
	HealthScore  int        `gorm:"default:0" json:"health_score"`
	LastDeployAt *time.Time `json:"last_deploy_at"`
	Active       bool       `gorm:"default:true" json:"active"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (GeoSite) TableName() string { return "geo_sites" }

func (s *GeoSite) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}
