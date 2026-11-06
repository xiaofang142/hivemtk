package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"hivemtk-user/internal/domain/asset"
	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/repository"
)

type LocalAssetFilter = repository.LocalAssetFilter

// LocalAssetService 本地资产业务（同源同构）
type LocalAssetService struct {
	assetRepo      repository.LocalAssetRepository
	dataRepo       repository.LocalAssetDataRepository
	syncLogRepo    repository.LocalAssetSyncLogRepository
	platformClient repository.PlatformAPIClient
}

func NewLocalAssetService(
	ar repository.LocalAssetRepository,
	dr repository.LocalAssetDataRepository,
	sr repository.LocalAssetSyncLogRepository,
	pc repository.PlatformAPIClient,
) *LocalAssetService {
	return &LocalAssetService{assetRepo: ar, dataRepo: dr, syncLogRepo: sr, platformClient: pc}
}

// CreateAssetInput 自建输入
type CreateAssetInput struct {
	AssetID   string          `json:"asset_id"`
	AssetType string          `json:"asset_type"`
	Industry  string          `json:"industry"`
	Name      string          `json:"name"`
	Data      json.RawMessage `json:"data"`
}

func platformNameIntoData(raw json.RawMessage, name string) json.RawMessage {
	if name == "" {
		return raw
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	if _, ok := m["name"]; ok {
		return raw
	}
	nameJSON, err := json.Marshal(name)
	if err != nil {
		return raw
	}
	m["name"] = nameJSON
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// UpdateAssetInput 更新输入
type UpdateAssetInput struct {
	AssetType string          `json:"asset_type"`
	Industry  string          `json:"industry"`
	Name      string          `json:"name"`
	Data      json.RawMessage `json:"data"`
}

// purchaseFailMsg 把平台购买失败的原因转成产品文案。
// 原因只能从 *platform.PlatformError 的结构化字段取：R11 起拒绝在传输层统一成该类型，
// 老的 "platform error <code>: <msg>" 字符串切片口径已随之失效，切不到就会把内部格式吐给用户。
func purchaseFailMsg(err error) string {
	var perr *platform.PlatformError
	if errors.As(err, &perr) {
		return "平台购买失败: " + perr.Msg()
	}
	return "平台购买失败: " + err.Error()
}

func (s *LocalAssetService) PurchaseAndSync(ctx context.Context, platformAssetID string) error {
	existing, err := s.assetRepo.FindByAssetID(ctx, platformAssetID)
	if err == nil && existing != nil {
		return bizerr.New(bizerr.CodeAssetDup, "资产已存在,请直接点击「同步到最新版本」")
	}

	// tenant_code 由 AssetMarketClient.Purchase 从 ctx 解析（口径同
	// currentTenantCode）：平台购买记录按 (merchant_key, tenant_code) 每租户一行。
	if err := s.platformClient.Purchase(ctx, platformAssetID); err != nil {

		return bizerr.New(bizerr.CodePlatformUnavail, purchaseFailMsg(err))
	}

	payload, err := s.platformClient.PullData(ctx, platformAssetID)
	if err != nil {
		return bizerr.Wrap(bizerr.CodeSyncFailed, "拉取平台数据失败", err)
	}
	payload.Data = platformNameIntoData(payload.Data, payload.Name)
	originalType := payload.AssetType
	normalizeBundleType(&payload.AssetType)
	payload.Data = normalizeBundleData(payload.Data, payload.Name, originalType)

	// browser_extension（T2 验收缺陷）：扩展资产的内容是 file_url 指向的安装包，
	// 平台没有 asset_data JSON——原样走 ValidateAssetData 必报「未知资产类型/
	// 资产 JSON 格式错误」，扩展永远买不下来。跳过内容校验，元数据照常落库。
	if payload.AssetType != "browser_extension" {
		if err := asset.ValidateAssetData(asset.AssetType(payload.AssetType), payload.Data); err != nil {
			return bizerr.Wrap(bizerr.CodeAssetInvalid, "平台数据校验失败："+err.Error(), err)
		}
	} else if len(payload.Data) == 0 {
		// local_asset_data.data 是 NOT NULL（二次回归 5000 实锤）：扩展把安装包
		// 元信息序列化成 data 落库，my-assets 渲染与后续下载入口都有据可依。
		meta, merr := json.Marshal(map[string]any{
			"name":       payload.Name,
			"asset_type": "browser_extension",
			"version":    payload.Version,
			"file_url":   payload.FileURL,
			"filename":   payload.Filename,
			"size_bytes": payload.SizeBytes,
			"sha256":     payload.SHA256,
		})
		if merr != nil {
			return bizerr.Wrap(bizerr.CodeInternal, "扩展元数据序列化失败", merr)
		}
		payload.Data = meta
	}

	now := time.Now()
	la := &model.LocalAsset{
		AssetID:     payload.AssetID,
		AssetType:   payload.AssetType,
		Industry:    payload.Industry,
		Name:        payload.Name,
		Version:     payload.Version,
		Source:      model.AssetSourcePurchased,
		IsActive:    true,
		PurchaseID:  &payload.PurchaseID,
		PurchasedAt: &now,
		SyncedAt:    now,
	}
	syncLog := &model.LocalAssetSyncLog{
		AssetID: la.AssetID, Action: "purchase_sync", Status: "success",
	}
	return s.assetRepo.PurchaseAndSyncTx(ctx, la, payload.Data, syncLog)
}

// SyncFromPlatform 同步最新版本
func (s *LocalAssetService) SyncFromPlatform(ctx context.Context, assetID string) error {
	la, err := s.assetRepo.FindByAssetID(ctx, assetID)
	if err != nil {
		return bizerr.Wrap(bizerr.CodeAssetNotFound, "资产不存在", err)
	}
	if la.Source != model.AssetSourcePurchased && la.Source != model.AssetSourceSynced {
		return bizerr.New(bizerr.CodeForbidden, "仅平台来源资产支持同步")
	}

	payload, err := s.platformClient.PullData(ctx, assetID)
	if err != nil {
		_ = s.syncLogRepo.Create(ctx, &model.LocalAssetSyncLog{
			AssetID: assetID, Action: "sync", Status: "failed", ErrorMsg: err.Error(),
		})
		return bizerr.Wrap(bizerr.CodeSyncFailed, "拉取失败", err)
	}
	payload.Data = platformNameIntoData(payload.Data, payload.Name)
	originalType := payload.AssetType
	normalizeBundleType(&payload.AssetType)
	payload.Data = normalizeBundleData(payload.Data, payload.Name, originalType)

	la.Version = payload.Version
	la.SyncedAt = time.Now()
	syncLog := &model.LocalAssetSyncLog{
		AssetID: assetID, Action: "sync", Status: "success",
	}
	if err := s.assetRepo.SyncDataTx(ctx, la, payload.Data, syncLog); err != nil {
		return err
	}
	return nil
}

// CreateManual 自建资产
func (s *LocalAssetService) CreateManual(ctx context.Context, in *CreateAssetInput) (*model.LocalAsset, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "资产名称不能为空")
	}
	if !asset.AssetType(in.AssetType).Valid() {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "asset_type 非法")
	}
	if !asset.Industry(in.Industry).Valid() {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "industry 不在支持列表内")
	}
	if err := asset.ValidateAssetData(asset.AssetType(in.AssetType), in.Data); err != nil {
		// 把具体缺哪个字段一起带出：只回「资产 JSON 校验失败」时，前端只能看到
		// 一句无法自助修复的话（缺 system_prompt / scripts / variants… 都长一样）。
		return nil, bizerr.Wrap(bizerr.CodeAssetInvalid, "资产 JSON 校验失败："+err.Error(), err)
	}
	if in.AssetID == "" {
		in.AssetID = "manual-" + uuid.NewString()
	}
	if existing, _ := s.assetRepo.FindByAssetID(ctx, in.AssetID); existing != nil && existing.ID > 0 {
		return nil, bizerr.New(bizerr.CodeAssetDup, "asset_id 已存在")
	}

	la := &model.LocalAsset{
		AssetID:   in.AssetID,
		AssetType: in.AssetType,
		Industry:  in.Industry,
		Name:      name,
		Version:   "1.0.0",
		Source:    model.AssetSourceManual,
		IsActive:  true,
		SyncedAt:  time.Now(),
	}
	if err := s.assetRepo.Create(ctx, la); err != nil {
		return nil, bizerr.Wrap(bizerr.CodeInternal, "创建资产失败", err)
	}
	lad := &model.LocalAssetData{LocalAssetID: la.ID, Data: in.Data, UpdatedAt: time.Now()}
	if err := s.dataRepo.Create(ctx, lad); err != nil {
		return nil, bizerr.Wrap(bizerr.CodeInternal, "保存资产数据失败", err)
	}
	return la, nil
}

