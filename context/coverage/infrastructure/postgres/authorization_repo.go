package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/juantevez/odontoagenda/context/coverage/domain/aggregate"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// AuthorizationPostgresRepository implementa repository.AuthorizationRepository.
type AuthorizationPostgresRepository struct {
	pool *pgxpool.Pool
}

func NewAuthorizationPostgresRepository(pool *pgxpool.Pool) *AuthorizationPostgresRepository {
	return &AuthorizationPostgresRepository{pool: pool}
}

func (r *AuthorizationPostgresRepository) Save(_ context.Context, _ *aggregate.AuthorizationRequest) error {
	return fmt.Errorf("not implemented")
}

func (r *AuthorizationPostgresRepository) Update(_ context.Context, _ *aggregate.AuthorizationRequest) error {
	return fmt.Errorf("not implemented")
}

func (r *AuthorizationPostgresRepository) FindByID(_ context.Context, _ uuid.UUID) (*aggregate.AuthorizationRequest, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *AuthorizationPostgresRepository) FindPendingByAgreement(_ context.Context, _ uuid.UUID) ([]*aggregate.AuthorizationRequest, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *AuthorizationPostgresRepository) FindPendingByPatient(_ context.Context, _ sharedtypes.PatientID, _ string) (*aggregate.AuthorizationRequest, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *AuthorizationPostgresRepository) FindExpired(_ context.Context, _ time.Time) ([]*aggregate.AuthorizationRequest, error) {
	return nil, fmt.Errorf("not implemented")
}
