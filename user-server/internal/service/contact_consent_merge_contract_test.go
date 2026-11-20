package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func consentMergeCustomers(t *testing.T, database *gorm.DB, ctx context.Context, count int) []model.Customer {
	t.Helper()
	customers := make([]model.Customer, count)
	for i := range customers {
		customers[i] = model.Customer{TenantID: tenant.WriteTenantID(ctx), ID: fmt.Sprintf("fa14-customer-%d", i), UnifiedID: fmt.Sprintf("fa14-identity-%d", i), Tags: "[]", ChurnRisk: "low"}
		require.NoError(t, database.WithContext(ctx).Create(&customers[i]).Error)
	}
	return customers
}

func TestContactConsentMerge_ChannelPurposeAndOriginalIdentityRules(t *testing.T) {
	database := setupCustomerServiceTestDB(t)
	ctx := context.Background()
	customers := consentMergeCustomers(t, database, ctx, 2)
	svc := NewContactConsentService(repository.NewContactConsentRepository(database))
	for _, purpose := range []string{model.ConsentPurposeMarketing, model.ConsentPurposeSupport, model.ConsentPurposeTransactional} {
		require.NoError(t, svc.Record(ctx, ConsentRecordInput{OneID: customers[0].UnifiedID, Purpose: purpose, Decision: model.ConsentDecisionGrant}))
	}
	require.NoError(t, svc.Record(ctx, ConsentRecordInput{OneID: customers[1].UnifiedID, Purpose: model.ConsentPurposeMarketing, ChannelScope: "wecom", Decision: model.ConsentDecisionRevoke, OccurredAt: time.Now().Add(-time.Hour)}))
	require.NoError(t, svc.SetPreference(ctx, customers[1].UnifiedID, model.ConsentPurposeMarketing, "sms", true, nil, ""))
	require.NoError(t, NewCustomerService().MergeCustomers(ctx, customers[0].ID, customers[1].ID))
	for _, oneID := range []string{customers[0].UnifiedID, customers[1].UnifiedID} {
		for _, tc := range []struct{ purpose, channel, status string }{
			{model.ConsentPurposeMarketing, "wecom", ConsentStatusRevoked},
			{model.ConsentPurposeMarketing, "sms", ConsentStatusPaused},
			{model.ConsentPurposeMarketing, "email", ConsentStatusGranted},
			{model.ConsentPurposeMarketing, "", ConsentStatusGranted},
			{model.ConsentPurposeSupport, "wecom", ConsentStatusGranted},
			{model.ConsentPurposeTransactional, "sms", ConsentStatusGranted},
		} {
			require.Equal(t, tc.status, svc.CheckSendConsent(ctx, oneID, tc.purpose, tc.channel).Status, "%s/%s/%s", oneID, tc.purpose, tc.channel)
		}
	}
	// 另一身份的晚 grant 和恢复不能覆盖原身份的否定。
	require.NoError(t, svc.Record(ctx, ConsentRecordInput{OneID: customers[0].UnifiedID, Purpose: model.ConsentPurposeMarketing, Decision: model.ConsentDecisionGrant}))
	require.NoError(t, svc.SetPreference(ctx, customers[0].UnifiedID, model.ConsentPurposeMarketing, "sms", false, nil, ""))
	require.Equal(t, ConsentStatusRevoked, svc.CheckSendConsent(ctx, customers[0].UnifiedID, model.ConsentPurposeMarketing, "wecom").Status)
	require.Equal(t, ConsentStatusPaused, svc.CheckSendConsent(ctx, customers[0].UnifiedID, model.ConsentPurposeMarketing, "sms").Status)
	// 原身份自己的晚 grant、精确渠道恢复和到期仍沿用原有规则。
	require.NoError(t, svc.Record(ctx, ConsentRecordInput{OneID: customers[1].UnifiedID, Purpose: model.ConsentPurposeMarketing, ChannelScope: "wecom", Decision: model.ConsentDecisionGrant}))
	require.Equal(t, ConsentStatusGranted, svc.CheckSendConsent(ctx, customers[0].UnifiedID, model.ConsentPurposeMarketing, "wecom").Status)
	require.NoError(t, svc.SetPreference(ctx, customers[1].UnifiedID, model.ConsentPurposeMarketing, "", true, nil, ""))
	require.NoError(t, svc.SetPreference(ctx, customers[1].UnifiedID, model.ConsentPurposeMarketing, "sms", false, nil, ""))
	require.Equal(t, ConsentStatusGranted, svc.CheckSendConsent(ctx, customers[0].UnifiedID, model.ConsentPurposeMarketing, "sms").Status)
	past := time.Now().Add(-time.Minute)
	require.NoError(t, svc.SetPreference(ctx, customers[1].UnifiedID, model.ConsentPurposeMarketing, "wecom", true, &past, ""))
	require.Equal(t, ConsentStatusGranted, svc.CheckSendConsent(ctx, customers[0].UnifiedID, model.ConsentPurposeMarketing, "wecom").Status)
}

