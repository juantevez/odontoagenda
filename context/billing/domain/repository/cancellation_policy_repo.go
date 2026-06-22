// Package repository — puerto de salida para ClinicCancellationPolicy.
package repository

import (
	"context"

	"github.com/juantevez/odontoagenda/context/billing/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ClinicCancellationPolicyRepository gestiona la política de cancelación por sede.
// Si no existe registro para una sede, retorna la política por defecto del sistema.
type ClinicCancellationPolicyRepository interface {
	// FindByClinic retorna la política de la sede.
	// Si no existe, retorna DefaultCancellationPolicy() sin error (nunca retorna ErrNotFound).
	FindByClinic(ctx context.Context, clinicID sharedtypes.ClinicID) (valueobject.CancellationPolicy, error)

	// Upsert crea o actualiza la política de una sede.
	Upsert(ctx context.Context, clinicID sharedtypes.ClinicID, policy valueobject.CancellationPolicy, updatedBy interface{}) error
}
