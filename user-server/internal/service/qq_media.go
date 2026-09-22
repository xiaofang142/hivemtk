package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"hivemtk-user/internal/channelbot/qq"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/httpclient"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

// QQ 机器人入站媒体（M-01）：官方消息事件把富媒体放在 d.attachments[]，
// 每项自带可直接 GET 的 url（image/jpeg|image/png|image/gif|video/mp4|voice|file 六类，
// 见 bot.q.qq.com/wiki/develop/api-v2/autogen/event/group_message_create.html）。
// QQ 没有「媒体 ID + 换取下载链接」这一步，但 url 是 CDN 临时链接且官方未公布有效期
// ⇒ 与钉钉 downloadCode 同口径：入站当场转存，不能等工作台点开时再取。

var (
	qqMediaFetchFn = FetchQQAttachment
	qqMediaStoreFn = channelMediaPersist
)

// qqSeamMu 守住下面两个全局的每一次读和每一次写（accessor 在本文件末尾，包内其余位置直读 0 处）。
// 理由同 telegram_media.go 的 tgSeamMu：转存在协程里调默认实现 FetchQQAttachment，而它读的就是
// 这两个全局 —— 协程前快照 seam 函数挡不住函数体里的第二跳。
var (
	qqSeamMu sync.RWMutex

	// qqAttachmentURLGuard 附件链接的取址策略。默认拒绝非 http(s) 与内网/环回字面量：
	// url 来自事件报文，验签只保证「出自 QQ」，不能把机器人回调端点变成内网探针。
	//
	// 策略单独成一个可换点有两个理由：入站用例要在本机起 http 服务验证真实字节链路（环回地址会被
	// 默认策略挡掉，那样下载腿永远没有正向证据）；以及若某环境要收紧成 QQ CDN 白名单，
	// 替换这一点即可，不必动下载代码。
	qqAttachmentURLGuard = rejectInternalAttachmentURL

	// qqMaxMediaBytes 入站附件字节上限。默认取全站口径，单独成一个变量而不是直接写常量，
	// 是为了让「读残」这条判定可测：真去构造 64MB 响应没人愿意跑。
	qqMaxMediaBytes int64 = maxInboundMediaBytes
)

func loadQQAttachmentURLGuard() func(string) error {
	qqSeamMu.RLock()
	defer qqSeamMu.RUnlock()
	return qqAttachmentURLGuard
}

func loadQQMaxMediaBytes() int64 {
	qqSeamMu.RLock()
	defer qqSeamMu.RUnlock()
	return qqMaxMediaBytes
}

func rejectInternalAttachmentURL(rawURL string) error {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return fmt.Errorf("qq attachment url scheme rejected")
	}
	if isLocalURL(rawURL) {
		return fmt.Errorf("qq attachment url points to loopback")
	}
	return nil
}

// FetchQQAttachment 直接 GET 事件携带的附件链接。
//
// 不带任何鉴权头是官方口径（链接本身可公开访问）；加 QQBot token 反而会被 CDN 拒。
func FetchQQAttachment(ctx context.Context, rawURL string) ([]byte, string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, "", fmt.Errorf("qq attachment url empty")
	}
	// 取一次策略再调用：锁只护"读到的是哪个函数"，不在持锁期间跑策略本身（RLock 对挂起的写
	// 不可重入，边持锁边调用可换出去的东西就是自锁的口子）。
	guard := loadQQAttachmentURLGuard()
	if err := guard(trimmed); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimmed, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("qq attachment download status %d", resp.StatusCode)
	}
	// 多读 1 字节用于判定「恰好被截断」：LimitReader 静默截断会把半张图片当完整件转存，
	// 比不存更难发现。取一次上限而不是读三遍，否则同一次下载内部可能用两个不同的上限。
	limit := loadQQMaxMediaBytes()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", fmt.Errorf("read qq attachment: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, "", fmt.Errorf("qq attachment larger than %d bytes", limit)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// persistQQMediaAsync 逐个附件转存并回填本行 message_hub：media_url 放首个，全量进
