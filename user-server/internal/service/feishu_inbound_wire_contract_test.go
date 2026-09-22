package service

// 飞书入站报文形状契约：官方 im.message.receive_v1 的 create_time 是**字符串**
// （文档示例 "create_time":"1609073151345"），而本仓结构体一度写成 int64 ——
// json.Unmarshal 直接失败，真实客户消息一条都进不了 message_hub。
// 本用例锁住两种形态都必须能解析入库。

import (
	"context"
	"fmt"
	"testing"

	"hivemtk-user/internal/model"
)

func feishuTextEventBody(msgID, createTime string) []byte {
	return []byte(`{"schema":"2.0","header":{"event_id":"ev-` + msgID + `","event_type":"im.message.receive_v1",` +
		`"create_time":` + createTime + `,"token":"v","app_id":"a","tenant_key":"tk"},` +
		`"event":{"sender":{"sender_id":{"open_id":"ou_1"},"sender_type":"user"},` +
		`"message":{"message_id":"` + msgID + `","chat_id":"oc_shape","chat_type":"p2p",` +
		`"message_type":"text","create_time":` + createTime + `,"content":"{\"text\":\"官方形状\"}"}}}`)
}

func TestFeishuInbound_OfficialPayloadShape(t *testing.T) {
	db := setupChannelFullDB(t)
	acc := &model.FeishuAccount{
		AccountName: "FS-shape", AppID: "a", AppSecret: "b",
		WebhookEnabled: true, AIAgentEnabled: false, Status: 1,
	}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("seed feishu account: %v", err)
	}
	svc := NewWebhookService(db)
	t.Cleanup(func() { svc.Stop(context.Background()) })

	cases := []struct {
		name       string
		msgID      string
		createTime string // 原始 JSON 片段：带引号=官方形态，不带=数字形态
	}{
		{"官方字符串毫秒", "om_str_ms", `"1609073151345"`},
		{"官方字符串纳秒", "om_str_ns", `"1693637833687199921"`},
		{"数字毫秒（容错）", "om_num_ms", `1609073151345`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hub, err := svc.dispatchFeishu(context.Background(), fmt.Sprintf("%d", acc.ID), &ParsedPayload{}, feishuTextEventBody(tc.msgID, tc.createTime))
			if err != nil {
				t.Fatalf("官方形状报文必须解析成功并入站，got %v", err)
			}
			if hub == nil || hub.MsgID != tc.msgID {
				t.Fatalf("hub 行缺失或 msg_id 非官方 message_id: %+v", hub)
			}
		})
	}

	var cnt int64
	if err := db.Model(&model.MessageHub{}).Where("platform = ? AND conversation_id = ?", "feishu", "oc_shape").Count(&cnt).Error; err != nil {
		t.Fatalf("count hub: %v", err)
	}
	if cnt != 3 {
		t.Fatalf("三条不同 message_id 应各入库一行，实际 %d", cnt)
	}
}
