package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupWorkflowTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDBOrSkip(t,
		&model.WorkflowVersion{},
		&model.WorkflowExecution{},
		&model.WorkflowNodeExecution{},
	)
	if database == nil {
		t.Skip("PostgreSQL 测试库不可达，跳过测试")
		return nil
	}
	db.SetTestDB(database)
	return database
}

func setupWorkflowOrchestratorController(database *gorm.DB) *WorkflowOrchestratorController {
	versionRepo := repository.NewWorkflowVersionRepository(database)
	execRepo := repository.NewWorkflowExecutionRepository(database)
	nodeExecRepo := repository.NewWorkflowNodeExecutionRepository(database)
	svc := service.NewWorkflowOrchestratorService(versionRepo, execRepo, nodeExecRepo)
	return NewWorkflowOrchestratorController(svc)
}

func setupWorkflowGinEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

func TestWorkflowOrchestratorController_CreateVersion(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)
	router := setupWorkflowGinEngine()
	router.POST("/workflows/versions", ctrl.CreateVersion)

	req := dto.WorkflowVersionCreateRequest{
		WorkflowID:  "wf-test-001",
		Name:        "Test Workflow v1",
		Description: "Test workflow description",
		Definition: map[string]any{
			"nodes": []any{
				map[string]any{"id": "n1", "type": "trigger", "name": "Start"},
				map[string]any{"id": "n2", "type": "action", "name": "Log Message", "config": map[string]any{"action_type": "log"}},
			},
			"edges": []any{
				map[string]any{"source": "n1", "target": "n2"},
			},
		},
		CreatedBy: "test-user",
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("POST", "/workflows/versions", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，得到 %d，响应: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("响应中缺少 data 字段")
	}
	if data["workflow_id"] != "wf-test-001" {
		t.Errorf("期望 workflow_id 为 wf-test-001，得到 %v", data["workflow_id"])
	}
	if data["version"].(float64) != 1 {
		t.Errorf("期望 version 为 1，得到 %v", data["version"])
	}
	if data["status"] != "draft" {
		t.Errorf("期望 status 为 draft，得到 %v", data["status"])
	}
}

func TestWorkflowOrchestratorController_CreateVersion_InvalidJSON(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)
	router := setupWorkflowGinEngine()
	router.POST("/workflows/versions", ctrl.CreateVersion)

	httpReq, _ := http.NewRequest("POST", "/workflows/versions", bytes.NewReader([]byte("invalid-json")))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("期望状态码 400，得到 %d", w.Code)
	}
}

func TestWorkflowOrchestratorController_CreateVersion_MissingRequired(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)
	router := setupWorkflowGinEngine()
	router.POST("/workflows/versions", ctrl.CreateVersion)

	req := dto.WorkflowVersionCreateRequest{
		WorkflowID: "wf-test-001",
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("POST", "/workflows/versions", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("期望状态码 400，得到 %d，响应: %s", w.Code, w.Body.String())
	}
}

func TestWorkflowOrchestratorController_GetVersion(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)

	version := &model.WorkflowVersion{
		WorkflowID: "wf-get-test",
		Version:    1,
		Name:       "Get Test",
		Definition: model.JSONMap{"nodes": []any{}},
		Status:     "draft",
	}
	if err := database.Create(version).Error; err != nil {
		t.Fatalf("创建测试数据失败: %v", err)
	}

	router := setupWorkflowGinEngine()
	router.GET("/workflows/versions/:id", ctrl.GetVersion)

	httpReq, _ := http.NewRequest("GET", fmt.Sprintf("/workflows/versions/%d", version.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，得到 %d，响应: %s", w.Code, w.Body.String())
	}
}

func TestWorkflowOrchestratorController_GetVersion_NotFound(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)
	router := setupWorkflowGinEngine()
	router.GET("/workflows/versions/:id", ctrl.GetVersion)

	httpReq, _ := http.NewRequest("GET", "/workflows/versions/99999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusNotFound {
		t.Errorf("期望状态码 404，得到 %d", w.Code)
	}
}

func TestWorkflowOrchestratorController_GetVersion_InvalidID(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)
	router := setupWorkflowGinEngine()
	router.GET("/workflows/versions/:id", ctrl.GetVersion)

	httpReq, _ := http.NewRequest("GET", "/workflows/versions/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("期望状态码 400，得到 %d", w.Code)
	}
}

