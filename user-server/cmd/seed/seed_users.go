// seed_users.go 模块 A：系统用户与坐席状态种子数据
//
// 演示账号清单（统一密码见 seedPassword，默认值是开源仓库里公开的 Seed@123456，
// 部署方可用 SEED_PASSWORD / ADMIN_PASSWORD 不改代码覆盖）：
// admin （超管，DataScope=all）
// cs01..cs03 （客服坐席，DataScope=self）
// staff01..staff05 （普通员工，DataScope=self）
//
// 每个客服坐席关联一条 AgentStatus，用于实时看板演示。
//
// 关键修复：Clean 阶段不再使用 "username IN (白名单)" 硬删，
// 改为按「real_name 包含 [seed-demo] 标签」或「phone 落在演示号段」精准清理。
// 原实现会把安装向导创建的真实超管（username 可能是 admin 或其他）一并删掉，
// 引发"seed 跑完真实超管登不上"的反复失效问题。
package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// seedPasswordDefault 是随开源仓库公开的演示口令，也是本常量的历史唯一取值。
// 它仍然作为未配置时的默认值保留（e2e/bootstrap 的既有流程依赖它），
// 但部署方现在可以不改代码就换掉写入的口令，见 seedPassword。
const seedPasswordDefault = "Seed@123456"

// seedPassword 本次 seed 实际写入 system_users 的统一密码。
// 取值优先级 SEED_PASSWORD > ADMIN_PASSWORD > seedPasswordDefault；
// 空白值等同未设置，避免误把空口令账号写进库里。
var seedPassword = resolveSeedPassword(os.Getenv)

func resolveSeedPassword(getenv func(string) string) string {
	for _, key := range []string{"SEED_PASSWORD", "ADMIN_PASSWORD"} {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
	}
	return seedPasswordDefault
}

// seedPasswordForLog 日志展示用：默认口令本就是公开信息，照常打印；
// 部署方自设的口令只打掩码，不把它写进终端/CI 日志。
func seedPasswordForLog() string {
	if seedPassword == seedPasswordDefault {
		return seedPassword + "（公开默认值，生产请用 SEED_PASSWORD 覆盖）"
	}
	return "********（已由环境变量覆盖，不回显）"
}

// seedPhonePrefix 演示号段前缀（11 位手机号段）
// 用于兼容「real_name 标签缺失」的旧 seed 数据清理
const seedPhonePrefix = "138000000"

type usersSeeder struct{}

func (s *usersSeeder) Name() string        { return "users" }
func (s *usersSeeder) Description() string { return "系统用户(9) + 坐席状态(3)" }

func (s *usersSeeder) Clean(database *gorm.DB) error {
	deletedAgents, err := cleanByCondition(database, &model.AgentStatus{}, "agent_name LIKE ?", "seed_%")
	if err != nil {
		return fmt.Errorf("清空 agent_statuses 失败: %w", err)
	}

	tx := database.Where(
		"real_name LIKE ? OR phone LIKE ? OR email LIKE ?",
		"%"+seedTag+"%",
		seedPhonePrefix+"%",
		"%@hivemtk.demo",
	).Delete(&model.SystemUser{})
	if tx.Error != nil {
		return fmt.Errorf("清空 system_users 失败: %w", tx.Error)
	}
	deletedUsers := tx.RowsAffected

	log.Printf("  · 清理 seed 数据: %d 条 agent_status, %d 条 system_user",
		deletedAgents, deletedUsers)
	return nil
}

