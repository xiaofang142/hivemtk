package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/repository"
)

// 桥接扩展分发通道的测试。
//
// 关键口径：
//   - 平台关闭（PLATFORM_ENABLED=false）⇒ ErrBridgeExtensionOffline，不是 500
//   - 平台上没有已上架的扩展资产 ⇒ 同样是 Offline（"现在拿不到"）
//   - 拿到的 file_url 是相对路径 ⇒ 必须补成绝对地址（用户端浏览器要能直接下）
//   - latest_version 在平台详情里可能是字符串、也可能是版本行对象 ⇒ 两种都要认

// enablePlatformForTest 打开平台开关（服务按 env 判定平台是否接入）。
func enablePlatformForTest(t *testing.T) {
	t.Helper()
	t.Setenv("PLATFORM_ENABLED", "true")
}

// stubPlatformClient 桩实现
type stubPlatformClient struct {
	list   []map[string]any
	detail map[string]any
	err    error
}

func (s *stubPlatformClient) ListAssets(_ context.Context, _, _ string, _, _ int) ([]map[string]any, int64, error) {
	if s.err != nil {
		return nil, 0, s.err
	}
	return s.list, int64(len(s.list)), nil
}

func (s *stubPlatformClient) GetAssetDetail(_ context.Context, _ string) (map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.detail, nil
}

func (s *stubPlatformClient) Purchase(_ context.Context, _ string) error { return s.err }
func (s *stubPlatformClient) PullData(_ context.Context, _ string) (*repository.PlatformAssetPayload, error) {
	return nil, s.err
}
func (s *stubPlatformClient) MyPurchases(_ context.Context) ([]map[string]any, error) {
	return nil, s.err
}
func (s *stubPlatformClient) ReportUsage(_ context.Context, _ string, _ int64) error { return s.err }

var _ repository.PlatformAPIClient = (*stubPlatformClient)(nil)

func TestBridgeExtensionService_PlatformDisabledIsOffline(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "false")
	svc := NewBridgeExtensionService(&stubPlatformClient{
		list: []map[string]any{{"asset_id": "ast-general-abc"}},
	})
	_, err := svc.Latest(context.Background())
	if !errors.Is(err, ErrBridgeExtensionOffline) {
		t.Errorf("平台关闭时应回 ErrBridgeExtensionOffline，实际 %v", err)
	}
}

func TestBridgeExtensionService_EmptyMarketIsOffline(t *testing.T) {
	enablePlatformForTest(t)
	svc := NewBridgeExtensionService(&stubPlatformClient{list: []map[string]any{}})
	_, err := svc.Latest(context.Background())
	if !errors.Is(err, ErrBridgeExtensionOffline) {
		t.Errorf("市场上没有扩展包时应回 ErrBridgeExtensionOffline，实际 %v", err)
	}
}

func TestBridgeExtensionService_NilClientIsOffline(t *testing.T) {
	enablePlatformForTest(t)
	svc := NewBridgeExtensionService(nil)
	_, err := svc.Latest(context.Background())
	if !errors.Is(err, ErrBridgeExtensionOffline) {
		t.Errorf("平台客户端为 nil 时应回 ErrBridgeExtensionOffline，实际 %v", err)
	}
}

// TestBridgeExtensionService_VersionInfoForm 平台 T1 修复后的详情形状：
// latest_version 保持字符串语义，版本行对象改放 version_info（跨仓契约，
// platform 仓 asset_market_service.go GetMarketAssetDetail）。
// 回归背景：2026-11-04 线上实测——本服务只认旧的 latest_version=对象形状，
// file_url 恒空 ⇒ 线上一律报「未上架」，v1.0.2 时代下载口即已断。
func TestBridgeExtensionService_VersionInfoForm(t *testing.T) {
	enablePlatformForTest(t)
	client := &stubPlatformClient{
		list: []map[string]any{{"asset_id": "ast-general-abc"}},
		detail: map[string]any{
			"name":           "HiveBridge 蜂桥",
			"latest_version": "1.0.3",
			"version_info": map[string]any{
				"version":     "1.0.3",
				"file_url":    "/files/extensions/2026/11/pkg.zip",
				"filename":    "hivebridge-1.0.3.zip",
				"size_bytes":  float64(227721),
				"file_sha256": "0c74dc8d",
			},
		},
	}
	svc := NewBridgeExtensionService(client)
	pkg, err := svc.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest 失败: %v", err)
	}
	if pkg.Version != "1.0.3" {
		t.Errorf("版本号应为 1.0.3，实际 %s", pkg.Version)
	}
	if pkg.FileURL != "/files/extensions/2026/11/pkg.zip" {
		t.Errorf("file_url 应从 version_info 取到，实际 %s", pkg.FileURL)
	}
	if pkg.Filename != "hivebridge-1.0.3.zip" {
		t.Errorf("文件名应取到，实际 %s", pkg.Filename)
	}
	if pkg.SizeBytes != 227721 {
		t.Errorf("字节数应为 227721，实际 %d", pkg.SizeBytes)
	}
	if pkg.FileSHA256 != "0c74dc8d" {
		t.Errorf("sha256 应取到，实际 %s", pkg.FileSHA256)
	}
}

