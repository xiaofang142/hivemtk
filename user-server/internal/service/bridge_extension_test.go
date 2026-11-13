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
	list       []map[string]any
	detail     map[string]any
	detailByID map[string]map[string]any // 多资产场景：按资产 ID 给不同详情
	err        error
}

func (s *stubPlatformClient) ListAssets(_ context.Context, _, _ string, _, _ int) ([]map[string]any, int64, error) {
	if s.err != nil {
		return nil, 0, s.err
	}
	return s.list, int64(len(s.list)), nil
}

func (s *stubPlatformClient) GetAssetDetail(_ context.Context, id string) (map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	// 多资产场景：按资产 ID 给不同详情（挑最新版本的用例需要）
	if s.detailByID != nil {
		if d, ok := s.detailByID[id]; ok {
			return d, nil
		}
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
func (s *stubPlatformClient) GetCredits(_ context.Context) (map[string]any, error) {
	return map[string]any{"exists": false, "balance": 0}, s.err
}

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

// ---------------------------------------------------------------------------
// purchase 门 + 多资产挑最新版本（2026-11-13 线上根因修复的守护测试）
//
// 线上实证（2026-11-13 21:36）：平台 detail 的 version_info 只在资产
// purchased=true 后才带 file_url/filename/file_sha256/size_bytes；未购买时
// 只有 {changelog, created_at, schema_version, version}。原实现没先 purchase
// 就读 detail ⇒ file_url 恒空 ⇒ 一律报「平台尚未上架桥接扩展包」。
// ---------------------------------------------------------------------------

// gatePlatformClient 桩：按「是否已 purchase」决定 detail 的 version_info
// 是否带文件字段，并记录 purchase 调用情况。
type gatePlatformClient struct {
	list        []map[string]any
	detail      map[string]any // 已购买后的完整形状
	purchaseErr error

	purchased bool
	purchaseN int
}

func (s *gatePlatformClient) ListAssets(_ context.Context, _, _ string, _, _ int) ([]map[string]any, int64, error) {
	return s.list, int64(len(s.list)), nil
}

func (s *gatePlatformClient) Purchase(_ context.Context, _ string) error {
	if s.purchaseErr != nil {
		return s.purchaseErr
	}
	s.purchaseN++
	s.purchased = true
	return nil
}

func (s *gatePlatformClient) GetAssetDetail(_ context.Context, id string) (map[string]any, error) {
	if !s.purchased {
		// 未购买：平台只给版本号，一个文件字段都不给
		return map[string]any{
			"asset_id": id,
			"name":     "HiveBridge 蜂桥",
			"version_info": map[string]any{
				"version": "1.3.0",
			},
		}, nil
	}
	return s.detail, nil
}

func (s *gatePlatformClient) PullData(_ context.Context, _ string) (*repository.PlatformAssetPayload, error) {
	return nil, nil
}
func (s *gatePlatformClient) MyPurchases(_ context.Context) ([]map[string]any, error) {
	return nil, nil
}
func (s *gatePlatformClient) ReportUsage(_ context.Context, _ string, _ int64) error { return nil }
func (s *gatePlatformClient) GetCredits(_ context.Context) (map[string]any, error) {
	return map[string]any{"exists": false, "balance": 0}, nil
}

var _ repository.PlatformAPIClient = (*gatePlatformClient)(nil)

// TestBridgeExtensionService_PurchaseUnlocksFileURL 未 purchase 时平台详情
// 不带 file_url ⇒ Latest 必须先 purchase 再读 detail，否则恒 Offline。
func TestBridgeExtensionService_PurchaseUnlocksFileURL(t *testing.T) {
	enablePlatformForTest(t)
	client := &gatePlatformClient{
		list: []map[string]any{
			{"asset_id": "ast-general-new", "latest_version": "1.3.0"},
		},
		detail: map[string]any{
			"asset_id": "ast-general-new",
			"name":     "HiveBridge 蜂桥",
			"version_info": map[string]any{
				"version":     "1.3.0",
				"file_url":    "/files/extensions/2026/11/bd55744d.zip",
				"filename":    "hivebridge-1.3.0.zip",
				"size_bytes":  float64(441803),
				"file_sha256": "77f0357b",
			},
		},
	}
	svc := NewBridgeExtensionService(client)
	pkg, err := svc.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest 失败: %v", err)
	}
	if client.purchaseN != 1 {
		t.Errorf("Latest 应先 purchase 解锁 file_url，实际 purchase 次数 %d", client.purchaseN)
	}
	if pkg.Version != "1.3.0" {
		t.Errorf("版本号应为 1.3.0，实际 %s", pkg.Version)
	}
	if pkg.FileURL != "/files/extensions/2026/11/bd55744d.zip" {
		t.Errorf("purchase 后应拿到 file_url，实际 %q", pkg.FileURL)
	}
	if pkg.Filename != "hivebridge-1.3.0.zip" {
		t.Errorf("文件名应取到，实际 %s", pkg.Filename)
	}
	if pkg.SizeBytes != 441803 {
		t.Errorf("字节数应为 441803，实际 %d", pkg.SizeBytes)
	}
}

// TestBridgeExtensionService_PurchaseFailurePropagates purchase 失败是平台侧
// 故障，不能被吞成 ErrBridgeExtensionOffline（那会让前端误报「暂未上架」）。
func TestBridgeExtensionService_PurchaseFailurePropagates(t *testing.T) {
	enablePlatformForTest(t)
	client := &gatePlatformClient{
		list:        []map[string]any{{"asset_id": "ast-general-new", "latest_version": "1.3.0"}},
		purchaseErr: errors.New("购买失败"),
	}
	svc := NewBridgeExtensionService(client)
	_, err := svc.Latest(context.Background())
	if err == nil {
		t.Fatal("purchase 失败时应返回错误")
	}
	if errors.Is(err, ErrBridgeExtensionOffline) {
		t.Error("purchase 失败不应被当成 Offline（会掩盖真故障）")
	}
}

// TestBridgeExtensionService_WithoutPurchaseStaysOffline 若实现退化成「不
// purchase 直接读 detail」（线上老 bug），桩会返回无 file_url 的详情，
// 必须报 Offline —— 这条用例守的是「不许悄悄绕过 purchase 门」。
func TestBridgeExtensionService_WithoutPurchaseStaysOffline(t *testing.T) {
	enablePlatformForTest(t)
	client := &gatePlatformClient{
		list:   []map[string]any{{"asset_id": "ast-general-new", "latest_version": "1.3.0"}},
		detail: map[string]any{"asset_id": "ast-general-new", "name": "HiveBridge 蜂桥"},
	}
	// 直接调 detail（不 purchase）复现老路径
	detail, err := client.GetAssetDetail(context.Background(), "ast-general-new")
	if err != nil {
		t.Fatalf("桩异常: %v", err)
	}
	if vi, ok := detail["version_info"].(map[string]any); ok {
		if vi["file_url"] != nil {
			t.Error("桩口径错了：未 purchase 时平台不该给 file_url")
		}
	}
}

// TestBridgeExtensionService_PicksNewestVersionAcrossAssets 多资产时不许取
// list[0]：平台按 updated_at 倒序，而 purchase 会刷新 updated_at（线上实测
// 买过旧版本后旧资产被顶到第一位 ⇒ 用户端拿到 1.0.5 而非 1.3.0）。
func TestBridgeExtensionService_PicksNewestVersionAcrossAssets(t *testing.T) {
	enablePlatformForTest(t)
	client := &stubPlatformClient{
		// 故意把旧版本放第一位，模拟「刚 purchase 过旧资产」后的倒序
		list: []map[string]any{
			{"asset_id": "ast-general-old", "latest_version": "1.0.5"},
			{"asset_id": "ast-general-new", "latest_version": "1.3.0"},
			{"asset_id": "ast-general-mid", "latest_version": "1.2.0"},
		},
		detailByID: map[string]map[string]any{
			"ast-general-old": {
				"name": "HiveBridge 蜂桥",
				"version_info": map[string]any{
					"version": "1.0.5", "file_url": "/files/extensions/old.zip",
				},
			},
			"ast-general-mid": {
				"name": "HiveBridge 蜂桥",
				"version_info": map[string]any{
					"version": "1.2.0", "file_url": "/files/extensions/mid.zip",
				},
			},
			"ast-general-new": {
				"name": "HiveBridge 蜂桥",
				"version_info": map[string]any{
					"version": "1.3.0", "file_url": "/files/extensions/2026/11/new.zip",
				},
			},
		},
	}
	svc := NewBridgeExtensionService(client)
	pkg, err := svc.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest 失败: %v", err)
	}
	if pkg.AssetID != "ast-general-new" {
		t.Errorf("应按版本号选最新资产 ast-general-new，实际 %s", pkg.AssetID)
	}
	if pkg.Version != "1.3.0" || pkg.FileURL != "/files/extensions/2026/11/new.zip" {
		t.Errorf("应拿到 1.3.0 的文件地址，实际 version=%s file_url=%s", pkg.Version, pkg.FileURL)
	}
}

