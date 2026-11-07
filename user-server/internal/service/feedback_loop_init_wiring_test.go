// feedback_loop_init_wiring_test.go 反馈链启动装配与开户赠送句柄下沉回归
// （AV-04c 批次6 刀3）。
//
// InitFeedbackCollector / InitFeedbackLoopComponents / tenant 开户赠送三处装配
// 收口为包级 Default 构造（句柄解析下沉仓储层）后：有库时仓储绑到测试库、组件
// 全部就绪；Default 构造与原 `NewXxxRepositoryWithDB(repository.GetDB())` 同连接
// 同解析时刻（repository/starter_kit_default_test.go、feedback_loop 系已有仓储侧
// 契约，这里锁 service 装配侧）。
package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// 开户赠送：Default 构造出的仓储与全局句柄同源（tenant.go 开户路径的装配等价性）。
func TestStarterKitRepositoryDefault_BindsGlobalHandle(t *testing.T) {
	_ = testutil.NewTestDB(t, &model.SystemConfig{})
	repo := repository.NewStarterKitRepositoryDefault()
	if repo == nil {
		t.Fatal("有库时 Default 构造不应为 nil")
	}
	// 用该仓储在测试库落一行站点配置并复查——连接即测试库全局句柄。
	//（system_config 每租户一行：uni_system_config_tenant。）
	cfg := &model.SystemConfig{Name: "starter_kit_wiring_probe"}
	if err := repo.CreateSiteConfig(context.Background(), cfg); err != nil {
		t.Fatalf("CreateSiteConfig: %v", err)
	}
	n, err := repo.CountSiteConfig(context.Background(), cfg.TenantID)
	if err != nil || n != 1 {
		t.Fatalf("CountSiteConfig 应在同一连接上读到 1 行: n=%d err=%v", n, err)
	}
}

// 反馈采集器：有库装配返回非 nil，且包级单例语义保持（重复 Init 幂等）。
func TestInitFeedbackCollector_WithDB(t *testing.T) {
	_ = testutil.NewTestDB(t)
	c1 := InitFeedbackCollector()
	if c1 == nil {
		t.Fatal("InitFeedbackCollector 应返回采集器")
	}
	if c2 := InitFeedbackCollector(); c1 != c2 {
		t.Fatal("sync.Once 单例语义破坏")
	}
	if GetFeedbackCollector() != c1 {
		t.Fatal("GetFeedbackCollector 应返回同一实例")
	}
}

// 反馈闭环五组件：有库装配全部就绪（共享同一仓储连接，与原取句柄传构造一致）。
func TestInitFeedbackLoopComponents_WithDB(t *testing.T) {
	_ = testutil.NewTestDB(t)
	components := InitFeedbackLoopComponents(nil, nil)
	if components == nil {
		t.Fatal("组件集合不应为 nil")
	}
	if components.Collector == nil || components.Analyzer == nil || components.Iterator == nil ||
		components.Bandit == nil || components.Optimizer == nil {
		t.Fatal("五组件应全部装配")
	}
}
