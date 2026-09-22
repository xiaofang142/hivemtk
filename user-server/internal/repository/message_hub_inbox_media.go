package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// SetInboundMediaURLs 按 (platform, account_id, conversation_id, msg_id) 四键定位入站行，
// 把转存后的媒体链接一次写全：media_url 存 urls[0]（列表页与工作台只读这一列），
// Extra.media_urls 存全量。调用方只把**转存成功**的链接放进来，所以 urls[0] 是首个成功转存的链接，
// 不是官方顺序里的第一张。
//
// 会话维度必须在键里：message_hub 的唯一键是 (platform, msg_id, conversation_id)，而
// (platform, account_id, msg_id) 不是——同一条官方消息被两个会话各上报一次就是两行，
// 少一维的 First 会把媒体写进 id 更小的那条，真正在等的行仍然留着 [图片]。
// 账号维度则是 fail-closed 的保险：唯一键不含 account_id，所以账号对不上意味着调用方的键
// 本身不一致，此时宁可判「没有这一行」漏掉一次回填，也不写进别人名下的会话行。
//
// 必须"一次写全"：一条富文本可以带多张图，逐条回填的话最后一次写会把前面的覆盖掉。
// found=false 表示没有匹配的 hub 行（入站落库与异步转存之间的竞态），此时不碰任何列；
// 读库本身的真错走 err，调用方据此和"没找到"分开报，别把读失败说成"行不存在"。
func (r *MessageHubRepository) SetInboundMediaURLs(
	ctx context.Context,
	platform, accountID, conversationID, msgID string,
	urls []string,
) (found bool, err error) {
	// 句柄缺失＝装配漏线，不是"这条消息不存在"：说成后者会把运维派去找一条没有的消息。
	if r == nil || r.db == nil {
		return false, errors.New("message hub media backfill: repository has no DB handle")
	}
	if platform == "" || accountID == "" || conversationID == "" || msgID == "" || len(urls) == 0 {
		return false, nil
	}
	var hub model.MessageHub
	if err := r.db.WithContext(ctx).
		Where("platform = ? AND account_id = ? AND conversation_id = ? AND msg_id = ?",
			platform, accountID, conversationID, msgID).
		First(&hub).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	// 合并而非整列替换：入站时落在 extra 里的键（robot_code / media_download_code / sender_nick …）
	// 必须留住，回填只新增 media_urls 一项。
	extra := mergeHubExtra(hub.Extra, map[string]any{"media_urls": urls})
	if err := r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Where("id = ?", hub.ID).
		Updates(map[string]any{"media_url": urls[0], "extra": extra}).Error; err != nil {
		return true, err
	}
	return true, nil
}
