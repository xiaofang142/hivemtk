package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/channelbot/telegram"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/httpclient"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

// TG 入站媒体（M-01）。
//
// 修复前的形状：TGMessage 只有 Text/Caption 两个正文字段，photo/video/voice/audio/document/
// sticker 一个都没声明 ⇒ 官方推来的媒体容器在解析阶段就被丢弃，MsgType 又写死 "text"，
// 空正文最终在 dispatch 里兜成 "[" + chatType + "]"（私聊图片落成一行 "[private]"）。
//
// 官方口径（core.telegram.org/bots/api）：媒体的 file_id 必须先经 getFile 换 file_path，
// 再按 https://api.telegram.org/file/bot<token>/<file_path> 下载；原文
// 「It is guaranteed that the link will be valid for at least 1 hour. When the link expires,
// a new one can be requested by calling getFile」+「The maximum file size to download is 20 MB」
// ⇒ 与 QQ（url 直接可 GET）不同，TG 多一跳凭证交换；file_path 过期不影响可补取，
// 但入站当场转存仍是唯一能让工作台长期看到原件的做法。

var (
	tgMediaFetchFn = FetchTelegramMedia
	tgMediaStoreFn = channelMediaPersist
)

// tgMaxMediaBytes 入站媒体字节上限，默认用官方那条 20 MB。
// 做成变量而不是常量：超限/读残两条判定要能在测试里用小数值真跑一遍（没人愿意传 20MB 夹具）。
var tgMaxMediaBytes int64 = telegram.MaxDownloadFileBytes

// tgAPIBaseOverride 换取/下载两步的接口域覆盖，仅测试用（生产留空即官方 api.telegram.org）。
//
// 为什么要有这个 seam：QQ 那条腿的下载 URL 本来就来自事件报文，测试指向环回服务是顺路的；
// TG 的 file_path 是相对路径、域由客户端自己拼 ⇒ 不给这一个口子，「账号 token → getFile →
// 下载 → 转存」整条真实 HTTP 链路就没有任何端到端证据，只剩两处桩互证（桩说它调了就绿了）。
var tgAPIBaseOverride = ""

// FetchTelegramMedia file_id → getFile → 下载。返回字节与响应 Content-Type。
//
// 凭证只出现在拼出来的 URL 里，因此三条错误串都不带 URL 原文（会进日志）。
func FetchTelegramMedia(ctx context.Context, token, fileID string) ([]byte, string, error) {
	opts := []core.ClientOption{core.WithHTTPClient(httpclient.Client)}
	if tgAPIBaseOverride != "" {
		opts = append(opts, core.WithBaseURL(tgAPIBaseOverride))
	}
	cli := telegram.NewTelegramClient(token, opts...)
	f, err := cli.GetFile(ctx, fileID)
	if err != nil {
		return nil, "", err
	}
	if tgMaxMediaBytes > 0 && f.FileSize > tgMaxMediaBytes {
		return nil, "", fmt.Errorf("tg media declared size %d exceeds limit %d", f.FileSize, tgMaxMediaBytes)
	}
	data, contentType, derr := cli.DownloadFile(ctx, f.FilePath, tgMaxMediaBytes)
	if derr != nil {
		return nil, "", derr
	}
	return data, contentType, nil
}

