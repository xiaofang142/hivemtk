package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/testutil"
)

// K-4 API 台账轮（2026-11-13）：POST /api/geo/site/export 无启用站点时历史行为
// 500「geo_site 未配置」（ INTERNAL_ERROR_6002）——配置态缺失被伪装成服务端故障。
// 修复对齐 TriggerDeploy 同根口径：gorm.ErrRecordNotFound → ErrSiteConfigInvalid
// （controller 映射 400 +「尚未启用静态站」操作引导）。
//
// 验收判据：
//  ① 无 active 站点时 ExportToHugo 返回 ErrSiteConfigInvalid（errors.Is 命中）；
//  ② 文案含操作引导「尚未启用静态站」；
//  ③ DB 层真故障（非 ErrRecordNotFound）不误伤为配置态——仍走原 500 路径
//     （此处以被驱逐的 ctx 触发连接级错误近似不可行，DB 故障分支保持原样不动，
//     由 errors.Is 的精确判定保证不会把任何错误都吞成 400）。
func TestExportToHugo_NoActiveSite_IsConfigInvalid(t *testing.T) {
	if testing.Short() {
		t.Skip("需要测试库")
	}
	db := testutil.NewTestDB(t,
		&model.GeoSite{},
	)
	svc := NewSiteDeployService(
		repository.NewGeoSiteRepositoryWithDB(db),
		repository.NewGeoArticleRepositoryWithDB(db),
		nil,
	)
	_, err := svc.ExportToHugo(context.Background())
	if err == nil {
		t.Fatal("无启用站点时 ExportToHugo 应报错")
	}
	if !errors.Is(err, ErrSiteConfigInvalid) {
		t.Fatalf("应归一为 ErrSiteConfigInvalid（controller 据此回 400），got: %v", err)
	}
	if !strings.Contains(err.Error(), "尚未启用静态站") {
		t.Errorf("文案应含操作引导「尚未启用静态站」: %v", err)
	}
}