func TestWorkflowOrchestratorController_ListVersions(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)

	for i := 1; i <= 3; i++ {
		database.Create(&model.WorkflowVersion{
			WorkflowID: "wf-list-test",
			Version:    i,
			Name:       fmt.Sprintf("Version %d", i),
			Definition: model.JSONMap{"nodes": []any{}},
			Status:     "draft",
		})
	}

	router := setupWorkflowGinEngine()
	router.GET("/workflows/versions", ctrl.ListVersions)

	httpReq, _ := http.NewRequest("GET", "/workflows/versions?workflow_id=wf-list-test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，得到 %d，响应: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatalf("期望 data 为数组，得到 %T", resp["data"])
	}
	if len(data) != 3 {
		t.Errorf("期望 3 条版本记录，得到 %d", len(data))
	}
}

func TestWorkflowOrchestratorController_ListVersions_MissingWorkflowID(t *testing.T) {

	db := setupWorkflowTestDB(t)
	ctrl := setupWorkflowOrchestratorController(db)
	router := gin.New()
	router.GET("/workflows/versions", ctrl.ListVersions)

	req, _ := http.NewRequest("GET", "/workflows/versions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 200(分页列表模式)，得到 %d", w.Code)
	}
}

func TestWorkflowOrchestratorController_UpdateVersion(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)

	version := &model.WorkflowVersion{
		WorkflowID: "wf-update-test",
		Version:    1,
		Name:       "Original Name",
		Definition: model.JSONMap{"nodes": []any{}},
		Status:     "draft",
	}
	database.Create(version)

	router := setupWorkflowGinEngine()
	router.PUT("/workflows/versions/:id", ctrl.UpdateVersion)

	req := dto.WorkflowVersionUpdateRequest{
		Name:        "Updated Name",
		Description: "Updated description",
		Definition:  map[string]any{"nodes": []any{map[string]any{"id": "updated", "type": "trigger"}}},
		Changelog:   "Updated",
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("PUT", fmt.Sprintf("/workflows/versions/%d", version.ID), bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，得到 %d，响应: %s", w.Code, w.Body.String())
	}

	var updated model.WorkflowVersion
	database.First(&updated, version.ID)
	if updated.Name != "Updated Name" {
		t.Errorf("期望名称为 Updated Name，得到 %s", updated.Name)
	}
}

func TestWorkflowOrchestratorController_PublishVersion(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)

	version := &model.WorkflowVersion{
		WorkflowID: "wf-publish-test",
		Version:    1,
		Name:       "Publish Test",
		Definition: model.JSONMap{"nodes": []any{map[string]any{"id": "n1", "type": "trigger"}}},
		Status:     "draft",
	}
	database.Create(version)

	router := setupWorkflowGinEngine()
	router.POST("/workflows/versions/:id/publish", ctrl.PublishVersion)

	httpReq, _ := http.NewRequest("POST", fmt.Sprintf("/workflows/versions/%d/publish", version.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，得到 %d，响应: %s", w.Code, w.Body.String())
	}

	var updated model.WorkflowVersion
	database.First(&updated, version.ID)
	if updated.Status != "published" {
		t.Errorf("期望状态为 published，得到 %s", updated.Status)
	}
}

func TestWorkflowOrchestratorController_ArchiveVersion(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)

	version := &model.WorkflowVersion{
		WorkflowID: "wf-archive-test",
		Version:    1,
		Name:       "Archive Test",
		Definition: model.JSONMap{"nodes": []any{}},
		Status:     "published",
	}
	database.Create(version)

	router := setupWorkflowGinEngine()
	router.POST("/workflows/versions/:id/archive", ctrl.ArchiveVersion)

	httpReq, _ := http.NewRequest("POST", fmt.Sprintf("/workflows/versions/%d/archive", version.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，得到 %d", w.Code)
	}

	var updated model.WorkflowVersion
	database.First(&updated, version.ID)
	if updated.Status != "archived" {
		t.Errorf("期望状态为 archived，得到 %s", updated.Status)
	}
}

