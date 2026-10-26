// message_hub_recall_wiring.go B2：把"撤回怎么出网"接到 /api/message-hub/:id/recall。
//
// 只做一件事：把有没有 DB 句柄翻译成"端口装没装"。
//
//   - 有句柄 ⇒ 交回 IntegrationReachAdapter（它已实现 RecallHub，含桥接渠道/窗口/
//     三家平台分派与业务码判定）；
//   - 无句柄 ⇒ 交回 **nil 接口**，让 MessageHubService 回 503。这里不能交回一个
//     db 为空的 adapter：那个 adapter 会报"服务未配置"，接口层读起来是 502
//     "撤回失败"，而事实是这套能力根本没装配 —— 两种事实必须能分开。
//
// 注意返回的是接口类型：直接 `return nil` 而不返回带类型的 nil 指针，
// 否则 `s.recaller == nil` 判不出来（经典 typed-nil 陷阱），端点会以为自己装了端口。
package app

import (
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

func NewMessageHubReachRecaller(db *gorm.DB) service.ReachRecaller {
	if db == nil {
		return nil
	}
	return NewIntegrationReachAdapterFromDB(db)
}
