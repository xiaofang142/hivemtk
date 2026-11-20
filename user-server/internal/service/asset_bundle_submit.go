package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/platform"
)

// platformAssetClient 平台资产客户端的最小接口（FA-07：测试注入假贡献者服务，
// 生产装配为 *platform.ContributorClient）。
type platformAssetClient interface {
	GetAssetDetail(assetID int64) (*platform.ContributorAssetInfo, error)
	CreateAsset(p platform.CreateAssetPayload) (int64, error)
	SubmitAudit(assetID int64) error
	UploadVersion(assetID int64, version, changelog string, data []byte) error
}

var _ platformAssetClient = (*platform.ContributorClient)(nil)

// SetPlatformClientForBundleService 注入平台客户端（测试用；生产走默认装配）。
func (s *AssetBundleService) SetPlatformClientForBundleService(c platformAssetClient) {
	s.platformClient = c
}

// contributorClient 取生效的平台客户端：注入优先，缺省为真实 ContributorClient。
func (s *AssetBundleService) contributorClient() platformAssetClient {
	if s.platformClient != nil {
		return s.platformClient
	}
	return platform.NewContributorClient()
}

// submitBundleLocks 同一资产包的提交流程在进程内串行（FA-07 并发提交防重复创建）：
// 首次提交「创建资产→落 platform_asset_id」是 check-then-act，两个并发提交会各建一个
// 平台资产。进程级 per-asset 锁把窗口关掉（多实例部署的跨实例窗口由幂等复用分支兜底）。
var submitBundleLocks sync.Map

