package service

import "testing"

// 维护人多账号映射：张三 WX1/WX2，李四 WX3/QQ4，王五 QQ5/TG6；
// 按来源账号只路由到对应维护人，一人多账号、一账号多维护人均有确定语义。
func TestLeadMaintainerMapping_BySourceAccount(t *testing.T) {
	m := LeadMaintainerMap{
		{Maintainer: "张三", Accounts: []string{"WX1", "WX2"}},
		{Maintainer: "李四", Accounts: []string{"WX3", "QQ4"}},
		{Maintainer: "王五", Accounts: []string{"QQ5", "TG6"}},
	}
	cases := map[string]string{
		"WX1": "张三", "WX2": "张三", "WX3": "李四", "QQ4": "李四", "QQ5": "王五", "TG6": "王五",
	}
	for account, want := range cases {
		got := m.MaintainersFor(account)
		if len(got) != 1 || got[0] != want {
			t.Fatalf("账号 %s 应只路由给 %s，实际 %v", account, want, got)
		}
	}
	if got := m.MaintainersFor("WX9"); len(got) != 0 {
		t.Fatalf("未绑定账号不应猜测维护人，实际 %v", got)
	}
}

// 一账号多维护人：全部返回，不取第一个。
func TestLeadMaintainerMapping_MultipleMaintainers(t *testing.T) {
	m := LeadMaintainerMap{
		{Maintainer: "张三", Accounts: []string{"WX1"}},
		{Maintainer: "李四", Accounts: []string{"WX1"}},
	}
	got := m.MaintainersFor("WX1")
	if len(got) != 2 || got[0] != "张三" || got[1] != "李四" {
		t.Fatalf("一账号多维护人应全部返回，实际 %v", got)
	}
}
