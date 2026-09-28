package controller

// 批19e 契约锁：编排步数必须有上界。
// CreateBrowserTaskReq 对每个数字参数都钳了区间（loop_count 1..1000、delay_ms、timeout_sec、
// retry_*），唯独 Steps 数组本身没有 max——于是一条任务可以带 100 万步。
// 落点不是「跑不完」那层（会话有 TimeoutSec≤3600 的硬闸兜着），而是**读放大**：
// steps 整列随任务详情返回，GET 一次任务就把百 MB JSON 拉进内存再吐给前端，
// 编排面板当场卡死；而这台实例是共享的，一个人的任务能拖慢所有人。
// 设计前提本身也是「单任务步数量级为个位数」（写台账决策 1 就是按它省掉复合索引的），
// 无上界等于让那条决策站不住。
// 正文长度不在本批改：单条命令帧在 nm-host 侧已有 1 MiB 硬顶（cmd/nm-host/main.go
// nmMaxOutboundFrameBytes），超帧的步根本执行不了；而在这里给 Value/Target 加字数上限，
// 会把「一条本来就合法的长评论」在服务端判死——那是更坏的交易。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"hivemtk-user/internal/browser_automation/dto"
	// 适配器 init() 自注册：TaskService.Create 会查 L3 注册表，不导进来就是「未注册」（本包首跑实测 500）。
	// 只导 xiaohongshu：请求体里用的就是它，导多的会让「注册表有几家」这个前提变得含混。
	"hivemtk-user/internal/browser_automation/model"
	_ "hivemtk-user/internal/browser_automation/platform/xiaohongshu"
	barepo "hivemtk-user/internal/browser_automation/repository"
	basvc "hivemtk-user/internal/browser_automation/service"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"

	"gorm.io/gorm"
)

const (
	b19eOwner = uint(781407)
	b19eCap   = 200 // 与 dto 里的 max 同口径：改上界必须同时改这里，边界两腿才会一起指出动了哪一侧
)

func newB19eCtrl(t *testing.T) (*TaskController, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BrowserTask{}, &model.BrowserSession{})
	if db == nil {
		t.Fatal("测试库不可达：步数上界的两腿无法判定（不 Skip，跳过等于没锁）")
	}
	taskRepo := barepo.NewBrowserTaskRepositoryWithDB(db)
	sessionRepo := barepo.NewBrowserSessionRepositoryWithDB(db)
	stepRepo := barepo.NewBrowserStepRepositoryWithDB(db)
	// hostProber 传 nil：Create 路径不碰「Host 在不在」，探针用不上它——
	// 哪天创建链要先问 Host，这里会当场 panic，比断言更硬。
	return NewTaskController(basvc.NewTaskService(taskRepo, sessionRepo,
		basvc.NewExecutor(nil, sessionRepo, stepRepo, nil, nil)), nil), db
}

// b19eBody n 步 wait 编排的请求体（wait 在白名单内，且不依赖任何页面状态）
func b19eBody(n int) string {
	items := make([]dto.StepItem, n)
	for i := range items {
		items[i] = dto.StepItem{Action: "wait", Ms: 10}
	}
	steps, err := json.Marshal(items)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf(`{"name":"b19e-%d","task_type":"one_shot","url":"https://example.com","steps":%s}`, n, steps)
}

func b19eCreate(t *testing.T, c *TaskController, n int) (int, errBody) {
	t.Helper()
	return b19cCall(t, c.Create, http.MethodPost, "/api/browser-automation/tasks", b19eBody(n), "", b19eOwner)
}

func TestB19EStepCountOverCapIsRejectedAtTheBoundary(t *testing.T) {
	c, db := newB19eCtrl(t)
	httpCode, body := b19eCreate(t, c, b19eCap+1)
	if httpCode != http.StatusBadRequest {
		t.Errorf("%d 步 → HTTP %d want 400（code=%v msg=%q）", b19eCap+1, httpCode, body.Code, body.Message)
	}
	if body.Code != string(utils.ErrorCodeInvalidParameter) {
		t.Errorf("%d 步 → code=%v want %s（超上界是入参问题，不是服务端故障）", b19eCap+1, body.Code, utils.ErrorCodeInvalidParameter)
	}
	if !strings.Contains(body.Message, "Steps") {
		t.Errorf("%d 步的拒绝文案得点出是哪个字段：%q", b19eCap+1, body.Message)
	}
	// 上界生效不该留下半成品：一行都不许落库（按名字数，本包的用例共用同一进程库，
	// 全用户计数会把别的腿合法创建的那行算进来）
	var n int64
	if err := db.Model(&model.BrowserTask{}).Where("user_id = ? AND name = ?", b19eOwner,
		fmt.Sprintf("b19e-%d", b19eCap+1)).Count(&n).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if n != 0 {
		t.Errorf("超上界的请求被拒后仍落了 %d 行任务", n)
	}
}

