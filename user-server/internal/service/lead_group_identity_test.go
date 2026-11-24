package service

import (
	"strings"
	"testing"
)

// D5/D16：群客户身份键按 tenant+platform+account+group_id 生成，
// 跨账号同名群不串、群名变化不影响，且 grp: 前缀与个人身份隔离。
func TestGroupCustomerUnifiedID_StableAndScoped(t *testing.T) {
	a := groupCustomerUnifiedID(1, "qq", "acc-1", "g-100")
	b := groupCustomerUnifiedID(1, "qq", "acc-1", "g-100")
	if a != b {
		t.Fatalf("同群同账号 OneID 必须稳定：%s != %s", a, b)
	}
	if !strings.HasPrefix(a, "grp:") {
		t.Fatalf("群身份必须有 grp: 前缀以与个人身份隔离：%s", a)
	}
	otherAccount := groupCustomerUnifiedID(1, "qq", "acc-2", "g-100")
	if a == otherAccount {
		t.Fatalf("跨账号同名群必须不同：%s", a)
	}
	otherGroup := groupCustomerUnifiedID(1, "qq", "acc-1", "g-200")
	if a == otherGroup {
		t.Fatalf("同账号不同群必须不同：%s", a)
	}
	if !strings.Contains(a, "g-100") {
		t.Fatalf("群ID 必须进入身份键：%s", a)
	}
}
