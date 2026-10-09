package app

import (
	"hivemtk-user/internal/pkg/sla"
)

// InitSLO 初始化 SLO 跟踪器并登记三条 SLI。
// 定义与全局实例位于 internal/pkg/sla（service 层的记录点需要直接引用，
// 而 service 不能反向 import 本装配包），此处仅作启动期显式初始化入口。
func InitSLO() *sla.SLOTracker {
	return sla.Default()
}

// GetSLOTracker 获取全局 SLO 跟踪器。
func GetSLOTracker() *sla.SLOTracker {
	return sla.Default()
}
