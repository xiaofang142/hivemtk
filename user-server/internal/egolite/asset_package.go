package egolite

// asset_package.go —— 资产包通道（S-7，C6）：把可安装清单打成 zip 下发。
//
// 为什么需要包而不是继续用「清单 + 脚本全文」：清单模式下 runner 逐文件落盘，
// 中途失败会留下半装状态（entry 写了、manifest 没写），且每个资产多一次 JSON
// 字段解析。zip 是原子的：runner 先解到临时目录、校验通过后再原子换入。
//
// 包内形状（与清单模式逐字节兼容，runner 侧两条路径写同一套文件）：
//
//	{id}/manifest.json     资产元信息（与 runner 现在自己写的 manifest 同键）
//	{id}/{entry}           入口脚本（entry 已 slug 化，见 BuildInstallableAssets）
//
// 安全边界（两端各判一次，互不信任）：
//   - id/entry 必须通过 slug 形状校验（无 ".."、非绝对路径、无反斜杠）；
//   - 单包条目数上限 512、单条目 4MB、总解压体积 32MB（防 zip bomb）；
//   - 同名 id 只保留首个（与服务端"目录资产优先"纪律一致）。

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

const (
	// AssetPackageMaxEntries 单包条目上限。
	AssetPackageMaxEntries = 512
	// AssetPackageMaxEntryBytes 单条目解压上限（4MB）。
	AssetPackageMaxEntryBytes = 4 << 20
	// AssetPackageMaxTotalBytes 整包解压上限（32MB）。
	AssetPackageMaxTotalBytes = 32 << 20
)

// BuildAssetPackage 把可安装清单打成 zip（纯函数，便于测试）。
//
// 顺序固定（先 manifest 再 entry，按 id 升序），保证同一份清单产出逐字节相同的包：
// runner 侧据 Content-Length/etag 判断"这轮要不要重下"才有意义。
func BuildAssetPackage(assets []InstallableAsset) ([]byte, error) {
	seen := make(map[string]bool, len(assets))
	ordered := make([]InstallableAsset, 0, len(assets))
	for _, a := range assets {
		if !safeAssetPath(a.ID) || !safeAssetPath(a.Entry) {
			return nil, fmt.Errorf("资产 %q 的 id/entry 非法（id=%q entry=%q）", a.Name, a.ID, a.Entry)
		}
		if a.Script == "" {
			return nil, fmt.Errorf("资产 %q 缺脚本", a.ID)
		}
		if seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		ordered = append(ordered, a)
	}
	if len(ordered) > AssetPackageMaxEntries {
		return nil, fmt.Errorf("条目数 %d 超过上限 %d", len(ordered), AssetPackageMaxEntries)
	}
	// 按 id 升序（插入排序即可，规模十位级）
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j-1].ID > ordered[j].ID; j-- {
			ordered[j-1], ordered[j] = ordered[j], ordered[j-1]
		}
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	total := int64(0)
	for _, a := range ordered {
		manifest := assetManifest(a)
		mb, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return nil, err
		}
		for _, f := range []struct {
			name string
			body []byte
		}{
			{path.Join(a.ID, "manifest.json"), mb},
			{path.Join(a.ID, a.Entry), []byte(a.Script)},
		} {
			name, body := f.name, f.body
			// 固定 Modified 时间不写（zip.Writer 默认零值），保证确定性输出。
			w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
			if err != nil {
				return nil, err
			}
			if len(body) > AssetPackageMaxEntryBytes {
				return nil, fmt.Errorf("条目 %s 超过单条上限 %d", name, AssetPackageMaxEntryBytes)
			}
			total += int64(len(body))
			if total > AssetPackageMaxTotalBytes {
				return nil, fmt.Errorf("包体积超过上限 %d", AssetPackageMaxTotalBytes)
			}
			if _, err := w.Write(body); err != nil {
				return nil, err
			}
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// assetManifest 包内 manifest 与 runner 现有 installAsset 写出的键保持一致。
func assetManifest(a InstallableAsset) map[string]any {
	source := a.Source
	if source == "" {
		source = "flow-publish"
	}
	version := fmt.Sprintf("%d.0.0", a.Version)
	return map[string]any{
		"id":      a.ID,
		"name":    a.Name,
		"version": version,
		"domains": a.Domains,
		"hivemtk": map[string]any{
			"entry":       a.Entry,
			"compat_min":  "0.6.0",
			"writes":      a.Writes,
			"source":      source,
			"flow_id":     a.FlowID,
			"asset_id":    a.AssetID,
			"asset_ver":   a.AssetVer,
			"script_hash": a.ScriptHash,
		},
	}
}

// safeAssetPath 校验相对路径形状（与服务端 slug 化、runner 落盘校验同一口径）。
func safeAssetPath(p string) bool {
	if p == "" || strings.Contains(p, "..") || strings.Contains(p, "\\") {
		return false
	}
	if strings.HasPrefix(p, "/") || path.IsAbs(p) {
		return false
	}
	return path.Clean(p) == p
}
