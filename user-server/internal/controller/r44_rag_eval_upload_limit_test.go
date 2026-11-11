package controller

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
)

// r44 rag eval 上传面的体积护栏用例。
//
// 【改前行为（无上限，本刀钉死）】BodyLimit 中间件显式豁免 multipart
// （body_limit.go「跳过 multipart」分支），而 Upload 端点对请求体零上限、
// 服务层 csv.ReadAll 整读——认证用户可POST 多 GB CSV 打出内存尖峰。
// 改前超大 body 一路放行进解析（负例先红）；改后 MaxBytesReader 8MB 档
// （对齐 egolite.go UploadArtifact 先例）直接 400。
//
// 超大载荷故意只用 3 行：改前红相时该请求会真走完入库（3MB/行的 text 列不违约束），
// 几秒内可复现「200 imported=3」的放行证据，而不是等 8MB 逐行解析卡死用例。
func setupRagEvalUploadRouter(t *testing.T) *gin.Engine {
	t.Helper()

	database := testutil.NewTestDB(t, &model.RagEvalQuestion{}, &model.RagEvalRun{})
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	ctrl := NewRagEvalGapController()
	r.POST("/api/rag/eval/upload", ctrl.Upload)
	return r
}

func postRagEvalUpload(t *testing.T, r *gin.Engine, csv string) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "eval.csv")
	if err != nil {
		t.Fatalf("构造 multipart 失败: %v", err)
	}
	if _, err := part.Write([]byte(csv)); err != nil {
		t.Fatalf("写入 CSV 失败: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("收口 multipart 失败: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/rag/eval/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// 负例②：超过 8MB 档的 CSV 必须拒绝（改前 200 放行，改后 400）。
func TestRagEvalUpload_RejectsOversizedCSV(t *testing.T) {
	r := setupRagEvalUploadRouter(t)

	big := strings.Repeat("q", 3*1024*1024)
	csv := "question,answer\n" + big + ",a1\n" + big + ",a2\n" + big + ",a3\n"
	w := postRagEvalUpload(t, r, csv)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("超 8MB 的 CSV 必须 400 拒绝，实际 %d body=%s", w.Code, w.Body.String())
	}
}

// 防改坏守卫：8MB 档内的小 CSV 照常导入（护栏不许打断合法上传）。
func TestRagEvalUpload_SmallCSVImported(t *testing.T) {
	r := setupRagEvalUploadRouter(t)

	csv := "question,answer\nQ1,A1\nQ2,A2\n"
	w := postRagEvalUpload(t, r, csv)
	if w.Code != http.StatusOK {
		t.Fatalf("小 CSV 应 200 导入，实际 %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Imported int `json:"imported"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应体解析失败: %v body=%s", err, w.Body.String())
	}
	if resp.Code != 0 || resp.Data.Imported != 2 {
		t.Fatalf("imported 应为 2（header 跳过），实际 code=%d imported=%d", resp.Code, resp.Data.Imported)
	}
}
