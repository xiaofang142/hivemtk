package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/httpclient"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// 公众号入站媒体（M-01）。
//
// 修复前的形状：service/wechat.go 的解析结构体**已经**把 MediaId/PicUrl/Format 读出来了，
// controller/wechat.go 构造 model.MessageEvent 时却只带 Content —— 三个字段原地蒸发，
// 图片/语音/视频落成一行 msg_type=text、content="" 的记录，AI 拿到的是一条空消息。
//
// 官方口径（developers.weixin.qq.com/doc/offiaccount/Message_Management/Receiving_standard_messages.html）：
// image 带 PicUrl+MediaId，voice 带 MediaId+Format，video/shortvideo 带 MediaId，
// 且「MediaId 可以调用获取临时素材接口拉取数据」；拉取接口是
// GET https://api.weixin.qq.com/cgi-bin/media/get?access_token=&media_id=
// （developers.weixin.qq.com/doc/service/api/material/temporary/api_getmedia.html）。
// 注意：官方只写明了**上传侧**临时素材保存 3 天，用户来信 MediaId 的时限与 PicUrl 的
// 有效期都没有公布 ⇒ 与钉钉同理，收到当场转存，不留到工作台点开时再取。

// wxMediaFetchFn / wxMediaStoreFn 是两个外部 IO 边界的注入点（生产指向实现本身）。
var (
	wxMediaFetchFn = FetchWeChatMedia
	wxMediaStoreFn = channelMediaPersist
)

