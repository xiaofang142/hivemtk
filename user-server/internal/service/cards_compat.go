package service

import (
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service/cards"
)

// 兼容别名：卡片域（douyin/kuaishou/xiaohongshu/xianyu/tiktok 卡片 + 卡片统计
// + 短链）已整体迁入 internal/service/cards（按域拆包第三刀，配方同 email 第二刀）。
// 根包内既有调用点（router/controller/测试）继续用 service.* 这一层，不改调用面。
// 依赖方向单向：cards 不 import 根 service（域名池用 DomainPoolResolver 接口接缝）。

type PlatformCardStatsService = cards.PlatformCardStatsService

type DouyinCardService = cards.DouyinCardService

type DouyinCardStatsService = cards.DouyinCardStatsService

type KuaishouCardService = cards.KuaishouCardService

type KuaishouCardStatsService = cards.KuaishouCardStatsService

type ShortLinkService = cards.ShortLinkService

type TikTokCardService = cards.TikTokCardService

type XiaohongshuCardService = cards.XiaohongshuCardService

type XiaohongshuCardStatsService = cards.XiaohongshuCardStatsService

type XianyuCardService = cards.XianyuCardService

type XianyuCardStatsService = cards.XianyuCardStatsService

func NewDouyinCardService(db any) DouyinCardService {
	return cards.NewDouyinCardService(db)
}

func NewDouyinCardStatsService(repo repository.DouyinCardStatsRepository) DouyinCardStatsService {
	return cards.NewDouyinCardStatsService(repo)
}

func NewKuaishouCardService(db any) KuaishouCardService {
	return cards.NewKuaishouCardService(db)
}

func NewKuaishouCardStatsService(repo repository.KuaishouCardStatsRepository) *KuaishouCardStatsService {
	return cards.NewKuaishouCardStatsService(repo)
}

func NewShortLinkService(shortLinkRepo repository.ShortLinkRepository, domainRepo repository.DomainPoolRepository, accessRepo repository.ShortLinkAccessRepository) ShortLinkService {
	return cards.NewShortLinkService(shortLinkRepo, domainRepo, accessRepo)
}

func NewTikTokCardService() TikTokCardService {
	return cards.NewTikTokCardService()
}

func NewXiaohongshuCardService(repo repository.XiaohongshuCardRepository, shortLinkService ShortLinkService, domainPoolService cards.DomainPoolResolver) XiaohongshuCardService {
	return cards.NewXiaohongshuCardService(repo, shortLinkService, domainPoolService)
}

func NewXiaohongshuCardStatsService(repo repository.XiaohongshuCardStatsRepository) XiaohongshuCardStatsService {
	return cards.NewXiaohongshuCardStatsService(repo)
}

func NewXianyuCardService(db any) XianyuCardService {
	return cards.NewXianyuCardService(db)
}

func NewXianyuCardStatsService(db any) XianyuCardStatsService {
	return cards.NewXianyuCardStatsService(db)
}

func NewPlatformDouyinCardStatsAdapter(inner DouyinCardStatsService) PlatformCardStatsService {
	return cards.NewPlatformDouyinCardStatsAdapter(inner)
}

func NewPlatformKuaishouCardStatsAdapter(inner *KuaishouCardStatsService) PlatformCardStatsService {
	return cards.NewPlatformKuaishouCardStatsAdapter(inner)
}

func NewPlatformXiaohongshuCardStatsAdapter(inner XiaohongshuCardStatsService) PlatformCardStatsService {
	return cards.NewPlatformXiaohongshuCardStatsAdapter(inner)
}

func NewPlatformXianyuCardStatsAdapter(inner XianyuCardStatsService) PlatformCardStatsService {
	return cards.NewPlatformXianyuCardStatsAdapter(inner)
}

func NewPlatformTiktokCardStatsAdapter(inner TikTokCardService) PlatformCardStatsService {
	return cards.NewPlatformTiktokCardStatsAdapter(inner)
}
