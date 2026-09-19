package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestSegmentSaveRequest_NoClientSQLChannel 钉死 2026-09-19 的 SQLi 收口：
// 分群保存请求不得存在任何携带原始 SQL 的字段（原 where_sql 可让已认证商户把
// 子查询布尔盲猜直接灌进 customers 的 WHERE，弱黑名单拦不住 select/union）。
// 反向测试：给 SegmentSaveRequest 重新加回 where_sql（或任何 *sql* json tag）⇒ 本测试红。
func TestSegmentSaveRequest_NoClientSQLChannel(t *testing.T) {
	typ := reflect.TypeOf(SegmentSaveRequest{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := strings.ToLower(f.Tag.Get("json"))
		if strings.Contains(tag, "sql") || strings.Contains(strings.ToLower(f.Name), "sql") {
			t.Fatalf("SegmentSaveRequest 出现原始 SQL 通道字段 %s (json:%q)，违反 where_sql 收口契约", f.Name, tag)
		}
	}

	// 携带 where_sql 的历史/恶意请求体必须被静默忽略且不改任何语义字段
	var req SegmentSaveRequest
	body := `{"name":"seg","rules":[{"k":"v"}],"trigger":"auto","where_sql":"(select password from admins limit 1) like 'a%'"}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("绑定恶意请求体失败: %v", err)
	}
	if req.Name != "seg" || req.Trigger != "auto" || len(req.Rules) == 0 {
		t.Errorf("合法字段被恶意体的存在所破坏: %+v", req)
	}
}
