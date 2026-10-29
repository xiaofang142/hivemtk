package cards

import (
	"context"

	"hivemtk-user/internal/dto"
)

// PlatformCardStatsService 平台卡片统计服务统一接口
//
// LM-：将抖音 / 快手 / 小红书 / 闲鱼 / TikTok 各平台卡片统计服务的差异
// （参数命名、返回结构、调用约定）收敛到统一入口。Controller / Router 通过
// platform 字段路由到具体实现，避免上层重复编写 4-5 套统计处理逻辑。
//
// 约定：
//   - GetCardStats / GetOverallStats 输入输出统一使用 dto.Platform*Request/Response；
//   - RecordActivity 接受统一的活动记录参数（cardID/userID/action/username/ip/ua）；
//   - 各平台 service 在保留自身专属方法（向后兼容）的同时，额外提供对应
//     Adapter 方法以满足本接口。
type PlatformCardStatsService interface {
	Platform() string

	GetCardStats(ctx context.Context, req *dto.PlatformCardStatsRequest) (*dto.PlatformCardStatsResponse, error)

	GetOverallStats(ctx context.Context, req *dto.PlatformCardOverallStatsRequest) (*dto.PlatformCardOverallStatsResponse, error)

	RecordActivity(ctx context.Context, cardID uint, userID uint, action string, username, ipAddress, userAgent string) error
}
