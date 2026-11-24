package service

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// faResp 构造一个假 HTTP 响应。
func faResp(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

// faLeadPage 构造一页纷享销客 leads JSON（start..start+n-1），total 为总条数。
func faLeadPage(start, n, total int) string {
	var b strings.Builder
	b.WriteString(`{"response":{"data":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		id := fmt.Sprintf("L%03d", start+i)
		b.WriteString(fmt.Sprintf(`{"id":%q,"name":"n%03d","mobile":"138%08d","company_name":"c"}`, id, start+i, start+i))
	}
	b.WriteString(`],"total_count":`)
	b.WriteString(fmt.Sprintf("%d", total))
	b.WriteString(`},"errcode":0,"errmsg":""}`)
	return b.String()
}

// TestFACRM_PaginationAcrossPages：101 条应分两页（100+1）全部取回。
func TestFACRM_PaginationAcrossPages(t *testing.T) {
	calls := 0
	do := func(req *http.Request) (*http.Response, error) {
		calls++
		page := 1
		if req.URL != nil && req.URL.Query().Get("page") == "2" {
			page = 2
		}
		// 从 body 里读 page（纷享用 POST body，非 query）——兼容两种。
		body, _ := io.ReadAll(req.Body)
		if strings.Contains(string(body), `"page":2`) {
			page = 2
		}
		if page == 1 {
			return faResp(200, faLeadPage(0, 100, 101)), nil
		}
		return faResp(200, faLeadPage(100, 1, 101)), nil
	}
	leads, err := fxFetchAllLeads(do, "tok")
	if err != nil {
		t.Fatalf("fxFetchAllLeads: %v", err)
	}
	if len(leads) != 101 {
		t.Fatalf("应取回 101 条, 实际 %d", len(leads))
	}
	if calls != 2 {
		t.Fatalf("应请求 2 页, 实际 %d", calls)
	}
	if leads[100].ID != "L100" {
		t.Fatalf("第二页首条应为 L100, 实际 %s", leads[100].ID)
	}
}

// TestFACRM_SecondPageFailure：第二页 HTTP500 应返回错误，不带部分成功假象。
func TestFACRM_SecondPageFailure(t *testing.T) {
	n := 0
	do := func(req *http.Request) (*http.Response, error) {
		n++
		if n == 1 {
			return faResp(200, faLeadPage(0, 100, 101)), nil
		}
		return faResp(500, "internal error"), nil
	}
	_, err := fxFetchAllLeads(do, "tok")
	if err == nil {
		t.Fatalf("第二页 HTTP500 应报错")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("错误应含状态码 500, 实际 %v", err)
	}
}

// TestFACRM_HTTP500JSON：HTTP500 携带合法 JSON 也不能被当成成功解析。
func TestFACRM_HTTP500JSON(t *testing.T) {
	do := func(req *http.Request) (*http.Response, error) {
		return faResp(500, faLeadPage(0, 3, 3)), nil
	}
	_, err := fxFetchAllLeads(do, "tok")
	if err == nil {
		t.Fatalf("HTTP500 即使 body 是合法 JSON 也应报错")
	}
}

// TestFACRM_EmptyPage：空页应停止且不报错。
func TestFACRM_EmptyPage(t *testing.T) {
	do := func(req *http.Request) (*http.Response, error) {
		return faResp(200, `{"response":{"data":[],"total_count":0},"errcode":0,"errmsg":""}`), nil
	}
	leads, err := fxFetchAllLeads(do, "tok")
	if err != nil {
		t.Fatalf("空页不应报错: %v", err)
	}
	if len(leads) != 0 {
		t.Fatalf("空页应返回 0 条, 实际 %d", len(leads))
	}
}

// TestFACRM_Errcode：业务错误码应返回错误。
func TestFACRM_Errcode(t *testing.T) {
	do := func(req *http.Request) (*http.Response, error) {
		return faResp(200, `{"response":{"data":[],"total_count":0},"errcode":42,"errmsg":"bad token"}`), nil
	}
	_, err := fxFetchAllLeads(do, "tok")
	if err == nil || !strings.Contains(err.Error(), "bad token") {
		t.Fatalf("errcode!=0 应报业务错误, 实际 %v", err)
	}
}
