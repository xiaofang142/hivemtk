package repository

// 契约锁：活动触达预算的扣减必须是一条条件更新语句判出来的，
// 不能是「先 SELECT 已用量，再 UPDATE 加一」——后者在多副本/并发 session 下会超发，
// 而超发的一次是**不可逆的真触达**（评论发出去就撤不回），比多扣一次额度严重得多。
//
// 手法：真测试库（testutil.NewTestDB）+ 刻意开多个 goroutine 抢同一额度，
// 判据是库里那列 campaign_act_used 的最终值不超过预算。
import (
	"context"
	"sync"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func campaignBudgetRepo(t *testing.T) (BrowserTaskRepository, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BrowserTask{})
	if db == nil {
		t.Fatal("测试库不可达：条件更新到底拦不拦得住超发，判不了（不 Skip）")
	}
	return NewBrowserTaskRepositoryWithDB(db), db
}

func seedBudgetTask(t *testing.T, db *gorm.DB, name, key string, budget, used int) *model.BrowserTask {
	t.Helper()
	task := &model.BrowserTask{
		Name: name, TaskType: "one_shot", Status: "ready",
		Url: "https://www.xiaohongshu.com/explore", Platform: "xiaohongshu",
		CampaignKey: key, CampaignActBudget: budget, CampaignActUsed: used,
		UserID: 26, LoopCount: 1, TimeoutSec: 60,
	}
	if err := db.WithContext(context.Background()).Create(task).Error; err != nil {
		t.Fatalf("任务落库失败: %v", err)
	}
	return task
}

// 预算为 2 时恰好放行两次，第三次必须被条件更新拒掉，且 used 停在 2。
func TestTryConsumeCampaignActBudgetStopsAtCap(t *testing.T) {
	repo, db := campaignBudgetRepo(t)
	task := seedBudgetTask(t, db, "预算2", "cap-2", 2, 0)
	ctx := context.Background()

	for i := 1; i <= 2; i++ {
		ok, err := repo.TryConsumeCampaignActBudget(ctx, task.ID)
		if err != nil || !ok {
			t.Fatalf("第 %d 次应在额度内放行，实得 ok=%v err=%v", i, ok, err)
		}
	}
	ok, err := repo.TryConsumeCampaignActBudget(ctx, task.ID)
	if err != nil {
		t.Fatalf("额度外不应报错（预算耗尽是配置事实），实得 %v", err)
	}
	if ok {
		t.Errorf("第 3 次必须被拒：预算已打满")
	}

	var got model.BrowserTask
	if err := db.WithContext(ctx).First(&got, task.ID).Error; err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if got.CampaignActUsed != 2 {
		t.Errorf("已用量应停在 2，实得 %d", got.CampaignActUsed)
	}
}

// 未编入活动 / 额度为 0 ⇒ 恒拒（应用层据此跳过扣减，两处口径必须一致）。
func TestTryConsumeCampaignActBudgetUnlimitedAlwaysRejects(t *testing.T) {
	repo, db := campaignBudgetRepo(t)
	ctx := context.Background()
	noKey := seedBudgetTask(t, db, "无活动键", "", 5, 0)
	zeroBudget := seedBudgetTask(t, db, "额度零", "zero", 0, 0)

	for _, task := range []*model.BrowserTask{noKey, zeroBudget} {
		ok, err := repo.TryConsumeCampaignActBudget(ctx, task.ID)
		if err != nil {
			t.Fatalf("%s: 不应报错，实得 %v", task.Name, err)
		}
		if ok {
			t.Errorf("%s: 不受限任务不该被条件更新放行（否则不限任务反而被锁）", task.Name)
		}
	}
}

// 并发抢同一额度：8 条腿同抢 3 条预算，最终 used 必须精确等于 3。
// 这是本用例存在的唯一理由——单线程顺序调用判不出 check-then-act 与条件更新的差别。
func TestTryConsumeCampaignActBudgetNoOvershoot(t *testing.T) {
	repo, db := campaignBudgetRepo(t)
	task := seedBudgetTask(t, db, "并发抢", "race-3", 3, 0)
	ctx := context.Background()

	const legs = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		granted int
	)
	wg.Add(legs)
	for i := 0; i < legs; i++ {
		go func() {
			defer wg.Done()
			ok, err := repo.TryConsumeCampaignActBudget(ctx, task.ID)
			if err != nil {
				t.Errorf("并发扣减不应报错: %v", err)
				return
			}
			if ok {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if granted != 3 {
		t.Errorf("8 条腿抢 3 条预算，应恰好放行 3 条，实得 %d", granted)
	}
	var got model.BrowserTask
	if err := db.WithContext(ctx).First(&got, task.ID).Error; err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if got.CampaignActUsed != 3 {
		t.Errorf("并发下已用量应精确等于 3（超发=真打扰出去），实得 %d", got.CampaignActUsed)
	}
}

// 已用满额的任务在并发下同样不得被重新放行（restart 后 campaign_act_used 从库读回）。
func TestTryConsumeCampaignActBudgetRestartKeepsCap(t *testing.T) {
	repo, db := campaignBudgetRepo(t)
	task := seedBudgetTask(t, db, "重启后", "restart-1", 1, 1)
	ctx := context.Background()

	ok, err := repo.TryConsumeCampaignActBudget(ctx, task.ID)
	if err != nil || ok {
		t.Fatalf("额度已在库中打满，重启后也应拒发，实得 ok=%v err=%v", ok, err)
	}
}