// TestBridgeExtensionService_VersionInfoWinsWhenBothPresent 两键并存（未来
// 平台若把两处都塞对象）时 version_info 优先，不允许读到旧键的陈旧值
func TestBridgeExtensionService_VersionInfoWinsWhenBothPresent(t *testing.T) {
	enablePlatformForTest(t)
	client := &stubPlatformClient{
		list: []map[string]any{{"asset_id": "ast-general-abc"}},
		detail: map[string]any{
			"name": "HiveBridge 蜂桥",
			"latest_version": map[string]any{
				"version":  "0.9.0-old",
				"file_url": "/files/extensions/old.zip",
			},
			"version_info": map[string]any{
				"version":  "1.0.3",
				"file_url": "/files/extensions/2026/11/pkg.zip",
			},
		},
	}
	svc := NewBridgeExtensionService(client)
	pkg, err := svc.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest 失败: %v", err)
	}
	if pkg.Version != "1.0.3" || pkg.FileURL != "/files/extensions/2026/11/pkg.zip" {
		t.Errorf("version_info 应优先于 latest_version，实际 version=%s file_url=%s", pkg.Version, pkg.FileURL)
	}
}

// TestBridgeExtensionService_LatestVersionObjectForm 平台详情里 latest_version 是版本行对象（旧平台形状，回退兼容）
func TestBridgeExtensionService_LatestVersionObjectForm(t *testing.T) {
	enablePlatformForTest(t)
	client := &stubPlatformClient{
		list: []map[string]any{{"asset_id": "ast-general-abc"}},
		detail: map[string]any{
			"name": "HiveBridge 蜂桥",
			"latest_version": map[string]any{
				"version":     "1.2.0",
				"file_url":    "/files/extensions/2026/10/pkg.zip",
				"filename":    "hivebridge-1.2.0.zip",
				"size_bytes":  float64(21504),
				"file_sha256": "abc123",
			},
		},
	}
	svc := NewBridgeExtensionService(client)
	pkg, err := svc.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest 失败: %v", err)
	}
	if pkg.Version != "1.2.0" {
		t.Errorf("版本号应为 1.2.0，实际 %s", pkg.Version)
	}
	if pkg.FileURL != "/files/extensions/2026/10/pkg.zip" {
		t.Errorf("file_url 应保留平台返回值，实际 %s", pkg.FileURL)
	}
	if pkg.Filename != "hivebridge-1.2.0.zip" {
		t.Errorf("文件名应取到，实际 %s", pkg.Filename)
	}
	if pkg.SizeBytes != 21504 {
		t.Errorf("字节数应为 21504，实际 %d", pkg.SizeBytes)
	}
	if pkg.FileSHA256 != "abc123" {
		t.Errorf("sha256 应取到，实际 %s", pkg.FileSHA256)
	}
}

// TestBridgeExtensionService_VersionStringForm latest_version 是纯字符串时，
// 没有文件地址 ⇒ 视为"平台上没有可下载的包"
func TestBridgeExtensionService_VersionStringForm(t *testing.T) {
	enablePlatformForTest(t)
	client := &stubPlatformClient{
		list: []map[string]any{{"asset_id": "ast-general-abc"}},
		detail: map[string]any{
			"name":           "HiveBridge 蜂桥",
			"latest_version": "1.2.0",
		},
	}
	svc := NewBridgeExtensionService(client)
	_, err := svc.Latest(context.Background())
	if !errors.Is(err, ErrBridgeExtensionOffline) {
		t.Errorf("没有 file_url 时应回 ErrBridgeExtensionOffline，实际 %v", err)
	}
}

// TestBridgeExtensionService_PlatformErrorPropagates 平台侧调用失败不是"没有这个东西"
func TestBridgeExtensionService_PlatformErrorPropagates(t *testing.T) {
	enablePlatformForTest(t)
	svc := NewBridgeExtensionService(&stubPlatformClient{err: errors.New("platform 500")})
	_, err := svc.Latest(context.Background())
	if err == nil {
		t.Fatal("平台侧失败时应返回错误")
	}
	if errors.Is(err, ErrBridgeExtensionOffline) {
		t.Error("平台侧失败不应被当成 Offline（那会让前端提示「暂未上架」，掩盖真故障）")
	}
}

// TestAbsoluteDownloadURL 相对地址补绝对、绝对地址原样返回
func TestAbsoluteDownloadURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://cdn.example.com/files/x.zip", "https://cdn.example.com/files/x.zip"},
		{"http://cdn.example.com/files/x.zip", "http://cdn.example.com/files/x.zip"},
		{"", ""},
	}
	for _, c := range cases {
		if got := absoluteDownloadURL(c.in); got != c.want {
			t.Errorf("absoluteDownloadURL(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}
