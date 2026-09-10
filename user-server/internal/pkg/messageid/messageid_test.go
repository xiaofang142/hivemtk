package messageid

import (
	"testing"

	"hivemtk-user/internal/model"
)

func TestNormalizeCustomerIDFromMessageHub_Nil(t *testing.T) {
	if got := NormalizeCustomerIDFromMessageHub(nil); got != "" {
		t.Fatalf("nil msg should normalize to empty, got %q", got)
	}
}

func TestNormalizeCustomerIDFromMessageHub_GroupConversation(t *testing.T) {
	msg := &model.MessageHub{IsGroup: true, ConversationID: "conv:g1", SenderID: "u9"}
	if got := NormalizeCustomerIDFromMessageHub(msg); got != "conv:g1" {
		t.Fatalf("group msg should use ConversationID, got %q", got)
	}
}

func TestNormalizeCustomerIDFromMessageHub_PrefixedSender(t *testing.T) {
	msg := &model.MessageHub{ConversationID: "conv:c1", SenderID: "conv:c1 part-a"}
	if got := NormalizeCustomerIDFromMessageHub(msg); got != "conv:c1" {
		t.Fatalf("sender prefixed by conversation should return conversation, got %q", got)
	}
}

func TestNormalizeCustomerIDFromMessageHub_Direction(t *testing.T) {
	outbound := &model.MessageHub{Direction: "outbound", SenderID: "agent-1", ReceiverID: "cust-9"}
	if got := NormalizeCustomerIDFromMessageHub(outbound); got != "cust-9" {
		t.Fatalf("outbound should use ReceiverID, got %q", got)
	}
	outboundNoReceiver := &model.MessageHub{Direction: "outbound", SenderID: "agent-1"}
	if got := NormalizeCustomerIDFromMessageHub(outboundNoReceiver); got != "agent-1" {
		t.Fatalf("outbound without receiver falls back to SenderID, got %q", got)
	}
	inbound := &model.MessageHub{Direction: "inbound", SenderID: "cust-9"}
	if got := NormalizeCustomerIDFromMessageHub(inbound); got != "cust-9" {
		t.Fatalf("inbound should use SenderID, got %q", got)
	}
}

func TestNormalizeCustomerNameFromMessageHub_ConversationFallback(t *testing.T) {
	conv := &model.MessageHub{ConversationID: "conv:王先生", SenderID: "conv:王先生 1"}
	if got := NormalizeCustomerNameFromMessageHub(conv); got != "王先生" {
		t.Fatalf("empty name should fall back to cleaned title, got %q", got)
	}
	named := &model.MessageHub{SenderName: "李雷", ConversationID: "conv:c1", SenderID: "u1"}
	if got := NormalizeCustomerNameFromMessageHub(named); got != "李雷" {
		t.Fatalf("explicit name should win, got %q", got)
	}
}

func TestCleanConversationTitle(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"conv:abc": "abc",
		"plain":    "plain",
	}
	for in, want := range cases {
		if got := CleanConversationTitle(in); got != want {
			t.Fatalf("CleanConversationTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
