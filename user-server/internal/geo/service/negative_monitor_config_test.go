package service

// negative_monitor_job_missing_config_test.go — 未配置租户的 negative_monitor 不得报错。
//
// 2026-11-10 实证：本地 `geo_config` 无行（新租户的合法初始状态）时，任务每 30 分钟
// 往 `geo_job_runs` 落一条 status=failed，错误是 "读取 GeoConfig 失败: record not found"。
// 这有两个后果：
//  1. 未配置品牌是本该"跳过"的正常状态（ConfigService.GetConfig 对同一情形返回默认
//     配置），报 failed 会把真实的执行故障淹没在噪声里；
//  2. 运维看到 failed 会去追一个并不存在的故障（本次就是如此）。
//
// 口径与 cfgRepo.Update 的「不存在即创建」、ConfigService.GetConfig 的「不存在即默认」
// 一致：无行 = 空配置 ⇒ 无品牌名 ⇒ "跳过本轮"（成功返回）。
//
// 分档边界（本文件第二个用例）同样重要：只有 gorm.ErrRecordNotFound 算「未配置」，
// 其余错误必须继续上报 —— 把表缺失/连接断开吞成"跳过"比噪声更坏。

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func TestNegativeMonitorJob_MissingConfigSkipsInsteadOfFailing(t *testing.T) {
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.ResetRegistryForTest(); tenant.SetMode(tenant.ModeOff) })

	database := testutil.NewTestDB(t, &model.GeoConfig{}, &model.GeoProbeRun{}, &model.GeoAlert{})
	if database == nil {
		t.Fatal("测试库不可达：本任务判不了")
	}
	db.SetTestDB(database)

	// 前置断言：确实没有 geo_config 行（否则这个用例测的不是它宣称的东西）。
	var rows int64
	if err := database.Model(&model.GeoConfig{}).Count(&rows).Error; err != nil {
		t.Fatalf("统计 geo_config 失败: %v", err)
	}
	if rows != 0 {
		t.Fatalf("前置条件不成立：geo_config 应无行，实际 %d 行（共享测试库被别的用例污染）", rows)
	}

	ctx := tenant.WithTenantID(context.Background(), tenant.DefaultTenantID)
	summary, err := negativeMonitorJob(ctx)
	if err != nil {
		t.Fatalf("无 geo_config 行时不得报错（那是未配置的合法初始状态），实际: %v", err)
	}
	if summary == "" {
		t.Fatal("应给出可读的跳过原因（summary 为空等于什么都没说）")
	}
	t.Logf("summary=%s", summary)
}

// fakeGeoConfigRepo 只回放指定错误，用于钉住错误分档（不连库）。
type fakeGeoConfigRepo struct {
	cfg *model.GeoConfig
	err error
}

func (f *fakeGeoConfigRepo) Get(context.Context) (*model.GeoConfig, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.cfg, nil
}

func (f *fakeGeoConfigRepo) Update(context.Context, *model.GeoConfig) error { return nil }

func (f *fakeGeoConfigRepo) GetBrandProfile(context.Context) (*repository.GeoBrandProfile, error) {
	return &repository.GeoBrandProfile{}, nil
}

// TestLoadGeoConfigForJob_ErrorClassification 钉住分档边界：
//   - ErrRecordNotFound ⇒ 空配置 + nil error（"未配置"不是故障）；
//   - 其他任何错误 ⇒ 原样上报（包上可读前缀），绝不被吞成"跳过"。
func TestLoadGeoConfigForJob_ErrorClassification(t *testing.T) {
	ctx := context.Background()

	t.Run("record_not_found_is_empty_config", func(t *testing.T) {
		cfg, err := loadGeoConfigForJob(ctx, &fakeGeoConfigRepo{err: gorm.ErrRecordNotFound})
		if err != nil {
			t.Fatalf("无行应视为空配置，实际报错: %v", err)
		}
		if cfg == nil {
			t.Fatal("应返回非 nil 的默认配置")
		}
		if cfg.BrandName != "" {
			t.Fatalf("默认配置的品牌名应为空（任务据此跳过），实际 %q", cfg.BrandName)
		}
	})

	t.Run("real_failure_still_reported", func(t *testing.T) {
		realErr := errors.New(`ERROR: relation "geo_config" does not exist (SQLSTATE 42P01)`)
		cfg, err := loadGeoConfigForJob(ctx, &fakeGeoConfigRepo{err: realErr})
		if err == nil {
			t.Fatal("表缺失/连接故障必须上报，不能被当成「未配置」静默跳过")
		}
		if cfg != nil {
			t.Fatal("报错时不应返回配置对象")
		}
		if !errors.Is(err, realErr) {
			t.Fatalf("应保留原始错误链（便于排障），实际: %v", err)
		}
	})

	t.Run("normal_row_passthrough", func(t *testing.T) {
		want := &model.GeoConfig{BrandName: "HiveMTK", Language: "zh"}
		cfg, err := loadGeoConfigForJob(ctx, &fakeGeoConfigRepo{cfg: want})
		if err != nil {
			t.Fatalf("正常路径不应报错: %v", err)
		}
		if cfg != want {
			t.Fatal("应原样返回仓储给出的配置")
		}
	})
}
