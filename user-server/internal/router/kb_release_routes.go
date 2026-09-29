// kb_release_routes.go T-P9-02：G-5 知识库变更流程的路由挂载点。
//
// 单独一个文件的理由与 bad_case_routes.go 同一条：让"这套读写口挂在哪个组下"只有一处可查。
// 发布制如果有第二组写入口（例如某个工具直接调 Publish），就会出现两条"这一版上线了"的
// 留痕口径，而这条链的下游是"线上到底在检索哪一版"这个问题唯一的答案来源。
package router

import (
	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/pkg/kbrelease"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"
)

// setupKBReleaseRoutes 在**已鉴权**的 /api 组下挂变更与发布两组端点。
//
// 必须挂在 auth 组内：提交/撤回/发布/回滚/放回/启停治理六个写入口都要把操作者落到具体的人
// （requested_by 与 changed_by 是 AC③ 留痕的唯一出处），而 gin 的 Use 只对其后注册的路由
// 生效 —— 挂早了就是匿名可写，留痕里全是一句空串的"谁改的"。
//
// 服务实例取自全局登记处（装配点 app.InitKBReleaseRuntime）。取到 nil 不是错误：路由照挂，
// 十五条端点里除 /kb-releases/gate（档位是进程事实，自己分支回数据）与
// /kb-changes/taxonomy（值域是 model 包常量）之外，每个请求回 503 ——
// "端点在、底座不在"必须与"这个功能没做"可区分。
func setupKBReleaseRoutes(auth *gin.RouterGroup) {
	svc := service.GlobalKBReleaseService()
	controller.NewKBReleaseController(svc).RegisterRoutes(auth)
	logger.Infof("[Router] kb_release 变更/发布 API 已连通（底座可用=%v，闸门档位=%s/%s）",
		svc.Available(), kbrelease.ModeForLog(), kbrelease.FlagEnv)
	if !kbrelease.ShadowsOrOn() {
		logger.Infof("[Router] kb_release 当前档位不改任何召回结果，且导入内容不打版本戳 ⇒ 变更流程可提交可发布，" +
			"但\"未发布不可见\"只对走变更通路的内容成立；观察请置 shadow，真拦须显式写 on")
	}
}
