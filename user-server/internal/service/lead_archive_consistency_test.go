package service

import "testing"

// 归档字段一致性：候选/降级线索不得靠手工归档越有效性闸；
// ArchivedAt 只在真正完成后填，失败/未完成不得伪填。
func TestLeadArchiveConsistency_GateAndTimestamp(t *testing.T) {
	cases := []struct {
		name      string
		candidate bool
		degraded  bool
		success   bool
		wantAllow bool
		wantStamp bool
	}{
		{"有效成功", false, false, true, true, true},
		{"候选不得越闸", true, false, true, false, false},
		{"降级不得越闸", false, true, true, false, false},
		{"失败不填归档时间", false, false, false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allow := leadArchiveAllowed(tc.candidate, tc.degraded)
			if allow != tc.wantAllow {
				t.Fatalf("准入期望 %v，实际 %v", tc.wantAllow, allow)
			}
			stamp := leadArchiveTimestamp(allow && tc.success)
			if stamp != tc.wantStamp {
				t.Fatalf("归档时间期望 %v，实际 %v", tc.wantStamp, stamp)
			}
		})
	}
}
