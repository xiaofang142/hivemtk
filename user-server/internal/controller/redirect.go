package controller

import (
	"context"
	"fmt"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// RedirectController 短链重定向控制器
//
// 私域部署（升级）：
//   - 抖音 / 快手 / 小红书 / 闲鱼 四个平台的卡片短链统一跳转到「卡片聊天页」
//   - 卡片聊天页包含卡片信息 + 联系客服按钮，点击按钮打开 /chat/embed/{platform}_card
//   - 不再直接 302 跳转到外部 redirect_url，避免跳出客服域
//
// 五层架构修复：所有 service 由 router 注入，controller 不再直接访问数据库
type RedirectController struct {
	shortLinkService            service.ShortLinkService
	douyinCardService           service.DouyinCardService
	kuaishouCardService         service.KuaishouCardService
	xiaohongshuCardService      service.XiaohongshuCardService
	xianyuCardService           service.XianyuCardService
	douyinCardStatsService      service.DouyinCardStatsService
	kuaishouCardStatsService    *service.KuaishouCardStatsService
	xiaohongshuCardStatsService service.XiaohongshuCardStatsService
	xianyuCardStatsService      service.XianyuCardStatsService
	tiktokCardService           service.TikTokCardService
}

// NewRedirectController 创建短链重定向控制器（service 由 router 注入）
func NewRedirectController(
	shortLinkService service.ShortLinkService,
	douyinCardService service.DouyinCardService,
	kuaishouCardService service.KuaishouCardService,
	xiaohongshuCardService service.XiaohongshuCardService,
	xianyuCardService service.XianyuCardService,
	douyinCardStatsService service.DouyinCardStatsService,
	kuaishouCardStatsService *service.KuaishouCardStatsService,
	xiaohongshuCardStatsService service.XiaohongshuCardStatsService,
	xianyuCardStatsService service.XianyuCardStatsService,
	tiktokCardService service.TikTokCardService,
) *RedirectController {
	return &RedirectController{
		shortLinkService:            shortLinkService,
		douyinCardService:           douyinCardService,
		kuaishouCardService:         kuaishouCardService,
		xiaohongshuCardService:      xiaohongshuCardService,
		xianyuCardService:           xianyuCardService,
		douyinCardStatsService:      douyinCardStatsService,
		kuaishouCardStatsService:    kuaishouCardStatsService,
		xiaohongshuCardStatsService: xiaohongshuCardStatsService,
		xianyuCardStatsService:      xianyuCardStatsService,
		tiktokCardService:           tiktokCardService,
	}
}

// RedirectShortLink 重定向短链
func (ctrl *RedirectController) RedirectShortLink(ctx *gin.Context) {
	code := ctx.Param("code")
	if code == "" {
		ctx.String(http.StatusBadRequest, "无效的短链")
		return
	}

	shortLink, err := ctrl.shortLinkService.GetByShortCode(ctx.Request.Context(), code)
	if err != nil {
		ctx.String(http.StatusNotFound, "短链不存在")
		return
	}

	if shortLink.Status == 2 {
		ctx.String(http.StatusGone, "短链已停用")
		return
	}
	if shortLink.ExpireTime != nil && shortLink.ExpireTime.Before(time.Now()) {
		ctx.String(http.StatusGone, "短链已过期")
		return
	}

	if _, err := ctrl.shortLinkService.AccessShortLink(ctx.Request.Context(), &dto.AccessShortLinkRequest{
		ShortCode: code,
		UserAgent: ctx.GetHeader("User-Agent"),
		IP:        ctx.ClientIP(),
		Referer:   ctx.GetHeader("Referer"),
	}); err != nil {
		logger.Errorf("[RedirectShortLink] 记录短链访问失败（code=%s）: %v", code, err)
	}

	baseURL := buildBaseURL(ctx)

	originalURL := shortLink.OriginalURL

	if id, ok := extractCardID(originalURL, "/douyin/card/"); ok {
		renderCardChatPage(ctx, ctrl.douyinCardService.GenerateCardChatPage, id, baseURL,
			func() { ctrl.recordCardView("douyin", id, ctx) })
		return
	}

	if id, ok := extractCardID(originalURL, "/kuaishou/card/"); ok {
		renderCardChatPage(ctx, ctrl.kuaishouCardService.GenerateCardChatPage, id, baseURL,
			func() { ctrl.recordCardView("kuaishou", id, ctx) })
		return
	}

	if id, ok := extractCardID(originalURL, "/xiaohongshu/card/"); ok {
		renderCardChatPage(ctx, ctrl.xiaohongshuCardService.GenerateCardChatPage, id, baseURL,
			func() { ctrl.recordCardView("xiaohongshu", id, ctx) })
		return
	}

	if id, ok := extractCardID(originalURL, "/xianyu/card/"); ok {
		renderCardChatPage(ctx, ctrl.xianyuCardService.GenerateCardChatPage, id, baseURL,
			func() { ctrl.recordCardView("xianyu", id, ctx) })
		return
	}

	if id, ok := extractCardID(originalURL, "/tiktok/card/"); ok {
		ctrl.renderTiktokCard(ctx, id)
		return
	}

	target := originalURL
	if u, err := url.Parse(target); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		ctx.String(http.StatusBadRequest, "非法的跳转地址")
		return
	}
	ctx.Redirect(http.StatusFound, target)
}

