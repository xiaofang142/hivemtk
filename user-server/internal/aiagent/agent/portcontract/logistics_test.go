package portcontract

import (
	"context"
	"testing"
)

var _ LogisticsPort = (*NoopLogisticsPort)(nil)

func TestNoopLogisticsPortTrack(t *testing.T) {
	p := NewNoopLogisticsPort()
	res, err := p.Track(context.Background(), &LogisticsTrackRequest{
		Platform:   "taobao",
		OrderID:    "O1",
		TrackingNo: "SF123",
		Carrier:    "SF",
	})
	if err != nil {
		t.Fatalf("Track() error = %v", err)
	}
	if res.Configured || res.Found || res.Realtime {
		t.Fatalf("Noop 端口应全部为未配置: %+v", res)
	}
	if res.TrackingNo != "SF123" || res.Carrier != "SF" {
		t.Fatalf("Noop 端口应回显请求字段: %+v", res)
	}
	if res.Notice == "" {
		t.Fatal("Notice 不应为空")
	}
}