func TestWorkflowOrchestratorController_DeleteVersion(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)

	version := &model.WorkflowVersion{
		WorkflowID: "wf-delete-test",
		Version:    1,
		Name:       "Delete Test",
		Definition: model.JSONMap{"nodes": []any{}},
		Status:     "draft",
	}
	database.Create(version)

	router := setupWorkflowGinEngine()
	router.DELETE("/workflows/versions/:id", ctrl.DeleteVersion)

	httpReq, _ := http.NewRequest("DELETE", fmt.Sprintf("/workflows/versions/%d", version.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，得到 %d", w.Code)
	}

	var count int64
	database.Model(&model.WorkflowVersion{}).Where("id = ?", version.ID).Count(&count)
	if count != 0 {
		t.Errorf("期望记录已被删除，但仍存在")
	}
}

func TestWorkflowOrchestratorController_Execute(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)

	database.Create(&model.WorkflowVersion{
		WorkflowID: "wf-exec-test",
		Version:    1,
		Name:       "Exec Test",
		Definition: model.JSONMap{"nodes": []any{}},
		Status:     "published",
	})

	router := setupWorkflowGinEngine()
	router.POST("/workflows/execute", ctrl.Execute)

	req := dto.WorkflowExecuteRequest{
		WorkflowID:     "wf-exec-test",
		TriggerPayload: map[string]any{"user_id": "u001", "message": "Hello"},
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("POST", "/workflows/execute", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，得到 %d，响应: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("响应中缺少 data 字段")
	}
	if data["status"] != "running" {
		t.Errorf("期望状态为 running，得到 %v", data["status"])
	}
}

func TestWorkflowOrchestratorController_Execute_NoPublishedVersion(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)

	router := setupWorkflowGinEngine()
	router.POST("/workflows/execute", ctrl.Execute)

	req := dto.WorkflowExecuteRequest{
		WorkflowID: "wf-no-published",
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("POST", "/workflows/execute", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("期望状态码 400，得到 %d", w.Code)
	}
}

func TestWorkflowOrchestratorController_GetExecution(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)

	exec := &model.WorkflowExecution{
		WorkflowID: "wf-exec-get",
		Version:    1,
		Status:     "running",
		Context:    model.JSONMap{"step": 1},
	}
	database.Create(exec)

	router := setupWorkflowGinEngine()
	router.GET("/workflows/executions/:id", ctrl.GetExecution)

	httpReq, _ := http.NewRequest("GET", fmt.Sprintf("/workflows/executions/%d", exec.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，得到 %d，响应: %s", w.Code, w.Body.String())
	}
}

func TestWorkflowOrchestratorController_ListExecutions(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)

	for i := 1; i <= 3; i++ {
		database.Create(&model.WorkflowExecution{
			WorkflowID: "wf-exec-list",
			Version:    1,
			Status:     "running",
		})
	}

	router := setupWorkflowGinEngine()
	router.GET("/workflows/executions", ctrl.ListExecutions)

	httpReq, _ := http.NewRequest("GET", "/workflows/executions?workflow_id=wf-exec-list&page=1&page_size=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，得到 %d，响应: %s", w.Code, w.Body.String())
	}
}

func TestWorkflowOrchestratorController_GetNodeExecutions(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)

	exec := &model.WorkflowExecution{
		WorkflowID: "wf-node-exec",
		Version:    1,
		Status:     "running",
	}
	database.Create(exec)

	database.Create(&model.WorkflowNodeExecution{
		ExecutionID: exec.ID,
		NodeID:      "n1",
		NodeType:    "trigger",
		NodeName:    "Start",
		Status:      "completed",
	})
	database.Create(&model.WorkflowNodeExecution{
		ExecutionID: exec.ID,
		NodeID:      "n2",
		NodeType:    "action",
		NodeName:    "Send Email",
		Status:      "running",
	})

	router := setupWorkflowGinEngine()
	router.GET("/workflows/executions/:id/nodes", ctrl.GetNodeExecutions)

	httpReq, _ := http.NewRequest("GET", fmt.Sprintf("/workflows/executions/%d/nodes", exec.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，得到 %d，响应: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatalf("期望 data 为数组，得到 %T", resp["data"])
	}
	if len(data) != 2 {
		t.Errorf("期望 2 条节点记录，得到 %d", len(data))
	}
}

