package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/repository"
)

// faCohortSvc 构造只靠 seam 的 CohortGapService：repo 指针非 nil（过守卫），
// 但所有计数都走注入函数，不碰 DB。
func faCohortSvc(now time.Time, customers func(context.Context, time.Time, time.Time) (int64, error), retained func(context.Context, time.Time, time.Time, time.Time, time.Time) (int64, error)) *CohortGapService {
	return &CohortGapService{
		repo:           &repository.CohortGapRepo{},
		nowFn:          func() time.Time { return now },
		countCustomers: customers,
		countRetained:  retained,
	}
}

// faCohortCST 以业务时区（CST）构造时刻（避免与 report_fa_test.go 的 faCST 重名）。
func faCohortCST(y int, m time.Month, d, h, min int) time.Time {
	loc := timeutil.StartOfBusinessDay(time.Date(y, m, d, h, min, 0, 0, time.UTC)).Location()
	return time.Date(y, m, d, h, min, 0, 0, loc)
}

func TestFACohort_WeekBoundaryAndW0Alignment(t *testing.T) {
	ctx := context.Background()
	// 冻结在周三 15:47 CST —— 周起点必须是上一个周日 00:00 CST（不保留时分）。
	now := faCohortCST(2026, 11, 18, 15, 47)
	type window struct{ start, end time.Time }
	var sizeWindows []window
	var retainedWindows []window
	svc := faCohortSvc(now,
		func(_ context.Context, start, end time.Time) (int64, error) {
			sizeWindows = append(sizeWindows, window{start, end})
			return 10, nil
		},
		func(_ context.Context, _cStart, _cEnd, wStart, wEnd time.Time) (int64, error) {
			retainedWindows = append(retainedWindows, window{wStart, wEnd})
			return 5, nil
		})

	res, err := svc.Cohort(ctx, 3)
	if err != nil {
		t.Fatalf("Cohort: %v", err)
	}
	if res.Status != "ok" || len(res.Errors) != 0 {
		t.Fatalf("无故障应 ok，实际 status=%s errors=%v", res.Status, res.Errors)
	}
	// 周起点：now=周三 11/18 → 周日 11/15 00:00 CST（周日起点）。
	// cohorts 顺序 i=2..0 => 最老 11/01、11/08、最新 11/15（最后一条 size 窗口）。
	wantLatestStart := faCohortCST(2026, 11, 15, 0, 0)
	lastSize := sizeWindows[len(sizeWindows)-1]
	if !lastSize.start.Equal(wantLatestStart) {
		t.Fatalf("周起点未归零到 CST 自然周零点: got %v want %v", lastSize.start, wantLatestStart)
	}
	if !lastSize.end.Equal(wantLatestStart.AddDate(0, 0, 7)) {
		t.Fatalf("size 窗口末端错误: got %v", lastSize.end)
	}
	// 第一个 cohort 必须是 11/01（= 11/15 - 14 天），验证完整周回退。
	firstSize := sizeWindows[0]
	if !firstSize.start.Equal(wantLatestStart.AddDate(0, 0, -14)) {
		t.Fatalf("最老 cohort 起点错误: got %v", firstSize.start)
	}
	// 最新 cohort（11/15）的 W+0 必须等于它自己的 cstart，不是 cend。
	// retainedWindows 按 cohort 顺序 × wi：老 cohort(3) + 中 cohort(2，W+2 未成熟) + 新 cohort(1，W+1/W+2 未成熟)。
	lastRetained := retainedWindows[len(retainedWindows)-1]
	if !lastRetained.start.Equal(lastSize.start) {
		t.Fatalf("W+0 窗口错位: W+0.start=%v, cohort.start=%v", lastRetained.start, lastSize.start)
	}
	if !lastRetained.end.Equal(lastSize.start.AddDate(0, 0, 7)) {
		t.Fatalf("W+0 窗口末端错误: %v", lastRetained.end)
	}
	// 正常值应落盘：W+0 = 5*100/10 = 50%
	v := res.Cohorts[2].Retention[0]
	if v == nil || *v != 50 {
		t.Fatalf("正常留存值应为 50, got %v", v)
	}
	if res.Cohorts[2].RetentionStates[0] != "ok" {
		t.Fatalf("正常格状态应为 ok, got %s", res.Cohorts[2].RetentionStates[0])
	}
}

