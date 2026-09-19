// human_task_routes.go T-P3-03：统一待办的路由挂载点。
//
// 单独一个文件是为了让"这套读口挂在哪个组下"这件事只有一处可查：
// 待办中心与坐席收件箱共用同一组端点（C3 的"分离视图"分离在前端组织方式，
// 不分离在数据出口），一旦有人为第二个视图另开一组路径，两处未读数就会各自漂移。
package router

import (
	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"
)

// setupHumanTaskRoutes 在**已鉴权**的 /api 组下挂七条端点（同 setupOrderDraftRoutes 的位置约束：
// gin 的 Use 只对手之后注册的路由生效，挂早了就是匿名可访问）。
//
// 服务实例取自全局登记处（装配点 app.InitHumanTaskRuntime，在 BuildSmartOrchestrator 之前）。
// 取到 nil 不是错误：路由照挂，每个请求回 503 —— 待办读口的"关闸"就是不给它底座。
func setupHumanTaskRoutes(auth *gin.RouterGroup) {
	svc := service.GlobalHumanTaskService()
	controller.NewHumanTaskController(svc).RegisterRoutes(auth)
	if svc == nil {
		logger.Infof("[Router] human_task 路由已挂载，但未装配服务 ⇒ 全部端点回 503（检查 app.InitHumanTaskRuntime 是否在路由之前跑过）")
		return
	}
	logger.Infof("[Router] human_task 统一待办 API 已连通（底座可用=%v）", svc.Available())
}