func TestContactConsentMerge_ChainedAndRepeatedLinks(t *testing.T) {
	database := setupCustomerServiceTestDB(t)
	ctx := context.Background()
	customers := consentMergeCustomers(t, database, ctx, 4)
	svc := NewContactConsentService(repository.NewContactConsentRepository(database))
	require.NoError(t, svc.Record(ctx, ConsentRecordInput{OneID: customers[0].UnifiedID, Purpose: model.ConsentPurposeMarketing, Decision: model.ConsentDecisionGrant}))
	require.NoError(t, svc.Record(ctx, ConsentRecordInput{OneID: customers[3].UnifiedID, Purpose: model.ConsentPurposeMarketing, Decision: model.ConsentDecisionRevoke}))
	merger := NewCustomerService()
	for _, pair := range [][2]int{{0, 1}, {2, 3}, {0, 2}} {
		require.NoError(t, merger.MergeCustomers(ctx, customers[pair[0]].ID, customers[pair[1]].ID))
	}
	// 已删除客户的重复合并保留明确错误，不重复生成任何事实或边。
	require.Error(t, merger.MergeCustomers(ctx, customers[0].ID, customers[1].ID))
	repo := repository.NewCustomerRepository()
	require.NoError(t, repo.WithTransaction(ctx, func(txCtx context.Context) error {
		for _, pair := range [][2]int{{1, 0}, {0, 1}, {3, 2}, {2, 0}, {0, 0}} {
			if err := repo.LinkConsentIdentities(txCtx, customers[pair[0]].UnifiedID, customers[pair[1]].UnifiedID); err != nil {
				return err
			}
		}
		return nil
	}))
	var links []model.ContactConsentIdentityLink
	require.NoError(t, database.Find(&links).Error)
	require.Len(t, links, 3)
	for _, customer := range customers {
		require.Equal(t, ConsentStatusRevoked, svc.CheckSendConsent(ctx, customer.UnifiedID, model.ConsentPurposeMarketing, "wecom").Status)
		rows, err := svc.Timeline(ctx, customer.UnifiedID, 100)
		require.NoError(t, err)
		require.Len(t, rows, 2)
	}
	// 关联组查询后的新偏好仍写原身份，只出现一次并对全部旧身份生效。
	require.NoError(t, svc.SetPreference(ctx, customers[1].UnifiedID, model.ConsentPurposeSupport, "", true, nil, ""))
	for _, customer := range customers {
		require.Equal(t, ConsentStatusPaused, svc.CheckSendConsent(ctx, customer.UnifiedID, model.ConsentPurposeSupport, "wecom").Status)
		prefs, err := svc.Preferences(ctx, customer.UnifiedID)
		require.NoError(t, err)
		require.Len(t, prefs, 1)
		require.Equal(t, customers[1].UnifiedID, prefs[0].OneID)
	}
}

