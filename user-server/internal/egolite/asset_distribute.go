package egolite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"hivemtk-user/internal/model"
)

// 资产分发（T3 的用户端等价实现；调研 §6.5 + F13 勘误定案）。
//
// 拓扑事实（T2 真机实测）：ego 的 learnings 目录是物理路径
// ~/.local/share/ego/ego-skills/learnings/{site}/，`EGO_BROWSER_AGENT_WORKSPACE`
// 不穿透到内嵌 Node——因此分发**不走 env 重定向**，而是 runner 拉取清单后
// 直接写物理目录（F13）。
//
// 分发对象两类，都在 user-server 内自包含（不碰 platform-server / binary_asset）：
//  1. 资产目录条目（egolite_assets，管理端发布 → 全租户可见，source=flow|pack）；
//  2. 用户自己 published 的 flow（本文件，服务端打包 → runner 落盘 → market 执行）。
//
// 本文件刻意做成**幂等清单**：runner 每次拉全量清单，按 sha256 比对本地已装版本，
// 只写变化项——不做增量协议，因为清单规模是「目录资产 + 一个用户的流程数」
// （十位级），增量协议的复杂度换不来收益。
//
// 同名冲突纪律：目录资产优先于用户自己的同 slug 流程（目录资产是审核过的版本，
// 且执行侧 asset_id 就是 asset_code，覆盖它等于让"点了 A 跑了 B"）。

// InstallableAsset 可安装资产（runner 侧落盘输入；形状即 runner 的安装契约）
type InstallableAsset struct {
	ID         string   `json:"id"` // 目录名（learnings/{id}）
	Name       string   `json:"name"`
	Version    int      `json:"version"`
	Domains    []string `json:"domains"`
	Writes     []string `json:"writes"`
	Entry      string   `json:"entry"`       // tools 下相对路径（如 searchRepos.js）
	Script     string   `json:"script"`      // 入口脚本全文
	ScriptHash string   `json:"script_hash"` // sha256(script)，runner 比对本地已装
	Source     string   `json:"source"`      // flow（用户流程）/ pack（目录资产）
	FlowID     uint     `json:"flow_id"`     // 用户流程来源时的 flow 号（目录资产为 0）
	AssetID    string   `json:"asset_id"`    // 目录资产编码（用户流程为空）
	AssetVer   string   `json:"asset_version"`

	// 签名面（S-6 / 决策 8：ed25519）。Sig 为空 = 未签名（旧数据，兼容）；
	// 有 Sig 时服务端与 runner 两端都验，验不过一律 fail-close。
	SigAlg    string `json:"sig_alg,omitempty"`   // 固定 ed25519
	Signature string `json:"signature,omitempty"` // base64(std)，对 manifest 规范化字节（signature 置空）的签名
	PubKeyFpr string `json:"pubkey_fingerprint,omitempty"`
}

// BuildInstallableAssets 把用户 published 流程转成可安装清单（纯函数，便于测试）。
//
// 过滤纪律：
//   - 只发 published（draft/submitted 不出门——submitted 是审核中，外发等于绕过审核）；
//   - 扫描门未通过的**一律不发**（双保险：保存时已扫，这里再判是因为
//     scan_passed 是可被 rescan 改写的状态列，出门口必须以当下值为准）；
//   - entry 名做 slug 化（只允许 [a-z0-9_-]，防止目录穿越——runner 落盘用）。
func BuildInstallableAssets(flows []*model.EgoliteFlow) ([]InstallableAsset, error) {
	out := make([]InstallableAsset, 0, len(flows))
	for _, f := range flows {
		if f == nil || f.Status != model.EgoFlowPublished || !f.ScanPassed || f.Script == "" {
			continue
		}
		id := slugify(f.Name)
		if id == "" {
			id = fmt.Sprintf("flow-%d", f.ID)
		}
		entry := slugify(strings.TrimSpace(f.Name))
		if entry == "" {
			entry = fmt.Sprintf("flow-%d", f.ID)
		}
		entry += ".js"

		var domains []string
		if len(f.DomainsJSON) > 0 {
			if err := json.Unmarshal(f.DomainsJSON, &domains); err != nil {
				return nil, fmt.Errorf("flow %d domains 解析失败: %w", f.ID, err)
			}
		}
		if len(domains) == 0 {
			continue // 无域名的资产不可执行（market 通道要求白名单），静默跳过
		}
		var writes []string
		if len(f.WritesJSON) > 0 {
			_ = json.Unmarshal(f.WritesJSON, &writes)
		}
		sum := sha256.Sum256([]byte(f.Script))
		out = append(out, InstallableAsset{
			ID:         id,
			Name:       f.Name,
			Version:    f.Version,
			Domains:    domains,
			Writes:     writes,
			Entry:      path.Join("tools", entry),
			Script:     f.Script,
			ScriptHash: hex.EncodeToString(sum[:]),
			Source:     model.EgoAssetSourceFlow,
			FlowID:     f.ID,
		})
	}
	return out, nil
}

