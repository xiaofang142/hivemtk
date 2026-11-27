package context

import "testing"

func TestAssembleGroupVisibilityProof(t *testing.T) {
	base := func() ContextAssemblerInput {
		return ContextAssemblerInput{
			Scope: ConversationScope{TenantID: 1, Platform: "wechat", ChannelAccountID: 2, IsGroup: true, GroupID: "g1"},
			Current: ContextMessage{
				MessageID:  "m-current",
				SenderID:   "u1",
				Visibility: VisibilityPublic,
			},
			Policy: SourcePolicy{Kind: SourcePolicyGroup, Version: "v1"},
		}
	}

	t.Run("unproven current is never verified", func(t *testing.T) {
		in := base()
		in.Current.Visibility = VisibilityUnknown
		got, err := AssembleAgentContext(in)
		if err != nil {
			t.Fatalf("AssembleAgentContext: %v", err)
		}
		if got.Provenance.VisibilityProof != "visibility_unproven" {
			t.Fatalf("visibility proof = %q, want visibility_unproven", got.Provenance.VisibilityProof)
		}
	})

	t.Run("proven current only is verified", func(t *testing.T) {
		got, err := AssembleAgentContext(base())
		if err != nil {
			t.Fatalf("AssembleAgentContext: %v", err)
		}
		if got.Provenance.VisibilityProof != "group_audienceVerified" {
			t.Fatalf("visibility proof = %q, want group_audienceVerified", got.Provenance.VisibilityProof)
		}
	})

	t.Run("private other member history is not verified", func(t *testing.T) {
		in := base()
		in.Recent = []ContextMessage{{
			MessageID:  "m-private",
			SenderID:   "u2",
			Visibility: VisibilityPrivate,
		}}
		got, err := AssembleAgentContext(in)
		if err != nil {
			t.Fatalf("AssembleAgentContext: %v", err)
		}
		if got.Provenance.VisibilityProof != "visibility_unproven" {
			t.Fatalf("visibility proof = %q, want visibility_unproven", got.Provenance.VisibilityProof)
		}
	})
}