// Update 编辑
func (s *LocalAssetService) Update(ctx context.Context, id int64, in *UpdateAssetInput) error {
	la, err := s.assetRepo.FindByID(ctx, id)
	if err != nil {
		return bizerr.Wrap(bizerr.CodeAssetNotFound, "资产不存在", err)
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return bizerr.New(bizerr.CodeParamInvalid, "资产名称不能为空")
	}
	if !asset.AssetType(in.AssetType).Valid() {
		return bizerr.New(bizerr.CodeParamInvalid, "asset_type 非法")
	}
	// 与 CreateManual 对齐：更新同样要校验 industry。原先只校验 asset_type，
	// 于是 PUT 可以把 industry 写成任意字符串（列表按行业筛选就再也命中不到）。
	if !asset.Industry(in.Industry).Valid() {
		return bizerr.New(bizerr.CodeParamInvalid, "industry 不在支持列表内")
	}
	if err := asset.ValidateAssetData(asset.AssetType(in.AssetType), in.Data); err != nil {
		return bizerr.Wrap(bizerr.CodeAssetInvalid, "资产 JSON 校验失败："+err.Error(), err)
	}
	la.Name = name
	la.AssetType = in.AssetType
	la.Industry = in.Industry
	la.UpdatedAt = time.Now()
	return s.assetRepo.UpdateWithDataTx(ctx, la, in.Data)
}

