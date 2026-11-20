package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetDocumentProgressRequiresProductScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctrl := &KnowledgeWorkspaceController{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/knowledge/documents/42/progress", nil)
	c.Params = gin.Params{{Key: "id", Value: "42"}}

	ctrl.GetDocumentProgress(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}