// 编辑路径同口径：创建拦得住、编辑绕得过去等于没拦（两个 tag 是两处字面量，
// 漏改任意一处都得有点名它的腿）。内侧 200 也验：上界写小到 0 时外侧照样「通过」，
// 只有内侧能发现合法编排存不下去。
func TestB19EUpdatePathSharesTheCap(t *testing.T) {
	c, db := newB19eCtrl(t)
	ctx := context.Background()
	seed := &model.BrowserTask{Name: "b19e-seed", TaskType: "one_shot", Status: "draft",
		Url: "https://example.com", UserID: b19eOwner, Platform: "xiaohongshu",
		TimeoutSec: 120, Steps: []byte(`[{"action":"open_tab"}]`)}
	if err := barepo.NewBrowserTaskRepositoryWithDB(db).Create(ctx, seed); err != nil {
		t.Fatalf("种子任务落库失败：%v", err)
	}
	idParam := fmt.Sprint(seed.ID)

	httpCode, body := b19cCall(t, c.Update, http.MethodPut, "/api/browser-automation/tasks/"+idParam,
		b19eBody(b19eCap+1), idParam, b19eOwner)
	if httpCode != http.StatusBadRequest || body.Code != string(utils.ErrorCodeInvalidParameter) {
		t.Errorf("编辑提交 %d 步 → HTTP %d code=%v want 400/%s（msg=%q）",
			b19eCap+1, httpCode, body.Code, utils.ErrorCodeInvalidParameter, body.Message)
	}
	var after model.BrowserTask
	if err := db.WithContext(ctx).Where("id = ?", seed.ID).First(&after).Error; err != nil {
		t.Fatalf("读回种子任务失败：%v", err)
	}
	// 比步数而不是比 JSON 原文：jsonb 存进去会被 PG 重新序列化（冒号后补空格），原文比对会假红
	var afterItems []dto.StepItem
	if err := json.Unmarshal(after.Steps, &afterItems); err != nil {
		t.Fatalf("被拒后 steps 解析失败：%v", err)
	}
	if len(afterItems) != 1 {
		t.Errorf("被拒的编辑把 steps 换成了 %d 步（原样应为 1 步）", len(afterItems))
	}

	if code2, body2 := b19cCall(t, c.Update, http.MethodPut, "/api/browser-automation/tasks/"+idParam,
		b19eBody(b19eCap), idParam, b19eOwner); code2 != http.StatusOK {
		t.Fatalf("编辑提交 %d 步（上界内侧）→ HTTP %d want 200（msg=%q）", b19eCap, code2, body2.Message)
	}
	var stored model.BrowserTask
	if err := db.WithContext(ctx).Where("id = ?", seed.ID).First(&stored).Error; err != nil {
		t.Fatalf("读回任务失败：%v", err)
	}
	var items []dto.StepItem
	if err := json.Unmarshal(stored.Steps, &items); err != nil {
		t.Fatalf("steps 列解析失败：%v", err)
	}
	if len(items) != b19eCap {
		t.Errorf("编辑后落库步数 %d want %d", len(items), b19eCap)
	}
}

// 反向锁：边界内侧必须照常通过——上界若被写小（或写成了 0/负数把合法请求也拦死），
// 用户就是「编排到一半存不下」，而这条腿是唯一能发现它的。
func TestB19EStepCountAtCapStillCreates(t *testing.T) {
	c, db := newB19eCtrl(t)
	httpCode, body := b19eCreate(t, c, b19eCap)
	if httpCode != http.StatusOK {
		t.Fatalf("%d 步（上界内侧）→ HTTP %d want 200（msg=%q）", b19eCap, httpCode, body.Message)
	}
	var created model.BrowserTask
	if err := db.WithContext(context.Background()).
		Where("user_id = ? AND name = ?", b19eOwner, fmt.Sprintf("b19e-%d", b19eCap)).First(&created).Error; err != nil {
		t.Fatalf("读回任务失败：%v", err)
	}
	var items []dto.StepItem
	if err := json.Unmarshal(created.Steps, &items); err != nil {
		t.Fatalf("steps 列解析失败：%v", err)
	}
	if len(items) != b19eCap {
		t.Errorf("落库步数 %d want %d（截断存步=用户看到的和跑掉的不是一回事）", len(items), b19eCap)
	}
}