// List 列表
func (s *LocalAssetService) List(ctx context.Context, f LocalAssetFilter) ([]*model.LocalAsset, int64, error) {
	return s.assetRepo.List(ctx, f)
}

// Get 详情
func (s *LocalAssetService) Get(ctx context.Context, id int64) (*model.LocalAsset, []byte, error) {
	la, err := s.assetRepo.FindByID(ctx, id)
	if err != nil {
		return nil, nil, bizerr.Wrap(bizerr.CodeAssetNotFound, "资产不存在", err)
	}
	lad, err := s.dataRepo.FindByLocalAssetID(ctx, la.ID)
	if err != nil {
		return la, nil, nil
	}
	return la, lad.Data, nil
}

// SoftDelete 软删
func (s *LocalAssetService) SoftDelete(ctx context.Context, id int64) error {
	// 先确认存在：GORM 的 UPDATE/DELETE 在 0 行时 err 仍为 nil，直接下发会把
	// 「删/停一个不存在的资产」报成成功，前端拿不到任何「这条早没了」的信号。
	if _, err := s.assetRepo.FindByID(ctx, id); err != nil {
		return bizerr.Wrap(bizerr.CodeAssetNotFound, "资产不存在", err)
	}
	return s.assetRepo.SoftDelete(ctx, id)
}

// ToggleActive 启停
func (s *LocalAssetService) ToggleActive(ctx context.Context, id int64, active bool) error {
	if _, err := s.assetRepo.FindByID(ctx, id); err != nil {
		return bizerr.Wrap(bizerr.CodeAssetNotFound, "资产不存在", err)
	}
	return s.assetRepo.ToggleActive(ctx, id, active)
}

// GetSyncLog 同步日志
func (s *LocalAssetService) GetSyncLog(ctx context.Context, assetID string, limit int) ([]*model.LocalAssetSyncLog, error) {
	return s.syncLogRepo.List(ctx, assetID, limit)
}

// LoadByType Loader 使用
func (s *LocalAssetService) LoadByType(ctx context.Context, assetType string) ([]*model.LocalAsset, [][]byte, error) {
	list, err := s.assetRepo.ListByTypeAndActive(ctx, assetType)
	if err != nil {
		return nil, nil, err
	}
	var datas [][]byte
	for _, la := range list {
		lad, err := s.dataRepo.FindByLocalAssetID(ctx, la.ID)
		if err == nil {
			datas = append(datas, lad.Data)
		}
		if la.PurchaseID != nil {
			_ = s.assetRepo.IncrementUseCount(ctx, la.ID, 1)
			go s.reportUsageAsync(ctx, la.AssetID)
		}
	}
	return list, datas, nil
}