func (ctrl *RedirectController) recordCardView(platform string, id uint, ctx *gin.Context) {
	bg := ctx.Request.Context()
	ip := ctx.ClientIP()
	ua := ctx.GetHeader("User-Agent")
	ref := ctx.GetHeader("Referer")
	switch platform {
	case "douyin":
		if err := ctrl.douyinCardStatsService.RecordActivity(bg, id, 0, "view", "", ip, ua); err != nil {
			logger.Errorf("[recordCardView] 抖音卡片浏览上报失败(id=%d): %v", id, err)
		}
	case "kuaishou":
		if err := ctrl.kuaishouCardStatsService.RecordActivity(bg, id, "view", ip, ua, ""); err != nil {
			logger.Errorf("[recordCardView] 快手卡片浏览上报失败(id=%d): %v", id, err)
		}
	case "xiaohongshu":
		if err := ctrl.xiaohongshuCardStatsService.RecordActivity(bg, id, 0, "view", "", ip, ua); err != nil {
			logger.Errorf("[recordCardView] 小红书卡片浏览上报失败(id=%d): %v", id, err)
		}
	case "xianyu":
		if err := ctrl.xianyuCardStatsService.RecordView(bg, id, ip, ua, ref); err != nil {
			logger.Errorf("[recordCardView] 闲鱼卡片浏览上报失败(id=%d): %v", id, err)
		}
	}
}

func (ctrl *RedirectController) renderTiktokCard(ctx *gin.Context, id uint) {
	card, err := ctrl.tiktokCardService.GetCardModelByID(ctx.Request.Context(), id)
	if err != nil || card == nil {
		ctx.Header("Content-Type", "text/html; charset=utf-8")
		ctx.String(http.StatusNotFound, fallbackCardHTML())
		return
	}

	if err := ctrl.tiktokCardService.RecordView(ctx.Request.Context(), id, ctx.ClientIP(), ctx.GetHeader("User-Agent")); err != nil {
		logger.Errorf("[renderTiktokCard] TikTok 卡片浏览上报失败(id=%d): %v", id, err)
	}

	target := card.RedirectURL
	if target == "" {
		target = "https://www.tiktok.com"
	}
	if u, err := url.Parse(target); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		ctx.String(http.StatusBadRequest, "非法的跳转地址")
		return
	}
	ctx.Redirect(http.StatusFound, target)
}

type cardChatPageGenerator func(ctx context.Context, id uint, baseURL string) (string, error)

func renderCardChatPage(ctx *gin.Context, gen cardChatPageGenerator, id uint, baseURL string, onSuccess ...func()) {
	html, err := gen(ctx.Request.Context(), id, baseURL)
	if err != nil {
		ctx.Header("Content-Type", "text/html; charset=utf-8")
		ctx.String(http.StatusNotFound, fallbackCardHTML())
		return
	}
	if len(onSuccess) > 0 && onSuccess[0] != nil {
		onSuccess[0]()
	}
	ctx.Header("Content-Type", "text/html; charset=utf-8")
	ctx.String(http.StatusOK, html)
}

func extractCardID(originalURL, pathPrefix string) (uint, bool) {
	if !strings.Contains(originalURL, pathPrefix) {
		return 0, false
	}
	idStr := strings.Replace(originalURL, pathPrefix, "", 1)
	if idx := strings.IndexAny(idStr, "?#/"); idx >= 0 {
		idStr = idStr[:idx]
	}
	var id uint
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil || id == 0 {
		return 0, false
	}
	return id, true
}

func buildBaseURL(ctx *gin.Context) string {
	scheme := ctx.GetHeader("X-Forwarded-Proto")
	if scheme == "" {
		if ctx.Request.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := ctx.GetHeader("Host")
	if host == "" {
		host = ctx.Request.Host
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

func fallbackCardHTML() string {
	return `<!DOCTYPE html><html lang="zh-CN"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1.0">
<title>卡片不存在</title>
<style>body{font-family:-apple-system,sans-serif;background:#f5f5f7;color:#6e6e73;text-align:center;padding:60px 20px;}
h1{font-size:18px;margin-bottom:8px;color:#1d1d1f;}p{font-size:14px;}</style></head>
<body><h1>卡片不存在或已下线</h1><p>请联系客服获取最新链接</p></body></html>`
}
