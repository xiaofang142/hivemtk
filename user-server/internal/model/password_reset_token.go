package model

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PasswordResetToken 密码重置令牌
type PasswordResetToken struct {
	ID     string `gorm:"type:varchar(36);primaryKey" json:"id"`
	UserID string `gorm:"type:varchar(36);index;not null" json:"user_id"`
	// BeforeCreate 生成 uuid+uuid 共 72 字符；原 varchar(64) 装不下，
	// 任何重置令牌 INSERT 必报 22001（value too long），forgot-password
	// 整链路损坏。拓宽为 varchar(128)（PG 元数据级变更，AutoMigrate 收口）。
	// 现在这列只存明文的 SHA-256（64 字符）；列宽不回缩，避免在这张有活行的表上
	// 做一次原地收窄，也让作废钩子跑之前进来的旧行不会因列宽被拒。
	Token string `gorm:"type:varchar(128);uniqueIndex;not null" json:"token"`
	// RawToken 是发进邮件里的那串明文，只在签发那一刻存在于进程内，不落库、不出 JSON。
	RawToken  string         `gorm:"-" json:"-"`
	ExpiresAt time.Time      `gorm:"index;not null" json:"expires_at"`
	UsedAt    *time.Time     `json:"used_at"`
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定表名
func (PasswordResetToken) TableName() string {
	return "password_reset_tokens"
}

// HashPasswordResetToken 把邮件里的明文映射成库里那列。
// 校验侧必须先过这一层再查，两边同一函数，不留"忘了哈希"的第二条路。
func HashPasswordResetToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// BeforeCreate 自动填充 ID 和 Token。
//
// Token 一律由 RawToken 哈希得来（含调用方预置的 Token，一并覆盖）：
// 留一条"传了原文就原样存"的逃生口，等于哈希化只是写法。
func (t *PasswordResetToken) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	if t.RawToken == "" {
		t.RawToken = uuid.New().String() + uuid.New().String()
	}
	t.Token = HashPasswordResetToken(t.RawToken)
	return nil
}