func TestWorkflowOrchestratorController_StopExecution(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)

	exec := &model.WorkflowExecution{
		WorkflowID: "wf-stop-test",
		Version:    1,
		Status:     "running",
	}
	database.Create(exec)

	router := setupWorkflowGinEngine()
	router.POST("/workflows/executions/:id/stop", ctrl.StopExecution)

	httpReq, _ := http.NewRequest("POST", fmt.Sprintf("/workflows/executions/%d/stop", exec.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，得到 %d，响应: %s", w.Code, w.Body.String())
	}

	var updated model.WorkflowExecution
	database.First(&updated, exec.ID)
	if updated.Status != "terminated" {
		t.Errorf("期望状态为 terminated，得到 %s", updated.Status)
	}
}

func TestWorkflowOrchestratorController_StopExecution_NotRunning(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)

	exec := &model.WorkflowExecution{
		WorkflowID: "wf-stop-completed",
		Version:    1,
		Status:     "completed",
	}
	database.Create(exec)

	router := setupWorkflowGinEngine()
	router.POST("/workflows/executions/:id/stop", ctrl.StopExecution)

	httpReq, _ := http.NewRequest("POST", fmt.Sprintf("/workflows/executions/%d/stop", exec.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusConflict {
		t.Errorf("期望状态码 409，得到 %d", w.Code)
	}
}

func TestWorkflowOrchestratorController_CRUD_FullFlow(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)
	router := setupWorkflowGinEngine()

	router.POST("/workflows/versions", ctrl.CreateVersion)
	router.GET("/workflows/versions", ctrl.ListVersions)
	router.GET("/workflows/versions/:id", ctrl.GetVersion)
	router.PUT("/workflows/versions/:id", ctrl.UpdateVersion)
	router.POST("/workflows/versions/:id/publish", ctrl.PublishVersion)
	router.POST("/workflows/versions/:id/archive", ctrl.ArchiveVersion)
	router.DELETE("/workflows/versions/:id", ctrl.DeleteVersion)

	createReq := dto.WorkflowVersionCreateRequest{
		WorkflowID:  "wf-e2e-test",
		Name:        "E2E Test Workflow",
		Description: "Full flow test",
		Definition: map[string]any{
			"nodes": []any{
				map[string]any{"id": "n1", "type": "trigger"},
			},
		},
		CreatedBy: "e2e-user",
	}
	body, _ := json.Marshal(createReq)
	req, _ := http.NewRequest("POST", "/workflows/versions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("创建失败: %d %s", w.Code, w.Body.String())
	}

	var createResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &createResp)
	data := createResp["data"].(map[string]any)
	versionID := uint(data["id"].(float64))

	req, _ = http.NewRequest("GET", "/workflows/versions?workflow_id=wf-e2e-test", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("列表查询失败: %d", w.Code)
	}

	req, _ = http.NewRequest("GET", fmt.Sprintf("/workflows/versions/%d", versionID), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("详情查询失败: %d", w.Code)
	}

	updateReq := dto.WorkflowVersionUpdateRequest{
		Name:       "Updated E2E",
		Definition: map[string]any{"nodes": []any{map[string]any{"id": "updated", "type": "trigger"}}},
	}
	updateBody, _ := json.Marshal(updateReq)
	req, _ = http.NewRequest("PUT", fmt.Sprintf("/workflows/versions/%d", versionID), bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("更新失败: %d %s", w.Code, w.Body.String())
	}

	req, _ = http.NewRequest("POST", fmt.Sprintf("/workflows/versions/%d/publish", versionID), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("发布失败: %d", w.Code)
	}

	req, _ = http.NewRequest("POST", fmt.Sprintf("/workflows/versions/%d/archive", versionID), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("归档失败: %d", w.Code)
	}

	req, _ = http.NewRequest("DELETE", fmt.Sprintf("/workflows/versions/%d", versionID), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("删除失败: %d", w.Code)
	}

	t.Log("全流程 CRUD 测试通过")
}

