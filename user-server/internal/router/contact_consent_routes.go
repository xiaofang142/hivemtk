package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// setupContactConsentRoutes D04：客户授权时间线与偏好管理口。
// 同意事实账只追加：授权/撤回都是新事件行，actor 取登录上下文。
func setupContactConsentRoutes(auth *gin.RouterGroup, db *gorm.DB) {
	svc := service.NewContactConsentService(repository.NewContactConsentRepository(db))
	ctrl := controller.NewContactConsentController(svc)

	auth.GET("/contacts/:customer_id/consents", ctrl.Timeline)
	auth.POST("/contacts/:customer_id/consents", ctrl.Record)
	auth.GET("/contacts/:customer_id/preferences", ctrl.ListPreferences)
	auth.PUT("/contacts/:customer_id/preferences", ctrl.SetPreference)
}
