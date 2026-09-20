package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"

	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupAppConfigTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.SystemConfig{},
	)
	db.SetTestDB(database)
	return database
}

func TestAppConfigController_GetAppConfig_Success(t *testing.T) {
	setupAppConfigTestDB(t)
	gin.SetMode(gin.TestMode)
	ctrl := NewAppConfigController()
	router := gin.New()
	router.GET("/app/config", ctrl.GetAppConfig)

	req, _ := http.NewRequest("GET", "/app/config", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestAppConfigController_GetAppConfig_ReturnsConfig(t *testing.T) {
	setupAppConfigTestDB(t)
	gin.SetMode(gin.TestMode)
	ctrl := NewAppConfigController()
	router := gin.New()
	router.GET("/app/config", ctrl.GetAppConfig)

	req, _ := http.NewRequest("GET", "/app/config", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code SUCCESS, got %v", response["code"])
	}
}

func TestAppConfigController_UpdateAppConfig_Success(t *testing.T) {
	setupAppConfigTestDB(t)
	gin.SetMode(gin.TestMode)
	ctrl := NewAppConfigController()
	router := gin.New()
	router.PUT("/app/config", ctrl.UpdateAppConfig)

	updateReq := AppConfigReq{
		BasicConfig: BasicConfig{
			AppName:        "Test App",
			Version:        "1.0.0",
			Environment:    "test",
			DebugMode:      true,
			SessionTimeout: 3600,
		},
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", "/app/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestAppConfigController_UpdateAppConfig_InvalidJSON(t *testing.T) {
	setupAppConfigTestDB(t)
	gin.SetMode(gin.TestMode)
	ctrl := NewAppConfigController()
	router := gin.New()
	router.PUT("/app/config", ctrl.UpdateAppConfig)

	req, _ := http.NewRequest("PUT", "/app/config", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

func TestAppConfigController_SyncWithPlatform_Success(t *testing.T) {
	setupAppConfigTestDB(t)
	gin.SetMode(gin.TestMode)
	ctrl := NewAppConfigController()
	router := gin.New()
	router.POST("/app/config/sync", ctrl.SyncWithPlatform)

	req, _ := http.NewRequest("POST", "/app/config/sync", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError && w.Code != http.StatusBadRequest {
		t.Errorf("Expected status OK, Bad Request or Internal Server Error, got %d", w.Code)
	}
}

func TestAppConfigController_HealthCheck_Success(t *testing.T) {
	setupAppConfigTestDB(t)
	gin.SetMode(gin.TestMode)
	ctrl := NewAppConfigController()
	router := gin.New()
	router.GET("/app/config/health", ctrl.HealthCheck)

	req, _ := http.NewRequest("GET", "/app/config/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestAppConfigController_HealthCheck_ReturnsStatus(t *testing.T) {
	setupAppConfigTestDB(t)
	gin.SetMode(gin.TestMode)
	ctrl := NewAppConfigController()
	router := gin.New()
	router.GET("/app/config/health", ctrl.HealthCheck)

	req, _ := http.NewRequest("GET", "/app/config/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code SUCCESS, got %v", response["code"])
	}
}

func TestAppConfigController_NewAppConfigController(t *testing.T) {
	ctrl := NewAppConfigController()
	if ctrl == nil {
		t.Error("Expected controller instance, got nil")
	}
}

// withPlatformCfg 替换进程级平台配置并在用例结束时还原。
// 控制器读的是 config.PlatformCfg 这个包级指针，不还原会污染同包其余用例。
func withPlatformCfg(t *testing.T, cfg *config.PlatformConfig) {
	t.Helper()
	orig := config.PlatformCfg
	config.PlatformCfg = cfg
	t.Cleanup(func() { config.PlatformCfg = orig })
}

// TestAppConfigController_SyncWithPlatform_ReportsDegradeReason 私域部署里"平台没接"是常态，
// "接了但挂了"才是故障：两者都只回 platform_available=false 时，运维会去查一个并不存在的故障，
// 前端也会把未配置显示成平台异常。降级原因必须分开回给调用方。
func TestAppConfigController_SyncWithPlatform_ReportsDegradeReason(t *testing.T) {
	setupAppConfigTestDB(t)
	gin.SetMode(gin.TestMode)
	ctrl := NewAppConfigController()
	router := gin.New()
	router.POST("/app/config/sync", ctrl.SyncWithPlatform)

	for _, tc := range []struct {
		name       string
		cfg        *config.PlatformConfig
		want       string
		wantMsgSub string
	}{
		{"未配置平台", nil, "not_configured", "未接入平台"},
		{"配置指向不可达平台", &config.PlatformConfig{APIURL: "http://127.0.0.1:1", Secret: "s"}, "unreachable", "平台已跳过"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withPlatformCfg(t, tc.cfg)
			req, _ := http.NewRequest("POST", "/app/config/sync", bytes.NewReader([]byte(`{}`)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("状态码=%d body=%s", w.Code, w.Body.String())
			}
			var out struct {
				Data struct {
					Extra map[string]any `json:"extra"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatalf("响应解析失败: %v body=%s", err, w.Body.String())
			}
			if got := out.Data.Extra["platform_available"]; got != false {
				t.Errorf("platform_available=%v want false", got)
			}
			if got := out.Data.Extra["platform_reason"]; got != tc.want {
				t.Errorf("platform_reason=%v want %q", got, tc.want)
			}
			var msg struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &msg); err != nil {
				t.Fatalf("响应解析失败: %v", err)
			}
			if !strings.Contains(msg.Message, tc.wantMsgSub) {
				t.Errorf("message=%q 应含 %q（两种降级原因不得回同一句文案）", msg.Message, tc.wantMsgSub)
			}
		})
	}
}

// TestAppConfigController_HealthCheck_DistinguishesNotConfigured 健康检查同理：
// 未配置平台的实例不该显示成"平台断开"。
func TestAppConfigController_HealthCheck_DistinguishesNotConfigured(t *testing.T) {
	setupAppConfigTestDB(t)
	gin.SetMode(gin.TestMode)
	ctrl := NewAppConfigController()
	router := gin.New()
	router.GET("/app/config/health", ctrl.HealthCheck)

	withPlatformCfg(t, nil)
	req, _ := http.NewRequest("GET", "/app/config/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应解析失败: %v body=%s", err, w.Body.String())
	}
	if got := out.Data["platform_connection"]; got != "not_configured" {
		t.Errorf("platform_connection=%v want not_configured", got)
	}
}

// livenessOnlyPlatform 复刻开源平台的真实路由集：只有 /health 存活，其余路径 404。
// /merchant-api/license/status 就是"其余路径"之一 —— 平台从未实现它（R12）。
func livenessOnlyPlatform(t *testing.T) *httptest.Server {
	t.Helper()
	paths := make(chan string, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.Path
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"status":"alive","timestamp":1700000000}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestAppConfigController_LivenessOnlyPlatformReportsConnected 钉的是 R12 本体：
// 平台明明健康（存活性端点 200）时三处探针都必须报 connected。
// 修复前它们打 /merchant-api/license/status 拿 404 ⇒ platform_connection=unreachable、
// sync 判"平台不可达"，即把一个健康平台长期报成故障，并每请求刷一条 Error 日志。
func TestAppConfigController_LivenessOnlyPlatformReportsConnected(t *testing.T) {
	setupAppConfigTestDB(t)
	gin.SetMode(gin.TestMode)
	srv := livenessOnlyPlatform(t)
	withPlatformCfg(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s"})
	t.Setenv("MERCHANT_API_SECRET", "s")

	t.Run("健康检查", func(t *testing.T) {
		router := gin.New()
		router.GET("/app/config/health", NewAppConfigController().HealthCheck)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/app/config/health", nil))

		var out struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("响应解析失败: %v body=%s", err, w.Body.String())
		}
		if got := out.Data["platform_connection"]; got != "connected" {
			t.Errorf("platform_connection=%v want connected（可达平台被判故障即 R12）", got)
		}
	})

	t.Run("配置同步", func(t *testing.T) {
		router := gin.New()
		router.POST("/app/config/sync", NewAppConfigController().SyncWithPlatform)
		req := httptest.NewRequest("POST", "/app/config/sync", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		var out struct {
			Data struct {
				Extra map[string]any `json:"extra"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("响应解析失败: %v body=%s", err, w.Body.String())
		}
		if got := out.Data.Extra["platform_available"]; got != true {
			t.Errorf("platform_available=%v want true，reason=%v", got, out.Data.Extra["platform_reason"])
		}
	})
}
