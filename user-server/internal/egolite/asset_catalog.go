package egolite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/datatypes"
)

// EgoAssetCatalog 资产目录服务：**平台端（本实例管理端）发布 → 用户端浏览/使用 →
// 下发用户浏览器执行** 全链路的自包含实现。
//
// 不耦合纪律（本文件的立身之本）：
//   - 不走 platform-server、不看 PLATFORM_ENABLED、不碰 binary_asset 通道；
//   - 「平台端」= 本实例的管理端（role=admin），发布动作就是本表写一行；
//   - 资产内容、扫描结论、签名之外的完整性校验（sha256）全部在 user-server 内闭环。
//
// 两条发布入口：
//  1. PublishFromFlow：审核用户提交的流程（submitted）→ 复扫 → 快照成目录条目；
//     审核通过后 flow 回到 published（用户侧看到"已上架"），脚本快照从此与 flow 解耦。
//  2. PublishFromPack：管理端直接发脚本包（自包含通道，适合基线能力/官方资产）。
//
// 发布闸（两条入口共用）：脚本必须过 ScanScript（block 级 0 命中）；域名必填；
// entry 必填且 slug 化到 tools/ 下（防穿越，与分发侧同一实现）。

// EgoAssetCatalog 资产目录
type EgoAssetCatalog struct {
	assetRepo EgoAssetRepository
	flowRepo  EgoFlowRepository
}

func NewEgoAssetCatalog(assetRepo EgoAssetRepository, flowRepo EgoFlowRepository) *EgoAssetCatalog {
	return &EgoAssetCatalog{assetRepo: assetRepo, flowRepo: flowRepo}
}

// PackPublishRequest 管理端直接发布脚本包
type PackPublishRequest struct {
	AssetCode    string          `json:"asset_code"` // 可省略，省略时按 name slug 化
	Name         string          `json:"name"`
	Domains      []string        `json:"domains"`
	Writes       []string        `json:"writes"`
	ParamsSchema json.RawMessage `json:"params_schema"`
	Entry        string          `json:"entry"` // 可省略，省略时按 asset_code 推导 tools/{code}.js
	Script       string          `json:"script"`
}

// PublishFromFlow 审核通过并发布为目录资产。
func (c *EgoAssetCatalog) PublishFromFlow(ctx context.Context, publisherUserID, flowID uint) (*model.EgoliteAsset, error) {
	// 管理端按流程号读（跨用户）；用户侧"提交审核"已保证归属校验
	flow, err := c.flowRepo.GetByID(ctx, flowID)
	if err != nil {
		return nil, fmt.Errorf("流程不存在")
	}
	if flow.Status != model.EgoFlowSubmitted {
		return nil, fmt.Errorf("只有已提交审核（submitted）的流程可发布为资产，当前状态 %s", flow.Status)
	}
	if strings.TrimSpace(flow.Script) == "" {
		return nil, fmt.Errorf("流程脚本为空，禁止发布")
	}

	// 发布前复扫（不信 submitted 时的历史结论：扫描规则会演进，出门口以当下扫描为准）
	report := ScanScript(flow.Script)
	if !report.Passed {
		// 复扫结论回写 flow（用户看到"审核未过 + 报告"），资产不落库
		_ = c.flowRepo.Update(ctx, flow.ID, flow.UserID, map[string]any{
			"scan_passed": false,
			"scan_report": datatypes.JSON(mustJSON(report)),
			"status":      model.EgoFlowPublished,
			"updated_at":  time.Now(),
		})
		return nil, fmt.Errorf("复扫未通过（block 命中 %d 条），禁止发布", len(report.Blocked))
	}

	code := slugify(flow.Name)
	if code == "" {
		code = fmt.Sprintf("flow-%d", flow.ID)
	}
	var domains, writes []string
	if len(flow.DomainsJSON) > 0 {
		if err := json.Unmarshal(flow.DomainsJSON, &domains); err != nil {
			return nil, fmt.Errorf("流程 domains 解析失败: %w", err)
		}
	}
	if len(domains) == 0 {
		return nil, fmt.Errorf("流程未声明 domains，不可作为可执行资产发布")
	}
	if len(flow.WritesJSON) > 0 {
		_ = json.Unmarshal(flow.WritesJSON, &writes)
	}

	version := 1
	if prev, err := c.assetRepo.GetByCode(ctx, code); err == nil && prev != nil {
		version = prev.Version + 1 // 同编码再发布 = 版本自增（内容快照不可变语义）
	}

	now := time.Now()
	asset, err := c.assetRepo.Upsert(ctx, &model.EgoliteAsset{
		AssetCode:       code,
		Name:            flow.Name,
		Version:         version,
		DomainsJSON:     mustJSONBytes(domains),
		WritesJSON:      mustJSONBytes(writes),
		ParamsSchema:    flow.ParamsSchema,
		Entry:           path.Join("tools", slugOr(code)+".js"),
		Script:          flow.Script,
		ScriptHash:      sha256Hex(flow.Script),
		Status:          model.EgoAssetPublished,
		ScanPassed:      true,
		ScanReport:      datatypes.JSON(mustJSON(report)),
		Source:          model.EgoAssetSourceFlow,
		SourceFlowID:    flow.ID,
		PublisherUserID: publisherUserID,
		PublishedAt:     &now,
	})
	if err != nil {
		return nil, err
	}

	// 审核通过：flow 回到 published 并回写复扫结论（用户侧唯一可观测的审核结果面）
	_ = c.flowRepo.Update(ctx, flow.ID, flow.UserID, map[string]any{
		"scan_passed": true,
		"scan_report": datatypes.JSON(mustJSON(report)),
		"status":      model.EgoFlowPublished,
		"updated_at":  time.Now(),
	})
	return asset, nil
}

