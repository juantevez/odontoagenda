// Package postgres contiene los adaptadores de salida PostgreSQL del bounded context Coverage.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/juantevez/odontoagenda/context/coverage/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/coverage/domain/repository"
	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// AgreementPostgresRepository implementa repository.AgreementRepository.
// Los métodos están en stub: retornan "not implemented".
// Implementación real pendiente en Fase 1 del plan de desarrollo.
type AgreementPostgresRepository struct {
	pool *pgxpool.Pool
}

func NewAgreementPostgresRepository(pool *pgxpool.Pool) *AgreementPostgresRepository {
	return &AgreementPostgresRepository{pool: pool}
}

func (r *AgreementPostgresRepository) Save(_ context.Context, _ *aggregate.Agreement) error {
	return fmt.Errorf("not implemented")
}

func (r *AgreementPostgresRepository) Update(_ context.Context, _ *aggregate.Agreement) error {
	return fmt.Errorf("not implemented")
}

func (r *AgreementPostgresRepository) FindByID(_ context.Context, _ uuid.UUID) (*aggregate.Agreement, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *AgreementPostgresRepository) FindByCode(_ context.Context, _ string) (*aggregate.Agreement, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *AgreementPostgresRepository) FindActive(_ context.Context, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Agreement], error) {
	return sharedtypes.PagedResult[*aggregate.Agreement]{}, fmt.Errorf("not implemented")
}

func (r *AgreementPostgresRepository) FindByProviderType(_ context.Context, _ valueobject.ProviderType, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Agreement], error) {
	return sharedtypes.PagedResult[*aggregate.Agreement]{}, fmt.Errorf("not implemented")
}

func (r *AgreementPostgresRepository) ExistsByCode(_ context.Context, _ string) (bool, error) {
	return false, fmt.Errorf("not implemented")
}

func (r *AgreementPostgresRepository) FindExpired(_ context.Context, _ time.Time) ([]*aggregate.Agreement, error) {
	return nil, fmt.Errorf("not implemented")
}

// ── PatientAffiliationPostgresRepository ─────────────────────────

type PatientAffiliationPostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPatientAffiliationPostgresRepository(pool *pgxpool.Pool) *PatientAffiliationPostgresRepository {
	return &PatientAffiliationPostgresRepository{pool: pool}
}

func (r *PatientAffiliationPostgresRepository) Upsert(_ context.Context, _ repository.PatientAffiliation) error {
	return fmt.Errorf("not implemented")
}

func (r *PatientAffiliationPostgresRepository) FindActive(_ context.Context, _ sharedtypes.PatientID, _ uuid.UUID) (*repository.PatientAffiliation, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *PatientAffiliationPostgresRepository) SuspendByPatient(_ context.Context, _ sharedtypes.PatientID) error {
	return fmt.Errorf("not implemented")
}