// FetchWeChatMedia 拉取用户来信的临时素材。
// 官方：图片/语音返回字节流；**视频类 media_id 返回的是 JSON（含 video_url）**，
// 由调用方按 Content-Type 分流，不能当字节上传。
func FetchWeChatMedia(ctx context.Context, accessToken, mediaID string) (io.ReadCloser, string, error) {
	if accessToken == "" {
		return nil, "", fmt.Errorf("wechat access token missing")
	}
	if mediaID == "" {
		return nil, "", fmt.Errorf("wechat media id empty")
	}
	u := fmt.Sprintf("%s/cgi-bin/media/get?access_token=%s&media_id=%s",
		wechatAPIBase, url.QueryEscape(accessToken), url.QueryEscape(mediaID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, "", fmt.Errorf("wechat media get status %d", resp.StatusCode)
	}
	return resp.Body, resp.Header.Get("Content-Type"), nil
}

// NormalizeInbound 把官方消息体归一化成中台事件字段。
// ok=false 表示这条不该作为客户消息进收件箱（事件推送、以及解不出任何正文的畸形消息）：
// 与抖音侧 N-07 同源——非消息事件落一行空 content 的 hub，既进收件箱又驱动一次空 AI 回复。
func (m *WechatIncomingMessage) NormalizeInbound() (msgType, content string, extra map[string]any, ok bool) {
	extra = map[string]any{}
	switch m.MsgType {
	case "text":
		return model.MsgTypeText, m.Content, extra, strings.TrimSpace(m.Content) != ""
	case "image":
		putIfNonEmpty(extra, "media_id", m.MediaID)
		putIfNonEmpty(extra, "pic_url", m.PicURL)
		return model.MsgTypeImage, "[图片]", extra, true
	case "voice":
		putIfNonEmpty(extra, "media_id", m.MediaID)
		putIfNonEmpty(extra, "format", m.Format)
		return model.MsgTypeAudio, "[语音]", extra, true
	case "video", "shortvideo":
		putIfNonEmpty(extra, "media_id", m.MediaID)
		if m.MsgType == "shortvideo" {
			// 中台没有"小视频"这个类型：类型仍归 video（否则 hub 落一个词表外的
			// "shortvideo"，工作台按类型筛选与统计都筛不到），区分靠正文占位符。
			return model.MsgTypeVideo, "[小视频]", extra, true
		}
		return model.MsgTypeVideo, "[视频]", extra, true
	case "location":
		extra["latitude"] = m.LocationX
		extra["longitude"] = m.LocationY
		extra["scale"] = m.Scale
		if label := strings.TrimSpace(m.Label); label != "" {
			return model.MsgTypeLocation, "[位置] " + label, extra, true
		}
		coord := fmt.Sprintf("[位置 %s,%s]", strconv.FormatFloat(m.LocationX, 'f', -1, 64),
			strconv.FormatFloat(m.LocationY, 'f', -1, 64))
		return model.MsgTypeLocation, coord, extra, true
	case "link":
		putIfNonEmpty(extra, "url", m.URL)
		putIfNonEmpty(extra, "description", m.Description)
		if title := strings.TrimSpace(m.Title); title != "" {
			return model.MsgTypeLink, title + " " + m.URL, extra, true
		}
		return model.MsgTypeLink, m.URL, extra, m.URL != ""
	case "event":
		return "", "", nil, false
	default:
		if strings.TrimSpace(m.Content) != "" {
			return m.MsgType, m.Content, extra, true
		}
		return "", "", nil, false
	}
}

func putIfNonEmpty(dst map[string]any, key, val string) {
	if val != "" {
		dst[key] = val
	}
}

// PersistInboundMediaAsync 把用户来信的临时素材转存到统一存储，成功后按 hub 行的
// msg_id（= EventID，形如 wx-<account>-<MsgId>）回填 media_url。
// 失败仅告警：占位符正文与渠道原生 media_id 已经落库，媒体不可用不该拖注入站链路。
func (s *WechatService) PersistInboundMediaAsync(ctx context.Context, accountID uint, hubMsgID, mediaID string) {
	if s.db == nil || mediaID == "" || hubMsgID == "" {
		return
	}
	hubRepo := repository.NewMessageHubRepositoryWithDB(s.db)
	// SafeGoDetached 而不是 SafeGo：调用方 handleIncomingMessage 用的是 30s 超时 ctx 并 defer cancel()，
	// 方法一返回就取消，沿用会让转存在起跑线上被杀掉（用例传 Background() 时看不出来）。
	// 包级注入点进协程前先快照成本地值：上一条用例残留的协程若直读包级变量，会和下一条用例装替身的写撞成 DATA RACE。
	mediaStoreFn := wxMediaStoreFn
	utils.SafeGoDetached(ctx, "wechat.media_persist", 5*time.Minute, func(gctx context.Context) {
		client, err := s.getTokenClient(gctx, accountID)
		if err != nil {
			logger.Ctx(gctx).Warn().Err(err).Uint("account_id", accountID).
				Msg("[Wechat] 媒体转存跳过：取不到 accessToken")
			return
		}
		token, terr := client.getAccessToken(gctx)
		if terr != nil || token == "" {
			logger.Ctx(gctx).Warn().Err(terr).Uint("account_id", accountID).
				Msg("[Wechat] 媒体转存跳过：accessToken 获取失败")
			return
		}
		rc, contentType, ferr := wxMediaFetchFn(gctx, token, mediaID)
		if ferr != nil {
			logger.Ctx(gctx).Warn().Err(ferr).Str("media_id", mediaID).Msg("[Wechat] 媒体下载失败（占位符保留）")
			return
		}
		defer func() { _ = rc.Close() }()
		data, rerr := readInboundMedia(rc, maxInboundMediaBytes)
		if rerr != nil {
			logger.Ctx(gctx).Warn().Err(rerr).Str("media_id", mediaID).Msg("[Wechat] 媒体读取失败（占位符保留）")
			return
		}
		// 视频类 media_id 的响应是 JSON（官方返回体里有 video_url），不是字节流。
		if isJSONPayload(data, contentType) {
			var vr struct {
				VideoURL string `json:"video_url"`
				ErrCode  int    `json:"errcode"`
				ErrMsg   string `json:"errmsg"`
			}
			if jerr := json.Unmarshal(data, &vr); jerr == nil && vr.VideoURL != "" {
				EnrichHubMediaURLByMsgID(gctx, hubRepo, "wechat", strconv.FormatUint(uint64(accountID), 10), hubMsgID, vr.VideoURL)
				logger.Ctx(gctx).Info().Str("msg_id", hubMsgID).Str("media_id", mediaID).
					Msg("[Wechat] 视频媒体按官方 video_url 回填（微信侧未公布该链接有效期，故同时保留 media_id）")
				return
			}
			logger.Ctx(gctx).Warn().Int("errcode", vr.ErrCode).Str("errmsg", vr.ErrMsg).
				Str("media_id", mediaID).Msg("[Wechat] 媒体接口返回 JSON 且无 video_url，跳过转存")
			return
		}
		publicURL, serr := mediaStoreFn(gctx, "wechat", mediaID, data, contentType, "")
		if serr != nil {
			logger.Ctx(gctx).Warn().Err(serr).Str("media_id", mediaID).Msg("[Wechat] 媒体转存失败")
			return
		}
		EnrichHubMediaURLByMsgID(gctx, hubRepo, "wechat", strconv.FormatUint(uint64(accountID), 10), hubMsgID, publicURL)
		logger.Ctx(gctx).Info().Str("msg_id", hubMsgID).Str("media_id", mediaID).Str("url", publicURL).
			Msg("[Wechat] 入站媒体已转存")
	})
}

// isJSONPayload 按 Content-Type 与首字节双重判定：微信在出错时也回 JSON（errcode/errmsg），
// 只看 header 会把错误体当图片上传。
func isJSONPayload(data []byte, contentType string) bool {
	if strings.Contains(strings.ToLower(contentType), "json") {
		return true
	}
	trimmed := bytes.TrimSpace(data)
	return len(trimmed) > 0 && trimmed[0] == '{'
}
