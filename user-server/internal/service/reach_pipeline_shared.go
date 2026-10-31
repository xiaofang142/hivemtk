package service

// reach_pipeline_shared.go 收敛 reach 触达域流水线文件间的共享小工具：
// JSON 序列化辅助与东八区时区定义（reach_pipeline* 与 reach_send_pipeline* 共用）。

import (
	"encoding/json"
	"time"

	"hivemtk-user/internal/model"
)

func toJSONArray(data []byte) model.JSONArray {
	if len(data) == 0 {
		return model.JSONArray{}
	}
	var arr []any
	if err := json.Unmarshal(data, &arr); err != nil {
		return model.JSONArray{}
	}
	return model.JSONArray(arr)
}

// cstZone 触达域统一使用的东八区时区（CST, UTC+8）。
var cstZone = time.FixedZone("CST", 8*3600)

func cstLoc() *time.Location {
	return time.FixedZone("CST", 8*3600)
}
