package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

type inferenceOutcomeDBStage struct {
	name, fault string
	timeout     bool
	calls       *[]string
}

func (s inferenceOutcomeDBStage) Name() string { return s.name }
func (s inferenceOutcomeDBStage) Execute(ctx context.Context, ic *agent_runtime.InferenceContext) agent_runtime.StageResult {
	*s.calls = append(*s.calls, s.name)
	if s.name == s.fault {
		ic.Decision.Reply = "不得保存为成功产出的回复"
		if s.timeout {
			<-ctx.Done()
			return agent_runtime.ContinueResult()
		}
		return agent_runtime.FailResult(errors.New("本地受控阶段故障"))
	}
	return (inferenceOutcomeHTTPStage{name: s.name}).Execute(ctx, ic)
}

// 生产适配器、服务和仓储连接真实槽位库；失败审计不得污染恢复产出或越过失败阶段。
func TestInferenceOutcomePostgresCheckpointRecovery(t *testing.T) {
	db := testutil.NewTestDB(t)
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS agent_checkpoints (
		tenant_id BIGINT NOT NULL DEFAULT 1,
		id BIGSERIAL PRIMARY KEY,
		thread_id VARCHAR(120) NOT NULL,
		stage VARCHAR(40) NOT NULL,
		state JSONB NOT NULL DEFAULT '{}',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT uk_agent_ckpt UNIQUE (tenant_id, thread_id, stage)
	)`).Error; err != nil {
		t.Fatalf("创建检查点测试表失败：%v", err)
	}
	t.Setenv("FF_LTC_CHECKPOINT", "1")
	repo := repository.NewAgentCheckpointRepository(db)
	store := &agentCheckpointStore{repo: repo}
	order := []string{"perception", "alignment", "gatekeeper", "planner", "reviewer"}
	for index, name := range order {
		for _, timeout := range []bool{false, true} {
			mode := map[bool]string{false: "error", true: "timeout"}[timeout]
			t.Run(name+"/"+mode, func(t *testing.T) {
				payload := agent_runtime.CustomerMessagePayload{ChannelType: "local", CustomerID: "checkpoint", SessionID: name + mode, Content: "本地检查点验收", MessageType: "text"}
				thread := agent_runtime.CheckpointThreadID(payload)
				var calls []string
				makeCycle := func(fault string) *agent_runtime.InferenceCycle {
					mk := func(stage string) agent_runtime.InferenceStage {
						return inferenceOutcomeDBStage{stage, fault, timeout, &calls}
					}
					cycle := agent_runtime.NewInferenceCycleWithStages(mk(order[0]), mk(order[1]), mk(order[2]), mk(order[3]))
					cycle.SetReviewerStage(mk(order[4]))
					cycle.SetCheckpointStore(store)
					cycle.StageTimeout = 20 * time.Millisecond
					return cycle
				}
				decision, err := makeCycle(name).RunOnce(context.Background(), payload, nil)
				if err == nil || decision == nil || decision.Outcome != agent_runtime.InferenceFailed || decision.FailedStage != name {
					t.Fatalf("故障未传播：decision=%+v err=%v", decision, err)
				}
				rows, err := repo.LoadAllByThread(context.Background(), thread)
				if err != nil || len(rows) != index {
					t.Fatalf("检查点越过失败阶段：rows=%d want=%d err=%v", len(rows), index, err)
				}
				if index > 0 {
					resume, blob, err := store.LoadResume(context.Background(), thread)
					var audit struct {
						Done        bool                             `json:"done"`
						LastAttempt *agent_runtime.InferenceDecision `json:"last_attempt"`
						Snapshot    struct {
							Stages   []agent_runtime.StageDecision   `json:"stages"`
							Decision agent_runtime.InferenceDecision `json:"decision"`
						} `json:"snapshot"`
					}
					if err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(blob, &audit); err != nil {
						t.Fatal(err)
					}
					if resume != name || audit.Done || !reflect.DeepEqual(audit.LastAttempt, decision) || len(audit.Snapshot.Stages) != index || audit.Snapshot.Decision.Reply != "" {
						t.Fatalf("失败审计与恢复游标不一致：resume=%s audit=%s", resume, blob)
					}
				}
				calls = nil
				decision, err = makeCycle("").RunOnce(context.Background(), payload, nil)
				if err != nil || decision.Outcome != agent_runtime.InferenceSucceeded || decision.Reply != "已复核回复" || !reflect.DeepEqual(calls, order[index:]) {
					t.Fatalf("恢复重放异常：calls=%v decision=%+v err=%v", calls, decision, err)
				}
				_, blob, err := store.LoadResume(context.Background(), thread)
				var completed struct {
					Done     bool `json:"done"`
					Snapshot struct {
						Decision agent_runtime.InferenceDecision `json:"decision"`
					} `json:"snapshot"`
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(blob, &completed); err != nil {
					t.Fatal(err)
				}
				if !completed.Done || !reflect.DeepEqual(completed.Snapshot.Decision, *decision) {
					t.Fatalf("成功终态未持久化：%s", blob)
				}
			})
		}
	}
}
