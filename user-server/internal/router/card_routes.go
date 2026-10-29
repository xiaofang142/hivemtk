package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/service"
	cardsvc "hivemtk-user/internal/service/cards"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/repository"
)

func setupCardRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {

	douyinCardCtrl := controller.NewDouyinCardController(cardsvc.NewDouyinCardService(gormDB))
	auth.GET("/douyin-card/list", douyinCardCtrl.GetList)
	auth.POST("/douyin-card", middleware.RequirePermission("cards.write"), douyinCardCtrl.Create)
	auth.PUT("/douyin-card/:id", middleware.RequirePermission("cards.write"), douyinCardCtrl.Update)
	auth.DELETE("/douyin-card/:id", middleware.RequirePermission("cards.delete"), douyinCardCtrl.Delete)
	auth.GET("/douyin-card/:id", douyinCardCtrl.GetByID)
	auth.GET("/douyin/list", douyinCardCtrl.GetList)
	auth.GET("/douyin/:id", douyinCardCtrl.GetByID)
	auth.POST("/douyin/create", middleware.RequirePermission("cards.write"), douyinCardCtrl.Create)
	auth.PUT("/douyin/update", middleware.RequirePermission("cards.write"), douyinCardCtrl.Update)
	auth.DELETE("/douyin/delete/:id", middleware.RequirePermission("cards.delete"), douyinCardCtrl.Delete)
	auth.GET("/douyin/view/:id", douyinCardCtrl.GetByID)
	auth.POST("/douyin/:id/generate-short-link", middleware.RequirePermission("cards.write"), douyinCardCtrl.GenerateShortLink)

	kuaishouCardCtrl := controller.NewKuaishouCardController(cardsvc.NewKuaishouCardService(gormDB))
	auth.GET("/kuaishou-card/list", kuaishouCardCtrl.GetList)
	auth.POST("/kuaishou-card", middleware.RequirePermission("cards.write"), kuaishouCardCtrl.Create)
	auth.PUT("/kuaishou-card/:id", middleware.RequirePermission("cards.write"), kuaishouCardCtrl.Update)
	auth.DELETE("/kuaishou-card/:id", middleware.RequirePermission("cards.delete"), kuaishouCardCtrl.Delete)
	auth.GET("/kuaishou-card/:id", kuaishouCardCtrl.GetByID)
	auth.GET("/kuaishou/list", kuaishouCardCtrl.GetList)
	auth.GET("/kuaishou/:id", kuaishouCardCtrl.GetByID)
	auth.POST("/kuaishou/create", middleware.RequirePermission("cards.write"), kuaishouCardCtrl.Create)
	auth.PUT("/kuaishou/update", middleware.RequirePermission("cards.write"), kuaishouCardCtrl.Update)
	auth.DELETE("/kuaishou/delete/:id", middleware.RequirePermission("cards.delete"), kuaishouCardCtrl.Delete)
	auth.GET("/kuaishou/view/:id", kuaishouCardCtrl.GetByID)
	auth.POST("/kuaishou/like/:id", kuaishouCardCtrl.LikeCard)
	auth.POST("/kuaishou/share/:id", kuaishouCardCtrl.ShareCard)
	auth.POST("/kuaishou/:id/generate-short-link", middleware.RequirePermission("cards.write"), kuaishouCardCtrl.GenerateShortLink)

	xiaohongshuCardCtrl := controller.NewXiaohongshuCardController(cardsvc.NewXiaohongshuCardService(repository.NewXiaohongshuCardRepository(gormDB), cardsvc.NewShortLinkService(repository.NewShortLinkRepository(gormDB), repository.NewDomainPoolRepository(gormDB), repository.NewShortLinkAccessRepository(gormDB)), service.NewDomainPoolService(repository.NewDomainPoolRepository(gormDB), repository.NewDomainBlacklistRepository(gormDB))))
	auth.GET("/xiaohongshu-card/list", xiaohongshuCardCtrl.GetList)
	auth.POST("/xiaohongshu-card", xiaohongshuCardCtrl.Create)
	auth.PUT("/xiaohongshu-card/:id", xiaohongshuCardCtrl.Update)
	auth.DELETE("/xiaohongshu-card/:id", xiaohongshuCardCtrl.Delete)
	auth.GET("/xiaohongshu-card/:id", xiaohongshuCardCtrl.GetByID)
	auth.GET("/xiaohongshu/list", xiaohongshuCardCtrl.GetList)
	auth.GET("/xiaohongshu/:id", xiaohongshuCardCtrl.GetByID)
	auth.POST("/xiaohongshu/create", xiaohongshuCardCtrl.Create)
	auth.PUT("/xiaohongshu/update", xiaohongshuCardCtrl.Update)
	auth.DELETE("/xiaohongshu/delete/:id", xiaohongshuCardCtrl.Delete)
	auth.GET("/xiaohongshu/view/:id", xiaohongshuCardCtrl.GetByID)
	auth.POST("/xiaohongshu/:id/generate-short-link", xiaohongshuCardCtrl.GenerateShortLink)

	xianyuCardCtrl := controller.NewXianyuCardController(cardsvc.NewXianyuCardService(gormDB), cardsvc.NewXianyuCardStatsService(gormDB))
	auth.GET("/xianyu-card/list", xianyuCardCtrl.GetList)
	auth.POST("/xianyu-card", xianyuCardCtrl.Create)
	auth.PUT("/xianyu-card/:id", xianyuCardCtrl.Update)
	auth.DELETE("/xianyu-card/:id", xianyuCardCtrl.Delete)
	auth.GET("/xianyu-card/:id", xianyuCardCtrl.GetByID)
	auth.GET("/xianyu/list", xianyuCardCtrl.GetList)
	auth.GET("/xianyu/:id", xianyuCardCtrl.GetByID)
	auth.POST("/xianyu/create", xianyuCardCtrl.Create)
	auth.PUT("/xianyu/update", xianyuCardCtrl.Update)
	auth.DELETE("/xianyu/delete/:id", xianyuCardCtrl.Delete)
	auth.GET("/xianyu/view/:id", xianyuCardCtrl.GetByID)
	auth.POST("/xianyu/:id/generate-short-link", xianyuCardCtrl.GenerateShortLink)
}