func TestContactConsentMerge_RollsBackEveryAssociation(t *testing.T) {
	for _, fault := range []struct{ operation, table string }{{"create", "contact_consent_identity_links"}, {"update", "customers"}, {"update", "customer_channels"}, {"delete", "customers"}} {
		t.Run(fault.operation+"/"+fault.table, func(t *testing.T) {
			database := setupCustomerServiceTestDB(t)
			ctx := context.Background()
			customers := consentMergeCustomers(t, database, ctx, 2)
			require.NoError(t, database.Model(&customers[1]).Update("email", "controlled-secondary@example.test").Error)
			event := model.CustomerEvent{CustomerID: customers[1].ID, EventType: model.EventTypeSignup}
			session := model.CustomerSession{SessionID: "fa14-rollback", OneID: customers[1].UnifiedID}
			require.NoError(t, database.Create(&event).Error)
			require.NoError(t, database.Create(&session).Error)
			svc := NewContactConsentService(repository.NewContactConsentRepository(database))
			require.NoError(t, svc.Record(ctx, ConsentRecordInput{OneID: customers[1].UnifiedID, Purpose: model.ConsentPurposeMarketing, Decision: model.ConsentDecisionRevoke}))
			injected := errors.New("controlled merge persistence failure")
			callback := func(tx *gorm.DB) {
				if tx.Statement.Table == fault.table {
					tx.AddError(injected)
				}
			}
			switch fault.operation {
			case "create":
				require.NoError(t, database.Callback().Create().Before("gorm:create").Register("fa14:merge-fault", callback))
				t.Cleanup(func() { database.Callback().Create().Remove("fa14:merge-fault") })
			case "update":
				require.NoError(t, database.Callback().Update().Before("gorm:update").Register("fa14:merge-fault", callback))
				t.Cleanup(func() { database.Callback().Update().Remove("fa14:merge-fault") })
			case "delete":
				require.NoError(t, database.Callback().Delete().Before("gorm:delete").Register("fa14:merge-fault", callback))
				t.Cleanup(func() { database.Callback().Delete().Remove("fa14:merge-fault") })
			}
			require.ErrorIs(t, NewCustomerService().MergeCustomers(ctx, customers[0].ID, customers[1].ID), injected)
			var persisted []model.Customer
			require.NoError(t, database.Order("id").Find(&persisted).Error)
			require.Len(t, persisted, 2)
			require.Empty(t, persisted[0].Email)
			var linkCount int64
			require.NoError(t, database.Model(&model.ContactConsentIdentityLink{}).Count(&linkCount).Error)
			require.Zero(t, linkCount)
			require.NoError(t, database.First(&event, "id = ?", event.ID).Error)
			require.Equal(t, customers[1].ID, event.CustomerID)
			require.NoError(t, database.First(&session, session.ID).Error)
			require.Equal(t, customers[1].UnifiedID, session.OneID)
			require.Equal(t, ConsentStatusMissing, svc.CheckSendConsent(ctx, customers[0].UnifiedID, model.ConsentPurposeMarketing, "wecom").Status)
			require.Equal(t, ConsentStatusRevoked, svc.CheckSendConsent(ctx, customers[1].UnifiedID, model.ConsentPurposeMarketing, "wecom").Status)
		})
	}
}

func TestContactConsentMerge_ConcurrentMergesRetainEveryIdentity(t *testing.T) {
	database := setupCustomerServiceTestDB(t)
	ctx := context.Background()
	customers := consentMergeCustomers(t, database, ctx, 3)
	svc := NewContactConsentService(repository.NewContactConsentRepository(database))
	require.NoError(t, svc.Record(ctx, ConsentRecordInput{OneID: customers[2].UnifiedID, Purpose: model.ConsentPurposeMarketing, Decision: model.ConsentDecisionRevoke}))
	start, results := make(chan struct{}), make(chan error, 2)
	for _, secondary := range customers[1:] {
		go func(id string) { <-start; results <- NewCustomerService().MergeCustomers(ctx, customers[0].ID, id) }(secondary.ID)
	}
	close(start)
	for range 2 {
		require.NoError(t, <-results)
	}
	for _, customer := range customers {
		require.Equal(t, ConsentStatusRevoked, svc.CheckSendConsent(ctx, customer.UnifiedID, model.ConsentPurposeMarketing, "wecom").Status)
	}
}

