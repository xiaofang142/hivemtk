package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupChannelOverviewRoutes(auth *gin.RouterGroup, db *gorm.DB) {
	svc := service.NewChannelOverviewService(repository.NewChannelOverviewRepository(db))
	ov := controller.NewChannelOverviewController(svc)

	auth.GET("/channels/overview", ov.Overview)

	auth.GET("/channels/customer/:customer_id", ov.ListCustomerChannels)
}