func (s *usersSeeder) Seed(database *gorm.DB, ctx *SeedContext) error {
	type userSpec struct {
		Username  string
		RealName  string
		Email     string
		Phone     string
		Role      string
		DataScope string
	}

	specs := []userSpec{
		{Username: "admin", RealName: "系统超管", Email: "admin@hivemtk.demo", Phone: "13800000001",
			Role: model.SystemUserRoleAdmin, DataScope: model.DataScopeAll},
		{Username: "cs01", RealName: "李美琪", Email: "cs01@hivemtk.demo", Phone: "13800000011",
			Role: model.SystemRoleCodeCustomerService, DataScope: model.DataScopeSelf},
		{Username: "cs02", RealName: "王浩然", Email: "cs02@hivemtk.demo", Phone: "13800000012",
			Role: model.SystemRoleCodeCustomerService, DataScope: model.DataScopeSelf},
		{Username: "cs03", RealName: "张雨桐", Email: "cs03@hivemtk.demo", Phone: "13800000013",
			Role: model.SystemRoleCodeCustomerService, DataScope: model.DataScopeSelf},
		{Username: "staff01", RealName: "陈思远", Email: "staff01@hivemtk.demo", Phone: "13800000021",
			Role: model.SystemRoleCodeStaff, DataScope: model.DataScopeSelf},
		{Username: "staff02", RealName: "刘梦琪", Email: "staff02@hivemtk.demo", Phone: "13800000022",
			Role: model.SystemRoleCodeStaff, DataScope: model.DataScopeSelf},
		{Username: "staff03", RealName: "赵子轩", Email: "staff03@hivemtk.demo", Phone: "13800000023",
			Role: model.SystemRoleCodeStaff, DataScope: model.DataScopeSelf},
		{Username: "staff04", RealName: "孙嘉怡", Email: "staff04@hivemtk.demo", Phone: "13800000024",
			Role: model.SystemRoleCodeStaff, DataScope: model.DataScopeSelf},
		{Username: "staff05", RealName: "周晨曦", Email: "staff05@hivemtk.demo", Phone: "13800000025",
			Role: model.SystemRoleCodeStaff, DataScope: model.DataScopeSelf},
	}

	users := make([]model.SystemUser, 0, len(specs))
	for _, spec := range specs {
		lastLogin := hoursAgo(randInt(1, 72))
		u := model.SystemUser{
			Username:  spec.Username,
			Password:  seedPassword,
			Email:     spec.Email,
			Phone:     spec.Phone,
			RealName:  spec.RealName + " " + seedTag,
			Role:      spec.Role,
			Status:    1,
			Enabled:   true,
			LastLogin: &lastLogin,
			DataScope: spec.DataScope,
		}
		if err := database.Create(&u).Error; err != nil {
			return fmt.Errorf("创建用户 %s 失败: %w", spec.Username, err)
		}
		users = append(users, u)

		if spec.Role == model.SystemUserRoleAdmin {
			ctx.AdminUserID = u.ID
		} else if spec.Role == model.SystemRoleCodeCustomerService {
			ctx.CSUserIDs = append(ctx.CSUserIDs, u.ID)
		} else {
			ctx.StaffUserIDs = append(ctx.StaffUserIDs, u.ID)
		}
	}

	agentStatuses := make([]model.AgentStatus, 0, len(ctx.CSUserIDs))
	statuses := []string{"online", "busy", "offline"}
	for i, csID := range ctx.CSUserIDs {
		realName := specs[i+1].RealName
		st := statuses[i%len(statuses)]
		var onlineAt *time.Time
		if st != "offline" {
			t := hoursAgo(randInt(1, 8))
			onlineAt = &t
		}
		lastActive := hoursAgo(randInt(0, 2))
		as := model.AgentStatus{
			AgentID:         csID,
			AgentName:       "seed_" + realName,
			Status:          st,
			MaxSessions:     5,
			ActiveSessions:  randInt(0, 4),
			TodaySessions:   randInt(3, 18),
			TodayMessages:   randInt(20, 200),
			AvgResponseTime: randInt(5, 60),
			OnlineAt:        onlineAt,
			LastActiveAt:    &lastActive,
		}
		if err := database.Create(&as).Error; err != nil {
			return fmt.Errorf("创建 AgentStatus (agent_id=%d) 失败: %w", csID, err)
		}
		agentStatuses = append(agentStatuses, as)
		ctx.AgentStatusIDs = append(ctx.AgentStatusIDs, as.ID)
	}

	log.Printf("  ✓ 已写入 %d 个系统用户 + %d 条坐席状态", len(users), len(agentStatuses))
	log.Printf("  [SEED-PASSWORD] 演示账号：admin/%s，cs01/staff01 等同密码", seedPasswordForLog())
	return nil
}

// 编译期确保 time 包被引用（var onlineAt *time.Time 已在 Seed 中显式使用）
var _ = time.Now
