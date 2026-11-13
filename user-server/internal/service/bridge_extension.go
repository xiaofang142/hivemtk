package service

import (
	"context"
	"errors"
	"strings"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/repository"
)

// 桥接扩展安装包的下载通道（用户端 ← 平台端）。
//
// 形态：浏览器扩展不上架 Chrome Web Store，平台运营在平台端上传安装包，
// 租户在用户端网页下载后自行「加载已解压的扩展程序」。
//
// 为什么不直接让前端去打平台的 /files：
//   1. 平台的 file_url 是**相对路径**（/files/extensions/...），用户端的浏览器
//      不知道平台的对外域名 —— 拼绝对地址这件事得有个知道平台地址的地方；
//   2. 平台地址在本地部署形态下不存在（PLATFORM_ENABLED=false 时压根没有平台），
//      那时用户端必须给出"该形态下扩展包从哪来"的确定答案，而不是 404。
//
// 本服务的职责就一条：把"平台上的最新安装包"翻译成用户端可用的下载信息。
// 离线单机形态返回 ErrBridgeExtensionOffline —— 用户端据此提示"当前部署未接入
// 平台，请在平台端上传后重试"，而不是给一个打不开的链接。

// bridgeExtensionAssetType 平台资产类型：浏览器扩展。
//
// 与平台端 model.AssetTypeBrowserExtension 字面一致（跨仓常量，无共享包）。
const bridgeExtensionAssetType = "browser_extension"

// ErrBridgeExtensionOffline 本部署未接入平台，没有可下载的扩展包
var ErrBridgeExtensionOffline = errors.New("当前部署未接入平台，无可下载的桥接扩展包")

