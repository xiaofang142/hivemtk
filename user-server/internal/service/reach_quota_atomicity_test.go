package service

import "testing"

// 多 scope 回写必须使用预占时确定的最小 scope token，不依赖数据库返回顺序。
func TestReachQuotaVerifyTokenUsesDeterministicFirstScope(t *testing.T) {
	m := &ReachQuotaManager{}
	rows := []quotaIntentRef{
		{IntentID: "intent#scope-z", Token: "tok" + "en-z"},
		{IntentID: "intent#scope-a", Token: "tok" + "en-a"},
	}

	if err := m.verifyToken(rows, "token-a"); err != nil {
		t.Fatalf("token from the lexicographically first scope should be accepted: %v", err)
	}
}
