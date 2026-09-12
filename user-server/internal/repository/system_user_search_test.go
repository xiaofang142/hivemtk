// R75 架构下沉回归测试：SystemUserRepository.SearchUsers
// （原 service/system_user.go SearchUsers 直连 repository.GetDB() 拼 ILIKE 查询下沉仓储化）
package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	dbUtil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

func TestSystemUser_SearchUsers(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SystemUser{})
	prev := dbUtil.GetDB()
	dbUtil.SetTestDB(db)
	defer dbUtil.SetTestDB(prev)

	repo := NewSystemUserRepository()
	ctx := context.Background()

	users := []*model.SystemUser{
		{Username: "alice", Email: "a@example.com", RealName: "张伟", Password: "x", Role: "admin"},
		{Username: "bob", Email: "b@Example.com", RealName: "李娜", Password: "x", Role: "staff"},
		{Username: "Charlie", Email: "c@other.cn", RealName: "王强", Password: "x", Role: "staff"},
	}
	for _, u := range users {
		if err := db.Create(u).Error; err != nil {
			t.Fatalf("seed 失败: %v", err)
		}
	}

	cases := []struct {
		keyword  string
		wantIDs  int64 // total
		wantName string
	}{
		{"alice", 1, "alice"},  // username 精确命中
		{"ALICE", 1, "alice"},  // ILIKE 不区分大小写
		{"example", 2, ""},     // email 双命中（a@/b@Example 大小写混合）
		{"张伟", 1, "alice"},     // real_name 命中
		{"char", 1, "Charlie"}, // username 前缀部分命中
		{"nomatch", 0, ""},     // 无命中
		{"", 3, ""},            // 空关键词 = 全量（分页由 offset/limit 控制）
	}
	for _, c := range cases {
		list, total, err := repo.SearchUsers(ctx, c.keyword, 0, 10)
		if err != nil {
			t.Fatalf("keyword=%q 查询失败: %v", c.keyword, err)
		}
		if total != c.wantIDs {
			t.Fatalf("keyword=%q total 应=%d, 实际=%d", c.keyword, c.wantIDs, total)
		}
		if len(list) != int(c.wantIDs) {
			t.Fatalf("keyword=%q 返回条数应=%d, 实际=%d", c.keyword, c.wantIDs, len(list))
		}
		if c.wantName != "" && list[0].Username != c.wantName {
			t.Fatalf("keyword=%q 首条应=%s, 实际=%s", c.keyword, c.wantName, list[0].Username)
		}
	}

	// 分页：limit=2 → 第一条 id DESC（Charlie 最后建 id 最大）
	page, total, err := repo.SearchUsers(ctx, "", 0, 2)
	if err != nil {
		t.Fatalf("分页查询失败: %v", err)
	}
	if total != 3 || len(page) != 2 {
		t.Fatalf("total=3/len=2 期望, 实际 total=%d len=%d", total, len(page))
	}
	if page[0].Username != "Charlie" {
		t.Fatalf("id DESC 首条应为 Charlie, 实际 %s", page[0].Username)
	}
}
