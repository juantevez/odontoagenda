// Package postgres contiene los adaptadores de salida PostgreSQL del bounded context Billing.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/juantevez/odontoagenda/context/billing/domain/aggregate"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// QuotePostgresRepository implementa repository.QuoteRepository.
// Métodos en stub — implementación real pendiente en Fase 1.
type QuotePostgresRepository struct {
	pool *pgxpool.Pool
}

func NewQuotePostgresRepository(pool *pgxpool.Pool) *QuotePostgresRepository {
	return &QuotePostgresRepository{pool: pool}
}

func (r *QuotePostgresRepository) Save(_ context.Context, _ *aggregate.Quote) error {
	return fmt.Errorf("not implemented")
}

func (r *QuotePostgresRepository) Update(_ context.Context, _ *aggregate.Quote) error {
	return fmt.Errorf("not implemented")
}

func (r *QuotePostgresRepository) FindByID(_ context.Context, _ uuid.UUID) (*aggregate.Quote, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *QuotePostgresRepository) FindByAppointmentID(_ context.Context, _ uuid.UUID) (*aggregate.Quote, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *QuotePostgresRepository) FindActiveByPatient(_ context.Context, _ sharedtypes.PatientID, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Quote], error) {
	return sharedtypes.PagedResult[*aggregate.Quote]{}, fmt.Errorf("not implemented")
}

func (r *QuotePostgresRepository) FindByPatient(_ context.Context, _ sharedtypes.PatientID, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Quote], error) {
	return sharedtypes.PagedResult[*aggregate.Quote]{}, fmt.Errorf("not implemented")
}

func (r *QuotePostgresRepository) FindByClinicAndDate(_ context.Context, _ sharedtypes.ClinicID, _ time.Time) ([]*aggregate.Quote, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *QuotePostgresRepository) ExistsByAppointmentID(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil // retorna false para que CreateQuote siempre intente crear (stub)
}