// Extra.media_urls（官方一条消息可带多个附件，只存首个等于把后面的图永久丢掉）。
// 失败仅告警：占位符正文已入库，媒体不可用不该拖注入站链路。
func (s *WebhookService) persistQQMediaAsync(ctx context.Context, accountID, hubMsgID string, atts []qq.Attachment) {
	if len(atts) == 0 || hubMsgID == "" {
		return
	}
	// 这里是队列路径（Receive → queue → worker → handleJob → dispatchQQ），ctx 是服务生命周期
	// ctx 而不是请求 ctx，故 SafeGo 足够；钉钉/公众号那两条同步链路才必须用 SafeGoDetached。
	// 包级注入点进协程前先快照成本地值：上一条用例残留的协程若直读包级变量，会和下一条用例装替身的写撞成 DATA RACE。
	maxBytes, mediaFetchFn, mediaStoreFn := loadQQMaxMediaBytes(), qqMediaFetchFn, qqMediaStoreFn
	utils.SafeGo(ctx, "qq.media_persist", func(gctx context.Context) {
		urls := make([]string, 0, len(atts))
		for i, att := range atts {
			if int64(att.Size) > maxBytes {
				logger.Ctx(gctx).Warn().Str("msg_id", hubMsgID).Int("size", att.Size).
					Msg("[QQ] 附件超过入站媒体上限，跳过转存")
				continue
			}
			data, contentType, ferr := mediaFetchFn(gctx, att.URL)
			if ferr != nil {
				logger.Ctx(gctx).Warn().Err(ferr).Str("msg_id", hubMsgID).Int("index", i).
					Msg("[QQ] 附件下载失败（占位符保留）")
				continue
			}
			// 附件序号进 mediaID 与文件名：官方允许一条消息重复挂同名文件，
			// 不带序号会让后一张覆盖前一张。
			mediaID := fmt.Sprintf("%s_%d", hubMsgID, i)
			hint := strconv.Itoa(i) + "_"
			if name := strings.TrimSpace(att.Filename); name != "" {
				hint += name
			}
			publicURL, serr := mediaStoreFn(gctx, "qq", mediaID, data, contentType, hint)
			if serr != nil {
				logger.Ctx(gctx).Warn().Err(serr).Str("media_id", mediaID).Msg("[QQ] 附件转存失败")
				continue
			}
			urls = append(urls, publicURL)
		}
		if len(urls) == 0 {
			return
		}
		s.backfillQQMedia(gctx, accountID, hubMsgID, urls)
		logger.Ctx(gctx).Info().Str("account_id", accountID).Str("msg_id", hubMsgID).
			Strs("urls", urls).Msg("[QQ] 入站媒体已转存")
	})
}

// backfillQQMedia 回填 media_url 与 Extra.media_urls。
// 不用 EnrichHubMediaURLByMsgID：那条只写单值列，多附件会在逐次写里互相覆盖。
func (s *WebhookService) backfillQQMedia(ctx context.Context, accountID, hubMsgID string, urls []string) {
	d := s.lazyDB()
	if d == nil || hubMsgID == "" {
		return
	}
	var hub model.MessageHub
	if err := d.WithContext(ctx).
		Where("platform = ? AND account_id = ? AND msg_id = ?", "qq", accountID, hubMsgID).
		First(&hub).Error; err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("msg_id", hubMsgID).
			Msg("[QQ] 媒体回填失败：找不到 hub 行")
		return
	}
	extra := hub.Extra
	if extra == nil {
		extra = model.JSONMap{}
	}
	extra["media_urls"] = urls
	if err := d.WithContext(ctx).Model(&model.MessageHub{}).
		Where("id = ?", hub.ID).
		Updates(map[string]any{"media_url": urls[0], "extra": extra}).Error; err != nil {
		logger.Ctx(ctx).Warn().Err(err).Uint("hub_id", hub.ID).Msg("[QQ] 媒体回填写库失败")
	}
}
