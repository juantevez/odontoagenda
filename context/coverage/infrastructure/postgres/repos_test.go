package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juantevez/odontoagenda/context/coverage/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/coverage/domain/repository"
	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
	"github.com/juantevez/odontoagenda/context/coverage/infrastructure/postgres"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// All three repositories are stubs that return "not implemented" for every method.
// Tests pass nil for the pool because the stubs never dereference it.

// ── AgreementPostgresRepository ───────────────────────────────────

func TestAgreementRepo_Save(t *testing.T) {
	r := postgres.NewAgreementPostgresRepository(nil)
	if err := r.Save(context.Background(), &aggregate.Agreement{}); err == nil {
		t.Fatal("expected not-implemented error")
	}
}

func TestAgreementRepo_Update(t *testing.T) {
	r := postgres.NewAgreementPostgresRepository(nil)
	if err := r.Update(context.Background(), &aggregate.Agreement{}); err == nil {
		t.Fatal("expected not-implemented error")
	}
}

func TestAgreementRepo_FindByID(t *testing.T) {
	r := postgres.NewAgreementPostgresRepository(nil)
	res, err := r.FindByID(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected not-implemented error")
	}
	if res != nil {
		t.Fatal("expected nil result")
	}
}

func TestAgreementRepo_FindByCode(t *testing.T) {
	r := postgres.NewAgreementPostgresRepository(nil)
	res, err := r.FindByCode(context.Background(), "AGR001")
	if err == nil {
		t.Fatal("expected not-implemented error")
	}
	if res != nil {
		t.Fatal("expected nil result")
	}
}

func TestAgreementRepo_FindActive(t *testing.T) {
	r := postgres.NewAgreementPostgresRepository(nil)
	_, err := r.FindActive(context.Background(), sharedtypes.NewPage(10, 0))
	if err == nil {
		t.Fatal("expected not-implemented error")
	}
}

func TestAgreementRepo_FindByProviderType(t *testing.T) {
	r := postgres.NewAgreementPostgresRepository(nil)
	_, err := r.FindByProviderType(context.Background(), valueobject.ProviderTypeObraSocial, sharedtypes.NewPage(10, 0))
	if err == nil {
		t.Fatal("expected not-implemented error")
	}
}

func TestAgreementRepo_ExistsByCode(t *testing.T) {
	r := postgres.NewAgreementPostgresRepository(nil)
	ok, err := r.ExistsByCode(context.Background(), "AGR001")
	if err == nil {
		t.Fatal("expected not-implemented error")
	}
	if ok {
		t.Fatal("expected false")
	}
}

func TestAgreementRepo_FindExpired(t *testing.T) {
	r := postgres.NewAgreementPostgresRepository(nil)
	res, err := r.FindExpired(context.Background(), time.Now())
	if err == nil {
		t.Fatal("expected not-implemented error")
	}
	if res != nil {
		t.Fatal("expected nil result")
	}
}

// ── PatientAffiliationPostgresRepository ─────────────────────────

func TestAffiliationRepo_Upsert(t *testing.T) {
	r := postgres.NewPatientAffiliationPostgresRepository(nil)
	if err := r.Upsert(context.Background(), repository.PatientAffiliation{}); err == nil {
		t.Fatal("expected not-implemented error")
	}
}

func TestAffiliationRepo_FindActive(t *testing.T) {
	r := postgres.NewPatientAffiliationPostgresRepository(nil)
	res, err := r.FindActive(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected not-implemented error")
	}
	if res != nil {
		t.Fatal("expected nil result")
	}
}

func TestAffiliationRepo_SuspendByPatient(t *testing.T) {
	r := postgres.NewPatientAffiliationPostgresRepository(nil)
	if err := r.SuspendByPatient(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected not-implemented error")
	}
}

// ── AuthorizationPostgresRepository ──────────────────────────────

func TestAuthorizationRepo_Save(t *testing.T) {
	r := postgres.NewAuthorizationPostgresRepository(nil)
	if err := r.Save(context.Background(), &aggregate.AuthorizationRequest{}); err == nil {
		t.Fatal("expected not-implemented error")
	}
}

func TestAuthorizationRepo_Update(t *testing.T) {
	r := postgres.NewAuthorizationPostgresRepository(nil)
	if err := r.Update(context.Background(), &aggregate.AuthorizationRequest{}); err == nil {
		t.Fatal("expected not-implemented error")
	}
}

func TestAuthorizationRepo_FindByID(t *testing.T) {
	r := postgres.NewAuthorizationPostgresRepository(nil)
	res, err := r.FindByID(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected not-implemented error")
	}
	if res != nil {
		t.Fatal("expected nil result")
	}
}

func TestAuthorizationRepo_FindPendingByAgreement(t *testing.T) {
	r := postgres.NewAuthorizationPostgresRepository(nil)
	res, err := r.FindPendingByAgreement(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected not-implemented error")
	}
	if res != nil {
		t.Fatal("expected nil result")
	}
}

func TestAuthorizationRepo_FindPendingByPatient(t *testing.T) {
	r := postgres.NewAuthorizationPostgresRepository(nil)
	res, err := r.FindPendingByPatient(context.Background(), uuid.New(), "PROC001")
	if err == nil {
		t.Fatal("expected not-implemented error")
	}
	if res != nil {
		t.Fatal("expected nil result")
	}
}

func TestAuthorizationRepo_FindExpired(t *testing.T) {
	r := postgres.NewAuthorizationPostgresRepository(nil)
	res, err := r.FindExpired(context.Background(), time.Now())
	if err == nil {
		t.Fatal("expected not-implemented error")
	}
	if res != nil {
		t.Fatal("expected nil result")
	}
}
