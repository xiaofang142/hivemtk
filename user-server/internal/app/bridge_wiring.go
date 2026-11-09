package app

import (
	"context"
	"time"

	"hivemtk-user/internal/bridge"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// WireBridgeGlobals 桥接全局注册装配（MD-04 S3 自 router.Setup 迁入）。
// 两段闭包体自 router.go 逐字节随迁：
//   - R-B1：msg_id=service.ContentHashMsgID 与落库行同源，扩展端复合去重键
//     msg_id|conversation_id 依赖它；用 PublishCtx 而非 Publish——认领那一步要
//     UPDATE message_hub（租户表），on 模式下 ctx 不带租户会被隔离守卫 fail-closed 拒绝。
//   - 归属判据：user_id=0 是租户资产（HTTP 登记入口只认实例级桥接凭证，没有用户身份），
//     对租户内用户可见；非零是显式认领，只对本人放行。
//
// ingressSvc 由 router.Setup 的唯一生产构造点显式传入：本函数不构造
// InboxIngressService，也不依赖 Router 初始化副作用。
func WireBridgeGlobals(gormDB *gorm.DB, ingressSvc *service.InboxIngressService) *bridge.BridgeIngestHandler {
	SetBridgeIngressSvc(ingressSvc)
	bridgeHandler := bridge.NewBridgeIngestHandler(ingressSvc)

	messageHubRepo := repository.NewMessageHubRepositoryWithDB(gormDB)
	bridgeHandler.SetOutboxQuerier(messageHubRepo)

	service.SetGlobalSSEPublisher(func(ctx context.Context, channel, accountID string, hubID uint64, convID, msgType, receiverID, content, mediaURL string, isAIReply bool, createdAt time.Time) {
		// R-B1：总线 Data 与 DB 补拉路径逐键一致（msg_id 必带——扩展端复合去重键
		// msg_id|conversation_id 依赖它；曾缺 msg_id 致同会话第二条起静默丢消息）。
		// msg_id 与落库行同源：service.ContentHashMsgID ≡ DeliverBridgeOutbound 赋值处。
		// 用 PublishCtx 而非 Publish：认领那一步要 UPDATE message_hub（租户表），
		// on 模式下 ctx 不带租户会被隔离守卫 fail-closed 拒绝
		// （"[SSE] 出站认领报错，放弃本次推送"）。ctx 由 service 侧派生，
		// 已带租户归属。
		bridge.GlobalSSEBus.PublishCtx(ctx, bridge.BuildOutboundSSEEvent(bridge.OutboundEventData{
			HubID:          hubID,
			MsgID:          service.ContentHashMsgID(channel, convID, content),
			Platform:       channel,
			AccountID:      accountID,
			ConversationID: convID,
			Content:        content,
			MsgType:        msgType,
			MediaURL:       mediaURL,
			ReceiverID:     receiverID,
			IsAIReply:      isAIReply,
			CreatedAt:      createdAt,
		}))
	})

	bridgeRepo := bridge.NewBridgeAccountRepository(gormDB)
	bridge.RegisterBridgeAccountRepo(bridgeRepo)
	// 归属判据：user_id=0 是租户资产（HTTP 登记入口只认实例级桥接凭证，没有用户身份），
	// 对租户内用户可见；非零是显式认领，只对本人放行。查无此行（含被隔离 callback
	// 挡掉的跨租户行）⇒ 不归当前用户，不能当成"尚未注册所以放行"。
	bridge.RegisterOwnershipChecker(func(ctx context.Context, userID uint, channel, accountID string) (bool, uint, error) {
		acc, err := bridgeRepo.GetByChannelAccount(ctx, channel, accountID)
		if err != nil {
			return false, 0, err
		}
		if acc == nil {
			return false, 0, nil
		}
		return acc.UserID == 0 || acc.UserID == userID, acc.TenantID, nil
	})

	return bridgeHandler
}
