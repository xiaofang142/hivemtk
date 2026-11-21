package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/ops/model"
	"hivemtk-user/internal/ops/service"
	"hivemtk-user/internal/pkg/db"

	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupABExperimentTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&sysmodel.SystemUser{},
		&sysmodel.User{},
		&sysmodel.Account{},
		&model.ABExperiment{},
		&model.ABVariant{},
		&model.ABConversionEvent{},
		&model.ABExperimentResult{},
	)
	db.SetTestDB(database)
	return database
}

func setupABExperimentController(t *testing.T) (*ABExperimentController, *gin.Engine) {
	setupABExperimentTestDB(t)
	ctrl := NewABExperimentController()
	router := gin.New()

	router.Use(func(ctx *gin.Context) {
		ctx.Set("user_id", uint(1))
		ctx.Next()
	})

	return ctrl, router
}

// TestABExperimentController_CreateExperiment_Success 测试创建实验成功
func TestABExperimentController_CreateExperiment_Success(t *testing.T) {
	ctrl, router := setupABExperimentController(t)
	router.POST("/experiments", ctrl.CreateExperiment)

	createReq := service.CreateExperimentRequest{
		Name:         "Test Experiment",
		Description:  "Test Description",
		SourceType:   "page",
		SourceID:     "page_123",
		TrafficSplit: 50,
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/experiments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestABExperimentController_CreateExperiment_InvalidJSON 测试无效 JSON
func TestABExperimentController_CreateExperiment_InvalidJSON(t *testing.T) {
	ctrl, router := setupABExperimentController(t)
	router.POST("/experiments", ctrl.CreateExperiment)

	req, _ := http.NewRequest("POST", "/experiments", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestABExperimentController_CreateExperiment_MissingRequiredFields 测试缺少必填字段
func TestABExperimentController_CreateExperiment_MissingRequiredFields(t *testing.T) {
	ctrl, router := setupABExperimentController(t)
	router.POST("/experiments", ctrl.CreateExperiment)

	createReq := service.CreateExperimentRequest{
		Name: "",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/experiments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestABExperimentController_GetExperiment_Success 测试获取实验详情成功
func TestABExperimentController_GetExperiment_Success(t *testing.T) {
	database := setupABExperimentTestDB(t)
	ctrl := NewABExperimentController()
	router := gin.New()

	router.Use(func(ctx *gin.Context) {
		ctx.Set("user_id", uint(1))
		ctx.Next()
	})

	experiment := model.ABExperiment{
		Name:        "Test Experiment",
		Description: "Test Description",
		Status:      "draft",
		SourceType:  "page",
	}
	database.Create(&experiment)

	router.POST("/experiments", ctrl.CreateExperiment)
	router.GET("/experiments/:id", ctrl.GetExperiment)

	req, _ := http.NewRequest("GET", "/experiments/"+fmt.Sprintf("%d", experiment.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestABExperimentController_GetExperiment_InvalidID 测试无效实验 ID
func TestABExperimentController_GetExperiment_InvalidID(t *testing.T) {
	ctrl, router := setupABExperimentController(t)
	router.GET("/experiments/:id", ctrl.GetExperiment)

	req, _ := http.NewRequest("GET", "/experiments/invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestABExperimentController_GetExperiment_NotFound 测试实验不存在
func TestABExperimentController_GetExperiment_NotFound(t *testing.T) {
	ctrl, router := setupABExperimentController(t)
	router.GET("/experiments/:id", ctrl.GetExperiment)

	req, _ := http.NewRequest("GET", "/experiments/999999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status Not Found, got %d", w.Code)
	}
}

// TestABExperimentController_GetExperimentList_Success 测试获取实验列表成功
func TestABExperimentController_GetExperimentList_Success(t *testing.T) {
	database := setupABExperimentTestDB(t)
	ctrl, router := setupABExperimentController(t)

	experiments := []model.ABExperiment{
		{Name: "Experiment 1", Status: "running"},
		{Name: "Experiment 2", Status: "draft"},
		{Name: "Experiment 3", Status: "completed"},
	}
	for _, exp := range experiments {
		database.Create(&exp)
	}

	router.GET("/experiments", ctrl.GetExperimentList)

	req, _ := http.NewRequest("GET", "/experiments?page=1&page_size=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestABExperimentController_GetExperimentList_EmptyList 测试空实验列表
func TestABExperimentController_GetExperimentList_EmptyList(t *testing.T) {
	ctrl, router := setupABExperimentController(t)
	router.GET("/experiments", ctrl.GetExperimentList)

	req, _ := http.NewRequest("GET", "/experiments?page=1&page_size=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

// TestABExperimentController_UpdateExperiment_Success 测试更新实验成功
func TestABExperimentController_UpdateExperiment_Success(t *testing.T) {
	database := setupABExperimentTestDB(t)
	ctrl := NewABExperimentController()
	router := gin.New()

	router.Use(func(ctx *gin.Context) {
		ctx.Set("user_id", uint(1))
		ctx.Next()
	})

	experiment := model.ABExperiment{
		Name:        "Test Experiment",
		Description: "Test Description",
		Status:      "draft",
		SourceType:  "page",
		SourceID:    "page_123",
	}
	database.Create(&experiment)

	router.PUT("/experiments/:id", ctrl.UpdateExperiment)

	updateReq := service.CreateExperimentRequest{
		Name:         "Updated Experiment",
		Description:  "Updated Description",
		SourceType:   "component",
		SourceID:     "component_456",
		TrafficSplit: 60,
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", "/experiments/"+fmt.Sprintf("%d", experiment.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestABExperimentController_UpdateExperiment_InvalidID 测试无效实验 ID
func TestABExperimentController_UpdateExperiment_InvalidID(t *testing.T) {
	ctrl, router := setupABExperimentController(t)
	router.PUT("/experiments/:id", ctrl.UpdateExperiment)

	updateReq := service.CreateExperimentRequest{
		Name: "Updated Experiment",
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", "/experiments/invalid", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestABExperimentController_UpdateExperiment_InvalidJSON 测试无效 JSON
func TestABExperimentController_UpdateExperiment_InvalidJSON(t *testing.T) {
	database := setupABExperimentTestDB(t)
	ctrl, router := setupABExperimentController(t)

	experiment := model.ABExperiment{
		Name: "Test Experiment",
	}
	database.Create(&experiment)

	router.PUT("/experiments/:id", ctrl.UpdateExperiment)

	req, _ := http.NewRequest("PUT", "/experiments/1", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestABExperimentController_DeleteExperiment_Success 测试删除实验成功
func TestABExperimentController_DeleteExperiment_Success(t *testing.T) {
	database := setupABExperimentTestDB(t)
	ctrl, router := setupABExperimentController(t)

	experiment := model.ABExperiment{
		Name:   "Test Experiment",
		Status: "draft",
	}
	database.Create(&experiment)

	router.DELETE("/experiments/:id", ctrl.DeleteExperiment)

	req, _ := http.NewRequest("DELETE", "/experiments/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestABExperimentController_DeleteExperiment_InvalidID 测试无效实验 ID
func TestABExperimentController_DeleteExperiment_InvalidID(t *testing.T) {
	ctrl, router := setupABExperimentController(t)
	router.DELETE("/experiments/:id", ctrl.DeleteExperiment)

	req, _ := http.NewRequest("DELETE", "/experiments/invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestABExperimentController_StartExperiment_Success 测试启动实验成功
func TestABExperimentController_StartExperiment_Success(t *testing.T) {
	database := setupABExperimentTestDB(t)
	ctrl, router := setupABExperimentController(t)

	experiment := model.ABExperiment{
		Name:   "Test Experiment",
		Status: "draft",
	}
	database.Create(&experiment)

	router.POST("/experiments/:id/start", ctrl.StartExperiment)

	req, _ := http.NewRequest("POST", "/experiments/1/start", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestABExperimentController_StartExperiment_InvalidID 测试无效实验 ID
func TestABExperimentController_StartExperiment_InvalidID(t *testing.T) {
	ctrl, router := setupABExperimentController(t)
	router.POST("/experiments/:id/start", ctrl.StartExperiment)

	req, _ := http.NewRequest("POST", "/experiments/invalid/start", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestABExperimentController_PauseExperiment_Success 测试暂停实验成功
func TestABExperimentController_PauseExperiment_Success(t *testing.T) {
	database := setupABExperimentTestDB(t)
	ctrl, router := setupABExperimentController(t)

	experiment := model.ABExperiment{
		Name:   "Test Experiment",
		Status: "running",
	}
	database.Create(&experiment)

	router.POST("/experiments/:id/pause", ctrl.PauseExperiment)

	req, _ := http.NewRequest("POST", "/experiments/1/pause", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestABExperimentController_PauseExperiment_InvalidID 测试无效实验 ID
func TestABExperimentController_PauseExperiment_InvalidID(t *testing.T) {
	ctrl, router := setupABExperimentController(t)
	router.POST("/experiments/:id/pause", ctrl.PauseExperiment)

	req, _ := http.NewRequest("POST", "/experiments/invalid/pause", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestABExperimentController_StopExperiment_Success 测试停止实验成功
func TestABExperimentController_StopExperiment_Success(t *testing.T) {
	database := setupABExperimentTestDB(t)
	ctrl, router := setupABExperimentController(t)

	experiment := model.ABExperiment{
		Name:   "Test Experiment",
		Status: "running",
	}
	database.Create(&experiment)

	router.POST("/experiments/:id/stop", ctrl.StopExperiment)

	req, _ := http.NewRequest("POST", "/experiments/1/stop", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestABExperimentController_StopExperiment_InvalidID 测试无效实验 ID
func TestABExperimentController_StopExperiment_InvalidID(t *testing.T) {
	ctrl, router := setupABExperimentController(t)
	router.POST("/experiments/:id/stop", ctrl.StopExperiment)

	req, _ := http.NewRequest("POST", "/experiments/invalid/stop", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestABExperimentController_GetExperimentResults_Success 测试获取实验结果成功
func TestABExperimentController_GetExperimentResults_Success(t *testing.T) {
	database := setupABExperimentTestDB(t)
	ctrl, router := setupABExperimentController(t)

	experiment := model.ABExperiment{
		Name:   "Test Experiment",
		Status: "completed",
	}
	database.Create(&experiment)

	router.GET("/experiments/:id/results", ctrl.GetExperimentResults)

	req, _ := http.NewRequest("GET", "/experiments/1/results", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestABExperimentController_GetExperimentResults_InvalidID 测试无效实验 ID
func TestABExperimentController_GetExperimentResults_InvalidID(t *testing.T) {
	ctrl, router := setupABExperimentController(t)
	router.GET("/experiments/:id/results", ctrl.GetExperimentResults)

	req, _ := http.NewRequest("GET", "/experiments/invalid/results", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestABExperimentController_GetConversionEvents_Success 测试获取转化事件成功
func TestABExperimentController_GetConversionEvents_Success(t *testing.T) {
	database := setupABExperimentTestDB(t)
	ctrl, router := setupABExperimentController(t)

	experiment := model.ABExperiment{
		Name:   "Test Experiment",
		Status: "completed",
	}
	database.Create(&experiment)

	router.GET("/experiments/:id/conversions", ctrl.GetConversionEvents)

	req, _ := http.NewRequest("GET", "/experiments/1/conversions?page=1&page_size=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestABExperimentController_GetConversionEvents_InvalidID 测试无效实验 ID
func TestABExperimentController_GetConversionEvents_InvalidID(t *testing.T) {
	ctrl, router := setupABExperimentController(t)
	router.GET("/experiments/:id/conversions", ctrl.GetConversionEvents)

	req, _ := http.NewRequest("GET", "/experiments/invalid/conversions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestABExperimentController_CreateExperiment_WithAllFields 测试创建带所有字段的实验
func TestABExperimentController_CreateExperiment_WithAllFields(t *testing.T) {
	ctrl, router := setupABExperimentController(t)
	router.POST("/experiments", ctrl.CreateExperiment)

	now := time.Now().Format("2006-01-02")
	endDate := time.Now().AddDate(0, 0, 30).Format("2006-01-02")

	createReq := service.CreateExperimentRequest{
		Name:         "Complete Experiment",
		Description:  "Complete Description",
		SourceType:   "message",
		SourceID:     "message_456",
		TrafficSplit: 50,
		StartDate:    now,
		EndDate:      endDate,
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/experiments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestABExperimentController_CreateExperiment_WithDates 测试创建带日期设置的实验
func TestABExperimentController_CreateExperiment_WithDates(t *testing.T) {
	ctrl, router := setupABExperimentController(t)
	router.POST("/experiments", ctrl.CreateExperiment)

	now := time.Now().Format("2006-01-02")
	endDate := time.Now().AddDate(0, 0, 14).Format("2006-01-02")

	createReq := service.CreateExperimentRequest{
		Name:       "Dated Experiment",
		SourceType: "page",
		SourceID:   "page_789",
		StartDate:  now,
		EndDate:    endDate,
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/experiments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestABExperimentController_GetExperimentList_NormalizedPagination 验证回显分页与实际查询一致。
func TestABExperimentController_GetExperimentList_NormalizedPagination(t *testing.T) {
	ctrl, router := setupABExperimentController(t)
	var experiments []model.ABExperiment
	for i := 0; i < 205; i++ {
		experiments = append(experiments, model.ABExperiment{
			Name: fmt.Sprintf("pagination-%d", i), Status: "draft",
			CreatedAt: time.Unix(1700000000+int64(i), 0),
		})
	}
	if err := db.GetDB().Create(&experiments).Error; err != nil {
		t.Fatalf("创建实验夹具失败：%v", err)
	}
	router.GET("/experiments", ctrl.GetExperimentList)

	cases := []struct {
		name, query    string
		page, pageSize int
		count          int
	}{
		{"默认分页", "", 1, 10, 10},
		{"负数回落", "?page=-1&page_size=-2", 1, 10, 10},
		{"非数字回落", "?page=abc&page_size=abc", 1, 10, 10},
		{"超限钳制", "?page=1&page_size=201", 1, 200, 200},
		{"合法第二页", "?page=2&page_size=2", 2, 2, 2},
		{"末页", "?page=2&page_size=200", 2, 200, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/experiments"+tc.query, nil))

			var got struct {
				Code int `json:"code"`
				Data struct {
					Page     int                   `json:"page"`
					PageSize int                   `json:"page_size"`
					Total    int64                 `json:"total"`
					List     []*model.ABExperiment `json:"list"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("响应不是有效 JSON：%v；body=%s", err, w.Body.String())
			}
			if w.Code != http.StatusOK || got.Code != 0 {
				t.Fatalf("期望 HTTP 200/code=0，实际 HTTP %d，body=%s", w.Code, w.Body.String())
			}
			if got.Data.Page != tc.page || got.Data.PageSize != tc.pageSize || got.Data.Total != 205 || len(got.Data.List) != tc.count {
				t.Fatalf("期望 page=%d/page_size=%d/total=205/count=%d，实际 page=%d/page_size=%d/total=%d/count=%d", tc.page, tc.pageSize, tc.count, got.Data.Page, got.Data.PageSize, got.Data.Total, len(got.Data.List))
			}
			wantFirst := experiments[204-(tc.page-1)*tc.pageSize].ID
			if got.Data.List[0].ID != wantFirst {
				t.Fatalf("期望分页首条ID=%d，实际%d", wantFirst, got.Data.List[0].ID)
			}
		})
	}
}

// TestABExperimentController_GetConversionEvents_NormalizedPagination 验证事件分页回显有效查询参数。
func TestABExperimentController_GetConversionEvents_NormalizedPagination(t *testing.T) {
	ctrl, router := setupABExperimentController(t)
	experiment := &model.ABExperiment{Name: "event-pagination", Status: "draft"}
	if err := db.GetDB().Create(experiment).Error; err != nil {
		t.Fatalf("创建实验失败：%v", err)
	}
	var events []model.ABConversionEvent
	for i := 0; i < 205; i++ {
		events = append(events, model.ABConversionEvent{
			ExperimentID: experiment.ID, EventName: fmt.Sprintf("event-%d", i),
			CreatedAt: time.Unix(1700000000+int64(i), 0),
		})
	}
	if err := db.GetDB().Create(&events).Error; err != nil {
		t.Fatalf("创建转化事件失败：%v", err)
	}
	router.GET("/experiments/:id/conversion-events", ctrl.GetConversionEvents)

	cases := []struct {
		query          string
		page, pageSize int
		count          int
	}{
		{"", 1, 10, 10},
		{"?page=-1&page_size=-2", 1, 10, 10},
		{"?page=abc&page_size=abc", 1, 10, 10},
		{"?page=1&page_size=201", 1, 200, 200},
		{"?page=2&page_size=2", 2, 2, 2},
		{"?page=2&page_size=200", 2, 200, 5},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/experiments/%d/conversion-events%s", experiment.ID, tc.query), nil))

			var got struct {
				Code int `json:"code"`
				Data struct {
					Page     int                        `json:"page"`
					PageSize int                        `json:"page_size"`
					Total    int64                      `json:"total"`
					List     []*model.ABConversionEvent `json:"list"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("解析响应失败：%v；body=%s", err, w.Body.String())
			}
			if w.Code != http.StatusOK || got.Code != 0 || got.Data.Page != tc.page || got.Data.PageSize != tc.pageSize || got.Data.Total != 205 || len(got.Data.List) != tc.count {
				t.Fatalf("期望HTTP200/code0/page%d/size%d/total205/count%d，实际HTTP%d，body=%s", tc.page, tc.pageSize, tc.count, w.Code, w.Body.String())
			}
			wantFirst := events[204-(tc.page-1)*tc.pageSize].ID
			if got.Data.List[0].ID != wantFirst {
				t.Fatalf("期望事件分页首条ID=%d，实际%d", wantFirst, got.Data.List[0].ID)
			}
		})
	}
}

// TestABExperimentController_GetExperimentList_WithPagination 测试分页获取实验列表
func TestABExperimentController_GetExperimentList_WithPagination(t *testing.T) {
	database := setupABExperimentTestDB(t)
	ctrl, router := setupABExperimentController(t)

	for i := 0; i < 25; i++ {
		experiment := model.ABExperiment{
			Name:   "Experiment " + string(rune(i)),
			Status: "running",
		}
		database.Create(&experiment)
	}

	router.GET("/experiments", ctrl.GetExperimentList)

	req, _ := http.NewRequest("GET", "/experiments?page=2&page_size=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}
