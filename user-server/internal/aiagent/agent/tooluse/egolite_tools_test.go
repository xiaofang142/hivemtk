package tooluse

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/datatypes"
)

// egolite_run 工具（specs/016 S11）的工具面语义：
//  ① flow/asset 二选一；flow 归属=流程创建者（browser_open_task 同款）；
//  ② 扫描门前置（红流程即时反馈，不异步静默）；写流程强制 confirm=require（决策 4）；
//  ③ market 通道必须显式 user_id（资产是全局目录，执行绑定到该用户的本机 runner）。

// egoToolFlowIDs 由 newEgoToolEnv 填充（自增 ID 不做 1/2/3 假设）
var egoToolFlowIDs [3]uint

func newEgoToolEnv(t *testing.T) *EgoLiteRunTool {
	t.Helper()
	db := testutil.NewTestDB(t, &model.EgoliteFlow{})
	if db == nil {
		t.Fatal("测试库不可用")
	}
	rows := []*model.EgoliteFlow{
		{UserID: 21, Name: "良性读流", Status: model.EgoFlowDraft, ScanPassed: true, WritesJSON: datatypes.JSON(`[]`), Script: "console.log(1)"},
		{UserID: 22, Name: "红流程", Status: model.EgoFlowDraft, ScanPassed: false, WritesJSON: datatypes.JSON(`[]`), Script: "console.log(1)"},
		{UserID: 23, Name: "写流", Status: model.EgoFlowDraft, ScanPassed: true, WritesJSON: datatypes.JSON(`["comment"]`), Script: "console.log(1)"},
	}
	for _, r := range rows {
		if err := db.Create(r).Error; err != nil {
			t.Fatal(err)
		}
	}
	egoToolFlowIDs = [3]uint{rows[0].ID, rows[1].ID, rows[2].ID}
	return NewEgoLiteRunTool(BrowserToolDeps{DB: db})
}

func TestEgoLiteRunToolFlowChannel(t *testing.T) {
	tool := newEgoToolEnv(t)
	// 派发是异步 goroutine：fake runner 用 channel 与测试同步（带超时防挂死）
	fired := make(chan struct{}, 8)
	var gotUser uint
	var gotReq EgoLiteRunRequest
	SetEgoLiteRunner(func(_ context.Context, userID uint, req EgoLiteRunRequest) (map[string]any, error) {
		gotUser = userID
		gotReq = req
		fired <- struct{}{}
		return map[string]any{"execution_id": uint(1)}, nil
	})
	defer SetEgoLiteRunner(nil)
	waitFired := func(t *testing.T) {
		t.Helper()
		select {
		case <-fired:
		case <-time.After(3 * time.Second):
			t.Fatal("异步派发未在 3s 内到达 fake runner")
		}
	}

	// 二选一必填
	res, _ := tool.Execute(context.Background(), map[string]any{})
	if res.Success {
		t.Error("缺参必须即时反馈")
	}

	// 良性流程：归属=创建者，confirm=none
	res, _ = tool.Execute(context.Background(), map[string]any{"flow_id": int64(egoToolFlowIDs[0])})
	if !res.Success {
		t.Fatalf("良性流程应派发成功: %+v", res)
	}
	waitFired(t)
	if gotUser != 21 || gotReq.FlowID != egoToolFlowIDs[0] || gotReq.ConfirmPolicy != "none" {
		t.Errorf("归属/参数不符: user=%d req=%+v", gotUser, gotReq)
	}

	// 写流程：confirm=require（客户端不可关闭）
	_, _ = tool.Execute(context.Background(), map[string]any{"flow_id": int64(egoToolFlowIDs[2])})
	waitFired(t)
	if gotReq.ConfirmPolicy != "require" {
		t.Errorf("写流程必须 require，got %q", gotReq.ConfirmPolicy)
	}

	// 红流程：扫描门即时反馈
	res, _ = tool.Execute(context.Background(), map[string]any{"flow_id": int64(egoToolFlowIDs[1])})
	if res.Success {
		t.Error("扫描门未通过的流程必须被拒")
	}
}

func TestEgoLiteRunToolMarketChannel(t *testing.T) {
	tool := newEgoToolEnv(t)
	fired := make(chan struct{}, 8)
	var gotUser uint
	var captured EgoLiteRunRequest
	SetEgoLiteRunner(func(_ context.Context, userID uint, req EgoLiteRunRequest) (map[string]any, error) {
		gotUser = userID
		captured = req
		fired <- struct{}{}
		return nil, nil
	})
	defer SetEgoLiteRunner(nil)
	waitFired := func(t *testing.T) {
		t.Helper()
		select {
		case <-fired:
		case <-time.After(3 * time.Second):
			t.Fatal("异步派发未在 3s 内到达 fake runner")
		}
	}

	// market 无 user_id：拒绝
	res, _ := tool.Execute(context.Background(), map[string]any{"asset_id": "pack-demo"})
	if res.Success {
		t.Error("market 通道缺 user_id 必须被拒（执行绑定到该用户的本机 runner）")
	}
	// 带 user_id：派发
	res, _ = tool.Execute(context.Background(), map[string]any{"asset_id": "pack-demo", "user_id": int64(31)})
	waitFired(t)
	if !res.Success || gotUser != 31 {
		t.Errorf("market 通道带 user_id 应派发: res=%+v user=%d", res, gotUser)
	}
	// params 透传
	_, _ = tool.Execute(context.Background(), map[string]any{
		"flow_id": int64(egoToolFlowIDs[0]), "params": map[string]any{"keyword": "ego"}, "space_name": "ag",
	})
	waitFired(t)
	if string(captured.Params) == "" || !json.Valid(captured.Params) {
		t.Errorf("params 应序列化透传: %q", string(captured.Params))
	}
	if captured.SpaceName != "ag" {
		t.Errorf("space_name 应透传: %q", captured.SpaceName)
	}
}
