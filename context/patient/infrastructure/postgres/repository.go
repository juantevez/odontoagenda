// Package postgres contiene los adaptadores de salida (repositorios PostgreSQL)
// del bounded context Patient.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/juantevez/odontoagenda/context/patient/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/patient/domain/coverage"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── PatientPostgresRepository ─────────────────────────────────────

type PatientPostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPatientPostgresRepository(pool *pgxpool.Pool) *PatientPostgresRepository {
	return &PatientPostgresRepository{pool: pool}
}

func (r *PatientPostgresRepository) Save(_ context.Context, _ *aggregate.Patient) error {
	return fmt.Errorf("not implemented")
}

func (r *PatientPostgresRepository) Update(_ context.Context, _ *aggregate.Patient) error {
	return fmt.Errorf("not implemented")
}

func (r *PatientPostgresRepository) FindByID(_ context.Context, _ sharedtypes.PatientID) (*aggregate.Patient, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *PatientPostgresRepository) FindByNationalID(_ context.Context, _ sharedvo.NationalID) (*aggregate.Patient, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *PatientPostgresRepository) FindByUserID(_ context.Context, _ sharedtypes.UserID) (*aggregate.Patient, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *PatientPostgresRepository) Search(_ context.Context, _ string, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Patient], error) {
	return sharedtypes.PagedResult[*aggregate.Patient]{}, fmt.Errorf("not implemented")
}

func (r *PatientPostgresRepository) FindNearClinic(_ context.Context, _ sharedtypes.ClinicID, _ float64, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Patient], error) {
	return sharedtypes.PagedResult[*aggregate.Patient]{}, fmt.Errorf("not implemented")
}

func (r *PatientPostgresRepository) ExistsByNationalID(_ context.Context, _ sharedvo.NationalID) (bool, error) {
	return false, fmt.Errorf("not implemented")
}

func (r *PatientPostgresRepository) FindPotentialDuplicates(_ context.Context, _ string, _ string) ([]*aggregate.Patient, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *PatientPostgresRepository) Archive(_ context.Context, _ sharedtypes.PatientID, _ string, _ sharedtypes.UserID) error {
	return fmt.Errorf("not implemented")
}

// ── CoverageHistoryPostgresRepository ────────────────────────────

type CoverageHistoryPostgresRepository struct {
	pool *pgxpool.Pool
}

func NewCoverageHistoryPostgresRepository(pool *pgxpool.Pool) *CoverageHistoryPostgresRepository {
	return &CoverageHistoryPostgresRepository{pool: pool}
}

func (r *CoverageHistoryPostgresRepository) Append(_ context.Context, _ coverage.CoverageHistoryEntry) error {
	return fmt.Errorf("not implemented")
}

func (r *CoverageHistoryPostgresRepository) FindByPatientID(_ context.Context, _ sharedtypes.PatientID, _ sharedtypes.Page) (sharedtypes.PagedResult[coverage.CoverageHistoryEntry], error) {
	return sharedtypes.PagedResult[coverage.CoverageHistoryEntry]{}, fmt.Errorf("not implemented")
}