// setupXianyuCardTrackRoutes 注册闲鱼卡片落地页的三条埋点写口。
//
// 卡片页由外部客户直接打开，没有登录态，所以这三条只能挂在公网组；鉴权不在
// 路由组上，而在 handler 内：模板渲染时按卡片 ID 签发 HMAC 凭证
// （internal/reach/card/token.go），上报必须带 X-Xianyu-Card-Token，且凭证
// 必须与 :id 绑定。未带/伪造凭证分别为 401/403，任何匿名请求都写不进库。
// 租户归属仍由 PublicIDTenant 按卡片 id 反查后绑定，统计写入落在卡片所属租户。
func setupXianyuCardTrackRoutes(public *gin.RouterGroup, gormDB *gorm.DB) {
	xianyuCardCtrl := controller.NewXianyuCardController(cardsvc.NewXianyuCardService(gormDB), cardsvc.NewXianyuCardStatsService(gormDB))
	track := public.Group("/xianyu/:id", middleware.PublicIDTenant(gormDB, "xianyu_cards"))
	track.POST("/click", xianyuCardCtrl.RecordClick)
	track.POST("/share", xianyuCardCtrl.RecordShare)
	track.POST("/view", xianyuCardCtrl.RecordView)
}

func setupCardStatsRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	douyinStatsCtrl := controller.NewDouyinCardStatsController(cardsvc.NewDouyinCardStatsService(repository.NewDouyinCardStatsRepository(gormDB)))
	auth.GET("/douyin-card/stats/:id", douyinStatsCtrl.GetCardStats)
	auth.GET("/douyin-card/overall-stats", douyinStatsCtrl.GetOverallStats)
	auth.GET("/douyin/stats/card/:id", douyinStatsCtrl.GetCardStats)
	auth.GET("/douyin/stats/overall", douyinStatsCtrl.GetOverallStats)

	kuaishouStatsCtrl := controller.NewKuaishouCardStatsController(cardsvc.NewKuaishouCardStatsService(repository.NewKuaishouCardStatsRepository(gormDB)))
	auth.GET("/kuaishou-card/stats/:id", kuaishouStatsCtrl.GetCardStats)
	auth.GET("/kuaishou-card/overall-stats", kuaishouStatsCtrl.GetOverallStats)
	auth.GET("/kuaishou/stats/card/:id", kuaishouStatsCtrl.GetCardStats)
	auth.GET("/kuaishou/stats/overall", kuaishouStatsCtrl.GetOverallStats)

	xiaohongshuStatsCtrl := controller.NewXiaohongshuCardStatsController(cardsvc.NewXiaohongshuCardStatsService(repository.NewXiaohongshuCardStatsRepository(gormDB)))
	auth.GET("/xiaohongshu-card/stats/:id", xiaohongshuStatsCtrl.GetCardStats)
	auth.GET("/xiaohongshu-card/overall-stats", xiaohongshuStatsCtrl.GetOverallStats)
	auth.GET("/xiaohongshu/stats/card/:id", xiaohongshuStatsCtrl.GetCardStats)
	auth.GET("/xiaohongshu/stats/overall", xiaohongshuStatsCtrl.GetOverallStats)

	xianyuStatsCtrl := controller.NewXianyuCardStatsController(cardsvc.NewXianyuCardStatsService(gormDB))
	auth.GET("/xianyu-card/stats/:id", xianyuStatsCtrl.GetCardStats)
	auth.GET("/xianyu-card/overall-stats", xianyuStatsCtrl.GetOverallStats)
	auth.GET("/xianyu/stats/card/:id", xianyuStatsCtrl.GetCardStats)
	auth.GET("/xianyu/stats/overall", xianyuStatsCtrl.GetOverallStats)

	factory := controller.NewCardStatsFactoryController(
		service.NewPlatformDouyinCardStatsAdapter(cardsvc.NewDouyinCardStatsService(repository.NewDouyinCardStatsRepository(gormDB))),
		service.NewPlatformKuaishouCardStatsAdapter(cardsvc.NewKuaishouCardStatsService(repository.NewKuaishouCardStatsRepository(gormDB))),
		service.NewPlatformXiaohongshuCardStatsAdapter(cardsvc.NewXiaohongshuCardStatsService(repository.NewXiaohongshuCardStatsRepository(gormDB))),
		service.NewPlatformXianyuCardStatsAdapter(cardsvc.NewXianyuCardStatsService(gormDB)),
		service.NewPlatformTiktokCardStatsAdapter(cardsvc.NewTikTokCardService()),
	)
	auth.GET("/card-stats/:platform/stats/:id", factory.GetCardStats)
	auth.GET("/card-stats/:platform/overall", factory.GetOverallStats)
}
