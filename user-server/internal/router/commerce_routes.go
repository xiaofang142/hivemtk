package router

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

// setupCommerceRoutes intentionally registers no production adapter without an approved contract.
func setupCommerceRoutes(auth *gin.RouterGroup, db *gorm.DB) {
	mountCommerceRoutes(auth, service.NewCommerceService(repository.NewCommerceRepository(db), nil))
}
func mountCommerceRoutes(auth *gin.RouterGroup, svc service.CommerceService) {
	ctrl := controller.NewCommerceController(svc)
	group := auth.Group("/v1/commerce")
	group.GET("/profiles", ctrl.Profiles)
	group.GET("/orders", ctrl.Orders)
	group.GET("/operations", ctrl.List)
	group.POST("/operations", ctrl.Propose)
	group.GET("/operations/:id", ctrl.Get)
	group.POST("/operations/:id/evidence", ctrl.Evidence)
	group.POST("/operations/:id/review", ctrl.Review)
	group.POST("/operations/:id/:verb", ctrl.Transition)
	group.PUT("/policy", ctrl.Policy)
	group.PUT("/members", ctrl.Member)
}