// BuildInstallableFromCatalog 把资产目录条目转成可安装清单（纯函数）。
//
// 与 flow 侧同一套过滤纪律（published + scan_passed + 有域名 + 有脚本），
// 唯独入口路径直接取资产自身 Entry（发布时已 slug 化到 tools/ 下），
// 且带上 asset_id/asset_version——执行侧 market origin 的入参就是这两个值。
func BuildInstallableFromCatalog(assets []*model.EgoliteAsset) ([]InstallableAsset, error) {
	out := make([]InstallableAsset, 0, len(assets))
	for _, a := range assets {
		if a == nil || a.Status != model.EgoAssetPublished || !a.ScanPassed || a.Script == "" {
			continue
		}
		id := slugify(a.AssetCode)
		if id == "" {
			continue
		}
		var domains []string
		if len(a.DomainsJSON) > 0 {
			if err := json.Unmarshal(a.DomainsJSON, &domains); err != nil {
				return nil, fmt.Errorf("资产 %s domains 解析失败: %w", a.AssetCode, err)
			}
		}
		if len(domains) == 0 {
			continue
		}
		var writes []string
		if len(a.WritesJSON) > 0 {
			_ = json.Unmarshal(a.WritesJSON, &writes)
		}
		sum := sha256.Sum256([]byte(a.Script))
		out = append(out, InstallableAsset{
			ID:         id,
			Name:       a.Name,
			Version:    a.Version,
			Domains:    domains,
			Writes:     writes,
			Entry:      a.Entry,
			Script:     a.Script,
			ScriptHash: hex.EncodeToString(sum[:]),
			Source:     a.Source,
			AssetID:    slugify(a.AssetCode),
			AssetVer:   fmt.Sprintf("%d", a.Version),
		})
	}
	return out, nil
}

// slugify 目录名/文件名安全化（只留 [a-z0-9_-]，中文名退化为空由调用方兜底）
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-_")
}

// ListInstallableForUser 服务端打包入口（routes 层调用）。
//
// 清单 = 目录资产（全租户已发布） + 该用户自己 published 的流程；
// 同 slug 时目录资产胜出（见文件头冲突纪律）。
func ListInstallableForUser(ctx context.Context, flowRepo EgoFlowRepository, assetRepo EgoAssetRepository, userID uint) ([]InstallableAsset, error) {
	out := make([]InstallableAsset, 0, 32)
	taken := make(map[string]bool, 32)

	if assetRepo != nil {
		assets, err := assetRepo.ListPublished(ctx)
		if err != nil {
			return nil, err
		}
		cat, err := BuildInstallableFromCatalog(assets)
		if err != nil {
			return nil, err
		}
		for _, a := range cat {
			if taken[a.ID] {
				continue
			}
			taken[a.ID] = true
			out = append(out, a)
		}
	}

	// published 的全量（单用户流程数十位级，一页 100 足够；超出时按页循环）
	all := make([]*model.EgoliteFlow, 0, 100)
	for page := 1; page <= 10; page++ {
		list, total, err := flowRepo.ListByUser(ctx, userID, page, 100)
		if err != nil {
			return nil, err
		}
		all = append(all, list...)
		if len(all) >= int(total) || len(list) == 0 {
			break
		}
	}
	flows, err := BuildInstallableAssets(all)
	if err != nil {
		return nil, err
	}
	for _, a := range flows {
		if taken[a.ID] {
			continue // 目录资产优先
		}
		taken[a.ID] = true
		out = append(out, a)
	}
	return out, nil
}
