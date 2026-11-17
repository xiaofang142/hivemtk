package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"hivemtk-user/internal/etl"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

func TestSaaSConfigParams_SidecarAddressIsStartupSnapshot(t *testing.T) {
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	database := testutil.NewTestDB(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{}, &model.TenantConfigParam{})
	ctx := context.Background()
	var firstCalls, secondCalls atomic.Int32
	parser := func(answer string, calls *atomic.Int32) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Error("解析请求方法改变")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Error("解析请求必须保留multipart file字段")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = file.Close()
			calls.Add(1)
			_, _ = w.Write([]byte(answer))
		}))
	}
	first := parser("首次解析器结果", &firstCalls)
	defer first.Close()
	second := parser("第二解析器结果", &secondCalls)
	defer second.Close()
	parameter := &model.ConfigParam{Group: "etl", Key: "parser_sidecar_url", Name: "合成解析地址",
		ValueType: "string", Value: first.URL, DefaultValue: "", Restart: true}
	if err := database.WithContext(ctx).Create(parameter).Error; err != nil {
		t.Fatal(err)
	}
	previousService := service.GlobalConfigParam()
	current := service.NewConfigParamService()
	service.SetGlobalForTest(current)
	t.Cleanup(func() {
		service.SetGlobalForTest(previousService)
		WireSaaSConfigParamSeams()
	})
	WireSaaSConfigParamSeams()
	if !etl.SidecarEnabled() {
		t.Fatal("合法首次地址未接入解析器")
	}
	text, err := etl.ParseDocument(ctx, "synthetic.docx", []byte("合成文档"))
	if err != nil || text != "首次解析器结果" {
		t.Fatalf("正常解析对照失败：%q，%v", text, err)
	}
	if err := current.UpdateValue(ctx, "etl", "parser_sidecar_url", second.URL, 1); err != nil {
		t.Fatal(err)
	}
	text, err = etl.ParseDocument(ctx, "synthetic.docx", []byte("第二份合成文档"))
	if err != nil || text != "首次解析器结果" || firstCalls.Load() != 2 || secondCalls.Load() != 0 {
		t.Errorf("运行期参数更新不能热切解析引擎：结果%q，首次调用%d，第二调用%d，错误%v", text, firstCalls.Load(), secondCalls.Load(), err)
	}
	WireSaaSConfigParamSeams()
	text, err = etl.ParseDocument(ctx, "synthetic.docx", []byte("重新装配后文档"))
	if err != nil || text != "第二解析器结果" || secondCalls.Load() != 1 {
		t.Errorf("明确重新装配应捕获新地址：%q，%v", text, err)
	}
	if err := current.UpdateValue(ctx, "etl", "parser_sidecar_url", "", 1); err != nil {
		t.Fatal(err)
	}
	WireSaaSConfigParamSeams()
	if etl.SidecarEnabled() {
		t.Fatal("空地址的正常禁用契约改变")
	}
	if err := current.UpdateValue(ctx, "etl", "parser_sidecar_url", second.URL, 1); err != nil {
		t.Fatal(err)
	}
	if etl.SidecarEnabled() {
		t.Error("空地址启动后不能因运行期参数更新而启用外挂解析")
	}
}
