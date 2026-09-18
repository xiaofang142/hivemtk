// sop_compensation_inventory_test.go T-P1-03：补偿语义的**穷尽性**契约。
//
// 卡面写的是"给 StartExecutor/EndExecutor/ConditionExecutor/NoopExecutor 补 Compensate，
// 6/6 执行器实现 Compensate"。实跑核查后按证据改写落点（与 T-P1-01 换址同理）：
// 那四类**没有可撤销状态**（只写时间戳/分支标签等留痕），给它们加空 Compensate 会把
// 补偿记录从 `skipped`（无副作用）变成 `completed`（撤销已完成），是对同一次失败的谎报；
// 而 `Compensable` 的类型断言本就是管理器区分"有回滚"与"无事可滚"的唯一依据，
// 一旦人人实现就失去信息量。
//
// 真正缺的是两处，本文件把它们变成可执行契约：
//
//	① 12 个消息类节点确有副作用却整个不补偿（原 :681 记 TODO）——现已实现**部分补偿**
//	   （清 ExecutionData 话术产物；出域消息不撤、幂等键不删），见 MessageNodeBase.Compensate；
//	② 其余类型"为何不撤"只活在注释里，补偿记录上 start 与"消息没撤回"长得一模一样——
//	   现由 CompensationNoter 自述并随记录落进可观测输出。
package service

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// allSOPNodeTypes RegisterAllNodeExecutors 注册的全部类型（19 个）。
// 新增/删除注册项时此表必须同步改，否则本文件的分区断言先红——这是有意的摩擦。
var allSOPNodeTypes = []string{
	SOPNodeTypeStart, SOPNodeTypeEnd, SOPNodeTypeWait,
	SOPNodeTypeLLM, SOPNodeTypeAIDecide,
	SOPNodeTypeCondition, SOPNodeTypeBranch,
	SOPNodeTypeGreeting, SOPNodeTypeInquire, SOPNodeTypeIntroduce,
	SOPNodeTypeHandle, SOPNodeTypeClose, SOPNodeTypeInvite,
	SOPNodeTypeFollowUp, SOPNodeTypeActivate, SOPNodeTypeNurture,
	SOPNodeTypeMessage, SOPNodeTypeAction, SOPNodeTypeSendOffer,
}

// compensableSOPNodeTypes 会真正下发撤销动作的类型。
var compensableSOPNodeTypes = map[string]bool{
	SOPNodeTypeLLM:       true,
	SOPNodeTypeAIDecide:  true,
	SOPNodeTypeWait:      true,
	SOPNodeTypeGreeting:  true, // 部分补偿（T-P1-03）
	SOPNodeTypeInquire:   true,
	SOPNodeTypeIntroduce: true,
	SOPNodeTypeHandle:    true,
	SOPNodeTypeClose:     true,
	SOPNodeTypeInvite:    true,
	SOPNodeTypeFollowUp:  true,
	SOPNodeTypeActivate:  true,
	SOPNodeTypeNurture:   true,
	SOPNodeTypeMessage:   true,
	SOPNodeTypeAction:    true,
	SOPNodeTypeSendOffer: true,
}

func TestNodeExecutor_CompensationInventory(t *testing.T) {
	ctx := context.Background()
	reg := NewNodeExecutorRegistry()
	RegisterAllNodeExecutors(reg, &SOPNodeExecutorDeps{})

	registered := append([]string(nil), reg.AllRegistered(ctx)...)
	sort.Strings(registered)
	want := append([]string(nil), allSOPNodeTypes...)
	sort.Strings(want)
	if strings.Join(registered, ",") != strings.Join(want, ",") {
		t.Fatalf("注册类型集合与本契约表不一致（新增节点类型必须同步登记补偿语义）\n got %v\nwant %v", registered, want)
	}

	for _, nodeType := range registered {
		executor := reg.MustGet(ctx, nodeType)
		_, compensable := executor.(Compensable)
		note := compensationNote(executor)

		if compensable != compensableSOPNodeTypes[nodeType] {
			t.Errorf("%s 可补偿性与登记不符: got %v want %v", nodeType, compensable, compensableSOPNodeTypes[nodeType])
		}
		// 核心不变式：不存在"既无可撤销动作、又没自述理由"的沉默类型。
		if !compensable && note == "" {
			t.Errorf("%s 既未实现 Compensable 也未实现 CompensationNote ⇒ 失败后留下什么无人知晓", nodeType)
		}
	}

	// 控制流类必须落到 skipped 且有理由（逐个点名，防止被"顺手加个空 Compensate"混成 completed）
	for _, nodeType := range []string{SOPNodeTypeStart, SOPNodeTypeEnd, SOPNodeTypeCondition, SOPNodeTypeBranch} {
		rec := NewCompensationManager(DefaultCompensationConfig()).CompensateNode(
			ctx,
			&ExecutionContext{Execution: &model.SOPExecution{}, Node: &dto.SOPNode{ID: "n", Type: nodeType}},
			reg.MustGet(ctx, nodeType),
		)
		if rec.Status != CompensationStatusSkipped {
			t.Errorf("%s 应为 skipped（无可撤销状态）, got %q", nodeType, rec.Status)
		}
		if rec.Reason == "" {
			t.Errorf("%s 的 skipped 必须自带理由，否则与'有副作用但没撤回'无法区分", nodeType)
		}
	}

	// 未注册类型走 Noop 兜底：同样要有理由（且这条理由自带风险披露）
	noopRec := NewCompensationManager(DefaultCompensationConfig()).CompensateNode(
		ctx,
		&ExecutionContext{Execution: &model.SOPExecution{}, Node: &dto.SOPNode{ID: "n", Type: "not_a_real_type"}},
		reg.MustGet(ctx, "not_a_real_type"),
	)
	if noopRec.Status != CompensationStatusSkipped || noopRec.Reason == "" {
		t.Errorf("Noop 兜底应有 skipped + 理由, got %+v", noopRec)
	}

	// 沉默执行器（既不可补偿也无声明）不得留空字段：默认文案要顶上
	silent := NewCompensationManager(DefaultCompensationConfig()).CompensateNode(
		ctx,
		&ExecutionContext{Execution: &model.SOPExecution{}, Node: &dto.SOPNode{ID: "n", Type: "silent"}},
		&mockNonCompensableExecutor{nodeType: "silent"},
	)
	if silent.Status != CompensationStatusSkipped || !strings.Contains(silent.Reason, "未声明") {
		t.Errorf("未声明理由的执行器应有兜底文案, got %+v", silent)
	}
}

