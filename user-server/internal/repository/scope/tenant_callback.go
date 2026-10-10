// tenant_callback.go 租户回调（多租户 P0，2026-10-11）。
//
// 放在 scope 包而不是 db 包：InitDB（生产）与 testutil.NewTestDB（测试库）都要挂
// 同一回调，而 testutil 不能 import db（model 包的测试经 testutil→db→model 成环）；
// scope 无内部依赖，三方都引得到。
//
// 为什么做成 GORM 回调而不是每个 repo 自己填：业务行的写入路径很多
// （管理端 CRUD、客户导入、bridge/ingest 管线、MCP 工具调用），逐路径填
// team_id 迟早漏一处——漏的那处就是越权口。统一挂在 Create 回调上，
// "模型带 TeamID 字段 + ctx 带团队身份 + 值还是 0" 三者齐备才回填，
// 平台侧写入与显式指定团队的调用方都不受影响。
//
// 开关与语义见 internal/repository/scope/team.go（TENANT_ISOLATION=off 全旁路）。
package scope

import (
	"context"
	"reflect"
	"sync"

	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// RegisterTenantTeamCallback 在连接初始化时挂一次（InitDB / 测试库装配共用）。
func RegisterTenantTeamCallback(g *gorm.DB) {
	if g == nil {
		return
	}
	_ = g.Callback().Create().Before("gorm:create").Register("tenant:fill_team_id", func(tx *gorm.DB) {
		if tx == nil || tx.Statement == nil || tx.Statement.Schema == nil {
			return
		}
		field := tx.Statement.Schema.LookUpField("TeamID")
		if field == nil {
			return // 非 Teamable 模型：与租户无关，直接放行
		}
		rv := tx.Statement.ReflectValue
		switch rv.Kind() {
		case reflect.Struct:
			fillTeamField(tx.Statement.Context, field, rv)
		case reflect.Slice, reflect.Array:
			for i := 0; i < rv.Len(); i++ {
				fillTeamField(tx.Statement.Context, field, rv.Index(i))
			}
		}
	})
}

func fillTeamField(ctx context.Context, field *schema.Field, rv reflect.Value) {
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return
		}
		rv = rv.Elem()
	}
	_ = FillTeamIDOnCreate(ctx, func() uint { return defaultTeamIDCached(ctx) }, func(teamID uint) error {
		if cur, zero := field.ValueOf(ctx, rv); !zero {
			if id, ok := cur.(uint); ok && id != 0 {
				return nil // 调用方显式指定过团队，不覆盖
			}
		}
		return field.Set(ctx, rv, teamID)
	})
}

// DefaultTeamName 默认团队名（与 model.DefaultTeamName 必须同值——
// scope 不能 import model（testutil→scope→model 成环），两处常量由
// repository.tenant_isolation_test 钉死同步）。
const DefaultTeamName = "默认团队"

// defaultTeamIDCached 解析「默认团队」id（进程内缓存一次；回填钩子保证其存在，
// 缓存未命中（极端：隔离开启但回填被跳过）时按 0 处理=平台侧，fail-open 一次写入
// 换启动健壮性——回填钩子的日志会把这种漂移喊出来）。
// 表名/列名直查（不经 model）：scope 是最底层包，不能反向依赖 model。
var (
	defaultTeamIDOnce sync.Once
	defaultTeamIDVal  uint
)

var defaultTeamIDDB func(ctx context.Context) *gorm.DB // 由挂回调方注入（db.GetDB）

// SetDefaultTeamIDDB 注入默认团队查询用的 DB 句柄（InitDB / testutil 装配时调用）。
func SetDefaultTeamIDDB(fn func(ctx context.Context) *gorm.DB) {
	defaultTeamIDDB = fn
}

func defaultTeamIDCached(ctx context.Context) uint {
	defaultTeamIDOnce.Do(func() {
		if defaultTeamIDDB == nil {
			logger.Warn("[tenant] 默认团队查询句柄未注入，系统管线新建行将落 team_id=0")
			return
		}
		g := defaultTeamIDDB(ctx)
		if g == nil {
			return
		}
		var id uint
		if err := g.WithContext(ctx).Table("teams").
			Where("name = ?", DefaultTeamName).Select("id").Scan(&id).Error; err != nil || id == 0 {
			logger.Warnf("[tenant] 默认团队未找到（回填钩子未跑？），系统管线新建行将落 team_id=0: %v", err)
			return
		}
		defaultTeamIDVal = id
	})
	return defaultTeamIDVal
}
