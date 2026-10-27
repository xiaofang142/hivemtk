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
// 分发对象两类：
//  1. 用户自己 published 的 flow（本文件，服务端打包 → runner 落盘 → market 执行）；
//  2. 平台下发的 egolite_skill 资产包（走平台 binary_asset 通道，签名验证见 asset_sign.go）。
//
// 本文件只做 ①（服务端唯一事实源是 egolite_flows 表），且刻意做成**幂等清单**：
// runner 每次拉全量清单，按 sha256 比对本地已装版本，只写变化项——不做增量协议，
// 因为清单规模是「一个用户的流程数」（十位级），增量协议的复杂度换不来收益。

// InstallableAsset 可安装资产（runner 侧落盘输入；形状即 runner 的安装契约）
type InstallableAsset struct {
	ID         string   `json:"id"`     // 目录名（learnings/{id}）
	Name       string   `json:"name"`
	Version    int      `json:"version"`
	Domains    []string `json:"domains"`
	Entry      string   `json:"entry"`       // tools 下相对路径（如 searchRepos.js）
	Script     string   `json:"script"`      // 入口脚本全文
	ScriptHash string   `json:"script_hash"` // sha256(script)，runner 比对本地已装
	FlowID     uint     `json:"flow_id"`
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
		sum := sha256.Sum256([]byte(f.Script))
		out = append(out, InstallableAsset{
			ID:         id,
			Name:       f.Name,
			Version:    f.Version,
			Domains:    domains,
			Entry:      path.Join("tools", entry),
			Script:     f.Script,
			ScriptHash: hex.EncodeToString(sum[:]),
			FlowID:     f.ID,
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

// ListPublishedForUser 服务端打包入口（routes 层调用）
func ListPublishedForUser(ctx context.Context, flowRepo EgoFlowRepository, userID uint) ([]InstallableAsset, error) {
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
	return BuildInstallableAssets(all)
}
