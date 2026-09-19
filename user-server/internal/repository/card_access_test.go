package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/timeutil"
)

// TestAccessTodayWindowIsBusinessDay 钉死「同卡同 IP 每日一限」的窗口按**业务日**切。
//
// 反向验证：把实现换回老写法（宿主机时区的日期串 + time.Parse 的 UTC 零点），
// start 会落到 2026-09-18T00:00Z、end 落到 2026-09-19T00:00Z，本用例三条断言全红。
func TestAccessTodayWindowIsBusinessDay(t *testing.T) {
	cst := time.FixedZone("CST", 8*3600)
	// 2026-09-18 17:30 UTC == 2026-09-19 01:30 CST：宿主机日比业务日**早**一天，
	// 正是 UTC 容器上把「今天」算成昨天的那个窗口。
	instant := time.Date(2026, 9, 18, 17, 30, 0, 0, time.UTC)

	start, end := accessTodayWindow(instant)
	if want := time.Date(2026, 9, 19, 0, 0, 0, 0, cst); !start.Equal(want) {
		t.Errorf("start = %s，期望业务日首 %s", start.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if want := time.Date(2026, 9, 20, 0, 0, 0, 0, cst); !end.Equal(want) {
		t.Errorf("end = %s，期望次日业务日首 %s", end.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if d := end.Sub(start); d != 24*time.Hour {
		t.Errorf("窗口长度 = %v，期望恰好 24h（半开区间，不重不漏）", d)
	}
}

// TestHasAccessTodayMatchesServerToday 用 PG 自己报的 current_date 作独立 oracle，
// 确认 Go 侧窗口起点与 DB 侧（会话时区钉 CST）对「今天」的判断同源。
// 这条断言只在真库上成立，因此必须跑在 testutil 的集成测试里。
func TestHasAccessTodayMatchesServerToday(t *testing.T) {
	db := testutil.NewTestDB(t, &model.CardAccess{})
	var pgToday string
	if err := db.Raw("SELECT to_char(current_date, 'YYYY-MM-DD')").Scan(&pgToday).Error; err != nil {
		t.Fatalf("读取 PG 的 current_date 失败: %v", err)
	}

	start, _ := accessTodayWindow(time.Now())
	if got := timeutil.BusinessDate(start); got != pgToday {
		t.Fatalf("Go 侧业务日 %s 与 PG current_date %s 不同源", got, pgToday)
	}

	repo := NewCardAccessRepository(db)
	ctx := context.Background()

	// 业务日内的行要命中；日首前一秒（即上一业务日的尾巴）不得命中。
	cardID := uint(730001)
	if err := repo.Create(ctx, &model.CardAccess{CardID: cardID, IPAddress: "10.73.0.1", AccessTime: start.Add(time.Hour)}); err != nil {
		t.Fatalf("写入业务日内访问失败: %v", err)
	}
	hit, err := repo.HasAccessToday(ctx, cardID, "10.73.0.1")
	if err != nil {
		t.Fatalf("HasAccessToday 报错: %v", err)
	}
	if !hit {
		t.Error("业务日内的访问必须被判为「今日已访问」")
	}

	if err := repo.Create(ctx, &model.CardAccess{CardID: cardID, IPAddress: "10.73.0.2", AccessTime: start.Add(-time.Second)}); err != nil {
		t.Fatalf("写入上一业务日访问失败: %v", err)
	}
	miss, err := repo.HasAccessToday(ctx, cardID, "10.73.0.2")
	if err != nil {
		t.Fatalf("HasAccessToday 报错: %v", err)
	}
	if miss {
		t.Error("日首之前一秒属于上一业务日，不得计入「今日」")
	}
}
