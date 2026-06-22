// Package postgres — implementación real del repositorio de políticas de cancelación.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/juantevez/odontoagenda/context/billing/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ClinicCancellationPolicyPostgresRepository implementa el puerto de salida
// para billing.clinic_cancellation_policies.
type ClinicCancellationPolicyPostgresRepository struct {
	pool *pgxpool.Pool
}

func NewClinicCancellationPolicyPostgresRepository(pool *pgxpool.Pool) *ClinicCancellationPolicyPostgresRepository {
	return &ClinicCancellationPolicyPostgresRepository{pool: pool}
}

// FindByClinic retorna la política configurada para la sede.
// Si no existe registro, retorna DefaultCancellationPolicy() sin error.
func (r *ClinicCancellationPolicyPostgresRepository) FindByClinic(
	ctx context.Context,
	clinicID sharedtypes.ClinicID,
) (valueobject.CancellationPolicy, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			cancellation_free_hours,
			late_cancellation_fee_percent,
			no_show_fee_percent,
			min_fee_cents
		FROM billing.clinic_cancellation_policies
		WHERE clinic_id = $1`,
		clinicID,
	)

	var policy valueobject.CancellationPolicy
	err := row.Scan(
		&policy.FreeHours,
		&policy.LateCancellationPercent,
		&policy.NoShowPercent,
		&policy.MinFeeCents,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Sin configuración específica: usar los valores por defecto del sistema.
			return valueobject.DefaultCancellationPolicy(), nil
		}
		return valueobject.CancellationPolicy{}, fmt.Errorf("CancellationPolicyRepo.FindByClinic: %w", err)
	}
	return policy, nil
}

// Upsert crea o actualiza la política de cancelación de una sede.
func (r *ClinicCancellationPolicyPostgresRepository) Upsert(
	ctx context.Context,
	clinicID sharedtypes.ClinicID,
	policy valueobject.CancellationPolicy,
	updatedBy interface{},
) error {
	// Convertir updatedBy a *uuid.UUID de forma segura.
	var updatedByUUID *uuid.UUID
	if updatedBy != nil {
		if id, ok := updatedBy.(uuid.UUID); ok {
			updatedByUUID = &id
		}
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO billing.clinic_cancellation_policies (
			clinic_id,
			cancellation_free_hours,
			late_cancellation_fee_percent,
			no_show_fee_percent,
			min_fee_cents,
			updated_at,
			updated_by
		) VALUES ($1, $2, $3, $4, $5, now(), $6)
		ON CONFLICT (clinic_id) DO UPDATE SET
			cancellation_free_hours      = EXCLUDED.cancellation_free_hours,
			late_cancellation_fee_percent = EXCLUDED.late_cancellation_fee_percent,
			no_show_fee_percent          = EXCLUDED.no_show_fee_percent,
			min_fee_cents                = EXCLUDED.min_fee_cents,
			updated_at                   = now(),
			updated_by                   = EXCLUDED.updated_by`,
		clinicID,
		policy.FreeHours,
		policy.LateCancellationPercent,
		policy.NoShowPercent,
		policy.MinFeeCents,
		updatedByUUID,
	)
	if err != nil {
		return fmt.Errorf("CancellationPolicyRepo.Upsert: %w", err)
	}
	return nil
}
