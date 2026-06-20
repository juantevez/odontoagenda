// Package postgres contiene los adaptadores de salida (repositorios PostgreSQL)
// del bounded context Professional.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/juantevez/odontoagenda/context/professional/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/professional/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

type ProfessionalPostgresRepository struct {
	pool *pgxpool.Pool
}

func NewProfessionalPostgresRepository(pool *pgxpool.Pool) *ProfessionalPostgresRepository {
	return &ProfessionalPostgresRepository{pool: pool}
}

func (r *ProfessionalPostgresRepository) Save(_ context.Context, _ *aggregate.Professional) error {
	return fmt.Errorf("not implemented")
}

func (r *ProfessionalPostgresRepository) Update(_ context.Context, _ *aggregate.Professional) error {
	return fmt.Errorf("not implemented")
}

func (r *ProfessionalPostgresRepository) FindByID(_ context.Context, _ sharedtypes.ProfessionalID) (*aggregate.Professional, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *ProfessionalPostgresRepository) FindByClinic(_ context.Context, _ sharedtypes.ClinicID, _ *valueobject.SpecialtyCode) ([]*aggregate.Professional, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *ProfessionalPostgresRepository) FindBySpecialty(_ context.Context, _ valueobject.SpecialtyCode) ([]*aggregate.Professional, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *ProfessionalPostgresRepository) FindAvailableAt(_ context.Context, _ sharedtypes.ClinicID, _ time.Time, _ *valueobject.SpecialtyCode) ([]*aggregate.Professional, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *ProfessionalPostgresRepository) FindWithExpiringLicenses(_ context.Context, _ int) ([]*aggregate.Professional, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *ProfessionalPostgresRepository) ExistsByNationalID(_ context.Context, _ string) (bool, error) {
	return false, fmt.Errorf("not implemented")
}
