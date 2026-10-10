package app

import (
	"context"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/service"
)

// WireUploadBodyLimitConfigParams 把全局请求体上限接到中间件的取值口。
//
// 这条是阶段二 2a 的落点之一。背景是「上传上限有三份副本、其中两份半失效」：
//
//   - controller.MaxUploadSize（10MB）—— 代码兜底，历史上实际生效的那一层；
//   - system_config.max_upload_size_mb —— 「系统配置」页能存能显示，但上传链路一处不读；
//   - misc.upload_max_size_mb —— 2a 新增的统一入口，默认 0（不覆盖）以保持存量站点行为不变。
//
// 三者的优先级链见 controller.resolveUploadMaxSize（env > 参数中心 > 兼容列 > 兜底）。
//
// ⚠️ 只有 middleware.max_json_body_mb 走注入：它落在 internal/middleware，不能反向依赖
// service。misc.upload_max_size_mb 的读取点在 internal/controller.upload.go，
// 那里的 resolveUploadParamMaxBytes **自己**调 service.GlobalConfigParam()（直连范式，
// 与 channelgw.ws_push_interval / wecom.quota_degrade 同款），不需要这里注入；
// 它的接线正确性由 internal/controller 下的真库行为测试盯住，不在本函数的返回值里声明。
//
// ⚠️ 生效时机两者不同：body 上限在 router.Setup 里**装配期读一次**，所以种子里标了
// Restart=true（改完要重启）；上传上限每请求读，改完即时生效。
func WireUploadBodyLimitConfigParams() []string {
	cp := service.GlobalConfigParam()
	// provider 是延迟读取的，不能捕获某个请求的 ctx —— 那个 ctx 会随请求结束被取消，
	// 请求来时读配置就会全部落回兜底值。
	bg := context.Background()

	middleware.SetMaxJSONBodyMBProvider(func() int {
		return cp.GetInt(bg, "middleware", "max_json_body_mb", middleware.DefaultMaxJSONBodyMB)
	})

	return []string{"middleware.max_json_body_mb"}
}
