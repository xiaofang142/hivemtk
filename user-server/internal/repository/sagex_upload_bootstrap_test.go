package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func TestSageXInstallationRepositoryTenantInstallationUnique(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	db := testutil.NewTestDB(t, &model.SageXInstallation{})
	repo := NewSageXInstallationRepository()
	repo.SetDB(db)
	ctxA := tenant.WithTenantID(context.Background(), 11)
	ctxB := tenant.WithTenantID(context.Background(), 22)

	first := &model.SageXInstallation{
		TenantID:       11,
		InstallationID: "shared-installation",
		AccountID:      "101",
		Token:          "token-a",
		Status:         1,
	}
	if err := repo.Create(ctxA, first); err != nil {
		t.Fatalf("create first installation: %v", err)
	}
	duplicate := &model.SageXInstallation{
		TenantID:       11,
		InstallationID: "shared-installation",
		AccountID:      "101",
		Token:          "token-a-duplicate",
		Status:         1,
	}
	if err := repo.Create(ctxA, duplicate); err == nil {
		t.Fatal("same tenant + installation ID duplicate was accepted; partial unique index missing")
	}

	otherTenant := &model.SageXInstallation{
		TenantID:       22,
		InstallationID: "shared-installation",
		AccountID:      "202",
		Token:          "token-b",
		Status:         1,
	}
	if err := repo.Create(ctxB, otherTenant); err != nil {
		t.Fatalf("same installation ID in another tenant must remain isolated: %v", err)
	}

	for i := 0; i < 2; i++ {
		emptyID := &model.SageXInstallation{
			TenantID:  11,
			AccountID: "101",
			Token:     fmt.Sprintf("token-empty-%d", i),
			Status:    1,
		}
		if err := repo.Create(ctxA, emptyID); err != nil {
			t.Fatalf("empty installation ID must stay outside partial unique index: %v", err)
		}
	}
}

func TestSageXInstallationRepositoryConcurrentCreateHasSingleWinner(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	db := testutil.NewTestDB(t, &model.SageXInstallation{})
	repo := NewSageXInstallationRepository()
	repo.SetDB(db)
	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			ctx := tenant.WithTenantID(context.Background(), 11)
			errs <- repo.Create(ctx, &model.SageXInstallation{
				TenantID:       11,
				InstallationID: "concurrent-installation",
				AccountID:      "101",
				Token:          fmt.Sprintf("token-concurrent-%d", worker),
				Status:         1,
			})
		}(i)
	}
	wg.Wait()
	close(errs)

	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent same-key creates succeeded %d times, want exactly 1", successes)
	}
}

func TestSageXInstallationRepositoryTenantLookupNilDBFailsClosed(t *testing.T) {
	repo := &SageXInstallationRepository{}
	var (
		inst *model.SageXInstallation
		err  error
	)
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("lookup panicked: %v", recovered)
			}
		}()
		inst, err = repo.GetByTenantAndInstallationID(context.Background(), 11, "installation-a")
	}()
	if !errors.Is(err, gorm.ErrInvalidDB) || inst != nil {
		t.Fatalf("nil DB lookup must fail closed with ErrInvalidDB: inst=%+v err=%v", inst, err)
	}
}

type sageXInstallationCreateIfAbsent interface {
	CreateIfAbsent(context.Context, *model.SageXInstallation) (bool, error)
}

type sageXInstallationTenantLookup interface {
	GetByTenantAndInstallationID(context.Context, uint, string) (*model.SageXInstallation, error)
}

func TestSageXInstallationRepositoryCreateIfAbsentIsIdempotent(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	db := testutil.NewTestDB(t, &model.SageXInstallation{})
	repo := NewSageXInstallationRepository()
	repo.SetDB(db)
	creator, ok := any(repo).(sageXInstallationCreateIfAbsent)
	if !ok {
		t.Fatal("repository does not expose atomic CreateIfAbsent seam")
	}
	lookup, ok := any(repo).(sageXInstallationTenantLookup)
	if !ok {
		t.Fatal("repository does not expose explicit tenant-aware installation lookup")
	}
	ctx := tenant.WithTenantID(context.Background(), 11)
	first := &model.SageXInstallation{
		TenantID:       11,
		InstallationID: "atomic-id",
		AccountID:      "101",
		Token:          "atomic-token-first",
		Status:         1,
	}
	created, err := creator.CreateIfAbsent(ctx, first)
	if err != nil || !created {
		t.Fatalf("first CreateIfAbsent created=%v err=%v", created, err)
	}
	second := &model.SageXInstallation{
		TenantID:       11,
		InstallationID: "atomic-id",
		AccountID:      "101",
		Token:          "atomic-token-second",
		Status:         1,
	}
	created, err = creator.CreateIfAbsent(ctx, second)
	if err != nil || created {
		t.Fatalf("duplicate CreateIfAbsent created=%v err=%v, want false/nil", created, err)
	}
	got, err := lookup.GetByTenantAndInstallationID(ctx, 11, "atomic-id")
	if err != nil || got == nil || got.Token != "atomic-token-first" {
		t.Fatalf("idempotent row token changed: got=%+v err=%v", got, err)
	}

	otherTenantCtx := tenant.WithTenantID(context.Background(), 22)
	created, err = creator.CreateIfAbsent(otherTenantCtx, &model.SageXInstallation{
		TenantID:       22,
		InstallationID: "atomic-id",
		AccountID:      "202",
		Token:          "atomic-token-other-tenant",
		Status:         1,
	})
	if err != nil || !created {
		t.Fatalf("other tenant same ID created=%v err=%v", created, err)
	}
}

func TestSageXInstallationRepositoryConcurrentCreateIfAbsentHasSingleToken(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	db := testutil.NewTestDB(t, &model.SageXInstallation{})
	repo := NewSageXInstallationRepository()
	repo.SetDB(db)
	creator, ok := any(repo).(sageXInstallationCreateIfAbsent)
	if !ok {
		t.Fatal("repository does not expose atomic CreateIfAbsent seam")
	}
	lookup, ok := any(repo).(sageXInstallationTenantLookup)
	if !ok {
		t.Fatal("repository does not expose explicit tenant-aware installation lookup")
	}

	const workers = 12
	type result struct {
		created bool
		token   string
		err     error
	}
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			token := fmt.Sprintf("atomic-race-token-%d", worker)
			ctx := tenant.WithTenantID(context.Background(), 11)
			created, err := creator.CreateIfAbsent(ctx, &model.SageXInstallation{
				TenantID:       11,
				InstallationID: "atomic-race-id",
				AccountID:      "101",
				Token:          token,
				Status:         1,
			})
			results <- result{created: created, token: token, err: err}
		}(i)
	}
	wg.Wait()
	close(results)

	winners := make([]string, 0, 1)
	for res := range results {
		if res.err != nil {
			t.Fatalf("CreateIfAbsent returned error: %v", res.err)
		}
		if res.created {
			winners = append(winners, res.token)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("CreateIfAbsent winners=%d, want 1", len(winners))
	}
	ctx := tenant.WithTenantID(context.Background(), 11)
	got, err := lookup.GetByTenantAndInstallationID(ctx, 11, "atomic-race-id")
	if err != nil || got == nil || got.Token != winners[0] {
		t.Fatalf("stored token=%+v, want winner token %q, err=%v", got, winners[0], err)
	}
}
