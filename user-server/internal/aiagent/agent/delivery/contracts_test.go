package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/pkg/tenant"
)

func TestInferenceDeliveryIdentityScope(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 773)
	base := Intent{MessageID: "message", Channel: "local", AccountID: "account", CustomerID: "customer", SessionID: "session", AgentCode: "default", Kind: "reply", Content: "正文"}
	key, err := Identity(ctx, base)
	if err != nil || len(key) != 64 {
		t.Fatalf("稳定身份无效：%s %v", key, err)
	}
	changed := base
	changed.Content = "不同正文"
	if other, _ := Identity(ctx, changed); other != key || Fingerprint(changed) == Fingerprint(base) {
		t.Fatal("正文改变生成新键或未改变指纹")
	}
	for _, field := range []string{"message", "channel", "account", "customer", "session", "agent_id", "agent_code", "agent_version", "kind", "tenant"} {
		t.Run(field, func(t *testing.T) {
			i, scope := base, ctx
			switch field {
			case "message":
				i.MessageID += "2"
			case "channel":
				i.Channel += "2"
			case "account":
				i.AccountID += "2"
			case "customer":
				i.CustomerID += "2"
			case "session":
				i.SessionID += "2"
			case "agent_id":
				i.AgentID++
			case "agent_code":
				i.AgentCode += "2"
			case "agent_version":
				i.AgentVersion++
			case "kind":
				i.Kind = "handoff"
			case "tenant":
				scope = tenant.WithTenantID(context.Background(), 774)
			}
			other, err := Identity(scope, i)
			if err != nil || other == key {
				t.Fatalf("隔离维度未入键：%s %v", other, err)
			}
		})
	}
	for _, invalid := range []string{"missing_message", "whitespace", "oversize", "kind", "negative_version"} {
		t.Run(invalid, func(t *testing.T) {
			i := base
			switch invalid {
			case "missing_message":
				i.MessageID = ""
			case "whitespace":
				i.AccountID = " account"
			case "oversize":
				i.SessionID = strings.Repeat("s", 513)
			case "kind":
				i.Kind = "unknown"
			case "negative_version":
				i.AgentVersion = -1
			}
			if _, err := Identity(ctx, i); !errors.Is(err, ErrIdentity) {
				t.Fatalf("非法身份未拒绝：%v", err)
			}
		})
	}
	if _, err := Identity(context.Background(), base); !errors.Is(err, ErrTenant) {
		t.Fatal("缺租户未关闭")
	}
}