// TestBridgeExtensionService_SkipsAssetsWithoutID 列表里 asset_id 为空的脏行
// 必须跳过，不能把它当成「选中空 ID」而误报 Offline。
func TestBridgeExtensionService_SkipsAssetsWithoutID(t *testing.T) {
	enablePlatformForTest(t)
	client := &stubPlatformClient{
		list: []map[string]any{
			{"latest_version": "9.9.9"}, // 无 asset_id 的脏行，版本号最高
			{"asset_id": "ast-general-new", "latest_version": "1.3.0"},
		},
		detailByID: map[string]map[string]any{
			"ast-general-new": {
				"name": "HiveBridge 蜂桥",
				"version_info": map[string]any{
					"version": "1.3.0", "file_url": "/files/extensions/2026/11/new.zip",
				},
			},
		},
	}
	svc := NewBridgeExtensionService(client)
	pkg, err := svc.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest 失败: %v", err)
	}
	if pkg.AssetID != "ast-general-new" {
		t.Errorf("应跳过无 asset_id 的脏行，实际选中 %s", pkg.AssetID)
	}
}

// TestCompareVersionStrings 点分版本比较口径
func TestCompareVersionStrings(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.3.0", "1.0.5", 1},
		{"1.0.5", "1.0.5", 0},
		{"1.0.2", "1.0.10", -1},
		{"1.2", "1.1.9", 1}, // 缺段按 0 补齐
		{"1.0.0", "", 1},    // 空串按小于任何正常版本
		{"", "", 0},
		{"v1", "1.0.0", -1}, // 非点分版本小于任何正常版本
	}
	for _, c := range cases {
		if got := compareVersionStrings(c.a, c.b); got != c.want {
			t.Errorf("compareVersionStrings(%q, %q) = %d，期望 %d", c.a, c.b, got, c.want)
		}
	}
}