func lockBundleSubmit(assetID string) func() {
	v, _ := submitBundleLocks.LoadOrStore(assetID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// SubmitToPlatform 将本地调试好的资产包提交到平台端审核上架（开发者上架链路）。
// 数据流：user-server -> platform-server(contributor-api)，符合资产包三端职责（开发者产出上交平台）。
//
// 幂等语义（v3.94.0）：首次提交创建平台资产并把 platform_asset_id 落库；
// 之后重复提交复用既有平台资产——
//   - approved：已上架，直接回显状态，不再重提（平台会拒绝 approved 重提）；
//   - pending：审核中，直接回显状态；
//   - draft/rejected/offline：先把最新内容推成新版本，再重新进入审核。
//
// 此前每次提交都 CreateAsset 新建平台资产且 ID 不落库：连点/重试会制造多个
// pending 资产，审核状态也没有回程。
func (s *AssetBundleService) SubmitToPlatform(ctx context.Context, assetID string) (int64, error) {
	// 平台集成是可选组件：关态下既不查库也不建 contributor 客户端，
	// 更不会因为"点了上架"而在日志里留下平台连接失败的 Error。
	if !config.PlatformEnabled() {
		return 0, fmt.Errorf("%w: 平台集成未启用（设 PLATFORM_ENABLED=true 并配好本地 platform-server 后可用）",
			platform.ErrPlatformNotConfigured)
	}
	// FA-07：同一资产包提交进程内串行，关掉「创建资产→落 ID」的并发窗口。
	unlock := lockBundleSubmit(assetID)
	defer unlock()

	bundle, err := s.GetBundleByAssetID(ctx, assetID)
	if err != nil {
		return 0, err
	}
	if err := ValidateBundleForPlatformSubmit(bundle); err != nil {
		return 0, err
	}
	cc := s.contributorClient()
	platformAssetID := bundle.PlatformAssetID

	if platformAssetID > 0 {
		// 幂等复用分支：先看平台当前审核状态再决定动作。
		// FA-07：状态获取失败不再盲目 fallthrough 重提——平台状态未知时送审
		// 可能审的是旧内容（push 也会因同样网络问题失败）或撞 approved 重提拒绝，
		// 返回可对账错误让调用方走 RefreshPlatformStatus。
		info, derr := cc.GetAssetDetail(platformAssetID)
		if derr != nil {
			return platformAssetID, fmt.Errorf("平台资产 %d 状态获取失败，本次未送审（请稍后用刷新状态对账后重试）：%w",
				platformAssetID, derr)
		}
		switch info.Status {
		case "approved":
			_ = s.repo.UpdatePlatformRef(ctx, bundle.ID, platformAssetID, info.Status, info.ReviewNote)
			return platformAssetID, nil
		case "pending":
			_ = s.repo.UpdatePlatformRef(ctx, bundle.ID, platformAssetID, info.Status, info.ReviewNote)
			return platformAssetID, nil
		default:
			// draft/rejected/offline/未知：推最新内容成新版本后重提。
			// FA-07：推送失败必须终止——送审旧稿=审核内容与展示内容不一致。
			if uerr := s.pushPlatformVersion(cc, platformAssetID, bundle); uerr != nil {
				return platformAssetID, fmt.Errorf("最新内容推送到平台失败，本次未送审（审核稿必须与最新内容一致）：%w", uerr)
			}
		}
	} else {
		platformAssetID, err = s.createPlatformAsset(cc, bundle)
		if err != nil {
			return 0, err
		}
		// 创建成功但后续提交失败时也要落 ID：重试才能走幂等复用分支。
		// FA-07：ID 落库失败必须返回可对账状态——平台资产已存在而本地不知道，
		// 盲目重试会重复创建平台资产。
		if err := s.repo.UpdatePlatformRef(ctx, bundle.ID, platformAssetID, "draft", ""); err != nil {
			return platformAssetID, fmt.Errorf("平台资产 %d 已创建但本地关联落库失败（重试前请先刷新状态核对，避免重复创建）：%w",
				platformAssetID, err)
		}
	}

	if err := cc.SubmitAudit(platformAssetID); err != nil {
		return platformAssetID, err
	}
	// 提交后拉一次审核状态落库（回程第一跳；后续由 refresh 接口/再次提交更新）
	status, note := "pending", ""
	if info, derr := cc.GetAssetDetail(platformAssetID); derr == nil {
		status, note = info.Status, info.ReviewNote
	}
	if err := s.repo.UpdatePlatformRef(ctx, bundle.ID, platformAssetID, status, note); err != nil {
		logger.Warnf("[asset_bundle] platform ref persist failed asset=%s platform=%d err=%v", assetID, platformAssetID, err)
	}
	return platformAssetID, nil
}

// RefreshPlatformStatus 从平台拉取审核状态并落库（审核结果回程）。
// 平台审核通过/驳回不会回调 user-server，本地以拉取方式闭环。
func (s *AssetBundleService) RefreshPlatformStatus(ctx context.Context, id int64) (*model.AssetBundle, error) {
	if !config.PlatformEnabled() {
		return nil, fmt.Errorf("%w: 平台集成未启用", platform.ErrPlatformNotConfigured)
	}
	bundle, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if bundle.PlatformAssetID <= 0 {
		return nil, ErrBundleNotSubmitted
	}
	info, err := s.contributorClient().GetAssetDetail(bundle.PlatformAssetID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdatePlatformRef(ctx, bundle.ID, bundle.PlatformAssetID, info.Status, info.ReviewNote); err != nil {
		logger.Warnf("[asset_bundle] platform ref persist failed asset=%s err=%v", bundle.AssetID, err)
	}
	return s.repo.FindByID(ctx, id)
}

// createPlatformAsset 首次上送：把 messages 打包创建平台资产
func (s *AssetBundleService) createPlatformAsset(cc platformAssetClient, bundle *model.AssetBundle) (int64, error) {
	clean := make([]map[string]string, 0, len(bundle.Messages))
	for _, m := range bundle.Messages {
		clean = append(clean, map[string]string{"role": m.Role, "content": m.Content})
	}
	data, err := json.Marshal(clean)
	if err != nil {
		return 0, err
	}
	return cc.CreateAsset(platform.CreateAssetPayload{
		AssetType:   "bundle",
		Industry:    bundle.Industry,
		Name:        bundle.Title,
		Description: bundle.Description,
		Version:     bundle.Version,
		Changelog:   "from merchant bundle " + bundle.AssetID,
		Data:        data,
	})
}

// pushPlatformVersion 内容有改动时把最新 messages 推成平台资产的新版本
func (s *AssetBundleService) pushPlatformVersion(cc platformAssetClient, platformAssetID int64, bundle *model.AssetBundle) error {
	clean := make([]map[string]string, 0, len(bundle.Messages))
	for _, m := range bundle.Messages {
		clean = append(clean, map[string]string{"role": m.Role, "content": m.Content})
	}
	data, err := json.Marshal(clean)
	if err != nil {
		return err
	}
	return cc.UploadVersion(platformAssetID, bundle.Version, "resubmit from "+bundle.AssetID, data)
}