// PublishFromPack 管理端直接发布脚本包（不依赖任何外部平台通道）。
func (c *EgoAssetCatalog) PublishFromPack(ctx context.Context, publisherUserID uint, req PackPublishRequest) (*model.EgoliteAsset, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("资产名称必填")
	}
	code := slugify(req.AssetCode)
	if code == "" {
		code = slugify(req.Name)
	}
	if code == "" {
		return nil, fmt.Errorf("asset_code 必填（可由名称 slug 化推导，名称需含英文字母/数字）")
	}
	if len(req.Domains) == 0 {
		return nil, fmt.Errorf("domains 必填（域名白名单，与 manifest 同纪律）")
	}
	for _, d := range req.Domains {
		d = strings.TrimSpace(d)
		if d == "" {
			return nil, fmt.Errorf("domains 不允许空串")
		}
		if strings.Contains(d, "*") && d != "*" {
			return nil, fmt.Errorf("domains 通配仅允许整站 *，不支持部分通配: %q", d)
		}
	}
	if strings.TrimSpace(req.Script) == "" {
		return nil, fmt.Errorf("script 必填")
	}

	report := ScanScript(req.Script)
	if !report.Passed {
		return nil, fmt.Errorf("扫描门未通过（block 命中 %d 条），禁止发布", len(report.Blocked))
	}

	version := 1
	if prev, err := c.assetRepo.GetByCode(ctx, code); err == nil && prev != nil {
		version = prev.Version + 1
	}
	now := time.Now()
	return c.assetRepo.Upsert(ctx, &model.EgoliteAsset{
		AssetCode:       code,
		Name:            req.Name,
		Version:         version,
		DomainsJSON:     mustJSONBytes(normalizeDomains(req.Domains)),
		WritesJSON:      mustJSONBytes(req.Writes),
		ParamsSchema:    datatypes.JSON(req.ParamsSchema),
		Entry:           normalizeEntry(req.Entry, code),
		Script:          req.Script,
		ScriptHash:      sha256Hex(req.Script),
		Status:          model.EgoAssetPublished,
		ScanPassed:      true,
		ScanReport:      datatypes.JSON(mustJSON(report)),
		Source:          model.EgoAssetSourcePack,
		PublisherUserID: publisherUserID,
		PublishedAt:     &now,
	})
}

// Offline 软下线（不删行：已装副本仍在，删行会让"为什么它还能跑"变成谜）。
func (c *EgoAssetCatalog) Offline(ctx context.Context, id uint) (*model.EgoliteAsset, error) {
	if _, err := c.assetRepo.GetByID(ctx, id); err != nil {
		return nil, fmt.Errorf("资产不存在")
	}
	if err := c.assetRepo.SetStatus(ctx, id, model.EgoAssetOffline); err != nil {
		return nil, err
	}
	return c.assetRepo.GetByID(ctx, id)
}

// PublicList 用户端资产橱窗（只有 published + 扫描通过；offline 不出门）。
func (c *EgoAssetCatalog) PublicList(ctx context.Context) ([]*model.EgoliteAsset, error) {
	return c.assetRepo.ListPublished(ctx)
}

// AdminList 管理端全量列表（含 offline）。
func (c *EgoAssetCatalog) AdminList(ctx context.Context, page, pageSize int) ([]*model.EgoliteAsset, int64, error) {
	return c.assetRepo.ListAll(ctx, page, pageSize)
}

// MarksInstalled 市场执行引用的弱计数（失败不影响执行主流程）。
func (c *EgoAssetCatalog) MarksInstalled(ctx context.Context, code string) {
	_ = c.assetRepo.IncInstall(ctx, code)
}

// assetWriteNames 资产声明的写动作名（空 = 纯读资产，无需步级确认）
func assetWriteNames(a *model.EgoliteAsset) []string {
	if a == nil || len(a.WritesJSON) == 0 {
		return nil
	}
	var writes []string
	_ = json.Unmarshal(a.WritesJSON, &writes)
	out := writes[:0]
	for _, w := range writes {
		if strings.TrimSpace(w) != "" {
			out = append(out, w)
		}
	}
	return out
}

// normalizeEntry 入口路径规范化：只留 basename → slug → tools/{x}.js。
// 与 runner 侧的双端穿越防护配合（这里消灭路径，runner 侧再判一次）。
func normalizeEntry(entry, fallback string) string {
	base := path.Base(strings.ReplaceAll(strings.TrimSpace(entry), "\\", "/"))
	base = strings.TrimSuffix(base, path.Ext(base))
	name := slugify(base)
	if name == "" {
		name = slugOr(fallback)
	}
	return path.Join("tools", name+".js")
}

// normalizeDomains 去空白 + 去重（顺序保留）
func normalizeDomains(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, d := range in {
		d = strings.TrimSpace(d)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

func slugOr(s string) string {
	if v := slugify(s); v != "" {
		return v
	}
	return "asset"
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func mustJSONBytes(v any) datatypes.JSON {
	b, _ := json.Marshal(v)
	return datatypes.JSON(b)
}
