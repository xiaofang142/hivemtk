package repository

import (
	"context"
	"encoding/json"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// 直接使用生产仓储和真实 PostgreSQL；不依赖可切换的租户回调保护原始 Table 查询。
func TestCheckpointRepositoryExplicitTenantRed(t *testing.T) {
	db := testutil.NewTestDB(t, &model.ConfigParam{})
	if err := db.Exec(`CREATE TABLE agent_checkpoints (
		id bigserial PRIMARY KEY, tenant_id bigint NOT NULL DEFAULT 1,
		thread_id text NOT NULL, stage text NOT NULL, state jsonb NOT NULL,
		created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
		UNIQUE (tenant_id, thread_id, stage))`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO agent_checkpoints (tenant_id,thread_id,stage,state,created_at,updated_at)
		VALUES (773,'same-thread','planner','{"owner":773}',now(),now()),
		(774,'same-thread','planner','{"owner":774}',now(),now())`).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewAgentCheckpointRepository(db)
	ctx := tenant.WithTenantID(context.Background(), 773)
	rows, err := repo.LoadAllByThread(ctx, "same-thread")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || string(rows[0].State) != `{"owner": 773}` {
		t.Errorf("显式租户查询越界：rows=%+v", rows)
	}
	if _, err := repo.LoadAllByThread(context.Background(), "same-thread"); err == nil {
		t.Error("缺可信租户的检查点查询仍然放行")
	}
	if err := repo.Save(ctx, "new-thread", "perception", json.RawMessage(`{"owner":773}`)); err != nil {
		t.Fatal(err)
	}
	var owner uint
	if err := db.Table("agent_checkpoints").Where("thread_id = ?", "new-thread").Select("tenant_id").Scan(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if owner != 773 {
		t.Errorf("显式租户写入却落入其他租户：got=%d", owner)
	}
	// 即使上层带系统豁免，也只能读当前显式租户，写回不能碰相同 thread 的另一租户。
	system := tenant.WithSystemScope(ctx)
	rows, err = repo.LoadAllByThread(system, "same-thread")
	if err != nil || len(rows) != 1 || string(rows[0].State) != `{"owner": 773}` {
		t.Fatalf("系统标记绕过检查点租户：%+v %v", rows, err)
	}
	if err := repo.Save(system, "same-thread", "planner", json.RawMessage(`{"owner":773,"updated":true}`)); err != nil {
		t.Fatal(err)
	}
	other, err := repo.LoadAllByThread(tenant.WithTenantID(context.Background(), 774), "same-thread")
	if err != nil || len(other) != 1 || string(other[0].State) != `{"owner": 774}` {
		t.Fatalf("幂等更新污染另一租户：%+v %v", other, err)
	}
	for _, invalid := range []context.Context{context.Background(), tenant.WithSystemScope(context.Background())} {
		if err := repo.Save(invalid, "missing-scope", "planner", json.RawMessage(`{}`)); err == nil {
			t.Fatal("无可信租户写入被接受")
		}
		if _, err := repo.LoadAllByThread(invalid, "same-thread"); err == nil {
			t.Fatal("无可信租户读取被接受")
		}
	}
	for _, invalid := range []struct {
		name, thread, stage string
		repo                *AgentCheckpointRepository
	}{
		{"nil_receiver", "t", "planner", nil},
		{"nil_db", "t", "planner", NewAgentCheckpointRepository(nil)},
		{"empty_thread", "", "planner", repo},
		{"empty_stage", "t", "", repo},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			if err := invalid.repo.Save(ctx, invalid.thread, invalid.stage, json.RawMessage(`{}`)); err == nil {
				t.Fatal("无效存储/身份被静默接受")
			}
			if invalid.stage != "" {
				if _, err := invalid.repo.LoadAllByThread(ctx, invalid.thread); err == nil {
					t.Fatal("无效读取被静默接受")
				}
			}
		})
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := repo.Save(canceled, "cancelled", "planner", json.RawMessage(`{}`)); err == nil {
		t.Fatal("仓储剥离租户时丢失取消语义")
	}
	if _, err := repo.LoadAllByThread(canceled, "same-thread"); err == nil {
		t.Fatal("取消查询仍执行")
	}
}
