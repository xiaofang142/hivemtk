package bridge

import (
	"testing"
	"time"
)

// G-06：HTTP ack 的 claimed_at 栅栏必须在任何写之前解析，非法值整批拒绝。
// 客户端拿 GET outbox 那一行的 claimed_at 原样回传；解析失败只允许 400，
// 不能退化成"无栅栏 ack"（那会把已重发的新行误标成 delivered）。
func TestBuildAckClaimFence(t *testing.T) {
	ts := time.Date(2026, 11, 27, 10, 30, 0, 123456789, time.UTC)
	raw := ts.Format(time.RFC3339Nano)

	cases := []struct {
		name    string
		req     *BridgeOutboxAckRequest
		want    map[string]time.Time
		wantErr bool
	}{
		{name: "nil request"},
		{
			name: "items claimed_at",
			req: &BridgeOutboxAckRequest{Items: []BridgeOutboxAckItem{
				{MsgID: "m1", ConversationID: "c1", ClaimedAt: raw, PushAttempts: 3},
				{MsgID: "m2", ConversationID: "c1"},
			}},
			want: map[string]time.Time{"m1": ts},
		},
		{
			name: "v1 fence",
			req: &BridgeOutboxAckRequest{
				MsgIDs: []string{"m3"},
				Fence:  map[string]string{"m3": raw},
			},
			want: map[string]time.Time{"m3": ts},
		},
		{
			name: "invalid fence rejected even when items valid",
			req: &BridgeOutboxAckRequest{
				Items: []BridgeOutboxAckItem{{MsgID: "m1", ClaimedAt: raw}},
				Fence: map[string]string{"m1": "not-a-time"},
			},
			want:    map[string]time.Time{"m1": ts},
			wantErr: true, // 顶层 fence 非法同样整批拒绝，不允许悄悄忽略
		},
		{
			name:    "invalid item claimed_at",
			req:     &BridgeOutboxAckRequest{Items: []BridgeOutboxAckItem{{MsgID: "m1", ClaimedAt: "yesterday"}}},
			wantErr: true,
		},
		{
			name:    "invalid fence claimed_at",
			req:     &BridgeOutboxAckRequest{Fence: map[string]string{"m1": "yesterday"}},
			wantErr: true,
		},
		{
			name: "empty values mean legacy",
			req: &BridgeOutboxAckRequest{
				Items: []BridgeOutboxAckItem{{MsgID: "m1", ClaimedAt: ""}},
				Fence: map[string]string{"m2": ""},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildAckClaimFence(tc.req)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("fence size=%d, want %d (%v)", len(got), len(tc.want), got)
			}
			for id, wantTS := range tc.want {
				gotTS, ok := got[id]
				if !ok || !gotTS.Equal(wantTS) {
					t.Errorf("fence[%s]=%v, want %v", id, gotTS, wantTS)
				}
			}
		})
	}
}
