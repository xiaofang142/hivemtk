package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupMerchantCallbackRoutes(public *gin.RouterGroup, db *gorm.DB) {
	// No authentication protocol or provider is enabled until it is verified live.
	mountMerchantCallbackRoutes(public, service.NewMerchantCallbackService(repository.NewMerchantCallbackRepository(db), repository.NewMerchantConnectionRepository(db), nil))
}

func mountMerchantCallbackRoutes(public *gin.RouterGroup, svc service.MerchantCallbackService) {
	ctrl := controller.NewMerchantCallbackController(svc)
	public.Any(middleware.MerchantCallbackPath, ctrl.Ingest)
	// Own the entire namespace, including malformed aliases, without a Gin debug redirect.
	public.Any(middleware.MerchantCallbackPath+"/*delivery", ctrl.Ingest)
}
