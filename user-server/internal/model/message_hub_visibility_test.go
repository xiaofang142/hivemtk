package model

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestMessageHubVisibilitySchemaContract(t *testing.T) {
	modelSchema, err := schema.Parse(
		&MessageHub{},
		&sync.Map{},
		schema.NamingStrategy{},
	)
	if err != nil {
		t.Fatalf("解析 MessageHub schema 失败：%v", err)
	}

	tests := []struct {
		name       string
		column     string
		dataType   string
		notNull    bool
		defaultVal string
		jsonName   string
	}{
		{name: "visibility", column: "visibility", dataType: "varchar(32)", notNull: true, defaultVal: "unknown", jsonName: "visibility"},
		{name: "audience", column: "audience_json", dataType: "jsonb", jsonName: "audience_json,omitempty"},
		{name: "evidence", column: "visibility_evidence", dataType: "jsonb", jsonName: "visibility_evidence,omitempty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := modelSchema.LookUpField(tt.column)
			if field == nil {
				t.Fatalf("MessageHub 缺少字段 %q", tt.column)
			}
			if field.Creatable == false {
				t.Fatalf("字段 %q 必须可由 GORM 建表/写入", tt.column)
			}
			if string(field.DataType) != tt.dataType {
				t.Fatalf("字段 %q 类型错误：got %q, want %q", tt.column, field.DataType, tt.dataType)
			}
			if field.NotNull != tt.notNull {
				t.Fatalf("字段 %q NotNull 错误：got %v, want %v", tt.column, field.NotNull, tt.notNull)
			}
			if tt.defaultVal != "" && field.DefaultValue != tt.defaultVal {
				t.Fatalf("字段 %q 默认值错误：got %q, want %q", tt.column, field.DefaultValue, tt.defaultVal)
			}
			if got := field.Tag.Get("json"); got != tt.jsonName {
				t.Fatalf("字段 %q JSON 名错误：got %q, want %q", tt.column, got, tt.jsonName)
			}
		})
	}
}

func TestMessageHubVisibilityJSONRoundTrip(t *testing.T) {
	original := MessageHub{
		Visibility:         VisibilityMember,
		AudienceJSON:       JSONStrings{"member-a", "member-b"},
		VisibilityEvidence: JSONMap{"source": "membership_event", "version": "v1"},
	}
	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化 MessageHub 失败：%v", err)
	}
	var decoded MessageHub
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("反序列化 MessageHub 失败：%v", err)
	}
	if decoded.Visibility != original.Visibility {
		t.Fatalf("visibility 丢失：got %q, want %q", decoded.Visibility, original.Visibility)
	}
	if !reflect.DeepEqual(decoded.AudienceJSON, original.AudienceJSON) {
		t.Fatalf("audience 丢失：got %#v, want %#v", decoded.AudienceJSON, original.AudienceJSON)
	}
	if !reflect.DeepEqual(decoded.VisibilityEvidence, original.VisibilityEvidence) {
		t.Fatalf("visibility evidence 丢失：got %#v, want %#v", decoded.VisibilityEvidence, original.VisibilityEvidence)
	}
}
