package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupSelfServiceRoutes(public *gin.RouterGroup, db *gorm.DB) {
	selfSvcCtrl := controller.NewSelfServiceController(service.NewPasswordResetService(db))
	authCtrl := controller.NewAuthController()
	public.POST("/public/register", middleware.BruteForceGuard("register"), authCtrl.Register)
	public.POST("/public/forgot-password", middleware.BruteForceGuard("forgot-password"), selfSvcCtrl.ForgotPassword)
	public.POST("/public/reset-password", middleware.BruteForceGuard("reset-password"), selfSvcCtrl.ResetPassword)
}
