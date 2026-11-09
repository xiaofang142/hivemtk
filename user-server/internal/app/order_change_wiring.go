// order_change_wiring.go TB-05：改单申请单装配。
//
// 调用方：cmd/api/main.go（db 就绪后、router.Setup 前沿 InitIntentChainRuntime
// 同款次序）；sync.Once 幂等；db=nil 不装配（工具不注册、HTTP 面 503）。
package app

import (
	"sync"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

var (
	orderChangeOnce sync.Once
	orderChangeSvc  *service.OrderChangeService
)

// InitOrderChangeRuntime 装配改单申请单服务（幂等；db=nil 不装配）。
func InitOrderChangeRuntime(gdb *gorm.DB) *service.OrderChangeService {
	orderChangeOnce.Do(func() {
		if gdb == nil {
			logger.Warn("[order_change] ⚠️ db 未装配，改单申请单域不启用（order.modify_request 工具不注册）")
			return
		}
		orderChangeSvc = service.NewOrderChangeService(
			repository.NewOrderChangeRequestRepositoryWithDB(gdb),
			repository.NewExternalOrderRepository(),
		)
		service.SetOrderChangeService(orderChangeSvc)
		logger.Info("[order_change] ✅ 改单申请单服务已装配（TB-05，capability 路由冻结）")
	})
	return orderChangeSvc
}

// GetOrderChangeService 取实例（未装配返回 nil）。
func GetOrderChangeService() *service.OrderChangeService {
	return orderChangeSvc
}
