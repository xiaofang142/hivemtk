package model

import (
	"reflect"
	"strings"
	"testing"
)

func TestSessionMessageCorrelationIDContract(t *testing.T) {
	field, ok := reflect.TypeOf(SessionMessage{}).FieldByName("CorrelationID")
	if !ok {
		t.Fatal("SessionMessage 缺少 CorrelationID，无法跨消息来源去重")
	}
	if field.Type.Kind() != reflect.String {
		t.Fatalf("CorrelationID 必须是字符串，实际 %s", field.Type)
	}
	gormTag := field.Tag.Get("gorm")
	for _, fragment := range []string{"type:varchar(100)", "index"} {
		if !strings.Contains(gormTag, fragment) {
			t.Fatalf("CorrelationID GORM 标签缺少 %q：%s", fragment, gormTag)
		}
	}
	if got := field.Tag.Get("json"); got != "correlation_id,omitempty" {
		t.Fatalf("CorrelationID JSON 标签错误：%q", got)
	}
}
