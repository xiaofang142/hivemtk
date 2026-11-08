package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/model"

	"github.com/gin-gonic/gin"
)

// #968 回归：POST /api/system/config 部分保存（只传 {site_name,theme_color}）
// 在整行回写模型下曾把 enable_register/enable_rag 静默清成 false（零值 bool
// 无法区分"没传"和"传 false"）。修复后缺键的 bool/int 字段从既有行取值，
// 显式传 false 仍生效。种子行走 GET 前先落库（与线上复现同形状）。
func TestSystemConfigController_SaveConfig_PartialKeepsBooleans(t *testing.T) {
	ctrl, router := setupSystemConfigControllerWithSeed(t, `{"enable_register":true,"enable_email_marketing":true,"enable_rag":true,"max_upload_size_mb":200}`)
	router.POST("/system/config", ctrl.SaveConfig)

	req, _ := http.NewRequest("POST", "/system/config",
		bytes.NewBufferString(`{"site_name":"x","theme_color":"#409EFF"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("部分保存应 200，实际 %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if v, _ := resp.Data["enable_register"].(bool); !v {
		t.Errorf("enable_register 缺键必须保留 true，实际 %v（#968 静默清 false）", resp.Data["enable_register"])
	}
	if v, _ := resp.Data["enable_rag"].(bool); !v {
		t.Errorf("enable_rag 缺键必须保留 true，实际 %v", resp.Data["enable_rag"])
	}
	if v, _ := resp.Data["max_upload_size_mb"].(float64); int(v) != 200 {
		t.Errorf("max_upload_size_mb 缺键必须保留 200，实际 %v", resp.Data["max_upload_size_mb"])
	}
	if resp.Data["site_name"] != "x" {
		t.Errorf("site_name 应更新为 x，实际 %v", resp.Data["site_name"])
	}
}

// 显式传 false 是合法的关闭动作：守卫不得把它翻回 true。
func TestSystemConfigController_SaveConfig_ExplicitFalseRespected(t *testing.T) {
	ctrl, router := setupSystemConfigControllerWithSeed(t, `{"enable_register":true,"enable_rag":true}`)
	router.POST("/system/config", ctrl.SaveConfig)

	req, _ := http.NewRequest("POST", "/system/config",
		bytes.NewBufferString(`{"site_name":"y","enable_register":false}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("显式关闭应 200，实际 %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if v, _ := resp.Data["enable_register"].(bool); v {
		t.Errorf("显式 enable_register=false 必须生效，实际 %v", resp.Data["enable_register"])
	}
	if v, _ := resp.Data["enable_rag"].(bool); !v {
		t.Errorf("enable_rag 缺键仍应保留 true，实际 %v", resp.Data["enable_rag"])
	}
}

// setupSystemConfigControllerWithSeed：建控制器+路由并预置一行配置（JSON 形状与 API 相同）。
func setupSystemConfigControllerWithSeed(t *testing.T, seedJSON string) (*SystemConfigController, *gin.Engine) {
	database := setupSystemConfigTestDB(t)
	seed := &model.SystemConfig{}
	if err := json.Unmarshal([]byte(seedJSON), seed); err != nil {
		t.Fatalf("seed json: %v", err)
	}
	// 绑定零值默认：开启默认开关（与列 default 对齐）
	seed.EnableRegister = seed.EnableRegister || containsTrue(seedJSON, "enable_register")
	if err := database.Create(seed).Error; err != nil {
		t.Fatalf("seed create: %v", err)
	}
	ctrl := NewSystemConfigController()
	router := gin.New()
	return ctrl, router
}

func containsTrue(raw, field string) bool {
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return false
	}
	v, _ := m[field].(bool)
	return v
}
