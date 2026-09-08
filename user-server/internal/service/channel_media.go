package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/httpclient"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/storage"

	"gorm.io/gorm"
)

// 媒体下载与转存：三渠道入站媒体（图片/语音/视频/文件）原先只落占位符文本，
// 渠道侧媒体 ID 有效期极短（WhatsApp 7 天 / 企微 3 天 / 飞书图片不过期但文件 2 天），
// 转存到统一存储后把可长期访问的 URL 回填 message_hub.media_url。

// channelMediaFollower 用 Bearer token 下载受保护媒体（WhatsApp Cloud API）。
type channelMediaFollower func(ctx context.Context, mediaID string) (io.ReadCloser, string, error)

// fetchDefaultMediaFollower 由 integration_reach_adapter 注入各渠道实现，避免 service 直依赖渠道凭证细节。
var channelMediaFollowers = map[string]channelMediaFollower{}

// RegisterChannelMediaFollower 注册渠道媒体下载器（渠道名小写：whatsapp/wecom/feishu）。
func RegisterChannelMediaFollower(channel string, f channelMediaFollower) {
	if f != nil {
		channelMediaFollowers[channel] = f
	}
}

// mediaPersistRecord 转存结果。
type mediaPersistRecord struct {
	PublicURL string
	MimeType  string
	Size      int64
}

// fetchChannelMediaFollower 取渠道下载器的内部入口（测试可注入）。
func fetchChannelMediaFollower(channel string) (channelMediaFollower, bool) {
	f, ok := channelMediaFollowers[channel]
	return f, ok
}

// PersistChannelMedia 下载渠道媒体并转存到默认存储，返回可长期访问的公开 URL。
// 存储未配置/下载失败时返回 error，调用方按需降级为占位符。
func PersistChannelMedia(ctx context.Context, channel, mediaID, filenameHint string) (*mediaPersistRecord, error) {
	if mediaID == "" {
		return nil, errors.New("media id empty")
	}
	follower, ok := fetchChannelMediaFollower(channel)
	if !ok {
		return nil, fmt.Errorf("no media follower for channel %s", channel)
	}
	rc, mediaURL, err := follower(ctx, mediaID)
	if err != nil {
		return nil, fmt.Errorf("fetch media: %w", err)
	}
	defer rc.Close()

	// 读取全部字节（媒体单文件上限 16MB（WA）/5MB(企微语音视频)，一次性读入可控）
	data, err := io.ReadAll(io.LimitReader(rc, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("read media: %w", err)
	}
	contentType := detectContentType(data, mediaURL)
	if int64(len(data)) > maxInboundMediaBytes {
		return nil, fmt.Errorf("media too large: %d bytes", len(data))
	}

	rec, err := storeInboundMedia(ctx, data, contentType, channel, mediaID, filenameHint)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// channelMediaPersist 简化转存入口：数据已在内存时直接上传（文件名 = mediaID + 推断扩展名）。
func channelMediaPersist(ctx context.Context, channel, mediaID string, data []byte, contentType, filenameHint string) (string, error) {
	rec, err := storeInboundMedia(ctx, data, contentType, channel, mediaID, filenameHint)
	if err != nil {
		return "", err
	}
	return rec.PublicURL, nil
}

// maxInboundMediaBytes 64MB 安全上限（WA 单文件 16MB、企微语音 2MB，均远小于此值）。
const maxInboundMediaBytes = 64 << 20

func storeInboundMedia(ctx context.Context, data []byte, contentType, channel, mediaID, filenameHint string) (*mediaPersistRecord, error) {
	cfgRepo := repository.NewObsConfigRepositoryWithDB(dbForStorage(ctx))
	cfg, err := cfgRepo.GetDefault(ctx)
	if err != nil || cfg == nil {
		// 兜底：无 obs_config 记录时按本地默认目录构造（与 router /files 静态目录一致）
		baseDir := os.Getenv("STORAGE_LOCAL_BASE_DIR")
		if baseDir == "" {
			baseDir = "./uploads"
		}
		cfg = &model.ObsConfig{
			Name:      "媒体转存默认本地存储",
			Provider:  model.ObsProviderLocal,
			Endpoint:  baseDir,
			Domain:    "/files",
			IsDefault: true,
			Status:    model.ObsStatusActive,
		}
	}
	driver, err := storage.Factory(cfg)
	if err != nil {
		return nil, fmt.Errorf("storage factory: %w", err)
	}

	filename := filenameHint
	if filename == "" {
		filename = mediaID
	}
	if ext := filepath.Ext(filename); ext == "" {
		filename += extFromContentType(contentType)
	}
	folder := filepath.ToSlash(filepath.Join("channels", channel, time.Now().Format("2006/01")))
	publicURL, _, err := driver.UploadReader(ctx, strings.NewReader(string(data)), int64(len(data)), folder, filename)
	if err != nil {
		return nil, fmt.Errorf("upload media: %w", err)
	}
	return &mediaPersistRecord{PublicURL: publicURL, MimeType: contentType, Size: int64(len(data))}, nil
}

// dbForStorage 媒体转存路径没有统一 ctx-carried db，退回全局 DB（与 repository.NewObsConfigRepository 同源）。
func dbForStorage(_ context.Context) *gorm.DB { return dbutil.GetDB() }

func detectContentType(data []byte, url string) string {
	if len(data) > 0 {
		if ct := http.DetectContentType(data); ct != "" && ct != "application/octet-stream" {
			return ct
		}
	}
	if url != "" {
		if ct := mime.TypeByExtension(filepath.Ext(url)); ct != "" {
			return ct
		}
	}
	return "application/octet-stream"
}

func extFromContentType(ct string) string {
	if exts, err := mime.ExtensionsByType(ct); err == nil && len(exts) > 0 {
		return exts[0]
	}
	switch {
	case strings.HasPrefix(ct, "image/"):
		return ".bin"
	default:
		return ".bin"
	}
}

// ─── 渠道媒体下载实现（注册到 channelMediaFollowers） ─────────────────

// FetchWhatsAppMedia WhatsApp Cloud API：GET /{media-id} 拿临时 URL → Bearer 下载二进制。
// media id 仅 7 天有效（官方文档），转存必须及时。
func FetchWhatsAppMedia(ctx context.Context, accessToken, phoneID, mediaID string) (io.ReadCloser, string, error) {
	if accessToken == "" || phoneID == "" {
		return nil, "", errors.New("whatsapp media credentials missing")
	}
	metaURL := fmt.Sprintf("https://graph.facebook.com/v21.0/%s?phone_number_id=%s", mediaID, phoneID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("wa media meta status %d: %s", resp.StatusCode, string(body))
	}
	var meta struct {
		URL      string `json:"url"`
		MimeType string `json:"mime_type"`
	}
	if err := json.Unmarshal(body, &meta); err != nil || meta.URL == "" {
		return nil, "", fmt.Errorf("wa media meta parse failed")
	}
	dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, meta.URL, nil)
	if err != nil {
		return nil, "", err
	}
	dlReq.Header.Set("Authorization", "Bearer "+accessToken)
	dlResp, err := httpclient.Client.Do(dlReq)
	if err != nil {
		return nil, "", err
	}
	if dlResp.StatusCode != http.StatusOK {
		dlResp.Body.Close()
		return nil, "", fmt.Errorf("wa media download status %d", dlResp.StatusCode)
	}
	ct := meta.MimeType
	if ct == "" {
		ct = dlResp.Header.Get("Content-Type")
	}
	return dlResp.Body, ct, nil
}

// FetchWeComMedia 企微：GET /cgi-bin/media/get?access_token=&media_id=。
// media_id 仅 3 天有效（官方文档）。
func FetchWeComMedia(ctx context.Context, accessToken, mediaID string) (io.ReadCloser, string, error) {
	if accessToken == "" {
		return nil, "", errors.New("wecom access token missing")
	}
	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/media/get?access_token=%s&media_id=%s", accessToken, mediaID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, "", fmt.Errorf("wecom media download status %d", resp.StatusCode)
	}
	return resp.Body, resp.Header.Get("Content-Type"), nil
}