// TestMessageNodeCompensate_PartialRollback 消息节点的补偿边界：撤内部产物，
// 但出域幂等键必须原样保留——删了它，重跑会对同一节点二次发送。
func TestMessageNodeCompensate_PartialRollback(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SOPExecution{})
	ctx := context.Background()

	exec := &model.SOPExecution{
		SOPID:       1,
		CustomerID:  "c1",
		Status:      SOPStatusRunning,
		CurrentNode: "g1",
	}
	if err := db.WithContext(ctx).Create(exec).Error; err != nil {
		t.Fatalf("建执行失败: %v", err)
	}
	// 幂等键按 {execID}:{nodeID} 组，故必须等拿到真 ID 后再写。
	data := model.JSONMap{
		"_greeting_content": "您好",
		"_greeting_source":  "prompt",
		"_inquire_content":  "另一节点产物",
		"_side_effects":     []any{"message_sent:" + strconv.FormatUint(uint64(exec.ID), 10) + ":g1"},
	}
	if err := db.Model(&model.SOPExecution{}).Where("id = ?", exec.ID).
		Update("execution_data", data).Error; err != nil {
		t.Fatalf("回写执行数据失败: %v", err)
	}

	reg := NewNodeExecutorRegistry()
	RegisterAllNodeExecutors(reg, &SOPNodeExecutorDeps{DB: db})
	executor := reg.MustGet(ctx, SOPNodeTypeGreeting)
	if _, ok := executor.(Compensable); !ok {
		t.Fatal("消息节点应实现 Compensable（部分补偿）")
	}

	mgr := NewCompensationManager(DefaultCompensationConfig())
	execCtx := &ExecutionContext{
		Execution: exec,
		Node:      &dto.SOPNode{ID: "g1", Type: SOPNodeTypeGreeting},
	}
	rec := mgr.CompensateNode(ctx, execCtx, executor)
	if rec.Status != CompensationStatusCompleted {
		t.Fatalf("消息节点补偿应 completed, got %+v", rec)
	}
	if !strings.Contains(rec.Reason, "出域") || !strings.Contains(rec.Reason, "message_sent") {
		t.Errorf("补偿记录应自带边界自述, got %q", rec.Reason)
	}

	after := reloadExecutionData(t, db, exec.ID)
	if _, ok := after["_greeting_content"]; ok {
		t.Error("本节点话术产物应被清除")
	}
	if _, ok := after["_greeting_source"]; ok {
		t.Error("本节点来源标记应被清除")
	}
	if _, ok := after["_inquire_content"]; !ok {
		t.Error("别的节点类型产物不得被越界清除")
	}
	if _, ok := after["_side_effects"]; !ok {
		t.Fatal("message_sent 幂等键必须保留：删掉会让重跑二次发送")
	}

	// 幂等：重复补偿既不改数据、也不产生第二次改写（用 updated_at 做证据）
	var before model.SOPExecution
	if err := db.First(&before, exec.ID).Error; err != nil {
		t.Fatalf("重读执行失败: %v", err)
	}
	second := mgr.CompensateNode(ctx, execCtx, executor)
	if second.Status != CompensationStatusCompleted {
		t.Errorf("重复补偿应仍成功（幂等）, got %+v", second)
	}
	var again model.SOPExecution
	if err := db.First(&again, exec.ID).Error; err != nil {
		t.Fatalf("重读执行失败: %v", err)
	}
	if !again.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("重复补偿仍回写了行（updated_at %v → %v），非幂等", before.UpdatedAt, again.UpdatedAt)
	}
	if len(again.ExecutionData) != len(after) {
		t.Errorf("重复补偿改写了数据: before %d after %d", len(after), len(again.ExecutionData))
	}
}

func reloadExecutionData(t *testing.T, db *gorm.DB, id uint) model.JSONMap {
	t.Helper()
	var fresh model.SOPExecution
	if err := db.First(&fresh, id).Error; err != nil {
		t.Fatalf("重读执行失败: %v", err)
	}
	return fresh.ExecutionData
}

// TestMessageNodeCompensate_DegradesWithoutDB 无库直构时补偿"无事可做"必须报成功，
// 不能报成补偿失败——否则 Summary 的 failed 数会被部署形态污染。
func TestMessageNodeCompensate_DegradesWithoutDB(t *testing.T) {
	ctx := context.Background()
	reg := NewNodeExecutorRegistry()
	RegisterAllNodeExecutors(reg, &SOPNodeExecutorDeps{DB: nil})

	rec := NewCompensationManager(DefaultCompensationConfig()).CompensateNode(
		ctx,
		&ExecutionContext{
			Execution: &model.SOPExecution{ID: 999},
			Node:      &dto.SOPNode{ID: "g1", Type: SOPNodeTypeGreeting},
		},
		reg.MustGet(ctx, SOPNodeTypeGreeting),
	)
	if rec.Status != CompensationStatusCompleted {
		t.Errorf("无库降级应 completed, got %+v", rec)
	}
}