// LoadOne 按 AssetID 加载单个已同步本地资产及其 ChatML 数据（闭环：运行时消费）。
//
// 实现 asset_bundle.LocalAssetLoader 接口，供 AssetBundleService.ResolveSystemPrompt
// 在 asset_bundles 未命中时回退使用，闭合「平台下发资产包 → 商户同步 → 运行时被
// 智能体消费」全链路。同时累加使用次数并 best-effort 异步上报用量到平台（telemetry）。
// 资产不存在时返回 (nil, nil, nil)。
func (s *LocalAssetService) LoadOne(ctx context.Context, assetID string) (*model.LocalAsset, []byte, error) {
	la, err := s.assetRepo.FindByAssetID(ctx, assetID)
	if err != nil || la == nil {
		return nil, nil, nil
	}
	lad, err := s.dataRepo.FindByLocalAssetID(ctx, la.ID)
	if err != nil || lad == nil {
		return la, nil, nil
	}
	if la.PurchaseID != nil {
		_ = s.assetRepo.IncrementUseCount(ctx, la.ID, 1)
		go s.reportUsageAsync(ctx, la.AssetID)
	}
	return la, lad.Data, nil
}

var reportingInFlight sync.Map

// ReportUsage 将本地累计的使用次数回传平台（闭环：本地使用 → 平台统计）。
func (s *LocalAssetService) ReportUsage(ctx context.Context, assetID string) error {
	la, err := s.assetRepo.FindByAssetID(ctx, assetID)
	if err != nil {
		return bizerr.New(bizerr.CodeNotFound, "资产不存在")
	}
	if la.PurchaseID == nil {
		return bizerr.New(bizerr.CodeForbidden, "非平台购买资产无需上报")
	}
	delta := la.UseCount - la.ReportedUseCount
	if delta <= 0 {
		return nil
	}
	if err := s.platformClient.ReportUsage(ctx, assetID, delta); err != nil {
		return bizerr.Wrap(bizerr.CodePlatformUnavail, "平台上报失败", err)
	}
	return s.assetRepo.SetReportedUseCount(ctx, la.ID, la.UseCount)
}

func (s *LocalAssetService) reportUsageAsync(parent context.Context, assetID string) {
	if _, loaded := reportingInFlight.LoadOrStore(assetID, true); loaded {
		return
	}
	defer reportingInFlight.Delete(assetID)
	ctx, cancel := context.WithTimeout(tenant.BackgroundWithTenant(parent), utils.ShortTimeout)
	defer cancel()
	_ = s.ReportUsage(ctx, assetID)
}

// normalizeBundleType 平台「bundle」类型归一化为 agent_persona（T1 验收 BUG-4 根治）。
//
// 平台市场允许 bundle（打包人设）类资产，用户端五类资产契约里没有这个词——
// 原样落库会连环失败：校验器报「未知资产类型」、AgentLoader 按 asset_type=
// 'agent_persona' 查询永远漏掉 bundle 行。bundle 的数据对象与 agent_persona
// 同构（name+system_prompt+messages），入库前直接归一化，全链路无感消费。
func normalizeBundleType(t *string) {
	if t != nil && *t == "bundle" {
		*t = string(asset.AssetTypeAgentPersona)
	}
}

// normalizeBundleData 兜底：平台/管理端建的 bundle 若把剧本存成裸数组，
// 归一化成 agent_persona 后会卡在「缺少必填字段: name / system_prompt」。
// 这里把数组包成 {name, system_prompt, messages}，system_prompt 用一句
// 中性的通用人设（真正内容仍在 messages 里，运行时照常注入）。
// 已经是对象的数据原样返回（我们的 27 个包都走这条）。
func normalizeBundleData(raw json.RawMessage, name string, originalType string) json.RawMessage {
	if originalType != "bundle" || len(raw) == 0 {
		return raw
	}
	var probe any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return raw
	}
	arr, ok := probe.([]any)
	if !ok {
		return raw
	}
	wrapped, err := json.Marshal(map[string]any{
		"name":          name,
		"system_prompt": "你是本行业的专业智能客服助手，按下方示例对话的话术风格与业务口径回复客户；不确定的信息如实告知并转人工。",
		"messages":      arr,
	})
	if err != nil {
		return raw
	}
	return wrapped
}
