package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
)

func TestLeadMiningAgentService_UnavailableStoreFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		svc  *LeadMiningAgentService
	}{
		{name: "nil-repo", svc: NewLeadMiningAgentServiceWithRepo(nil)},
		{name: "nil-receiver", svc: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			checks := []struct {
				name string
				call func(*testing.T)
			}{
				{
					name: "list",
					call: func(t *testing.T) {
						got, err := tt.svc.List(ctx, "lead")
						if got != nil || !errors.Is(err, errLeadMiningAgentStoreUnavailable) {
							t.Fatalf("List 无存储必须 fail-closed: got=%+v err=%v", got, err)
						}
					},
				},
				{
					name: "get",
					call: func(t *testing.T) {
						got, err := tt.svc.Get(ctx, 1)
						if got != nil || !errors.Is(err, errLeadMiningAgentStoreUnavailable) {
							t.Fatalf("Get 无存储必须 fail-closed: got=%+v err=%v", got, err)
						}
					},
				},
				{
					name: "create",
					call: func(t *testing.T) {
						err := tt.svc.Create(ctx, &model.AIAgent{Name: "lead"})
						if !errors.Is(err, errLeadMiningAgentStoreUnavailable) {
							t.Fatalf("Create 无存储必须 fail-closed: err=%v", err)
						}
					},
				},
				{
					name: "update",
					call: func(t *testing.T) {
						err := tt.svc.Update(ctx, &model.AIAgent{ID: 1, Name: "lead"})
						if !errors.Is(err, errLeadMiningAgentStoreUnavailable) {
							t.Fatalf("Update 无存储必须 fail-closed: err=%v", err)
						}
					},
				},
				{
					name: "delete",
					call: func(t *testing.T) {
						err := tt.svc.Delete(ctx, 1)
						if !errors.Is(err, errLeadMiningAgentStoreUnavailable) {
							t.Fatalf("Delete 无存储必须 fail-closed: err=%v", err)
						}
					},
				},
			}

			for _, check := range checks {
				t.Run(check.name, func(t *testing.T) {
					check.call(t)
				})
			}
		})
	}
}