func TestWorkflowOrchestratorController_ExecuteFlow(t *testing.T) {
	database := setupWorkflowTestDB(t)
	if database == nil {
		return
	}
	ctrl := setupWorkflowOrchestratorController(database)
	router := setupWorkflowGinEngine()

	router.POST("/workflows/versions", ctrl.CreateVersion)
	router.POST("/workflows/versions/:id/publish", ctrl.PublishVersion)
	router.POST("/workflows/execute", ctrl.Execute)
	router.GET("/workflows/executions/:id", ctrl.GetExecution)
	router.GET("/workflows/executions/:id/nodes", ctrl.GetNodeExecutions)
	router.POST("/workflows/executions/:id/stop", ctrl.StopExecution)

	createReq := dto.WorkflowVersionCreateRequest{
		WorkflowID: "wf-exec-flow",
		Name:       "Exec Flow Test",
		Definition: map[string]any{
			"nodes": []any{
				map[string]any{"id": "n1", "type": "trigger", "name": "Webhook"},
				map[string]any{"id": "n2", "type": "action", "name": "Log Message", "config": map[string]any{"action_type": "log"}},
			},
			"edges": []any{
				map[string]any{"source": "n1", "target": "n2"},
			},
		},
	}
	body, _ := json.Marshal(createReq)
	req, _ := http.NewRequest("POST", "/workflows/versions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var createResp map[string]any
	if w.Code != http.StatusOK {
		t.Fatalf("创建失败: %d %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("解析创建响应失败: %v", err)
	}
	data, ok := createResp["data"].(map[string]any)
	if !ok {
		t.Fatalf("创建响应缺少 data: %s", w.Body.String())
	}
	versionID := uint(data["id"].(float64))

	req, _ = http.NewRequest("POST", fmt.Sprintf("/workflows/versions/%d/publish", versionID), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("发布失败: %d", w.Code)
	}

	execReq := dto.WorkflowExecuteRequest{
		WorkflowID:     "wf-exec-flow",
		TriggerPayload: map[string]any{"test": true},
	}
	execBody, _ := json.Marshal(execReq)
	req, _ = http.NewRequest("POST", "/workflows/execute", bytes.NewReader(execBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("执行失败: %d %s", w.Code, w.Body.String())
	}

	var execResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &execResp)
	execID := uint(execResp["data"].(map[string]any)["id"].(float64))

	req, _ = http.NewRequest("GET", fmt.Sprintf("/workflows/executions/%d", execID), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("获取执行详情失败: %d", w.Code)
	}

	req, _ = http.NewRequest("GET", fmt.Sprintf("/workflows/executions/%d/nodes", execID), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("获取节点执行失败: %d", w.Code)
	}

	req, _ = http.NewRequest("POST", fmt.Sprintf("/workflows/executions/%d/stop", execID), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("停止执行失败: %d", w.Code)
	}

	t.Log("执行流程测试通过")
}

// 成功夹具使用合法图，非法图仍须在 API 边界拒绝且不入库。
func TestWorkflowOrchestratorController_CreateVersion_InvalidDefinition(t *testing.T) {
	for _, tc := range []struct {
		name       string
		definition map[string]any
	}{
		{"empty", map[string]any{"nodes": []any{}}},
		{"string-node", map[string]any{"nodes": []any{"updated"}}},
		{"missing-action", map[string]any{"nodes": []any{
			map[string]any{"id": "n1", "type": "trigger"},
			map[string]any{"id": "n2", "type": "action"},
		}}},
		{"unknown-action", map[string]any{"nodes": []any{
			map[string]any{"id": "n1", "type": "trigger"},
			map[string]any{"id": "n2", "type": "action", "config": map[string]any{"action_type": "send-everything"}},
		}}},
		{"missing-edge-target", map[string]any{
			"nodes": []any{map[string]any{"id": "n1", "type": "trigger"}},
			"edges": []any{map[string]any{"source": "n1", "target": "missing"}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := setupWorkflowTestDB(t)
			ctrl := setupWorkflowOrchestratorController(database)
			router := setupWorkflowGinEngine()
			router.POST("/workflows/versions", ctrl.CreateVersion)
			body, err := json.Marshal(dto.WorkflowVersionCreateRequest{
				WorkflowID: "invalid-" + tc.name, Name: "非法图回归", Definition: tc.definition,
			})
			if err != nil {
				t.Fatalf("序列化非法图测试请求失败: %v", err)
			}
			req := httptest.NewRequest("POST", "/workflows/versions", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("非法图应拒绝，实际 %d %s", w.Code, w.Body.String())
			}
			var count int64
			if err := database.Model(&model.WorkflowVersion{}).Count(&count).Error; err != nil {
				t.Fatalf("核对拒绝后无版本落库失败: %v", err)
			}
			if count != 0 {
				t.Fatalf("非法图不应落库，实际 %d 条", count)
			}
		})
	}
}
