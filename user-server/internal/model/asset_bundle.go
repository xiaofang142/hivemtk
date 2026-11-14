// Package model 提供 AI 资产包（AssetBundle）的 GORM 实体定义。
//
// 方向9：资产包模式 - 100% 遵守 OpenAI 协议
// 文档依据：docs/企业级架构优化/资产包模式.md
//
// 核心思想：
//  1. 资产包 = 100% 遵守 OpenAI messages 协议的 JSON 数组
//  2. 后端用 GORM JSONB 存储 messages 数组（既支持 OpenAI 标准又便于查询）
//  3. Weave 织布算法动态拼装最终 prompt（资产包 + RAG + 历史 + 当前问题）
//
// 命名规范遵循五层架构：
//   - 此文件属于 model 层：仅含实体定义，禁止业务方法
//   - CRUD 操作 → repository/asset_bundle_repo.go
//   - 业务逻辑 → service/asset_bundle.go
//   - API DTO → dto/asset_bundle.go
//   - HTTP 路由 → controller/asset_bundle.go
package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

// AssetBundleScope 资产包作用域（私有/共享/官方）
type AssetBundleScope string

const (
	AssetBundleScopePrivate  AssetBundleScope = "private"
	AssetBundleScopeShared   AssetBundleScope = "shared"
	AssetBundleScopeOfficial AssetBundleScope = "official"
)

// AssetBundleStatus 资产包状态
type AssetBundleStatus string

const (
	AssetBundleStatusDraft    AssetBundleStatus = "draft"
	AssetBundleStatusActive   AssetBundleStatus = "active"
	AssetBundleStatusInactive AssetBundleStatus = "inactive"
	AssetBundleStatusArchived AssetBundleStatus = "archived"
)

// AssetBundleMessage 资产包内的单条消息（OpenAI ChatML 协议）
type AssetBundleMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
}

// AssetBundleMessages 消息数组（实现 driver.Valuer / sql.Scanner 让 GORM 透明序列化到 JSONB）
type AssetBundleMessages []AssetBundleMessage

// Value 序列化到 JSONB
func (m AssetBundleMessages) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

// Scan 从 JSONB 反序列化
func (m *AssetBundleMessages) Scan(src any) error {
	if src == nil {
		*m = nil
		return nil
	}
	var data []byte
	switch v := src.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return errors.New("AssetBundleMessages.Scan: unsupported type")
	}
	return json.Unmarshal(data, m)
}

// AssetBundle AI 资产包主表
//
// 设计原则：
//  1. AssetID 在租户内唯一（uniqueIndex 由 tenancyidx.Normalize 复合为
//     (tenant_id, asset_id)；不同租户可各自拥有同一 AssetID 的资产包）
//  2. Messages 字段是 JSONB，100% 遵守 OpenAI ChatML 协议
//  3. Author 字段记录开发者，Title 给人看
//  4. Version 语义化版本号
//  5. Scope 区分私有/共享/官方
//  6. Tags 数组便于多维筛选（行业/语言/品类）
type AssetBundle struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID                 int64               `gorm:"primaryKey;autoIncrement" json:"id"`
	AssetID            string              `gorm:"size:64;uniqueIndex;not null" json:"asset_id"`
	Title              string              `gorm:"size:256;not null" json:"title"`
	Description        string              `gorm:"type:text" json:"description"`
	Author             string              `gorm:"size:64;index" json:"author"`
	Version            string              `gorm:"size:16;default:'1.0.0'" json:"version"`
	Scope              AssetBundleScope    `gorm:"size:16;index;default:'private'" json:"scope"`
	Status             AssetBundleStatus   `gorm:"size:16;index;default:'draft'" json:"status"`
	Industry           string              `gorm:"size:32;index" json:"industry"`
	Language           string              `gorm:"size:8;index;default:'zh'" json:"language"`
	Tags               pq.StringArray      `gorm:"type:text[];default:'{}'" json:"tags"`
	Messages           AssetBundleMessages `gorm:"type:jsonb;not null;default:'[]'" json:"messages"`
	Examples           JSONArray           `gorm:"type:jsonb;column:examples;default:'[]'" json:"examples"`
	SupportedLanguages pq.StringArray      `gorm:"type:text[];column:supported_languages;default:'{}'" json:"supported_languages"`
	UseCount           int64               `gorm:"default:0" json:"use_count"`
	Rating             float64             `gorm:"default:0" json:"rating"`
	RatingCount        int                 `gorm:"default:0" json:"rating_count"`
	// CoverImage 资产包封面 URL（由统一存储服务返回，值形如 /files/covers/2026/09/03/uuid.jpg）
	CoverImage string `gorm:"size:500" json:"cover_image"`

	// 平台提交回执（v3.94.0）：SubmitToPlatform 落库平台资产 ID 与审核状态，
	// 重复提交据此复用既有平台资产（幂等），审核进度可回显。
	PlatformAssetID     int64      `gorm:"default:0" json:"platform_asset_id"`
	PlatformStatus      string     `gorm:"size:32;default:''" json:"platform_status"`
	PlatformReviewNote  string     `gorm:"type:text" json:"platform_review_note"`
	PlatformSubmittedAt *time.Time `json:"platform_submitted_at"`

	CreatedAt  time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 表名
func (AssetBundle) TableName() string { return "asset_bundles" }

// AssetBundleVersionLog 资产包版本变更日志
type AssetBundleVersionLog struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID         int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	AssetID    string         `gorm:"size:64;index;not null" json:"asset_id"`
	FromVer    string         `gorm:"size:16" json:"from_ver"`
	ToVer      string         `gorm:"size:16;not null" json:"to_ver"`
	ChangeNote string         `gorm:"type:text" json:"change_note"`
	Operator   string         `gorm:"size:64" json:"operator"`
	CreatedAt  time.Time      `gorm:"autoCreateTime" json:"created_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 表名
func (AssetBundleVersionLog) TableName() string { return "asset_bundle_version_logs" }

// AssetBundleRating 资产包评分记录（一人一包一票，唯一索引防重复评分；
// (asset_id,user_id) 唯一索引由 tenancyidx 启动时规范化为含 tenant_id 前缀）
type AssetBundleRating struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID      int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	AssetID string `gorm:"size:64;uniqueIndex:idx_bundle_rating;not null" json:"asset_id"`
	UserID  uint   `gorm:"uniqueIndex:idx_bundle_rating;not null" json:"user_id"`
	Score   int    `gorm:"not null" json:"score"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 表名
func (AssetBundleRating) TableName() string { return "asset_bundle_ratings" }
