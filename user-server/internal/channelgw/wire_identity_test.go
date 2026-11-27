package channelgw_test

import (
	"testing"

	"hivemtk-user/internal/channelgw"
	"hivemtk-user/internal/pkg/channelbus"
)

// TestWireProtocolIdentitySingleSource 钉住"线路协议常量只有一个权威源"：
// channelgw 的导出别名必须与 internal/pkg/channelbus 完全相等，任何一侧漂移都会
// 让 bridge HTTP/WS、SageX upload、webhook ack 又退回各写一套协议值。
func TestWireProtocolIdentitySingleSource(t *testing.T) {
	checks := map[string][2]string{
		"schema":              {channelbus.SchemaVersion, "hivemtk.channel.v1"},
		"frame.register":      {channelgw.FrameRegister, channelbus.FrameRegister},
		"frame.register_rej":  {channelgw.FrameRegisterReject, channelbus.FrameRegisterReject},
		"frame.inbound":       {channelgw.FrameInbound, channelbus.FrameInbound},
		"frame.history":       {channelgw.FrameHistory, channelbus.FrameHistory},
		"frame.pong":          {channelgw.FramePong, channelbus.FramePong},
		"frame.ack":           {channelgw.FrameAck, channelbus.FrameAck},
		"frame.ping":          {channelgw.FramePing, channelbus.FramePing},
		"frame.outbound":      {channelgw.FrameOutboundReply, channelbus.FrameOutboundReply},
		"frame.config_push":   {channelgw.FrameConfigPush, channelbus.FrameConfigPush},
		"transport.http":      {string(channelgw.TransportHTTP), channelbus.TransportHTTP},
		"transport.websocket": {string(channelgw.TransportWebSocket), channelbus.TransportWebSocket},
	}
	for name, pair := range checks {
		if pair[0] != pair[1] {
			t.Errorf("%s 漂移: channelgw=%q channelbus=%q", name, pair[0], pair[1])
		}
	}
	if channelgw.CurrentProtocolVersion != channelbus.CurrentProtocolVersion {
		t.Errorf("协议版本漂移: channelgw=%d channelbus=%d", channelgw.CurrentProtocolVersion, channelbus.CurrentProtocolVersion)
	}
	if channelbus.CurrentProtocolVersion != channelbus.ProtocolVersionV2 {
		t.Errorf("当前协议版本应为 v2，实际=%d", channelbus.CurrentProtocolVersion)
	}
}