// BridgeExtensionPackage 扩展安装包信息
type BridgeExtensionPackage struct {
	AssetID    string `json:"asset_id"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	FileURL    string `json:"file_url"`
	Filename   string `json:"filename"`
	SizeBytes  int64  `json:"size_bytes"`
	FileSHA256 string `json:"file_sha256"`
	// DownloadURL 绝对下载地址（用户端浏览器直接用这个）。
	DownloadURL string `json:"download_url"`
}

// BridgeExtensionService 桥接扩展分发服务
type BridgeExtensionService struct {
	platformClient repository.PlatformAPIClient
}

// NewBridgeExtensionService 构造（注入平台客户端；平台关闭时该客户端是空实现）
func NewBridgeExtensionService(pc repository.PlatformAPIClient) *BridgeExtensionService {
	return &BridgeExtensionService{platformClient: pc}
}

// Latest 取平台上最新一版桥接扩展安装包。
//
// 流程与资产市场的商品流一致：先列市场（只看已上架），在候选里挑出版本号
// 最新的 browser_extension，先 purchase（免费资产零成本、幂等）再拉详情拿
// file_url —— file_url 在 asset_versions 行上，列表接口不带它。
//
// 两处关键口径（2026-11-13 线上排查实证，别改）：
//  1. **必须先 purchase 再 detail**：平台 GetMarketAssetDetail 的 version_info
//     只在资产已购买（purchased=true）后才带 file_url/filename/file_sha256/
//     size_bytes；未购买时 version_info 只有 changelog/created_at/
//     schema_version/version ⇒ 不 purchase 就永远拿不到 file_url，线上表现为
//     一律报「平台尚未上架桥接扩展包」。免费资产 price=0，购买零成本。
//  2. **不能取 list[0]**：平台 ListAssets 按 updated_at 倒序，而 purchase 会
//     刷新 updated_at ⇒ 买过的旧资产会被顶到第一位，取 list[0] 会把租户导到
//     旧版本（实测 1.3.0 上传后用户端返回 1.0.5）。故按版本号语义比较挑最新。
func (s *BridgeExtensionService) Latest(ctx context.Context) (*BridgeExtensionPackage, error) {
	if !config.PlatformEnabled() {
		return nil, ErrBridgeExtensionOffline
	}
	if s.platformClient == nil {
		return nil, ErrBridgeExtensionOffline
	}
	list, _, err := s.platformClient.ListAssets(ctx, bridgeExtensionAssetType, "", 1, 50)
	if err != nil {
		return nil, err
	}
	assetID := latestExtensionAssetID(list)
	if assetID == "" {
		return nil, ErrBridgeExtensionOffline
	}
	// 先购买（免费资产零成本、幂等）：只有 purchased=true 的平台详情才会在
	// version_info 里带出 file_url 等文件字段。
	if err := s.platformClient.Purchase(ctx, assetID); err != nil {
		return nil, err
	}
	detail, err := s.platformClient.GetAssetDetail(ctx, assetID)
	if err != nil {
		return nil, err
	}
	pkg := &BridgeExtensionPackage{
		AssetID: assetID,
		Name:    stringField(detail, "name"),
		Version: stringField(detail, "latest_version"),
	}
	// 版本行对象在详情里的键经历过一次跨仓漂移（T1 修复，platform 仓
	// asset_market_service.go GetMarketAssetDetail）：
	//   - 旧：result["latest_version"] = v —— 版本对象直接盖掉字符串；
	//   - 新：latest_version 保持 assets 表的字符串语义（"1.0.3"），
	//         版本行对象改放 result["version_info"]（前端徽章曾把 JSON
	//         原文渲染成 v{...}，故拆键）。
	// 本服务两端都要认：version_info 优先（新平台），latest_version 仍为
	// map 时回退（旧平台）。只认旧键的后果是 file_url 恒空 ⇒ 线上一律
	// 报"未上架"（2026-11-04 线上回归实测，v1.0.2 时代即已断）。
	if v, ok := detail["version_info"].(map[string]any); ok {
		pkg.Version = stringField(v, "version")
		pkg.FileURL = stringField(v, "file_url")
		pkg.Filename = stringField(v, "filename")
		pkg.FileSHA256 = stringField(v, "file_sha256")
		if sb, ok := v["size_bytes"].(float64); ok {
			pkg.SizeBytes = int64(sb)
		}
	} else if v, ok := detail["latest_version"].(map[string]any); ok {
		pkg.Version = stringField(v, "version")
		pkg.FileURL = stringField(v, "file_url")
		pkg.Filename = stringField(v, "filename")
		pkg.FileSHA256 = stringField(v, "file_sha256")
		if sb, ok := v["size_bytes"].(float64); ok {
			pkg.SizeBytes = int64(sb)
		}
	} else {
		pkg.Version = stringField(detail, "latest_version")
	}
	if pkg.FileURL == "" {
		return nil, ErrBridgeExtensionOffline
	}
	pkg.DownloadURL = absoluteDownloadURL(pkg.FileURL)
	return pkg, nil
}

// absoluteDownloadURL 把平台返回的相对路径补成绝对地址。
//
// 平台的 file_url 是 /files/... 形态（本地存储驱动的 PublicURL 前缀），
// 用户端浏览器要能直接下 ⇒ 前面得拼上平台对外地址。
// 已经是绝对地址（http/https 开头）时原样返回 —— 云存储驱动（OSS/S3）返回的就是绝对地址。
func absoluteDownloadURL(fileURL string) string {
	u := strings.TrimSpace(fileURL)
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	base := strings.TrimRight(strings.TrimSpace(config.PlatformURL()), "/")
	if base == "" {
		return u
	}
	if !strings.HasPrefix(u, "/") {
		u = "/" + u
	}
	return base + u
}

// stringField 从 map 里取字符串字段（非字符串或缺失时返回空串）
func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// latestExtensionAssetID 从平台市场列表里挑出版本号最新的 browser_extension
// 资产 ID。
//
// 为什么不取 list[0]：平台 ListAssets 按 updated_at 倒序，而 purchase 会刷新
// updated_at（2026-11-13 实测：买过旧版本后旧资产被顶到第一位，用户端因此
// 拿到 1.0.5 而不是刚上传的 1.3.0）。故按 latest_version 语义比较挑最新。
//
// 版本号按「点分数字段逐段比，缺段按 0」处理：1.3.0 > 1.0.5 > 1.0.2；
// 版本相同（或都解析不出）时保持列表原顺序，让平台排序兜底。
func latestExtensionAssetID(list []map[string]any) string {
	best := ""
	bestVer := ""
	for _, item := range list {
		id := stringField(item, "asset_id")
		if id == "" {
			continue
		}
		ver := stringField(item, "latest_version")
		if best == "" || compareVersionStrings(ver, bestVer) > 0 {
			best, bestVer = id, ver
		}
	}
	return best
}

// compareVersionStrings 比较两个点分版本号：a>b 返回 1，相等 0，a<b 返回 -1。
// 非点分版本（如 "v1"、空串）按小于任何正常版本处理，保证有版本号的优先。
func compareVersionStrings(a, b string) int {
	as, bs := splitVersion(a), splitVersion(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		x, y := 0, 0
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

// splitVersion 把 "1.3.0" 切成 [1,3,0]；非数字段按 0 处理（宁小勿大）。
func splitVersion(v string) []int {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n := 0
		digits := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
			digits++
		}
		if digits == 0 {
			n = 0
		}
		out = append(out, n)
	}
	return out
}
