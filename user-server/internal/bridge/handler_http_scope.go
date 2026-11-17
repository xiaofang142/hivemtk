package bridge

import "errors"

// validateHTTPIngestScope 核对整批后才补齐字段，冲突批次不会先处理合法项。
// 空顶层会话保留同账号多会话批次；历史沿用其父消息的有效范围。
func validateHTTPIngestScope(req *HTTPIngestRequest, channel, accountID, conversationID string) error {
	if req == nil || channel == "" || accountID == "" {
		return errors.New("ingest scope required")
	}
	channel = NormalizeBridgeChannel(channel)
	if req.Channel != "" && NormalizeBridgeChannel(req.Channel) != channel {
		return errors.New("ingest channel scope mismatch")
	}
	if req.AccountID != "" && req.AccountID != accountID {
		return errors.New("ingest account scope mismatch")
	}
	if conversationID != "" && req.ConversationID != "" && req.ConversationID != conversationID {
		return errors.New("ingest conversation scope mismatch")
	}
	for _, message := range req.Messages {
		if message == nil {
			continue
		}
		if message.Channel != "" && NormalizeBridgeChannel(message.Channel) != channel {
			return errors.New("ingest message channel scope mismatch")
		}
		if message.AccountID != "" && message.AccountID != accountID {
			return errors.New("ingest message account scope mismatch")
		}
		if conversationID != "" && message.ConversationID != "" && message.ConversationID != conversationID {
			return errors.New("ingest message conversation scope mismatch")
		}
	}
	req.Channel, req.AccountID, req.ConversationID = channel, accountID, conversationID
	for _, message := range req.Messages {
		if message == nil {
			continue
		}
		message.Channel, message.AccountID = channel, accountID
		if message.ConversationID == "" {
			message.ConversationID = conversationID
		}
	}
	return nil
}
