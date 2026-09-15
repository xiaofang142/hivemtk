package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// LiveCode 活码模型
type LiveCode struct {
	ID              string    `json:"id" gorm:"type:varchar(36);primary_key"`
	Name            string    `json:"name" gorm:"size:100;not null"`
	ShortLink       string    `json:"short_link" gorm:"size:50;uniqueIndex"`
	ShortDomainID   int       `json:"short_domain_id" gorm:"not null"`
	EntryDomainID   int       `json:"entry_domain_id" gorm:"not null"`
	LandingDomainID int       `json:"landing_domain_id" gorm:"not null"`
	Status          int       `json:"status" gorm:"default:1"`
	TotalViews      int       `json:"total_views" gorm:"default:0"`
	TodayViews      int       `json:"today_views" gorm:"default:0"`
	TotalClicks     int       `json:"total_clicks" gorm:"default:0"`
	DailyClicks     int       `json:"daily_clicks" gorm:"default:0"`
	ImageURL        string    `json:"image_url" gorm:"size:500"`
	EntryURL        string    `json:"entry_url" gorm:"size:500"`
	LandingURL      string    `json:"landing_url" gorm:"size:500"`
	CreatedAt       time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt       time.Time `json:"updated_at" gorm:"autoUpdateTime"`

	ShortDomain   *DomainPool    `json:"short_domain" gorm:"foreignKey:ShortDomainID;references:id"`
	EntryDomain   *DomainPool    `json:"entry_domain" gorm:"foreignKey:EntryDomainID;references:id"`
	LandingDomain *DomainPool    `json:"landing_domain" gorm:"foreignKey:LandingDomainID;references:id"`
	QRCodes       []LiveCodeQR   `json:"qr_codes" gorm:"foreignKey:LiveCodeID"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 返回表名
func (LiveCode) TableName() string {
	return "live_codes"
}

// BeforeCreate GORM钩子，在创建前生成ID
func (l *LiveCode) BeforeCreate(tx *gorm.DB) error {
	if l.ID == "" {
		l.ID = uuid.New().String()
	}
	return nil
}
