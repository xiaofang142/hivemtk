package main

import (
	"encoding/json"
	"testing"
)

// 事件类型必须按脚本自述透传：服务端的写操作闸（waiting_confirm）只认 type=confirm。
// 这条用例钉住的是"曾经一律 action，导致 confirm 闸永远不触发"的回归。

func TestEventTypeFromPayloadPassesConfirm(t *testing.T) {
	cases := map[string]string{
		`{"type":"confirm","action":"comment"}`: "confirm",
		`{"type":"handoff","space":"x"}`:        "handoff",
		`{"type":"snapshot","refs":[]}`:         "snapshot",
		`{"type":"extract","items":[1]}`:        "extract",
		`{"type":"artifact","url":"s3://a"}`:    "artifact",
		`{"type":"action","ok":true}`:           "action",
		// C16 开始/结束帧：runner 组装器自产（作者手打在发布门被 EGO-R034 拦），
		// 必须按 asset 透传——落成 action 会让服务端拿不到脚本侧起止对齐点。
		`{"type":"asset","event":"start","execution_id":"e1"}`: "asset",
		`{"type":"asset","event":"end","ok":true}`:             "asset",
		`{"type":"log","msg":"随意"}`:                            "action", // 白名单外：不许凭空造状态迁移
		`{"ok":true}`:                                          "action", // 没写 type：按动作行
		`[1,2,3]`:                                              "action", // 非对象 JSON（数组/标量）
		`"just a string"`:                                      "action",
		`{`:                                                    "action", // 坏 JSON（不该发生，扫的已校验过）
	}
	for in, want := range cases {
		if got := eventTypeFromPayload(json.RawMessage(in)); got != want {
			t.Errorf("payload=%s 的类型是 %q，应为 %q", in, got, want)
		}
	}
}
