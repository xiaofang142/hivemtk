package channelgw

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsvc "hivemtk-user/internal/service"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// TestRuntimeWSPushIntervalReadsSeededKey 钉住 ws_push_interval 的读取 key 与种子登记一致。
//
// 这条是被 readpoints 门漏掉的一类故障：读取点确实存在，但它当时用的 key 是
// "ws_push_interval_default"，种子表里根本没有这个 key，于是 GetDuration 每次都 miss
// 然后回落到兜底值——运维在管理台改这个参数永远不生效。而 readpoints 门只看
// 「种子 key 有没有读取点」，看不出读取点用的是另一个 key。
//
// 真库 + 改值才能区分 key 对错：key 写错时这个用例会读到兜底值 2s 而非库里改的 7s。
func TestRuntimeWSPushIntervalReadsSeededKey(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{})
	require.NoError(t, appsvc.SeedConfigParams(t.Context(), db))
	svc := appsvc.NewConfigParamService(db)
	appsvc.SetGlobalForTest(svc)
	t.Cleanup(func() { appsvc.SetGlobalForTest(nil) })

	require.NoError(t, svc.UpdateValue(t.Context(), "channelgw", "ws_push_interval", "7", 1))
	require.Equal(t, 7*time.Second, runtimeWSPushIntervalDefault(t.Context()),
		"channelgw.ws_push_interval 改了却没生效：读取点用的 key 与种子登记对不上")
}

// TestChannelGWRuntimeKeysAreAllRegistered 把 channelgw 组所有直连读取点的 key 摆在一起，
// 任何一条读取点改 key 或新增读取点时都必须同步更新本测试，防止再次漂移。
func TestChannelGWRuntimeKeysAreAllRegistered(t *testing.T) {
	t.Parallel()
	// 顺序与 internal/channelgw/ws.go 顶部的 runtimeXxx 函数一一对应
	readers := []string{
		"ws_register_timeout",
		"ws_read_idle_timeout",
		"ws_write_timeout",
		"ws_push_interval",
		"ws_pipeline_timeout",
	}
	values := []time.Duration{
		runtimeWSRegisterTimeout(t.Context()),
		runtimeWSReadIdleTimeout(t.Context()),
		runtimeWSWriteTimeout(t.Context()),
		runtimeWSPushIntervalDefault(t.Context()),
		runtimeWSPipelineTimeout(t.Context()),
	}
	require.Len(t, values, len(readers), "读取函数数量与登记 key 数量对不上：有读取点没登记，或登记了没读取点")
	for i, k := range readers {
		require.Positive(t, values[i], "channelgw.%s 回落到非正值", k)
	}
}