func TestContactConsentMerge_TenantIsolationAndMissingContext(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	database := setupCustomerServiceTestDB(t)
	ctxA := tenant.WithTenantID(context.Background(), 41)
	ctxB := tenant.WithTenantID(context.Background(), 42)
	customers := consentMergeCustomers(t, database, ctxA, 2)
	foreign := model.Customer{TenantID: 42, ID: "fa14-foreign", UnifiedID: "fa14-foreign-one", Tags: "[]"}
	require.NoError(t, database.WithContext(ctxB).Create(&foreign).Error)
	svc := NewContactConsentService(repository.NewContactConsentRepository(database))
	require.NoError(t, svc.Record(ctxA, ConsentRecordInput{OneID: customers[0].UnifiedID, Purpose: model.ConsentPurposeMarketing, Decision: model.ConsentDecisionGrant}))
	require.NoError(t, svc.Record(ctxB, ConsentRecordInput{OneID: customers[0].UnifiedID, Purpose: model.ConsentPurposeMarketing, Decision: model.ConsentDecisionRevoke}))
	foreignLink := model.ContactConsentIdentityLink{TenantID: 42, LeftOneID: customers[0].UnifiedID, RightOneID: foreign.UnifiedID}
	require.NoError(t, database.WithContext(ctxB).Create(&foreignLink).Error)
	for _, ctx := range []context.Context{ctxA, tenant.WithSystemScope(ctxA)} {
		require.Error(t, NewCustomerService().MergeCustomers(ctx, customers[0].ID, foreign.ID))
		require.Equal(t, ConsentStatusGranted, svc.CheckSendConsent(ctx, customers[0].UnifiedID, model.ConsentPurposeMarketing, "wecom").Status)
		rows, err := svc.Timeline(ctx, customers[0].UnifiedID, 100)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, uint(41), rows[0].TenantID)
	}
	for _, ctx := range []context.Context{context.Background(), tenant.WithSystemScope(context.Background())} {
		require.Error(t, NewCustomerService().MergeCustomers(ctx, customers[0].ID, customers[1].ID))
		require.Equal(t, ConsentStatusUnavailable, svc.CheckSendConsent(ctx, customers[0].UnifiedID, model.ConsentPurposeMarketing, "wecom").Status)
		_, err := svc.Timeline(ctx, customers[0].UnifiedID, 100)
		require.Error(t, err)
		_, err = svc.Preferences(ctx, customers[0].UnifiedID)
		require.Error(t, err)
	}
	require.NoError(t, NewCustomerService().MergeCustomers(ctxA, customers[0].ID, customers[1].ID))
	require.Equal(t, ConsentStatusRevoked, svc.CheckSendConsent(ctxB, customers[0].UnifiedID, model.ConsentPurposeMarketing, "wecom").Status)
	rows, err := svc.Timeline(ctxB, customers[0].UnifiedID, 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, uint(42), rows[0].TenantID)
}

func TestContactConsentMerge_RejectsUnscopedAndMismatchedWrites(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	database := setupCustomerServiceTestDB(t)
	repo := repository.NewContactConsentRepository(database)
	for _, ctx := range []context.Context{context.Background(), tenant.WithSystemScope(context.Background()), tenant.WithSystemScope(tenant.WithTenantID(context.Background(), 41))} {
		err := repo.AppendEvent(ctx, &model.ContactConsentEvent{TenantID: 42, OneID: "foreign", Purpose: model.ConsentPurposeMarketing, Decision: model.ConsentDecisionGrant, OccurredAt: time.Now()})
		require.Error(t, err, "缺少租户或显式跨租户写入必须拒绝")
		err = repo.UpsertPreference(ctx, &model.ContactPreference{TenantID: 42, OneID: "foreign", Purpose: model.ConsentPurposeMarketing, Paused: true})
		require.Error(t, err, "偏好不能借系统豁免写其他租户")
	}
}
