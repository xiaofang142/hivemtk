package controller

// 契约锁：PUT /tasks/:id 必须真的能改「失败自动重试」三件套。
// 立项理由（登记残项，本轮清账）：Editor.vue 的表单里就有这一组开关与两个数字，
// save() 把整个 form 发出去，而 UpdateBrowserTaskReq 根本没这三个字段 ——
// gin 的 ShouldBindJSON 对未知字段默认宽容，于是「200 成功 + 已保存」toast 之后
// 库里一个字节都没变，详情页照旧显示旧值。静默丢失比报错更难发现：用户以为自己开了自动重试。
// 同一条纪律已经立过一次（创建拦得住、编辑绕得过去=没拦）：两条路径的区间必须同口径。

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
	barepo "hivemtk-user/internal/browser_automation/repository"
	basvc "hivemtk-user/internal/browser_automation/service"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"

	"gorm.io/gorm"
)

const b20eOwner = uint(781420)

func newB20eCtrl(t *testing.T) (*TaskController, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BrowserTask{}, &model.BrowserSession{})
	if db == nil {
		t.Fatal("测试库不可达：重试字段的三腿无法判定（不 Skip，跳过等于没锁）")
	}
	taskRepo := barepo.NewBrowserTaskRepositoryWithDB(db)
	sessionRepo := barepo.NewBrowserSessionRepositoryWithDB(db)
	stepRepo := barepo.NewBrowserStepRepositoryWithDB(db)
	// hostProber 传 nil：Update 路径不碰「Host 在不在」（与创建腿同口径）。
	return NewTaskController(basvc.NewTaskService(taskRepo, sessionRepo,
		basvc.NewExecutor(nil, sessionRepo, stepRepo, nil, nil)), nil), db
}

// b20eSeed 落一行已知重试配置的任务，返回其 id。
// 三个数字全部铺成非零且互不相同：0 会被 gorm 的 default 标签顶掉（retry_delay_sec 默认 300），
// 铺成同一个数则「改错了字段」与「没改」在断言里长一样。
func b20eSeed(t *testing.T, db *gorm.DB, name string, onFail bool, delay, maxTimes int) uint {
	t.Helper()
	seed := &model.BrowserTask{Name: name, TaskType: "one_shot", Status: "draft",
		Url: "https://example.com", UserID: b20eOwner, Platform: "xiaohongshu",
		TimeoutSec: 120, Steps: []byte(`[{"action":"open_tab"}]`),
		RetryOnFail: onFail, RetryDelaySec: delay, MaxRetryTimes: maxTimes}
	if err := barepo.NewBrowserTaskRepositoryWithDB(db).Create(context.Background(), seed); err != nil {
		t.Fatalf("种子任务落库失败：%v", err)
	}
	return seed.ID
}

// 读回一律用独立零值 struct：复用已填充的 struct 再 First() 会把旧字段并进 WHERE（record not found 假红）。
func b20eRead(t *testing.T, db *gorm.DB, id uint) model.BrowserTask {
	t.Helper()
	var got model.BrowserTask
	if err := db.WithContext(context.Background()).Where("id = ?", id).First(&got).Error; err != nil {
		t.Fatalf("读回任务 id=%d 失败：%v", id, err)
	}
	return got
}

func b20ePut(t *testing.T, c *TaskController, id uint, body string) (int, errBody) {
	t.Helper()
	p := fmt.Sprint(id)
	return b19cCall(t, c.Update, http.MethodPut, "/api/browser-automation/tasks/"+p, body, p, b20eOwner)
}