func TestFACohort_ImmatureNotZero(t *testing.T) {
	ctx := context.Background()
	// 冻结在周日 11/15 21:00 CST：当前周未结束，最新 cohort 的 W+1/W+2 都是未来 → immature。
	now := faCohortCST(2026, 11, 15, 21, 0)
	svc := faCohortSvc(now,
		func(context.Context, time.Time, time.Time) (int64, error) { return 10, nil },
		func(context.Context, time.Time, time.Time, time.Time, time.Time) (int64, error) { return 0, nil })

	res, err := svc.Cohort(ctx, 3)
	if err != nil {
		t.Fatalf("Cohort: %v", err)
	}
	row := res.Cohorts[len(res.Cohorts)-1] // 最新 cohort（当前周）
	// W+0 = 本周起点（<=now）→ ok；W+1/W+2 起点在未来 → immature。
	if row.Retention[0] == nil {
		t.Fatalf("W+0 应为 ok 值")
	}
	for wi := 1; wi < 3; wi++ {
		if row.Retention[wi] != nil {
			t.Fatalf("未成熟格 W+%d 应为 nil，不得显示 0%%: got %v", wi, *row.Retention[wi])
		}
		if row.RetentionStates[wi] != "immature" {
			t.Fatalf("未成熟格状态: got %s", row.RetentionStates[wi])
		}
	}
}

func TestFACohort_SizeQueryFailureUnavailable(t *testing.T) {
	ctx := context.Background()
	now := faCohortCST(2026, 11, 18, 12, 0)
	svc := faCohortSvc(now,
		func(context.Context, time.Time, time.Time) (int64, error) { return 0, errors.New("db down") },
		func(context.Context, time.Time, time.Time, time.Time, time.Time) (int64, error) { return 5, nil })

	res, err := svc.Cohort(ctx, 3)
	if err != nil {
		t.Fatalf("Cohort: %v", err)
	}
	if res.Status != "unavailable" {
		t.Fatalf("所有分母查询失败应 unavailable, got %s", res.Status)
	}
	if len(res.Errors) == 0 {
		t.Fatal("应有错误明细")
	}
	foundSizeErr := false
	for _, row := range res.Cohorts {
		for wi, v := range row.Retention {
			if v != nil {
				t.Fatalf("分母未知时格 W+%d 不得显示 0%%: %v", wi, *v)
			}
			// 未来周优先判 immature（未成熟），其余成熟格应为 size_error。
			if row.RetentionStates[wi] == "size_error" {
				foundSizeErr = true
			} else if row.RetentionStates[wi] != "immature" {
				t.Fatalf("分母未知格状态: got %s", row.RetentionStates[wi])
			}
		}
	}
	if !foundSizeErr {
		t.Fatal("应至少有一个 size_error 格")
	}
}

func TestFACohort_RetainedQueryFailurePartial(t *testing.T) {
	ctx := context.Background()
	now := faCohortCST(2026, 11, 18, 12, 0)
	call := 0
	svc := faCohortSvc(now,
		func(context.Context, time.Time, time.Time) (int64, error) { return 10, nil },
		func(context.Context, time.Time, time.Time, time.Time, time.Time) (int64, error) {
			call++
			if call == 2 { // 第二次留存查询失败 → 恰一格 error
				return 0, errors.New("event query failed")
			}
			return 5, nil
		})

	res, err := svc.Cohort(ctx, 4)
	if err != nil {
		t.Fatalf("Cohort: %v", err)
	}
	if res.Status != "partial" {
		t.Fatalf("部分查询失败应 partial, got %s", res.Status)
	}
	if len(res.Errors) == 0 {
		t.Fatal("应有错误明细")
	}
	foundErrCell := false
	for _, row := range res.Cohorts {
		for wi, v := range row.Retention {
			if row.RetentionStates[wi] == "error" {
				foundErrCell = true
				if v != nil {
					t.Fatalf("查询失败格 W+%d 不得显示 0%%: %v", wi, *v)
				}
			}
		}
	}
	if !foundErrCell {
		t.Fatal("未找到 error 状态的格")
	}
}

func TestFACohort_NoCohortNotZero(t *testing.T) {
	ctx := context.Background()
	now := faCohortCST(2026, 11, 18, 12, 0)
	svc := faCohortSvc(now,
		func(context.Context, time.Time, time.Time) (int64, error) { return 0, nil }, // 空 cohort
		func(context.Context, time.Time, time.Time, time.Time, time.Time) (int64, error) { return 0, nil })

	res, err := svc.Cohort(ctx, 3)
	if err != nil {
		t.Fatalf("Cohort: %v", err)
	}
	for _, row := range res.Cohorts {
		for wi, v := range row.Retention {
			if row.RetentionStates[wi] == "ok" && v != nil {
				t.Fatalf("无样本 cohort 的 W+%d 不得显示 0%%: %v", wi, *v)
			}
		}
	}
}
