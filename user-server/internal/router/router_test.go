package router

import (
	"testing"

	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func TestInstallLeadMiningReAct_RequiresCompleteResolver(t *testing.T) {
	service.SetLeadReactHook(service.LeadReactResolver{}, nil)
	t.Cleanup(func() { service.SetLeadReactHook(service.LeadReactResolver{}, nil) })
	if err := installLeadMiningReAct(nil); err == nil {
		t.Fatal("缺少数据库时不得装配完整 ReAct resolver")
	}
	database := testutil.NewTestDB(t)
	if err := installLeadMiningReAct(database); err != nil {
		t.Fatalf("生产 Router 应装配完整 ReAct resolver: %v", err)
	}
}

func TestSetup_HealthEndpoint(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	routes := r.Routes()
	if len(routes) == 0 {
		t.Error("Expected routes to be registered")
	}
}

func TestSetup_PublicRoutes(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	routes := r.Routes()
	foundHealth := false
	foundLogin := false
	for _, route := range routes {
		if route.Path == "/api/health" {
			foundHealth = true
		}
		if route.Path == "/api/auth/login" {
			foundLogin = true
		}
	}
	if !foundHealth {
		t.Error("Expected /api/health route to be registered")
	}
	if !foundLogin {
		t.Error("Expected /api/auth/login route to be registered")
	}
}

func TestSetup_AuthRoutes(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	routes := r.Routes()
	foundAuthRefresh := false
	for _, route := range routes {
		if route.Path == "/api/auth/refresh-token" {
			foundAuthRefresh = true
		}
	}
	if !foundAuthRefresh {
		t.Error("Expected /api/auth/refresh-token route to be registered")
	}
}

func TestSetup_CardRoutes(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	routes := r.Routes()
	foundDouyin := false
	for _, route := range routes {
		if route.Path == "/api/douyin-card/list" {
			foundDouyin = true
		}
	}
	if !foundDouyin {
		t.Error("Expected /api/douyin-card/list route to be registered")
	}
}

func TestSetup_SystemRoutes(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	routes := r.Routes()
	foundSystemConfig := false
	for _, route := range routes {
		if route.Path == "/api/system/config" {
			foundSystemConfig = true
		}
	}
	if !foundSystemConfig {
		t.Error("Expected /api/system/config route to be registered")
	}
}

func TestSetup_ShortLinkRoutes(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	routes := r.Routes()
	foundShortLink := false
	for _, route := range routes {
		if route.Path == "/api/short-link/list" {
			foundShortLink = true
		}
	}
	if !foundShortLink {
		t.Error("Expected /api/short-link/list route to be registered")
	}
}

func TestSetup_LiveCodeRoutes(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	routes := r.Routes()
	foundLiveCode := false
	for _, route := range routes {
		if route.Path == "/api/live-code/list" {
			foundLiveCode = true
		}
	}
	if !foundLiveCode {
		t.Error("Expected /api/live-code/list route to be registered")
	}
}

func TestSetup_RAGRoutes(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	routes := r.Routes()
	foundRAG := false
	for _, route := range routes {
		if route.Path == "/api/rag/documents" {
			foundRAG = true
		}
	}
	if !foundRAG {
		t.Error("Expected /api/rag/documents route to be registered")
	}
}

func TestSetup_UploadRoute(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	routes := r.Routes()
	foundUpload := false
	for _, route := range routes {
		if route.Path == "/api/upload" {
			foundUpload = true
		}
	}
	if !foundUpload {
		t.Error("Expected /api/upload route to be registered")
	}
}