// ① 三个字段都改得动（缺一字段就是一条静默丢失的腿）。
func TestB20EUpdatePersistsAllThreeRetryFields(t *testing.T) {
	c, db := newB20eCtrl(t)
	id := b20eSeed(t, db, "b20e-persist", false, 60, 1)
	body := `{"retry_on_fail":true,"retry_delay_sec":600,"max_retry_times":5}`
	code, resp := b20ePut(t, c, id, body)
	if code != http.StatusOK {
		t.Fatalf("PUT %s → HTTP %d want 200（msg=%q）", body, code, resp.Message)
	}
	got := b20eRead(t, db, id)
	if !got.RetryOnFail || got.RetryDelaySec != 600 || got.MaxRetryTimes != 5 {
		t.Errorf("落库 retry=(%v,%d,%d) want (true,600,5) —— 接口回 200 而库里没变，用户看到的 toast 就是谎",
			got.RetryOnFail, got.RetryDelaySec, got.MaxRetryTimes)
	}
}

// ② 不带这三个字段时必须原样留着（指针语义）：改个名字不许顺手把自动重试关掉。
func TestB20EUpdateWithoutRetryFieldsKeepsThem(t *testing.T) {
	c, db := newB20eCtrl(t)
	id := b20eSeed(t, db, "b20e-keep", true, 420, 4)
	if code, resp := b20ePut(t, c, id, `{"name":"b20e-keep-renamed"}`); code != http.StatusOK {
		t.Fatalf("只改名字的 PUT → HTTP %d want 200（msg=%q）", code, resp.Message)
	}
	got := b20eRead(t, db, id)
	if got.Name != "b20e-keep-renamed" {
		t.Errorf("名字没改成：%q", got.Name)
	}
	if !got.RetryOnFail || got.RetryDelaySec != 420 || got.MaxRetryTimes != 4 {
		t.Errorf("未提供的重试字段被重置成 (%v,%d,%d)，want 原样 (true,420,4)",
			got.RetryOnFail, got.RetryDelaySec, got.MaxRetryTimes)
	}
}

// ③ 区间与 Create 同口径：越界即 400 且库里一个字节都不动（内侧两腿负责发现"上界被写小"）。
func TestB20ERetryRangeMatchesCreateOnBothSides(t *testing.T) {
	c, db := newB20eCtrl(t)
	for _, tc := range []struct {
		name    string
		body    string
		want400 bool
		expect  [2]int // 内侧通过时落库的 (delay, max)
	}{
		{"delay 小于 30", `{"retry_delay_sec":5}`, true, [2]int{}},
		{"delay 大于 86400", `{"retry_delay_sec":86401}`, true, [2]int{}},
		{"max 大于 10", `{"max_retry_times":11}`, true, [2]int{}},
		{"delay 内侧 30", `{"retry_delay_sec":30}`, false, [2]int{30, 7}},
		{"max 内侧 10", `{"max_retry_times":10}`, false, [2]int{500, 10}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := b20eSeed(t, db, "b20e-range-"+strings.ReplaceAll(tc.name, " ", "-"), true, 500, 7)
			code, resp := b20ePut(t, c, id, tc.body)
			if tc.want400 {
				if code != http.StatusBadRequest || resp.Code != string(utils.ErrorCodeInvalidParameter) {
					t.Errorf("%s → HTTP %d code=%v want 400/%s（msg=%q）",
						tc.name, code, resp.Code, utils.ErrorCodeInvalidParameter, resp.Message)
				}
				got := b20eRead(t, db, id)
				if got.RetryDelaySec != 500 || got.MaxRetryTimes != 7 {
					t.Errorf("%s 被拒后库里却变成 (%d,%d)", tc.name, got.RetryDelaySec, got.MaxRetryTimes)
				}
				return
			}
			if code != http.StatusOK {
				t.Fatalf("%s（区间内侧）→ HTTP %d want 200（msg=%q）—— 上界写小了就是「合法配置存不下」",
					tc.name, code, resp.Message)
			}
			got := b20eRead(t, db, id)
			if got.RetryDelaySec != tc.expect[0] || got.MaxRetryTimes != tc.expect[1] {
				t.Errorf("%s 落库 (%d,%d) want (%d,%d)", tc.name,
					got.RetryDelaySec, got.MaxRetryTimes, tc.expect[0], tc.expect[1])
			}
		})
	}
}