// persistTelegramMediaAsync 逐条媒体转存后回填本行 message_hub。
//
// TG 一条消息只带一种媒体（官方互斥），但 photo 是「同一张图的多个尺寸」⇒ 归一层已挑定最大
// 那一档，这里得到的 refs 通常只有一条；仍按列表处理，不假设长度。
// 失败仅告警：占位符正文已入库，媒体不可用不该拖注入站链路。
func (s *WebhookService) persistTelegramMediaAsync(ctx context.Context, accountID, hubMsgID string, media []telegram.TGMediaRef) {
	if len(media) == 0 || hubMsgID == "" {
		return
	}
	// 队列路径（Receive → queue → worker → handleJob → dispatchTelegram），ctx 是服务生命周期 ctx。
	utils.SafeGo(ctx, "telegram.media_persist", func(gctx context.Context) {
		token, terr := s.tgBotToken(gctx, accountID)
		if terr != nil || token == "" {
			logger.Ctx(gctx).Warn().Err(terr).Str("account_id", accountID).
				Msg("[Telegram] 媒体转存跳过：账号凭证缺失")
			return
		}
		urls := make([]string, 0, len(media))
		fileIDs := make([]string, 0, len(media))
		for i, ref := range media {
			if tgMaxMediaBytes > 0 && ref.FileSize > tgMaxMediaBytes {
				logger.Ctx(gctx).Warn().Str("msg_id", hubMsgID).Int64("size", ref.FileSize).
					Msg("[Telegram] 媒体超过官方下载上限，跳过转存")
				continue
			}
			data, contentType, ferr := tgMediaFetchFn(gctx, token, ref.FileID)
			if ferr != nil {
				logger.Ctx(gctx).Warn().Err(ferr).Str("msg_id", hubMsgID).Int("index", i).
					Msg("[Telegram] 媒体下载失败（占位符保留）")
				continue
			}
			// 序号进 mediaID 与文件名：同一条消息的多次补取、以及官方同名文件，
			// 不带序号都会在存储侧互相覆盖。
			mediaID := fmt.Sprintf("%s_%d", hubMsgID, i)
			hint := strconv.Itoa(i) + "_"
			if name := strings.TrimSpace(ref.FileName); name != "" {
				hint += name
			}
			publicURL, serr := tgMediaStoreFn(gctx, "telegram", mediaID, data, contentType, hint)
			if serr != nil {
				logger.Ctx(gctx).Warn().Err(serr).Str("media_id", mediaID).Msg("[Telegram] 媒体转存失败")
				continue
			}
			urls = append(urls, publicURL)
			fileIDs = append(fileIDs, ref.FileID)
		}
		if len(urls) == 0 {
			return
		}
		s.backfillTGMedia(gctx, accountID, hubMsgID, urls, fileIDs)
		logger.Ctx(gctx).Info().Str("account_id", accountID).Str("msg_id", hubMsgID).
			Strs("urls", urls).Msg("[Telegram] 入站媒体已转存")
	})
}

// backfillTGMedia 回填 media_url、Extra.media_urls 与 Extra.media_file_ids。
// file_id 官方说明可复用于下载（与 QQ 的 CDN 链接、WA 的 7 天 media_id 不同，TG 的 file_id
// 长期有效）⇒ 留痕后，链接失效或存储迁移时还能重新拉取原件。
func (s *WebhookService) backfillTGMedia(ctx context.Context, accountID, hubMsgID string, urls, fileIDs []string) {
	d := s.lazyDB()
	if d == nil || hubMsgID == "" {
		return
	}
	var hub model.MessageHub
	if err := d.WithContext(ctx).
		Where("platform = ? AND account_id = ? AND msg_id = ?", "telegram", accountID, hubMsgID).
		First(&hub).Error; err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("msg_id", hubMsgID).
			Msg("[Telegram] 媒体回填失败：找不到 hub 行")
		return
	}
	extra := hub.Extra
	if extra == nil {
		extra = model.JSONMap{}
	}
	extra["media_urls"] = urls
	if len(fileIDs) > 0 {
		extra["media_file_ids"] = fileIDs
	}
	if err := d.WithContext(ctx).Model(&model.MessageHub{}).
		Where("id = ?", hub.ID).
		Updates(map[string]any{"media_url": urls[0], "extra": extra}).Error; err != nil {
		logger.Ctx(ctx).Warn().Err(err).Uint("hub_id", hub.ID).Msg("[Telegram] 媒体回填写库失败")
	}
}

// tgBotToken 取账号的 bot token（getFile 与下载都要它）。
func (s *WebhookService) tgBotToken(ctx context.Context, accountID string) (string, error) {
	if s.telegramRepo == nil {
		return "", fmt.Errorf("telegram repo not wired")
	}
	accID, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || accID == 0 {
		return "", fmt.Errorf("bad account id %q", accountID)
	}
	acc, err := s.telegramRepo.GetByID(ctx, uint(accID))
	if err != nil || acc == nil {
		return "", fmt.Errorf("telegram account %d not found", accID)
	}
	return strings.TrimSpace(acc.BotToken), nil
}
