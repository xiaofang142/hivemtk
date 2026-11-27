package model

import (
	"testing"

	"hivemtk-user/internal/pkg/channelbus"
)

// TestBridgeAckTerminalStatusSingleSource 钉住下行三态没有第二份词表。
func TestBridgeAckTerminalStatusSingleSource(t *testing.T) {
	checks := map[string][2]string{
		"delivered": {BridgeAckStatusDelivered, channelbus.StatusDelivered},
		"failed":    {BridgeAckStatusFailed, channelbus.StatusFailed},
		"unknown":   {BridgeAckStatusUnknown, channelbus.StatusUnknown},
	}
	for name, pair := range checks {
		if pair[0] != pair[1] {
			t.Errorf("%s 漂移: model=%q channelbus=%q", name, pair[0], pair[1])
		}
	}
	for _, status := range []string{BridgeAckStatusDelivered, BridgeAckStatusFailed, BridgeAckStatusUnknown} {
		if !IsBridgeAckTerminalStatus(status) {
			t.Errorf("canonical 终态 %q 被判定为非法", status)
		}
	}
	if IsBridgeAckTerminalStatus("acked") {
		t.Error("响应状态 acked 不应被当作投递终态")
	}
}
