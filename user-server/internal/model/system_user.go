package model

import (
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// SystemUserRole 系统用户角色常量
//
// 私域独立部署下，SystemUser 的角色：
//   - admin     ：部署超管（平台管理员），由 system_init 初始化向导创建，每实例唯一；
//     跨团队全量直通（多租户 P0 需求 3）
//   - team_admin：团队管理员（多租户 P0 新增）——仅能管理本团队的用户与配置，
//     业务数据范围 = 本团队（DataScope=team）
//   - user      ：系统普通用户（默认角色）
//
// 注意：SystemUser.admin 与 TeamUser.admin 是不同实体的角色，权限层级不同。
// 存量库的 role CHECK（migrations/025）由 postMigrateRelaxSystemUserRoleCheck 放宽，
// 否则 team_admin 在带 CHECK 的库上插不进去。
const (
	SystemUserRoleAdmin     = "admin"
	SystemUserRoleTeamAdmin = "team_admin"
	SystemUserRoleUser      = "user"
)

// IsValidSystemUserRole 校验系统用户角色是否合法
func IsValidSystemUserRole(role string) bool {
	return role == SystemUserRoleAdmin ||
		role == SystemUserRoleTeamAdmin ||
		role == SystemUserRoleUser
}

// DataScope 数据范围枚举（行级权限）
//
// 控制用户能查询到的数据范围：
//   - all        : 全部数据（仅 admin）
//   - department : 本部门数据
//   - team       : 本团队数据
//   - self       : 仅自己创建的数据
const (
	DataScopeAll        = "all"
	DataScopeDepartment = "department"
	DataScopeTeam       = "team"
	DataScopeSelf       = "self"
)

// IsValidDataScope 校验数据范围是否合法
func IsValidDataScope(scope string) bool {
	return scope == DataScopeAll ||
		scope == DataScopeDepartment ||
		scope == DataScopeTeam ||
		scope == DataScopeSelf
}

// DefaultDataScopeForRole 根据角色返回默认数据范围
// admin → all（平台侧全量）；team_admin → team（本团队）；其他 → self
func DefaultDataScopeForRole(role string) string {
	switch role {
	case SystemUserRoleAdmin:
		return DataScopeAll
	case SystemUserRoleTeamAdmin:
		return DataScopeTeam
	default:
		return DataScopeSelf
	}
}

// SystemUser 系统用户模型
type SystemUser struct {
	ID           uint       `json:"id" gorm:"primaryKey"`
	Username     string     `json:"username" gorm:"size:50;uniqueIndex;not null"`
	Password     string     `json:"-" gorm:"size:100;not null"`
	Email        string     `json:"email" gorm:"size:100"`
	Phone        string     `json:"phone" gorm:"size:20;index"`
	RealName     string     `json:"real_name" gorm:"size:50"`
	Role         string     `json:"role" gorm:"size:20;default:'staff'"`
	Status       int        `json:"status" gorm:"default:1"`
	Enabled      bool       `json:"enabled" gorm:"column:enabled;default:true;not null"`
	LastLogin    *time.Time `json:"last_login"`
	DataScope    string     `json:"data_scope" gorm:"size:20;default:'self'"`
	DepartmentID uint       `json:"department_id" gorm:"index;default:0"`
	TeamID       uint       `json:"team_id" gorm:"index;default:0"`
	CreatedAt    time.Time  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt    time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName 返回表名
func (SystemUser) TableName() string {
	return "system_users"
}

// BeforeCreate GORM钩子，在创建前加密密码
func (u *SystemUser) BeforeCreate(tx *gorm.DB) error {
	if u.DataScope == "" {
		u.DataScope = DefaultDataScopeForRole(u.Role)
	}
	return HashSystemUserPassword(u)
}

// HashSystemUserPassword 加密密码（包级函数，避免 model 上挂业务方法）
func HashSystemUserPassword(u *SystemUser) error {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.Password = string(hashedPassword)
	return nil
}

// CheckSystemUserPassword 验证密码（包级函数）
func CheckSystemUserPassword(u *SystemUser, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password))
	return err == nil
}

// IsSystemUserAdmin 检查是否为管理员（：使用角色常量而非硬编码）
func IsSystemUserAdmin(u *SystemUser) bool {
	return u.Role == SystemUserRoleAdmin
}