// FetchFeishuMedia 飞书：GET /open-apis/im/v1/messages/{message_id}/resources/{file_key}?type=file|image。
func FetchFeishuMedia(ctx context.Context, tenantToken, messageID, fileKey, resType string) (io.ReadCloser, string, error) {
	if tenantToken == "" {
		return nil, "", errors.New("feishu tenant token missing")
	}
	if resType == "" {
		resType = "file"
	}
	url := fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/messages/%s/resources/%s?type=%s", messageID, fileKey, resType)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+tenantToken)
	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, "", fmt.Errorf("feishu media download status %d", resp.StatusCode)
	}
	return resp.Body, resp.Header.Get("Content-Type"), nil
}

// EnrichHubMediaURL 按 hub 行主键回填 media_url（best-effort，失败仅告警）。
func EnrichHubMediaURL(ctx context.Context, hubRepo *repository.MessageHubRepository, hubID uint, publicURL string) {
	if hubRepo == nil || hubID == 0 || publicURL == "" {
		return
	}
	if err := hubRepo.UpdateMediaURL(ctx, hubID, publicURL); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Uint("hub_id", hubID).Str("url", publicURL).
			Msg("[Media] 回填 message_hub.media_url 失败（非阻断）")
	}
}

// EnrichHubMediaURLByMsgID 按渠道消息 ID（msg_id）回填该行 media_url（best-effort）。
func EnrichHubMediaURLByMsgID(ctx context.Context, hubRepo *repository.MessageHubRepository, platform, accountID, msgID, publicURL string) {
	if hubRepo == nil || msgID == "" || publicURL == "" {
		return
	}
	hub, err := hubRepo.GetByPlatformAccountMsgID(ctx, platform, accountID, msgID)
	if err != nil || hub == nil || hub.ID == 0 {
		// 飞书/企微 dispatch 直接建 hub，msg_id 一致；查不到说明该行由 Ingress 幂等去重保留，
		// 仍按 platform+msg_id 兜底更新一次
		if updErr := hubRepo.UpdateMediaURLByMsgID(ctx, platform, msgID, publicURL); updErr != nil {
			logger.Ctx(ctx).Warn().Err(updErr).Str("msg_id", msgID).Msg("[Media] 按 msg_id 回填 media_url 失败（非阻断）")
		}
		return
	}
	EnrichHubMediaURL(ctx, hubRepo, hub.ID, publicURL)
}
