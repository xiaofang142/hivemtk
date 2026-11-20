package service

import (
	"context"
	"reflect"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// 真实客户合并后，原身份的撤回/暂停必须同时出现在客户360查询与发送闸口。
func TestContactConsentMerge_RetainsFactsAndBlocksSend(t *testing.T) {
	for _, kind := range []string{"secondary_revoke", "primary_revoke", "secondary_pause", "primary_pause"} {
		t.Run(kind, func(t *testing.T) {
			database := setupCustomerServiceTestDB(t)
			testutil.ResetSchema(t, database, &model.ContactConsentEvent{}, &model.ContactPreference{})
			ctx := context.Background()
			primary := model.Customer{ID: "fa14-primary", UnifiedID: "fa14-one-primary", Tags: "[]", ChurnRisk: "low"}
			secondary := model.Customer{ID: "fa14-secondary", UnifiedID: "fa14-one-secondary", Tags: "[]", ChurnRisk: "low"}
			for _, customer := range []*model.Customer{&primary, &secondary} {
				if err := database.Create(customer).Error; err != nil {
					t.Fatal(err)
				}
			}
			consents := NewContactConsentService(repository.NewContactConsentRepository(database))
			grantID, deniedID := primary.UnifiedID, secondary.UnifiedID
			if kind == "primary_revoke" || kind == "primary_pause" {
				grantID, deniedID = secondary.UnifiedID, primary.UnifiedID
			}
			if err := consents.Record(ctx, ConsentRecordInput{OneID: grantID, Purpose: model.ConsentPurposeMarketing, Decision: model.ConsentDecisionGrant}); err != nil {
				t.Fatal(err)
			}
			want := ConsentStatusRevoked
			if kind == "secondary_revoke" || kind == "primary_revoke" {
				if err := consents.Record(ctx, ConsentRecordInput{OneID: deniedID, Purpose: model.ConsentPurposeMarketing, Decision: model.ConsentDecisionRevoke, OccurredAt: time.Now().Add(-time.Hour)}); err != nil {
					t.Fatal(err)
				}
			} else {
				want = ConsentStatusPaused
				if err := consents.SetPreference(ctx, deniedID, model.ConsentPurposeMarketing, "wecom", true, nil, ""); err != nil {
					t.Fatal(err)
				}
			}
			var before, after []model.ContactConsentEvent
			var prefsBefore, prefsAfter []model.ContactPreference
			if err := database.Order("id").Find(&before).Error; err != nil {
				t.Fatal(err)
			}
			if err := database.Order("id").Find(&prefsBefore).Error; err != nil {
				t.Fatal(err)
			}
			if err := NewCustomerService().MergeCustomers(ctx, primary.ID, secondary.ID); err != nil {
				t.Fatal(err)
			}
			if err := database.Order("id").Find(&after).Error; err != nil {
				t.Fatal(err)
			}
			if err := database.Order("id").Find(&prefsAfter).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(prefsBefore, prefsAfter) {
				t.Error("合并不得改写原授权事件和偏好事实")
			}
			for _, oneID := range []string{primary.UnifiedID, secondary.UnifiedID} {
				decision := consents.CheckSendConsent(ctx, oneID, model.ConsentPurposeMarketing, "wecom")
				if decision.Status != want {
					t.Errorf("身份 %s 判定=%s，期望%s", oneID, decision.Status, want)
				}
				timeline, err := consents.Timeline(ctx, oneID, 100)
				if err != nil || !reflect.DeepEqual(timeline, reverseConsentEvents(before)) {
					t.Errorf("时间线必须保留全部原身份事件: %+v，错误=%v", timeline, err)
				}
				prefs, err := consents.Preferences(ctx, oneID)
				if err != nil || len(prefs) != len(prefsBefore) {
					t.Errorf("偏好必须保留原身份: %+v，错误=%v", prefs, err)
				}
				adapter := newSuccessAdapter("fa14-controlled")
				cfg := DefaultSendPipelineConfig(adapter)
				cfg.ConsentChecker, cfg.ConsentMode = consents, ConsentModeRevocationOnly
				response := NewSendPipeline(cfg).Send(ctx, &ReachSendRequest{Channel: "wecom", CustomerID: oneID, Content: "controlled test"})
				if response.Success || adapter.Count(ctx) != 0 {
					t.Errorf("%s必须零外发: success=%v calls=%d", want, response.Success, adapter.Count(ctx))
				}
			}
		})
	}
}

func reverseConsentEvents(rows []model.ContactConsentEvent) []model.ContactConsentEvent {
	// 此用例的 grant 较新、revoke 较旧，ID顺序并非时间顺序。
	result := append([]model.ContactConsentEvent{}, rows...)
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].OccurredAt.After(result[i].OccurredAt) || (result[j].OccurredAt.Equal(result[i].OccurredAt) && result[j].ID > result[i].ID) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}
